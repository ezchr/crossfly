//go:build crossfly_full

package janimal

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob/jbrain"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The Brain behaviours and sensors of Java's brain-driven animals (goat, frog, armadillo, sniffer, axolotl,
// camel...): AnimalMakeLove, AnimalPanic, FollowTemptation, BabyFollowAdult, CountDownCooldownTicks,
// TemptingSensor and AdultSensor (net.minecraft.world.entity.ai.behavior / sensing), generic over the body.

// BrainAnimal is what the animal behaviours need from a brain-driven animal body E.
type BrainAnimal[E any] interface {
	AnimalState() *Animal
	// H is the mob's entity handle, Mem its brain's memories.
	H() *world.EntityHandle
	Mem() *jbrain.Memories
	// Open returns the animal behind h as an E (same transaction), false if it is none.
	Open(tx *world.Tx, h *world.EntityHandle) (E, bool)
	// TypeID is the entity type ("minecraft:goat").
	TypeID() string
	Position() mgl64.Vec3
	Width() float64
	Baby() bool
	OnFire() bool
	// CanMate is Animal.canMate (the class's override if any).
	CanMate(partner E) bool
	StopNavigation()
	NavigationDone() bool
	// PanicPos is the AnimalPanic position getter (LandRandomPos.getPos(mob, 5, 4) for most).
	PanicPos() (mgl64.Vec3, bool)
	// BreedWith is Animal.spawnChildFromBreeding with partner (SpawnChildFromBreeding on a Host).
	BreedWith(partner E)
	Alive() bool
	// TemptingPlayer is the player TemptingSensor picks: the nearest non-spectator within the TEMPT_RANGE
	// (TargetingConditions.forNonCombat().ignoreLineOfSight()) holding the animal's temptation in a hand and
	// not riding it.
	TemptingPlayer() (*world.EntityHandle, bool)
}

func distTo(a, b mgl64.Vec3) float64 { return a.Sub(b).LenSqr() }

func panicking(mem *jbrain.Memories) bool { return mem.Has(jbrain.MemIsPanicking.ID()) }

// lockGazeAndWalkToEachOther is BehaviorUtils.lockGazeAndWalkToEachOther.
func lockGazeAndWalkToEachOther(am, bm *jbrain.Memories, a, b *world.EntityHandle, speed float32, close int) {
	ta, tb := jbrain.TrackEntity(b, true), jbrain.TrackEntity(a, true)
	jbrain.Set(am, jbrain.MemLookTarget, jbrain.PositionTracker(ta))
	jbrain.Set(am, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: ta, Speed: speed, CloseEnough: close})
	jbrain.Set(bm, jbrain.MemLookTarget, jbrain.PositionTracker(tb))
	jbrain.Set(bm, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: tb, Speed: speed, CloseEnough: close})
}

// AnimalMakeLove is behavior.AnimalMakeLove(partnerType, speed, closeEnough): an animal in love walks to a
// visible partner of the type that can mate, and the two breed when within 3 blocks after 60-109 ticks.
func AnimalMakeLove[E BrainAnimal[E]](partnerType string, speed float32, closeEnough int) *jbrain.Behavior[E] {
	return jbrain.NewBehavior[E]("AnimalMakeLove", []jbrain.Req{
		jbrain.Present(jbrain.MemNearestVisibleLivingEntities), jbrain.Absent(jbrain.MemBreedTarget),
		jbrain.Reg(jbrain.MemWalkTarget), jbrain.Reg(jbrain.MemLookTarget), jbrain.Absent(jbrain.MemIsPanicking),
	}, 110, 110, &makeLove[E]{partnerType: partnerType, speed: speed, close: closeEnough})
}

type makeLove[E BrainAnimal[E]] struct {
	jbrain.TaskBase[E]
	partnerType string
	speed       float32
	close       int
	childAt     int64
}

func (t *makeLove[E]) partner(c *jbrain.Ctx, e E) (E, bool) {
	var zero E
	v, ok := jbrain.Get(e.Mem(), jbrain.MemNearestVisibleLivingEntities)
	if !ok {
		return zero, false
	}
	h, ok := v.FindClosest(func(h *world.EntityHandle) bool {
		if h.Type().EncodeEntity() != t.partnerType {
			return false
		}
		p, ok := e.Open(c.Tx, h)
		return ok && e.CanMate(p) && !panicking(p.Mem())
	})
	if !ok {
		return zero, false
	}
	return e.Open(c.Tx, h)
}

func (t *makeLove[E]) CanStart(c *jbrain.Ctx, e E) bool {
	if !e.AnimalState().InLove() {
		return false
	}
	_, ok := t.partner(c, e)
	return ok
}

func (t *makeLove[E]) Start(c *jbrain.Ctx, e E) {
	p, ok := t.partner(c, e)
	if !ok {
		return
	}
	jbrain.Set(e.Mem(), jbrain.MemBreedTarget, p.H())
	jbrain.Set(p.Mem(), jbrain.MemBreedTarget, e.H())
	lockGazeAndWalkToEachOther(e.Mem(), p.Mem(), e.H(), p.H(), t.speed, t.close)
	t.childAt = c.Time + int64(60+c.Rand.IntN(50))
}

func (t *makeLove[E]) target(c *jbrain.Ctx, e E) (E, bool) {
	var zero E
	h, ok := jbrain.Get(e.Mem(), jbrain.MemBreedTarget)
	if !ok || h.Type().EncodeEntity() != t.partnerType {
		return zero, false
	}
	return e.Open(c.Tx, h)
}

func (t *makeLove[E]) CanStillUse(c *jbrain.Ctx, e E) bool {
	p, ok := t.target(c, e)
	if !ok {
		return false
	}
	v, _ := jbrain.Get(e.Mem(), jbrain.MemNearestVisibleLivingEntities)
	return p.Alive() && e.CanMate(p) && v.Contains(p.H()) && c.Time <= t.childAt &&
		!panicking(e.Mem()) && !panicking(p.Mem())
}

func (t *makeLove[E]) Tick(c *jbrain.Ctx, e E) {
	p, ok := t.target(c, e)
	if !ok {
		return
	}
	lockGazeAndWalkToEachOther(e.Mem(), p.Mem(), e.H(), p.H(), t.speed, t.close)
	if distTo(e.Position(), p.Position()) < 9 && c.Time >= t.childAt {
		e.BreedWith(p)
		e.Mem().Erase(jbrain.MemBreedTarget.ID())
		p.Mem().Erase(jbrain.MemBreedTarget.ID())
	}
}

func (t *makeLove[E]) Stop(_ *jbrain.Ctx, e E) {
	e.Mem().Erase(jbrain.MemBreedTarget.ID())
	e.Mem().Erase(jbrain.MemWalkTarget.ID())
	e.Mem().Erase(jbrain.MemLookTarget.ID())
	t.childAt = 0
}

// AnimalPanic is behavior.AnimalPanic(speed): after damage of a panic_causes type (the HURT_BY memory) the
// animal runs to random spots for 100-120 ticks, to water when it burns.
func AnimalPanic[E BrainAnimal[E]](speed float32, causes func(e E, src world.DamageSource) bool) *jbrain.Behavior[E] {
	return jbrain.NewBehavior[E]("AnimalPanic", []jbrain.Req{jbrain.Reg(jbrain.MemIsPanicking), jbrain.Reg(jbrain.MemHurtBy)},
		100, 120, &animalPanic[E]{speed: speed, causes: causes})
}

type animalPanic[E BrainAnimal[E]] struct {
	jbrain.TaskBase[E]
	speed  float32
	causes func(e E, src world.DamageSource) bool
}

func (t *animalPanic[E]) CanStart(_ *jbrain.Ctx, e E) bool {
	if src, ok := jbrain.Get(e.Mem(), jbrain.MemHurtBy); ok && t.causes(e, src) {
		return true
	}
	return panicking(e.Mem())
}

func (t *animalPanic[E]) CanStillUse(*jbrain.Ctx, E) bool { return true }

func (t *animalPanic[E]) Start(_ *jbrain.Ctx, e E) {
	jbrain.Set(e.Mem(), jbrain.MemIsPanicking, true)
	e.Mem().Erase(jbrain.MemWalkTarget.ID())
	e.StopNavigation()
}

func (t *animalPanic[E]) Stop(_ *jbrain.Ctx, e E) { e.Mem().Erase(jbrain.MemIsPanicking.ID()) }

func (t *animalPanic[E]) Tick(c *jbrain.Ctx, e E) {
	if !e.NavigationDone() {
		return
	}
	var to mgl64.Vec3
	found := false
	if e.OnFire() && c.Tx != nil {
		if p, ok := lookForWaterWide(c.Tx, e.Position(), e.Width()); ok {
			to, found = mgl64.Vec3{float64(p[0]) + 0.5, float64(p[1]), float64(p[2]) + 0.5}, true
		}
	}
	if !found {
		to, found = e.PanicPos()
	}
	if found {
		jbrain.Set(e.Mem(), jbrain.MemWalkTarget, jbrain.WalkTarget{Target: jbrain.TrackVec(to), Speed: t.speed})
	}
}

// lookForWaterWide is AnimalPanic.lookForWater: like PanicGoal's, but a mob two blocks wide needs the
// blocks south, east and south-east of the water to be water too.
func lookForWaterWide(tx *world.Tx, pos mgl64.Vec3, width float64) (cube.Pos, bool) {
	origin := cube.PosFromVec3(pos)
	if len(tx.Block(origin).Model().BBox(origin, tx)) != 0 {
		return cube.Pos{}, false
	}
	wide := int(width+0.999999) == 2
	var found cube.Pos
	ok := false
	ManhattanOrdered(origin, 5, 1, 5, 11, func(p cube.Pos) bool {
		if !isWater(tx, p) {
			return true
		}
		if wide && !(isWater(tx, p.Add(cube.Pos{0, 0, 1})) && isWater(tx, p.Add(cube.Pos{1, 0, 0})) && isWater(tx, p.Add(cube.Pos{1, 0, 1}))) {
			return true
		}
		found, ok = p, true
		return false
	})
	return found, ok
}

// FollowTemptation is behavior.FollowTemptation(speed, closeEnough): the animal walks to the tempting player
// and looks at it; when it stops it may not be tempted for 100 ticks.
func FollowTemptation[E BrainAnimal[E]](speed func(e E) float32, closeEnough func(e E) float64) *jbrain.Behavior[E] {
	if closeEnough == nil {
		closeEnough = func(E) float64 { return 2.5 }
	}
	return jbrain.NewBehavior[E]("FollowTemptation", []jbrain.Req{
		jbrain.Reg(jbrain.MemLookTarget), jbrain.Reg(jbrain.MemWalkTarget), jbrain.Absent(jbrain.MemTemptationCooldownTicks),
		jbrain.Present(jbrain.MemTemptingPlayer), jbrain.Absent(jbrain.MemBreedTarget), jbrain.Absent(jbrain.MemIsPanicking),
	}, jbrain.DefaultDuration, jbrain.DefaultDuration, &followTemptation[E]{speed: speed, close: closeEnough})
}

type followTemptation[E BrainAnimal[E]] struct {
	jbrain.TaskBase[E]
	speed func(e E) float32
	close func(e E) float64
}

// TimedOut is FollowTemptation.timedOut: never.
func (t *followTemptation[E]) TimedOut(int64, int64) bool { return false }

func (t *followTemptation[E]) CanStillUse(_ *jbrain.Ctx, e E) bool {
	m := e.Mem()
	return m.Has(jbrain.MemTemptingPlayer.ID()) && !m.Has(jbrain.MemBreedTarget.ID()) && !panicking(m)
}

func (t *followTemptation[E]) Stop(_ *jbrain.Ctx, e E) {
	jbrain.Set(e.Mem(), jbrain.MemTemptationCooldownTicks, 100)
	e.Mem().Erase(jbrain.MemWalkTarget.ID())
	e.Mem().Erase(jbrain.MemLookTarget.ID())
}

func (t *followTemptation[E]) Tick(c *jbrain.Ctx, e E) {
	h, ok := jbrain.Get(e.Mem(), jbrain.MemTemptingPlayer)
	if !ok {
		return
	}
	jbrain.Set(e.Mem(), jbrain.MemLookTarget, jbrain.PositionTracker(jbrain.TrackEntity(h, true)))
	if c.Tx == nil {
		return
	}
	p, ok := h.Entity(c.Tx)
	if !ok {
		return
	}
	pp := p.Position()
	ce := t.close(e)
	if distTo(e.Position(), pp) < ce*ce {
		e.Mem().Erase(jbrain.MemWalkTarget.ID())
		return
	}
	jbrain.Set(e.Mem(), jbrain.MemWalkTarget, jbrain.WalkTarget{Target: jbrain.TrackEntity(h, false), Speed: t.speed(e), CloseEnough: 2})
}

// BabyFollowAdult is behavior.BabyFollowAdult.create(UniformInt(minRange, maxRange), speed): a baby walks to
// the nearest visible adult of its type when it is within maxRange+1 but not within minRange blocks.
func BabyFollowAdult[E BrainAnimal[E]](minRange, maxRange int, speed func(e E) float32) *jbrain.OneShot[E] {
	return jbrain.Trigger[E]("BabyFollowAdult", []jbrain.Req{
		jbrain.Present(jbrain.MemNearestVisibleAdult), jbrain.Reg(jbrain.MemLookTarget), jbrain.Absent(jbrain.MemWalkTarget),
	}, func(c *jbrain.Ctx, e E) bool {
		if !e.Baby() {
			return false
		}
		h, ok := jbrain.Get(e.Mem(), jbrain.MemNearestVisibleAdult)
		if !ok || c.Tx == nil {
			return false
		}
		adult, ok := h.Entity(c.Tx)
		if !ok {
			return false
		}
		d := distTo(e.Position(), adult.Position())
		hi, lo := float64(maxRange+1), float64(minRange)
		if !(d < hi*hi) || d < lo*lo {
			return false
		}
		jbrain.Set(e.Mem(), jbrain.MemLookTarget, jbrain.PositionTracker(jbrain.TrackEntity(h, true)))
		jbrain.Set(e.Mem(), jbrain.MemWalkTarget, jbrain.WalkTarget{Target: jbrain.TrackEntity(h, false), Speed: speed(e), CloseEnough: minRange - 1})
		return true
	})
}

// CountDownCooldownTicks is behavior.CountDownCooldownTicks(memory): the int memory counts down to 0 and is
// erased.
func CountDownCooldownTicks[E any](mem jbrain.Memory[int]) *jbrain.Behavior[E] {
	return jbrain.NewBehavior[E]("CountDownCooldownTicks", []jbrain.Req{jbrain.Present(mem)}, jbrain.DefaultDuration,
		jbrain.DefaultDuration, &countDown[E]{mem: mem})
}

type countDown[E any] struct {
	jbrain.TaskBase[E]
	mem jbrain.Memory[int]
}

func (t *countDown[E]) TimedOut(int64, int64) bool { return false }
func (t *countDown[E]) CanStillUse(c *jbrain.Ctx, _ E) bool {
	v, ok := jbrain.Get(c.Mem, t.mem)
	return ok && v > 0
}
func (t *countDown[E]) Tick(c *jbrain.Ctx, _ E) {
	if v, ok := jbrain.Get(c.Mem, t.mem); ok {
		jbrain.Set(c.Mem, t.mem, v-1)
	}
}
func (t *countDown[E]) Stop(c *jbrain.Ctx, _ E) { c.Mem.Erase(t.mem.ID()) }

// TemptingSensor is sensing.TemptingSensor (forAnimal): TEMPTING_PLAYER is the player TemptingPlayer finds.
func TemptingSensor[E BrainAnimal[E]]() jbrain.Sensor[E] { return temptingSensor[E]{} }

type temptingSensor[E BrainAnimal[E]] struct{}

func (temptingSensor[E]) Requires() []jbrain.MemoryID {
	return []jbrain.MemoryID{jbrain.MemTemptingPlayer.ID()}
}

func (temptingSensor[E]) Sense(c *jbrain.Ctx, e E) {
	h, ok := e.TemptingPlayer()
	jbrain.SetOpt(c.Mem, jbrain.MemTemptingPlayer, h, ok)
}

// AdultSensor is sensing.AdultSensor: NEAREST_VISIBLE_ADULT is the closest visible adult of the body's type.
func AdultSensor[E BrainAnimal[E]]() jbrain.Sensor[E] { return adultSensor[E]{} }

type adultSensor[E BrainAnimal[E]] struct{}

func (adultSensor[E]) Requires() []jbrain.MemoryID {
	return []jbrain.MemoryID{jbrain.MemNearestVisibleAdult.ID(), jbrain.MemNearestVisibleLivingEntities.ID()}
}

func (adultSensor[E]) Sense(c *jbrain.Ctx, e E) {
	v, ok := jbrain.Get(c.Mem, jbrain.MemNearestVisibleLivingEntities)
	if !ok {
		return
	}
	id := e.TypeID()
	h, found := v.FindClosest(func(h *world.EntityHandle) bool {
		if h.Type().EncodeEntity() != id || c.Tx == nil {
			return false
		}
		o, ok := e.Open(c.Tx, h)
		return ok && !o.Baby()
	})
	jbrain.SetOpt(c.Mem, jbrain.MemNearestVisibleAdult, h, found)
}
