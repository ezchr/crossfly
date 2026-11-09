//go:build crossfly_full

package filter

import (
	"encoding/json"
	"sync"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

func init() {
	mob.SetFilterCompiler(Compile)
}

// Compile is the mob.FilterCompiler: it parses raw and returns a filter
// that evaluates against a mob.FilterContext.
func Compile(raw json.RawMessage) (mob.Filter, error) {
	f, err := Parse(raw)
	if err != nil {
		return nil, err
	}
	return mobFilter{f: f}, nil
}

type mobFilter struct{ f *Filter }

// Test ...
func (m mobFilter) Test(ctx *mob.FilterContext) bool {
	if m.f == nil {
		return true
	}
	if ctx == nil {
		return m.f.Eval(FromMob(ctx))
	}
	// Target searches test every entity they pass: the context comes from a
	// pool, with its actors stored in it, so a test allocates nothing.
	tc := testContexts.Get().(*testContext)
	tc.fill(ctx)
	ok := m.f.Eval(&tc.Context)
	*tc = testContext{}
	testContexts.Put(tc)
	return ok
}

// testContext is a Context with room for its actors.
type testContext struct {
	Context
	actors [7]EntityActor
	n      int
}

var testContexts = sync.Pool{New: func() any { return new(testContext) }}

// fill is FromMob into tc.
func (tc *testContext) fill(c *mob.FilterContext) {
	tx := c.Tx
	if tx == nil && c.Self != nil {
		tx = c.Self.Tx()
	}
	ctx := &tc.Context
	ctx.Other = tc.wrap(tx, c.Other)
	ctx.Target = tc.wrap(tx, c.Target)
	ctx.Player = tc.wrap(tx, c.Player)
	ctx.Parent = tc.wrap(tx, c.Parent)
	ctx.Baby = tc.wrap(tx, c.Baby)
	ctx.Damager = tc.wrap(tx, c.Damager)
	if c.Self != nil {
		ctx.Self = tc.wrap(tx, c.Self)
		ctx.Rand = c.Self.Rand()
	}
	if tx != nil {
		ctx.World = TxWorld{Tx: tx}
	}
	if c.HasBlock && c.Block != nil {
		ctx.Block = TxBlock{B: c.Block, Pos: c.BlockPos, Tx: tx}
	}
	if c.Damage != nil {
		ctx.Damage = &Damage{Cause: DamageCause(c.Damage), Fatal: c.DamageFatal}
	}
}

// wrap is Wrap with the actor stored in tc.
func (tc *testContext) wrap(tx *world.Tx, e world.Entity) Actor {
	if e == nil {
		return nil
	}
	if m, ok := e.(*mob.Mob); ok && m == nil {
		return nil
	}
	if p, ok := e.(*player.Player); ok && p == nil {
		return nil
	}
	a := &tc.actors[tc.n]
	tc.n++
	*a = EntityActor{Tx: tx, E: e}
	return a
}

// Filter returns the parsed filter tree.
func (m mobFilter) Filter() *Filter { return m.f }
