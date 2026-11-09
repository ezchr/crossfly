//go:build crossfly_full

package core

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// CauseOf returns the Bedrock damage cause name (ActorDamageCause, as entity
// JSON writes it: "entity_attack", "lava", "fall", ...) of a damage source.
func CauseOf(src world.DamageSource) string {
	switch s := src.(type) {
	case CauseSource:
		return s.Cause
	case entity.AttackDamageSource:
		return "entity_attack"
	case entity.ProjectileDamageSource:
		return "projectile"
	case entity.VoidDamageSource:
		return "void"
	case entity.SuffocationDamageSource:
		return "suffocation"
	case entity.DrowningDamageSource:
		return "drowning"
	case entity.FallDamageSource:
		return "fall"
	case entity.GlideDamageSource:
		return "fly_into_wall"
	case entity.LightningDamageSource:
		return "lightning"
	case entity.ExplosionDamageSource:
		if _, ok := s.Source.(world.Entity); ok {
			return "entity_explosion"
		}
		return "block_explosion"
	case block.FireDamageSource:
		return "fire"
	case block.LavaDamageSource:
		return "lava"
	case block.MagmaDamageSource:
		return "magma"
	case block.DamageSource:
		return "contact"
	case effect.PoisonDamageSource, effect.InstantDamageSource:
		return "magic"
	case effect.WitherDamageSource:
		return "wither"
	case player.StarvationDamageSource:
		return "starve"
	}
	return "none"
}

// SourceOf returns a Dragonfly damage source for a Bedrock cause name, used by
// hurt_on_condition and breathable.
func SourceOf(cause string) world.DamageSource {
	switch cause {
	case "lava":
		return block.LavaDamageSource{}
	case "fire", "fire_tick":
		return block.FireDamageSource{}
	case "magma":
		return block.MagmaDamageSource{}
	case "drowning":
		return entity.DrowningDamageSource{}
	case "suffocation":
		return entity.SuffocationDamageSource{}
	case "void":
		return entity.VoidDamageSource{}
	case "fall":
		return entity.FallDamageSource{}
	case "lightning":
		return entity.LightningDamageSource{}
	}
	return CauseSource{Cause: cause}
}

// CauseSource is a damage source for a Bedrock cause Dragonfly has no own
// source for ("temperature", "freezing", "suffocation" in blocks, ...).
type CauseSource struct{ Cause string }

func (s CauseSource) ReducedByArmour() bool {
	return s.Cause == "entity_attack" || s.Cause == "projectile"
}
func (CauseSource) ReducedByResistance() bool { return true }
func (s CauseSource) Fire() bool {
	return s.Cause == "fire" || s.Cause == "fire_tick" || s.Cause == "lava"
}
func (CauseSource) IgnoreTotem() bool { return false }

// lastHurtKey keys the cause of the last damage a mob took (mob.Data).
type lastHurtKey struct{}

// LastHurt is the last damage a mob took: its Bedrock cause and the mob tick
// (Mob.Ticks) it happened. BDS Actor::getLastHurtCause / getLastHurtTimestamp.
type LastHurt struct {
	Cause string
	Tick  int64
}

// LastHurtOf returns the last damage m took. It is recorded by the
// minecraft:health component (every mob that can be hurt has one).
func LastHurtOf(m *mob.Mob) (LastHurt, bool) {
	v, ok := m.Data(lastHurtKey{})
	if !ok {
		return LastHurt{}, false
	}
	return v.(LastHurt), true
}

func recordHurt(m *mob.Mob, src world.DamageSource) {
	m.SetData(lastHurtKey{}, LastHurt{Cause: CauseOf(src), Tick: m.Ticks()})
}

// LastHitByPlayer reports whether a player hurt the mob in the last 100 ticks
// (Molang query.last_hit_by_player; BDS keeps the player for 100 ticks).
func LastHitByPlayer(m *mob.Mob) bool {
	t := m.LastHurtByPlayerTick()
	return t >= 0 && m.Ticks()-t <= 100
}
