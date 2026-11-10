package world

import (
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// Stand-in for the engine's per-position component state (requested from the core agent in
// /root/javaproto/dfredstone_owners.md). It keeps the same API; the core version keeps the values in the redstone
// engine and drops them when the engine forgets a position or a chunk is unloaded. Delete this file once that is on
// release-1. Values are only read and written by transactions of their own world, one at a time.
var componentStates sync.Map // *World -> map[cube.Pos]uint64

// componentStateMap returns the state map of the transaction's world.
func (r RedstoneTransaction) componentStateMap() map[cube.Pos]uint64 {
	w := r.tx.World()
	if m, ok := componentStates.Load(w); ok {
		return m.(map[cube.Pos]uint64)
	}
	m := make(map[cube.Pos]uint64)
	componentStates.Store(w, m)
	return m
}

// State returns the transient state a redstone component keeps at pos (SetState), and whether there is any.
func (r RedstoneTransaction) State(pos cube.Pos) (v uint64, ok bool) {
	v, ok = r.componentStateMap()[pos]
	return v, ok
}

// SetState stores transient state for the redstone component at pos: what it remembers between ticks that its block
// state does not hold, such as the inputs a repeater has yet to pass on.
func (r RedstoneTransaction) SetState(pos cube.Pos, v uint64) {
	m := r.componentStateMap()
	if _, ok := m[pos]; !ok && len(m) >= componentStateLimit {
		// Nothing removes the states of components that are gone: start again. Components rebuild their state
		// from their block.
		clear(m)
	}
	m[pos] = v
}

// componentStateLimit bounds the states kept per world by the stand-in.
const componentStateLimit = 1 << 16

// ClearState removes the transient state of the redstone component at pos.
func (r RedstoneTransaction) ClearState(pos cube.Pos) {
	delete(r.componentStateMap(), pos)
}
