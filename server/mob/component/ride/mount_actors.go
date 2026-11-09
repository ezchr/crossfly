//go:build crossfly_full

package ride

import "github.com/df-mc/dragonfly/server/mob"

// Hard-coded actor behaviour of the BDS Horse and Camel classes that is not
// about riding: natural healing and the camel's sit events.

func init() {
	mob.RegisterSystem(mob.System{Name: "ride/horse_camel", Stage: mob.StageBody, Order: 50, Tick: horseCamelTick})
}

func horseCamelTick(m *mob.Mob) {
	switch m.Identifier() {
	case "minecraft:horse", "minecraft:donkey", "minecraft:mule", "minecraft:skeleton_horse", "minecraft:zombie_horse":
		naturalHeal(m)
	case "minecraft:camel", "minecraft:camel_husk":
		naturalHeal(m)
		monitorSitting(m)
	}
}

// naturalHeal is the healing of Horse::postAiStep and Camel::aiStep: a
// living mob (deathTime 0; the system only runs for living mobs) heals 1
// health with chance 1/900 a tick. Llamas are not Horses in Bedrock and do
// not heal. camel_husk is a Camel (see monitorSitting).
func naturalHeal(m *mob.Mob) {
	if m.Rand().IntN(900) == 0 && m.Health() < m.MaxHealth() {
		m.Heal(1, naturalHealing{})
	}
}

// naturalHealing is the healing source of naturalHeal.
type naturalHealing struct{}

func (naturalHealing) HealingSource() {}

// camelSitKey keeps the sitting state monitorSitting saw last.
type camelSitKey struct{}

// monitorSitting is Camel::_monitorSitting (server side): when the sitting
// flag changes, the camel runs minecraft:start_sitting or
// minecraft:stop_sitting, whose groups swap the collision box (2.375 high
// standing, 0.945 sitting). It starts out standing. camel_husk is newer
// than BDS 1.21.3; its definition has the same two events, which nothing
// but this Camel code fires, so it is a Camel.
func monitorSitting(m *mob.Mob) {
	sitting := m.Meta().Sitting
	last, _ := m.Data(camelSitKey{})
	if was, _ := last.(bool); was == sitting {
		return
	}
	m.SetData(camelSitKey{}, sitting)
	if sitting {
		m.Fire("minecraft:start_sitting", nil)
	} else {
		m.Fire("minecraft:stop_sitting", nil)
	}
}
