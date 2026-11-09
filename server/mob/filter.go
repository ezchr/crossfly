//go:build crossfly_full

package mob

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// FilterContext is what a filter is evaluated against (BDS
// VariantParameterList): the subjects a test's "subject" field picks from,
// plus the block/item/damage some tests read. Self is always set by the
// caller; the rest are nil/zero when absent, and a test whose subject is
// absent evaluates false.
type FilterContext struct {
	Tx   *world.Tx
	Self *Mob
	// Other is the "other" subject: the entity being tested against (the
	// candidate target of nearest_attackable_target, the interacting player,
	// the attacker in damage_sensor).
	Other world.Entity
	// Target, Player, Parent, Baby, Damager are the other named subjects.
	Target, Player, Parent, Baby, Damager world.Entity
	// Block is the "block" subject (has_block / is_block tests).
	Block    world.Block
	BlockPos cube.Pos
	HasBlock bool
	// Item is the item in use (interact, has_equipment on hand).
	Item item.Stack
	// Damage is the damage source for has_damage / damage_sensor.
	Damage world.DamageSource
}

// Filter is a compiled filter tree (all_of/any_of/none_of over tests).
type Filter interface {
	Test(ctx *FilterContext) bool
}

// FilterCompiler compiles filter JSON (an object, an array or a legacy
// form). Package mob/filter (A2) installs it; it owns the test registry.
type FilterCompiler func(raw json.RawMessage) (Filter, error)

var filterCompiler FilterCompiler

// SetFilterCompiler installs the filter compiler. Only package mob/filter
// calls it. It panics when called twice.
func SetFilterCompiler(c FilterCompiler) {
	if filterCompiler != nil {
		panic("mob: filter compiler already set")
	}
	filterCompiler = c
}

// FilterFunc adapts a function to Filter.
type FilterFunc func(ctx *FilterContext) bool

// Test ...
func (f FilterFunc) Test(ctx *FilterContext) bool { return f(ctx) }

// AlwaysTrue is the filter used for absent/empty filter JSON.
var AlwaysTrue Filter = FilterFunc(func(*FilterContext) bool { return true })

// noCompiler is used when filters are compiled before package mob/filter is
// linked in: like an unknown test in BDS, it evaluates false.
var noCompiler Filter = FilterFunc(func(*FilterContext) bool { return false })

// CompileFilter compiles filter JSON. Absent, null and empty ({} or [])
// filters always pass. Compile once, when a component or goal is built, not
// per tick.
func CompileFilter(raw json.RawMessage) (Filter, error) {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || bytes.Equal(t, []byte("null")) || bytes.Equal(t, []byte("{}")) || bytes.Equal(t, []byte("[]")) {
		return AlwaysTrue, nil
	}
	if filterCompiler == nil {
		return noCompiler, nil
	}
	f, err := filterCompiler(raw)
	if err != nil {
		return noCompiler, fmt.Errorf("mob: filter: %w", err)
	}
	return f, nil
}

// Ctx returns a FilterContext with Self = m and Tx set, for the caller to
// fill in further.
func (m *Mob) Ctx() *FilterContext { return &FilterContext{Tx: m.Tx(), Self: m} }
