package world

import (
	"maps"
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// RedstoneUpdateCause describes the world event that caused a redstone update to be evaluated.
type RedstoneUpdateCause uint8

const (
	// RedstoneUpdateCauseBlockUpdate means a block or liquid change invalidated nearby redstone.
	RedstoneUpdateCauseBlockUpdate RedstoneUpdateCause = iota
	// RedstoneUpdateCauseScheduledTick means a scheduled redstone tick invalidated a component.
	RedstoneUpdateCauseScheduledTick
	// RedstoneUpdateCauseCompilerRebuild means a redstone compiler rebuild invalidated a component.
	RedstoneUpdateCauseCompilerRebuild
)

// RedstoneUpdate represents a redstone state transition proposed by the world redstone engine. Handlers may cancel
// the event to suppress the proposed mutation and any propagation from that mutation.
type RedstoneUpdate struct {
	// Pos is the block position that will receive the update.
	Pos cube.Pos
	// ChangedNeighbour is the neighbouring block that caused the update, if any.
	ChangedNeighbour cube.Pos
	// HasChangedNeighbour reports whether ChangedNeighbour is set. A zero block position is valid, so callers must not
	// use ChangedNeighbour == cube.Pos{} as an absence check.
	HasChangedNeighbour bool
	// ChangedRedstoneRelevant reports whether ChangedNeighbour was a redstone component before or after the change.
	ChangedRedstoneRelevant bool
	// Source is the original block position that caused this redstone propagation, if known.
	Source cube.Pos
	// HasSource reports whether Source is set.
	HasSource bool
	// Before is the block currently at Pos.
	Before Block
	// After is the block that will replace Before, if the update is a block-state update. After is nil for updates
	// that perform side effects instead of replacing the block.
	After Block
	// OldPower is the last redstone power observed by the engine at Pos.
	OldPower int
	// NewPower is the redstone power observed by the engine at Pos for this update.
	NewPower int
	// CurrentTick is the world tick during which the update was evaluated.
	CurrentTick int64
	// Cause identifies why the update was evaluated.
	Cause RedstoneUpdateCause
}

// RedstonePowerSource is implemented by blocks that emit redstone power. The face passed is the face of the source
// block that power is being read from.
type RedstonePowerSource interface {
	RedstonePower(pos cube.Pos, tx *Tx, face cube.Face) int
}

// RedstoneStrongPowerSource is implemented by sources that strongly power blocks from specific faces. Strong power may
// pass through solid blocks, unlike weak redstone wire power.
type RedstoneStrongPowerSource interface {
	RedstoneStrongPower(pos cube.Pos, tx *Tx, face cube.Face) int
}

// RedstoneWeakBlockPowerer may be implemented by sources whose weak output can make an adjacent conductive block
// weakly powered. Weakly powered blocks activate adjacent mechanisms, but do not power adjacent redstone dust.
type RedstoneWeakBlockPowerer interface {
	RedstoneWeaklyPowersBlocks() bool
}

// RedstonePowerRelayer is implemented by redstone wire-like blocks that relay power through a compiled redstone
// network. The returned value is the signal loss when power crosses one relayer edge.
type RedstonePowerRelayer interface {
	RedstoneSignalLoss(pos cube.Pos, tx *Tx) int
}

// RedstonePowerRelayerNeighbourer may be implemented by relayers with non-adjacent connections, such as redstone
// wire stepping up and down block edges.
type RedstonePowerRelayerNeighbourer interface {
	RedstoneRelayerNeighbours(pos cube.Pos, tx *Tx) []cube.Pos
}

// RedstoneLocalRelayerNeighbourer may be implemented by a RedstonePowerRelayerNeighbourer whose relayer neighbours
// depend only on the blocks at most two blocks away from it (on each axis), such as redstone dust. The redstone
// engine then keeps them until a block that near changes.
type RedstoneLocalRelayerNeighbourer interface {
	RedstoneLocalRelayerNeighbours()
}

// RedstonePowerConsumer is implemented by blocks whose block state changes when their input power changes. The
// returned block is written to the world if changed is true and the redstone update event is not cancelled.
type RedstonePowerConsumer interface {
	RedstonePowerUpdate(pos cube.Pos, tx *Tx, power int) (after Block, changed bool)
}

// RedstonePowerPostUpdater may be implemented by consumers that need to apply side effects after an uncancelled
// redstone state update, such as syncing the other half of a door.
type RedstonePowerPostUpdater interface {
	RedstonePowerPostUpdate(pos cube.Pos, tx *Tx, before, after Block, oldPower, newPower int)
}

// RedstonePowerAction is implemented by blocks that perform a side effect when their input power changes, such as TNT
// priming on a rising edge. The action is run only if the redstone update event is not cancelled.
type RedstonePowerAction interface {
	RedstonePowerAction(pos cube.Pos, tx *Tx, oldPower, newPower int)
}

// RedstoneAnalogWatcher is implemented by blocks that read something that changes without a block update, such as how
// full a container is (a comparator). RedstoneAnalogCheck is called once a tick after the blocks with block entities
// ticked, and after a change made between ticks, for the watchers among the ticking block entities.
type RedstoneAnalogWatcher interface {
	RedstoneAnalogCheck(pos cube.Pos, tx *Tx)
}

// RedstonePowerContextAction may be implemented by action blocks that need the proposed update metadata to distinguish
// self-caused redstone changes from external block updates.
type RedstonePowerContextAction interface {
	RedstonePowerActionUpdate(pos cube.Pos, tx *Tx, update RedstoneUpdate)
}

// RedstoneNonConductive may be implemented by solid redstone blocks that should not conduct strong power.
type RedstoneNonConductive interface {
	RedstoneNonConductive()
}

// redstoneEngine evaluates the immediate-power network for a world and caches transient power state. Neighbour updates
// and scheduled block ticks remain the semantic model for block behaviour; the engine just resolves wire/relayer power
// and lets consumers and actions schedule any delayed or directional changes themselves.
type redstoneEngine struct {
	currentTick int64
	dirty       map[cube.Pos]redstoneDirty
	// spareDirty is an empty map tick swaps in for dirty, so a tick does not copy the dirty set. Only maps that held
	// few positions are kept: a map does not shrink, and ranging over or copying one costs its peak size.
	spareDirty map[cube.Pos]redstoneDirty
	// candidates is the reused buffer for the sorted dirty positions of a tick.
	candidates []cube.Pos
	// relevantRIDs caches isRedstoneRelevant by block runtime ID: 1 relevant, 2 not, 0 unknown.
	relevantRIDs      []uint8
	power             map[cube.Pos]int
	output            map[cube.Pos]int
	evaluating        map[cube.Pos]struct{}
	suppressedSources map[cube.Pos]int
	torchBurnout      map[cube.Pos]redstoneTorchBurnout
	// compileState is the graph compilation state kept for the next compilation.
	compileState *redstoneCompile
	// lists are what compilations found around positions, kept until a block near them changes.
	lists redstoneListMemo
	// held is the power conductors hold, worked out during a graph power evaluation (heldConductorPower), while
	// holding is set.
	held    map[uint64]redstoneHeldPower
	holding bool
	// stepBuffers are walk states for powerFrom.
	stepBuffers []*redstoneStepBuffer
	// graphBuffers is the scratch state for graphPower.
	graphBuffers *redstoneGraphBuffers
}

// redstoneDirty records why a position needs redstone evaluation.
type redstoneDirty struct {
	changed                 cube.Pos
	hasChanged              bool
	changedRedstoneRelevant bool
	source                  cube.Pos
	hasSource               bool
	cause                   RedstoneUpdateCause
}

// redstoneTorchBurnout tracks a torch's recent turn-off history and whether it is currently burned out.
type redstoneTorchBurnout struct {
	offTicks             []int64
	burnedOut            bool
	pendingSelfTriggered bool
}

const (
	redstoneTorchBurnoutThreshold   = 8
	redstoneTorchBurnoutWindowTicks = 60
)

// redstoneGraph is the immediate-power network compiled for one engine tick. Edges are signal loss between relayers
// only — never delay, direction locking, or mechanical state — so delayed or directional components stay as graph
// endpoints and drive their own state changes via scheduled ticks or actions.
type redstoneGraph struct {
	nodes []redstoneNode
	edges []redstoneEdge
	// index is the index of each node by position.
	index map[cube.Pos]int
}

// redstoneNode represents one redstone-relevant block in a compiled graph.
type redstoneNode struct {
	pos    cube.Pos
	source bool
	sink   bool
}

// redstoneEdge connects two graph nodes with a signal-loss weight.
type redstoneEdge struct {
	from, to int
	weight   int
}

// newRedstoneEngine creates a redstone engine initialized at tick.
func newRedstoneEngine(tick int64) *redstoneEngine {
	return &redstoneEngine{
		currentTick: tick,
		dirty:       make(map[cube.Pos]redstoneDirty),
		power:       make(map[cube.Pos]int),
		output:      make(map[cube.Pos]int),
		evaluating:  make(map[cube.Pos]struct{}),
	}
}

// invalidateAround marks pos and its direct neighbours dirty for redstone evaluation.
func (e *redstoneEngine) invalidateAround(pos, changed cube.Pos, cause RedstoneUpdateCause, r cube.Range) {
	e.invalidateAroundWith(pos, redstoneDirty{changed: changed, hasChanged: true, source: changed, hasSource: true, cause: cause}, r)
}

// invalidateAroundBlockChange marks pos and its direct neighbours dirty for a block change. A change of a block that is
// not redstone-relevant, with nothing redstone-relevant within reach (redstoneNearby), is skipped: graph compilation
// would find nothing around it.
func (e *redstoneEngine) invalidateAroundBlockChange(w *World, pos cube.Pos, before, after Block, cause RedstoneUpdateCause, r cube.Range) {
	if e == nil || pos.OutOfBounds(r) {
		return
	}
	relevant := isRedstoneRelevant(before) || isRedstoneRelevant(after)
	if !relevant && !e.redstoneNearby(w, pos) {
		return
	}
	d := redstoneDirty{
		changed:                 pos,
		hasChanged:              true,
		changedRedstoneRelevant: relevant,
		source:                  pos,
		hasSource:               true,
		cause:                   cause,
	}
	e.invalidateAroundWith(pos, d, r)
}

// invalidateAroundLiquidChange marks pos and its direct neighbours dirty for a liquid change, unless nothing
// redstone-relevant is within reach (redstoneNearby).
func (e *redstoneEngine) invalidateAroundLiquidChange(w *World, pos cube.Pos, r cube.Range) {
	if e == nil || pos.OutOfBounds(r) || !e.redstoneNearby(w, pos) {
		return
	}
	e.invalidateAround(pos, pos, RedstoneUpdateCauseBlockUpdate, r)
}

// redstoneReach is the Manhattan distance from a changed block within which graph compilation can find redstone:
// the change marks pos and its neighbours dirty, and compile looks from every dirty position at its neighbours and,
// through a conductor among them, at the conductor's neighbours (compileRegion, compileAdjacentRedstone).
const redstoneReach = 3

// redstoneNearby reports whether a redstone-relevant block, or a chunk that is not loaded, is within redstoneReach of
// pos. If not, the positions a change at pos marks dirty compile an empty graph, so the change can be skipped.
func (e *redstoneEngine) redstoneNearby(w *World, pos cube.Pos) bool {
	r := w.Range()
	var (
		col      *Column
		colPos   ChunkPos
		colValid bool
	)
	for dy := -redstoneReach; dy <= redstoneReach; dy++ {
		y := pos[1] + dy
		if y < r[0] || y > r[1] {
			continue
		}
		restY := redstoneReach - abs(dy)
		for dx := -restY; dx <= restY; dx++ {
			x := pos[0] + dx
			restX := restY - abs(dx)
			for dz := -restX; dz <= restX; dz++ {
				z := pos[2] + dz
				cp := ChunkPos{int32(x >> 4), int32(z >> 4)}
				if !colValid || cp != colPos {
					c, ok := w.chunks[cp]
					if !ok {
						// Unknown: a chunk loaded before the tick could hold redstone.
						return true
					}
					col, colPos, colValid = c, cp, true
				}
				if e.relevantRuntimeID(w, col.Block(uint8(x), int16(y), uint8(z), 0)) {
					return true
				}
			}
		}
	}
	return false
}

// relevantRuntimeID reports whether the block with runtime ID rid is redstone-relevant. Relevance depends only on the
// block's type, which a block entity stored at the position shares with the registered block.
func (e *redstoneEngine) relevantRuntimeID(w *World, rid uint32) bool {
	if int(rid) < len(e.relevantRIDs) {
		switch e.relevantRIDs[rid] {
		case 1:
			return true
		case 2:
			return false
		}
	}
	relevant := isRedstoneRelevant(w.conf.Blocks.BlockByRuntimeIDOrAir(rid))
	if int(rid) >= len(e.relevantRIDs) {
		if int(rid) >= 1<<20 {
			return relevant
		}
		e.relevantRIDs = append(e.relevantRIDs, make([]uint8, int(rid)+1-len(e.relevantRIDs))...)
	}
	if relevant {
		e.relevantRIDs[rid] = 1
	} else {
		e.relevantRIDs[rid] = 2
	}
	return relevant
}

// invalidateAroundWith marks pos and its direct neighbours dirty using the same update context.
func (e *redstoneEngine) invalidateAroundWith(pos cube.Pos, d redstoneDirty, r cube.Range) {
	if e == nil || pos.OutOfBounds(r) {
		return
	}
	e.invalidate(pos, d, r)
	pos.Neighbours(func(neighbour cube.Pos) {
		e.invalidate(neighbour, d, r)
	}, r)
}

// invalidate marks a single in-range position dirty for redstone evaluation.
func (e *redstoneEngine) invalidate(pos cube.Pos, d redstoneDirty, r cube.Range) {
	if pos.OutOfBounds(r) {
		return
	}
	if existing, ok := e.dirty[pos]; ok {
		e.dirty[pos] = mergeRedstoneDirty(existing, d)
		return
	}
	e.dirty[pos] = d
}

// mergeRedstoneDirty keeps the strongest cause when multiple invalidations touch the same position before evaluation.
func mergeRedstoneDirty(a, b redstoneDirty) redstoneDirty {
	if redstoneDirtyPriority(b) >= redstoneDirtyPriority(a) {
		b.changedRedstoneRelevant = a.changedRedstoneRelevant || b.changedRedstoneRelevant
		return b
	}
	a.changedRedstoneRelevant = a.changedRedstoneRelevant || b.changedRedstoneRelevant
	return a
}

func redstoneDirtyPriority(d redstoneDirty) int {
	switch d.cause {
	case RedstoneUpdateCauseBlockUpdate:
		return 3
	case RedstoneUpdateCauseScheduledTick:
		return 2
	case RedstoneUpdateCauseCompilerRebuild:
		return 1
	default:
		return 0
	}
}

// removeChunk drops transient redstone state tied to an unloaded chunk.
func (e *redstoneEngine) removeChunk(chunkPos ChunkPos) {
	if e == nil {
		return
	}
	maps.DeleteFunc(e.dirty, func(pos cube.Pos, dirty redstoneDirty) bool {
		return chunkPosFromBlockPos(pos) == chunkPos ||
			(dirty.hasChanged && chunkPosFromBlockPos(dirty.changed) == chunkPos)
	})
	maps.DeleteFunc(e.power, func(pos cube.Pos, _ int) bool {
		return chunkPosFromBlockPos(pos) == chunkPos
	})
	maps.DeleteFunc(e.output, func(pos cube.Pos, _ int) bool {
		return chunkPosFromBlockPos(pos) == chunkPos
	})
	maps.DeleteFunc(e.torchBurnout, func(pos cube.Pos, _ redstoneTorchBurnout) bool {
		return chunkPosFromBlockPos(pos) == chunkPos
	})
}

// forget clears cached redstone input and output power for pos.
func (e *redstoneEngine) forget(pos cube.Pos) {
	if e == nil {
		return
	}
	delete(e.power, pos)
	delete(e.output, pos)
	delete(e.evaluating, pos)
}

// tick evaluates all dirty redstone positions for the current world tick.
func (e *redstoneEngine) tick(tx *Tx, tick int64) {
	if e == nil || len(e.dirty) == 0 {
		return
	}
	e.currentTick = tick
	// Take the dirty set and continue with an empty one, instead of copying it: what this tick invalidates is
	// evaluated in the next. A small set is cleared and reused for a later tick.
	dirty := e.dirty
	e.dirty, e.spareDirty = e.spareDirty, nil
	if e.dirty == nil {
		// A circuit that keeps many positions dirty gets a map sized like the last set, up to the reuse limit.
		e.dirty = make(map[cube.Pos]redstoneDirty, min(len(dirty), redstoneDirtyReuseLimit))
	}
	defer func() {
		if len(dirty) <= redstoneDirtyReuseLimit {
			clear(dirty)
			e.spareDirty = dirty
		}
	}()

	candidates := e.candidates[:0]
	e.candidates = nil
	for pos := range dirty {
		candidates = append(candidates, pos)
	}
	slices.SortFunc(candidates, compareBlockPos)
	defer func() {
		// Kept until the tick ends: the updates read it (redstoneDirtyRef).
		if cap(candidates) <= redstoneDirtyReuseLimit {
			e.candidates = candidates[:0]
		}
	}()

	graph := e.compile(tx, candidates)
	cancelledSources, checkedSources := e.updateGraphSources(tx, graph, dirty, candidates)
	previousSuppressed := e.suppressedSources
	e.suppressedSources = cancelledSources
	defer func() {
		e.suppressedSources = previousSuppressed
	}()

	powers := e.graphPower(tx, graph)
	for i, node := range graph.nodes {
		if node.sink {
			e.update(tx, node.pos, redstoneDirtyRef{dirty, candidates, node.pos}, powers[i])
		}
	}
	for _, node := range graph.nodes {
		if _, ok := checkedSources[node.pos]; ok {
			continue
		}
		if node.source {
			e.updateSource(tx, node.pos, redstoneDirtyRef{dirty, candidates, node.pos})
		}
	}
}

// redstoneDirtyReuseLimit is the largest dirty set whose map, and candidate buffer, a tick keeps for reuse.
const redstoneDirtyReuseLimit = 2048

// redstoneDirtyRef refers to the dirty context of a position, found only when an update needs it: most positions a
// tick evaluates do not change.
type redstoneDirtyRef struct {
	dirty map[cube.Pos]redstoneDirty
	// sorted are the positions of dirty, sorted with compareBlockPos.
	sorted []cube.Pos
	pos    cube.Pos
}

// get returns redstoneDirtyContext for the position.
func (r redstoneDirtyRef) get() redstoneDirty {
	if d, ok := r.dirty[r.pos]; ok {
		return d
	}
	if len(r.sorted) == 0 {
		return redstoneDirtyContext(r.dirty, r.pos)
	}
	// The nearest dirty position, the first in sorted order among equally near ones, as redstoneDirtyContext.
	best, bestDist := r.sorted[0], redstoneManhattanDistance(r.pos, r.sorted[0])
	for _, dirtyPos := range r.sorted[1:] {
		if dist := redstoneManhattanDistance(r.pos, dirtyPos); dist < bestDist {
			best, bestDist = dirtyPos, dist
		}
	}
	return r.dirty[best]
}

// redstoneDirtyContext returns the direct dirty context for pos, or the nearest dirty context that pulled pos into
// the current graph. Graph compilation intentionally includes connected blocks that are not dirty themselves.
func redstoneDirtyContext(dirty map[cube.Pos]redstoneDirty, pos cube.Pos) redstoneDirty {
	if d, ok := dirty[pos]; ok {
		return d
	}
	var (
		bestPos  cube.Pos
		best     redstoneDirty
		bestDist int
		ok       bool
	)
	for dirtyPos, d := range dirty {
		dist := redstoneManhattanDistance(pos, dirtyPos)
		if !ok || dist < bestDist || (dist == bestDist && compareBlockPos(dirtyPos, bestPos) < 0) {
			bestPos, best, bestDist, ok = dirtyPos, d, dist, true
		}
	}
	if !ok {
		return redstoneDirty{changed: pos, hasChanged: true, source: pos, hasSource: true, cause: RedstoneUpdateCauseCompilerRebuild}
	}
	return best
}

// redstoneUpdate builds the public update payload for a dirty context.
func (d redstoneDirty) redstoneUpdate(pos cube.Pos, before Block, oldPower, newPower int, tick int64) RedstoneUpdate {
	return RedstoneUpdate{
		Pos:                     pos,
		ChangedNeighbour:        d.changed,
		HasChangedNeighbour:     d.hasChanged,
		ChangedRedstoneRelevant: d.changedRedstoneRelevant,
		Source:                  d.source,
		HasSource:               d.hasSource,
		Before:                  before,
		OldPower:                oldPower,
		NewPower:                newPower,
		CurrentTick:             tick,
		Cause:                   d.cause,
	}
}

// propagatedFrom keeps the original source but records the block state that changed during propagation.
func (d redstoneDirty) propagatedFrom(pos cube.Pos) redstoneDirty {
	d.changed = pos
	d.hasChanged = true
	d.changedRedstoneRelevant = true
	return d
}

func redstoneManhattanDistance(a, b cube.Pos) int {
	return abs(a[0]-b[0]) + abs(a[1]-b[1]) + abs(a[2]-b[2])
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// redstoneCompile is the state of one graph compilation. Compilation only reads the world, so what it finds out about
// a position (whether it conducts, which relayers it connects to) holds until it ends and is worked out once.
type redstoneCompile struct {
	tx    *Tx
	e     *redstoneEngine
	nodes []redstoneNode
	// seen are the positions compileRegion visited or found not redstone-relevant, adjacent the ones
	// compileAdjacentRedstone looked around.
	seen, adjacent posSet
	// relayerNeighbours caches redstoneRelayerNeighbourPositions.
	relayerNeighbours map[cube.Pos][]cube.Pos
	queue, connected  []cube.Pos
	adjacentBuf       []cube.Pos
	// col caches the chunk looked up last.
	col    *Column
	colPos ChunkPos
}

// column returns the loaded chunk holding pos, or nil.
func (c *redstoneCompile) column(pos cube.Pos) *Column {
	cp := chunkPosFromBlockPos(pos)
	if c.col != nil && c.colPos == cp {
		return c.col
	}
	col, ok := c.tx.World().chunks[cp]
	if !ok {
		return nil
	}
	c.col, c.colPos = col, cp
	return col
}

// block is World.blockLoaded.
func (c *redstoneCompile) block(pos cube.Pos) (Block, bool) {
	w := c.tx.World()
	if pos.OutOfBounds(w.ra) {
		return w.conf.Blocks.Air(), false
	}
	col := c.column(pos)
	if col == nil {
		return w.conf.Blocks.Air(), false
	}
	rid := col.Block(uint8(pos[0]), int16(pos[1]), uint8(pos[2]), 0)
	if w.conf.Blocks.NBTBlock(rid) {
		if b, ok := col.BlockEntities[pos]; ok {
			return b, true
		}
	}
	return w.conf.Blocks.BlockByRuntimeIDOrAir(rid), true
}

// relevant is redstoneEngine.relevantLoaded.
func (c *redstoneCompile) relevant(pos cube.Pos) bool {
	w := c.tx.World()
	if pos.OutOfBounds(w.ra) {
		return false
	}
	col := c.column(pos)
	if col == nil {
		return false
	}
	return c.e.relevantRuntimeID(w, col.Block(uint8(pos[0]), int16(pos[1]), uint8(pos[2]), 0))
}

// takeCompile returns compilation state for tx, reusing the engine's.
func (e *redstoneEngine) takeCompile(tx *Tx) *redstoneCompile {
	c := e.compileState
	e.compileState = nil
	if c == nil {
		c = &redstoneCompile{relayerNeighbours: make(map[cube.Pos][]cube.Pos)}
	}
	c.tx, c.e = tx, e
	return c
}

// putCompile clears compilation state for reuse. State that grew large is dropped: clearing a map costs its peak size.
func (e *redstoneEngine) putCompile(c *redstoneCompile) {
	if c.seen.len() > redstoneCompileReuseLimit || c.adjacent.len() > redstoneCompileReuseLimit {
		return
	}
	c.seen.clear()
	c.adjacent.clear()
	clear(c.relayerNeighbours)
	c.tx, c.nodes, c.queue, c.connected, c.col = nil, nil, c.queue[:0], c.connected[:0], nil
	e.compileState = c
}

// redstoneCompileReuseLimit is the most positions a compilation may have looked at for its state to be reused.
const redstoneCompileReuseLimit = 1 << 14

// compile builds a deterministic graph around the candidate positions.
func (e *redstoneEngine) compile(tx *Tx, candidates []cube.Pos) redstoneGraph {
	c := e.takeCompile(tx)
	defer e.putCompile(c)
	c.nodes = make([]redstoneNode, 0, len(candidates))
	for _, pos := range candidates {
		nodeCount := len(c.nodes)
		c.compileRegion(pos)
		if len(c.nodes) == nodeCount {
			c.compileAdjacentRedstone(pos)
		}
	}
	for i := 0; i < len(c.nodes); i++ {
		c.compileAdjacentRedstone(c.nodes[i].pos)
	}
	nodes := c.nodes
	slices.SortFunc(nodes, func(a, b redstoneNode) int {
		return compareBlockPos(a.pos, b.pos)
	})
	index := make(map[cube.Pos]int, len(nodes))
	for i, node := range nodes {
		index[node.pos] = i
	}
	return redstoneGraph{nodes: nodes, edges: c.compileEdges(nodes, index), index: index}
}

// compileAdjacentRedstone adds redstone blocks that can interact with pos directly or through an adjacent conductor.
// Looking around a position a second time finds nothing new, so it is done once.
func (c *redstoneCompile) compileAdjacentRedstone(pos cube.Pos) {
	if !c.adjacent.add(pos) {
		return
	}
	for _, p := range c.adjacentRelevant(pos) {
		if !c.seen.has(p) {
			c.compileRegion(p)
		}
	}
}

// adjacentRelevant returns the loaded redstone-relevant blocks next to pos, next to a conductor next to pos, and,
// if pos conducts, next to pos: the blocks compileAdjacentRedstone walks from. They are kept until a block near pos
// changes. The slice returned must not be changed.
func (c *redstoneCompile) adjacentRelevant(pos cube.Pos) []cube.Pos {
	w := c.tx.World()
	if list, ok := c.e.lists.get(&w.changes, redstoneListAdjacent, pos); ok {
		return list
	}
	seq := w.changes.seq
	r := c.tx.Range()
	c.adjacentBuf = c.adjacentBuf[:0]
	neighbours, n := redstoneNeighbours(pos, r)
	if c.mayConduct(pos) {
		for _, neighbour := range neighbours[:n] {
			c.addRelevant(neighbour)
		}
	}
	for _, neighbour := range neighbours[:n] {
		c.addRelevant(neighbour)
		if c.mayConduct(neighbour) {
			next, m := redstoneNeighbours(neighbour, r)
			for _, p := range next[:m] {
				c.addRelevant(p)
			}
		}
	}
	stored := slices.Clone(c.adjacentBuf)
	c.e.lists.set(redstoneListAdjacent, pos, seq, stored)
	return stored
}

// addRelevant adds pos to adjacentBuf if it holds a loaded redstone-relevant block.
func (c *redstoneCompile) addRelevant(pos cube.Pos) {
	if c.relevant(pos) && !slices.Contains(c.adjacentBuf, pos) {
		c.adjacentBuf = append(c.adjacentBuf, pos)
	}
}

// redstoneNeighbours returns the neighbours of pos within r, the ones cube.Pos.Neighbours visits.
func redstoneNeighbours(pos cube.Pos, r cube.Range) (n [6]cube.Pos, count int) {
	if pos.OutOfBounds(r) {
		return n, 0
	}
	n[0], n[1] = cube.Pos{pos[0] + 1, pos[1], pos[2]}, cube.Pos{pos[0] - 1, pos[1], pos[2]}
	count = 2
	if pos[1]+1 <= r[1] {
		n[count] = cube.Pos{pos[0], pos[1] + 1, pos[2]}
		count++
	}
	if pos[1]-1 >= r[0] {
		n[count] = cube.Pos{pos[0], pos[1] - 1, pos[2]}
		count++
	}
	n[count], n[count+1] = cube.Pos{pos[0], pos[1], pos[2] + 1}, cube.Pos{pos[0], pos[1], pos[2] - 1}
	return n, count + 2
}

// mayConduct reports whether the loaded block at pos conducts power through any face, false if it is not loaded.
// A conducting block makes compilation include the receivers next to it.
func (c *redstoneCompile) mayConduct(pos cube.Pos) bool {
	w := c.tx.World()
	if v, ok := w.memo.get(&w.changes, redstoneMemoConductor, pos); ok {
		return v != 0
	}
	b, ok := c.block(pos)
	return ok && w.conductorFaces(c.tx, pos, b) != 0
}

// relevantLoaded reports whether pos holds a loaded redstone-relevant block. Relevance depends only on the block's
// type (relevantRuntimeID).
func (e *redstoneEngine) relevantLoaded(w *World, pos cube.Pos) bool {
	if pos.OutOfBounds(w.ra) {
		return false
	}
	col, ok := w.chunks[chunkPosFromBlockPos(pos)]
	if !ok {
		return false
	}
	return e.relevantRuntimeID(w, col.Block(uint8(pos[0]), int16(pos[1]), uint8(pos[2]), 0))
}

// redstoneAllFaces is the conductorFaces of a block that conducts through every face.
const redstoneAllFaces = 1<<6 - 1

// compileRegion walks a connected redstone-relevant region into nodes.
func (c *redstoneCompile) compileRegion(pos cube.Pos) {
	r := c.tx.Range()
	if c.seen.has(pos) || pos.OutOfBounds(r) {
		return
	}
	queue := append(c.queue[:0], pos)
	for head := 0; head < len(queue); head++ {
		p := queue[head]
		if p.OutOfBounds(r) || !c.seen.add(p) {
			continue
		}

		b, ok := c.block(p)
		if !ok {
			continue
		}
		source, consumer, action, relayer := classifyRedstoneBlock(b)
		if !source && !consumer && !action && !relayer {
			continue
		}
		c.nodes = append(c.nodes, redstoneNode{
			pos:    p,
			source: source,
			sink:   consumer || action,
		})
		if !relayer {
			continue
		}
		for _, neighbour := range c.relayerConnectedPositions(p, b) {
			if c.relevant(neighbour) {
				queue = append(queue, neighbour)
			}
		}
	}
	c.queue = queue[:0]
}

// relayerNeighbourPositions is redstoneRelayerNeighbourPositions for the loaded block b at pos. The slice returned
// must not be changed.
func (c *redstoneCompile) relayerNeighbourPositions(pos cube.Pos, b Block) []cube.Pos {
	if n, ok := c.relayerNeighbours[pos]; ok {
		return n
	}
	w := c.tx.World()
	local := redstoneLocalRelayer(b)
	if local {
		if n, ok := c.e.lists.get(&w.changes, redstoneListRelayerNeighbours, pos); ok {
			c.relayerNeighbours[pos] = n
			return n
		}
	}
	seq := w.changes.seq
	n := c.e.redstoneRelayerNeighbourPositions(c.tx, pos, b)
	c.relayerNeighbours[pos] = n
	if local {
		c.e.lists.set(redstoneListRelayerNeighbours, pos, seq, n)
	}
	return n
}

// redstoneLocalRelayer reports whether the relayer neighbours of b depend only on the blocks near it, so that they
// may be kept until a block near it changes.
func redstoneLocalRelayer(b Block) bool {
	if _, ok := b.(RedstonePowerRelayerNeighbourer); !ok {
		return true
	}
	_, ok := b.(RedstoneLocalRelayerNeighbourer)
	return ok
}

// relayerConnectedPositions returns relayers connected to pos in either direction. Graph membership must be weakly
// connected so a one-way relayer, such as dust climbing glowstone, still compiles with its lower input path. The
// slice returned is reused by the next call.
func (c *redstoneCompile) relayerConnectedPositions(pos cube.Pos, b Block) []cube.Pos {
	w := c.tx.World()
	local := redstoneLocalRelayer(b)
	if local {
		if list, ok := c.e.lists.get(&w.changes, redstoneListRelayerConnected, pos); ok {
			return list
		}
	}
	seq := w.changes.seq
	connected := append(c.connected[:0], c.relayerNeighbourPositions(pos, b)...)
	r := c.tx.Range()
	for x := pos[0] - 1; x <= pos[0]+1; x++ {
		for y := pos[1] - 1; y <= pos[1]+1; y++ {
			for z := pos[2] - 1; z <= pos[2]+1; z++ {
				candidate := cube.Pos{x, y, z}
				if candidate == pos || candidate.OutOfBounds(r) || slices.Contains(connected, candidate) {
					continue
				}
				candidateBlock, ok := c.block(candidate)
				if !ok {
					continue
				}
				if _, ok := candidateBlock.(RedstonePowerRelayer); !ok {
					continue
				}
				local = local && redstoneLocalRelayer(candidateBlock)
				if slices.Contains(c.relayerNeighbourPositions(candidate, candidateBlock), pos) {
					connected = append(connected, candidate)
				}
			}
		}
	}
	c.connected = connected
	if local {
		stored := slices.Clone(connected)
		c.e.lists.set(redstoneListRelayerConnected, pos, seq, stored)
		return stored
	}
	return connected
}

// compileEdges builds deterministic weighted edges between relayer-connected nodes.
func (c *redstoneCompile) compileEdges(nodes []redstoneNode, index map[cube.Pos]int) []redstoneEdge {
	w := c.tx.World()
	edges := make([]redstoneEdge, 0, len(nodes))
	for i, node := range nodes {
		b, loaded := w.blockLoaded(node.pos)
		if !loaded {
			continue
		}
		relayer, ok := b.(RedstonePowerRelayer)
		if !ok {
			continue
		}
		for _, neighbour := range c.relayerNeighbourPositions(node.pos, b) {
			j, ok := index[neighbour]
			if !ok {
				continue
			}
			weight := 0
			if neighbourBlock, ok := w.blockLoaded(neighbour); ok {
				if _, neighbourRelayer := neighbourBlock.(RedstonePowerRelayer); neighbourRelayer {
					weight = max(relayer.RedstoneSignalLoss(node.pos, c.tx), 1)
				}
			}
			edges = append(edges, redstoneEdge{from: i, to: j, weight: weight})
		}
	}
	slices.SortFunc(edges, compareRedstoneEdge)
	return edges
}

// update applies a computed input power to a consumer or action block.
func (e *redstoneEngine) update(tx *Tx, pos cube.Pos, ref redstoneDirtyRef, newPower int) {
	b := tx.Block(pos)
	oldPower, newPower := e.power[pos], ClampRedstonePower(newPower)

	after, blockChanged := b, false
	if consumer, ok := b.(RedstonePowerConsumer); ok {
		after, blockChanged = consumer.RedstonePowerUpdate(pos, tx, newPower)
	}
	action, hasAction := b.(RedstonePowerAction)
	contextAction, hasContextAction := b.(RedstonePowerContextAction)

	shouldRunAction := hasContextAction || (hasAction && oldPower != newPower)
	if oldPower == newPower && !blockChanged && !shouldRunAction {
		storeRedstonePower(e.power, pos, newPower)
		return
	}
	d := ref.get()
	update := d.redstoneUpdate(pos, b, oldPower, newPower, e.currentTick)
	if blockChanged {
		update.After = after
	}
	if !e.redstoneUpdateAllowed(tx, update) {
		return
	}

	if !blockChanged && !shouldRunAction {
		storeRedstonePower(e.power, pos, newPower)
		return
	}

	if blockChanged {
		tx.SetBlock(pos, after, &SetOpts{DisableRedstoneUpdates: true})
		if _, far := after.(interface{ NotifiesNeighboursOfNeighbours() }); javaRedstone && far && oldPower != newPower {
			tx.World().updateNeighboursOfNeighbours(pos)
		}
		e.invalidateAroundWith(pos, d.propagatedFrom(pos), tx.Range())
		if postUpdater, ok := b.(RedstonePowerPostUpdater); ok {
			postUpdater.RedstonePowerPostUpdate(pos, tx, b, after, oldPower, newPower)
		}
	}
	if hasContextAction {
		contextAction.RedstonePowerActionUpdate(pos, tx, update)
	} else if shouldRunAction {
		action.RedstonePowerAction(pos, tx, oldPower, newPower)
	}
	if blockChanged || shouldRunAction {
		storeRedstonePower(e.power, pos, newPower)
	}
}

// updateGraphSources updates non-relayer source outputs before graph propagation.
func (e *redstoneEngine) updateGraphSources(tx *Tx, graph redstoneGraph, dirty map[cube.Pos]redstoneDirty, candidates []cube.Pos) (map[cube.Pos]int, map[cube.Pos]struct{}) {
	var cancelled map[cube.Pos]int
	var checked map[cube.Pos]struct{}
	for _, node := range graph.nodes {
		if !node.source {
			continue
		}
		b, ok := tx.World().blockLoaded(node.pos)
		if !ok {
			continue
		}
		if _, ok := b.(RedstonePowerRelayer); ok {
			continue
		}
		if checked == nil {
			checked = make(map[cube.Pos]struct{})
		}
		checked[node.pos] = struct{}{}

		if !e.updateSource(tx, node.pos, redstoneDirtyRef{dirty, candidates, node.pos}) {
			if cancelled == nil {
				cancelled = make(map[cube.Pos]int)
			}
			cancelled[node.pos] = e.output[node.pos]
		}
	}
	return cancelled, checked
}

// updateSource updates cached output power for a source and reports whether it was allowed.
func (e *redstoneEngine) updateSource(tx *Tx, pos cube.Pos, ref redstoneDirtyRef) bool {
	b := tx.Block(pos)
	oldPower, newPower := e.output[pos], e.sourcePower(pos, tx)
	if oldPower == newPower {
		return true
	}
	d := ref.get()
	update := d.redstoneUpdate(pos, b, oldPower, newPower, e.currentTick)
	if !e.redstoneUpdateAllowed(tx, update) {
		return false
	}
	storeRedstonePower(e.output, pos, newPower)
	e.invalidateAroundWith(pos, d.propagatedFrom(pos), tx.Range())
	return true
}

// storeRedstonePower stores non-zero redstone power and removes zero entries from cache maps.
func storeRedstonePower(cache map[cube.Pos]int, pos cube.Pos, power int) {
	if power == 0 {
		delete(cache, pos)
		return
	}
	cache[pos] = power
}

// directPower returns the strongest direct power reaching pos from any side.
func (e *redstoneEngine) directPower(pos cube.Pos, tx *Tx) int {
	power := 0
	for _, face := range cube.Faces() {
		power = max(power, e.directPowerFrom(pos, tx, face))
	}
	return power
}

// directPowerFrom returns direct power reaching pos through face.
func (e *redstoneEngine) directPowerFrom(pos cube.Pos, tx *Tx, face cube.Face) int {
	neighbour := pos.Side(face)
	if neighbour.OutOfBounds(tx.Range()) {
		return 0
	}
	b, ok := tx.World().blockLoaded(neighbour)
	if !ok {
		return 0
	}
	if source, ok := b.(RedstonePowerSource); ok {
		return ClampRedstonePower(e.redstonePower(source, neighbour, tx, face.Opposite()))
	}
	return 0
}

// strongPower returns the strongest strong power reaching pos from any side.
func (e *redstoneEngine) strongPower(pos cube.Pos, tx *Tx) int {
	power := 0
	for _, face := range cube.Faces() {
		power = max(power, e.strongPowerFrom(pos, tx, face))
	}
	return power
}

// strongPowerFrom returns strong power reaching pos through face.
func (e *redstoneEngine) strongPowerFrom(pos cube.Pos, tx *Tx, face cube.Face) int {
	neighbour := pos.Side(face)
	if neighbour.OutOfBounds(tx.Range()) {
		return 0
	}
	b, ok := tx.World().blockLoaded(neighbour)
	if !ok {
		return 0
	}
	if source, ok := b.(RedstoneStrongPowerSource); ok {
		if power, ok := e.suppressedSources[neighbour]; ok {
			return ClampRedstonePower(power)
		}
		return ClampRedstonePower(source.RedstoneStrongPower(neighbour, tx, face.Opposite()))
	}
	return 0
}

// conductedStrongPower returns strong power conducted through adjacent conductive blocks.
func (e *redstoneEngine) conductedStrongPower(pos cube.Pos, tx *Tx) int {
	power := 0
	for _, face := range cube.Faces() {
		power = max(power, e.conductedStrongPowerFrom(pos, tx, face))
	}
	return power
}

// conductedStrongPowerFrom returns strong power conducted through the block on face.
func (e *redstoneEngine) conductedStrongPowerFrom(pos cube.Pos, tx *Tx, face cube.Face) int {
	conductorPos := pos.Side(face)
	if conductorPos.OutOfBounds(tx.Range()) {
		return 0
	}
	if !e.conducts(tx, conductorPos, face.Opposite()) {
		return 0
	}
	held, key, hold := e.heldConductorPower(conductorPos)
	if hold && held.strong >= 0 {
		return int(held.strong)
	}
	power := 0
	for _, sourceFace := range cube.Faces() {
		power = max(power, e.strongPowerFrom(conductorPos, tx, sourceFace))
	}
	if hold {
		held = e.heldEntry(key)
		held.strong = int8(power)
		e.held[key] = held
	}
	return power
}

// redstoneHeldPower is the strong and weak power held by a conductor, -1 if not worked out yet.
type redstoneHeldPower struct {
	strong, weak int8
}

// heldConductorPower returns the power held by the conductor at pos as worked out earlier in the current graph power
// evaluation, if it is being run and is not evaluating a source (whose output may read the power around it with
// itself left out). The world does not change while the graph power is evaluated.
func (e *redstoneEngine) heldConductorPower(pos cube.Pos) (redstoneHeldPower, uint64, bool) {
	if !e.holding || len(e.evaluating) != 0 {
		return redstoneHeldPower{}, 0, false
	}
	key, ok := packMemoPos(pos)
	if !ok {
		return redstoneHeldPower{}, 0, false
	}
	return e.heldEntry(key), key, true
}

// heldEntry returns the held power stored for key.
func (e *redstoneEngine) heldEntry(key uint64) redstoneHeldPower {
	if held, ok := e.held[key]; ok {
		return held
	}
	return redstoneHeldPower{strong: -1, weak: -1}
}

// conducts reports whether the loaded block at pos conducts strong power through face.
func (e *redstoneEngine) conducts(tx *Tx, pos cube.Pos, face cube.Face) bool {
	w := tx.World()
	if v, ok := w.memo.get(&w.changes, redstoneMemoConductor, pos); ok {
		return v&(1<<face) != 0
	}
	b, ok := w.blockLoaded(pos)
	return ok && w.conductorFaces(tx, pos, b)&(1<<face) != 0
}

// weakBlockPower returns weak power directly applied to a conductive block by sources that weak-power blocks.
func (e *redstoneEngine) weakBlockPower(pos cube.Pos, tx *Tx) int {
	power := 0
	for _, face := range cube.Faces() {
		power = max(power, e.weakBlockPowerFrom(pos, tx, face))
	}
	return power
}

// weakBlockPowerFrom returns weak power applied to pos from face by a source that weak-powers conductive blocks.
func (e *redstoneEngine) weakBlockPowerFrom(pos cube.Pos, tx *Tx, face cube.Face) int {
	sourcePos := pos.Side(face)
	if sourcePos.OutOfBounds(tx.Range()) {
		return 0
	}
	b, ok := tx.World().blockLoaded(sourcePos)
	if !ok {
		return 0
	}
	if source, ok := b.(RedstonePowerSource); ok && e.redstoneWeaklyPowersBlocks(b) {
		return ClampRedstonePower(e.redstonePower(source, sourcePos, tx, face.Opposite()))
	}
	return 0
}

// conductedWeakPower returns weak power conducted through adjacent conductive blocks for mechanism activation.
func (e *redstoneEngine) conductedWeakPower(pos cube.Pos, tx *Tx) int {
	power := 0
	for _, face := range cube.Faces() {
		power = max(power, e.conductedWeakPowerFrom(pos, tx, face))
	}
	return power
}

// conductedWeakPowerFrom returns weak power conducted through the block on face for mechanism activation. It excludes
// strong power and only counts sources that explicitly weak-power conductive blocks, such as redstone dust.
func (e *redstoneEngine) conductedWeakPowerFrom(pos cube.Pos, tx *Tx, face cube.Face) int {
	conductorPos := pos.Side(face)
	if conductorPos.OutOfBounds(tx.Range()) {
		return 0
	}
	if !e.conducts(tx, conductorPos, face.Opposite()) {
		return 0
	}
	held, key, hold := e.heldConductorPower(conductorPos)
	if hold && held.weak >= 0 {
		return int(held.weak)
	}
	power := e.weakBlockPower(conductorPos, tx)
	if hold {
		held = e.heldEntry(key)
		held.weak = int8(power)
		e.held[key] = held
	}
	return power
}

// conductedActivationPower returns power that can activate a non-relayer mechanism behind adjacent conductive blocks.
func (e *redstoneEngine) conductedActivationPower(pos cube.Pos, tx *Tx) int {
	return max(e.conductedStrongPower(pos, tx), e.conductedWeakPower(pos, tx))
}

// conductedActivationPowerFrom returns mechanism activation power conducted through the block on face.
func (e *redstoneEngine) conductedActivationPowerFrom(pos cube.Pos, tx *Tx, face cube.Face) int {
	return max(e.conductedStrongPowerFrom(pos, tx, face), e.conductedWeakPowerFrom(pos, tx, face))
}

// conductivePowerTo returns power held by pos as a conductive block, excluding direct component activation.
func (e *redstoneEngine) conductivePowerTo(pos cube.Pos, tx *Tx) int {
	b, ok := tx.World().blockLoaded(pos)
	if !ok || tx.World().conductorFaces(tx, pos, b) != redstoneAllFaces {
		return 0
	}
	return max(e.strongPower(pos, tx), e.weakBlockPower(pos, tx))
}

// acceptsDirectSourcePower reports whether direct source output should activate the block at pos.
func (e *redstoneEngine) acceptsDirectSourcePower(pos cube.Pos, tx *Tx) bool {
	b, ok := tx.World().blockLoaded(pos)
	if !ok {
		return true
	}
	if isRedstoneRelevant(b) {
		return true
	}
	return tx.World().conductorFaces(tx, pos, b) != redstoneAllFaces
}

// acceptsWeakConductedPower reports whether the block at pos may be activated by a weakly powered conductor.
func (e *redstoneEngine) acceptsWeakConductedPower(pos cube.Pos, tx *Tx) bool {
	b, ok := tx.World().blockLoaded(pos)
	if !ok {
		return true
	}
	_, relayer := b.(RedstonePowerRelayer)
	return !relayer
}

// redstoneWeaklyPowersBlocks reports whether b's weak source output can weak-power adjacent conductive blocks.
func (e *redstoneEngine) redstoneWeaklyPowersBlocks(b Block) bool {
	weakBlockPowerer, ok := b.(RedstoneWeakBlockPowerer)
	return ok && weakBlockPowerer.RedstoneWeaklyPowersBlocks()
}

// sourcePower returns the strongest output emitted by a source block.
func (e *redstoneEngine) sourcePower(pos cube.Pos, tx *Tx) int {
	b, ok := tx.World().blockLoaded(pos)
	if !ok {
		return 0
	}
	source, ok := b.(RedstonePowerSource)
	if !ok {
		return 0
	}
	power := 0
	for _, face := range cube.Faces() {
		power = max(power, ClampRedstonePower(e.redstonePower(source, pos, tx, face)))
	}
	return power
}

// graphPower computes propagated graph power for every graph node.
func (e *redstoneEngine) graphPower(tx *Tx, graph redstoneGraph) []int {
	powers := make([]int, len(graph.nodes))
	if len(graph.nodes) == 0 {
		return powers
	}

	// The power conductors hold is worked out once for this evaluation.
	if e.held == nil {
		e.held = make(map[uint64]redstoneHeldPower)
	}
	e.holding = true
	defer func() {
		e.holding = false
		if len(e.held) > redstoneCompileReuseLimit {
			e.held = nil
			return
		}
		clear(e.held)
	}()
	index := graph.index
	n := len(graph.nodes)
	buf := e.graphBuffers
	e.graphBuffers = nil
	if buf == nil {
		buf = &redstoneGraphBuffers{}
	}
	defer func() {
		clear(buf.sources)
		clear(buf.relayers)
		e.graphBuffers = buf
	}()
	sources, relayers := grow(buf.sources, n), grow(buf.relayers, n)
	// graph.edges is sorted by the node it leaves: the edges of node i are graph.edges[start[i]:start[i+1]].
	start := grow(buf.start, n+1)
	buf.sources, buf.relayers, buf.start = sources, relayers, start
	for i, node := range graph.nodes {
		if b, ok := tx.World().blockLoaded(node.pos); ok {
			sources[i], _ = b.(RedstonePowerSource)
			relayers[i], _ = b.(RedstonePowerRelayer)
		}
	}
	clear(start)
	for _, edge := range graph.edges {
		start[edge.from+1]++
	}
	for i := 1; i <= n; i++ {
		start[i] += start[i-1]
	}

	queue := buf.queue[:0]
	defer func() { buf.queue = queue[:0] }()
	push := func(i, power int) {
		power = ClampRedstonePower(power)
		if power <= powers[i] {
			return
		}
		powers[i] = power
		queue = append(queue, i)
	}

	for i, node := range graph.nodes {
		if node.sink && relayers[i] == nil {
			push(i, e.conductedActivationPower(node.pos, tx))
			continue
		}
		push(i, e.conductedStrongPower(node.pos, tx))
	}

	for i, source := range sources {
		// Relayers such as redstone wire store their previous output as RedstonePower.
		// They must be recomputed from real sources, not used as seeds themselves.
		if source == nil || relayers[i] != nil {
			continue
		}
		pos := graph.nodes[i].pos
		for _, face := range cube.Faces() {
			j, ok := index[pos.Side(face)]
			if !ok {
				continue
			}
			power := ClampRedstonePower(e.redstonePower(source, pos, tx, face))
			push(j, power)
		}
	}

	for head := 0; head < len(queue); head++ {
		i := queue[head]
		if relayers[i] == nil {
			continue
		}
		for _, edge := range graph.edges[start[i]:start[i+1]] {
			push(edge.to, powers[i]-edge.weight)
		}
	}
	return powers
}

// powerTo returns the strongest redstone power currently reaching pos.
func (e *redstoneEngine) powerTo(pos cube.Pos, tx *Tx) int {
	power := 0
	for _, face := range cube.Faces() {
		power = max(power, e.powerFrom(pos, tx, face))
	}
	if e.acceptsWeakConductedPower(pos, tx) {
		power = max(power, e.conductedActivationPower(pos, tx))
	} else {
		power = max(power, e.conductedStrongPower(pos, tx))
	}
	return ClampRedstonePower(power)
}

// powerFrom returns redstone power reaching pos through face.
func (e *redstoneEngine) powerFrom(pos cube.Pos, tx *Tx, face cube.Face) int {
	power := e.conductedStrongPowerFrom(pos, tx, face)
	if e.acceptsWeakConductedPower(pos, tx) {
		power = e.conductedActivationPowerFrom(pos, tx, face)
	}
	buf := e.takeStepBuffer()
	defer e.putStepBuffer(buf)
	queue := append(buf.queue, redstoneStep{pos: pos.Side(face), from: face.Opposite(), loss: 0, depth: 0})
	seen := buf.seen
	for head := 0; head < len(queue); head++ {
		s := queue[head]
		if s.pos.OutOfBounds(tx.Range()) || s.loss >= 15 || s.depth >= 15 {
			continue
		}
		if s.pos == pos {
			continue
		}
		if loss, ok := seen[s.pos]; ok && loss <= s.loss {
			continue
		}
		seen[s.pos] = s.loss

		b, ok := tx.World().blockLoaded(s.pos)
		if !ok {
			continue
		}
		relayer, isRelayer := b.(RedstonePowerRelayer)
		// See graphPower: relayers carry recomputed power through edges, so their
		// stored RedstonePower should not count as an independent source here.
		if source, ok := b.(RedstonePowerSource); ok && !isRelayer && e.acceptsDirectSourcePower(pos, tx) {
			power = max(power, ClampRedstonePower(e.redstonePower(source, s.pos, tx, s.from)-s.loss))
		}
		if !isRelayer {
			continue
		}
		for _, next := range e.redstoneRelayerNeighbourPositions(tx, s.pos, b) {
			to := redstoneStepFace(s.pos, next)
			if to == s.from {
				continue
			}
			nextBlock, ok := tx.World().blockLoaded(next)
			if !ok {
				continue
			}
			loss := s.loss
			if _, nextRelayer := nextBlock.(RedstonePowerRelayer); nextRelayer {
				loss += max(relayer.RedstoneSignalLoss(s.pos, tx), 1)
			}
			if loss <= 15 {
				queue = append(queue, redstoneStep{pos: next, from: to.Opposite(), loss: loss, depth: s.depth + 1})
			}
		}
	}
	buf.queue = queue
	return ClampRedstonePower(power)
}

// redstoneStep is a relayer powerFrom walks through.
type redstoneStep struct {
	pos   cube.Pos
	from  cube.Face
	loss  int
	depth int
}

// redstoneStepBuffer is the walk state of a powerFrom call, reused by later calls.
type redstoneStepBuffer struct {
	queue []redstoneStep
	seen  map[cube.Pos]int
}

// takeStepBuffer returns an empty walk state. A source powerFrom asks for its power may call powerFrom again, so
// each call takes its own.
func (e *redstoneEngine) takeStepBuffer() *redstoneStepBuffer {
	if n := len(e.stepBuffers); n > 0 {
		b := e.stepBuffers[n-1]
		e.stepBuffers = e.stepBuffers[:n-1]
		return b
	}
	return &redstoneStepBuffer{seen: make(map[cube.Pos]int, 16)}
}

// putStepBuffer empties b for a later powerFrom call.
func (e *redstoneEngine) putStepBuffer(b *redstoneStepBuffer) {
	if len(b.seen) > 256 || len(e.stepBuffers) >= 8 {
		return
	}
	b.queue = b.queue[:0]
	clear(b.seen)
	e.stepBuffers = append(e.stepBuffers, b)
}

// torchBurnoutStatus returns whether the torch at pos is burned out and whether it can recover at currentTick.
func (e *redstoneEngine) torchBurnoutStatus(pos cube.Pos, currentTick int64) (burnedOut, recoverable bool) {
	data, ok := e.pruneTorchBurnout(pos, currentTick)
	if !ok {
		return false, true
	}
	if !data.burnedOut {
		return false, true
	}
	return true, len(data.offTicks) < redstoneTorchBurnoutThreshold
}

// recordTorchTurnOff records a torch being forced off and reports whether that torch should burn out.
func (e *redstoneEngine) recordTorchTurnOff(pos cube.Pos, currentTick int64) bool {
	if e.torchBurnout == nil {
		e.torchBurnout = make(map[cube.Pos]redstoneTorchBurnout)
	}
	data, _ := e.pruneTorchBurnout(pos, currentTick)
	data.offTicks = append(data.offTicks, currentTick)
	if len(data.offTicks) >= redstoneTorchBurnoutThreshold {
		data.burnedOut = true
	}
	e.torchBurnout[pos] = data
	return data.burnedOut
}

// clearTorchBurnout removes transient burnout state for a redstone torch.
func (e *redstoneEngine) clearTorchBurnout(pos cube.Pos) {
	if e == nil {
		return
	}
	delete(e.torchBurnout, pos)
}

// markTorchSelfTriggered records that the next torch turn-off at pos was caused by that torch's own output loop.
func (e *redstoneEngine) markTorchSelfTriggered(pos cube.Pos) {
	if e == nil {
		return
	}
	if e.torchBurnout == nil {
		e.torchBurnout = make(map[cube.Pos]redstoneTorchBurnout)
	}
	data := e.torchBurnout[pos]
	data.pendingSelfTriggered = true
	e.torchBurnout[pos] = data
}

// consumeTorchSelfTriggered reports and clears whether the next torch turn-off at pos was self-triggered.
func (e *redstoneEngine) consumeTorchSelfTriggered(pos cube.Pos) bool {
	if e == nil || e.torchBurnout == nil {
		return false
	}
	data, ok := e.torchBurnout[pos]
	if !ok {
		return false
	}
	selfTriggered := data.pendingSelfTriggered
	data.pendingSelfTriggered = false
	if len(data.offTicks) == 0 && !data.burnedOut {
		delete(e.torchBurnout, pos)
	} else {
		e.torchBurnout[pos] = data
	}
	return selfTriggered
}

// pruneTorchBurnout removes expired turn-off entries and returns the remaining burnout data.
func (e *redstoneEngine) pruneTorchBurnout(pos cube.Pos, currentTick int64) (redstoneTorchBurnout, bool) {
	if e == nil || e.torchBurnout == nil {
		return redstoneTorchBurnout{}, false
	}
	data, ok := e.torchBurnout[pos]
	if !ok {
		return redstoneTorchBurnout{}, false
	}
	data.offTicks = slices.DeleteFunc(data.offTicks, func(tick int64) bool {
		return currentTick-tick >= redstoneTorchBurnoutWindowTicks
	})
	if len(data.offTicks) == 0 && !data.burnedOut && !data.pendingSelfTriggered {
		delete(e.torchBurnout, pos)
		return redstoneTorchBurnout{}, false
	}
	e.torchBurnout[pos] = data
	return data, true
}

// redstonePower reads source power while guarding against recursive source evaluation.
func (e *redstoneEngine) redstonePower(source RedstonePowerSource, pos cube.Pos, tx *Tx, face cube.Face) int {
	if power, ok := e.suppressedSources[pos]; ok {
		return ClampRedstonePower(power)
	}
	if _, ok := e.evaluating[pos]; ok {
		return 0
	}
	e.evaluating[pos] = struct{}{}
	defer delete(e.evaluating, pos)
	return source.RedstonePower(pos, tx, face)
}

// redstoneUpdateAllowed dispatches redstone callbacks and reports whether the update was cancelled.
func (e *redstoneEngine) redstoneUpdateAllowed(tx *Tx, update RedstoneUpdate) bool {
	h := tx.World().Handler()
	if _, nop := h.(NopHandler); nop {
		// Nothing to dispatch to, and nothing can cancel the update.
		return true
	}
	ctx := tx.Event()
	h.HandleRedstoneUpdate(ctx, update)
	return !ctx.Cancelled()
}

// redstoneGraphBuffers is the scratch state of graphPower, reused by later calls.
type redstoneGraphBuffers struct {
	sources  []RedstonePowerSource
	relayers []RedstonePowerRelayer
	start    []int
	queue    []int
}

// grow returns s with length n, reusing its array if it is big enough.
func grow[S ~[]E, E any](s S, n int) S {
	if cap(s) < n {
		return make(S, n)
	}
	return s[:n]
}

// redstoneLightDiffuser is the local subset needed to exclude transparent conductors.
type redstoneLightDiffuser interface {
	LightDiffusionLevel() uint8
}

// redstoneStrongPowerConductor reports whether b can conduct strong power through face.
func redstoneStrongPowerConductor(pos cube.Pos, b Block, src BlockSource, face cube.Face) bool {
	name, _ := b.EncodeBlock()
	return redstoneNamedStrongPowerConductor(pos, b, name, src, face)
}

// redstoneNamedStrongPowerConductor is redstoneStrongPowerConductor for a block whose encoded name is known.
func redstoneNamedStrongPowerConductor(pos cube.Pos, b Block, name string, src BlockSource, face cube.Face) bool {
	// Java decides by BlockState.isRedstoneConductor: a full collision shape, unless the block says otherwise
	// (glass, leaves, ice and a few more never conduct; mud and soul sand always do). Slime does.
	if javaRedstone {
		switch javaConductorOverride[name] {
		case -1:
			return false
		case 1:
			return true
		}
	}
	if !b.Model().FaceSolid(pos, face, src) {
		return false
	}
	if _, ok := b.(RedstoneNonConductive); ok {
		return false
	}
	if javaRedstone && javaConductorKnown[name] {
		return true
	}
	if diffuser, ok := b.(redstoneLightDiffuser); ok && diffuser.LightDiffusionLevel() == 0 {
		return false
	}
	return true
}

// RedstoneFullPowerConductor reports whether b is a full solid redstone conductor according to the default redstone
// conductivity rules. Blocks whose model depends on the blocks around them
// read those from src.
func RedstoneFullPowerConductor(pos cube.Pos, b Block, src BlockSource) bool {
	name, _ := b.EncodeBlock()
	for _, face := range cube.Faces() {
		if !redstoneNamedStrongPowerConductor(pos, b, name, src, face) {
			return false
		}
	}
	return true
}

// redstoneRelayerNeighbourPositions returns sorted relayer neighbours for b at pos.
func (e *redstoneEngine) redstoneRelayerNeighbourPositions(tx *Tx, pos cube.Pos, b Block) []cube.Pos {
	if neighbourer, ok := b.(RedstonePowerRelayerNeighbourer); ok {
		neighbours := slices.Clone(neighbourer.RedstoneRelayerNeighbours(pos, tx))
		slices.SortFunc(neighbours, compareBlockPos)
		return neighbours
	}
	neighbours := make([]cube.Pos, 0, len(cube.Faces()))
	e.redstoneRelayerNeighbours(tx, pos, func(neighbour cube.Pos) {
		neighbours = append(neighbours, neighbour)
	})
	slices.SortFunc(neighbours, compareBlockPos)
	return neighbours
}

// redstoneRelayerNeighbours visits the six default adjacent relayer neighbours.
func (e *redstoneEngine) redstoneRelayerNeighbours(tx *Tx, pos cube.Pos, f func(cube.Pos)) {
	for _, face := range cube.Faces() {
		neighbour := pos.Side(face)
		if !neighbour.OutOfBounds(tx.Range()) {
			f(neighbour)
		}
	}
}

// redstoneStepFace returns the dominant face direction from one relayer position to another.
func redstoneStepFace(from, to cube.Pos) cube.Face {
	dx, dy, dz := to[0]-from[0], to[1]-from[1], to[2]-from[2]
	switch {
	case dy > 0:
		return cube.FaceUp
	case dy < 0:
		return cube.FaceDown
	case dx > 0:
		return cube.FaceEast
	case dx < 0:
		return cube.FaceWest
	case dz > 0:
		return cube.FaceSouth
	case dz < 0:
		return cube.FaceNorth
	default:
		return cube.FaceUp
	}
}

// classifyRedstoneBlock reports the redstone capabilities implemented by b.
func classifyRedstoneBlock(b Block) (source, consumer, action, relayer bool) {
	if JavaRedstoneInactive(b) {
		// Its redstone methods do nothing without Java redstone.
		return
	}
	if javaRedstone {
		if _, ok := b.(CoreRedstoneOnly); ok {
			// Not a redstone component in Java Edition (TNT).
			return
		}
	}
	_, source = b.(RedstonePowerSource)
	_, consumer = b.(RedstonePowerConsumer)
	_, action = b.(RedstonePowerAction)
	if !action {
		_, action = b.(RedstonePowerContextAction)
	}
	_, relayer = b.(RedstonePowerRelayer)
	return
}

// isRedstoneRelevant reports whether b should be included in redstone graph compilation.
func isRedstoneRelevant(b Block) bool {
	source, consumer, action, relayer := classifyRedstoneBlock(b)
	return source || consumer || action || relayer
}

// compareBlockPos orders positions deterministically by Y, Z, then X.
func compareBlockPos(a, b cube.Pos) int {
	if a[1] != b[1] {
		return a[1] - b[1]
	}
	if a[2] != b[2] {
		return a[2] - b[2]
	}
	return a[0] - b[0]
}

// compareRedstoneEdge orders edges deterministically by endpoints and weight.
func compareRedstoneEdge(a, b redstoneEdge) int {
	if a.from != b.from {
		return a.from - b.from
	}
	if a.to != b.to {
		return a.to - b.to
	}
	return a.weight - b.weight
}

// ClampRedstonePower clamps power to the vanilla 0-15 redstone range.
func ClampRedstonePower(power int) int {
	if power < 0 {
		return 0
	}
	if power > 15 {
		return 15
	}
	return power
}
