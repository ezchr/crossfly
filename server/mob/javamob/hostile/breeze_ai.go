//go:build crossfly_full

package hostile

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob/jbrain"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// BreezePose is the Java pose of a breeze (STANDING, SLIDING, SHOOTING,
// INHALING, LONG_JUMPING). Bedrock gets SHOOTING and the jump as its actor
// flags (javasession maps them back).
type BreezePose uint8

const (
	BreezeStanding BreezePose = iota
	BreezeSliding
	BreezeShooting
	BreezeInhaling
	BreezeLongJumping
)

// Breeze constants (Breeze, Shoot, LongJump).
const (
	BreezeHealth           = 30
	BreezeSpeed            = 0.63
	BreezeFollowRange      = 24
	BreezeAttack           = 3
	BreezeXP               = 10
	breezeShootRangeSq     = 256
	breezeShootDelay       = 15
	breezeShootRecover     = 4
	breezeShootCooldown    = 10
	breezeJumpCooldown     = 10
	breezeJumpHurtCooldown = 2
	breezeInhaleTicks      = 10
	breezeMaxJumpVelocity  = 0.058333334 // per point of follow range
	breezeMaxHeadYRot      = 30
	breezeHeadRotSpeed     = 25
)

// breezeJumpAngles is LongJump.ALLOWED_ANGLES.
var breezeJumpAngles = []int{40, 55, 60, 75, 80}

// BreezeBody is what the breeze behaviours need from the breeze.
type BreezeBody interface {
	Tx() *world.Tx
	Pos() mgl64.Vec3
	H() *world.EntityHandle
	OnGround() bool
	InWater() bool
	InLava() bool
	Riding() bool
	// DeepInFloatable is isInFluidDeeperThan(fluidJumpThreshold, #entity_floatable).
	DeepInFloatable() bool
	Levitating() bool
	SetBreezePose(p BreezePose)
	BreezePose() BreezePose
	Sound(name string, volume, pitch float64)
	// LookAtEyes is lookAt(Anchor.EYES, pos).
	LookAtEyes(pos mgl64.Vec3)
	// FiringY is Breeze.getFiringYPosition: y + height/2 + 0.3.
	FiringY() float64
	FollowRange() float64
	Gravity() float64
	// JumpBoost is getJumpBoostPower (0 without the effect).
	JumpBoost() float64
	BodyYaw() float64
	SetYaw(float64)
	SetVelocity(mgl64.Vec3)
	// SetDiscardFriction is setDiscardFriction.
	SetDiscardFriction(bool)
	// ShootWindCharge spawns a breeze wind charge aimed along dir with
	// speed 0.7 and the given inaccuracy (Projectile.spawnProjectileUsingShoot).
	ShootWindCharge(dir mgl64.Vec3, inaccuracy float64)
	// LineOfSightTo is BreezeUtil.hasLineOfSight: a clear collider line from
	// the feet within max(50, follow range).
	LineOfSightTo(target mgl64.Vec3) bool
	// SnapToSurface is LongJump.snapToSurface: the block above the first
	// collider within 10 below (else above) the point.
	SnapToSurface(p mgl64.Vec3) (cube.Pos, bool)
	// CanJumpFrom is LongJump.canJumpFromCurrentPosition: not on honey, 4
	// blocks of air or water above.
	CanJumpFrom() bool
	// DangerousBlock is EntityType.isBlockDangerous of the block at pos.
	DangerousBlock(pos cube.Pos) bool
	// RandomPosAway is DefaultRandomPos.getPosAway(breeze, 5, 5, from).
	RandomPosAway(from mgl64.Vec3) (mgl64.Vec3, bool)
}

// breezeRandomBehind is BreezeUtil.randomPointBehindTarget: 4..8 blocks
// behind the way the enemy looks, ±45° (Gaussian).
func breezeRandomBehind(pos mgl64.Vec3, headYaw float64, r jbrain.Random, gaussian float64) mgl64.Vec3 {
	view := headYaw + 180 + gaussian*90/2
	dist := lerp(float64(r.Float32()), 4, 8)
	return pos.Add(directionFromRotation(0, view).Mul(dist))
}

// directionFromRotation is Vec3.directionFromRotation(xRot, yRot).
func directionFromRotation(xRot, yRot float64) mgl64.Vec3 {
	return viewVector(yRot, xRot)
}

// breezeJumpVector is LongJumpUtil.calculateJumpVectorForAngle without the
// collision check (the breeze passes false).
func breezeJumpVector(from, target mgl64.Vec3, maxVelocity float64, angle int, gravity float64) (mgl64.Vec3, bool) {
	plane := mgl64.Vec3{target[0] - from[0], 0, target[2] - from[2]}
	if plane.Len() != 0 {
		plane = plane.Normalize().Mul(0.5)
	}
	dir := target.Sub(plane).Sub(from)
	a := float64(angle) * math.Pi / 180
	xzAng := math.Atan2(dir[2], dir[0])
	r2 := dir[0]*dir[0] + dir[2]*dir[2]
	r := math.Sqrt(r2)
	y := dir[1]
	v0sqr := r2 * gravity / (r*math.Sin(2*a) - 2*y*math.Pow(math.Cos(a), 2))
	if v0sqr < 0 || math.IsNaN(v0sqr) || math.IsInf(v0sqr, 0) {
		return mgl64.Vec3{}, false
	}
	v0 := math.Sqrt(v0sqr)
	if v0 > maxVelocity {
		return mgl64.Vec3{}, false
	}
	v0r := v0 * math.Cos(a)
	v0y := v0 * math.Sin(a)
	return mgl64.Vec3{v0r * math.Cos(xzAng), v0y, v0r * math.Sin(xzAng)}.Mul(float64(float32(0.95))), true
}

// attackTargetPos returns the attack target and its position.
func attackTargetPos(c *jbrain.Ctx) (world.Entity, mgl64.Vec3, bool) {
	h, _ := jbrain.Get(c.Mem, jbrain.MemAttackTarget)
	p, e, ok := entityPos(c.Tx, h)
	return e, p, ok
}

// shootTask is Shoot: standing, with the target within 16 blocks, inhale for
// 15 ticks, fire a wind charge (inaccuracy 5 - 4·difficulty), recover for 4
// ticks; 10 ticks of cooldown after.
type shootTask[E BreezeBody] struct{ jbrain.TaskBase[E] }

func (shootTask[E]) CanStart(c *jbrain.Ctx, e E) bool {
	if e.BreezePose() != BreezeStanding {
		return false
	}
	_, p, ok := attackTargetPos(c)
	if !ok {
		return false
	}
	if p.Sub(e.Pos()).LenSqr() >= breezeShootRangeSq {
		c.Mem.Erase(jbrain.MemBreezeShoot.ID())
		return false
	}
	return true
}

func (shootTask[E]) CanStillUse(c *jbrain.Ctx, _ E) bool {
	return c.Mem.Has(jbrain.MemAttackTarget.ID()) && c.Mem.Has(jbrain.MemBreezeShoot.ID())
}

func (shootTask[E]) Start(c *jbrain.Ctx, e E) {
	if c.Mem.Has(jbrain.MemAttackTarget.ID()) {
		e.SetBreezePose(BreezeShooting)
	}
	jbrain.SetWithExpiry(c.Mem, jbrain.MemBreezeShootCharging, jbrain.Unit{}, breezeShootDelay)
	e.Sound("entity.breeze.inhale", 1, 1)
}

func (shootTask[E]) Stop(c *jbrain.Ctx, e E) {
	if e.BreezePose() == BreezeShooting {
		e.SetBreezePose(BreezeStanding)
	}
	jbrain.SetWithExpiry(c.Mem, jbrain.MemBreezeShootCooldown, jbrain.Unit{}, breezeShootCooldown)
	c.Mem.Erase(jbrain.MemBreezeShoot.ID())
}

func (shootTask[E]) Tick(c *jbrain.Ctx, e E) {
	t, p, ok := attackTargetPos(c)
	if !ok {
		return
	}
	e.LookAtEyes(p)
	if c.Mem.Has(jbrain.MemBreezeShootCharging.ID()) || c.Mem.Has(jbrain.MemBreezeShootRecovering.ID()) {
		return
	}
	jbrain.SetWithExpiry(c.Mem, jbrain.MemBreezeShootRecovering, jbrain.Unit{}, breezeShootRecover)
	heightPart := 0.3
	if r, ok := t.(interface{ Riding() bool }); ok && r.Riding() {
		heightPart = 0.8
	}
	box := targetBox(t)
	ty := box.Min()[1] + (box.Max()[1]-box.Min()[1])*heightPart
	pos := e.Pos()
	dir := mgl64.Vec3{p[0] - pos[0], ty - e.FiringY(), p[2] - pos[2]}
	e.ShootWindCharge(dir, float64(5-difficultyID(c.Tx)*4))
	e.Sound("entity.breeze.shoot", 1.5, 1)
}

func newBreezeShoot[E BreezeBody]() jbrain.Control[E] {
	reqs := []jbrain.Req{jbrain.Present(jbrain.MemAttackTarget), jbrain.Absent(jbrain.MemBreezeShootCooldown), jbrain.Absent(jbrain.MemBreezeShootCharging),
		jbrain.Absent(jbrain.MemBreezeShootRecovering), jbrain.Present(jbrain.MemBreezeShoot), jbrain.Absent(jbrain.MemWalkTarget), jbrain.Absent(jbrain.MemBreezeJumpTarget)}
	d := breezeShootDelay + 1 + breezeShootRecover
	return jbrain.NewBehavior[E]("Shoot", reqs, d, d, shootTask[E]{})
}

// newBreezeShootWhenStuck is ShootWhenStuck: riding, in water or levitating
// it can only shoot (BREEZE_SHOOT for 60 ticks).
func newBreezeShootWhenStuck[E BreezeBody]() jbrain.Control[E] {
	reqs := []jbrain.Req{jbrain.Present(jbrain.MemAttackTarget), jbrain.Absent(jbrain.MemBreezeJumpInhaling), jbrain.Absent(jbrain.MemBreezeJumpTarget),
		jbrain.Absent(jbrain.MemWalkTarget), jbrain.Absent(jbrain.MemBreezeShoot)}
	return jbrain.Trigger("ShootWhenStuck", reqs, func(c *jbrain.Ctx, e E) bool {
		if !e.Riding() && !e.InWater() && !e.Levitating() {
			return false
		}
		jbrain.SetWithExpiry(c.Mem, jbrain.MemBreezeShoot, jbrain.Unit{}, 60)
		return true
	})
}

// newBreezeSlide is Slide: on the ground and standing, pick where to slide:
// away from a target within 4 blocks (if that point is in sight and further
// from it), else behind the target or onto the 4..8 ring around it.
func newBreezeSlide[E BreezeBody](gaussian func(jbrain.Random) float64) jbrain.Control[E] {
	reqs := []jbrain.Req{jbrain.Present(jbrain.MemAttackTarget), jbrain.Absent(jbrain.MemWalkTarget), jbrain.Absent(jbrain.MemBreezeJumpCooldown), jbrain.Absent(jbrain.MemBreezeShoot)}
	return jbrain.Trigger("Slide", reqs, func(c *jbrain.Ctx, e E) bool {
		if !e.OnGround() || e.InWater() || e.BreezePose() != BreezeStanding {
			return false
		}
		t, tp, ok := attackTargetPos(c)
		if !ok {
			return true
		}
		pos := e.Pos()
		var dest mgl64.Vec3
		found := false
		centre := blockPos(pos).Vec3Centre()
		if closerThanXZY(centre, tp, 4, 10) {
			if p, ok := e.RandomPosAway(tp); ok && e.LineOfSightTo(p) && p.Sub(tp).LenSqr() > pos.Sub(tp).LenSqr() {
				dest, found = p, true
			}
		}
		if !found {
			if c.Rand.IntN(2) == 0 {
				dest = breezeRandomBehind(tp, headYawOf(t), c.Rand, gaussian(c.Rand))
			} else {
				d := tp.Sub(pos)
				dist := d.Len() - lerp(c.Rand.Float64(), 8, 4)
				if d.Len() > 0 {
					dest = pos.Add(d.Normalize().Mul(dist))
				} else {
					dest = pos
				}
			}
		}
		jbrain.Set(c.Mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: jbrain.TrackBlock(jbrain.Containing(dest)), Speed: 0.6, CloseEnough: 1})
		return true
	})
}

// headYawOf returns an entity's head yaw.
func headYawOf(e world.Entity) float64 {
	if h, ok := e.(interface{ HeadYaw() float64 }); ok {
		return h.HeadYaw()
	}
	return e.Rotation().Yaw()
}

// longJumpTask is LongJump: pick a landing spot behind the target in sight
// (not on a dangerous block), inhale 10 ticks, leap on a ballistic arc (the
// first of the shuffled angles 40..80 that the follow-range-limited speed
// reaches), land, and shoot next (BREEZE_SHOOT 100 ticks). Jump cooldown 10
// ticks, 2 when hurt.
type longJumpTask[E BreezeBody] struct {
	jbrain.TaskBase[E]
	gaussian func(jbrain.Random) float64
}

func (t longJumpTask[E]) CanStart(c *jbrain.Ctx, e E) bool {
	if !e.OnGround() && !e.InWater() {
		return false
	}
	if e.DeepInFloatable() || e.InLava() {
		return false
	}
	if c.Mem.Has(jbrain.MemBreezeJumpTarget.ID()) {
		return true
	}
	target, tp, ok := attackTargetPos(c)
	if !ok {
		return false
	}
	pos := e.Pos()
	if tp.Sub(pos).LenSqr() >= e.FollowRange()*e.FollowRange() {
		c.Mem.Erase(jbrain.MemAttackTarget.ID())
		return false
	}
	if tp.Sub(pos).Len()-4 <= 0 || !e.CanJumpFrom() {
		return false
	}
	land, ok := e.SnapToSurface(breezeRandomBehind(tp, headYawOf(target), c.Rand, t.gaussian(c.Rand)))
	if !ok || e.DangerousBlock(land.Side(cube.FaceDown)) {
		return false
	}
	if !e.LineOfSightTo(land.Vec3Centre()) && !e.LineOfSightTo(land.Add(cube.Pos{0, 4, 0}).Vec3Centre()) {
		return false
	}
	jbrain.Set(c.Mem, jbrain.MemBreezeJumpTarget, land)
	return true
}

func (longJumpTask[E]) CanStillUse(c *jbrain.Ctx, e E) bool {
	return e.BreezePose() != BreezeStanding && !c.Mem.Has(jbrain.MemBreezeJumpCooldown.ID())
}

func (longJumpTask[E]) Start(c *jbrain.Ctx, e E) {
	if !c.Mem.Has(jbrain.MemBreezeJumpInhaling.ID()) {
		jbrain.SetWithExpiry(c.Mem, jbrain.MemBreezeJumpInhaling, jbrain.Unit{}, breezeInhaleTicks)
	}
	e.SetBreezePose(BreezeInhaling)
	e.Sound("entity.breeze.charge", 1, 1)
	if p, ok := jbrain.Get(c.Mem, jbrain.MemBreezeJumpTarget); ok {
		e.LookAtEyes(p.Vec3Centre())
	}
}

func (longJumpTask[E]) Tick(c *jbrain.Ctx, e E) {
	inWater := e.InWater()
	if !inWater && c.Mem.Has(jbrain.MemBreezeLeavingWater.ID()) {
		c.Mem.Erase(jbrain.MemBreezeLeavingWater.ID())
	}
	switch {
	case !c.Mem.Has(jbrain.MemBreezeJumpInhaling.ID()) && e.BreezePose() == BreezeInhaling:
		land, ok := jbrain.Get(c.Mem, jbrain.MemBreezeJumpTarget)
		var v mgl64.Vec3
		found := false
		if ok {
			v, found = breezeOptimalJump(e, c.Rand, mgl64.Vec3{float64(land[0]) + 0.5, float64(land[1]), float64(land[2]) + 0.5})
		}
		if !found {
			e.SetBreezePose(BreezeStanding)
			return
		}
		if inWater {
			jbrain.Set(c.Mem, jbrain.MemBreezeLeavingWater, jbrain.Unit{})
		}
		e.Sound("entity.breeze.jump", 1, 1)
		e.SetBreezePose(BreezeLongJumping)
		e.SetYaw(e.BodyYaw())
		e.SetDiscardFriction(true)
		e.SetVelocity(v)
	case e.BreezePose() == BreezeLongJumping && (e.OnGround() || (inWater && !c.Mem.Has(jbrain.MemBreezeLeavingWater.ID()))):
		e.Sound("entity.breeze.land", 1, 1)
		e.SetBreezePose(BreezeStanding)
		e.SetDiscardFriction(false)
		cd := int64(breezeJumpCooldown)
		if c.Mem.Has(jbrain.MemHurtBy.ID()) {
			cd = breezeJumpHurtCooldown
		}
		jbrain.SetWithExpiry(c.Mem, jbrain.MemBreezeJumpCooldown, jbrain.Unit{}, cd)
		jbrain.SetWithExpiry(c.Mem, jbrain.MemBreezeShoot, jbrain.Unit{}, 100)
	}
}

func (longJumpTask[E]) Stop(c *jbrain.Ctx, e E) {
	if p := e.BreezePose(); p == BreezeLongJumping || p == BreezeInhaling {
		e.SetBreezePose(BreezeStanding)
	}
	c.Mem.Erase(jbrain.MemBreezeJumpTarget.ID())
	c.Mem.Erase(jbrain.MemBreezeJumpInhaling.ID())
	c.Mem.Erase(jbrain.MemBreezeLeavingWater.ID())
}

// breezeOptimalJump is LongJump.calculateOptimalJumpVector: the shuffled
// angles, the first that works, plus Jump Boost along the jump's y.
func breezeOptimalJump[E BreezeBody](e E, r jbrain.Random, target mgl64.Vec3) (mgl64.Vec3, bool) {
	angles := append([]int(nil), breezeJumpAngles...)
	for i := len(angles) - 1; i > 0; i-- { // Util.shuffledCopy (Fisher-Yates)
		j := r.IntN(i + 1)
		angles[i], angles[j] = angles[j], angles[i]
	}
	maxV := float64(float32(breezeMaxJumpVelocity)) * e.FollowRange()
	for _, a := range angles {
		v, ok := breezeJumpVector(e.Pos(), target, maxV, a, e.Gravity())
		if !ok {
			continue
		}
		if b := e.JumpBoost(); b > 0 {
			v = v.Add(mgl64.Vec3{0, v.Normalize()[1] * b})
		}
		return v, true
	}
	return mgl64.Vec3{}, false
}

func newBreezeLongJump[E BreezeBody](gaussian func(jbrain.Random) float64) jbrain.Control[E] {
	reqs := []jbrain.Req{jbrain.Present(jbrain.MemAttackTarget), jbrain.Absent(jbrain.MemBreezeJumpCooldown), jbrain.Reg(jbrain.MemBreezeJumpInhaling),
		jbrain.Reg(jbrain.MemBreezeJumpTarget), jbrain.Absent(jbrain.MemBreezeShoot), jbrain.Absent(jbrain.MemWalkTarget), jbrain.Reg(jbrain.MemBreezeLeavingWater)}
	return jbrain.NewBehavior[E]("LongJump", reqs, 200, 200, longJumpTask[E]{gaussian: gaussian})
}

// BreezeCanAttack is Breeze.canAttack: only players and iron golems.
func BreezeCanAttack(id string) bool { return id == "minecraft:player" || id == "minecraft:iron_golem" }

// BreezeInvulnerableTo is Breeze.isInvulnerableTo: breezes do not hurt each
// other.
func BreezeInvulnerableTo(attackerID string) bool { return attackerID == "minecraft:breeze" }

// BreezeDeflects is Breeze.deflection: every projectile but wind charges
// (its own and players') bounces back.
func BreezeDeflects(projectileID string) bool {
	return projectileID != "minecraft:breeze_wind_charge" && projectileID != "minecraft:wind_charge"
}
