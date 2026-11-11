//go:build crossfly_full

package javamob

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Behaviour is the Go side of one Java mob class (Zombie, Creeper, ...): Type.New returns one per mob, and it keeps
// the class's own fields. RegisterGoals is required; a behaviour implements the optional interfaces below for the
// methods its class overrides.
type Behaviour interface {
	// RegisterGoals adds the goals and target goals (Mob.registerGoals). It runs once when the mob is set up, after
	// the attributes, so it may read them.
	RegisterGoals(m *Mob)
}

// The optional hooks of a Behaviour, each named after the Java method it stands for.
type (
	// Ticker runs at the start of the mob's tick (an override of tick() that does its work before super.tick()).
	Ticker interface{ Tick(m *Mob) }
	// PostTicker runs at the end of the mob's tick (work after super.tick()).
	PostTicker interface{ PostTick(m *Mob) }
	// AiStepper runs at the start of aiStep (work before super.aiStep()).
	AiStepper interface{ AiStep(m *Mob) }
	// ServerAiStepper is Mob.customServerAiStep: after the goals and navigation, before the controls.
	ServerAiStepper interface{ CustomServerAiStep(m *Mob) }
	// SpawnFinalizer is Mob.finalizeSpawn: equipment, babies, variants. It runs after the base finalizeSpawn
	// (the random follow range bonus and the left-handed roll).
	SpawnFinalizer interface {
		FinalizeSpawn(m *Mob, reason SpawnReason)
	}
	// DamageFilter sees damage before it is dealt (the start of an overridden hurtServer). Set d.Cancel to drop it.
	DamageFilter interface{ FilterDamage(m *Mob, d *mob.Damage) }
	// HurtListener hears about damage dealt (the end of an overridden hurtServer / actuallyHurt).
	HurtListener interface {
		OnHurt(m *Mob, dealt float64, src world.DamageSource)
	}
	// DeathListener runs when the mob dies (LivingEntity.die), before the loot drops.
	DeathListener interface {
		OnDeath(m *Mob, src world.DamageSource)
	}
	// DeathLooter is Mob.dropCustomDeathLoot: drops besides the loot table and the equipment.
	DeathLooter interface {
		DropCustomDeathLoot(m *Mob, src world.DamageSource, byPlayer bool)
	}
	// Persister saves the behaviour's own fields (addAdditionalSaveData / readAdditionalSaveData). EncodeNBT runs
	// outside any transaction: write the behaviour's fields only.
	Persister interface {
		EncodeNBT(data map[string]any)
		DecodeNBT(m *Mob, data map[string]any)
	}
	// Attacker replaces Mob.doHurtTarget.
	Attacker interface {
		DoHurtTarget(m *Mob, target world.Entity) bool
	}
	// SunSensitive is the isSunSensitive check before burning in daylight (Zombie: true unless it is a baby
	// converting...). Without it a type with BurnsInDaylight always burns.
	SunSensitive interface{ SunSensitive(m *Mob) bool }
	// WaterSensitive is LivingEntity.isSensitiveToWater (enderman): hurt by water and rain.
	WaterSensitive interface{ SensitiveToWater(m *Mob) bool }
	// FarAwayRemover is Mob.removeWhenFarAway.
	FarAwayRemover interface {
		RemoveWhenFarAway(m *Mob, distSqr float64) bool
	}
	// Interactor is Mob.mobInteract: a player using the mob.
	Interactor interface {
		MobInteract(m *Mob, user item.User, held item.Stack) (item.Stack, bool)
	}
	// TargetFilter is an override of setTarget/canAttack: whether the mob may target e at all.
	TargetFilter interface {
		CanAttack(m *Mob, e world.Entity) bool
	}
	// BabySetter is Mob.setBaby for ageable mobs (attributes and dimensions of a baby).
	BabySetter interface{ SetBaby(m *Mob, baby bool) }
	// Climber is LivingEntity.onClimbable for mobs that climb without a ladder (Spider.onClimbable: isClimbing).
	Climber interface{ OnClimbable(m *Mob) bool }
	// Jumper overrides LivingEntity.jumpFromGround and jumpInLiquid (MagmaCube). Returning false runs the
	// base jump.
	Jumper interface {
		JumpFromGround(m *Mob) bool
		JumpInLiquid(m *Mob, lava bool) bool
	}
	// MaxHeadYRot is Mob.getMaxHeadYRot (75 by default), HeadRotSpeed getHeadRotSpeed (10), MaxHeadXRot
	// getMaxHeadXRot (40).
	HeadLimiter interface {
		MaxHeadYRot() float64
		MaxHeadXRot() float64
		HeadRotSpeed() float64
	}
	// GroupFinalizer is finalizeSpawn with its SpawnGroupData: group is what the previous member of the pack returned
	// (nil for the first), the result goes to the next one. It runs after SpawnFinalizer.
	GroupFinalizer interface {
		FinalizeSpawnGroup(m *Mob, reason SpawnReason, group any) any
	}
	// GroupSizer is Mob.getMaxSpawnClusterSize (4) and isMaxGroupSizeReached (false) for natural spawning.
	GroupSizer interface {
		MaxSpawnClusterSize() int
		IsMaxGroupSizeReached(n int) bool
	}
	// XPModifier changes the base experience reward (Zombie: 2.5 times for a baby) before the equipment bonus.
	XPModifier interface{ XPReward(m *Mob, base int) int }
	// FallListener hears about fall damage being worked out (an override of causeFallDamage: a creeper swells).
	FallListener interface {
		OnFall(m *Mob, distance float64)
	}
	// MaxFallDistancer replaces Mob.getMaxFallDistance (Creeper).
	MaxFallDistancer interface{ MaxFallDistance(m *Mob) int }
	// CobwebIgnorer mobs are not slowed by cobwebs (Spider.makeStuckInBlock).
	CobwebIgnorer interface{ IgnoresCobwebs() bool }
	// TargetListener hears about target changes (an override of Mob.setTarget: the enderman's stare and speed).
	TargetListener interface {
		OnTargetChanged(m *Mob, old, new world.Entity)
	}
	// Brain is a Brain (sensors, memories, activities, behaviours) for brain-driven mobs: villagers, piglins,
	// wardens. It ticks where Mob.customServerAiStep would tick it. The brain framework itself lives in its own
	// package; the base only calls it.
	Brain interface{ Tick(m *Mob) }
)

// Mob is a Java Edition mob: a *mob.Mob (the entity the clients see, its health, effects, equipment and NBT) run
// by the Java state of package javamob. Like *mob.Mob it is only valid in the transaction it was opened in.
type Mob struct {
	*mob.Mob
	s *state
}

// Of returns the Java mob behind e, if e is one.
func Of(e world.Entity) (*Mob, bool) {
	bm, ok := mob.Of(e)
	if !ok {
		return nil, false
	}
	return wrap(bm)
}

// OfHandle opens the Java mob behind h in tx.
func OfHandle(tx *world.Tx, h *world.EntityHandle) (*Mob, bool) {
	if h == nil {
		return nil, false
	}
	e, ok := h.Entity(tx)
	if !ok {
		return nil, false
	}
	return Of(e)
}

func wrap(bm *mob.Mob) (*Mob, bool) {
	st, ok := mob.ComponentOf[*state](bm)
	if !ok {
		return nil, false
	}
	if c := st.cur; c != nil && c.Mob == bm {
		return c, true
	}
	m := &Mob{Mob: bm, s: st}
	st.cur = m
	return m, true
}

// JavaType returns the mob's Java type.
func (m *Mob) JavaType() *Type { return m.s.t }

// Behaviour returns the mob's behaviour (its class). Type-assert it to the mob's own type.
func (m *Mob) Behaviour() Behaviour { return m.s.b }

// Attributes returns the mob's Java attributes.
func (m *Mob) Attributes() *Attributes { return m.s.attrs }

// Attr is the value of an attribute (getAttributeValue).
func (m *Mob) Attr(a Attribute) float64 { return m.s.attrs.Value(a) }

// Goals returns the goal selector, Targets the target selector.
func (m *Mob) Goals() *GoalSelector   { return &m.s.goals }
func (m *Mob) Targets() *GoalSelector { return &m.s.targets }

// Navigation returns the path navigation.
func (m *Mob) Navigation() *path.Navigation { return m.s.nav }

// SetNavigation replaces the navigation (createNavigation of a mob that needs another kind).
func (m *Mob) SetNavigation(n *path.Navigation) { m.s.nav = n }

// LookControl, MoveControl and JumpControl return the controls.
func (m *Mob) LookControl() *LookControl { return &m.s.look }
func (m *Mob) MoveControl() *MoveControl { return &m.s.move }
func (m *Mob) JumpControl() *JumpControl { return &m.s.jump }

// Sensing returns the line of sight cache (Mob.getSensing).
func (m *Mob) Sensing() *Sensing { return &m.s.sensing }

// SetBrain gives the mob a brain, ticked in customServerAiStep.
func (m *Mob) SetBrain(b Brain) { m.s.brain = b }

// Brain returns the mob's brain, or nil.
func (m *Mob) Brain() Brain { return m.s.brain }

// Rand is the mob's random source (Entity.random).
func (m *Mob) Rand() *rand.Rand { return m.Mob.Rand() }

// TickCount is Entity.tickCount.
func (m *Mob) TickCount() int { return m.s.tickCount }

// Width and Height are getBbWidth and getBbHeight (the dimensions with the baby and scale factors).
func (m *Mob) Width() float64  { return m.s.width }
func (m *Mob) Height() float64 { return m.s.height }

// EyeHeight is getEyeHeight, EyeY getEyeY.
func (m *Mob) EyeHeight() float64 { return m.s.eyeHeight }
func (m *Mob) EyeY() float64      { return m.Position()[1] + m.s.eyeHeight }

// EyePosition is getEyePosition.
func (m *Mob) EyePosition() mgl64.Vec3 { return m.Position().Add(mgl64.Vec3{0, m.s.eyeHeight}) }

// BBox is the collision box in the world (getBoundingBox).
func (m *Mob) BBox() cube.BBox { return m.boxAt(m.Position()) }

func (m *Mob) boxAt(p mgl64.Vec3) cube.BBox {
	w := m.s.width / 2
	return cube.Box(p[0]-w, p[1], p[2]-w, p[0]+w, p[1]+m.s.height, p[2]+w)
}

// SetDimensions changes the mob's dimensions (refreshDimensions: a baby, a slime of another size). The eye height
// scales with the height.
func (m *Mob) SetDimensions(width, height float64) {
	if height > 0 && m.s.height > 0 {
		m.s.eyeHeight *= height / m.s.height
	}
	m.s.width, m.s.height = width, height
	m.UpdateMeta(func(meta *mob.Meta) { meta.Width, meta.Height = width, height })
}

// OnGround is onGround.
func (m *Mob) OnGround() bool { return m.s.onGround }

// HorizontalCollision, VerticalCollision and MinorHorizontalCollision report how the last move was stopped.
func (m *Mob) HorizontalCollision() bool      { return m.s.horizontalCollision }
func (m *Mob) VerticalCollision() bool        { return m.s.verticalCollision }
func (m *Mob) MinorHorizontalCollision() bool { return m.s.minorHorizontalCollision }

// FallDistance is fallDistance; ResetFallDistance zeroes it.
func (m *Mob) FallDistance() float64 { return m.s.fallDistance }
func (m *Mob) ResetFallDistance()    { m.s.fallDistance = 0; m.Mob.ResetFallDistance() }

// InWater is isInWater (wasTouchingWater), InLava isInLava, InLiquid isInLiquid, UnderWater isUnderWater.
func (m *Mob) InWater() bool          { return m.s.inWater }
func (m *Mob) InLava() bool           { return m.s.lavaHeight > 0 }
func (m *Mob) InLiquid() bool         { return m.s.inWater || m.s.lavaHeight > 0 }
func (m *Mob) UnderWater() bool       { return m.s.eyeInWater }
func (m *Mob) InFloatableFluid() bool { return m.s.inWater }

// FluidHeight is getFluidHeight of water or lava.
func (m *Mob) FluidHeight(lava bool) float64 {
	if lava {
		return m.s.lavaHeight
	}
	return m.s.waterHeight
}

// FluidJumpThreshold is getFluidJumpThreshold: 0.4 for a mob taller than 0.4 (0.85 of its eye height otherwise).
func (m *Mob) FluidJumpThreshold() float64 {
	if m.s.eyeHeight < 0.4 {
		return 0
	}
	return 0.4
}

// InWaterOrRain is isInWaterOrRain.
func (m *Mob) InWaterOrRain() bool { return m.s.inWater || m.inRain() }

func (m *Mob) inRain() bool {
	tx := m.Tx()
	p := m.Position()
	pos := cube.PosFromVec3(p)
	top := cube.PosFromVec3(mgl64.Vec3{p[0], m.BBox().Max()[1], p[2]})
	return tx.RainingAt(pos) || tx.RainingAt(top)
}

// Passenger reports whether the mob rides something.
func (m *Mob) Passenger() bool { return m.Riding() }

// Speed is LivingEntity.getSpeed: the speed the move control set (not the attribute).
func (m *Mob) MoveSpeed() float64 { return m.s.speed }

// SetSpeed is Mob.setSpeed: the speed and the forward input (zza).
func (m *Mob) SetMoveSpeed(v float64) { m.s.speed, m.s.zza = v, v }

// SetZza, SetXxa and SetYya set the forward, strafe and vertical input.
func (m *Mob) SetZza(v float64) { m.s.zza = v }
func (m *Mob) SetXxa(v float64) { m.s.xxa = v }
func (m *Mob) SetYya(v float64) { m.s.yya = v }

// Zza is the forward input.
func (m *Mob) Zza() float64 { return m.s.zza }

// SetJumping is setJumping; Jumping reports it.
func (m *Mob) SetJumping(v bool) { m.s.jumping = v }
func (m *Mob) Jumping() bool     { return m.s.jumping }

// StopInPlace is Mob.stopInPlace.
func (m *Mob) StopInPlace() {
	m.s.nav.Stop()
	m.s.xxa, m.s.yya, m.s.speed, m.s.zza = 0, 0, 0, 0
	m.SetVelocity(mgl64.Vec3{})
}

// MaxUpStep is maxUpStep (the step height attribute).
func (m *Mob) MaxUpStep() float64 { return m.Attr(AttrStepHeight) }

// FollowRange is the follow range attribute's value, FollowRangeBase its base.
func (m *Mob) FollowRange() float64     { return m.Attr(AttrFollowRange) }
func (m *Mob) FollowRangeBase() float64 { return m.s.attrs.Base(AttrFollowRange) }

// SetWantedPosition is getMoveControl().setWantedPosition.
func (m *Mob) SetWantedPosition(x, y, z, speed float64) { m.s.move.SetWantedPosition(x, y, z, speed) }

// PathfindingMalus is Mob.getPathfindingMalus.
func (m *Mob) PathfindingMalus(t path.Type) float32 {
	if m.s.malusSet[t] {
		return m.s.malus[t]
	}
	return t.Malus()
}

// SetPathfindingMalus is Mob.setPathfindingMalus.
func (m *Mob) SetPathfindingMalus(t path.Type, v float32) { m.s.malus[t], m.s.malusSet[t] = v, true }

// MaxFallDistance is Mob.getMaxFallDistance.
func (m *Mob) MaxFallDistance() int {
	if f, ok := m.s.b.(MaxFallDistancer); ok {
		return f.MaxFallDistance(m)
	}
	if m.TargetHandle() == nil {
		return m.comfortableFallDistance(0)
	}
	sacrifice := int(float32(m.Health()) - float32(m.MaxHealth())*0.33)
	sacrifice -= (3 - difficultyID(m.Tx())) * 4
	if sacrifice < 0 {
		sacrifice = 0
	}
	return m.comfortableFallDistance(float64(sacrifice))
}

// ComfortableFallDistance is Mob.getComfortableFallDistance: the safe fall distance plus the damage the mob accepts.
func (m *Mob) ComfortableFallDistance(allowed float64) int { return m.comfortableFallDistance(allowed) }

// SetEyeHeight sets the eye height (dimensions with their own eye height, a baby zombie's).
func (m *Mob) SetEyeHeight(v float64) { m.s.eyeHeight = v }

func (m *Mob) comfortableFallDistance(allowed float64) int {
	return int(m.Attr(AttrSafeFallDistance) + allowed)
}

func difficultyID(tx *world.Tx) int {
	switch tx.World().Difficulty() {
	case world.DifficultyPeaceful:
		return 0
	case world.DifficultyEasy:
		return 1
	case world.DifficultyHard:
		return 3
	}
	return 2
}

// DifficultyID is Difficulty.getId of the mob's world: 0 peaceful, 1 easy, 2 normal, 3 hard.
func (m *Mob) DifficultyID() int { return difficultyID(m.Tx()) }

// HeadYRot is yHeadRot, BodyYRot yBodyRot, YRot getYRot, XRot getXRot (degrees).
func (m *Mob) HeadYRot() float64 { return m.HeadYaw() }
func (m *Mob) BodyYRot() float64 { return m.BodyYaw() }
func (m *Mob) YRot() float64     { return m.Yaw() }
func (m *Mob) XRot() float64     { return m.Pitch() }

// SetHeadYRot, SetBodyYRot, SetYRot and SetXRot set them.
func (m *Mob) SetHeadYRot(v float64) { m.SetHeadYaw(v) }
func (m *Mob) SetBodyYRot(v float64) { m.SetBodyYaw(v) }
func (m *Mob) SetYRot(v float64)     { m.SetYaw(v) }
func (m *Mob) SetXRot(v float64)     { m.SetPitch(v) }

func (m *Mob) headLimits() (yRot, xRot, speed float64) {
	if h, ok := m.s.b.(HeadLimiter); ok {
		return h.MaxHeadYRot(), h.MaxHeadXRot(), h.HeadRotSpeed()
	}
	return 75, 40, 10
}

// MaxHeadYRot is getMaxHeadYRot.
func (m *Mob) MaxHeadYRot() float64 { y, _, _ := m.headLimits(); return y }

// SetAggressive is Mob.setAggressive (Java arms raised, bow drawn).
func (m *Mob) SetAggressive(v bool) {
	if m.Meta().Aggressive != v {
		m.UpdateMeta(func(meta *mob.Meta) { meta.Aggressive = v })
	}
}

// Aggressive is isAggressive.
func (m *Mob) Aggressive() bool { return m.Meta().Aggressive }

// SetBaby is setBaby: the baby flag, its dimensions and the behaviour's own changes (BabySetter).
func (m *Mob) SetBaby(v bool) {
	if m.Baby() == v {
		return
	}
	m.UpdateMeta(func(meta *mob.Meta) { meta.Baby = v })
	scale := 1.0
	if v {
		scale = 0.5
	}
	m.SetDimensions(m.s.t.Width*scale, m.s.t.Height*scale)
	if b, ok := m.s.b.(BabySetter); ok {
		b.SetBaby(m, v)
	}
}

// PersistenceRequired is Mob.isPersistenceRequired; SetPersistenceRequired sets it (a name tag, a picked-up item).
func (m *Mob) PersistenceRequired() bool { return m.s.persistenceRequired }
func (m *Mob) SetPersistenceRequired() {
	m.s.persistenceRequired = true
	m.SetPersistent(true)
}

// RequiresCustomPersistence is Mob.requiresCustomPersistence: riding or leashed.
func (m *Mob) RequiresCustomPersistence() bool { return m.Riding() }

// RemoveWhenFarAway is Mob.removeWhenFarAway (true unless the behaviour says otherwise).
func (m *Mob) RemoveWhenFarAway(distSqr float64) bool {
	if r, ok := m.s.b.(FarAwayRemover); ok {
		return r.RemoveWhenFarAway(m, distSqr)
	}
	return true
}

// NoActionTime is Mob.noActionTime (ticks without being hurt or near a player), ResetNoActionTime zeroes it.
func (m *Mob) NoActionTime() int     { return m.s.noActionTime }
func (m *Mob) ResetNoActionTime()    { m.s.noActionTime = 0; m.ResetInactivity() }
func (m *Mob) SetNoActionTime(v int) { m.s.noActionTime = v }

// CanPickUpLoot is Mob.canPickUpLoot; SetCanPickUpLoot sets it.
func (m *Mob) CanPickUpLoot() bool     { return m.s.canPickUpLoot }
func (m *Mob) SetCanPickUpLoot(v bool) { m.s.canPickUpLoot = v }

// LeftHanded is Mob.isLeftHanded.
func (m *Mob) LeftHanded() bool { return m.s.leftHanded }

// NoAI is Mob.isNoAi; SetNoAI sets it.
func (m *Mob) NoAI() bool { return m.s.noAI }
func (m *Mob) SetNoAI(v bool) {
	m.s.noAI = v
	m.UpdateMeta(func(meta *mob.Meta) { meta.NoAI = v })
}

// SpawnReason is the reason the mob was finalized with.
func (m *Mob) SpawnReason() SpawnReason { return m.s.reason }

// HurtTime is LivingEntity.hurtTime (10 after a hit, counting down).
func (m *Mob) HurtTime() int { return m.s.hurtTime }

// LastHurtByMobTimestamp is the TickCount the mob was last hurt by an entity (getLastHurtByMobTimestamp).
func (m *Mob) LastHurtByMobTimestamp() int { return m.s.lastHurtByMobTime }

// LastHurtByMob returns the living entity that last hurt the mob within 100 ticks (getLastHurtByMob).
func (m *Mob) LastHurtByMob() (world.Entity, bool) {
	e, _, ok := m.LastHurtBy()
	if !ok || m.s.tickCount-m.s.lastHurtByMobTime > 100 {
		return nil, false
	}
	return e, true
}

// SetTarget is Mob.setTarget: nil clears it. A target the mob may not attack is dropped (asValidTarget: creative
// and spectator players; TargetFilter).
func (m *Mob) SetTarget(e world.Entity) {
	if e != nil && !m.validTarget(e) {
		e = nil
	}
	m.Mob.SetTarget(e)
}

// Target returns the target if it is still valid (Mob.getTarget).
func (m *Mob) Target() (world.Entity, bool) {
	e, ok := m.Mob.Target()
	if !ok {
		return nil, false
	}
	if !m.validTarget(e) {
		return nil, false
	}
	return e, true
}

// Swing swings the main arm (swingForAttack): viewers see the arm swing.
func (m *Mob) Swing() {
	for _, v := range m.Tx().Viewers(m.Position()) {
		v.ViewEntityAction(m, entity.SwingArmAction{})
	}
}

// StartUsingItem is startUsingItem: draw the bow, eat. Viewers see the item in use.
func (m *Mob) StartUsingItem() {
	m.s.useStart = m.s.tickCount
	if !m.Meta().UsingItem {
		m.UpdateMeta(func(meta *mob.Meta) { meta.UsingItem = true })
	}
}

// StopUsingItem is stopUsingItem.
func (m *Mob) StopUsingItem() {
	if m.Meta().UsingItem {
		m.UpdateMeta(func(meta *mob.Meta) { meta.UsingItem = false })
	}
}

// UsingItem is isUsingItem.
func (m *Mob) UsingItem() bool { return m.Meta().UsingItem }

// TicksUsingItem is getTicksUsingItem.
func (m *Mob) TicksUsingItem() int {
	if !m.UsingItem() {
		return 0
	}
	return m.s.tickCount - m.s.useStart
}

// LookAtEntity is Mob.lookAt(entity, yMax, xMax): turn the facing and pitch (not the head) towards the entity.
func (m *Mob) LookAtEntity(e world.Entity, yMax, xMax float64) {
	p, t := m.Position(), e.Position()
	xd, zd := t[0]-p[0], t[2]-p[2]
	var yd float64
	if _, ok := e.(entity.Living); ok {
		yd = eyeOf(e)[1] - m.EyeY()
	} else {
		bb := boxOf(e)
		yd = (bb.Min()[1]+bb.Max()[1])/2 - m.EyeY()
	}
	sd := math.Sqrt(xd*xd + zd*zd)
	yRot := float64(float32(math.Atan2(zd, xd)*180/math.Pi) - 90)
	xRot := float64(float32(-(math.Atan2(yd, sd) * 180 / math.Pi)))
	m.SetXRot(m.XRot() + math.Max(-xMax, math.Min(xMax, wrapDegrees(xRot-m.XRot()))))
	m.SetYRot(m.YRot() + math.Max(-yMax, math.Min(yMax, wrapDegrees(yRot-m.YRot()))))
}

// sqr is x*x.
func sqr(x float64) float64 { return x * x }

// wrapDegrees is Mth.wrapDegrees.
func wrapDegrees(a float64) float64 {
	a = math.Mod(a, 360)
	if a >= 180 {
		a -= 360
	}
	if a < -180 {
		a += 360
	}
	return a
}

// rotateIfNecessary is Mth.rotateIfNecessary: current turned towards target until it is within max of it.
func rotateIfNecessary(current, target, maxDiff float64) float64 {
	d := wrapDegrees(target - current)
	if d < -maxDiff {
		return target + maxDiff
	}
	if d > maxDiff {
		return target - maxDiff
	}
	return current
}

// rotateTowards is Mth.rotateTowards / LookControl.rotateTowards.
func rotateTowards(from, to, max float64) float64 {
	return from + math.Max(-max, math.Min(max, wrapDegrees(to-from)))
}
