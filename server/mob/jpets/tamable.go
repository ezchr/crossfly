//go:build crossfly_full

package jpets

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Tamable is the TamableAnimal part of a pet's behaviour (wolf, cat,
// parrot, nautilus): embed it and call its methods from the behaviour.
// The owner itself is kept by package mob (Mob.SetOwner, saved with the
// mob); Tamable keeps the tame flag, the order to sit and the sitting pose.
type Tamable struct {
	TamableState
	// CanFly is TamableAnimal.canFlyToOwner (parrots: may teleport onto
	// leaves).
	CanFly bool
	// SideEffects is applyTamingSideEffects, run by SetTame(.., true) (the
	// wolf's health, the nautilus's attributes).
	SideEffects func(m *javamob.Mob, tame bool)
}

// IsTame is TamableAnimal.isTame.
func (t *Tamable) IsTame() bool { return t.Tame }

// SetTame is TamableAnimal.setTame: the tame flag (DATA_FLAGS_ID 0x04,
// shown to both clients as Meta.Tamed) and, with sideEffects, the type's
// applyTamingSideEffects.
func (t *Tamable) SetTame(m *javamob.Mob, tame, sideEffects bool) {
	t.Tame = tame
	if m.Meta().Tamed != tame {
		m.UpdateMeta(func(meta *mob.Meta) { meta.Tamed = tame })
	}
	if sideEffects && t.SideEffects != nil {
		t.SideEffects(m, tame)
	}
}

// SetInSittingPose is TamableAnimal.setInSittingPose (DATA_FLAGS_ID 0x01).
func (t *Tamable) SetInSittingPose(m *javamob.Mob, v bool) {
	t.InSittingPose = v
	if m.Meta().Sitting != v {
		m.UpdateMeta(func(meta *mob.Meta) { meta.Sitting = v })
	}
}

// TameTo is TamableAnimal.tame(player): tame with side effects, owned by p.
func (t *Tamable) TameTo(m *javamob.Mob, p world.Entity) {
	t.SetTame(m, true, true)
	m.SetOwner(p)
}

// UnableToMoveToOwner is TamableAnimal.unableToMoveToOwner.
func (t *Tamable) UnableToMoveToOwner(m *javamob.Mob) bool {
	o, ok := ownerOf(m)
	return PetMobility{
		OrderedToSit:   t.OrderedToSit,
		Passenger:      m.Riding(),
		Leashed:        leashed(m),
		OwnerSpectator: ok && spectator(o),
	}.UnableToMoveToOwner()
}

// ShouldTryTeleportToOwner is TamableAnimal.shouldTryTeleportToOwner: the
// owner is 12 or more blocks away.
func (t *Tamable) ShouldTryTeleportToOwner(m *javamob.Mob) bool {
	o, ok := ownerOf(m)
	return ok && distSq(m.Position(), o.Position()) >= TeleportWhenDistanceSq
}

// TryToTeleportToOwner is TamableAnimal.tryToTeleportToOwner: up to ten
// spots around the owner (TeleportSpot); on success the pet is put on the
// block centre and its path dropped.
func (t *Tamable) TryToTeleportToOwner(m *javamob.Mob) {
	o, ok := ownerOf(m)
	if !ok {
		return
	}
	pos, ok := TeleportSpot(m.Rand(), cube.PosFromVec3(o.Position()), t.CanFly, teleportWorld{m: m})
	if !ok {
		return
	}
	m.Teleport(mgl64.Vec3{float64(pos[0]) + 0.5, float64(pos[1]), float64(pos[2]) + 0.5})
	m.Navigation().Stop()
}

// teleportWorld answers canTeleportTo for m.
type teleportWorld struct{ m *javamob.Mob }

// Walkable is WalkNodeEvaluator.getPathTypeStatic(mob, pos) == WALKABLE.
func (w teleportWorld) Walkable(pos cube.Pos) bool {
	return w.m.Navigation().TypeAt(w.m, pos) == path.Walkable
}

// Leaves reports whether pos holds leaves (LeavesBlock).
func (w teleportWorld) Leaves(pos cube.Pos) bool {
	_, ok := w.m.Tx().Block(pos).(block.Leaves)
	return ok
}

// Free is level.noCollision(mob, box moved by pos - blockPosition).
func (w teleportWorld) Free(pos cube.Pos) bool {
	d := pos.Sub(cube.PosFromVec3(w.m.Position()))
	box := w.m.BBox().Translate(mgl64.Vec3{float64(d[0]), float64(d[1]), float64(d[2])})
	return !path.Collides(w.m.Tx(), box)
}

// leashed reports whether a leash holds the mob (Leashable.isLeashed; the
// leash itself is package mob/component/core's, shared by both modes).
func leashed(m *javamob.Mob) bool { return core.Leashed(m.Mob) }

// EncodeTamable and DecodeTamable save TamableAnimal's fields: "Sitting"
// (the order); the tame flag follows the owner (readAdditionalSaveData:
// tame when an owner is saved).
func (t *Tamable) EncodeTamable(data map[string]any) {
	data["Sitting"] = boolByte(t.OrderedToSit)
	data["jpets:Tame"] = boolByte(t.Tame)
}

func (t *Tamable) DecodeTamable(m *javamob.Mob, data map[string]any) {
	_, hasOwner := m.OwnerUUID()
	tame := hasOwner
	if v, ok := data["jpets:Tame"].(uint8); ok && !hasOwner {
		tame = v != 0
	}
	t.SetTame(m, tame, true)
	if v, ok := data["Sitting"].(uint8); ok {
		t.OrderedToSit = v != 0
	}
	t.SetInSittingPose(m, t.OrderedToSit)
}

func boolByte(v bool) uint8 {
	if v {
		return 1
	}
	return 0
}

// ---- SitWhenOrderedToGoal ----

// sitWhenOrdered is SitWhenOrderedToGoal (flags JUMP, MOVE).
type sitWhenOrdered struct{ t *Tamable }

func (g *sitWhenOrdered) Flags() javamob.Flags { return javamob.FlagJump | javamob.FlagMove }

func (g *sitWhenOrdered) CanUse(m *javamob.Mob) bool {
	o, ownerHere := ownerOf(m)
	var d float64
	var hurtBy bool
	if ownerHere {
		d = distSq(m.Position(), o.Position())
		hurtBy = lastHurtByMob(o)
	}
	return SitWhenOrderedTo(g.t.OrderedToSit, g.t.Tame, m.InWater(), m.OnGround(), ownerHere, d, hurtBy)
}

func (g *sitWhenOrdered) CanContinueToUse(*javamob.Mob) bool { return g.t.OrderedToSit }

func (g *sitWhenOrdered) Start(m *javamob.Mob) {
	m.Navigation().Stop()
	g.t.SetInSittingPose(m, true)
}

func (g *sitWhenOrdered) Stop(m *javamob.Mob) { g.t.SetInSittingPose(m, false) }

// lastHurtByMob is LivingEntity.getLastHurtByMob() != null: something hurt
// e within the last 100 ticks (5 s for players).
func lastHurtByMob(e world.Entity) bool {
	switch o := e.(type) {
	case *player.Player:
		_, at, ok := o.LastAttacker()
		return ok && sinceTicks(at) <= 100
	}
	if jm, ok := javamob.Of(e); ok {
		_, ok := jm.LastHurtByMob()
		return ok
	}
	return false
}

// ---- FollowOwnerGoal ----

// followOwner is FollowOwnerGoal (flags MOVE, LOOK) with Paper's change: the
// pet only looks at an owner within 16 blocks.
type followOwner struct {
	t                   *Tamable
	speed               float64
	startDist, stopDist float64
	owner               *world.EntityHandle
	timeToRecalcPath    int
	oldWaterCost        float32
}

func newFollowOwner(t *Tamable, speed, start, stop float64) *followOwner {
	return &followOwner{t: t, speed: speed, startDist: start, stopDist: stop}
}

func (g *followOwner) Flags() javamob.Flags { return javamob.FlagMove | javamob.FlagLook }

func (g *followOwner) CanUse(m *javamob.Mob) bool {
	o, ok := ownerOf(m)
	if !ok || g.t.UnableToMoveToOwner(m) {
		return false
	}
	if distSq(m.Position(), o.Position()) < g.startDist*g.startDist {
		return false
	}
	g.owner = o.H()
	return true
}

func (g *followOwner) CanContinueToUse(m *javamob.Mob) bool {
	if m.Navigation().Done() || g.t.UnableToMoveToOwner(m) {
		return false
	}
	o, ok := g.owner.Entity(m.Tx())
	return ok && distSq(m.Position(), o.Position()) > g.stopDist*g.stopDist
}

func (g *followOwner) Start(m *javamob.Mob) {
	g.timeToRecalcPath = 0
	g.oldWaterCost = m.PathfindingMalus(path.Water)
	m.SetPathfindingMalus(path.Water, 0)
}

func (g *followOwner) Stop(m *javamob.Mob) {
	g.owner = nil
	m.Navigation().Stop()
	m.SetPathfindingMalus(path.Water, g.oldWaterCost)
}

func (g *followOwner) Tick(m *javamob.Mob) {
	o, ok := g.owner.Entity(m.Tx())
	if !ok {
		return
	}
	far := g.t.ShouldTryTeleportToOwner(m)
	if !far && distSq(m.Position(), o.Position()) <= 256 {
		m.LookControl().SetLookAtEntity(o, 10, maxHeadXRot(m))
	}
	g.timeToRecalcPath--
	if g.timeToRecalcPath <= 0 {
		g.timeToRecalcPath = adjustedTickDelay(10)
		if far {
			g.t.TryToTeleportToOwner(m)
		} else {
			m.Navigation().MoveToEntity(m, o, g.speed)
		}
	}
}

// ---- OwnerHurtByTargetGoal / OwnerHurtTargetGoal ----

// ownerHurtBy is OwnerHurtByTargetGoal (hurt false: targets what last hurt
// the owner) and OwnerHurtTargetGoal (hurt true: what the owner last hit).
// Flags TARGET. Each hit is taken once (the timestamp check).
type ownerHurtBy struct {
	goal.Target
	t         *Tamable
	hurt      bool
	wants     func(m *javamob.Mob, target, owner world.Entity) bool
	candidate *world.EntityHandle
	timestamp int64
}

func newOwnerHurtBy(t *Tamable, hurt bool, wants func(m *javamob.Mob, target, owner world.Entity) bool) *ownerHurtBy {
	return &ownerHurtBy{Target: goal.Target{GoalBase: javamob.GoalBase{Control: javamob.FlagTarget}, UnseenMemoryTicks: 60}, t: t, hurt: hurt, wants: wants}
}

func (g *ownerHurtBy) CanUse(m *javamob.Mob) bool {
	if !g.t.Tame || g.t.OrderedToSit {
		return false
	}
	o, ok := ownerOf(m)
	if !ok {
		return false
	}
	var (
		cand *world.EntityHandle
		ts   int64
	)
	if g.hurt {
		cand, ts, ok = LastHurtMob(o)
	} else {
		cand, ts, ok = lastHurtByMobOf(o)
	}
	if !ok || ts == g.timestamp {
		return false
	}
	t, ok := cand.Entity(m.Tx())
	// TargetGoal.canAttack(target, TargetingConditions.DEFAULT): forCombat
	// with line of sight, within the home.
	if !ok || !javamob.ForCombat(0).Test(m, t) || !m.WithinHome(cube.PosFromVec3(t.Position())) {
		return false
	}
	if g.wants != nil && !g.wants(m, t, o) {
		return false
	}
	g.candidate = cand
	return true
}

func (g *ownerHurtBy) Start(m *javamob.Mob) {
	if t, ok := g.candidate.Entity(m.Tx()); ok {
		m.SetTarget(t)
	}
	if o, ok := ownerOf(m); ok {
		if g.hurt {
			_, g.timestamp, _ = LastHurtMob(o)
		} else {
			_, g.timestamp, _ = lastHurtByMobOf(o)
		}
	}
	g.Target.Start(m)
}
