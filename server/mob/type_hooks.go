//go:build crossfly_full

package mob

import "github.com/go-gl/mathgl/mgl64"

// TypeHooks replace steps of the engine for every mob of one type. Package
// mob/javamob (Java mob mode, mobs=java) registers its types with them to run
// Java Edition's entity tick instead of the stages. A type without hooks
// pays one nil check per tick.
type TypeHooks struct {
	// Tick, if set, runs instead of the stages (tick.go) every simulated
	// tick, for living and dying mobs: it is responsible for the movement
	// and for showing it (SendMovement). Effects, the hurt immunity and the
	// death timer are still counted by the engine before it runs.
	Tick func(m *Mob)
	// KnockBack, if set, replaces Mob.KnockBack.
	KnockBack func(m *Mob, src mgl64.Vec3, force, height float64)
}

// RegisterTypeWithHooks is RegisterType for a type whose mobs run with h.
func RegisterTypeWithHooks(def Definition, h TypeHooks) *Type {
	t := RegisterType(def)
	t.hooks = h
	return t
}

// Hooks returns the type's hooks (zero for a definition-driven type).
func (t *Type) Hooks() TypeHooks { return t.hooks }
