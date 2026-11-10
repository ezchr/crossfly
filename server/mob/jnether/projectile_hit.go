//go:build crossfly_full

package jnether

import (
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/mob/component/combat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// FireballDamageSource is the fireball damage type (DamageSources.fireball:
// is_projectile and is_fire): the hit of a small or large fireball. Fire
// resistance and fire immunity (blazes, ghasts) ignore it. Large reports a
// large (ghast) fireball, which Ghast.isReflectedFireball checks.
type FireballDamageSource struct {
	entity.ProjectileDamageSource
	Large bool
}

// Fire ...
func (FireballDamageSource) Fire() bool { return true }

// DirectEntity is the projectile (DamageSource.getDirectEntity).
func (s FireballDamageSource) DirectEntity() world.Entity { return s.Projectile }

// CausingEntity is the shooter (DamageSource.getEntity).
func (s FireballDamageSource) CausingEntity() world.Entity { return s.Owner }

// MagicDamageSource is DamageSources.magic: a wither skull without a living
// owner. Armour does not reduce it.
type MagicDamageSource struct{ Direct world.Entity }

func (MagicDamageSource) ReducedByArmour() bool     { return false }
func (MagicDamageSource) ReducedByResistance() bool { return true }
func (MagicDamageSource) Fire() bool                { return false }
func (MagicDamageSource) IgnoreTotem() bool         { return false }

// WitherHealingSource is RegainReason.WITHER: a wither healing after its
// skull killed something.
type WitherHealingSource struct{}

// HealingSource ...
func (WitherHealingSource) HealingSource() {}

// entitySound is a sound event of an entity type: Bedrock plays it as a
// LevelSoundEvent with the type, Java as minecraft:entity.<type>.<event>.
type entitySound struct{ event, id string }

func (entitySound) Play(*world.World, mgl64.Vec3) {}

// LevelSoundEvent ...
func (s entitySound) LevelSoundEvent() (event, entityType string, baby bool) {
	return s.event, s.id, false
}

// hurt hurts victim, reporting whether it took the hit.
func hurt(victim world.Entity, dmg float64, src world.DamageSource) bool {
	_, vulnerable, _ := entity.HurtEntity(victim, dmg, src)
	return vulnerable
}

// smallFireballHit is SmallFireball.onHitEntity/onHitBlock/onHit: the
// entity hit burns for 5 s and takes 5 (its fire is put back when the hit
// did nothing); the block hit gets fire on its face when the fireball is
// incendiary.
func (p *javaProjectile) smallFireballHit(e *entity.Ent, tx *world.Tx, hit trace.Result) {
	owner := p.ownerEntity(tx)
	switch r := hit.(type) {
	case entityHit:
		victim := r.e
		f, burns := victim.(interface {
			OnFireDuration() time.Duration
			SetOnFire(time.Duration)
		})
		var before time.Duration
		if burns {
			before = f.OnFireDuration()
			if before < 5*time.Second {
				f.SetOnFire(5 * time.Second)
			}
		}
		src := FireballDamageSource{ProjectileDamageSource: entity.ProjectileDamageSource{Projectile: e, Owner: owner}}
		if !hurt(victim, 5, src) && burns {
			f.SetOnFire(before)
		}
	case trace.BlockResult:
		if p.incendiary {
			pos := r.BlockPosition().Side(r.Face())
			if _, air := tx.Block(pos).(block.Air); air {
				block.Fire{}.Start(tx, pos)
			}
		}
	}
	p.discard(e)
}

// largeFireballHit is LargeFireball.onHitEntity + onHit: 6 to the entity
// hit, then an explosion of explosionPower where the fireball is
// (ExplosionInteraction.MOB: blocks only with mobGriefing, fire when
// incendiary).
func (p *javaProjectile) largeFireballHit(e *entity.Ent, tx *world.Tx, hit trace.Result) {
	if r, ok := hit.(entityHit); ok {
		src := FireballDamageSource{ProjectileDamageSource: entity.ProjectileDamageSource{Projectile: e, Owner: p.ownerEntity(tx)}, Large: true}
		hurt(r.e, 6, src)
	}
	explodeMob(tx, e, p.explosionPower, p.incendiary)
	p.discard(e)
}

// explodeMob is Level.explode with ExplosionInteraction.MOB: blocks break
// (drops decay as 1/power) only with mobGriefing; entities are always hit.
func explodeMob(tx *world.Tx, src *entity.Ent, power float64, fire bool) {
	if !combat.MobGriefing(tx) {
		combat.ExplodeEntitiesOnly(tx, src, power)
		return
	}
	block.ExplosionConfig{SpawnFire: fire, Source: src}.Explode(tx, world.EntityExplosionSource{Entity: src, ExplosionSize: power})
}

// skullHit is WitherSkull.onHitEntity + onHit: 8 (wither_skull) from a
// living owner, who heals 5 when the hit kills; 5 magic without one; Wither
// II for 10 s on normal, 40 s on hard; then an explosion of power 1
// without fire (MOB).
func (p *javaProjectile) skullHit(e *entity.Ent, tx *world.Tx, hit trace.Result) {
	if r, ok := hit.(entityHit); ok {
		victim := r.e
		owner := p.ownerEntity(tx)
		var wasHurt bool
		if lo, ok := owner.(entity.Living); ok && !lo.Dead() {
			wasHurt = hurt(victim, 8, entity.ProjectileDamageSource{Projectile: e, Owner: owner})
			if wasHurt {
				if l, ok := victim.(entity.Living); ok && l.Dead() {
					lo.Heal(5, WitherHealingSource{})
				}
			}
		} else {
			wasHurt = hurt(victim, 5, MagicDamageSource{Direct: e})
		}
		if l, ok := victim.(entity.Living); ok && wasHurt && !l.Dead() {
			if secs := witherSeconds(tx); secs > 0 {
				l.AddEffect(effect.New(effect.Wither, 2, time.Duration(secs)*time.Second))
			}
		}
	}
	explodeMob(tx, e, 1, false)
	p.discard(e)
}

// witherSeconds is the Wither effect length of a skull hit by difficulty:
// 10 s on normal, 40 s on hard, none on easy.
func witherSeconds(tx *world.Tx) int {
	switch difficultyID(tx) {
	case 2:
		return 10
	case 3:
		return 40
	}
	return 0
}

// difficultyID is the world difficulty as Java's id (0 peaceful .. 3 hard).
func difficultyID(tx *world.Tx) int {
	id, ok := world.DifficultyID(tx.World().Difficulty())
	if !ok {
		return 2
	}
	return id
}

// dragonFireballHit is DragonFireball.onHit: unless it hit its owner, a
// cloud of dragon's breath (Harming II, radius 3 growing to 7 over its 600
// ticks) where it landed, or on the first living entity within 4 blocks.
func (p *javaProjectile) dragonFireballHit(e *entity.Ent, tx *world.Tx, hit trace.Result) {
	if r, ok := hit.(entityHit); ok && p.owner != nil && r.e.H() == p.owner {
		return
	}
	pos := p.data.Pos
	box := ProjectileTypeBox(p.t, pos).GrowVec3(mgl64.Vec3{4, 2, 4})
	for other := range tx.EntitiesWithin(box.Grow(2)) {
		if _, ok := other.(entity.Living); !ok {
			continue
		}
		if !other.H().Type().BBox(other).Translate(other.Position()).IntersectsWith(box) {
			continue
		}
		if other.Position().Sub(pos).LenSqr() < 16 {
			pos = other.Position()
			break
		}
	}
	const duration = 600
	cloud := entity.NewAreaEffectCloudWith(world.EntitySpawnOpts{Position: pos}, potion.StrongHarming(),
		duration*time.Second/20, time.Second, 0, 3, 0, (7.0-3.0)/duration)
	tx.AddEntity(cloud)
	tx.AddParticle(pos, particle.Named{Bedrock: "minecraft:dragon_breath_lingering", Java: "dragon_breath"})
	tx.PlaySound(pos, sound.Explosion{})
	p.discard(e)
}

// ProjectileTypeBox is the box of a projectile of t at pos.
func ProjectileTypeBox(t ProjectileType, pos mgl64.Vec3) cube.BBox {
	return t.BBox(nil).Translate(pos)
}
