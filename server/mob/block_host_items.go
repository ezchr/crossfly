//go:build crossfly_full

package mob

import "github.com/df-mc/dragonfly/server/world"

// FireMobEvent fires the entity event on e if it is a mob, as a thrown brown or blue egg does on the chicks it
// hatches (minecraft:hatch_warm, minecraft:hatch_cold). It reports whether e is a mob.
func (BlockHost) FireMobEvent(e world.Entity, event string) bool {
	m, ok := e.(*Mob)
	if !ok {
		return false
	}
	m.Fire(event, nil)
	return true
}
