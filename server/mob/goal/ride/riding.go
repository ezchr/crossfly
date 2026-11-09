//go:build crossfly_full

package ride

import (
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	cride "github.com/df-mc/dragonfly/server/mob/component/ride"
	"github.com/df-mc/dragonfly/server/mob/component/tame"
	gcore "github.com/df-mc/dragonfly/server/mob/goal/core"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// ---- behavior.player_ride_tamed ----

// playerRideTamed is minecraft:behavior.player_ride_tamed (BDS
// PlayerVehicleTamedGoal): while a player steers the mob, this goal holds
// the move and look controls so no other goal walks it around; the
// steering is input_ground_controlled.
//
//	Flags move|look.
type playerRideTamed struct{ mob.GoalBase }

func newPlayerRideTamed(spec mob.GoalSpec) (mob.Goal, error) {
	return &playerRideTamed{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}}, nil
}

func (*playerRideTamed) CanUse(m *mob.Mob) bool {
	r, ok := m.ControllingPassenger()
	if !ok {
		return false
	}
	_, isPlayer := r.(*player.Player)
	return isPlayer
}

func (*playerRideTamed) Start(m *mob.Mob) { m.Navigation().Stop(m) }

// ---- behavior.controlled_by_player ----

// controlledByPlayer is minecraft:behavior.controlled_by_player (BDS
// ControlledByPlayerGoal): a player riding the mob and holding its
// item_controllable item steers it: each tick the mob turns
// fractional_rotation of the way to the rider's yaw, at most
// fractional_rotation_limit degrees, and walks forward at movement x
// mount_speed_multiplier (x boostable's multiplier while boosted).
//
//	fractional_rotation 0.5, fractional_rotation_limit 5,
//	mount_speed_multiplier 1. Flags move|look.
type controlledByPlayer struct {
	mob.GoalBase
	fraction, limit, mult float64
	course                cride.Course
}

func newControlledByPlayer(spec mob.GoalSpec) (mob.Goal, error) {
	v := struct {
		Fraction *float64 `json:"fractional_rotation"`
		Limit    *float64 `json:"fractional_rotation_limit"`
		Mult     *float64 `json:"mount_speed_multiplier"`
	}{}
	if err := spec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	g := &controlledByPlayer{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, fraction: 0.5, limit: 5, mult: 1}
	setF(&g.fraction, v.Fraction)
	setF(&g.limit, v.Limit)
	setF(&g.mult, v.Mult)
	return g, nil
}

// rider is the player steering m with the control item in hand.
func (g *controlledByPlayer) rider(m *mob.Mob) (world.Entity, *cride.ItemControllable, bool) {
	r, ok := m.ControllingPassenger()
	if !ok {
		return nil, nil, false
	}
	if _, ok := r.(*player.Player); !ok {
		return nil, nil, false
	}
	ic, ok := cride.ItemControllableOf(m)
	if !ok || !ic.Holds(r) {
		return nil, nil, false
	}
	return r, ic, true
}

func (g *controlledByPlayer) CanUse(m *mob.Mob) bool {
	_, _, ok := g.rider(m)
	return ok
}

func (g *controlledByPlayer) Start(m *mob.Mob) { m.Navigation().Stop(m) }

func (g *controlledByPlayer) Stop(m *mob.Mob) {
	if ic, ok := cride.ItemControllableOf(m); ok {
		ic.SetCourse(nil)
	}
}

func (g *controlledByPlayer) Tick(m *mob.Mob) {
	r, ic, ok := g.rider(m)
	if !ok {
		return
	}
	yaw := m.Yaw()
	turn := wrapDegrees(r.Rotation().Yaw()-yaw) * g.fraction
	turn = max(-g.limit, min(g.limit, turn))
	yaw = wrapDegrees(yaw + turn)
	m.SetYaw(yaw)
	m.SetBodyYaw(yaw)
	m.SetHeadYaw(yaw)
	g.course.Speed = m.Speed() * g.mult
	ic.SetCourse(&g.course)
}

// ---- behavior.run_around_like_crazy ----

// runAroundLikeCrazy is minecraft:behavior.run_around_like_crazy (BDS
// RunAroundLikeCrazyGoal, java RunAroundLikeCrazyGoal): an untamed horse
// with a rider runs to random spots 5 blocks around at speed_multiplier
// until it is tamed or loses the rider (tamemount does the taming rolls).
//
//	speed_multiplier 1. Flags move.
type runAroundLikeCrazy struct {
	mob.GoalBase
	speed float64
	pos   mgl64.Vec3
}

func newRunAroundLikeCrazy(spec mob.GoalSpec) (mob.Goal, error) {
	v := struct {
		Speed *float64 `json:"speed_multiplier"`
	}{}
	if err := spec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	g := &runAroundLikeCrazy{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}, speed: 1}
	setF(&g.speed, v.Speed)
	return g, nil
}

func (g *runAroundLikeCrazy) CanUse(m *mob.Mob) bool {
	if tame.Tamed(m) || m.RiderCount() == 0 {
		return false
	}
	pos, ok := gcore.RandomPos(m, 5, 4, 0, nil, 10)
	if !ok {
		return false
	}
	g.pos = pos
	return true
}

func (g *runAroundLikeCrazy) CanContinueToUse(m *mob.Mob) bool {
	return !tame.Tamed(m) && m.RiderCount() > 0 && !m.Navigation().Done()
}

func (g *runAroundLikeCrazy) Start(m *mob.Mob) { m.Navigation().MoveTo(m, g.pos, g.speed) }

// ---- behavior.find_mount ----

// findMount is minecraft:behavior.find_mount (BDS FindMountGoal): after
// start_delay tries the mob looks within_radius for the nearest mob it may
// ride (a free seat, its family allowed; not in water with avoid_water;
// only with a target if target_needed), walks to it repathing every 4-10
// ticks and gets on within mount_distance (default: twice its width plus
// the mount's width plus 2). A failed attempt waits 20 ticks per failure
// (up to max_failed_attempts).
//
//	within_radius 0, avoid_water false, start_delay 0, target_needed false,
//	mount_distance -1, max_failed_attempts 20. Flags move.
type findMount struct {
	mob.GoalBase
	radius          float64
	avoidWater      bool
	startDelay      int
	targetNeeded    bool
	mountDist       float64
	maxFailed       int
	counter, failed int
	repath          int
	mount           *world.EntityHandle
	mounted         bool
}

func newFindMount(spec mob.GoalSpec) (mob.Goal, error) {
	v := struct {
		Radius       float64  `json:"within_radius"`
		AvoidWater   bool     `json:"avoid_water"`
		StartDelay   int      `json:"start_delay"`
		TargetNeeded bool     `json:"target_needed"`
		MountDist    *float64 `json:"mount_distance"`
		MaxFailed    *int     `json:"max_failed_attempts"`
	}{}
	if err := spec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	g := &findMount{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}, radius: v.Radius, avoidWater: v.AvoidWater,
		startDelay: v.StartDelay, targetNeeded: v.TargetNeeded, mountDist: -1, maxFailed: 20}
	setF(&g.mountDist, v.MountDist)
	if v.MaxFailed != nil {
		g.maxFailed = *v.MaxFailed
	}
	return g, nil
}

// mountable reports whether m may get on e now: a live mob not riding
// itself, or a player (shoulders), whose rideable takes m.
func (g *findMount) mountable(m *mob.Mob, e world.Entity) bool {
	if e.H() == m.H() {
		return false
	}
	if v, ok := mob.Of(e); ok {
		if v.Dead() || v.Riding() || (g.avoidWater && v.Physics().InWater) {
			return false
		}
	} else if p, ok := e.(*player.Player); ok {
		if p.Dead() || p.GameMode() == world.GameModeSpectator {
			return false
		}
	} else {
		return false
	}
	r, ok := mob.VehicleOf(e)
	if !ok {
		return false
	}
	f, ok := r.(entity.RiderFilter)
	return ok && f.AcceptsRider(m)
}

func (g *findMount) CanUse(m *mob.Mob) bool {
	if !m.HasNavigation() || m.Riding() {
		return false
	}
	if g.targetNeeded && m.TargetHandle() == nil {
		return false
	}
	if g.counter < g.startDelay {
		g.counter++
		return false
	}
	best, bestDist := (*world.EntityHandle)(nil), -1.0
	for e := range m.Tx().EntitiesWithin(m.BBox().Grow(g.radius)) {
		if !g.mountable(m, e) {
			continue
		}
		if d := e.Position().Sub(m.Pos()).LenSqr(); bestDist < 0 || d < bestDist {
			best, bestDist = e.H(), d
		}
	}
	if best == nil {
		g.counter = 0
		return false
	}
	g.mount = best
	return true
}

func (g *findMount) CanContinueToUse(m *mob.Mob) bool {
	if m.Riding() || g.mount == nil {
		return false
	}
	e, ok := g.mount.Entity(m.Tx())
	return ok && g.mountable(m, e)
}

// inRange is FindMountGoal::isInMountRange.
func (g *findMount) inRange(m *mob.Mob, v world.Entity) bool {
	// BDS compares mount_distance with the squared distance as it is.
	d := g.mountDist
	if d < 0 {
		d = 2*m.BBox().Width() + v.H().Type().BBox(v).Width() + 2
		d *= d
	}
	return v.Position().Sub(m.Pos()).LenSqr() < d
}

func (g *findMount) Start(m *mob.Mob) {
	v, ok := g.mount.Entity(m.Tx())
	if !ok {
		return
	}
	g.repath = 0
	if g.inRange(m, v) {
		return
	}
	if p := m.Navigation().CreatePathToEntity(m, v); p == nil || !p.Complete() {
		g.giveUp(m)
	}
}

func (g *findMount) giveUp(m *mob.Mob) {
	g.mount = nil
	g.counter = -20 * g.failed
}

func (g *findMount) Stop(m *mob.Mob) {
	m.Navigation().Stop(m)
	switch {
	case g.mounted:
		g.failed, g.mounted = 0, false
	case g.failed < g.maxFailed:
		g.failed++
	}
	g.mount = nil
}

func (g *findMount) Tick(m *mob.Mob) {
	if g.mount == nil {
		return
	}
	v, ok := g.mount.Entity(m.Tx())
	if !ok {
		return
	}
	if g.inRange(m, v) {
		if m.StartRiding(v) {
			g.mounted = true
		}
		return
	}
	if g.repath--; g.repath <= 0 {
		g.repath = 4 + m.Rand().IntN(7)
		m.LookControl().SetLookAtEntity(v, 30, 30)
		if !m.Navigation().MoveToEntity(m, v, 1) {
			g.giveUp(m)
		}
	}
}

// ---- behavior.skeleton_horse_trap ----

// skeletonHorseTrap is minecraft:behavior.skeleton_horse_trap (BDS
// SkeletonHorseTrapGoal): a trap horse waits duration seconds (then it is
// removed) for a player within_radius; then lightning strikes, the horse
// fires minecraft:spring_trap and gets a skeleton rider, and three more
// skeleton horses with riders spawn around it.
//
//	duration 1, within_radius 0. Flags none.
type skeletonHorseTrap struct {
	mob.GoalBase
	left   int
	radius float64
	sprung bool
}

func newSkeletonHorseTrap(spec mob.GoalSpec) (mob.Goal, error) {
	v := struct {
		Duration *float64 `json:"duration"`
		Radius   float64  `json:"within_radius"`
	}{}
	if err := spec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	g := &skeletonHorseTrap{GoalBase: mob.GoalBase{Control: spec.FlagsOr(0)}, left: 20, radius: v.Radius}
	if v.Duration != nil {
		g.left = int(*v.Duration * 20)
	}
	return g, nil
}

func (g *skeletonHorseTrap) CanUse(m *mob.Mob) bool {
	if g.sprung {
		return false
	}
	if g.left--; g.left <= 0 {
		_ = m.Close()
		return false
	}
	_, ok := nearestPlayer(m, g.radius)
	return ok
}

func (g *skeletonHorseTrap) Tick(m *mob.Mob) {
	if g.sprung {
		return
	}
	g.sprung = true
	tx := m.Tx()
	tx.AddEntity(entity.NewLightningWithDamage(world.EntitySpawnOpts{Position: m.Pos()}, 0, false, 0))
	m.Fire("minecraft:spring_trap", nil)
	trapRider(m)
	for i := 0; i < 3; i++ {
		off := mgl64.Vec3{m.Rand().Float64()*2 - 1, 0, m.Rand().Float64()*2 - 1}
		h, err := mob.Spawn(tx, "minecraft:skeleton_horse", m.Pos().Add(off), mob.SpawnOptions{Event: "minecraft:spring_trap"})
		if err == nil {
			trapRider(h)
		}
	}
	// The trap is gone for good: the group goes (saved with the definition), so
	// a reload does not set it again. BDS turns the trap off on the horse.
	if in, ok := m.Instance().(interface {
		RemoveGroup(name string) bool
		Commit()
	}); ok && in.RemoveGroup("minecraft:skeleton_trap") {
		in.Commit()
		m.ReapplyComponents()
	}
}

// trapRider puts a skeleton on a sprung trap horse.
func trapRider(h *mob.Mob) {
	s, err := mob.Spawn(h.Tx(), "minecraft:skeleton", h.Pos(), mob.SpawnOptions{})
	if err != nil {
		return
	}
	s.SetPersistent(true)
	s.StartRiding(h)
}

// ---- helpers ----

func setF(dst *float64, v *float64) {
	if v != nil {
		*dst = *v
	}
}

func wrapDegrees(d float64) float64 {
	d = math.Mod(d, 360)
	if d >= 180 {
		d -= 360
	}
	if d < -180 {
		d += 360
	}
	return d
}

// nearestPlayer is the nearest survival player within r of m.
func nearestPlayer(m *mob.Mob, r float64) (*player.Player, bool) {
	var best *player.Player
	bestDist := r * r
	for e := range m.Tx().Players() {
		p, ok := e.(*player.Player)
		if !ok || p.Dead() || p.GameMode() == world.GameModeSpectator {
			continue
		}
		if d := p.Position().Sub(m.Pos()).LenSqr(); d <= bestDist {
			best, bestDist = p, d
		}
	}
	return best, best != nil
}

// holding reports whether e holds one of names in a hand.
func holding(e world.Entity, match func(item.Stack) bool) bool {
	h, ok := e.(interface {
		HeldItems() (item.Stack, item.Stack)
	})
	if !ok {
		return false
	}
	main, off := h.HeldItems()
	return match(main) || match(off)
}
