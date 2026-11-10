//go:build crossfly_full

package world

import (
	"maps"
	"slices"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// The parts of the world for Java redstone (Crossfly Full): handling the changes made between ticks at once, block
// updates past direct neighbours, and the transaction methods the Java redstone blocks use.

// checkAnalogWatchers lets the comparators found in the last tick look at what they read.
func (w *World) checkAnalogWatchers(tx *Tx) {
	for _, pos := range w.analogWatchers {
		if aw, ok := tx.Block(pos).(RedstoneAnalogWatcher); ok {
			aw.RedstoneAnalogCheck(pos, tx)
		}
	}
}

// watchAnalogNow adds a comparator set between ticks to the watchers, so that a container changed next to it before
// the next tick wakes it at once, as Java's Container.setChanged updates the comparators beside it.
func (w *World) watchAnalogNow(pos cube.Pos, b Block) {
	if _, ok := b.(RedstoneAnalogWatcher); ok && !w.beTicks.active && !slices.Contains(w.analogWatchers, pos) {
		w.analogWatchers = append(w.analogWatchers, pos)
	}
}

// pendingWork is the work queued for the next tick before a transaction ran: neighbour updates and redstone changes.
type pendingWork struct {
	updates int
	dirty   map[cube.Pos]redstoneDirty
}

// reacts reports whether the world reacts to changes between ticks: only while someone is in it, as only then
// does it tick (ticker.tick).
func (w *World) reacts() bool {
	return w.conf.Synchronous || w.hasViewers()
}

// pending notes the work queued for the next tick.
func (w *World) pending() pendingWork {
	p := pendingWork{updates: len(w.neighbourUpdates)}
	// A world nobody is in does not react between ticks (settle returns at once), so the redstone changes are not
	// copied: they pile up until a player comes, and copying them on every transaction cost a share of a core.
	if w.redstone != nil && len(w.redstone.dirty) > 0 && w.reacts() {
		// Copied by entry, not with maps.Clone: a clone copies the table at its peak size.
		p.dirty = make(map[cube.Pos]redstoneDirty, len(w.redstone.dirty))
		maps.Copy(p.dirty, w.redstone.dirty)
	}
	return p
}

// settle reacts to the changes a transaction run between ticks made (a player, a command, a plugin): their neighbour
// updates and the redstone they affect are handled now, in the current tick, as Java Edition handles them at once,
// instead of in the next tick, including the updates the reaction itself causes. Work queued before the transaction
// waits for the next tick.
func (w *World) settle(before pendingWork) {
	// A world nobody is in does not tick (ticker.tick): it does not react between ticks either.
	if !w.reacts() {
		return
	}
	newUpdates := len(w.neighbourUpdates) > before.updates
	newDirty := w.redstone != nil && len(w.redstone.dirty) > len(before.dirty)
	if !newUpdates && !newDirty && (w.redstone == nil || maps.EqualFunc(w.redstone.dirty, before.dirty, func(a, b redstoneDirty) bool { return a == b })) {
		// Nothing changed in blocks, but a container may have (a player moving items): comparators look now.
		if len(w.analogWatchers) > 0 {
			tx := newTx(w)
			w.checkAnalogWatchers(tx)
			tx.close()
			tx.runDeferred()
		}
		return
	}
	// Set the earlier work aside and handle only the transaction's.
	earlier := slices.Clone(w.neighbourUpdates[:before.updates])
	w.neighbourUpdates = slices.Delete(w.neighbourUpdates, 0, before.updates)
	var earlierDirty map[cube.Pos]redstoneDirty
	if w.redstone != nil {
		earlierDirty = before.dirty
		for pos, d := range before.dirty {
			if now, ok := w.redstone.dirty[pos]; ok && now == d {
				delete(w.redstone.dirty, pos)
			}
		}
	}

	tx := newTx(w)
	if w.redstone == nil {
		ticker{}.performNeighbourUpdates(tx)
	} else {
		// All of it, as flushUpdates does in a tick: dust whose power changed updates the neighbours of its
		// neighbours, and a repeater at the end of a line must hear of it now, not in the next tick.
		w.set.Lock()
		tick := w.set.CurrentTick
		w.set.Unlock()
		ticker{}.flushUpdates(tx, tick)
	}
	w.checkAnalogWatchers(tx)
	tx.close()
	tx.runDeferred()

	// What was queued before, then what the reaction queued, all for the next tick.
	w.neighbourUpdates = append(earlier, w.neighbourUpdates...)
	if w.redstone != nil && len(earlierDirty) > 0 {
		if w.redstone.dirty == nil {
			w.redstone.dirty = make(map[cube.Pos]redstoneDirty, len(earlierDirty))
		}
		for pos, d := range earlierDirty {
			if _, ok := w.redstone.dirty[pos]; !ok {
				w.redstone.dirty[pos] = d
			}
		}
	}
}

// updateNeighboursOfNeighbours updates the neighbours of every neighbour of pos, as Java Edition's redstone dust
// does when its power changes (updateNeighborsAt for itself and each side): a block two away, such as TNT under
// the block a wire powers, hears of it.
func (w *World) updateNeighboursOfNeighbours(pos cube.Pos) {
	pos.Neighbours(func(n cube.Pos) {
		n.Neighbours(func(m cube.Pos) {
			if m != pos {
				w.updateNeighbour(m, n)
			}
		}, w.Range())
	}, w.Range())
}

func (w *World) cancelBlockUpdate(pos cube.Pos, b Block) {
	if w == nil || pos.OutOfBounds(w.Range()) {
		return
	}
	w.scheduledUpdates.cancel(w.conf.Blocks, pos, b)
}

// flushUpdates runs the queued neighbour updates and the redstone changes they cause, again while either queues more
// neighbour updates (dust updating the neighbours of its neighbours), as Java Edition runs all of it at once. The
// rounds are bounded; what is left waits for the next phase.
func (t ticker) flushUpdates(tx *Tx, tick int64) {
	w := tx.World()
	for i := 0; i < maxUpdateFlushRounds; i++ {
		t.performNeighbourUpdates(tx)
		w.redstone.tick(tx, tick)
		if len(w.neighbourUpdates) == 0 {
			return
		}
	}
}

// maxUpdateFlushRounds bounds flushUpdates.
const maxUpdateFlushRounds = 16

// scheduled reports whether a tick is scheduled for b at pos that has not run yet.
func (queue *scheduledTickQueue) scheduled(br BlockRegistry, pos cube.Pos, b Block) bool {
	// LevelTicks.hasScheduledTick: a tick of the block's type waits at the position.
	return queue.pending[scheduledTickType{pos: pos, kind: scheduledTickKind(b)}] > 0
}

// cancel removes the ticks scheduled at pos for b.
func (queue *scheduledTickQueue) cancel(br BlockRegistry, pos cube.Pos, b Block) {
	index := scheduledTickIndex{pos: pos, hash: br.BlockHash(b)}
	kind := scheduledTickKind(b)
	queue.ticks = slices.DeleteFunc(queue.ticks, func(t scheduledTick) bool {
		if t.pos != index.pos || scheduledTickKind(t.b) != kind {
			return false
		}
		if t.t > queue.currentTick {
			queue.unpend(t)
		}
		return true
	})
	delete(queue.furthestTicks, index)
}

// BlockUpdateScheduled reports whether an update is scheduled for the block b at pos and has not run yet. Java Edition
// keeps one scheduled tick per block at a time: a block may use this to do the same.
func (tx *Tx) BlockUpdateScheduled(pos cube.Pos, b Block) bool {
	return tx.World().scheduledUpdates.scheduled(tx.World().conf.Blocks, pos, b)
}

// NotifyNeighbour gives the block at pos a neighbour update, as if the block at changed had changed, without changing
// anything. Redstone components use it to update the blocks a change affects past their direct neighbours, as Java
// Edition's repeater updates the blocks around the block in front of it.
func (tx *Tx) NotifyNeighbour(pos, changed cube.Pos) {
	w := tx.World()
	if pos.OutOfBounds(w.Range()) {
		return
	}
	w.updateNeighbour(pos, changed)
}

// RunNeighbourUpdatesNow runs fn and then, unless neighbour updates are being run already, the neighbour updates fn
// caused and every update those cause, before it returns. Java Edition's CollectingNeighborUpdater runs updates that
// are not part of a running chain at once: a block a shape update destroys (a coral fan whose support a piston
// replaces with a moving block) updates its neighbours before the piston places its next moving block, so TNT
// beside it primes in the middle of the push. Updates queued before fn wait for their usual turn.
func (tx *Tx) RunNeighbourUpdatesNow(fn func()) {
	w := tx.World()
	if w.neighbourUpdateDepth > 0 {
		fn()
		return
	}
	earlier := w.neighbourUpdates
	w.neighbourUpdates = nil
	fn()
	ticker{}.performNeighbourUpdates(tx)
	w.neighbourUpdates = append(earlier, w.neighbourUpdates...)
}

// FlushNeighbourUpdates runs every queued neighbour update and the redstone changes they cause now, unless neighbour
// updates are being run already (then they run in that chain), as Java Edition runs a block's updates as it changes.
func (tx *Tx) FlushNeighbourUpdates() {
	if tx.World().neighbourUpdateDepth > 0 {
		return
	}
	ticker{}.flushUpdates(tx, tx.CurrentTick())
}

// CancelBlockUpdate cancels scheduled updates for a block at pos.
func (tx *Tx) CancelBlockUpdate(pos cube.Pos, b Block) {
	tx.World().cancelBlockUpdate(pos, b)
}

// RedstoneInputPower returns the power the redstone engine last gave the consumer or action block at pos, worked out
// with the same rules as its updates (dust powers only what it points at). It is 0 for a block the engine has not
// updated. A block reading its power outside its own redstone update, such as on a neighbour update, uses this.
func (tx *Tx) RedstoneInputPower(pos cube.Pos) int {
	return tx.World().redstone.power[pos]
}

// ScheduleBlockUpdatePriority schedules a block tick with an explicit same-tick priority.
// It otherwise follows ScheduleBlockUpdate's existing deduplication and minimum-delay rules.
func (tx *Tx) ScheduleBlockUpdatePriority(pos cube.Pos, b Block, delay time.Duration, priority TickPriority) {
	w := tx.World()
	if pos.OutOfBounds(w.Range()) {
		return
	}
	priority = min(TickPriorityExtremelyLow, max(TickPriorityExtremelyHigh, priority))
	w.scheduledUpdates.schedulePriority(w.conf.Blocks, pos, b, delay, priority)
}
