package player

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/go-gl/mathgl/mgl64"
)

var _ block.WindBurstLauncher = (*Player)(nil)

// windLaunch is BDS 1.21.3 WindChargeKnockbackComponent: a player a wind burst with negates_fall_damage
// pushed remembers the height it was pushed at (Explosion::explode sets it to the player's Y, every burst
// again), and WindChargeFallDamageSystem drops it once the player was on the ground (or in water or lava
// not rising, levitating, gliding or riding) for more than 10 ticks, or teleported.
type windLaunch struct {
	active bool
	y      float64
	ticks  int
	// bursts counts every wind burst that pushed the player, for anti-cheats (WindBursts).
	bursts uint64
}

// limitFall is the fall distance counted on landing at y: only what the player fell below the height the
// burst pushed it at. BDS 1.21.3 keeps that height in WindChargeKnockbackComponent, but the code that reads
// it on landing is not in the dump; Java fallback: no Bedrock data. Java's
// ServerPlayer.currentImpulseImpactPos rule (fall distance at most impactY - y) is used.
func (w windLaunch) limitFall(fallDistance, y float64) float64 {
	if !w.active {
		return fallDistance
	}
	return math.Min(fallDistance, math.Max(w.y-y, 0))
}

// WindBurstLaunch adds the push of a wind burst to the player's velocity, less the knockback resistance of
// its armour (none at all from a resistance of 1). With negatesFallDamage, the player's later fall damage
// only counts from the height it was pushed at.
func (p *Player) WindBurstLaunch(push mgl64.Vec3, negatesFallDamage bool) {
	if p.Dead() || !finiteVec3(push) {
		return
	}
	res := p.Armour().KnockBackResistance()
	if res >= 1 {
		return
	}
	push = push.Mul(math.Max(1-res, 0))
	p.wind.bursts++
	if negatesFallDamage {
		if !p.wind.active {
			p.wind = windLaunch{active: true, bursts: p.wind.bursts}
		}
		p.wind.y = p.Position().Y()
	}
	p.SetVelocity(p.Velocity().Add(push))
}

// tickWindLaunch is WindChargeFallDamageSystem::_tickWindChargeFallDamageSystem.
func (p *Player) tickWindLaunch() {
	if !p.wind.active {
		return
	}
	vy := p.Velocity().Y()
	_, liquid := p.tx.Liquid(cube.PosFromVec3(p.Position()))
	_, levitating := p.Effect(effect.Levitation)
	if (p.OnGround() && vy <= 0) || levitating || p.Gliding() || p.riddenEntity != nil || (liquid && vy < 1.1920929e-07) {
		p.wind.ticks++
	}
	if p.wind.ticks > 10 {
		p.wind = windLaunch{bursts: p.wind.bursts}
	}
}

// WindLaunched reports whether a wind burst pushed the player and at what height, while that still limits
// its fall damage.
func (p *Player) WindLaunched() (float64, bool) {
	return p.wind.y, p.wind.active
}

// WindBursts is how many wind bursts have pushed the player since it joined: a change means the server
// gave it a push the client shows a ping later (an anti-cheat can't see it otherwise: the push is set as
// velocity on the client, and a burst deals no damage).
func (p *Player) WindBursts() uint64 {
	return p.wind.bursts
}
