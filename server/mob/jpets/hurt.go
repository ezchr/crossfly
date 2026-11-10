//go:build crossfly_full

package jpets

import (
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// Who hurt whom, for OwnerHurtTargetGoal and OwnerHurtByTargetGoal
// (LivingEntity.lastHurtMob / lastHurtByMob with their timestamps). A
// player's attackers are kept by package player (Player.LastAttacker and
// LastHurtTarget); the mobs a player or mob hits are recorded here by a hurt
// hook on every mob. Timestamps are sequence numbers: the goals only compare
// them for equality (a new hit), and the 100 tick window of getLastHurtByMob
// is checked with the wall clock.

func init() {
	mob.RegisterHurtHook(mob.HurtHook{Name: "jpets/last_hurt_mob", Hurt: recordHit})
}

var (
	hitsMu sync.Mutex
	hits   = map[*world.EntityHandle]hitRecord{}
	hitSeq int64
)

type hitRecord struct {
	victim *world.EntityHandle
	seq    int64
	at     time.Time
}

// attackerOf is the entity that dealt src: the attacker, or the owner of a
// projectile.
func attackerOf(src world.DamageSource) world.Entity {
	switch s := src.(type) {
	case entity.AttackDamageSource:
		return s.Attacker
	case entity.ProjectileDamageSource:
		return s.Owner
	}
	return nil
}

func recordHit(m *mob.Mob, _ float64, src world.DamageSource) {
	if !javamob.Installed() {
		return
	}
	m.SetData(lastDamageKey{}, damageRecord{typ: damageTypeOf(src), tick: m.Ticks()})
	a := attackerOf(src)
	if a == nil || a.H() == m.H() {
		return
	}
	hitsMu.Lock()
	hitSeq++
	hits[a.H()] = hitRecord{victim: m.H(), seq: hitSeq, at: time.Now()}
	hitsMu.Unlock()
}

// LastHurtMob is LivingEntity.getLastHurtMob with its timestamp: the mob or
// player e last hurt.
func LastHurtMob(e world.Entity) (*world.EntityHandle, int64, bool) {
	hitsMu.Lock()
	r, ok := hits[e.H()]
	hitsMu.Unlock()
	if p, isPlayer := e.(*player.Player); isPlayer {
		// A player the owner hit (package player records the victim).
		if v, at, ok2 := player.LastHurtTarget(p.H()); ok2 && (!ok || at.After(r.at)) {
			return v, at.UnixNano(), true
		}
	}
	if !ok {
		return nil, 0, false
	}
	return r.victim, r.seq, true
}

// lastHurtByMobOf is getLastHurtByMob with getLastHurtByMobTimestamp: who
// hurt e within the last 100 ticks.
func lastHurtByMobOf(e world.Entity) (*world.EntityHandle, int64, bool) {
	switch o := e.(type) {
	case *player.Player:
		h, at, ok := o.LastAttacker()
		if !ok || sinceTicks(at) > 100 {
			return nil, 0, false
		}
		return h, at.UnixNano(), true
	}
	return nil, 0, false
}

// sinceTicks is the server ticks (50 ms) since t.
func sinceTicks(t time.Time) int64 { return int64(time.Since(t) / (50 * time.Millisecond)) }

// lastDamageKey keeps LivingEntity.lastDamageSource (its Java damage type)
// and lastDamageStamp of a mob.
type lastDamageKey struct{}

type damageRecord struct {
	typ  string
	tick int64
}

// damageTypeOf is the Java damage type of a Dragonfly damage source.
func damageTypeOf(src world.DamageSource) string {
	switch s := src.(type) {
	case entity.AttackDamageSource:
		if _, ok := s.Attacker.(*player.Player); ok {
			return "minecraft:player_attack"
		}
		return "minecraft:mob_attack"
	case entity.ProjectileDamageSource:
		return "minecraft:arrow"
	case entity.ExplosionDamageSource:
		return "minecraft:explosion"
	case entity.LightningDamageSource:
		return "minecraft:lightning_bolt"
	case entity.FallDamageSource:
		return "minecraft:fall"
	case entity.DrowningDamageSource:
		return "minecraft:drown"
	case entity.SuffocationDamageSource:
		return "minecraft:in_wall"
	case entity.VoidDamageSource:
		return "minecraft:out_of_world"
	case block.FireDamageSource:
		return "minecraft:on_fire"
	case block.LavaDamageSource:
		return "minecraft:lava"
	case block.MagmaDamageSource:
		return "minecraft:hot_floor"
	case block.DamageSource:
		return "minecraft:cactus"
	case effect.PoisonDamageSource, effect.InstantDamageSource:
		return "minecraft:magic"
	case effect.WitherDamageSource:
		return "minecraft:wither"
	}
	return "minecraft:generic"
}

// lastDamageType is getLastDamageSource() (kept 40 ticks) as its Java type.
func lastDamageType(m *javamob.Mob) string {
	v, ok := m.Data(lastDamageKey{})
	if !ok {
		return ""
	}
	r := v.(damageRecord)
	if m.Ticks()-r.tick > 40 {
		return ""
	}
	return r.typ
}
