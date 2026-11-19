package entity

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// NewWindCharge creates a wind charge, the projectile of a thrown wind charge item, with an owner (nil for a
// dispenser).
func NewWindCharge(opts world.EntitySpawnOpts, owner world.Entity) *world.EntityHandle {
	conf := windChargeConf
	conf.Owner = ownerHandle(owner)
	return opts.New(WindChargeType, conf)
}

// The wind charge of the 1.26.50 behaviour pack (entities/wind_charge_projectile.json):
// minecraft:projectile {gravity 0, inertia 1 (no drag), power 1.5, on_hit impact_damage 1 with knockback,
// wind_burst_on_hit}, and minecraft:explode {power 1.2, knockback_scaling 1.22, damage_scaling 0,
// breaks_blocks false, causes_fire false, allow_underwater true, toggles_blocks true, max_resistance 0,
// negates_fall_damage true, particle_effect wind_burst, sound_effect wind_charge.burst}.
const (
	windChargePower            = 1.2
	windChargeKnockbackScaling = 1.22
)

var windChargeConf = ProjectileBehaviourConfig{
	Gravity: 0,
	Drag:    0,
	// The impact damage is a flat 1. Dragonfly's projectile damage is ceil(Damage x speed), which is 1 for
	// any speed up to 2 with 0.5: thrown (1.5) or dispensed (1.1).
	Damage: 0.5,
	Hit:    windChargeHit,
}

// windChargeHit is WindChargeProjectile::remove: the charge bursts where it is when it hits a block or an
// entity, then it is gone.
func windChargeHit(e *Ent, tx *world.Tx, _ trace.Result) {
	var vel = e.Velocity()
	if p, ok := e.Behaviour().(*ProjectileBehaviour); ok {
		vel = p.HitVelocity()
	}
	block.WindBurst{
		Power:             windChargePower,
		KnockbackScaling:  windChargeKnockbackScaling,
		NegatesFallDamage: true,
		Source:            e,
		SourceVelocity:    vel,
		Sound:             sound.WindChargeBurst{},
		Particle:          particle.WindExplosion{},
	}.Burst(tx, e.Position())
	_ = e.Close()
}

// WindChargeType is a world.EntityType implementation for wind charges.
var WindChargeType windChargeType

type windChargeType struct{}

func (windChargeType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Ent{tx: tx, handle: handle, data: data}
}

func (windChargeType) EncodeEntity() string { return "minecraft:wind_charge_projectile" }

// BBox is the collision_box of the definition: 0.3125 wide and high.
func (windChargeType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.15625, 0, -0.15625, 0.15625, 0.3125, 0.15625)
}

func (windChargeType) DecodeNBT(_ map[string]any, data *world.EntityData) {
	data.Data = windChargeConf.New()
}
func (windChargeType) EncodeNBT(*world.EntityData) map[string]any { return nil }
