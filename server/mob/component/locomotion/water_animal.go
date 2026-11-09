//go:build crossfly_full

package locomotion

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/go-gl/mathgl/mgl64"
)

func init() {
	mob.RegisterSystem(mob.System{Name: "locomotion/water_animal", Stage: mob.StageSensors, Order: 53, Tick: waterAnimalTick})
}

// waterAnimalFlop returns the flop velocity factors of a mob of a BDS 1.21.3
// WaterAnimal class (WaterAnimalFlag: Fish = cod, Salmon, Pufferfish,
// TropicalFish, Dolphin, Tadpole), or false for any other mob.
// WaterAnimal::getFlopVerticalVelocityFactor is 0.5 (its vtable slot is
// folded with EnderCrystal::getShadowRadius), getFlopHorizontalVelocityFactor
// 0.2; the tadpole overrides them with 0.8 and 0.1. Squids are not
// WaterAnimals.
func waterAnimalFlop(id string) (vertical, horizontal float64, ok bool) {
	switch id {
	case "minecraft:cod", "minecraft:salmon", "minecraft:pufferfish", "minecraft:tropicalfish", "minecraft:dolphin":
		return 0.5, 0.2, true
	case "minecraft:tadpole":
		return 0.8, 0.1, true
	}
	return 0, 0, false
}

// waterAnimalTick is WaterAnimal::preAiStep (WaterAnimalPreAIStepSystem,
// before the AI):
//   - on the ground out of water the mob flops: up by the vertical factor
//     (x Mob::getJumpMultiplier, 1 without honey blocks), sideways by the
//     horizontal factor x (random x 2 - 1) on x and z, turned to a random
//     yaw, with the flop sound, and it is off the ground;
//   - otherwise (in water or in the air) it sinks by 0.1 unless the block
//     above its feet is water or it levitates: fish (has_gravity false) fall
//     out of the air and keep under the surface. Not for the dolphin yet:
//     BDS DolphinMoveControl (not ported; it has the generic move control)
//     swims and breaches against that push, the generic control cannot, so
//     the dolphin would never reach the surface to breach or breathe.
//
// Dolphin::aiStep's breathe sound on land never plays: it asks for the
// ground, which preAiStep has just left.
func waterAnimalTick(m *mob.Mob) {
	vertical, horizontal, ok := waterAnimalFlop(m.Identifier())
	if !ok {
		return
	}
	p := m.Physics()
	v := m.Vel()
	if p.OnGround && !p.InWater {
		r := m.Rand()
		v[1] += vertical
		v[0] += horizontal * (r.Float64()*2 - 1)
		v[2] += horizontal * (r.Float64()*2 - 1)
		m.SetVelocity(v)
		m.SetYaw(r.Float64() * 360)
		m.PlaySound("flop")
		p.OnGround = false
		return
	}
	if m.Identifier() == "minecraft:dolphin" {
		return
	}
	if _, levitating := m.Effect(effect.Levitation); levitating {
		return
	}
	if l, ok := m.Tx().Liquid(cube.PosFromVec3(m.Pos().Add(mgl64.Vec3{0, 1, 0}))); ok {
		if _, water := l.(block.Water); water {
			return
		}
	}
	v[1] -= 0.1
	m.SetVelocity(v)
}
