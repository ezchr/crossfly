//go:build crossfly_full

package nether

import (
	"iter"
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The Java projectiles of the nether and End mobs: the hurting projectiles
// (AbstractHurtingProjectile: small and large fireballs, wither skulls,
// dragon fireballs) and the shulker bullet. Each is an entity type with its
// own behaviour (javaProjectile) that moves the entity itself, the way
// Projectile.tick does, and tells viewers. Bedrock and Java clients draw
// them from the identifier (javasession maps wither_skull_dangerous to a
// wither_skull with the dangerous flag).

// ProjectileType is the entity type of one of the projectiles.
type ProjectileType struct {
	id   string
	size float64
	kind projectileKind
}

type projectileKind uint8

const (
	kindSmallFireball projectileKind = iota
	kindLargeFireball
	kindWitherSkull
	kindDangerousSkull
	kindDragonFireball
	kindShulkerBullet
)

var (
	// SmallFireballType is minecraft:small_fireball (SmallFireball, 0.3125
	// blocks): 5 fire damage and 5 s of fire on the entity hit, fire on the
	// block hit (with mobGriefing for mob owners).
	SmallFireballType = ProjectileType{id: "minecraft:small_fireball", size: 0.3125, kind: kindSmallFireball}
	// LargeFireballType is minecraft:fireball (LargeFireball, 1 block): 6
	// damage on the entity hit and an explosion (power 1 for ghasts). Hitting
	// it sends it where the attacker looks.
	LargeFireballType = ProjectileType{id: "minecraft:fireball", size: 1, kind: kindLargeFireball}
	// WitherSkullType is minecraft:wither_skull (WitherSkull, 0.3125).
	WitherSkullType = ProjectileType{id: "minecraft:wither_skull", size: 0.3125, kind: kindWitherSkull}
	// DangerousWitherSkullType is the blue skull (WitherSkull with
	// DATA_DANGEROUS): Bedrock has its own identifier for it.
	DangerousWitherSkullType = ProjectileType{id: "minecraft:wither_skull_dangerous", size: 0.3125, kind: kindDangerousSkull}
	// DragonFireballType is minecraft:dragon_fireball (DragonFireball, 1).
	DragonFireballType = ProjectileType{id: "minecraft:dragon_fireball", size: 1, kind: kindDragonFireball}
	// ShulkerBulletType is minecraft:shulker_bullet (ShulkerBullet, 0.3125).
	ShulkerBulletType = ProjectileType{id: "minecraft:shulker_bullet", size: 0.3125, kind: kindShulkerBullet}
)

// ProjectileTypes returns the projectile entity types, for a server's entity
// registry in Java mob mode (Bedrock mode registers its own types with these
// identifiers).
func ProjectileTypes() []world.EntityType {
	return []world.EntityType{SmallFireballType, LargeFireballType, WitherSkullType, DangerousWitherSkullType, DragonFireballType, ShulkerBulletType}
}

// Open ...
func (t ProjectileType) Open(tx *world.Tx, h *world.EntityHandle, data *world.EntityData) world.Entity {
	if p, ok := data.Data.(*javaProjectile); ok {
		p.data = data
	}
	return entity.Open(tx, h, data)
}

// EncodeEntity ...
func (t ProjectileType) EncodeEntity() string { return t.id }

// BBox ...
func (t ProjectileType) BBox(world.Entity) cube.BBox {
	s := t.size / 2
	return cube.Box(-s, 0, -s, s, t.size, s)
}

// DecodeNBT reads a saved projectile (Projectile/AbstractHurtingProjectile
// addAdditionalSaveData: acceleration_power, ExplosionPower, the shulker
// bullet's steering). The owner and the bullet's target are not kept.
func (t ProjectileType) DecodeNBT(m map[string]any, data *world.EntityData) {
	p := newProjectile(t, nil)
	if v, ok := m["acceleration_power"].(float64); ok {
		p.accel = v
	}
	if v, ok := m["ExplosionPower"].(uint8); ok {
		p.explosionPower = float64(v)
	}
	if v, ok := m["Steps"].(int32); ok {
		p.flightSteps = int(v)
	}
	data.Data = p
}

// EncodeNBT ...
func (t ProjectileType) EncodeNBT(data *world.EntityData) map[string]any {
	p, ok := data.Data.(*javaProjectile)
	if !ok {
		return nil
	}
	m := map[string]any{"acceleration_power": p.accel}
	if t.kind == kindLargeFireball {
		m["ExplosionPower"] = uint8(p.explosionPower)
	}
	if t.kind == kindShulkerBullet {
		m["Steps"] = int32(p.flightSteps)
	}
	return m
}

// projectileConfig puts a behaviour into the entity data of a new handle.
type projectileConfig struct{ p *javaProjectile }

// Apply ...
func (c projectileConfig) Apply(data *world.EntityData) { data.Data = c.p }

// javaProjectile is the behaviour of every projectile type here.
type javaProjectile struct {
	t    ProjectileType
	data *world.EntityData
	r    *rand.Rand

	owner     *world.EntityHandle
	leftOwner bool
	ticks     int
	removed   bool

	// accel is AbstractHurtingProjectile.accelerationPower.
	accel float64
	// explosionPower is LargeFireball.explosionPower.
	explosionPower float64
	// incendiary is AbstractHurtingProjectile.isIncendiary: whether a mob's
	// fireball sets fire (mobGriefing when it was fired).
	incendiary bool
	// lastDeflectedBy is Projectile.lastDeflectedBy.
	lastDeflectedBy *world.EntityHandle

	// Shulker bullet steering (ShulkerBullet).
	target                  *world.EntityHandle
	moveDir                 cube.Face
	hasMoveDir              bool
	flightSteps             int
	targetDelta             mgl64.Vec3
}

func newProjectile(t ProjectileType, owner *world.EntityHandle) *javaProjectile {
	seed := uint64(time.Now().UnixNano())
	return &javaProjectile{t: t, owner: owner, accel: 0.1, explosionPower: 1, incendiary: true, r: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))}
}

// Owner returns the entity that fired the projectile (Projectile.getOwner),
// which entity.Ent.ProjectileOwner and the clients read.
func (p *javaProjectile) Owner() *world.EntityHandle { return p.owner }

// Dangerous reports whether the projectile is a blue wither skull.
func (p *javaProjectile) Dangerous() bool { return p.t.kind == kindDangerousSkull }

// ownerEntity is Projectile.getOwner in tx, nil when gone.
func (p *javaProjectile) ownerEntity(tx *world.Tx) world.Entity {
	if p.owner == nil {
		return nil
	}
	if e, ok := p.owner.Entity(tx); ok {
		return e
	}
	return nil
}

// HurtingOpts are the options of a hurting projectile fired by a mob
// (AbstractHurtingProjectile(type, mob, direction, level)).
type HurtingOpts struct {
	// Owner fired it; the projectile starts at the owner's feet unless Pos
	// is set.
	Owner world.Entity
	// Pos, when not zero, is where it starts (the blaze moves its fireballs
	// to its middle + 0.5, the ghast to its head).
	Pos mgl64.Vec3
	// Dir is the direction; it is normalised and scaled by the acceleration
	// power 0.1 (assignDirectionalMovement).
	Dir mgl64.Vec3
	// ExplosionPower is a large fireball's power (the ghast's 1).
	ExplosionPower int
	// Incendiary is isIncendiary: false for a mob's fireball without
	// mobGriefing.
	Incendiary bool
}

// SpawnHurting adds a hurting projectile of t (small/large fireball, wither
// skull, dragon fireball) and returns its handle.
func SpawnHurting(tx *world.Tx, t ProjectileType, o HurtingOpts) *world.EntityHandle {
	var oh *world.EntityHandle
	pos := o.Pos
	var rot cube.Rotation
	if o.Owner != nil {
		oh = o.Owner.H()
		if pos == (mgl64.Vec3{}) {
			pos = o.Owner.Position()
		}
		rot = o.Owner.Rotation()
	}
	p := newProjectile(t, oh)
	p.incendiary = o.Incendiary
	if o.ExplosionPower > 0 {
		p.explosionPower = float64(o.ExplosionPower)
	}
	vel := normalize(o.Dir).Mul(p.accel)
	h := world.EntitySpawnOpts{Position: pos, Velocity: vel, Rotation: rot}.New(t, projectileConfig{p})
	tx.AddEntity(h)
	return h
}

// SpawnShulkerBullet adds a shulker bullet fired by owner at target
// (ShulkerBullet(level, owner, target, invalidStartAxis)): it starts at the
// middle of owner's box, moving up, and avoids invalidAxis (the axis of the
// shulker's attach face) on its first step.
func SpawnShulkerBullet(tx *world.Tx, owner, target world.Entity, invalidAxis cube.Axis) *world.EntityHandle {
	p := newProjectile(ShulkerBulletType, owner.H())
	p.target = target.H()
	box := owner.H().Type().BBox(owner).Translate(owner.Position())
	pos := box.Min().Add(box.Max()).Mul(0.5)
	p.moveDir, p.hasMoveDir = cube.FaceUp, true
	p.selectNextMoveDirection(tx, pos, &invalidAxis, target)
	h := world.EntitySpawnOpts{Position: pos}.New(ShulkerBulletType, projectileConfig{p})
	tx.AddEntity(h)
	return h
}

// Tick ...
func (p *javaProjectile) Tick(e *entity.Ent, tx *world.Tx) *entity.Movement {
	if p.data == nil || p.removed {
		return nil
	}
	if p.t.kind == kindShulkerBullet {
		p.tickBullet(e, tx)
	} else {
		p.tickHurting(e, tx)
	}
	return nil
}

// tickHurting is AbstractHurtingProjectile.tick.
func (p *javaProjectile) tickHurting(e *entity.Ent, tx *world.Tx) {
	pos, vel := p.data.Pos, p.data.Vel
	// applyInertia: movement + normalize(movement) x accelerationPower, x
	// inertia (0.8 in water).
	inertia := p.inertia()
	if inWater(tx, pos) {
		inertia = 0.8
	}
	vel = vel.Add(normalize(vel).Mul(p.accel)).Mul(inertia)

	hit, ok := p.hitResult(tx, e, pos, vel)
	newPos := pos.Add(vel)
	if ok {
		newPos = hit.Position()
	}
	p.move(e, tx, newPos, vel)
	// Projectile.tick: checkLeftOwner, then Entity.tick (tickCount++).
	p.checkLeftOwner(tx, e, vel)
	p.ticks++
	if p.shouldBurn() {
		// igniteForSeconds(1): the fireball is drawn burning.
		if e.OnFireDuration() < time.Second {
			e.SetOnFire(time.Second)
		}
	}
	if p.belowWorld(tx) {
		p.discard(e)
		return
	}
	if ok && !p.removed {
		p.hitOrDeflect(e, tx, hit)
	}
}

// inertia is getInertia: 0.95, 0.73 for a dangerous skull.
func (p *javaProjectile) inertia() float64 {
	if p.t.kind == kindDangerousSkull {
		return 0.73
	}
	return 0.95
}

// shouldBurn: fireballs burn, skulls and dragon fireballs do not.
func (p *javaProjectile) shouldBurn() bool {
	return p.t.kind == kindSmallFireball || p.t.kind == kindLargeFireball
}

// move sets the position and velocity and tells the viewers.
func (p *javaProjectile) move(e *entity.Ent, tx *world.Tx, pos, vel mgl64.Vec3) {
	old := p.data.Pos
	p.data.Pos, p.data.Vel = pos, vel
	if l := math.Hypot(vel[0], vel[2]); l > 1e-7 || math.Abs(vel[1]) > 1e-7 {
		// ProjectileUtil.rotateTowardsMovement (without the 0.2 lerp,
		// which only smooths what clients draw).
		p.data.Rot = cube.Rotation{mgl64.RadToDeg(math.Atan2(vel[0], vel[2])), mgl64.RadToDeg(math.Atan2(vel[1], l))}
	}
	for _, v := range tx.Viewers(old) {
		v.ViewEntityVelocity(e, vel)
		v.ViewEntityMovement(e, pos, p.data.Rot, false)
	}
}

// belowWorld is Entity.checkBelowWorld: 64 blocks under the world.
func (p *javaProjectile) belowWorld(tx *world.Tx) bool {
	return p.data.Pos[1] < float64(tx.Range().Min()-64)
}

// discard removes the projectile.
func (p *javaProjectile) discard(e *entity.Ent) {
	if !p.removed {
		p.removed = true
		_ = e.Close()
	}
}

// computeMargin is ProjectileUtil.computeMargin: the boxes of the entities
// a projectile may hit grow from 0 to 0.3 over its first ticks.
func computeMargin(ticks int) float64 {
	return math.Max(0, math.Min(0.3, float64(ticks-2)/20))
}

// hitResult is ProjectileUtil.getHitResultOnMoveVector: the first block
// (collision boxes, no fluids) on the move, then the nearest entity the
// move passes through up to there.
func (p *javaProjectile) hitResult(tx *world.Tx, e *entity.Ent, from, vel mgl64.Vec3) (trace.Result, bool) {
	to := from.Add(vel)
	var hit trace.Result
	if vel.LenSqr() > 0 {
		trace.TraverseBlocks(from, to, func(pos cube.Pos) bool {
			if r, ok := trace.BlockIntercept(pos, tx, tx.Block(pos), from, to); ok {
				hit = r
				return false
			}
			return true
		})
	}
	if hit != nil {
		to = hit.Position()
	}
	margin := computeMargin(p.ticks)
	box := e.H().Type().BBox(e).Translate(from).Extend(vel).Grow(1)
	nearest := math.MaxFloat64
	for other := range p.candidates(tx, e, box) {
		bb := other.H().Type().BBox(other).Translate(other.Position()).Grow(margin)
		r, ok := trace.BBoxIntercept(bb, from, to)
		if !ok {
			continue
		}
		if d := r.Position().Sub(from).LenSqr(); d < nearest {
			nearest = d
			hit = entityHit{e: other, pos: r.Position(), bb: bb, face: r.Face()}
		}
	}
	return hit, hit != nil
}

// entityHit is an entity hit at a point.
type entityHit struct {
	e    world.Entity
	pos  mgl64.Vec3
	bb   cube.BBox
	face cube.Face
}

func (h entityHit) BBox() cube.BBox       { return h.bb }
func (h entityHit) Position() mgl64.Vec3  { return h.pos }
func (h entityHit) Face() cube.Face       { return h.face }
func (h entityHit) Entity() world.Entity  { return h.e }

// candidates yields the entities in box the projectile may hit
// (canHitEntity).
func (p *javaProjectile) candidates(tx *world.Tx, e *entity.Ent, box cube.BBox) iter.Seq[world.Entity] {
	return func(yield func(world.Entity) bool) {
		for other := range tx.EntitiesWithin(box.Grow(2)) {
			if other.H() == e.H() {
				continue
			}
			if !other.H().Type().BBox(other).Translate(other.Position()).IntersectsWith(box) {
				continue
			}
			if !p.canHitEntity(tx, other) {
				continue
			}
			if !yield(other) {
				return
			}
		}
	}
}

// canHitEntity is Projectile.canHitEntity (+ the !noPhysics of the
// hurting projectiles and bullets): pickable and alive, and not the owner
// (or what it rides) before the projectile left the owner's box.
func (p *javaProjectile) canHitEntity(tx *world.Tx, other world.Entity) bool {
	if !pickable(other) {
		return false
	}
	if p.owner != nil && !p.leftOwner && other.H() == p.owner {
		return false
	}
	return true
}

// pickable is Entity.canBeHitByProjectile (isAlive && isPickable) for the
// entities Dragonfly has, minus the noPhysics shulker bullets.
func pickable(e world.Entity) bool {
	if l, ok := e.(entity.Living); ok {
		if l.Dead() {
			return false
		}
		if g, ok := e.(interface{ GameMode() world.GameMode }); ok && !g.GameMode().HasCollision() {
			return false
		}
		return true
	}
	switch e.H().Type().EncodeEntity() {
	case LargeFireballType.id, "minecraft:wind_charge_projectile", "minecraft:breeze_wind_charge_projectile",
		"minecraft:ender_crystal", "minecraft:armor_stand", "minecraft:boat", "minecraft:chest_boat",
		"minecraft:minecart", "minecraft:chest_minecart", "minecraft:hopper_minecart", "minecraft:tnt_minecart",
		"minecraft:command_block_minecart":
		return true
	}
	return false
}

// checkLeftOwner is Projectile.checkLeftOwner: the projectile left its
// owner once the owner's box no longer touches its box stretched along the
// move and grown by 1.
func (p *javaProjectile) checkLeftOwner(tx *world.Tx, e *entity.Ent, vel mgl64.Vec3) {
	if p.leftOwner {
		return
	}
	o := p.ownerEntity(tx)
	if o == nil {
		p.leftOwner = true
		return
	}
	box := e.H().Type().BBox(e).Translate(p.data.Pos).Extend(vel).Grow(1)
	p.leftOwner = !o.H().Type().BBox(o).Translate(o.Position()).IntersectsWith(box)
}

// ProjectileDeflector is implemented by entities that turn projectiles back
// (Entity.deflection returning REVERSE: the breeze).
type ProjectileDeflector interface {
	DeflectsProjectile(projectile world.Entity) bool
}

// hitOrDeflect is Projectile.hitTargetOrDeflectSelf.
func (p *javaProjectile) hitOrDeflect(e *entity.Ent, tx *world.Tx, hit trace.Result) {
	if eh, ok := hit.(entityHit); ok {
		if d, ok := eh.e.(ProjectileDeflector); ok && d.DeflectsProjectile(e) {
			if eh.e.H() != p.lastDeflectedBy {
				p.deflectReverse(e, 1)
				p.lastDeflectedBy = eh.e.H()
			}
			return
		}
	}
	p.onHit(e, tx, hit)
}

// deflectReverse is ProjectileDeflection.REVERSE + onDeflection(false).
func (p *javaProjectile) deflectReverse(e *entity.Ent, power float64) {
	rot := 170 + p.r.Float64()*20
	p.data.Vel = p.data.Vel.Mul(-power * 0.5)
	p.data.Rot[0] += rot
	p.onDeflection(false)
}

// onDeflection is AbstractHurtingProjectile.onDeflection: a hit resets the
// acceleration power to 0.1, any other deflection halves it.
func (p *javaProjectile) onDeflection(byAttack bool) {
	if p.t.kind == kindShulkerBullet {
		return
	}
	if byAttack {
		p.accel = 0.1
	} else {
		p.accel *= 0.5
	}
}

// DeflectBy is ProjectileDeflection.AIM_DEFLECT by an attacker: the
// projectile flies where the attacker looks (power 1), the attacker becomes
// its owner and its acceleration power is reset (Player.deflectProjectile,
// Projectile.onRedirectProjectile).
func (p *javaProjectile) DeflectBy(e *entity.Ent, by world.Entity, newOwner *world.EntityHandle) {
	if by != nil {
		p.data.Vel = by.Rotation().Vec3()
	}
	p.owner = newOwner
	p.onDeflection(true)
	for _, v := range e.Tx().Viewers(p.data.Pos) {
		v.ViewEntityVelocity(e, p.data.Vel)
	}
}

// Hurt is what a hit on the projectile does: a large fireball (the only
// redirectable_projectile here) is sent where the attacker looks
// (Player.deflectProjectile, or Projectile.onRedirectProjectile with the
// shooter of the projectile that hit it); a shulker bullet is destroyed
// (ShulkerBullet.hurtServer); the others ignore it.
func (p *javaProjectile) Hurt(e *entity.Ent, _ float64, src world.DamageSource) (float64, bool) {
	if p.removed {
		return 0, false
	}
	switch p.t.kind {
	case kindLargeFireball:
		var by world.Entity
		switch s := src.(type) {
		case entity.AttackDamageSource:
			by = s.Attacker
		case entity.ProjectileDamageSource:
			by = s.Owner
		case FireballDamageSource:
			by = s.Owner
		default:
			return 0, false
		}
		var oh *world.EntityHandle
		if by != nil {
			oh = by.H()
		}
		p.DeflectBy(e, by, oh)
		return 0, true
	case kindShulkerBullet:
		tx := e.Tx()
		tx.PlaySound(p.data.Pos, entitySound{event: "hurt", id: ShulkerBulletType.id})
		p.discard(e)
		return 0, true
	}
	return 0, false
}

// onHit dispatches a hit (Projectile.onHit and the overrides).
func (p *javaProjectile) onHit(e *entity.Ent, tx *world.Tx, hit trace.Result) {
	if eh, ok := hit.(entityHit); ok {
		// A redirectable projectile hit by this one flies where this one's
		// owner looks (onRedirectProjectile; the shulker bullet uses its
		// own momentum, which a large fireball ignores as it accelerates).
		if other, ok := eh.e.(*entity.Ent); ok {
			if op, ok := other.Behaviour().(*javaProjectile); ok && op.t.kind == kindLargeFireball {
				if p.t.kind == kindShulkerBullet {
					op.data.Vel = normalize(p.data.Vel)
					op.owner = p.owner
					op.onDeflection(true)
				} else {
					op.DeflectBy(other, p.ownerEntity(tx), p.owner)
				}
			}
		}
	}
	switch p.t.kind {
	case kindSmallFireball:
		p.smallFireballHit(e, tx, hit)
	case kindLargeFireball:
		p.largeFireballHit(e, tx, hit)
	case kindWitherSkull, kindDangerousSkull:
		p.skullHit(e, tx, hit)
	case kindDragonFireball:
		p.dragonFireballHit(e, tx, hit)
	case kindShulkerBullet:
		p.bulletHit(e, tx, hit)
	}
}

// normalize is Vec3.normalize: zero for a vector shorter than 1e-5.
func normalize(v mgl64.Vec3) mgl64.Vec3 {
	l := v.Len()
	if l < 1e-5 {
		return mgl64.Vec3{}
	}
	return v.Mul(1 / l)
}

// inWater reports whether pos is in water (Entity.isInWater, by the block
// at the position).
func inWater(tx *world.Tx, pos mgl64.Vec3) bool {
	l, ok := tx.Liquid(cube.PosFromVec3(pos))
	if !ok {
		return false
	}
	_, water := l.(interface{ WaterBlock() })
	if water {
		return true
	}
	n, _ := l.EncodeBlock()
	return n == "minecraft:water" || n == "minecraft:flowing_water"
}
