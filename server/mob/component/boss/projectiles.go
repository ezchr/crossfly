//go:build crossfly_full

package boss

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Projectiles the bosses fire, from their 1.26 entity JSON
// (minecraft:projectile) and BDS WitherSkull / DragonFireball /
// WindChargeProjectile. They are plain entity types with
// entity.ProjectileBehaviour; Bedrock draws them from the identifier, the
// Java session maps the identifiers (entity_types.go). They are registered
// with combat.RegisterProjectile, so minecraft:shooter (and anything calling
// combat.ShootProjectile) fires them, and listed by ProjectileTypes for a
// server's entity registry.
var (
	// WitherSkullType is minecraft:wither_skull: power 1.2, no gravity, an
	// explosion of power 1 (blocks with mobGriefing) and Wither II for 10 s
	// (normal) / 40 s (hard) on the entity hit.
	WitherSkullType = projectileType{id: "minecraft:wither_skull", size: 0.3125}
	// BlueWitherSkullType is minecraft:wither_skull_dangerous: the same at
	// power 0.6 (BDS lets it break obsidian; Dragonfly's explosions cannot
	// be told so).
	BlueWitherSkullType = projectileType{id: "minecraft:wither_skull_dangerous", size: 0.3125}
	// DragonFireballType is minecraft:dragon_fireball: power 1.3, a cloud of
	// harming (radius 6, 6 s) where it lands.
	DragonFireballType = projectileType{id: "minecraft:dragon_fireball", size: 0.31}
	// BreezeWindChargeType is minecraft:breeze_wind_charge_projectile:
	// power 0.7, 1 damage on a hit, then a wind burst of radius 3 that
	// pushes entities (knockback_scaling 0.6) and hurts nothing.
	BreezeWindChargeType = projectileType{id: "minecraft:breeze_wind_charge_projectile", size: 0.3125}
)

// ProjectileTypes returns the projectile entity types of this package, to
// add to a server's entity registry.
func ProjectileTypes() []world.EntityType {
	return []world.EntityType{WitherSkullType, BlueWitherSkullType, DragonFireballType, BreezeWindChargeType}
}

type projectileType struct {
	id   string
	size float64
}

func (t projectileType) Open(tx *world.Tx, h *world.EntityHandle, data *world.EntityData) world.Entity {
	return entity.Open(tx, h, data)
}
func (t projectileType) EncodeEntity() string { return t.id }
func (t projectileType) BBox(world.Entity) cube.BBox {
	s := t.size / 2
	return cube.Box(-s, 0, -s, s, t.size, s)
}

// DecodeNBT drops saved projectiles: they are not kept across restarts.
func (t projectileType) DecodeNBT(_ map[string]any, data *world.EntityData) {
	data.Data = t.conf(nil).New()
}
func (projectileType) EncodeNBT(*world.EntityData) map[string]any { return nil }

// conf is the projectile behaviour of the type.
func (t projectileType) conf(owner *world.EntityHandle) entity.ProjectileBehaviourConfig {
	c := entity.ProjectileBehaviourConfig{Owner: owner, Gravity: 0, Drag: 0}
	switch t.id {
	case WitherSkullType.id, BlueWitherSkullType.id:
		c.Damage = 0
		c.Hit = skullHit
	case DragonFireballType.id:
		c.Damage = 0
		c.Hit = fireballHit
	case BreezeWindChargeType.id:
		c.Damage = 1
		c.Hit = windChargeHit
	}
	return c
}

// projectileOwner returns the owner of a projectile entity.
func projectileOwner(e world.Entity) *world.EntityHandle {
	ent, ok := e.(*entity.Ent)
	if !ok {
		return nil
	}
	if o, ok := ent.Behaviour().(interface{ Owner() *world.EntityHandle }); ok {
		return o.Owner()
	}
	return nil
}

// skullOwnedByWither reports whether e is a wither skull a wither fired.
func skullOwnedByWither(e world.Entity) bool {
	t := e.H().Type()
	if t != WitherSkullType && t != BlueWitherSkullType {
		return false
	}
	o := projectileOwner(e)
	return o != nil && o.Type().EncodeEntity() == WitherID
}

// spawnProjectile adds a projectile of t at from moving vel.
func spawnProjectile(tx *world.Tx, t projectileType, owner world.Entity, from, vel mgl64.Vec3) *world.EntityHandle {
	yaw := mgl64.RadToDeg(math.Atan2(-vel[0], vel[2]))
	pitch := -mgl64.RadToDeg(math.Atan2(vel[1], math.Hypot(vel[0], vel[2])))
	var oh *world.EntityHandle
	if owner != nil {
		oh = owner.H()
	}
	h := world.EntitySpawnOpts{Position: from, Velocity: vel, Rotation: cube.Rotation{yaw, pitch}}.New(t, t.conf(oh))
	tx.AddEntity(h)
	return h
}

// aim is ProjectileComponent::shoot: normalize(dir) + gaussian x 0.0075 x
// uncertainty on each axis, times power.
func aim(m *mob.Mob, dir mgl64.Vec3, power, uncertainty float64) mgl64.Vec3 {
	if dir.Len() < 1e-4 {
		return mgl64.Vec3{}
	}
	d := dir.Normalize()
	r := m.Rand()
	for i := range d {
		d[i] += r.NormFloat64() * 0.0075 * uncertainty
	}
	return d.Normalize().Mul(power)
}

// ShootWitherSkull is WitherBoss::_performRangedAttack: a skull (blue when
// dangerous) from head 0..2 at pos, with uncertainty 7.5 - difficulty.
func ShootWitherSkull(m *mob.Mob, head int, pos mgl64.Vec3, dangerous bool) {
	tx := m.Tx()
	from := HeadPos(m, head)
	t, power := WitherSkullType, 1.2
	if dangerous {
		t, power = BlueWitherSkullType, 0.6
	}
	vel := aim(m, pos.Sub(mgl64.Vec3{0, 0.5, 0}).Sub(from), power, 7.5-float64(sense.DifficultyID(tx)))
	if vel == (mgl64.Vec3{}) {
		return
	}
	spawnProjectile(tx, t, m, from, vel)
	m.PlaySound("shoot")
}

// ShootDragonFireball fires a dragon fireball from the dragon's head at
// target (ShooterComponent with minecraft:dragon_fireball: power 1.3,
// uncertainty 10).
func ShootDragonFireball(m *mob.Mob, target world.Entity) bool {
	from := DragonOf(m).HeadPos()
	if from == (mgl64.Vec3{}) {
		from = m.Position().Add(mgl64.Vec3{0, m.BBox().Height() / 2, 0})
	}
	vel := aim(m, sense.Center(target).Sub(from), 1.3, 10)
	if vel == (mgl64.Vec3{}) {
		return false
	}
	spawnProjectile(m.Tx(), DragonFireballType, m, from, vel)
	m.PlaySound("shoot")
	return true
}

// ShootWindCharge fires a breeze wind charge from the breeze's body
// (+0.3 of its height) at the target's middle (fire_at_target owner/target
// offsets): power 0.7, uncertainty 5 - 4 x difficulty.
func ShootWindCharge(m *mob.Mob, target world.Entity) bool {
	tx := m.Tx()
	box := m.BBox()
	from := sense.Center(m).Add(mgl64.Vec3{0, box.Height() * 0.3, 0})
	tb := sense.BBox(target)
	to := target.Position().Add(mgl64.Vec3{0, tb.Height() * 0.5, 0})
	vel := aim(m, to.Sub(from), 0.7, math.Max(0, 5-4*float64(sense.DifficultyID(tx))))
	if vel == (mgl64.Vec3{}) {
		return false
	}
	spawnProjectile(tx, BreezeWindChargeType, m, from, vel)
	m.PlaySound("shoot")
	return true
}

// skullHit explodes a wither skull (power 1) and withers the entity hit.
func skullHit(e *entity.Ent, tx *world.Tx, target trace.Result) {
	if r, ok := target.(trace.EntityResult); ok {
		var dur time.Duration
		switch sense.DifficultyID(tx) {
		case 2:
			dur = 10 * time.Second
		case 3:
			dur = 40 * time.Second
		}
		if l, ok := r.Entity().(interface{ AddEffect(effect.Effect) }); ok && dur > 0 {
			l.AddEffect(effect.New(effect.Wither, 2, dur))
		}
	}
	Explode(tx, e, e.Position(), 1, false, MobGriefing(tx))
	_ = e.Close()
}

// fireballHit leaves the dragon's breath: a cloud of harming, radius 6 for
// 6 seconds.
func fireballHit(e *entity.Ent, tx *world.Tx, target trace.Result) {
	pos := target.Position()
	cloud := entity.NewAreaEffectCloudWith(world.EntitySpawnOpts{Position: pos}, potion.Harming(),
		6*time.Second, time.Second, 0, 6, 0, 0)
	tx.AddEntity(cloud)
	tx.AddParticle(pos, particle.Named{Bedrock: "minecraft:dragon_breath_lingering", Java: "dragon_breath"})
	tx.PlaySound(pos, sound.Explosion{})
	_ = e.Close()
}

// windChargeHit is the wind burst of a breeze wind charge.
func windChargeHit(e *entity.Ent, tx *world.Tx, _ trace.Result) {
	WindBurst(tx, e, e.Position(), 3, 0.6)
	_ = e.Close()
}

// WindBurst is WindBurstUtility::burst: no damage, entities within 2r are
// pushed away from the centre by (1 - d/2r) x scaling (less their knockback
// resistance).
func WindBurst(tx *world.Tx, src world.Entity, center mgl64.Vec3, r, scaling float64) {
	box := cube.Box(center[0]-r-1, center[1]-r-1, center[2]-r-1, center[0]+r+1, center[1]+r+1, center[2]+r+1).Grow(r)
	var hit []world.Entity
	for e := range tx.EntitiesWithin(box) {
		if src != nil && e.H() == src.H() {
			continue
		}
		if _, ok := e.(velocityEntity); ok {
			hit = append(hit, e)
		}
	}
	for _, e := range hit {
		eye := sense.EyePos(e)
		dist := mgl64.Vec3{eye[0], sense.BBox(e).Min()[1], eye[2]}.Sub(center).Len()
		if !sense.IsPlayer(e) {
			dist = math.Max(0, dist-sense.Width(e)/2)
		}
		f := dist / (2 * r)
		if f > 1 {
			continue
		}
		res := knockbackResistance(e)
		if res >= 1 {
			continue
		}
		dir := eye.Sub(center)
		if dir.Len() < 1e-4 {
			continue
		}
		Push(e, dir.Normalize().Mul((1-f)*scaling*(1-res)))
	}
	tx.AddParticle(center, particle.Named{Bedrock: "minecraft:breeze_wind_explosion_emitter", Java: "gust_emitter_large"})
}
