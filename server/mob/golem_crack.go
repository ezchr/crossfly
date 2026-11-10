//go:build crossfly_full

package mob

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

func init() { RegisterHurtHook(HurtHook{Name: "iron golem crack", Hurt: ironGolemCrack}) }

// ironGolemCrack plays the crack sound of an iron golem a hit took into a lower crack level (IronGolem::hurtEffects):
// the levels start at 25, 50 and 75 health, 75, 50 and 25 % of the vanilla golem's 100. BDS plays LevelSoundEvent
// CrackIronGolem at the golem's body, which the client plays as mob.irongolem.crack; it is played here by that name,
// which Java viewers hear as entity.iron_golem.damage.
func ironGolemCrack(m *Mob, dealt float64, _ world.DamageSource) {
	if dealt <= 0 || m.Identifier() != "minecraft:iron_golem" {
		return
	}
	// BDS compares the level of the health (Actor::getHealth, an int) with that of the health plus the damage.
	health := int(m.Health())
	if ironGolemCrackLevel(health) == ironGolemCrackLevel(int(float64(health)+dealt)) {
		return
	}
	body := m.Position().Add(mgl64.Vec3{0, m.BBox().Height() * 0.5, 0})
	m.Tx().PlaySound(body, sound.Custom{Name: "mob.irongolem.crack", Volume: 1, Pitch: 1})
}

// ironGolemCrackLevel is how cracked an iron golem with the health passed looks: 0 below 25, 1 below 50, 2 below 75,
// 3 from 75.
func ironGolemCrackLevel(health int) int {
	switch {
	case health < 25:
		return 0
	case health < 50:
		return 1
	case health < 75:
		return 2
	}
	return 3
}
