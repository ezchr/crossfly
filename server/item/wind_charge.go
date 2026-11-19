package item

import (
	"time"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// WindCharge is a throwable item crafted from breeze rods. Thrown, it bursts where it lands and pushes
// everything nearby away, the thrower too (a "wind charge jump").
//
// Values from the vanilla 1.21.0 behaviour pack (items/wind_charge.json): max stack 64,
// minecraft:cooldown {category wind_charge, duration 0.5}, minecraft:projectile
// wind_charge_projectile, minecraft:throwable {launch_power_scale 1.5, max_launch_power 1.5}.
// BDS ThrowableItemComponent::_doThrow plays the "throw" sound and takes one item unless the player
// has instabuild (creative).
type WindCharge struct{}

// Use throws a wind charge from the user's eyes at 1.5 blocks a tick.
func (WindCharge) Use(tx *world.Tx, user User, ctx *UseContext) bool {
	create := tx.World().EntityRegistry().Config().WindCharge
	if create == nil {
		return false
	}
	opts := world.EntitySpawnOpts{Position: eyePosition(user), Velocity: user.Rotation().Vec3().Mul(1.5)}
	tx.AddEntity(create(opts, user))
	tx.PlaySound(user.Position(), sound.WindChargeThrow{})

	ctx.SubtractFromCount(1)
	return true
}

// Cooldown is the 0.5 s use cooldown of a wind charge.
func (WindCharge) Cooldown() time.Duration {
	return time.Second / 2
}

// MaxCount ...
func (WindCharge) MaxCount() int {
	return 64
}

// EncodeItem ...
func (WindCharge) EncodeItem() (name string, meta int16) {
	return "minecraft:wind_charge", 0
}
