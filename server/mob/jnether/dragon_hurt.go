//go:build crossfly_full

package jnether

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

// SetHitPart records the part a Java client attacked (javasession calls
// it with the part entity's index before the hit lands).
func (d *Dragon) SetHitPart(part int) { d.hitPart = part }

// partHit is the part a hit struck: the one a Java client named, else the
// first part on the attacker's line of sight (a Bedrock client only names
// the dragon), else the body.
func (d *Dragon) partHit(src world.DamageSource) int {
	if d.hitPart >= 0 {
		p := d.hitPart
		d.hitPart = -1
		return p
	}
	var attacker world.Entity
	switch s := src.(type) {
	case entity.AttackDamageSource:
		attacker = s.Attacker
	case entity.ProjectileDamageSource:
		if s.Projectile != nil {
			// Where the projectile is: the part its box touches.
			pb := s.Projectile.H().Type().BBox(s.Projectile).Translate(s.Projectile.Position())
			for i := range dragonParts {
				if d.Part(i).Grow(0.3).IntersectsWith(pb) {
					return i
				}
			}
		}
		return PartBody
	}
	if attacker == nil {
		return PartBody
	}
	eye := entity.EyePosition(attacker)
	end := eye.Add(attacker.Rotation().Vec3().Mul(8))
	best, bestD := PartBody, math.MaxFloat64
	for i := range dragonParts {
		if r, ok := trace.BBoxIntercept(d.Part(i), eye, end); ok {
			if dd := r.Position().Sub(eye).LenSqr(); dd < bestD {
				best, bestD = i, dd
			}
		}
	}
	return best
}

// FilterDamage is EnderDragon.hurt(level, part, source, damage) + the
// handleKillingBlow override, run on every hit before it is dealt: nothing
// while dying; the phase may change it (sitting: arrows and wind charges
// bounce off burning); any part but the head and neck takes d/4 + min(d,
// 1); only players and explosions hurt it; a perched dragon that took a
// quarter of its health takes off; a lethal hit on a flying dragon leaves it
// at 1 health and starts its death flight. It returns the damage to deal,
// and false to cancel the hit.
func (d *Dragon) FilterDamage(h DragonHost, src world.DamageSource, dmg float64) (float64, bool) {
	part := d.partHit(src)
	if d.phases.cur.id() == PhaseDying {
		return 0, false
	}
	dmg = d.phases.cur.onHurt(src, dmg)
	if part != PartHead && part != PartNeck {
		dmg = dmg/4 + math.Min(dmg, 1)
	}
	if dmg < 0.01 {
		return 0, false
	}
	if !hurtsDragon(src) {
		return 0, false
	}
	if d.phases.cur.sitting() {
		d.sittingDamage += math.Min(dmg, h.Health())
		if d.sittingDamage > 0.25*h.MaxHealth() {
			d.sittingDamage = 0
			d.phases.set(h, PhaseTakeoff)
		}
	} else if dmg >= h.Health() {
		// handleKillingBlow: not dead, at 1 health, flying to its death.
		dmg = h.Health() - 1
		d.phases.set(h, PhaseDying)
		if dmg <= 0 {
			return 0, false
		}
	}
	return dmg, true
}

// hurtsDragon: source.getEntity() is a player, or the damage is an
// explosion (always_hurts_ender_dragons = #is_explosion).
func hurtsDragon(src world.DamageSource) bool {
	var by world.Entity
	switch s := src.(type) {
	case entity.AttackDamageSource:
		by = s.Attacker
	case entity.ProjectileDamageSource:
		by = s.Owner
	case FireballDamageSource:
		by = s.Owner
	case entity.ExplosionDamageSource:
		return true
	}
	if by == nil {
		return false
	}
	_, player := by.(interface{ GameMode() world.GameMode })
	return player
}

// AllowKnockback is EnderDragon.knockback: none while sitting.
func (d *Dragon) AllowKnockback() bool { return !d.phases.cur.sitting() }

// expReward is getDefaultExpToDrop: 12000 for the first dragon of a world,
// 500 after.
func (d *Dragon) expReward() int {
	if d.Fight != nil && !d.Fight.PreviouslyKilled() {
		return 12000
	}
	return 500
}

// TickDeath is EnderDragon.tickDeath: 200 ticks rising 0.1 a tick, huge
// explosions from tick 180, experience in bursts of 8% every 5 ticks after
// tick 150 and 20% at the end, the death sound to every player (tick 1),
// then the fight learns the dragon died and the dragon is removed. It
// returns true once the dragon is to be removed.
func (d *Dragon) TickDeath(h DragonHost) bool {
	tx := h.Tx()
	if d.Fight != nil {
		d.Fight.UpdateDragon(tx, h, 0, h.MaxHealth())
	}
	d.deathTime++
	pos := h.Position()
	r := h.Rand()
	if d.deathTime >= 180 && d.deathTime <= 200 {
		p := pos.Add(mgl64.Vec3{(r.Float64() - 0.5) * 8, 2 + (r.Float64()-0.5)*4, (r.Float64() - 0.5) * 8})
		tx.AddParticle(p, particle.HugeExplosion{})
	}
	if d.expToDrop < 0 {
		d.expToDrop = d.expReward()
	}
	if d.deathTime > 150 && d.deathTime%5 == 0 {
		spawnXP(tx, pos, int(math.Floor(float64(d.expToDrop)*0.08)))
	}
	if d.deathTime == 1 {
		for p := range tx.Players() {
			tx.PlaySound(p.Position(), entitySound{event: "death", id: DragonID})
		}
	}
	move := mgl64.Vec3{0, 0.1, 0}
	h.SetPosition(pos.Add(move))
	for i := range d.parts {
		d.parts[i] = d.parts[i].Add(move)
	}
	if d.deathTime >= 200 {
		spawnXP(tx, h.Position(), int(math.Floor(float64(d.expToDrop)*0.2)))
		if d.Fight != nil {
			d.Fight.DragonKilled(tx, h)
		}
		return true
	}
	return false
}

// DragonID is the ender dragon's identifier.
const DragonID = "minecraft:ender_dragon"

func spawnXP(tx *world.Tx, pos mgl64.Vec3, amount int) {
	if amount <= 0 {
		return
	}
	for _, o := range entity.NewExperienceOrbs(pos, amount) {
		tx.AddEntity(o)
	}
}

// igniteProjectile sets a projectile on fire for 1 second
// (Entity.igniteForSeconds(1)).
func igniteProjectile(e world.Entity) {
	if f, ok := e.(interface {
		OnFireDuration() time.Duration
		SetOnFire(time.Duration)
	}); ok && f.OnFireDuration() < time.Second {
		f.SetOnFire(time.Second)
	}
}

// EncodeNBT is the dragon's part of addAdditionalSaveData.
func (d *Dragon) EncodeNBT(m map[string]any) {
	m["DragonPhase"] = int32(d.Phase())
	m["DragonDeathTime"] = int32(d.deathTime)
	m["sitting_damage_received"] = float32(d.sittingDamage)
	if d.expToDrop >= 0 {
		m["Bukkit.expToDrop"] = int32(d.expToDrop)
	}
}

// DecodeNBT is readAdditionalSaveData.
func (d *Dragon) DecodeNBT(h DragonHost, m map[string]any) {
	if v, ok := m["DragonPhase"].(int32); ok {
		d.phases.set(h, int(v))
	}
	if v, ok := m["DragonDeathTime"].(int32); ok {
		d.deathTime = int(v)
	}
	if v, ok := m["sitting_damage_received"].(float32); ok {
		d.sittingDamage = float64(v)
	}
	if v, ok := m["Bukkit.expToDrop"].(int32); ok {
		d.expToDrop = int(v)
	}
}
