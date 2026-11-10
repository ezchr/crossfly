//go:build crossfly_full

package jmob

import (
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// DefaultRandomPosTowards is DefaultRandomPos.getPosTowards(mob, h, v,
// towards, maxXzRadians). It uses the mob base's when it has one and
// falls back to LandRandomPos.getPosTowards.
func DefaultRandomPosTowards(m *javamob.Mob, h, v int, towards mgl64.Vec3, maxRad float64) (mgl64.Vec3, bool) {
	if d, ok := any(m).(interface {
		DefaultRandomPosTowards(h, v int, towards mgl64.Vec3, maxRad float64) (mgl64.Vec3, bool)
	}); ok {
		return d.DefaultRandomPosTowards(h, v, towards, maxRad)
	}
	return m.LandRandomPosTowards(h, v, towards)
}

// LastDamageSource is LivingEntity.getLastDamageSource (kept 40 ticks). It
// uses the mob base's when it has one, else the last attacker within 40
// ticks.
func LastDamageSource(m *javamob.Mob) (world.DamageSource, bool) {
	if d, ok := any(m).(interface {
		LastDamageSource() (world.DamageSource, bool)
	}); ok {
		return d.LastDamageSource()
	}
	by, ok := m.LastHurtByMob()
	if !ok || m.TickCount()-m.LastHurtByMobTimestamp() > 40 {
		return nil, false
	}
	return entity.AttackDamageSource{Attacker: by}, true
}
