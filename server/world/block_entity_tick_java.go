//go:build crossfly_full

package world

import (
	"cmp"
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// Java Edition ticks block entities in the order they were created (Column.blockEntityOrder), every one of them,
// then lets the ones that watch block entities (comparators) look. Most block entities do neither (redstone dust,
// chests, signs), so only the ones that tick or watch are listed and sorted each tick. A block entity that becomes
// one of them during the tick (a block set in its place) is added where the full list would have had it, so the
// order is the same as ticking the full list.

// blockEntityTick is a block entity in the tick list: when it was created and where it is.
type blockEntityTick struct {
	order uint64
	pos   cube.Pos
}

// compareBlockEntityTick orders block entities by creation, then by position.
func compareBlockEntityTick(a, b blockEntityTick) int {
	if a.order != b.order {
		return cmp.Compare(a.order, b.order)
	}
	return compareBlockPos(a.pos, b.pos)
}

// tickingBlockEntity reports whether the block entity b does anything in the block entity tick.
func tickingBlockEntity(b Block) bool {
	if _, ok := b.(TickerBlock); ok {
		return true
	}
	_, ok := b.(RedstoneAnalogWatcher)
	return ok
}

// blockEntityTicks holds the block entities ticked in the current world tick. Between collect and end it also
// follows changes of block entities (noteWrite).
type blockEntityTicks struct {
	active bool
	sorted bool
	// chunks are the chunks whose block entities were collected.
	chunks map[ChunkPos]struct{}
	// written are the positions whose block entity changed since they were collected.
	written map[cube.Pos]struct{}
	// entries are the collected block entities that tick or watch, and the ones that became one.
	entries []blockEntityTick
	// pending are block entities that became one before the list was sorted; their order is looked up then.
	pending []cube.Pos
	// skipped are block entities that became one after a pass went past them, for the next pass.
	skipped []blockEntityTick
	// running is the list of the pass being run, at the index of the block entity being visited.
	running []blockEntityTick
	at      int
	analog  []blockEntityTick
}

// begin starts collecting the block entities of a tick.
func (bt *blockEntityTicks) begin() {
	if bt.chunks == nil {
		bt.chunks = make(map[ChunkPos]struct{})
		bt.written = make(map[cube.Pos]struct{})
	}
	clear(bt.chunks)
	clear(bt.written)
	bt.entries, bt.pending, bt.skipped, bt.analog = bt.entries[:0], bt.pending[:0], bt.skipped[:0], bt.analog[:0]
	bt.active, bt.sorted, bt.running = true, false, nil
}

// collect lists the block entities of the chunk c at pos that tick or watch.
func (bt *blockEntityTicks) collect(pos ChunkPos, c *Column) {
	bt.chunks[pos] = struct{}{}
	for p, b := range c.BlockEntities {
		if tickingBlockEntity(b) {
			bt.entries = append(bt.entries, blockEntityTick{pos: p})
		}
	}
}

// sort orders the list by when each block entity was created, looked up now as the full list was sorted.
func (bt *blockEntityTicks) sort(w *World) {
	order := func(pos cube.Pos) uint64 {
		if c, ok := w.chunks[chunkPosFromBlockPos(pos)]; ok {
			return c.blockEntityTickOrder(pos)
		}
		return 0
	}
	for i := range bt.entries {
		bt.entries[i].order = order(bt.entries[i].pos)
	}
	for _, pos := range bt.pending {
		bt.entries = append(bt.entries, blockEntityTick{order: order(pos), pos: pos})
	}
	slices.SortFunc(bt.entries, compareBlockEntityTick)
	bt.sorted = true
}

// run calls f for each block entity of list in order, and for the ones that become one during the pass and come
// after the one being visited. It returns the list with those.
func (bt *blockEntityTicks) run(list []blockEntityTick, f func(pos cube.Pos)) []blockEntityTick {
	bt.running = list
	for bt.at = 0; bt.at < len(bt.running); bt.at++ {
		f(bt.running[bt.at].pos)
	}
	list, bt.running = bt.running, nil
	return list
}

// analogList returns the list for the pass after the tick pass: the tick list and the block entities that became
// one after the tick pass went past them.
func (bt *blockEntityTicks) analogList() []blockEntityTick {
	bt.analog = append(append(bt.analog[:0], bt.entries...), bt.skipped...)
	if len(bt.skipped) > 0 {
		slices.SortFunc(bt.analog, compareBlockEntityTick)
	}
	return bt.analog
}

// end stops following block entity changes.
func (bt *blockEntityTicks) end() {
	bt.active, bt.running = false, nil
}

// noteWrite is called before the block entity at pos in the chunk c changes, with the block entity there before
// (had is false if there was none). The first change of a position since it was collected sees the block entity
// collected there: if it was collected and did not tick or watch, the full list held it, so it is added to the list
// in case it is one now (each pass checks the block at its turn).
func (bt *blockEntityTicks) noteWrite(c *Column, pos cube.Pos, before Block, had bool) {
	if !bt.active {
		return
	}
	if _, ok := bt.written[pos]; ok {
		return
	}
	if _, ok := bt.chunks[chunkPosFromBlockPos(pos)]; !ok {
		return
	}
	bt.written[pos] = struct{}{}
	if !had || tickingBlockEntity(before) {
		return
	}
	if !bt.sorted {
		bt.pending = append(bt.pending, pos)
		return
	}
	// Nothing changed this block entity since the list was sorted: its order now is the one it was sorted by.
	e := blockEntityTick{order: c.blockEntityTickOrder(pos), pos: pos}
	if bt.running != nil && compareBlockEntityTick(e, bt.running[bt.at]) > 0 {
		rest := bt.running[bt.at+1:]
		i, _ := slices.BinarySearchFunc(rest, e, compareBlockEntityTick)
		bt.running = slices.Insert(bt.running, bt.at+1+i, e)
		return
	}
	bt.skipped = append(bt.skipped, e)
}
