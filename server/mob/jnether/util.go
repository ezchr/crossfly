//go:build crossfly_full

package jnether

import (
	"math"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat"
	"github.com/df-mc/dragonfly/server/world"
)

// alive is LivingEntity.isAlive for any entity.
func alive(e world.Entity) bool {
	if l, ok := e.(entity.Living); ok {
		return !l.Dead()
	}
	return true
}

// canAttackDefault is LivingEntity.canAttack: a living entity that may be
// attacked (no creative or spectator player).
func canAttackDefault(e world.Entity) bool {
	if _, ok := e.(entity.Living); !ok {
		return false
	}
	return targetable(e)
}

func sqrt(v float64) float64 { return math.Sqrt(v) }

// fireImmune is LivingEntity.isInvulnerableTo for a fireImmune type: no
// damage tagged is_fire (fire, lava, fireballs). The base does not do this
// for FireImmune types, so each of this package's fire-immune mobs filters
// its damage with it.
func fireImmune(d *mob.Damage) {
	if d.Source != nil && d.Source.Fire() {
		d.Cancel = true
	}
}

// mobGriefing is the mobGriefing game rule.
func mobGriefing(tx *world.Tx) bool { return combat.MobGriefing(tx) }
