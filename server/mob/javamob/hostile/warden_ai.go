//go:build crossfly_full

package hostile

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob/jbrain"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// WardenPose is the Java pose the warden shows (Pose: STANDING, EMERGING,
// DIGGING, ROARING, SNIFFING). Both clients get it: Bedrock as the actor
// flags emerging/digging/roaring/sniffing (javasession turns them into the
// Java pose).
type WardenPose uint8

const (
	WardenStanding WardenPose = iota
	WardenEmerging
	WardenDigging
	WardenRoaring
	WardenSniffing
)

// WardenAi durations (WardenAi).
const (
	wardenDiggingDuration  = 100 // Mth.ceil(100.0F)
	WardenEmergeDuration   = 134 // Mth.ceil(133.59999F)
	WardenRoarDuration     = 84  // Mth.ceil(84.0F)
	wardenSniffingDuration = 84  // Mth.ceil(83.2F)
	wardenDigCooldown      = 1200
	wardenDisturbanceTTL   = 100
	wardenRoarAnger        = 20
	sonicBoomDistanceXZ    = 15
	sonicBoomDistanceY     = 20
	sonicBoomCooldown      = 40
	sonicBoomSoundTicks    = 34 // Mth.ceil(34.0)
	sonicBoomDuration      = 60 // Mth.ceil(60.0F)
	roarSoundDelay         = 25
)

// WardenBody is what the warden behaviours need from the warden (Warden's
// own methods). The warden mob of this package implements it.
type WardenBody interface {
	Tx() *world.Tx
	Pos() mgl64.Vec3
	H() *world.EntityHandle
	OnGround() bool
	InLiquid() bool
	Riding() bool
	StopRiding()
	SetWardenPose(p WardenPose)
	WardenPose() WardenPose
	// Sound plays a Java sound at the warden.
	Sound(name string, volume, pitch float64)
	// EntityEvent broadcasts a Java entity event (62 sonic charge).
	EntityEvent(id int)
	// CanTargetEntity is Warden.canTargetEntity.
	CanTargetEntity(h *world.EntityHandle) bool
	// IncreaseAngerAt is Warden.increaseAngerAt(entity, amount, sound).
	IncreaseAngerAt(h *world.EntityHandle, amount int, sound bool)
	// SetAttackTarget is Warden.setAttackTarget.
	SetAttackTarget(h *world.EntityHandle)
	// EntityAngryAt is Warden.getEntityAngryAt.
	EntityAngryAt() (*world.EntityHandle, bool)
	// LookAt is LookControl.setLookAt(position) / BehaviorUtils.lookAtEntity.
	LookAt(pos mgl64.Vec3)
	// Discard removes the warden (remove(DISCARDED)).
	Discard()
	// ChestPos is position + the WARDEN_CHEST attachment (0, 1.6, 0).
	ChestPos() mgl64.Vec3
	// WithinWorldBorder is WorldBorder.isWithinBounds.
	WithinWorldBorder(pos cube.Pos) bool
}

// closerThanXZY is Entity.closerThan(entity, horizontal, vertical).
func closerThanXZY(a, b mgl64.Vec3, xz, y float64) bool {
	dx, dy, dz := b[0]-a[0], b[1]-a[1], b[2]-a[2]
	return dx*dx+dz*dz < xz*xz && dy*dy < y*y
}

// entityPos returns the position of a handle's entity in tx.
func entityPos(tx *world.Tx, h *world.EntityHandle) (mgl64.Vec3, world.Entity, bool) {
	if h == nil || tx == nil {
		return mgl64.Vec3{}, nil, false
	}
	e, ok := h.Entity(tx)
	if !ok {
		return mgl64.Vec3{}, nil, false
	}
	return e.Position(), e, true
}

// setDigCooldown is WardenAi.setDigCooldown: renew the dig cooldown (only if
// it is set).
func setDigCooldown(mem *jbrain.Memories) {
	if mem.Has(jbrain.MemDigCooldown.ID()) {
		jbrain.SetWithExpiry(mem, jbrain.MemDigCooldown, jbrain.Unit{}, wardenDigCooldown)
	}
}

// SetWardenDisturbance is WardenAi.setDisturbanceLocation: a warden not yet
// angry at anyone and without a target investigates pos for 5 s.
func SetWardenDisturbance(w WardenBody, mem *jbrain.Memories, pos cube.Pos) {
	if !w.WithinWorldBorder(pos) {
		return
	}
	if _, angry := w.EntityAngryAt(); angry || mem.Has(jbrain.MemAttackTarget.ID()) {
		return
	}
	setDigCooldown(mem)
	jbrain.SetWithExpiry(mem, jbrain.MemSniffCooldown, jbrain.Unit{}, wardenDisturbanceTTL)
	jbrain.SetWithExpiry[jbrain.PositionTracker](mem, jbrain.MemLookTarget, jbrain.TrackBlock(pos), wardenDisturbanceTTL)
	jbrain.SetWithExpiry(mem, jbrain.MemDisturbanceLocation, pos, wardenDisturbanceTTL)
	mem.Erase(jbrain.MemWalkTarget.ID())
}

// newDigCooldownSetter is WardenAi.DIG_COOLDOWN_SETTER.
func newDigCooldownSetter[E WardenBody]() jbrain.Control[E] {
	return jbrain.Trigger("DigCooldownSetter", []jbrain.Req{jbrain.Reg(jbrain.MemDigCooldown)}, func(c *jbrain.Ctx, e E) bool {
		if c.Mem.Has(jbrain.MemDigCooldown.ID()) {
			jbrain.SetWithExpiry(c.Mem, jbrain.MemDigCooldown, jbrain.Unit{}, wardenDigCooldown)
		}
		return true
	})
}

// newSetWardenLookTarget is SetWardenLookTarget: look at the roar target, or
// else the disturbance.
func newSetWardenLookTarget[E WardenBody]() jbrain.Control[E] {
	reqs := []jbrain.Req{jbrain.Reg(jbrain.MemLookTarget), jbrain.Reg(jbrain.MemDisturbanceLocation), jbrain.Reg(jbrain.MemRoarTarget), jbrain.Absent(jbrain.MemAttackTarget)}
	return jbrain.Trigger("SetWardenLookTarget", reqs, func(c *jbrain.Ctx, e E) bool {
		var target cube.Pos
		if h, ok := jbrain.Get(c.Mem, jbrain.MemRoarTarget); ok {
			p, _, ok := entityPos(c.Tx, h)
			if !ok {
				return false
			}
			target = jbrain.Containing(p)
		} else if d, ok := jbrain.Get(c.Mem, jbrain.MemDisturbanceLocation); ok {
			target = d
		} else {
			return false
		}
		jbrain.Set[jbrain.PositionTracker](c.Mem, jbrain.MemLookTarget, jbrain.TrackBlock(target))
		return true
	})
}

// newSetRoarTarget is SetRoarTarget.create(Warden::getEntityAngryAt).
func newSetRoarTarget[E WardenBody]() jbrain.Control[E] {
	reqs := []jbrain.Req{jbrain.Absent(jbrain.MemRoarTarget), jbrain.Absent(jbrain.MemAttackTarget), jbrain.Reg(jbrain.MemCantReachWalkTargetSince)}
	return jbrain.Trigger("SetRoarTarget", reqs, func(c *jbrain.Ctx, e E) bool {
		h, ok := e.EntityAngryAt()
		if !ok || !e.CanTargetEntity(h) {
			return false
		}
		jbrain.Set(c.Mem, jbrain.MemRoarTarget, h)
		c.Mem.Erase(jbrain.MemCantReachWalkTargetSince.ID())
		return true
	})
}

// newTryToSniff is TryToSniff: with something attackable near, no
// disturbance and no sniff cooldown, start sniffing (cooldown 100..200).
func newTryToSniff[E WardenBody]() jbrain.Control[E] {
	reqs := []jbrain.Req{jbrain.Reg(jbrain.MemIsSniffing), jbrain.Reg(jbrain.MemWalkTarget), jbrain.Absent(jbrain.MemSniffCooldown),
		jbrain.Present(jbrain.MemNearestAttackable), jbrain.Absent(jbrain.MemDisturbanceLocation)}
	return jbrain.Trigger("TryToSniff", reqs, func(c *jbrain.Ctx, e E) bool {
		jbrain.Set(c.Mem, jbrain.MemIsSniffing, jbrain.Unit{})
		jbrain.SetWithExpiry(c.Mem, jbrain.MemSniffCooldown, jbrain.Unit{}, int64(100+c.Rand.IntN(101)))
		c.Mem.Erase(jbrain.MemWalkTarget.ID())
		e.SetWardenPose(WardenSniffing)
		return true
	})
}

// sniffingTask is Sniffing: sniff for 84 ticks, then get angry at the nearest
// attackable entity within 6 blocks (20 up/down) and investigate it.
type sniffingTask[E WardenBody] struct{ jbrain.TaskBase[E] }

func (sniffingTask[E]) CanStillUse(*jbrain.Ctx, E) bool { return true }

func (sniffingTask[E]) Start(_ *jbrain.Ctx, e E) { e.Sound("entity.warden.sniff", 5, 1) }

func (sniffingTask[E]) Stop(c *jbrain.Ctx, e E) {
	if e.WardenPose() == WardenSniffing {
		e.SetWardenPose(WardenStanding)
	}
	c.Mem.Erase(jbrain.MemIsSniffing.ID())
	h, ok := jbrain.Get(c.Mem, jbrain.MemNearestAttackable)
	if !ok || !e.CanTargetEntity(h) {
		return
	}
	p, _, ok := entityPos(c.Tx, h)
	if !ok {
		return
	}
	if closerThanXZY(e.Pos(), p, 6, 20) {
		e.IncreaseAngerAt(h, 35, true)
	}
	if !c.Mem.Has(jbrain.MemDisturbanceLocation.ID()) {
		SetWardenDisturbance(e, c.Mem, jbrain.Containing(p))
	}
}

func newSniffing[E WardenBody]() jbrain.Control[E] {
	reqs := []jbrain.Req{jbrain.Present(jbrain.MemIsSniffing), jbrain.Absent(jbrain.MemAttackTarget), jbrain.Absent(jbrain.MemWalkTarget),
		jbrain.Reg(jbrain.MemLookTarget), jbrain.Reg(jbrain.MemNearestAttackable), jbrain.Reg(jbrain.MemDisturbanceLocation), jbrain.Reg(jbrain.MemSniffCooldown)}
	return jbrain.NewBehavior[E]("Sniffing", reqs, wardenSniffingDuration, wardenSniffingDuration, sniffingTask[E]{})
}

// emergingTask is Emerging: rise from the ground for 134 ticks.
type emergingTask[E WardenBody] struct{ jbrain.TaskBase[E] }

func (emergingTask[E]) CanStillUse(*jbrain.Ctx, E) bool { return true }

func (emergingTask[E]) Start(_ *jbrain.Ctx, e E) {
	e.SetWardenPose(WardenEmerging)
	e.Sound("entity.warden.emerge", 5, 1)
}

func (emergingTask[E]) Stop(_ *jbrain.Ctx, e E) {
	if e.WardenPose() == WardenEmerging {
		e.SetWardenPose(WardenStanding)
	}
}

func newEmerging[E WardenBody]() jbrain.Control[E] {
	reqs := []jbrain.Req{jbrain.Present(jbrain.MemIsEmerging), jbrain.Absent(jbrain.MemWalkTarget), jbrain.Reg(jbrain.MemLookTarget)}
	return jbrain.NewBehavior[E]("Emerging", reqs, WardenEmergeDuration, WardenEmergeDuration, emergingTask[E]{})
}

// diggingTask is Digging: on the ground the warden digs down for 100 ticks
// and is gone; in liquid it only growls and is gone at once.
type diggingTask[E WardenBody] struct {
	jbrain.TaskBase[E]
	removed *bool
}

func (t diggingTask[E]) CanStillUse(*jbrain.Ctx, E) bool { return !*t.removed }

func (diggingTask[E]) CanStart(_ *jbrain.Ctx, e E) bool { return e.OnGround() || e.InLiquid() }

func (t diggingTask[E]) Start(c *jbrain.Ctx, e E) {
	if e.OnGround() {
		e.SetWardenPose(WardenDigging)
		e.Sound("entity.warden.dig", 5, 1)
		return
	}
	e.Sound("entity.warden.agitated", 5, 1)
	t.Stop(c, e)
}

func (t diggingTask[E]) Stop(_ *jbrain.Ctx, e E) {
	if !*t.removed {
		*t.removed = true
		e.Discard()
	}
}

func newDigging[E WardenBody]() jbrain.Control[E] {
	reqs := []jbrain.Req{jbrain.Absent(jbrain.MemAttackTarget), jbrain.Absent(jbrain.MemWalkTarget)}
	return jbrain.NewBehavior[E]("Digging", reqs, wardenDiggingDuration, wardenDiggingDuration, diggingTask[E]{removed: new(bool)})
}

// newForceUnmount is ForceUnmount: a riding warden gets off.
func newForceUnmount[E WardenBody]() jbrain.Control[E] {
	return jbrain.Trigger("ForceUnmount", nil, func(c *jbrain.Ctx, e E) bool {
		if !e.Riding() {
			return false
		}
		e.StopRiding()
		return true
	})
}

// roarTask is Roar: face the roar target, roar (the sound after 25 ticks)
// for 84 ticks, +20 anger, then attack it.
type roarTask[E WardenBody] struct{ jbrain.TaskBase[E] }

func (roarTask[E]) CanStillUse(*jbrain.Ctx, E) bool { return true }

func (roarTask[E]) Start(c *jbrain.Ctx, e E) {
	jbrain.SetWithExpiry(c.Mem, jbrain.MemRoarSoundDelay, jbrain.Unit{}, roarSoundDelay)
	c.Mem.Erase(jbrain.MemWalkTarget.ID())
	h, _ := jbrain.Get(c.Mem, jbrain.MemRoarTarget)
	if p, _, ok := entityPos(c.Tx, h); ok {
		e.LookAt(p)
	}
	e.SetWardenPose(WardenRoaring)
	e.IncreaseAngerAt(h, wardenRoarAnger, false)
}

func (roarTask[E]) Tick(c *jbrain.Ctx, e E) {
	if !c.Mem.Has(jbrain.MemRoarSoundDelay.ID()) && !c.Mem.Has(jbrain.MemRoarSoundCooldown.ID()) {
		jbrain.SetWithExpiry(c.Mem, jbrain.MemRoarSoundCooldown, jbrain.Unit{}, WardenRoarDuration-roarSoundDelay)
		e.Sound("entity.warden.roar", 3, 1)
	}
}

func (roarTask[E]) Stop(c *jbrain.Ctx, e E) {
	if e.WardenPose() == WardenRoaring {
		e.SetWardenPose(WardenStanding)
	}
	if h, ok := jbrain.Get(c.Mem, jbrain.MemRoarTarget); ok {
		e.SetAttackTarget(h)
	}
	c.Mem.Erase(jbrain.MemRoarTarget.ID())
}

func newRoar[E WardenBody]() jbrain.Control[E] {
	reqs := []jbrain.Req{jbrain.Present(jbrain.MemRoarTarget), jbrain.Absent(jbrain.MemAttackTarget), jbrain.Reg(jbrain.MemRoarSoundCooldown), jbrain.Reg(jbrain.MemRoarSoundDelay)}
	return jbrain.NewBehavior[E]("Roar", reqs, WardenRoarDuration, WardenRoarDuration, roarTask[E]{})
}

// SonicBoomHit hurts the target of a sonic boom: 10 sonic boom damage
// (ignores armour), and a push of 2.5 sideways and 0.5 up away from the
// warden, less the target's knockback resistance. The base sets it.
var SonicBoomHit = func(tx *world.Tx, warden *world.EntityHandle, target world.Entity, dir mgl64.Vec3) {}

// SonicBoomParticles shows the boom's rings from the chest to the target
// (ParticleTypes.SONIC_BOOM at each step). The base sets it.
var SonicBoomParticles = func(tx *world.Tx, from, dir mgl64.Vec3, steps int) {}

// sonicBoomTask is SonicBoom: with the target within 15 blocks (20 up/down),
// charge for 34 ticks, then the boom (if the target is still in range), and
// a 40-tick cooldown when the 60 ticks are over.
type sonicBoomTask[E WardenBody] struct{ jbrain.TaskBase[E] }

func (sonicBoomTask[E]) CanStart(c *jbrain.Ctx, e E) bool {
	h, _ := jbrain.Get(c.Mem, jbrain.MemAttackTarget)
	p, _, ok := entityPos(c.Tx, h)
	return ok && closerThanXZY(e.Pos(), p, sonicBoomDistanceXZ, sonicBoomDistanceY)
}

func (sonicBoomTask[E]) CanStillUse(*jbrain.Ctx, E) bool { return true }

func (sonicBoomTask[E]) Start(c *jbrain.Ctx, e E) {
	jbrain.SetWithExpiry(c.Mem, jbrain.MemAttackCoolingDown, true, sonicBoomDuration)
	jbrain.SetWithExpiry(c.Mem, jbrain.MemSonicBoomSoundDelay, jbrain.Unit{}, sonicBoomSoundTicks)
	e.EntityEvent(62)
	e.Sound("entity.warden.sonic_charge", 3, 1)
}

func (sonicBoomTask[E]) Tick(c *jbrain.Ctx, e E) {
	h, _ := jbrain.Get(c.Mem, jbrain.MemAttackTarget)
	if p, _, ok := entityPos(c.Tx, h); ok {
		e.LookAt(p)
	}
	if c.Mem.Has(jbrain.MemSonicBoomSoundDelay.ID()) || c.Mem.Has(jbrain.MemSonicBoomSoundCooldown.ID()) {
		return
	}
	jbrain.SetWithExpiry(c.Mem, jbrain.MemSonicBoomSoundCooldown, jbrain.Unit{}, sonicBoomDuration-sonicBoomSoundTicks)
	if h == nil || !e.CanTargetEntity(h) {
		return
	}
	p, t, ok := entityPos(c.Tx, h)
	if !ok || !closerThanXZY(e.Pos(), p, sonicBoomDistanceXZ, sonicBoomDistanceY) {
		return
	}
	src := e.ChestPos()
	delta := eyePosOf(t).Sub(src)
	if delta.Len() == 0 {
		return
	}
	dir := delta.Normalize()
	SonicBoomParticles(c.Tx, src, dir, int(math.Floor(delta.Len()))+7)
	e.Sound("entity.warden.sonic_boom", 3, 1)
	SonicBoomHit(c.Tx, e.H(), t, dir)
}

func (sonicBoomTask[E]) Stop(c *jbrain.Ctx, _ E) { setSonicBoomCooldown(c.Mem, sonicBoomCooldown) }

// setSonicBoomCooldown is SonicBoom.setCooldown.
func setSonicBoomCooldown(mem *jbrain.Memories, ticks int64) {
	jbrain.SetWithExpiry(mem, jbrain.MemSonicBoomCooldown, jbrain.Unit{}, ticks)
}

func newSonicBoom[E WardenBody]() jbrain.Control[E] {
	reqs := []jbrain.Req{jbrain.Present(jbrain.MemAttackTarget), jbrain.Absent(jbrain.MemSonicBoomCooldown),
		jbrain.Reg(jbrain.MemSonicBoomSoundCooldown), jbrain.Reg(jbrain.MemSonicBoomSoundDelay)}
	return jbrain.NewBehavior[E]("SonicBoom", reqs, sonicBoomDuration, sonicBoomDuration, sonicBoomTask[E]{})
}

// eyePosOf returns an entity's eye position.
func eyePosOf(e world.Entity) mgl64.Vec3 {
	if eh, ok := e.(interface{ EyeHeight() float64 }); ok {
		return e.Position().Add(mgl64.Vec3{0, eh.EyeHeight()})
	}
	return e.Position().Add(mgl64.Vec3{0, e.H().Type().BBox(e).Height() * 0.85})
}
