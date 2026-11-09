//go:build crossfly_full

package social

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// targetOf returns m's living target.
func targetOf(m *mob.Mob) (world.Entity, bool) {
	t, ok := m.Target()
	if !ok || !kit.Alive(t) {
		return nil, false
	}
	return t, true
}

// jumpAroundTarget is minecraft:behavior.jump_around_target (BDS
// JumpAroundTargetGoal; the jump maths is Java LongJumpUtil
// .calculateJumpVectorForAngle): while the target is within
// valid_distance_to_target and filters pass, the breeze prepares for
// prepare_jump_duration, then jumps to a spot landing_distance_from_target
// from the target (within landing_position_spread_degrees of the side it is
// on, snapped to the surface within snap_to_surface_block_range) with the
// first of jump_angles whose launch speed stays within max_jump_velocity.
// After landing it waits jump_cooldown_duration, or
// jump_cooldown_when_hurt_duration when hurt within last_hurt_duration.
//
//	jump_cooldown_duration            0.5 s
//	jump_cooldown_when_hurt_duration  0.1 s
//	last_hurt_duration                2 s
//	prepare_jump_duration             0.5 s
//	max_jump_velocity                 1.4
//	landing_distance_from_target      {4, 20}
//	landing_position_spread_degrees   90
//	required_vertical_space           4 (free blocks above the mob)
//	snap_to_surface_block_range       10
//	valid_distance_to_target          {4, 20}
//	jump_angles                       [40, 55, 60, 75, 80]
//
// check_collision, entity_bounding_box_scale and
// line_of_sight_obstruction_height_ignore (the clear-flight check) are not
// done. Sounds: "charge" while preparing, "jump", "land"; actor flag
// jump_goal_jump while it runs. No control flags (the BDS constructor sets
// none), so it runs beside move_around_target; it stops the path while it
// runs.
type jumpAroundTarget struct {
	mob.GoalBase
	filter                 mob.Filter
	cooldown, hurtCooldown int
	lastHurt, prepare      int
	maxVel                 float64
	landing, valid         kit.Range
	spread                 float64
	vertSpace, snap        int
	angles                 []float64

	next     int64
	phase    int // 0 prepare, 1 airborne, 2 landed
	timer    int
	vel      mgl64.Vec3
	airTicks int
}

func newJumpAroundTarget(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Filters      json.RawMessage `json:"filters"`
		Cooldown     *float64        `json:"jump_cooldown_duration"`
		HurtCooldown *float64        `json:"jump_cooldown_when_hurt_duration"`
		LastHurt     *float64        `json:"last_hurt_duration"`
		Prepare      *float64        `json:"prepare_jump_duration"`
		MaxVel       *float64        `json:"max_jump_velocity"`
		Landing      json.RawMessage `json:"landing_distance_from_target"`
		Valid        json.RawMessage `json:"valid_distance_to_target"`
		Spread       *float64        `json:"landing_position_spread_degrees"`
		VertSpace    *int            `json:"required_vertical_space"`
		Snap         *int            `json:"snap_to_surface_block_range"`
		Angles       []float64       `json:"jump_angles"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	g := &jumpAroundTarget{
		GoalBase:     mob.GoalBase{Control: spec.FlagsOr(0)},
		cooldown:     kit.Ticks(kit.Or(v.Cooldown, 0.5)),
		hurtCooldown: kit.Ticks(kit.Or(v.HurtCooldown, 0.1)),
		lastHurt:     kit.Ticks(kit.Or(v.LastHurt, 2)),
		prepare:      kit.Ticks(kit.Or(v.Prepare, 0.5)),
		maxVel:       kit.Or(v.MaxVel, 1.4),
		spread:       kit.Or(v.Spread, 90),
		vertSpace:    kit.Or(v.VertSpace, 4),
		snap:         kit.Or(v.Snap, 10),
		angles:       v.Angles,
	}
	if len(g.angles) == 0 {
		g.angles = []float64{40, 55, 60, 75, 80}
	}
	var err error
	if g.filter, err = kit.Filter(v.Filters); err != nil {
		return nil, err
	}
	if g.landing, err = kit.ParseRange(v.Landing, kit.Range{Min: 4, Max: 20}); err != nil {
		return nil, err
	}
	if g.valid, err = kit.ParseRange(v.Valid, kit.Range{Min: 4, Max: 20}); err != nil {
		return nil, err
	}
	return g, nil
}

// spaceAbove reports whether n blocks above the mob's feet are passable.
func spaceAbove(m *mob.Mob, n int) bool {
	base := cube.PosFromVec3(m.Position())
	for i := 1; i <= n; i++ {
		if kit.Solid(m.Tx(), base.Add(cube.Pos{0, i, 0})) {
			return false
		}
	}
	return true
}

// jumpVector is Java LongJumpUtil.calculateJumpVectorForAngle with gravity
// 0.08: the launch velocity that lands at to when leaving from at the
// given angle, or false when it would exceed maxVel.
func jumpVector(from, to mgl64.Vec3, angleDeg, maxVel float64) (mgl64.Vec3, bool) {
	d := to.Sub(from)
	dist := math.Hypot(d[0], d[2])
	if dist < 1e-4 {
		return mgl64.Vec3{}, false
	}
	a := mgl64.DegToRad(angleDeg)
	const gravity = 0.08
	den := dist*math.Sin(2*a) - 2*d[1]*math.Pow(math.Cos(a), 2)
	if den <= 0 {
		return mgl64.Vec3{}, false
	}
	v2 := dist * dist * gravity / den
	if v2 < 0 {
		return mgl64.Vec3{}, false
	}
	v := math.Sqrt(v2)
	if v > maxVel {
		return mgl64.Vec3{}, false
	}
	h := v * math.Cos(a)
	return mgl64.Vec3{h * d[0] / dist, v * math.Sin(a), h * d[2] / dist}, true
}

// landingSpot picks where to land around the target.
func (g *jumpAroundTarget) landingSpot(m *mob.Mob, t world.Entity) (mgl64.Vec3, bool) {
	tp := t.Position()
	base := kit.YawTowards(tp, m.Position())
	yaw := base + (m.Rand().Float64()-0.5)*g.spread
	p := tp.Add(yawDir(yaw).Mul(g.landing.Roll(m.Rand())))
	col := cube.PosFromVec3(p)
	if gp, ok := kit.Ground(m.Tx(), col, g.snap); ok {
		return blockCentre(gp), true
	}
	return mgl64.Vec3{}, false
}

// CanUse ...
func (g *jumpAroundTarget) CanUse(m *mob.Mob) bool {
	if m.Ticks() < g.next {
		return false
	}
	t, ok := targetOf(m)
	if !ok || !g.filter.Test(kit.OtherCtx(m, t)) {
		return false
	}
	d := t.Position().Sub(m.Position()).Len()
	if d < g.valid.Min || d > g.valid.Max || !spaceAbove(m, g.vertSpace) {
		return false
	}
	land, ok := g.landingSpot(m, t)
	if !ok {
		return false
	}
	angles := append([]float64(nil), g.angles...)
	m.Rand().Shuffle(len(angles), func(i, j int) { angles[i], angles[j] = angles[j], angles[i] })
	for _, a := range angles {
		if v, ok := jumpVector(m.Position(), land, a, g.maxVel); ok {
			g.vel = v
			return true
		}
	}
	return false
}

// CanContinueToUse ...
func (g *jumpAroundTarget) CanContinueToUse(m *mob.Mob) bool { return g.phase != 2 && g.airTicks < 100 }

// Start ...
func (g *jumpAroundTarget) Start(m *mob.Mob) {
	g.phase, g.timer, g.airTicks = 0, g.prepare, 0
	m.StopMovement()
	kit.Flag(m, protocol.EntityDataFlagJumpGoal, true)
	kit.Sound(m, "charge")
}

// Tick ...
func (g *jumpAroundTarget) Tick(m *mob.Mob) {
	if t, ok := targetOf(m); ok {
		kit.LookAtEntity(m, t)
	}
	m.Navigation().Stop(m)
	switch g.phase {
	case 0:
		if g.timer--; g.timer > 0 {
			return
		}
		m.SetYaw(mgl64.RadToDeg(math.Atan2(-g.vel[0], g.vel[2])))
		m.SetVelocity(g.vel)
		kit.Sound(m, "jump")
		g.phase = 1
	case 1:
		g.airTicks++
		if g.airTicks > 2 && m.OnGround() {
			kit.Sound(m, "land")
			g.phase = 2
		}
	}
}

// Stop ...
func (g *jumpAroundTarget) Stop(m *mob.Mob) {
	kit.Flag(m, protocol.EntityDataFlagJumpGoal, false)
	cd := g.cooldown
	if _, tick, ok := m.LastHurtBy(); ok && m.Ticks()-tick <= int64(g.lastHurt) {
		cd = g.hurtCooldown
	}
	g.next = m.Ticks() + int64(cd)
	g.phase = 2
}

// AppendDebugInfo ...
func (g *jumpAroundTarget) AppendDebugInfo(b *strings.Builder) { b.WriteString("JumpAroundTarget") }

// moveAroundTarget is minecraft:behavior.move_around_target (1.26, not in
// the 1.21.3 dump; source: 1.26 JSON + Java Breeze Slide): while filters
// pass, the mob walks to a spot destination_position_range from the target,
// within destination_pos_spread_degrees of the side it is on, at
// movement_speed. height_difference_limit > 0 rejects spots that much higher
// or lower than the mob.
//
//	destination_pos_spread_degrees  90
//	destination_position_range      {4, 8}
//	height_difference_limit         0 (no limit)
//	movement_speed                  1
//
// Control flag move.
type moveAroundTarget struct {
	mob.GoalBase
	filter     mob.Filter
	spread     float64
	rng        kit.Range
	heightDiff float64
	speed      float64
	dest       mgl64.Vec3
}

func newMoveAroundTarget(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Filters    json.RawMessage `json:"filters"`
		Spread     *float64        `json:"destination_pos_spread_degrees"`
		Range      json.RawMessage `json:"destination_position_range"`
		HeightDiff float64         `json:"height_difference_limit"`
		Speed      *float64        `json:"movement_speed"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	g := &moveAroundTarget{
		GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)},
		spread:   kit.Or(v.Spread, 90), heightDiff: v.HeightDiff, speed: kit.Or(v.Speed, 1),
	}
	var err error
	if g.filter, err = kit.Filter(v.Filters); err != nil {
		return nil, err
	}
	if g.rng, err = kit.ParseRange(v.Range, kit.Range{Min: 4, Max: 8}); err != nil {
		return nil, err
	}
	return g, nil
}

// CanUse ...
func (g *moveAroundTarget) CanUse(m *mob.Mob) bool {
	t, ok := targetOf(m)
	if !ok || !g.filter.Test(kit.OtherCtx(m, t)) {
		return false
	}
	tp := t.Position()
	yaw := kit.YawTowards(tp, m.Position()) + (m.Rand().Float64()-0.5)*g.spread
	p := tp.Add(yawDir(yaw).Mul(g.rng.Roll(m.Rand())))
	gp, ok := kit.Ground(m.Tx(), cube.PosFromVec3(p), 4)
	if !ok {
		return false
	}
	g.dest = blockCentre(gp)
	if g.heightDiff > 0 && math.Abs(g.dest[1]-m.Position()[1]) > g.heightDiff {
		return false
	}
	return m.Navigation().CreatePath(m, g.dest) != nil
}

// CanContinueToUse ...
func (g *moveAroundTarget) CanContinueToUse(m *mob.Mob) bool {
	_, ok := targetOf(m)
	return ok && !m.Navigation().Done()
}

// Start ...
func (g *moveAroundTarget) Start(m *mob.Mob) { kit.MoveTo(m, g.dest, g.speed) }

// Stop ...
func (g *moveAroundTarget) Stop(m *mob.Mob) { kit.Stop(m) }

// AppendDebugInfo ...
func (g *moveAroundTarget) AppendDebugInfo(b *strings.Builder) { b.WriteString("MoveAroundTarget") }

// firedKey counts the shots fire_at_target took without a projectile
// (mob.Data).
type firedKey struct{}

// ShotsWithoutProjectile is how many times m's fire_at_target fired while
// neither its projectile_def nor a minecraft:shooter could make a
// projectile.
func ShotsWithoutProjectile(m *mob.Mob) int {
	v, _ := m.Data(firedKey{})
	n, _ := v.(int)
	return n
}

// fireAtTarget is minecraft:behavior.fire_at_target (BDS FireAtTargetGoal):
// when the cooldown is over, filters pass and the target is within
// attack_range and seen, the mob faces it for pre_shoot_delay, fires once
// if the target is within ranged_fov, then holds for post_shoot_delay;
// attack_cooldown starts when it stops.
//
//	attack_range          {0, 16}
//	attack_cooldown       0 s
//	pre_shoot_delay       0 s, post_shoot_delay 0 s
//	ranged_fov            90 (BDS +96)
//	max_head_rotation_x   30, max_head_rotation_y 30 (BDS +88, +92)
//	owner_anchor/target_anchor 2, owner_offset/target_offset (not used)
//
// projectile_def minecraft:breeze_wind_charge_projectile does not exist in
// Dragonfly: the shot goes through the mob's minecraft:shooter when it has
// one, otherwise it is only counted (ShotsWithoutProjectile). Sounds:
// "inhale" when aiming, "shoot" when firing; actor flag
// facing_target_to_range_attack while aiming. Control flags move|look.
type fireAtTarget struct {
	mob.GoalBase
	filter     mob.Filter
	rng        kit.Range
	cooldown   int
	pre, post  int
	fov        float64
	maxX, maxY float64
	projectile string

	next, ends int64
	timer      int
	fired      bool
}

func newFireAtTarget(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Filters    json.RawMessage `json:"filters"`
		Range      json.RawMessage `json:"attack_range"`
		Cooldown   float64         `json:"attack_cooldown"`
		Pre        float64         `json:"pre_shoot_delay"`
		Post       float64         `json:"post_shoot_delay"`
		FOV        *float64        `json:"ranged_fov"`
		MaxX       *float64        `json:"max_head_rotation_x"`
		MaxY       *float64        `json:"max_head_rotation_y"`
		Projectile string          `json:"projectile_def"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	g := &fireAtTarget{
		GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)},
		cooldown: kit.Ticks(v.Cooldown), pre: kit.Ticks(v.Pre), post: kit.Ticks(v.Post),
		fov: kit.Or(v.FOV, 90), maxX: kit.Or(v.MaxX, 30), maxY: kit.Or(v.MaxY, 30), projectile: v.Projectile,
	}
	var err error
	if g.filter, err = kit.Filter(v.Filters); err != nil {
		return nil, err
	}
	if g.rng, err = kit.ParseRange(v.Range, kit.Range{Min: 0, Max: 16}); err != nil {
		return nil, err
	}
	return g, nil
}

// CanUse ...
func (g *fireAtTarget) CanUse(m *mob.Mob) bool {
	if m.Ticks() < g.next {
		return false
	}
	t, ok := targetOf(m)
	if !ok || !g.filter.Test(kit.OtherCtx(m, t)) {
		return false
	}
	d := t.Position().Sub(m.Position()).Len()
	return d >= g.rng.Min && d <= g.rng.Max && kit.CanSee(m, t)
}

// CanContinueToUse ...
func (g *fireAtTarget) CanContinueToUse(m *mob.Mob) bool {
	_, ok := targetOf(m)
	return ok && m.Ticks() <= g.ends
}

// Start ...
func (g *fireAtTarget) Start(m *mob.Mob) {
	g.timer, g.fired = g.pre, false
	g.ends = m.Ticks() + int64(g.pre+g.post)
	kit.Stop(m)
	kit.Flag(m, protocol.EntityDataFlagFacingTargetToRangeAttack, true)
	kit.Sound(m, "inhale")
}

// Tick ...
func (g *fireAtTarget) Tick(m *mob.Mob) {
	t, ok := targetOf(m)
	if !ok {
		return
	}
	m.LookControl().SetLookAtEntity(t, g.maxY, g.maxX)
	if g.fired {
		return
	}
	if g.timer--; g.timer > 0 {
		return
	}
	if !sense.WithinFOV(m, sense.Center(t), g.fov) {
		return
	}
	g.fired = true
	kit.Flag(m, protocol.EntityDataFlagFacingTargetToRangeAttack, false)
	kit.Sound(m, "shoot")
	// BDS FireAtTargetGoal fires projectile_def (the wind charge, spawned
	// by component/boss through combat.RegisterProjectile).
	if g.projectile != "" && combat.ShootProjectile(m, t, g.projectile, 0, 0) {
		return
	}
	if s, ok := mob.ComponentOf[sense.Shooter](m); ok {
		s.Shoot(m, t)
		return
	}
	m.SetData(firedKey{}, ShotsWithoutProjectile(m)+1)
}

// Stop ...
func (g *fireAtTarget) Stop(m *mob.Mob) {
	kit.Flag(m, protocol.EntityDataFlagFacingTargetToRangeAttack, false)
	g.next = m.Ticks() + int64(g.cooldown)
}

// AppendDebugInfo ...
func (g *fireAtTarget) AppendDebugInfo(b *strings.Builder) { b.WriteString("FireAtTarget") }
