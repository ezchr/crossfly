//go:build crossfly_full

package goal

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// MeleeAttack is MeleeAttackGoal: chase the target and hit it once a second when in reach.
type MeleeAttack struct {
	javamob.GoalBase
	Speed float64
	// FollowEvenIfNotSeen keeps chasing a target out of sight (followingTargetEvenIfNotSeen).
	FollowEvenIfNotSeen bool

	path             *path.Path
	pathed           mgl64.Vec3
	ticksUntilRepath int
	ticksUntilAttack int
	lastCanUseCheck  int64
	// OnTick, if set, runs at the end of Tick (ZombieAttackGoal raises the arms).
	OnTick func(m *javamob.Mob, g *MeleeAttack)
	// OnStart and OnStop, if set, run after Start and Stop.
	OnStart, OnStop func(m *javamob.Mob, g *MeleeAttack)
	// Attack, if set, replaces checkAndPerformAttack (a spider's reach, a creeper that only swells).
	Attack func(m *javamob.Mob, g *MeleeAttack, target world.Entity)
}

// NewMeleeAttack returns MeleeAttackGoal(mob, speed, followEvenIfNotSeen).
func NewMeleeAttack(speed float64, followEvenIfNotSeen bool) *MeleeAttack {
	return &MeleeAttack{GoalBase: javamob.GoalBase{Control: javamob.FlagMove | javamob.FlagLook}, Speed: speed, FollowEvenIfNotSeen: followEvenIfNotSeen, lastCanUseCheck: -100}
}

func alive(e world.Entity) bool {
	l, ok := e.(entity.Living)
	return !ok || !l.Dead()
}

func (g *MeleeAttack) CanUse(m *javamob.Mob) bool {
	now := m.Tx().CurrentTick()
	if now-g.lastCanUseCheck < 20 {
		return false
	}
	g.lastCanUseCheck = now
	t, ok := m.Target()
	if !ok || !alive(t) {
		return false
	}
	g.path = m.Navigation().CreatePathToEntity(m, t, 0)
	return g.path != nil || m.IsWithinMeleeAttackRange(t)
}

func (g *MeleeAttack) CanContinueToUse(m *javamob.Mob) bool {
	t, ok := m.Target()
	if !ok || !alive(t) {
		return false
	}
	if !g.FollowEvenIfNotSeen {
		return !m.Navigation().Done()
	}
	return m.WithinHome(cube.PosFromVec3(t.Position()))
}

func (g *MeleeAttack) Start(m *javamob.Mob) {
	m.Navigation().MoveAlong(m, g.path, g.Speed)
	m.SetAggressive(true)
	g.ticksUntilRepath, g.ticksUntilAttack = 0, 0
	if g.OnStart != nil {
		g.OnStart(m, g)
	}
}

func (g *MeleeAttack) Stop(m *javamob.Mob) {
	// A creative or spectator target is dropped (EntitySelector.NO_CREATIVE_OR_SPECTATOR).
	if _, ok := m.Mob.Target(); ok {
		if _, valid := m.Target(); !valid {
			m.SetTarget(nil)
		}
	}
	m.SetAggressive(false)
	m.Navigation().Stop()
	if g.OnStop != nil {
		g.OnStop(m, g)
	}
}

func (g *MeleeAttack) RequiresUpdateEveryTick() bool { return true }

func (g *MeleeAttack) Tick(m *javamob.Mob) {
	t, ok := m.Target()
	if !ok {
		return
	}
	m.LookControl().SetLookAtEntity(t, 30, 30)
	g.ticksUntilRepath = max(g.ticksUntilRepath-1, 0)
	tp := t.Position()
	if (g.FollowEvenIfNotSeen || m.Sensing().HasLineOfSight(m, t)) && g.ticksUntilRepath <= 0 &&
		(g.pathed == (mgl64.Vec3{}) || tp.Sub(g.pathed).LenSqr() >= 1 || m.Rand().Float32() < 0.05) {
		g.pathed = tp
		g.ticksUntilRepath = 4 + m.Rand().IntN(7)
		d := tp.Sub(m.Position()).LenSqr()
		if d > 1024 {
			g.ticksUntilRepath += 10
		} else if d > 256 {
			g.ticksUntilRepath += 5
		}
		if !m.Navigation().MoveToEntity(m, t, g.Speed) {
			g.ticksUntilRepath += 15
		}
		g.ticksUntilRepath = javamob.AdjustedTickDelay(g, g.ticksUntilRepath)
	}
	g.ticksUntilAttack = max(g.ticksUntilAttack-1, 0)
	if g.Attack != nil {
		g.Attack(m, g, t)
	} else {
		g.CheckAndPerformAttack(m, t)
	}
	if g.OnTick != nil {
		g.OnTick(m, g)
	}
}

// CheckAndPerformAttack is checkAndPerformAttack: hit the target if the cooldown is over, it is in reach and in
// sight.
func (g *MeleeAttack) CheckAndPerformAttack(m *javamob.Mob, t world.Entity) {
	if g.ticksUntilAttack <= 0 && m.IsWithinMeleeAttackRange(t) && m.Sensing().HasLineOfSight(m, t) {
		g.ResetAttackCooldown()
		m.Swing()
		m.DoHurtTarget(t)
	}
}

// ResetAttackCooldown is resetAttackCooldown: 20 ticks.
func (g *MeleeAttack) ResetAttackCooldown() { g.ticksUntilAttack = javamob.AdjustedTickDelay(g, 20) }

// TicksUntilNextAttack is getTicksUntilNextAttack, AttackInterval getAttackInterval.
func (g *MeleeAttack) TicksUntilNextAttack() int { return g.ticksUntilAttack }
func (g *MeleeAttack) AttackInterval() int       { return javamob.AdjustedTickDelay(g, 20) }

// LeapAtTarget is LeapAtTargetGoal: jump at a target 2-4 blocks away.
type LeapAtTarget struct {
	javamob.GoalBase
	YD     float64
	target *world.EntityHandle
}

// NewLeapAtTarget returns LeapAtTargetGoal(mob, yd).
func NewLeapAtTarget(yd float32) *LeapAtTarget {
	return &LeapAtTarget{GoalBase: javamob.GoalBase{Control: javamob.FlagJump | javamob.FlagMove}, YD: float64(yd)}
}

func (g *LeapAtTarget) CanUse(m *javamob.Mob) bool {
	if _, ok := m.ControllingPassenger(); ok {
		return false
	}
	t, ok := m.Target()
	if !ok {
		return false
	}
	g.target = t.H()
	d := t.Position().Sub(m.Position()).LenSqr()
	if d < 4 || d > 16 || !m.OnGround() {
		return false
	}
	return m.Rand().IntN(javamob.ReducedTickDelay(5)) == 0
}
func (g *LeapAtTarget) CanContinueToUse(m *javamob.Mob) bool { return !m.OnGround() }
func (g *LeapAtTarget) Start(m *javamob.Mob) {
	t, ok := g.target.Entity(m.Tx())
	if !ok {
		return
	}
	v := m.Velocity()
	d := t.Position().Sub(m.Position())
	d[1] = 0
	if d.LenSqr() > 1e-7 {
		d = d.Normalize().Mul(0.4).Add(v.Mul(0.2))
	}
	m.SetVelocity(mgl64.Vec3{d[0], g.YD, d[2]})
}

// AvoidEntity is AvoidEntityGoal: run from the nearest matching entity within MaxDist, faster when it is within 7.
type AvoidEntity struct {
	javamob.GoalBase
	Match        func(world.Entity) bool
	MaxDist      float64
	Walk, Sprint float64
	avoid        *world.EntityHandle
	path         *path.Path
}

// NewAvoidEntity returns AvoidEntityGoal(mob, class, maxDist, walk, sprint) for the entities match accepts.
func NewAvoidEntity(match func(world.Entity) bool, maxDist float32, walk, sprint float64) *AvoidEntity {
	return &AvoidEntity{GoalBase: javamob.GoalBase{Control: javamob.FlagMove}, Match: match, MaxDist: float64(maxDist), Walk: walk, Sprint: sprint}
}

func (g *AvoidEntity) CanUse(m *javamob.Mob) bool {
	c := javamob.ForCombat(g.MaxDist)
	box := m.BBox().GrowVec3(mgl64.Vec3{g.MaxDist, 3, g.MaxDist})
	e, ok := m.NearestEntity(box, g.Match, c)
	if !ok {
		return false
	}
	pos, ok := m.DefaultRandomPosAway(16, 7, e.Position())
	if !ok {
		return false
	}
	if pos.Sub(e.Position()).LenSqr() < m.Position().Sub(e.Position()).LenSqr() {
		return false
	}
	g.path = m.Navigation().CreatePath(m, cube.PosFromVec3(pos), 0)
	g.avoid = e.H()
	return g.path != nil
}
func (g *AvoidEntity) CanContinueToUse(m *javamob.Mob) bool { return !m.Navigation().Done() }
func (g *AvoidEntity) Start(m *javamob.Mob)                 { m.Navigation().MoveAlong(m, g.path, g.Walk) }
func (g *AvoidEntity) Stop(*javamob.Mob)                    { g.avoid = nil }
func (g *AvoidEntity) Tick(m *javamob.Mob) {
	e, ok := g.avoid.Entity(m.Tx())
	if ok && e.Position().Sub(m.Position()).LenSqr() < 49 {
		m.Navigation().SetSpeedModifier(g.Sprint)
	} else {
		m.Navigation().SetSpeedModifier(g.Walk)
	}
}

// BrightOutside is Level.isBrightOutside: the overworld sky darkened by less than 4.
func BrightOutside(m *javamob.Mob) bool {
	return m.Tx().World().Dimension() == world.Overworld && javamob.SkyDarken(m.Tx()) < 4
}

// RestrictSun is RestrictSunGoal: while it is bright outside and the mob wears no helmet, its paths stop before the
// open sky.
type RestrictSun struct{ javamob.GoalBase }

func (g *RestrictSun) CanUse(m *javamob.Mob) bool {
	return BrightOutside(m) && m.ItemBySlot(javamob.SlotHead).Empty()
}
func (g *RestrictSun) Start(m *javamob.Mob) { m.Navigation().SetAvoidSun(true) }
func (g *RestrictSun) Stop(m *javamob.Mob)  { m.Navigation().SetAvoidSun(false) }

// FleeSun is FleeSunGoal: a burning mob under the open sky runs into the shade.
type FleeSun struct {
	javamob.GoalBase
	Speed  float64
	wanted mgl64.Vec3
}

// NewFleeSun returns FleeSunGoal(mob, speed).
func NewFleeSun(speed float64) *FleeSun {
	return &FleeSun{GoalBase: javamob.GoalBase{Control: javamob.FlagMove}, Speed: speed}
}

func (g *FleeSun) CanUse(m *javamob.Mob) bool {
	if _, ok := m.Target(); ok {
		return false
	}
	if !BrightOutside(m) || !m.IsOnFire() || !path.CanSeeSky(m.Tx(), cube.PosFromVec3(m.Position())) {
		return false
	}
	if !m.ItemBySlot(javamob.SlotHead).Empty() {
		return false
	}
	r := m.Rand()
	pos := cube.PosFromVec3(m.Position())
	for i := 0; i < 10; i++ {
		p := pos.Add(cube.Pos{r.IntN(20) - 10, r.IntN(6) - 3, r.IntN(20) - 10})
		if !path.CanSeeSky(m.Tx(), p) && m.WalkTargetValue(p) < 0 {
			g.wanted = mgl64.Vec3{float64(p[0]) + 0.5, float64(p[1]), float64(p[2]) + 0.5}
			return true
		}
	}
	return false
}
func (g *FleeSun) CanContinueToUse(m *javamob.Mob) bool { return !m.Navigation().Done() }
func (g *FleeSun) Start(m *javamob.Mob)                 { m.Navigation().MoveTo(m, g.wanted, 1, g.Speed) }

// RangedAttacker is RangedAttackMob: a mob that shoots (performRangedAttack).
type RangedAttacker interface {
	PerformRangedAttack(m *javamob.Mob, target world.Entity, power float64)
}

// RangedBowAttack is RangedBowAttackGoal: keep the target within AttackRadius, strafe, draw the bow for a second and
// shoot, then wait AttackIntervalMin ticks.
type RangedBowAttack struct {
	javamob.GoalBase
	Speed             float64
	AttackIntervalMin int
	attackRadiusSqr   float64
	attackTime        int
	seeTime           int
	clockwise         bool
	backwards         bool
	strafingTime      int
}

// NewRangedBowAttack returns RangedBowAttackGoal(mob, speed, interval, radius).
func NewRangedBowAttack(speed float64, interval int, radius float32) *RangedBowAttack {
	return &RangedBowAttack{GoalBase: javamob.GoalBase{Control: javamob.FlagMove | javamob.FlagLook}, Speed: speed,
		AttackIntervalMin: interval, attackRadiusSqr: float64(radius * radius), attackTime: -1, strafingTime: -1}
}

// HoldingBow is isHolding(Items.BOW).
func HoldingBow(m *javamob.Mob) bool {
	main, off := m.HeldItems()
	_, a := main.Item().(item.Bow)
	_, b := off.Item().(item.Bow)
	return a || b
}

func (g *RangedBowAttack) CanUse(m *javamob.Mob) bool {
	_, ok := m.Target()
	return ok && HoldingBow(m)
}
func (g *RangedBowAttack) CanContinueToUse(m *javamob.Mob) bool {
	return (g.CanUse(m) || !m.Navigation().Done()) && HoldingBow(m)
}
func (g *RangedBowAttack) Start(m *javamob.Mob) { m.SetAggressive(true) }
func (g *RangedBowAttack) Stop(m *javamob.Mob) {
	m.SetAggressive(false)
	g.seeTime, g.attackTime = 0, -1
	m.StopUsingItem()
}
func (g *RangedBowAttack) RequiresUpdateEveryTick() bool { return true }

func (g *RangedBowAttack) Tick(m *javamob.Mob) {
	t, ok := m.Target()
	if !ok {
		return
	}
	dist := t.Position().Sub(m.Position()).LenSqr()
	see := m.Sensing().HasLineOfSight(m, t)
	if see != (g.seeTime > 0) {
		g.seeTime = 0
	}
	if see {
		g.seeTime++
	} else {
		g.seeTime--
	}
	if !(dist > g.attackRadiusSqr) && g.seeTime >= 20 {
		m.Navigation().Stop()
		g.strafingTime++
	} else {
		m.Navigation().MoveToEntity(m, t, g.Speed)
		g.strafingTime = -1
	}
	if g.strafingTime >= 20 {
		if m.Rand().Float32() < 0.3 {
			g.clockwise = !g.clockwise
		}
		if m.Rand().Float32() < 0.3 {
			g.backwards = !g.backwards
		}
		g.strafingTime = 0
	}
	if g.strafingTime > -1 {
		if dist > g.attackRadiusSqr*float64(float32(0.75)) {
			g.backwards = false
		} else if dist < g.attackRadiusSqr*float64(float32(0.25)) {
			g.backwards = true
		}
		fwd, right := 0.5, -0.5
		if g.backwards {
			fwd = -0.5
		}
		if g.clockwise {
			right = 0.5
		}
		m.MoveControl().Strafe(fwd, right)
		m.LookAtEntity(t, 30, 30)
	} else {
		m.LookControl().SetLookAtEntity(t, 30, 30)
	}
	if m.UsingItem() {
		if !see && g.seeTime < -60 {
			m.StopUsingItem()
		} else if see {
			if pull := m.TicksUsingItem(); pull >= 20 {
				m.StopUsingItem()
				if r, ok := m.Behaviour().(RangedAttacker); ok {
					r.PerformRangedAttack(m, t, BowPower(pull))
				}
				g.attackTime = g.AttackIntervalMin
			}
		}
	} else if g.attackTime--; g.attackTime <= 0 && g.seeTime >= -60 {
		m.StartUsingItem()
	}
}

// BowPower is BowItem.getPowerForTime.
func BowPower(ticks int) float64 {
	p := float32(ticks) / 20
	p = (p*p + p*2) / 3
	return float64(min(p, 1))
}

// BreakDoor is BreakDoorGoal: a mob stuck at a wooden door on its path breaks it in 240 ticks on the allowed
// difficulties.
type BreakDoor struct {
	javamob.GoalBase
	// Difficulty reports whether the world difficulty (Difficulty id) lets the mob break doors.
	Difficulty func(id int) bool
	// Time is the break time in ticks (at least 240).
	Time int

	door      cube.Pos
	hasDoor   bool
	passed    bool
	openDir   [2]float32
	breakTime int
	lastProg  int
}

// NewBreakDoor returns BreakDoorGoal(mob, difficulties).
func NewBreakDoor(difficulty func(id int) bool) *BreakDoor {
	return &BreakDoor{Difficulty: difficulty, Time: 240, lastProg: -1}
}

func woodenDoor(tx *world.Tx, pos cube.Pos) (block.WoodDoor, bool) {
	d, ok := tx.Block(pos).(block.WoodDoor)
	return d, ok
}

func (g *BreakDoor) isOpen(m *javamob.Mob) bool {
	if !g.hasDoor {
		return false
	}
	d, ok := woodenDoor(m.Tx(), g.door)
	if !ok {
		g.hasDoor = false
		return false
	}
	return d.Open
}

// doorCanUse is DoorInteractGoal.canUse.
func (g *BreakDoor) doorCanUse(m *javamob.Mob) bool {
	if !m.HorizontalCollision() {
		return false
	}
	p := m.Navigation().Path()
	if p == nil || p.Done() {
		return false
	}
	tx := m.Tx()
	pos := m.Position()
	for i := 0; i < min(p.NextIndex()+2, p.NodeCount()); i++ {
		n := p.Node(i)
		g.door = cube.Pos{n.X, n.Y + 1, n.Z}
		dx, dz := float64(g.door[0])-pos[0], float64(g.door[2])-pos[2]
		if dx*dx+dz*dz <= 2.25 {
			_, g.hasDoor = woodenDoor(tx, g.door)
			if g.hasDoor {
				return true
			}
		}
	}
	g.door = cube.PosFromVec3(pos).Side(cube.FaceUp)
	_, g.hasDoor = woodenDoor(tx, g.door)
	return g.hasDoor
}

func (g *BreakDoor) CanUse(m *javamob.Mob) bool {
	return g.doorCanUse(m) && g.Difficulty(m.DifficultyID()) && !g.isOpen(m)
}

func (g *BreakDoor) CanContinueToUse(m *javamob.Mob) bool {
	c := g.door.Vec3Centre()
	return g.breakTime <= g.Time && !g.isOpen(m) && c.Sub(m.Position()).LenSqr() < 4 && g.Difficulty(m.DifficultyID())
}

func (g *BreakDoor) Start(m *javamob.Mob) {
	g.passed = false
	p := m.Position()
	g.openDir = [2]float32{float32(float64(g.door[0]) + 0.5 - p[0]), float32(float64(g.door[2]) + 0.5 - p[2])}
	g.breakTime = 0
}

func (g *BreakDoor) Stop(*javamob.Mob) { g.lastProg = -1 }

func (g *BreakDoor) RequiresUpdateEveryTick() bool { return true }

func (g *BreakDoor) Tick(m *javamob.Mob) {
	tx := m.Tx()
	if m.Rand().IntN(20) == 0 {
		tx.PlaySound(g.door.Vec3Centre(), sound.DoorCrash{})
		m.Swing()
	}
	g.breakTime++
	if prog := int(float32(g.breakTime) / float32(g.Time) * 10); prog != g.lastProg {
		g.lastProg = prog
	}
	if g.breakTime == g.Time && g.Difficulty(m.DifficultyID()) {
		if d, ok := woodenDoor(tx, g.door); ok {
			other := g.door.Side(cube.FaceUp)
			if d.Top {
				other = g.door.Side(cube.FaceDown)
			}
			tx.SetBlock(g.door, nil, nil)
			if _, ok := woodenDoor(tx, other); ok {
				tx.SetBlock(other, nil, nil)
			}
			tx.AddParticle(g.door.Vec3Centre(), particle.BlockBreak{Block: d})
		}
	}
}
