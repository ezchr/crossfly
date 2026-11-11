package entity

import (
	"math"
	"math/rand/v2"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The fishing hook, as BDS 1.21.3 FishingHook and the minecraft:fishing_hook definition of the vanilla behaviour
// pack. Fishing in water (the bite timers of FishingHook::_updateServer/_serverLured/_serverHooked and the fishing loot
// table) is not implemented: the hook floats and bobs on water but nothing bites.
const (
	// fishingHookCastSpeed and fishingHookCastPower: the FishingHook constructor sets 0.4 (+1096) and 1.5 (+1100), the
	// speed of the look direction and the length of the cast velocity (FishingHook::reloadHardcoded).
	fishingHookCastSpeed = 0.4
	fishingHookCastPower = 1.5
	// fishingHookSpread scales the gaussian added to each axis of the normalised cast direction (reloadHardcoded).
	fishingHookSpread = 0.0075
	// fishingHookGravity: FishingHook::_updateGravity adds (water percentage*2-1)*0.04 to the Y velocity, so 0.04 of
	// gravity in air.
	fishingHookGravity = 0.04
	// fishingHookAirDrag and fishingHookGroundDrag multiply the velocity every tick: 0.92 in the air, 0.5 while on the
	// ground or against a wall, times 0.9 more in water (_updateGravity).
	fishingHookAirDrag    = 0.92
	fishingHookGroundDrag = 0.5
	fishingHookWaterDrag  = 0.9
	// fishingHookWaterChecks is how many slices of its box the hook tests for water (+1104, _getWaterPercentage).
	fishingHookWaterChecks = 5
	// fishingHookMaxDistanceSq: the hook is removed once it is more than 32 blocks from its owner's feet
	// (postNormalTick, distanceToSqr > 1024).
	fishingHookMaxDistanceSq = 1024
	// fishingHookOwnerGrace is how many ticks the owner cannot be hooked by its own cast (_hitCheck, +1132 >= 5).
	fishingHookOwnerGrace = 5
	// fishingHookSearchGrow and fishingHookTargetGrow: _hitCheck looks for entities in the hook's box stretched along
	// its velocity and grown by 1, and tests the motion against their boxes grown by 0.3.
	fishingHookSearchGrow = 1.0
	fishingHookTargetGrow = 0.3
	// fishingHookHeightFraction: a hooked entity carries the hook at 0.8 of its height (postNormalTick).
	fishingHookHeightFraction = 0.8
	// fishingHookPull and fishingHookPullLift: reeling in pulls the hooked entity by 0.1 of the way to 1 block above
	// the owner's head, plus 0.08 of that pull's length upwards (FishingHook::_pullCloser(target, 0.1)).
	fishingHookPull     = 0.1
	fishingHookPullLift = 0.08
	// fishingHookKnockBackForce and fishingHookKnockBackHeight: the hook hurts what it hooks for 0 with knock back on
	// (_hitCheck: Actor::hurt(ActorDamageByChildActorSource(hook, owner, projectile), 0, knock)), so the hooked
	// entity is knocked back like by any projectile hit (ProjectileBehaviour.hitEntity).
	fishingHookKnockBackForce  = 0.45
	fishingHookKnockBackHeight = 0.3608
)

// The durability a fishing rod loses when reeling in (FishingHook::retrieve, passed to ItemStackBase::hurtAndBreak by
// FishingRodItem::use).
const (
	// FishingRodHookedDurability: the hook held an entity, which is pulled in.
	FishingRodHookedDurability = 3
	// FishingRodGroundDurability: the hook hit a block.
	FishingRodGroundDurability = 2
	// FishingRodCatchDurability: a fish was caught (not implemented: nothing bites).
	FishingRodCatchDurability = 1
)

// FishingHookType is a world.EntityType implementation for the fishing hook a fishing rod casts.
var FishingHookType fishingHookType

type fishingHookType struct{}

func (fishingHookType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Ent{tx: tx, handle: handle, data: data}
}

func (fishingHookType) EncodeEntity() string { return "minecraft:fishing_hook" }

// BBox is the minecraft:collision_box of fishing_hook.json: 0.15 wide and high.
func (fishingHookType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.075, 0, -0.075, 0.075, 0.15, 0.075)
}

// DecodeNBT: the hook is minecraft:transient. A loaded one has no owner and removes itself on its first tick.
func (fishingHookType) DecodeNBT(_ map[string]any, data *world.EntityData) {
	data.Data = &FishingHookBehaviour{}
}
func (fishingHookType) EncodeNBT(*world.EntityData) map[string]any { return nil }

// fishingHookConfig creates the behaviour of a newly cast hook.
type fishingHookConfig struct{ owner *world.EntityHandle }

func (c fishingHookConfig) Apply(data *world.EntityData) {
	data.Data = &FishingHookBehaviour{owner: c.owner}
}

// NewFishingHook casts a fishing hook for owner. The velocity of opts is ignored: the hook flies where owner looks,
// with owner's own velocity added, as FishingHook::reloadHardcoded does. owner may not be nil.
func NewFishingHook(opts world.EntitySpawnOpts, owner world.Entity) *world.EntityHandle {
	ownerVel := velocityOf(owner)
	opts.Position = opts.Position.Add(ownerVel)
	opts.Velocity = fishingHookVelocity(owner.Rotation(), ownerVel, [3]float64{rand.NormFloat64(), rand.NormFloat64(), rand.NormFloat64()})
	opts.Rotation = velocityRotation(opts.Velocity)
	h := opts.New(FishingHookType, fishingHookConfig{owner: owner.H()})
	setFishingHook(owner.H(), h)
	return h
}

// fishingHookVelocity is the velocity of a hook cast by an entity looking along rot and moving at ownerVel
// (FishingHook::reloadHardcoded): the look direction at 0.4 plus ownerVel, normalised, with gaussian*0.0075 added to
// each axis, times 1.5.
func fishingHookVelocity(rot cube.Rotation, ownerVel mgl64.Vec3, gaussian [3]float64) mgl64.Vec3 {
	yaw, pitch := mgl64.DegToRad(rot.Yaw()), mgl64.DegToRad(rot.Pitch())
	v := mgl64.Vec3{
		-math.Sin(yaw) * math.Cos(pitch) * fishingHookCastSpeed,
		-math.Sin(pitch) * fishingHookCastSpeed,
		math.Cos(yaw) * math.Cos(pitch) * fishingHookCastSpeed,
	}.Add(ownerVel)
	l := v.Len()
	if l == 0 || !finiteVec3(v) {
		return mgl64.Vec3{}
	}
	for i := range v {
		v[i] = (v[i]/l + gaussian[i]*fishingHookSpread) * fishingHookCastPower
	}
	return v
}

// ReelFishingHook reels in the fishing hook owner has out, if any (FishingRodItem::use with a hook out). It returns
// the durability owner's rod loses and true, or false if owner has no hook out in tx's world.
func ReelFishingHook(tx *world.Tx, owner world.Entity) (int, bool) {
	h := fishingHookOf(owner.H())
	if h == nil {
		return 0, false
	}
	e, ok := h.Entity(tx)
	if !ok {
		// The hook is in another world (the owner left it, and it removes itself there) or was unloaded with its
		// chunk: a new one is cast.
		clearFishingHook(owner.H(), h)
		return 0, false
	}
	ent, ok := e.(*Ent)
	if !ok {
		return 0, false
	}
	b, ok := ent.Behaviour().(*FishingHookBehaviour)
	if !ok {
		return 0, false
	}
	return b.retrieve(ent, tx), true
}

// HurtsWithoutDamage reports whether src hurts what it hits although it deals no damage: the hit of a fishing hook,
// which hurts for 0 (and so knocks back) as FishingHook::_hitCheck does. Living entities that drop hits of 0 damage
// take these.
func HurtsWithoutDamage(src world.DamageSource) bool {
	s, ok := src.(ProjectileDamageSource)
	return ok && s.Projectile != nil && s.Projectile.H().Type() == FishingHookType
}

var (
	fishingHooksMu sync.Mutex
	// fishingHooks are the hooks out by owner (Actor::setFishingHookID).
	fishingHooks = map[*world.EntityHandle]*world.EntityHandle{}
)

func fishingHookOf(owner *world.EntityHandle) *world.EntityHandle {
	fishingHooksMu.Lock()
	defer fishingHooksMu.Unlock()
	return fishingHooks[owner]
}

func setFishingHook(owner, hook *world.EntityHandle) {
	fishingHooksMu.Lock()
	defer fishingHooksMu.Unlock()
	fishingHooks[owner] = hook
}

// clearFishingHook forgets hook as owner's hook (Actor::clearFishingHookID), unless owner has cast another since.
func clearFishingHook(owner, hook *world.EntityHandle) {
	fishingHooksMu.Lock()
	defer fishingHooksMu.Unlock()
	if fishingHooks[owner] == hook {
		delete(fishingHooks, owner)
	}
}

// FishingHookBehaviour is the behaviour of a fishing hook (BDS FishingHook::postNormalTick): it flies with its own
// gravity and drag, floats on water, hooks the first entity it hits and follows it, and is removed when its owner
// dies, stops holding a fishing rod, goes more than 32 blocks away or leaves the world.
type FishingHookBehaviour struct {
	owner  *world.EntityHandle
	hooked *world.EntityHandle
	// inGround is set once the hook hit a block (FishingHook+1152): it hooks no entity after that, and reeling it in
	// costs FishingRodGroundDurability.
	inGround bool
	// ticks is the number of ticks since the hook was cast.
	ticks int
	// bob is the phase of the bobbing on the water surface (+1120).
	bob float64
	// onGround and wall are whether the last move ended on the ground or against a wall.
	onGround, wall bool
	mc             MovementComputer
	closed         bool
}

// Owner returns the entity that cast the hook. Bedrock shows the line from its hand (actor data owner).
func (b *FishingHookBehaviour) Owner() *world.EntityHandle { return b.owner }

// TargetEntity returns the entity the hook holds, or nil (actor data target).
func (b *FishingHookBehaviour) TargetEntity() *world.EntityHandle { return b.hooked }

// InGround reports whether the hook hit a block.
func (b *FishingHookBehaviour) InGround() bool { return b.inGround }

// Tick moves the hook one tick (FishingHook::postNormalTick).
func (b *FishingHookBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if b.closed {
		b.remove(e)
		return nil
	}
	b.ticks++
	pos, vel, rot := e.Position(), e.Velocity(), e.Rotation()
	if !finiteVec3(pos) || !finiteVec3(vel) {
		b.remove(e)
		return nil
	}
	var lava bool
	if vel, lava = b.updateGravity(tx, e, pos, vel); lava {
		// _updateGravity: a hook in lava is retrieved at once, the rod is not damaged.
		b.retrieve(e, tx)
		return nil
	}

	var hooked world.Entity
	if b.hooked != nil {
		t, ok := b.hooked.Entity(tx)
		if !ok || !hookAlive(t) {
			b.remove(e)
			return nil
		}
		hooked = t
		vel = velocityOf(t)
		rot = t.Rotation()
	} else if !b.hitCheck(e, tx, pos, vel) {
		dPos, after := b.mc.CheckCollision(tx, e, pos, vel)
		b.wall = vel[0] != 0 && after[0] == 0 || vel[2] != 0 && after[2] == 0
		b.onGround = b.mc.OnGround()
		pos, vel = pos.Add(dPos), after
		rot = velocityRotation(vel)
	} else if b.hooked != nil {
		hooked, _ = b.hooked.Entity(tx)
	}

	if !b.ownerHolds(tx, pos) {
		b.remove(e)
		return nil
	}
	if hooked != nil && hookAlive(hooked) {
		pos = hookedPosition(hooked)
	}
	old, oldVel := e.Position(), e.Velocity()
	e.data.Pos, e.data.Vel, e.data.Rot = pos, vel, rot
	return &Movement{v: tx.Viewers(pos), e: e, pos: pos, vel: vel, dpos: pos.Sub(old), dvel: vel.Sub(oldVel), rot: rot, onGround: b.onGround}
}

// updateGravity applies the hook's gravity, drag and buoyancy to vel (FishingHook::_updateGravity). It reports true if
// the hook is in lava.
func (b *FishingHookBehaviour) updateGravity(tx *world.Tx, e *Ent, pos, vel mgl64.Vec3) (mgl64.Vec3, bool) {
	drag := fishingHookAirDrag
	if b.onGround || b.wall {
		drag = fishingHookGroundDrag
	}
	b.bob += 0.1
	bp := cube.PosFromVec3(pos)
	// level is how high the water at the hook reaches over its feet (0 out of water), bob how far the hook dips.
	var level, bob float64
	if isWater(tx, bp) {
		if isWater(tx, bp.Side(cube.FaceUp)) {
			level = 1.01
		} else {
			level = 1 - (pos[1] - float64(bp[1]))
			bob = (math.Sin(b.bob) + 1) * 0.035
		}
	} else if isLava(tx, bp) {
		return vel, true
	}
	vy := vel[1] + (fishingHookWaterPercentage(tx, e.H().Type().BBox(e).Translate(pos))*2-1)*fishingHookGravity
	if level != 0 {
		drag *= fishingHookWaterDrag
		vy = math.Min((level-bob)*0.15, vy*0.8+0.05)
	}
	return mgl64.Vec3{vel[0] * drag, vy * drag, vel[2] * drag}, false
}

// fishingHookWaterPercentage is the part of box in water, tested in 5 horizontal slices
// (FishingHook::_getWaterPercentage).
func fishingHookWaterPercentage(tx *world.Tx, box cube.BBox) float64 {
	var pct float64
	minY, h := box.Min()[1], box.Height()
	for i := 0; i < fishingHookWaterChecks; i++ {
		lo := minY + float64(i)*h/fishingHookWaterChecks
		hi := minY + float64(i+1)*h/fishingHookWaterChecks
		if boxInWater(tx, cube.Box(box.Min()[0], lo, box.Min()[2], box.Max()[0], hi, box.Max()[2])) {
			pct += 1.0 / fishingHookWaterChecks
		}
	}
	return pct
}

// boxInWater reports whether any block box touches is water.
func boxInWater(tx *world.Tx, box cube.BBox) bool {
	lo, hi := cube.PosFromVec3(box.Min()), cube.PosFromVec3(box.Max())
	for x := lo[0]; x <= hi[0]; x++ {
		for y := lo[1]; y <= hi[1]; y++ {
			for z := lo[2]; z <= hi[2]; z++ {
				if isWater(tx, cube.Pos{x, y, z}) {
					return true
				}
			}
		}
	}
	return false
}

// isWater and isLava report whether the liquid at pos (also a waterlogged block's) is water or lava.
func isWater(tx *world.Tx, pos cube.Pos) bool {
	l, ok := tx.Liquid(pos)
	if ok {
		_, ok = l.(block.Water)
	}
	return ok
}

func isLava(tx *world.Tx, pos cube.Pos) bool {
	l, ok := tx.Liquid(pos)
	if ok {
		_, ok = l.(block.Lava)
	}
	return ok
}

// hitCheck traces the hook's motion this tick (FishingHook::_hitCheck): the ray from pos to pos+vel is cut short by
// the first block it hits (liquids are passed through), and the nearest hookable entity whose box, grown by 0.3,
// the ray touches is hooked and hurt. It reports whether anything was hit; the hook then does not move this tick.
func (b *FishingHookBehaviour) hitCheck(e *Ent, tx *world.Tx, pos, vel mgl64.Vec3) bool {
	if b.inGround {
		return false
	}
	start, end := pos, pos.Add(vel)
	blockHit := false
	if start != end {
		trace.TraverseBlocks(start, end, func(p cube.Pos) bool {
			if r, ok := trace.BlockIntercept(p, tx, tx.Block(p), start, end); ok {
				end, blockHit = r.Position(), true
				return false
			}
			return true
		})
	}

	var owner world.Entity
	if b.owner != nil {
		owner, _ = b.owner.Entity(tx)
	}
	search := e.H().Type().BBox(e).Translate(pos).Extend(vel).Grow(fishingHookSearchGrow)
	var (
		target world.Entity
		dist   float64
	)
	// EntitiesWithin keeps entities whose position lies in the box: grow it by more than any entity is wide, and test
	// the boxes themselves, as BDS does.
	for other := range tx.EntitiesWithin(search.Grow(4)) {
		if other.H() == e.H() || !b.hookable(tx, other, owner) {
			continue
		}
		box := other.H().Type().BBox(other).Translate(other.Position())
		if !box.IntersectsWith(search) {
			continue
		}
		r, ok := trace.BBoxIntercept(box.Grow(fishingHookTargetGrow), start, end)
		if !ok {
			continue
		}
		if d := r.Position().Sub(start).LenSqr(); target == nil || d < dist {
			target, dist = other, d
		}
	}
	if target != nil {
		b.hooked = target.H()
		hurtHooked(e, target, owner, vel)
		for _, v := range tx.Viewers(e.Position()) {
			v.ViewEntityState(e)
		}
	}
	if target == nil && !blockHit {
		return false
	}
	b.inGround = target == nil
	return true
}

// hookable reports whether the hook may catch other (_hitCheck): a living entity that is alive and not a spectator,
// not the owner during the first 5 ticks, not the owner's vehicle and not riding the owner.
func (b *FishingHookBehaviour) hookable(tx *world.Tx, other, owner world.Entity) bool {
	if !hookAlive(other) {
		return false
	}
	if g, ok := other.(interface{ GameMode() world.GameMode }); ok && !g.GameMode().HasCollision() {
		return false
	}
	if owner == nil {
		return true
	}
	if other.H() == owner.H() && b.ticks < fishingHookOwnerGrace {
		return false
	}
	if r, ok := RiderOf(owner); ok && r.RidingEntityHandle() == other.H() {
		return false
	}
	if r, ok := RiderOf(other); ok && r.RidingEntityHandle() == owner.H() {
		return false
	}
	return true
}

// hookAlive reports whether e is a living entity that is not dead.
func hookAlive(e world.Entity) bool {
	l, ok := e.(Living)
	return ok && !l.Dead()
}

// hurtHooked hurts the entity a hook caught for 0 and knocks it back along the hook's motion when the hurt was taken.
func hurtHooked(hook *Ent, victim, owner world.Entity, vel mgl64.Vec3) {
	l, ok := victim.(Living)
	if !ok {
		return
	}
	if _, res := l.Hurt(0, ProjectileDamageSource{Projectile: hook, Owner: owner}); res.Accepted() {
		l.KnockBack(l.Position().Sub(vel), fishingHookKnockBackForce, fishingHookKnockBackHeight)
	}
}

// ownerHolds reports whether the hook may stay out (postNormalTick): it has an owner in this world, and an owning
// player is alive, holds a fishing rod in its main hand and is at most 32 blocks from the hook.
func (b *FishingHookBehaviour) ownerHolds(tx *world.Tx, pos mgl64.Vec3) bool {
	if b.owner == nil {
		return false
	}
	o, ok := b.owner.Entity(tx)
	if !ok {
		return false
	}
	if o.H().Type().EncodeEntity() != "minecraft:player" {
		return true
	}
	if !hookAlive(o) {
		return false
	}
	c, ok := o.(item.Carrier)
	if !ok {
		return false
	}
	if main, _ := c.HeldItems(); main.Empty() {
		return false
	} else if _, rod := main.Item().(item.FishingRod); !rod {
		return false
	}
	return pos.Sub(o.Position()).LenSqr() <= fishingHookMaxDistanceSq
}

// retrieve reels the hook in (FishingHook::retrieve): a hooked entity is pulled towards the owner. It removes the hook
// and returns the durability the rod loses.
func (b *FishingHookBehaviour) retrieve(e *Ent, tx *world.Tx) int {
	dmg := 0
	if b.hooked != nil {
		if t, ok := b.hooked.Entity(tx); ok && hookAlive(t) {
			if b.owner != nil {
				if o, ok := b.owner.Entity(tx); ok {
					pullHooked(tx, o, t)
				}
			}
			dmg = FishingRodHookedDurability
		}
	}
	if b.inGround {
		dmg = FishingRodGroundDurability
	}
	b.remove(e)
	return dmg
}

// pullHooked pulls target towards owner (FishingHook::_pullCloser(target, 0.1)): a rider is dismounted first.
func pullHooked(tx *world.Tx, owner, target world.Entity) {
	if r, ok := RiderOf(target); ok && r.RidingEntityHandle() != nil {
		r.DismountEntity(tx)
	}
	if l, ok := target.(Living); ok {
		l.SetVelocity(fishingHookPullVelocity(eyePosition(owner), bedrockPosition(target)))
	}
}

// fishingHookPullVelocity is the velocity _pullCloser gives an entity at target pulled by an owner whose head is at
// ownerHead: 0.1 of the way to 1 block above the head, plus 0.08 of that length upwards.
func fishingHookPullVelocity(ownerHead, target mgl64.Vec3) mgl64.Vec3 {
	d := ownerHead.Add(mgl64.Vec3{0, 1, 0}).Sub(target).Mul(fishingHookPull)
	d[1] += d.Len() * fishingHookPullLift
	return d
}

// hookedPosition is where the hook sits on the entity it holds: its middle, at 0.8 of its height (postNormalTick).
func hookedPosition(t world.Entity) mgl64.Vec3 {
	pos := t.Position()
	pos[1] += t.H().Type().BBox(t).Height() * fishingHookHeightFraction
	return pos
}

// eyePosition is where e's head is (ActorLocation Head: the camera position).
func eyePosition(e world.Entity) mgl64.Vec3 {
	pos := e.Position()
	if eyed, ok := e.(interface{ EyeHeight() float64 }); ok {
		pos[1] += eyed.EyeHeight()
	}
	return pos
}

// bedrockPosition is e's position as BDS keeps it: a player's at its eyes, every other entity's at its feet.
func bedrockPosition(e world.Entity) mgl64.Vec3 {
	if e.H().Type().EncodeEntity() == "minecraft:player" {
		return eyePosition(e)
	}
	return e.Position()
}

// velocityOf returns the velocity of e, if it has one.
func velocityOf(e world.Entity) mgl64.Vec3 {
	if v, ok := e.(interface{ Velocity() mgl64.Vec3 }); ok {
		if vel := v.Velocity(); finiteVec3(vel) {
			return vel
		}
	}
	return mgl64.Vec3{}
}

// velocityRotation is the yaw and pitch of something moving at vel.
func velocityRotation(vel mgl64.Vec3) cube.Rotation {
	return cube.Rotation{
		mgl64.RadToDeg(math.Atan2(vel[0], vel[2])),
		mgl64.RadToDeg(math.Atan2(vel[1], math.Hypot(vel[0], vel[2]))),
	}
}

// remove removes the hook and forgets it as its owner's (FishingHook::remove).
func (b *FishingHookBehaviour) remove(e *Ent) {
	b.closed = true
	if b.owner != nil {
		clearFishingHook(b.owner, e.H())
	}
	_ = e.Close()
}
