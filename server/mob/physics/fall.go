//go:build crossfly_full

package physics

import (
	"math"

	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
)

// The mobs a rider's client moves skip travel; package mob lands them with
// land too when its client brings them down (mob.SetLand).
func init() { mob.SetLand(land) }

// fallDamage is the damage of a landing after a dist block fall,
// causeFallDamageToActor of the mob's BDS class (fall damage multiplier
// data and block multiplier 1):
//   - Horse (horse, donkey, mule, skeleton and zombie horse):
//     ceil(dist x 0.5 - 3), no jump boost; a fall of more than one block
//     plays the land sound;
//   - Llama (llama, trader llama): ceil(dist x 0.5 - 3), nothing under 6
//     blocks;
//   - every other mob (Mob::causeFallDamageToActor): ceil(dist - 3 - jump
//     boost level).
func fallDamage(m *mob.Mob, dist float64) float64 {
	switch m.Identifier() {
	case "minecraft:horse", "minecraft:donkey", "minecraft:mule", "minecraft:skeleton_horse", "minecraft:zombie_horse":
		if dist > 1 {
			m.PlaySound("land")
		}
		return math.Ceil(dist*0.5 - 3)
	case "minecraft:llama", "minecraft:trader_llama":
		if dist < 6 {
			return 0
		}
		return math.Ceil(dist*0.5 - 3)
	}
	jb := 0.0
	if e, ok := m.Effect(effect.JumpBoost); ok {
		jb = float64(e.Level())
	}
	return math.Ceil(dist - 3 - jb)
}
