package world

import (
	"cmp"
	"maps"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/sliceutil"
)

// ticker implements World ticking methods.
type ticker struct {
	interval time.Duration
}

// tickLoop starts ticking the World 20 times every second, updating all
// entities, blocks and other features such as the time and weather of the
// world, as required.
func (t ticker) tickLoop(w *World) {
	tc := time.NewTicker(t.interval)
	defer tc.Stop()
	for {
		select {
		case <-tc.C:
			<-w.exec(t.tick)
		case <-w.closing:
			// World is being closed: Stop ticking and get rid of a task.
			w.running.Done()
			return
		}
	}
}

// AdvanceTick advances the World by a single tick. It is generally only useful
// for Worlds created with Config.Synchronous set: other Worlds tick
// automatically 20 times per second. Synchronous Worlds tick loaded chunks
// even when no viewers are present.
func (w *World) AdvanceTick() {
	<-w.exec(ticker{}.tick)
}

// tick performs a tick on the World and updates the time, weather, blocks and
// entities that require updates.
func (t ticker) tick(tx *Tx) {
	viewers, loaders := tx.World().allViewers()
	w := tx.World()
	if javaRedstone {
		w.ticked = true
	}

	w.set.Lock()
	if s := w.set.Spawn; s[1] > tx.Range()[1] && w.Dimension() == Overworld {
		// Vanilla will set the spawn position's Y value to max to indicate that
		// the player should spawn at the highest position in the world.
		w.set.Spawn[1] = tx.highestObstructingBlock(s[0], s[2]) + 1
	}
	if len(viewers) == 0 && w.set.CurrentTick != 0 && !w.conf.Synchronous {
		// Don't continue ticking if no viewers are in the world. Synchronous
		// worlds only tick on explicit AdvanceTick calls, so they always tick.
		w.set.Unlock()
		return
	}
	if w.advance {
		w.set.CurrentTick++
		if w.set.TimeCycle {
			w.set.Time++
		}
		if w.set.WeatherCycle {
			w.advanceWeather()
		}
	}

	if javaRedstone {
		w.updateWeatherIntensity()
	}
	rain, thunder, tick, tim, cycle := w.set.Raining, w.set.Thundering && w.set.Raining, w.set.CurrentTick, int(w.set.Time), w.set.TimeCycle

	tryAdvanceDay := false
	if tx.w.set.RequiredSleepTicks > 0 {
		tx.w.set.RequiredSleepTicks--
		tryAdvanceDay = tx.w.set.RequiredSleepTicks <= 0
	}

	w.set.Unlock()

	if tryAdvanceDay {
		t.tryAdvanceDay(tx, cycle)
	}

	if tick%20 == 0 {
		for _, viewer := range viewers {
			if w.Dimension().TimeCycle() && cycle {
				viewer.ViewTime(tim)
			}
			if w.Dimension().WeatherCycle() {
				viewer.ViewWeather(rain, thunder)
			}
		}
	}
	if thunder {
		w.tickLightning(tx)
	}

	if !javaRedstone {
		t.tickEntities(tx, tick)
		for _, h := range tickHooks {
			h(tx)
		}
		w.scheduledUpdates.tick(tx, tick)
		t.tickBlocksRandomly(tx, loaders, tick)
		t.performNeighbourUpdates(tx)
		w.redstone.tick(tx, tick)
		return
	}
	// Java Edition's tick order (Crossfly Full, Java redstone).
	// Block ticks scheduled during this tick (by entities as well) count their delay from this world tick.
	w.scheduledUpdates.currentTick = tick
	clear(w.pistonDisplacement)
	w.scheduledUpdates.tick(tx, tick)
	// Java runs the neighbour updates a scheduled tick causes right away, so pistons they reach queue their
	// block events for this tick, before block entities (moving blocks) tick: a sticky piston turned off by an
	// observer then retracts while its blocks are still moving and drops them, as in flying machines.
	t.flushUpdates(tx, tick)
	w.runBlockEvents(tx, func() { t.flushUpdates(tx, tick) })
	// Java (ServerLevel.tick) ticks entities after block ticks and block events and before block entities: TNT
	// primed by a block event this tick burns its first fuse tick now, as in Java.
	t.tickEntities(tx, tick)
	for _, h := range tickHooks {
		h(tx)
	}
	t.flushUpdates(tx, tick)
	t.tickBlocksRandomly(tx, loaders, tick)
	t.performNeighbourUpdates(tx)
	w.redstone.tick(tx, tick)
	// Block events queued from here on (a piston its moved blocks updated as they landed) wait for the next tick, as
	// Java Edition runs block events once a tick, before block entities tick.
}

// performNeighbourUpdates performs all block updates that came as a result of a neighbouring block being changed.
func (t ticker) performNeighbourUpdates(tx *Tx) {
	if !javaRedstone {
		t.performNeighbourUpdateRound(tx)
		return
	}
	// Updates caused by these updates run in the same tick, as Java Edition runs a chain of neighbour updates at once
	// (a line of powered rails turns on together). The cap stops a block that keeps updating itself.
	for done := 0; len(tx.World().neighbourUpdates) > 0 && done < maxChainedNeighbourUpdates; {
		done += t.performNeighbourUpdateRound(tx)
	}
}

// maxChainedNeighbourUpdates is how many neighbour updates may run in one tick before the rest wait for the next.
const maxChainedNeighbourUpdates = 1 << 16

// performNeighbourUpdateRound runs the neighbour updates queued so far and returns how many there were.
func (t ticker) performNeighbourUpdateRound(tx *Tx) int {
	updates := slices.Clone(tx.World().neighbourUpdates)
	clear(tx.World().neighbourUpdates)
	tx.World().neighbourUpdates = tx.World().neighbourUpdates[:0]

	tx.World().neighbourUpdateDepth++
	defer func() { tx.World().neighbourUpdateDepth-- }()
	for _, update := range updates {
		pos, changedNeighbour := update.pos, update.neighbour
		if ticker, ok := tx.Block(pos).(NeighbourUpdateTicker); ok {
			ticker.NeighbourUpdateTick(pos, changedNeighbour, tx)
		}
		if liquid, ok := tx.additionalLiquid(pos); ok {
			if ticker, ok := liquid.(NeighbourUpdateTicker); ok {
				ticker.NeighbourUpdateTick(pos, changedNeighbour, tx)
			}
		}
	}
	return len(updates)
}

// tickBlocksRandomly executes random block ticks in loaded chunks within range of loaders.
func (t ticker) tickBlocksRandomly(tx *Tx, loaders []*Loader, tick int64) {
	var (
		r             = int32(tx.World().tickRange())
		g             randUint4
		blockEntities []cube.Pos
		randomBlocks  []cube.Pos
	)
	if r == 0 {
		// NOP if the simulation distance is 0.
		return
	}

	loaded := make([]ChunkPos, 0, len(loaders))
	if tx.World().conf.Synchronous {
		loaded = slices.Collect(maps.Keys(tx.World().chunks))
	} else {
		for _, loader := range loaders {
			loader.mu.RLock()
			pos := loader.pos
			loader.mu.RUnlock()

			loaded = append(loaded, pos)
		}
	}

	bt := &tx.World().beTicks
	if javaRedstone {
		bt.begin()
		defer bt.end()
	}
	for pos, c := range tx.World().chunks {
		if !t.anyWithinDistance(pos, loaded, r) {
			// No loaders in this chunk that are within the simulation distance, so proceed to the next.
			continue
		}
		if javaRedstone {
			bt.collect(pos, c)
		} else {
			blockEntities = append(blockEntities, slices.Collect(maps.Keys(c.BlockEntities))...)
		}

		cx, cz := int(pos[0]<<4), int(pos[1]<<4)

		// We generate up to j random positions for every sub chunk.
		for j := 0; j < tx.World().conf.RandomTickSpeed; j++ {
			x, y, z := g.uint4(tx.World().r), g.uint4(tx.World().r), g.uint4(tx.World().r)

			for i, sub := range c.Sub() {
				if sub.Empty() {
					// SubChunk is empty, so skip it right away.
					continue
				}
				// Generally we would want to make sure the block has its block entities, but provided blocks
				// with block entities are generally ticked already, we are safe to assume that blocks
				// implementing the RandomTicker don't rely on additional block entity data.
				if rid := sub.Layers()[0].At(x, y, z); tx.World().conf.Blocks.RandomTickBlock(rid) {
					subY := (i + (tx.Range().Min() >> 4)) << 4
					randomBlocks = append(randomBlocks, cube.Pos{cx + int(x), subY + int(y), cz + int(z)})

					// Only generate new coordinates if a tickable block was actually found. If not, we can just re-use
					// the coordinates for the next sub chunk.
					x, y, z = g.uint4(tx.World().r), g.uint4(tx.World().r), g.uint4(tx.World().r)
				}
			}
		}
	}

	for _, pos := range randomBlocks {
		if rb, ok := tx.Block(pos).(RandomTicker); ok {
			rb.RandomTick(pos, tx, tx.World().r)
		}
	}
	if !javaRedstone {
		for _, pos := range blockEntities {
			if tb, ok := tx.Block(pos).(TickerBlock); ok {
				tb.Tick(tick, pos, tx)
			}
		}
		return
	}
	// Java ticks block entities in the order they were added; map order would make machines random.
	w := tx.World()
	bt.sort(w)
	bt.entries = bt.run(bt.entries, func(pos cube.Pos) {
		if tb, ok := tx.Block(pos).(TickerBlock); ok {
			tb.Tick(tick, pos, tx)
			// Java runs a block entity's updates before the next one ticks: a moved block that arrives updates a
			// piston beside it while the rest of its line is still moving (and so cannot be pushed yet).
			t.flushUpdates(tx, tick)
		}
	})
	// Then what watches the block entities (comparators reading containers), once every block entity has ticked.
	w.analogWatchers = w.analogWatchers[:0]
	bt.run(bt.analogList(), func(pos cube.Pos) {
		if aw, ok := tx.Block(pos).(RedstoneAnalogWatcher); ok {
			w.analogWatchers = append(w.analogWatchers, pos)
			aw.RedstoneAnalogCheck(pos, tx)
		}
	})
}

// anyWithinDistance checks if any of the ChunkPos loaded are within the distance r of the ChunkPos pos.
func (t ticker) anyWithinDistance(pos ChunkPos, loaded []ChunkPos, r int32) bool {
	for _, chunkPos := range loaded {
		xDiff, zDiff := chunkPos[0]-pos[0], chunkPos[1]-pos[1]
		if (xDiff*xDiff)+(zDiff*zDiff) <= r*r {
			// The chunk was within the simulation distance of at least one viewer, so we can proceed to
			// ticking the block.
			return true
		}
	}
	return false
}

// tickEntities ticks all entities in the world, making sure they are still located in the correct chunks and
// updating where necessary.
func (t ticker) tickEntities(tx *Tx, tick int64) {
	for handle, lastPos := range tx.World().entities {
		e := handle.mustEntity(tx)
		chunkPos := chunkPosFromVec3(handle.data.Pos)

		c, ok := tx.World().chunks[chunkPos]
		if !ok {
			continue
		}

		if lastPos != chunkPos {
			// The entity was stored using an outdated chunk position. We update it and make sure it is ready
			// for loaders to view it.
			tx.World().entities[handle] = chunkPos
			// Both chunks are saved again: the entity is stored with the chunk it is in.
			c.Entities, c.modified = append(c.Entities, handle), true

			var viewers []Viewer

			// When changing an entity's world, then teleporting it immediately, we could end up in a situation
			// where the old chunk of the entity was not loaded. In this case, it should be safe simply to ignore
			// the loaders from the old chunk. We can assume they never saw the entity in the first place.
			if old, ok := tx.World().chunks[lastPos]; ok {
				old.Entities, old.modified = sliceutil.DeleteVal(old.Entities, handle), true
				viewers = old.viewers
			}

			for _, viewer := range viewers {
				if slices.Index(c.viewers, viewer) == -1 {
					// First we hide the entity from all loaders that were previously viewing it, but no
					// longer are.
					viewer.HideEntity(e)
				}
			}
			for _, viewer := range c.viewers {
				if slices.Index(viewers, viewer) == -1 {
					// Then we show the entity to all loaders that are now viewing the entity in the new
					// chunk.
					showEntity(e, viewer)
				}
			}
		}

		if tx.World().conf.Synchronous || len(c.viewers) > 0 {
			if te, ok := e.(TickerEntity); ok {
				te.Tick(tx, tick)
			}
		}
	}
}

// randUint4 is a structure used to generate random uint4s.
type randUint4 struct {
	x uint64
	n uint8
}

// uint4 returns a random uint4.
func (g *randUint4) uint4(r *rand.Rand) uint8 {
	if g.n == 0 {
		g.x = r.Uint64()
		g.n = 16
	}
	val := g.x & 0b1111

	g.x >>= 4
	g.n--
	return uint8(val)
}

// scheduledTickQueue implements a queue for scheduled block updates. Scheduled
// block updates are both position and block type specific.
type scheduledTickQueue struct {
	ticks         []scheduledTick
	furthestTicks map[scheduledTickIndex]int64
	currentTick   int64
	nextOrder     int64
}

type scheduledTick struct {
	pos      cube.Pos
	b        Block
	bhash    uint64
	t        int64
	priority TickPriority
	order    int64
}

type scheduledTickIndex struct {
	pos  cube.Pos
	hash uint64
}

// newScheduledTickQueue creates a queue for scheduled block ticks.
func newScheduledTickQueue(tick int64) *scheduledTickQueue {
	return &scheduledTickQueue{furthestTicks: make(map[scheduledTickIndex]int64), currentTick: tick}
}

// tick processes scheduled ticks, calling ScheduledTicker.ScheduledTick for any
// block update that is scheduled for the tick passed, and removing it from the
// queue.
func (queue *scheduledTickQueue) tick(tx *Tx, tick int64) {
	queue.currentTick = tick
	if !javaRedstone {
		queue.tickInOrder(tx, tick)
		return
	}

	// Collect before invoking callbacks: they may schedule, cancel or retarget future work.
	due := make([]scheduledTick, 0)
	future := queue.ticks[:0]
	for _, t := range queue.ticks {
		if t.t <= tick {
			due = append(due, t)
		} else {
			future = append(future, t)
		}
	}
	queue.ticks = future
	maps.DeleteFunc(queue.furthestTicks, func(_ scheduledTickIndex, t int64) bool { return t <= tick })
	// Java Edition keeps fluid ticks in a queue of their own and runs it after all block ticks of the tick
	// (ServerLevel.tick: blockTicks, then fluidTicks). Run in between, the updates of flowing water reach observers
	// and pistons before their own ticks do, a flying machine's pistons fire in another order, and a flooder ends
	// up moving its waterlogged block with a full push, which places it dry: the flooder loses its water.
	slices.SortStableFunc(due, func(a, b scheduledTick) int {
		if d := cmp.Compare(fluidTick(a), fluidTick(b)); d != 0 {
			return d
		}
		if d := cmp.Compare(a.t, b.t); d != 0 {
			return d
		}
		if d := cmp.Compare(a.priority, b.priority); d != 0 {
			return d
		}
		return cmp.Compare(a.order, b.order)
	})
	w := tx.World()
	// Java Edition keeps one scheduled tick per block type and position. Ticks here are kept per state, so blocks
	// schedule every state they may have by then; once one tick of a block type ran at a position, the others of
	// that type due in the same tick are dropped, as they would not exist in Java.
	type posType struct {
		pos  cube.Pos
		base uint64
	}
	ran := make(map[posType]bool)
	for _, t := range due {
		b := tx.Block(t.pos)
		var key posType
		if t.b != nil {
			base, _ := t.b.Hash()
			key = posType{pos: t.pos, base: base}
			if ran[key] {
				continue
			}
		}
		if ticker, ok := b.(ScheduledTicker); ok && w.conf.Blocks.BlockHash(b) == t.bhash {
			if t.b != nil {
				ran[key] = true
			}
			ticker.ScheduledTick(t.pos, tx, w.r)
		} else if liquid, ok := tx.additionalLiquid(t.pos); ok && w.conf.Blocks.BlockHash(liquid) == t.bhash {
			if ticker, ok := liquid.(ScheduledTicker); ok {
				ticker.ScheduledTick(t.pos, tx, w.r)
			}
		}
	}
}

// fluidTick is 1 for a tick scheduled for a liquid, which Java Edition runs after the block ticks, and 0 otherwise.
func fluidTick(t scheduledTick) int {
	if _, ok := t.b.(Liquid); ok {
		return 1
	}
	return 0
}

// tickInOrder runs the due ticks in the order they were scheduled, without Java Edition's priorities.
func (queue *scheduledTickQueue) tickInOrder(tx *Tx, tick int64) {
	w := tx.World()
	for _, t := range queue.ticks {
		if t.t > tick {
			continue
		}
		b := tx.Block(t.pos)
		if ticker, ok := b.(ScheduledTicker); ok && w.conf.Blocks.BlockHash(b) == t.bhash {
			ticker.ScheduledTick(t.pos, tx, w.r)
		} else if liquid, ok := tx.additionalLiquid(t.pos); ok && w.conf.Blocks.BlockHash(liquid) == t.bhash {
			if ticker, ok := liquid.(ScheduledTicker); ok {
				ticker.ScheduledTick(t.pos, tx, w.r)
			}
		}
	}

	// Clear scheduled ticks that were processed from the queue.
	queue.ticks = slices.DeleteFunc(queue.ticks, func(t scheduledTick) bool {
		return t.t <= tick
	})
	maps.DeleteFunc(queue.furthestTicks, func(index scheduledTickIndex, t int64) bool {
		return t <= tick
	})
}

// schedule schedules a block update at the position passed for the block type
// passed after a specific delay. A block update is only scheduled if no block
// update with the same position and block type is already scheduled at a later
// time than the newly scheduled update.
func (queue *scheduledTickQueue) schedule(br BlockRegistry, pos cube.Pos, b Block, delay time.Duration) {
	queue.schedulePriority(br, pos, b, delay, TickPriorityNormal)
}

func (queue *scheduledTickQueue) schedulePriority(br BlockRegistry, pos cube.Pos, b Block, delay time.Duration, priority TickPriority) {
	resTick := queue.currentTick + int64(max(delay/(time.Second/20), 1))
	index := scheduledTickIndex{pos: pos, hash: br.BlockHash(b)}
	if t, ok := queue.furthestTicks[index]; ok && t >= resTick && t > queue.currentTick {
		return
	}
	queue.furthestTicks[index] = resTick
	queue.nextOrder++
	queue.ticks = append(queue.ticks, scheduledTick{pos: pos, t: resTick, b: b, bhash: index.hash, priority: priority, order: queue.nextOrder})
}

// retarget moves the ticks scheduled at pos for from over to to, for a block whose state changed
// without a block update, so that they still run.
func (queue *scheduledTickQueue) retarget(br BlockRegistry, pos cube.Pos, from, to Block) {
	old := scheduledTickIndex{pos: pos, hash: br.BlockHash(from)}
	furthest, ok := queue.furthestTicks[old]
	if !ok {
		return
	}
	index := scheduledTickIndex{pos: pos, hash: br.BlockHash(to)}
	for i, t := range queue.ticks {
		if t.pos == pos && t.bhash == old.hash {
			queue.ticks[i].b, queue.ticks[i].bhash = to, index.hash
		}
	}
	delete(queue.furthestTicks, old)
	queue.furthestTicks[index] = max(queue.furthestTicks[index], furthest)
}

// fromChunk returns all scheduled ticks positioned within a ChunkPos.
func (queue *scheduledTickQueue) fromChunk(pos ChunkPos) []scheduledTick {
	m := make([]scheduledTick, 0, 8)
	for _, t := range queue.ticks {
		if pos == chunkPosFromBlockPos(t.pos) {
			m = append(m, t)
		}
	}
	return m
}

// removeChunk removes all scheduled ticks positioned within a ChunkPos.
func (queue *scheduledTickQueue) removeChunk(pos ChunkPos) {
	queue.ticks = slices.DeleteFunc(queue.ticks, func(tick scheduledTick) bool {
		return chunkPosFromBlockPos(tick.pos) == pos
	})
	maps.DeleteFunc(queue.furthestTicks, func(index scheduledTickIndex, _ int64) bool {
		return chunkPosFromBlockPos(index.pos) == pos
	})
}

// add adds a slice of scheduled ticks to the queue. It assumes no duplicate
// ticks are present in the slice.
func (queue *scheduledTickQueue) add(ticks []scheduledTick) {
	for i := range ticks {
		if ticks[i].order == 0 {
			queue.nextOrder++
			ticks[i].order = queue.nextOrder
		} else {
			queue.nextOrder = max(queue.nextOrder, ticks[i].order)
		}
	}
	queue.ticks = append(queue.ticks, ticks...)
	for _, t := range ticks {
		index := scheduledTickIndex{pos: t.pos, hash: t.bhash}
		if existing, ok := queue.furthestTicks[index]; ok {
			queue.furthestTicks[index] = max(existing, t.t)
		} else {
			queue.furthestTicks[index] = t.t
		}
	}
}
