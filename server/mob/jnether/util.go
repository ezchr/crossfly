//go:build crossfly_full

package jnether

import (
	"math"

	"github.com/df-mc/dragonfly/server/entity"
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

// mobGriefing is the mobGriefing game rule.
func mobGriefing(tx *world.Tx) bool { return combat.MobGriefing(tx) }
