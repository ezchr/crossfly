//go:build crossfly_full

package jmob

import (
	"math"
	"slices"
	"sort"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/df-mc/dragonfly/server/mob/jbrain"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Task is a long running behaviour of a Java mob (embed TaskBase).
type Task = jbrain.Task[*javamob.Mob]

// TaskBase gives a Task Java's Behavior defaults.
type TaskBase = jbrain.TaskBase[*javamob.Mob]

// Trigger builds a OneShot behaviour of a Java mob (BehaviorBuilder.create).
func Trigger(name string, reqs []jbrain.Req, fn func(c *jbrain.Ctx, m *javamob.Mob) bool) *jbrain.OneShot[*javamob.Mob] {
	return jbrain.Trigger(name, reqs, fn)
}

// Behaviour wraps a Task (new Behavior(entryCondition, min, max)).
func Behaviour(name string, entry []jbrain.Req, minDuration, maxDuration int, t Task) *jbrain.Behavior[*javamob.Mob] {
	return jbrain.NewBehavior(name, entry, minDuration, maxDuration, t)
}

// BlockPos is Entity.blockPosition.
func BlockPos(m *javamob.Mob) cube.Pos { return jbrain.Containing(m.Position()) }

func manhattan(a, b cube.Pos) int {
	return iabs(a[0]-b[0]) + iabs(a[1]-b[1]) + iabs(a[2]-b[2])
}

func iabs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// CloserToCenterThan is BlockPos.closerToCenterThan(pos, dist).
func CloserToCenterThan(b cube.Pos, p mgl64.Vec3, dist float64) bool {
	c := mgl64.Vec3{float64(b[0]) + .5, float64(b[1]) + .5, float64(b[2]) + .5}
	return c.Sub(p).LenSqr() < dist*dist
}

// SetWalkAndLookTargetMemories is BehaviorUtils.setWalkAndLookTargetMemories.
func SetWalkAndLookTargetMemories(mem *jbrain.Memories, target jbrain.PositionTracker, speed float32, closeEnough int) {
	jbrain.Set(mem, jbrain.MemLookTarget, target)
	jbrain.Set(mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: target, Speed: speed, CloseEnough: closeEnough})
}

// LookAtEntity is BehaviorUtils.lookAtEntity.
func LookAtEntity(mem *jbrain.Memories, target *world.EntityHandle) {
	jbrain.Set[jbrain.PositionTracker](mem, jbrain.MemLookTarget, jbrain.TrackEntity(target, true))
}

// LockGazeAndWalkToEachOther is BehaviorUtils.lockGazeAndWalkToEachOther.
func LockGazeAndWalkToEachOther(tx *world.Tx, a, b *javamob.Mob, speed float32, closeEnough int) {
	ma, mb := Mem(a), Mem(b)
	if ma == nil || mb == nil {
		return
	}
	LookAtEntity(ma, b.H())
	LookAtEntity(mb, a.H())
	SetWalkAndLookTargetMemories(ma, jbrain.TrackEntity(b.H(), true), speed, closeEnough)
	SetWalkAndLookTargetMemories(mb, jbrain.TrackEntity(a.H(), true), speed, closeEnough)
}

// EntityIsVisible is BehaviorUtils.entityIsVisible.
func EntityIsVisible(c *jbrain.Ctx, h *world.EntityHandle) bool {
	v, ok := jbrain.Get(c.Mem, jbrain.MemNearestVisibleLivingEntities)
	return ok && v.Contains(c.Tx, h)
}

// TargetIsValid is BehaviorUtils.targetIsValid: the memory holds an alive,
// visible entity of type id.
func TargetIsValid(c *jbrain.Ctx, mem jbrain.Memory[*world.EntityHandle], id string) bool {
	h, ok := jbrain.Get(c.Mem, mem)
	if !ok {
		return false
	}
	e, ok := h.Entity(c.Tx)
	return ok && javamob.IsType(e, id) && IsAlive(e) && EntityIsVisible(c, h)
}

// moveToTargetSink is MoveToTargetSink: walk to WALK_TARGET.
type moveToTargetSink struct {
	TaskBase
	cooldown int
	path     *path.Path
	lastPos  cube.Pos
	hasLast  bool
	speed    float32
}

// MoveToTargetSink is new MoveToTargetSink(minTimeout, maxTimeout)
// (150, 250 by default).
func MoveToTargetSink(minTimeout, maxTimeout int) Control {
	return Behaviour("MoveToTargetSink", []jbrain.Req{jbrain.Reg(jbrain.MemCantReachWalkTargetSince), jbrain.Absent(jbrain.MemPath), jbrain.Present(jbrain.MemWalkTarget)}, minTimeout, maxTimeout, &moveToTargetSink{})
}

func (s *moveToTargetSink) reached(c *jbrain.Ctx, m *javamob.Mob, wt jbrain.WalkTarget) bool {
	return manhattan(wt.Target.BlockPos(c.Tx), BlockPos(m)) <= wt.CloseEnough
}

func (s *moveToTargetSink) CanStart(c *jbrain.Ctx, m *javamob.Mob) bool {
	if s.cooldown > 0 {
		s.cooldown--
		return false
	}
	wt, _ := jbrain.Get(c.Mem, jbrain.MemWalkTarget)
	reached := s.reached(c, m, wt)
	if !reached && s.tryComputePath(c, m, wt) {
		s.lastPos, s.hasLast = wt.Target.BlockPos(c.Tx), true
		return true
	}
	c.Mem.Erase(jbrain.MemWalkTarget.ID())
	if reached {
		c.Mem.Erase(jbrain.MemCantReachWalkTargetSince.ID())
	}
	return false
}

func walkTargetSpectator(c *jbrain.Ctx, wt jbrain.WalkTarget) bool {
	t, ok := wt.Target.(*jbrain.EntityTracker)
	if !ok {
		return false
	}
	e, ok := t.Handle.Entity(c.Tx)
	if !ok {
		return false
	}
	p, ok := e.(*player.Player)
	return ok && p.GameMode() == world.GameModeSpectator
}

func (s *moveToTargetSink) CanStillUse(c *jbrain.Ctx, m *javamob.Mob) bool {
	if s.path == nil || !s.hasLast {
		return false
	}
	wt, ok := jbrain.Get(c.Mem, jbrain.MemWalkTarget)
	return !m.Navigation().Done() && ok && !s.reached(c, m, wt) && !walkTargetSpectator(c, wt)
}

func (s *moveToTargetSink) Stop(c *jbrain.Ctx, m *javamob.Mob) {
	if wt, ok := jbrain.Get(c.Mem, jbrain.MemWalkTarget); ok && !s.reached(c, m, wt) && m.Navigation().Stuck() {
		s.cooldown = c.Rand.IntN(40)
	}
	m.Navigation().Stop()
	c.Mem.Erase(jbrain.MemWalkTarget.ID())
	c.Mem.Erase(jbrain.MemPath.ID())
	s.path = nil
}

func (s *moveToTargetSink) Start(c *jbrain.Ctx, m *javamob.Mob) {
	jbrain.Set[any](c.Mem, jbrain.MemPath, s.path)
	m.Navigation().MoveAlong(m, s.path, float64(s.speed))
}

func (s *moveToTargetSink) Tick(c *jbrain.Ctx, m *javamob.Mob) {
	p := m.Navigation().Path()
	if s.path != p {
		s.path = p
		jbrain.Set[any](c.Mem, jbrain.MemPath, p)
	}
	if p != nil && s.hasLast {
		wt, _ := jbrain.Get(c.Mem, jbrain.MemWalkTarget)
		cur := wt.Target.BlockPos(c.Tx)
		if distSqPos(cur, s.lastPos) > 4 && s.tryComputePath(c, m, wt) {
			s.lastPos = cur
			s.Start(c, m)
		}
	}
}

func distSqPos(a, b cube.Pos) float64 {
	dx, dy, dz := float64(a[0]-b[0]), float64(a[1]-b[1]), float64(a[2]-b[2])
	return dx*dx + dy*dy + dz*dz
}

func (s *moveToTargetSink) tryComputePath(c *jbrain.Ctx, m *javamob.Mob, wt jbrain.WalkTarget) bool {
	target := wt.Target.BlockPos(c.Tx)
	s.path = m.Navigation().CreatePath(m, target, 0)
	s.speed = wt.Speed
	if s.reached(c, m, wt) {
		c.Mem.Erase(jbrain.MemCantReachWalkTargetSince.ID())
		return false
	}
	if s.path != nil && s.path.CanReach() {
		c.Mem.Erase(jbrain.MemCantReachWalkTargetSince.ID())
	} else if !c.Mem.Has(jbrain.MemCantReachWalkTargetSince.ID()) {
		jbrain.Set(c.Mem, jbrain.MemCantReachWalkTargetSince, c.Time)
	}
	if s.path != nil {
		return true
	}
	bottom := mgl64.Vec3{float64(target[0]) + .5, float64(target[1]), float64(target[2]) + .5}
	if step, ok := DefaultRandomPosTowards(m, 10, 7, bottom, math.Pi/2); ok {
		s.path = m.Navigation().CreatePath(m, jbrain.Containing(step), 0)
		return s.path != nil
	}
	return false
}

// lookAtTargetSink is LookAtTargetSink.
type lookAtTargetSink struct{ TaskBase }

// LookAtTargetSink is new LookAtTargetSink(min, max).
func LookAtTargetSink(minDuration, maxDuration int) Control {
	return Behaviour("LookAtTargetSink", []jbrain.Req{jbrain.Present(jbrain.MemLookTarget)}, minDuration, maxDuration, lookAtTargetSink{})
}

func (lookAtTargetSink) CanStillUse(c *jbrain.Ctx, m *javamob.Mob) bool {
	t, ok := jbrain.Get(c.Mem, jbrain.MemLookTarget)
	return ok && t.VisibleBy(c.Tx, c.Mem)
}

func (lookAtTargetSink) Stop(c *jbrain.Ctx, m *javamob.Mob) { c.Mem.Erase(jbrain.MemLookTarget.ID()) }

func (lookAtTargetSink) Tick(c *jbrain.Ctx, m *javamob.Mob) {
	if t, ok := jbrain.Get(c.Mem, jbrain.MemLookTarget); ok {
		m.LookControl().SetLookAt(m, t.Position(c.Tx))
	}
}

// SetEntityLookTarget is SetEntityLookTarget.create(predicate, maxDist): look
// at the closest visible entity passing match within maxDist.
func SetEntityLookTarget(match func(e world.Entity) bool, maxDist float64) *jbrain.OneShot[*javamob.Mob] {
	maxSq := maxDist * maxDist
	return Trigger("SetEntityLookTarget", []jbrain.Req{jbrain.Absent(jbrain.MemLookTarget), jbrain.Present(jbrain.MemNearestVisibleLivingEntities)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		h, ok := NearestVisible(c, func(e world.Entity) bool {
			return match(e) && distSq(e.Position(), m.Position()) <= maxSq && !isPassengerOf(m, e)
		})
		if !ok {
			return false
		}
		jbrain.Set[jbrain.PositionTracker](c.Mem, jbrain.MemLookTarget, jbrain.TrackEntity(h, true))
		return true
	})
}

func isPassengerOf(m *javamob.Mob, e world.Entity) bool {
	r, ok := e.(interface{ RidingEntity() (world.Entity, bool) })
	if !ok {
		return false
	}
	v, ok := r.RidingEntity()
	return ok && v.H() == m.H()
}

// OfType matches entities of a type identifier ("minecraft:villager").
func OfType(id string) func(world.Entity) bool {
	return func(e world.Entity) bool { return javamob.IsType(e, id) }
}

// OfCategory matches Java mobs of a MobCategory name ("creature").
func OfCategory(name string) func(world.Entity) bool {
	return func(e world.Entity) bool {
		jm, ok := javamob.Of(e)
		return ok && jm.JavaType().Category.Name == name
	}
}

// doNothing is DoNothing: run for a random time, do nothing.
type doNothing struct {
	min, max int
	state    jbrain.RunState
	end      int64
}

// DoNothing is new DoNothing(min, max).
func DoNothing(minDuration, maxDuration int) Control {
	return &doNothing{min: minDuration, max: maxDuration}
}

func (d *doNothing) State() jbrain.RunState              { return d.state }
func (d *doNothing) RequiredMemories() []jbrain.MemoryID { return nil }
func (d *doNothing) Name() string                        { return "DoNothing" }
func (d *doNothing) TryStart(c *jbrain.Ctx, _ *javamob.Mob) bool {
	d.state = jbrain.Running
	d.end = c.Time + int64(d.min+c.Rand.IntN(d.max+1-d.min))
	return true
}
func (d *doNothing) TickOrStop(c *jbrain.Ctx, m *javamob.Mob) {
	if c.Time > d.end {
		d.DoStop(c, m)
	}
}
func (d *doNothing) DoStop(*jbrain.Ctx, *javamob.Mob) { d.state = jbrain.Stopped }

// swim is Swim: jump in water or lava with a chance per tick.
type swim struct {
	TaskBase
	chance float32
}

// Swim is new Swim(chance).
func Swim(chance float32) Control {
	return Behaviour("Swim", nil, jbrain.DefaultDuration, jbrain.DefaultDuration, &swim{chance: chance})
}

func (s *swim) CanStart(_ *jbrain.Ctx, m *javamob.Mob) bool {
	return (m.InFloatableFluid() && m.FluidHeight(false) > m.FluidJumpThreshold()) || m.InLava()
}
func (s *swim) CanStillUse(c *jbrain.Ctx, m *javamob.Mob) bool { return s.CanStart(c, m) }
func (s *swim) Tick(c *jbrain.Ctx, m *javamob.Mob) {
	if m.Rand().Float32() < s.chance {
		m.JumpControl().Jump()
	}
}

// UpdateActivityFromSchedule is UpdateActivityFromSchedule.create.
func UpdateActivityFromSchedule() *jbrain.OneShot[*javamob.Mob] {
	return Trigger("UpdateActivityFromSchedule", nil, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		if b, ok := Of(m); ok {
			b.UpdateActivityFromSchedule(c)
		}
		return true
	})
}

// SetWalkTargetFromLookTarget is SetWalkTargetFromLookTarget.create(speed,
// closeEnough): walk to what the mob looks at.
func SetWalkTargetFromLookTarget(speed float32, closeEnough int) *jbrain.OneShot[*javamob.Mob] {
	return Trigger("SetWalkTargetFromLookTarget", []jbrain.Req{jbrain.Absent(jbrain.MemWalkTarget), jbrain.Present(jbrain.MemLookTarget)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		t, _ := jbrain.Get(c.Mem, jbrain.MemLookTarget)
		jbrain.Set(c.Mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: t, Speed: speed, CloseEnough: closeEnough})
		return true
	})
}

// InteractWith is InteractWith.of(type, maxDist, selfFilter, targetFilter,
// memory, speed, closeEnough): pick the closest visible entity of type id
// within maxDist and walk to it.
func InteractWith(id string, maxDist int, self func(m *javamob.Mob) bool, target func(e world.Entity) bool, mem jbrain.Memory[*world.EntityHandle], speed float32, closeEnough int) *jbrain.OneShot[*javamob.Mob] {
	maxSq := float64(maxDist * maxDist)
	return Trigger("InteractWith", []jbrain.Req{jbrain.Reg(jbrain.MemLookTarget), jbrain.Absent(jbrain.MemWalkTarget), jbrain.Absent(mem), jbrain.Present(jbrain.MemNearestVisibleLivingEntities)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		if self != nil && !self(m) {
			return false
		}
		h, ok := NearestVisible(c, func(e world.Entity) bool {
			return distSq(e.Position(), m.Position()) <= maxSq && javamob.IsType(e, id) && (target == nil || target(e))
		})
		if !ok {
			return false
		}
		jbrain.Set(c.Mem, mem, h)
		tr := jbrain.TrackEntity(h, true)
		jbrain.Set[jbrain.PositionTracker](c.Mem, jbrain.MemLookTarget, tr)
		jbrain.Set(c.Mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: tr, Speed: speed, CloseEnough: closeEnough})
		return true
	})
}

// SetLookAndInteract is SetLookAndInteract.create(type, interactionRange):
// pick an entity of the type within range to interact with.
func SetLookAndInteract(id string, interactionRange int) *jbrain.OneShot[*javamob.Mob] {
	rSq := float64(interactionRange * interactionRange)
	return Trigger("SetLookAndInteract", []jbrain.Req{jbrain.Reg(jbrain.MemLookTarget), jbrain.Absent(jbrain.MemInteractionTarget), jbrain.Present(jbrain.MemNearestVisibleLivingEntities)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		h, ok := NearestVisible(c, func(e world.Entity) bool {
			return distSq(e.Position(), m.Position()) <= rSq && javamob.IsType(e, id)
		})
		if !ok {
			return false
		}
		jbrain.Set(c.Mem, jbrain.MemInteractionTarget, h)
		jbrain.Set[jbrain.PositionTracker](c.Mem, jbrain.MemLookTarget, jbrain.TrackEntity(h, true))
		return true
	})
}

// SetWalkTargetAwayFromEntity is SetWalkTargetAwayFrom.entity(memory, speed,
// desiredDistance, interruptCurrentWalk).
func SetWalkTargetAwayFromEntity(mem jbrain.Memory[*world.EntityHandle], speed float32, desired int, interrupt bool) *jbrain.OneShot[*javamob.Mob] {
	return Trigger("SetWalkTargetAwayFrom", []jbrain.Req{jbrain.Reg(jbrain.MemWalkTarget), jbrain.Present(mem)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		cur, hasCur := jbrain.Get(c.Mem, jbrain.MemWalkTarget)
		if hasCur && !interrupt {
			return false
		}
		h, _ := jbrain.Get(c.Mem, mem)
		e, ok := h.Entity(c.Tx)
		if !ok {
			return false
		}
		body, avoid := m.Position(), e.Position()
		if distSq(body, avoid) >= float64(desired*desired) {
			return false
		}
		if hasCur && cur.Speed == speed {
			dir := cur.Target.Position(c.Tx).Sub(body)
			if dir.Dot(avoid.Sub(body)) < 0 {
				return false
			}
		}
		for range 10 {
			if flee, ok := m.LandRandomPosAway(16, 7, avoid); ok {
				jbrain.Set(c.Mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: jbrain.TrackVec(flee), Speed: speed})
				break
			}
		}
		return true
	})
}

// WeightedTrigger is a trigger with its weight in a TriggerGate.
type WeightedTrigger struct {
	T      *jbrain.OneShot[*javamob.Mob]
	Weight int
}

// TriggerOneShuffled is TriggerGate.triggerOneShuffled: shuffle by weight
// (ShufflingList) and fire triggers until one fires; it always reports true.
func TriggerOneShuffled(children ...WeightedTrigger) *jbrain.OneShot[*javamob.Mob] {
	list := slices.Clone(children)
	keys := make([]float64, len(list))
	var reqs []jbrain.Req
	return Trigger("TriggerGate", reqs, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		for i := range list {
			keys[i] = -math.Pow(float64(c.Rand.Float32()), 1/float64(list[i].Weight))
		}
		idx := make([]int, len(list))
		for i := range idx {
			idx[i] = i
		}
		sort.SliceStable(idx, func(a, b int) bool { return keys[idx[a]] < keys[idx[b]] })
		shuffled := make([]WeightedTrigger, len(list))
		for i, j := range idx {
			shuffled[i] = list[j]
		}
		list = shuffled
		for _, t := range list {
			if t.T.Fire(c, m) {
				break
			}
		}
		return true
	})
}
