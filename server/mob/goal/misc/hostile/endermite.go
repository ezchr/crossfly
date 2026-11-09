//go:build crossfly_full

package hostile

import "github.com/df-mc/dragonfly/server/mob"

// endermiteLifetime is how long an endermite lives (Endermite::aiStep).
const endermiteLifetime = 2400

func init() {
	mob.RegisterSystem(mob.System{Name: "misc-hostile/endermite_lifetime", Stage: mob.StageSensors, Order: 220, Tick: endermiteTick})
}

type lifetimeKey struct{}

// endermiteTick is the lifetime of Endermite::aiStep: an endermite counts
// every AI step unless it is nameable and has a name tag, and is removed
// after 2400 (two minutes), persistent or not. BDS saves the count as
// Lifetime; here it starts again when the endermite is loaded (no
// endermite component saves it).
func endermiteTick(m *mob.Mob) {
	if m.Identifier() != "minecraft:endermite" {
		return
	}
	n, ok := lifetimeOf(m)
	if !ok {
		n = new(int)
		m.SetData(lifetimeKey{}, n)
	}
	if !m.HasComponent("minecraft:nameable") || m.NameTag() == "" {
		*n++
	}
	if *n >= endermiteLifetime {
		_ = m.Close()
	}
}

// lifetimeOf returns the lifetime counter of an endermite.
func lifetimeOf(m *mob.Mob) (*int, bool) {
	v, ok := m.Data(lifetimeKey{})
	if !ok {
		return nil, false
	}
	return v.(*int), true
}
