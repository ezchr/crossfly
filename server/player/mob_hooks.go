//go:build crossfly_full

package player

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

// Hooks for mobs: entity inventory windows (a horse's saddle, armour and chest) and who last
// hurt the player (pets defend their owner: behavior.owner_hurt_by_target).

// OpenEntityContainer opens the inventory window of e (entity.WindowOf) for the player, if its
// session can show one, and reports whether it did. Both the Bedrock and the Java session can.
func (p *Player) OpenEntityContainer(e world.Entity) bool {
	if o, ok := p.conn().(interface {
		OpenEntityContainer(e world.Entity, tx *world.Tx) bool
	}); ok {
		return o.OpenEntityContainer(e, p.tx)
	}
	return false
}

// lastAttackers holds the entity that last hurt each player (by handle).
var lastAttackers sync.Map

type attackRecord struct {
	attacker *world.EntityHandle
	at       time.Time
}

// recordAttacker remembers who dealt damage from src (an attack, or the owner of a projectile).
func (p *Player) recordAttacker(src world.DamageSource) {
	var attacker world.Entity
	switch s := src.(type) {
	case entity.AttackDamageSource:
		attacker = s.Attacker
	case entity.ProjectileDamageSource:
		attacker = s.Owner
	}
	if attacker == nil || attacker.H() == p.H() {
		return
	}
	lastAttackers.Store(p.H(), attackRecord{attacker: attacker.H(), at: time.Now()})
	if trackHurtTargets.Load() {
		lastHurtTargets.Store(attacker.H(), attackRecord{attacker: p.H(), at: time.Now()})
	}
}

// lastHurtTargets holds the player each entity last hurt (by the attacker's handle), kept only once
// TrackHurtTargets was called (the Java mob mode's pets: nothing is recorded otherwise).
var (
	lastHurtTargets  sync.Map
	trackHurtTargets atomic.Bool
)

// TrackHurtTargets turns on recording the player victims of LastHurtTarget.
func TrackHurtTargets() { trackHurtTargets.Store(true) }

// LastHurtTarget returns the player the entity e last hurt and when (Java's LivingEntity.lastHurtMob for
// player victims: a pet attacks what its owner hit). For players only; mobs hit by players are recorded
// by the mob packages.
func LastHurtTarget(e *world.EntityHandle) (*world.EntityHandle, time.Time, bool) {
	v, ok := lastHurtTargets.Load(e)
	if !ok {
		return nil, time.Time{}, false
	}
	r := v.(attackRecord)
	return r.attacker, r.at, true
}

// mobDamager is a mob (package mob, which imports this package): an entity with type families.
type mobDamager interface {
	world.Entity
	Identifier() string
	Families() []string
}

// DamagingEntitySource is implemented by damage sources dealt by an entity that are not attacks,
// projectiles or explosions, such as a guardian's magic beam: Hurt scales their damage by
// difficulty when the entity is a mob.
type DamagingEntitySource interface {
	world.DamageSource
	DamagingEntity() world.Entity
}

// scaleMobDamage is the difficulty scaling of Player::_hurt: damage dealt by a mob (an attack, an
// explosion it caused, anything with the mob as its damaging actor) is 0 on peaceful, d*0.5+1 when
// above 2 on easy and d*1.5 on hard. Projectiles (cause projectile) and the evoker's fangs (the
// damaging actor type evocation_illager) are not scaled, nor is damage from players.
func (p *Player) scaleMobDamage(dmg float64, src world.DamageSource) float64 {
	var e world.Entity
	switch s := src.(type) {
	case entity.AttackDamageSource:
		e = s.Attacker
	case entity.ExplosionDamageSource:
		if es, ok := s.Source.(world.EntityExplosionSource); ok {
			e = es.Entity
		}
	case DamagingEntitySource:
		e = s.DamagingEntity()
	}
	m, ok := e.(mobDamager)
	if !ok || m.Identifier() == "minecraft:evocation_illager" || p.tx == nil {
		return dmg
	}
	switch p.tx.World().Difficulty() {
	case world.DifficultyPeaceful:
		return 0
	case world.DifficultyEasy:
		if dmg > 2 {
			return dmg*0.5 + 1
		}
	case world.DifficultyHard:
		return dmg * 1.5
	}
	return dmg
}

// LastAttacker returns the entity that last hurt the player (an attacker or the owner of a
// projectile) and when, as BDS keeps an actor's last hurt by mob.
func (p *Player) LastAttacker() (*world.EntityHandle, time.Time, bool) {
	v, ok := lastAttackers.Load(p.H())
	if !ok {
		return nil, time.Time{}, false
	}
	r := v.(attackRecord)
	return r.attacker, r.at, true
}
