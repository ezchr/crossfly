//go:build crossfly_full

package boss

import (
	"math"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/mob"
	bc "github.com/df-mc/dragonfly/server/mob/component/boss"
	"github.com/df-mc/dragonfly/server/mob/component/combat"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// Dragon goals (BDS Dragon*Goal). They steer through the dragon state in
// server/mob/component/boss (bc.Dragon: TargetPos, AirSpeed, TurnSpeed, the
// node graph); the movement itself is EnderDragon::postAiStep there. The
// flying group holds takeoff (0), charge (1), strafe (2) and the holding
// pattern (3); the sitting group landing (0), flaming (1) and scanning (2);
// the death group the death goal. Goals switch the groups through the
// on_start_landing / on_start_takeoff / on_death triggers.

// dragonBase is DragonBaseGoalDefinition: active_speed, flight_speed,
// turn_speed, target_zone.
type dragonBase struct {
	mob.GoalBase
	active, flight, turn float64
	zoneMin, zoneMax     float64
}

func parseDragonBase(spec mob.GoalSpec, active float64) (dragonBase, error) {
	var v struct {
		Active *float64 `json:"active_speed"`
		Flight *float64 `json:"flight_speed"`
		Turn   *float64 `json:"turn_speed"`
		Zone   jsonRaw  `json:"target_zone"`
	}
	if err := spec.Decode(&v); err != nil {
		return dragonBase{}, err
	}
	zone, err := sense.ParseRange(v.Zone.raw(), sense.Range{Min: 10, Max: 150})
	if err != nil {
		return dragonBase{}, err
	}
	return dragonBase{
		GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)},
		active:   or(v.Active, active), flight: or(v.Flight, 0.6), turn: or(v.Turn, 0.7),
		zoneMin: zone.Min, zoneMax: zone.Max,
	}, nil
}

// retarget is the shared "pick a new target position" test: none, closer
// than 10 or farther than 150, or a collision.
func retarget(m *mob.Mob, d *bc.Dragon) bool {
	dd := d.TargetDistSq(m)
	return d.TargetPos == (mgl64.Vec3{}) || dd < 100 || dd > 22500 || d.Collided(m)
}

// --- behavior.dragonholdingpattern ---

// holdingPattern is DragonHoldingPatternGoal: the dragon circles the
// rings; when a path ends it may land (1 in crystals+3) or go after the
// nearest player within 64 of the podium it can see.
type holdingPattern struct {
	mob.GoalBase
	path      *bc.DragonPath
	clockwise bool
}

func newHoldingPattern(spec mob.GoalSpec) (mob.Goal, error) {
	return &holdingPattern{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}}, nil
}

func (g *holdingPattern) CanUse(m *mob.Mob) bool {
	_, has := m.Target()
	return !bc.Sitting(m) && !has
}

func (g *holdingPattern) CanContinueToUse(m *mob.Mob) bool {
	_, has := m.Target()
	return !has
}

func (g *holdingPattern) Start(m *mob.Mob) {
	d := bc.DragonOf(m)
	d.TargetPos = mgl64.Vec3{}
	m.ResetTarget()
	g.path = nil
	bc.SetSitting(m, false)
}

func (g *holdingPattern) Stop(m *mob.Mob) {
	bc.DragonOf(m).TargetPos = mgl64.Vec3{}
	g.path = nil
}

func (g *holdingPattern) Tick(m *mob.Mob) {
	d := bc.DragonOf(m)
	if retarget(m, d) {
		g.findNewTarget(m, d)
	}
}

func (g *holdingPattern) findNewTarget(m *mob.Mob, d *bc.Dragon) {
	tx := m.Tx()
	r := m.Rand()
	c := d.NumCrystals
	if g.path != nil && g.path.Done() {
		podium := bc.Podium(tx)
		if r.IntN(c+3) != 0 {
			p, ok := bc.NearestPlayer(tx, podium, 64)
			dd := 64.0
			if ok {
				dd = bc.PodiumDistSq(tx, p.Position()) / 512
			}
			r1 := r.IntN(int(math.Abs(dd))+2) == 0
			if ok && (r1 || r.IntN(c+2) == 0) && sense.CanSee(m, p) {
				m.SetTarget(p)
				return
			}
		} else {
			bc.FireTrigger(m, bc.OnStartLanding)
			return
		}
	}
	if g.path == nil || g.path.Done() {
		j := d.ClosestNode(tx, m.Position())
		k := d.RingNext(j, r.IntN(8) == 0, &g.clockwise)
		g.path = d.FindPath(tx, j, k, nil)
		g.path.Next()
	}
	d.NavigateTo(m, g.path)
}

func (g *holdingPattern) AppendDebugInfo(b *strings.Builder) { b.WriteString("Dragon holding pattern") }

// --- behavior.dragonstrafeplayer ---

// strafePlayer is DragonStrafePlayerGoal: the dragon flies a path to its
// target and, once it has seen it within fireball_range for
// target_in_range_and_in_view_time and faces it within view_angle, shoots
// one dragon fireball (minecraft:shooter) and goes back to circling.
//
//	active_speed 1, flight_speed 0.6, turn_speed 0.7, target_zone {10, 150}
//	switch_direction_probability 0.125
//	target_in_range_and_in_view_time 0.25 s
//	fireball_range 64, view_angle 10
type strafePlayer struct {
	dragonBase
	switchProb float64
	required   int
	rng        float64
	viewAngle  float64

	target    *world.EntityHandle
	path      *bc.DragonPath
	clockwise bool
	fired     bool
	timer     int
}

func newStrafePlayer(spec mob.GoalSpec) (mob.Goal, error) {
	base, err := parseDragonBase(spec, 1)
	if err != nil {
		return nil, err
	}
	var v struct {
		Switch   *float64 `json:"switch_direction_probability"`
		InView   *float64 `json:"target_in_range_and_in_view_time"`
		Range    *float64 `json:"fireball_range"`
		ViewAngl *float64 `json:"view_angle"`
	}
	if err := spec.Decode(&v); err != nil {
		return nil, err
	}
	return &strafePlayer{dragonBase: base, switchProb: or(v.Switch, 0.125), required: bc.Ticks(or(v.InView, 0.25)),
		rng: or(v.Range, 64), viewAngle: or(v.ViewAngl, 10)}, nil
}

func (g *strafePlayer) CanUse(m *mob.Mob) bool {
	_, has := m.Target()
	return !bc.Sitting(m) && has
}

func (g *strafePlayer) CanContinueToUse(m *mob.Mob) bool {
	if g.fired || g.target == nil {
		return false
	}
	t, ok := g.target.Entity(m.Tx())
	return ok && bc.Living(t)
}

func (g *strafePlayer) Start(m *mob.Mob) {
	g.fired, g.timer, g.path, g.target = false, 0, nil, nil
	d := bc.DragonOf(m)
	if t, ok := m.Target(); ok {
		g.setTarget(m, d, t)
	}
	bc.SetSitting(m, false)
	d.AirSpeed, d.TurnSpeed = g.active, g.turn
}

// aimHeight is the height above the target the path ends at.
func aimHeight(m *mob.Mob, t world.Entity) float64 {
	hd := math.Hypot(math.Floor(t.Position()[0])-m.Position()[0], math.Floor(t.Position()[2])-m.Position()[2])
	return math.Min(hd*0.0125-0.6, 10)
}

func (g *strafePlayer) setTarget(m *mob.Mob, d *bc.Dragon, t world.Entity) {
	tx := m.Tx()
	g.target = t.H()
	m.SetTarget(t)
	tp := t.Position()
	final := cube.Pos{int(math.Floor(tp[0])), int(math.Floor(tp[1] + aimHeight(m, t))), int(math.Floor(tp[2]))}
	g.path = d.FindPath(tx, d.ClosestNode(tx, m.Position()), d.ClosestNode(tx, tp), &final)
	g.path.Next()
	d.NavigateTo(m, g.path)
}

func (g *strafePlayer) Stop(m *mob.Mob) {
	d := bc.DragonOf(m)
	m.ResetTarget()
	d.TargetPos = mgl64.Vec3{}
	d.AirSpeed, d.TurnSpeed = g.flight, g.turn
}

func (g *strafePlayer) Tick(m *mob.Mob) {
	tx := m.Tx()
	d := bc.DragonOf(m)
	t, ok := g.target.Entity(tx)
	if !ok {
		return
	}
	if g.path.Done() {
		tp := t.Position()
		d.TargetPos = mgl64.Vec3{tp[0], tp[1] + aimHeight(m, t), tp[2]}
	}
	dd := d.TargetDistSq(m)
	if d.TargetPos == (mgl64.Vec3{}) || dd < g.zoneMin*g.zoneMin || dd > g.zoneMax*g.zoneMax {
		if g.path.Done() {
			j := d.ClosestNode(tx, m.Position())
			k := d.RingNext(j, m.Rand().Float64() < g.switchProb, &g.clockwise)
			g.path = d.FindPath(tx, j, k, nil)
			g.path.Next()
		}
		d.NavigateTo(m, g.path)
	}
	if t.Position().Sub(m.Position()).LenSqr() >= g.rng*g.rng || !sense.CanSee(m, t) {
		g.timer = max(g.timer-1, 0)
		return
	}
	g.timer++
	to := t.Position().Sub(m.Position())
	to[1] = 0
	face := mgl64.Vec3{math.Sin(mgl64.DegToRad(m.Yaw())), 0, -math.Cos(mgl64.DegToRad(m.Yaw()))}
	if to.Len() == 0 {
		return
	}
	angle := mgl64.RadToDeg(math.Acos(math.Max(-1, math.Min(1, to.Normalize().Dot(face))))) + 0.5
	if g.timer >= g.required && angle >= 0 && angle < g.viewAngle && !g.fired {
		if s, ok := mob.ComponentOf[*combat.Shooter](m); ok {
			s.Shoot(m, t)
		} else {
			bc.ShootDragonFireball(m, t)
		}
		g.timer = 0
		g.path.Finish()
		g.fired = true
	}
}

func (g *strafePlayer) AppendDebugInfo(b *strings.Builder) { b.WriteString("Dragon strafe player") }

// --- behavior.dragonchargeplayer ---

// chargePlayer is DragonChargePlayerGoal: right after a perch, the dragon
// dives at active_speed 3 to where the nearest player within 150 was, for
// continue_charge_threshold_time after it arrives.
type chargePlayer struct {
	dragonBase
	threshold int
	timer     int
}

func newChargePlayer(spec mob.GoalSpec) (mob.Goal, error) {
	base, err := parseDragonBase(spec, 3)
	if err != nil {
		return nil, err
	}
	var v struct {
		Threshold *float64 `json:"continue_charge_threshold_time"`
	}
	if err := spec.Decode(&v); err != nil {
		return nil, err
	}
	return &chargePlayer{dragonBase: base, threshold: bc.Ticks(or(v.Threshold, 0.5))}, nil
}

func (g *chargePlayer) CanUse(m *mob.Mob) bool {
	_, has := m.Target()
	return bc.Sitting(m) && has
}

func (g *chargePlayer) CanContinueToUse(m *mob.Mob) bool {
	d := bc.DragonOf(m)
	if d.TargetPos == (mgl64.Vec3{}) {
		return false
	}
	if g.timer <= 0 {
		return true
	}
	old := g.timer
	g.timer++
	return old < g.threshold
}

func (g *chargePlayer) Start(m *mob.Mob) {
	d := bc.DragonOf(m)
	g.timer = 0
	bc.SetSitting(m, false)
	d.AirSpeed, d.TurnSpeed = g.active, 0.7
	d.TargetPos = mgl64.Vec3{}
	if p, ok := bc.NearestPlayer(m.Tx(), m.Position(), g.zoneMax); ok {
		d.TargetPos = p.Position()
	}
}

func (g *chargePlayer) Tick(m *mob.Mob) {
	d := bc.DragonOf(m)
	dd := d.TargetDistSq(m)
	if dd < g.zoneMin*g.zoneMin || dd > g.zoneMax*g.zoneMax || d.Collided(m) {
		g.timer++
	}
}

func (g *chargePlayer) Stop(m *mob.Mob) {
	d := bc.DragonOf(m)
	g.timer = 0
	d.TargetPos = mgl64.Vec3{}
	d.AirSpeed = g.flight
	m.ResetTarget()
}

func (g *chargePlayer) AppendDebugInfo(b *strings.Builder) { b.WriteString("Dragon charge player") }

// --- behavior.dragontakeoff ---

// takeoff is DragonTakeoffGoal: from the podium the dragon flies to the
// ring node 40 blocks behind its head and is done 10 blocks from the
// podium.
type takeoff struct {
	mob.GoalBase
	path  *bc.DragonPath
	first bool
}

func newTakeoff(spec mob.GoalSpec) (mob.Goal, error) {
	return &takeoff{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}}, nil
}

func (g *takeoff) CanUse(m *mob.Mob) bool {
	_, has := m.Target()
	return bc.Sitting(m) && !has
}

func (g *takeoff) CanContinueToUse(m *mob.Mob) bool {
	return bc.PodiumDistSq(m.Tx(), m.Position()) <= 100
}

func (g *takeoff) Start(m *mob.Mob) {
	bc.DragonOf(m).TargetPos = mgl64.Vec3{}
	g.path = nil
	bc.SetSitting(m, false)
	g.first = true
}

func (g *takeoff) Stop(m *mob.Mob) {
	bc.DragonOf(m).TargetPos = mgl64.Vec3{}
	g.path = nil
}

func (g *takeoff) Tick(m *mob.Mob) {
	if !g.first {
		return
	}
	g.first = false
	tx := m.Tx()
	d := bc.DragonOf(m)
	look := bc.HeadLook(m)
	j := d.ClosestNode(tx, mgl64.Vec3{look[0] * -40, 105, look[2] * -40})
	g.path = d.FindPath(tx, d.ClosestNode(tx, m.Position()), d.RingEnd(j), nil)
	d.NavigateTo(m, g.path)
}

func (g *takeoff) AppendDebugInfo(b *strings.Builder) { b.WriteString("Dragon takeoff") }

// --- behavior.dragonlanding ---

// landing is DragonLandingGoal: a path to the ring node facing the nearest
// player (or the east one), then the final approach onto the podium at
// air speed 1.5 with full turning; within 1 block it sits.
type landing struct {
	mob.GoalBase
	path  *bc.DragonPath
	final bool
}

func newLanding(spec mob.GoalSpec) (mob.Goal, error) {
	return &landing{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}}, nil
}

func (g *landing) CanUse(m *mob.Mob) bool { return !bc.Sitting(m) }

func (g *landing) CanContinueToUse(m *mob.Mob) bool {
	if !g.final {
		return true
	}
	d := bc.DragonOf(m)
	if d.TargetDistSq(m) < 1 {
		d.FlameCount = 0
		bc.SetSitting(m, true)
		return false
	}
	return true
}

func (g *landing) reset(m *mob.Mob) {
	d := bc.DragonOf(m)
	d.TargetPos = mgl64.Vec3{}
	d.AirSpeed, d.TurnSpeed = 0.6, 0.7
	g.final = false
}

func (g *landing) Start(m *mob.Mob) {
	g.reset(m)
	g.path = nil
}

func (g *landing) Stop(m *mob.Mob) { g.reset(m) }

func (g *landing) Tick(m *mob.Mob) {
	d := bc.DragonOf(m)
	if g.final || !retarget(m, d) {
		return
	}
	tx := m.Tx()
	podium := bc.Podium(tx)
	if g.path == nil || g.path.Done() {
		var j int
		if p, ok := bc.NearestPlayer(tx, podium, 128); ok {
			n := mgl64.Vec3{p.Position()[0], 0, p.Position()[2]}
			if n.Len() > 0 {
				n = n.Normalize()
			}
			j = d.ClosestNode(tx, mgl64.Vec3{n[0] * -40, 105, n[2] * -40})
		} else {
			j = d.ClosestNode(tx, mgl64.Vec3{40, podium[1], 0})
		}
		end := cube.Pos{0, int(podium[1]), 0}
		g.path = d.FindPath(tx, d.ClosestNode(tx, m.Position()), j, &end)
		if g.path == nil {
			g.path = &bc.DragonPath{Nodes: []cube.Pos{end}}
		}
		g.path.Next()
	}
	if !g.path.Done() {
		d.NavigateTo(m, g.path)
	}
	if g.path.Done() {
		g.final = true
		d.TargetPos = podium
		d.AirSpeed, d.TurnSpeed = 1.5, 0
	}
}

func (g *landing) AppendDebugInfo(b *strings.Builder) { b.WriteString("Dragon landing") }

// --- behavior.dragonscanning ---

// scanning is DragonScanningGoal: perched, the dragon turns its head to the
// nearest player within 20 and after 25 ticks makes it the target (the
// flaming goal); with nobody near for 100 ticks it takes off, charging a
// visible player within 150.
type scanning struct {
	mob.GoalBase
	timer float64
}

func newScanning(spec mob.GoalSpec) (mob.Goal, error) {
	return &scanning{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}}, nil
}

func (g *scanning) CanUse(m *mob.Mob) bool {
	_, has := m.Target()
	return bc.Sitting(m) && !has
}

func (g *scanning) CanContinueToUse(m *mob.Mob) bool {
	tx := m.Tx()
	if p, ok := bc.NearestPlayer(tx, m.Position(), 20); ok {
		if g.timer > 25 {
			m.SetTarget(p)
			return false
		}
		return true
	}
	if g.timer < 100 {
		return true
	}
	if p, ok := bc.NearestPlayer(tx, m.Position(), 150); ok && sense.CanSee(m, p) {
		m.SetTarget(p)
	}
	bc.FireTrigger(m, bc.OnStartTakeoff)
	return false
}

func (g *scanning) Start(m *mob.Mob) {
	g.timer = 0
	bc.DragonOf(m).TargetPos = mgl64.Vec3{}
	m.ResetTarget()
	bc.SetSitting(m, true)
}

func (g *scanning) Stop(m *mob.Mob) { bc.DragonOf(m).TargetPos = mgl64.Vec3{} }

func (g *scanning) Tick(m *mob.Mob) {
	g.timer++
	p, ok := bc.NearestPlayer(m.Tx(), m.Position(), 20)
	if !ok {
		return
	}
	d := bc.DragonOf(m)
	yaw := m.Yaw()
	to := p.Position().Sub(m.Position())
	to[1] = 0
	face := mgl64.Vec3{math.Sin(mgl64.DegToRad(yaw)), 0, -math.Cos(mgl64.DegToRad(yaw))}
	if to.Len() == 0 {
		return
	}
	angle := mgl64.RadToDeg(math.Acos(math.Max(-1, math.Min(1, to.Normalize().Dot(face)))))
	if angle >= -0.5 && angle <= 9.5 {
		return
	}
	head := d.HeadPos()
	if head == (mgl64.Vec3{}) {
		head = m.Position()
	}
	dx, dz := p.Position()[0]-math.Floor(head[0]), p.Position()[2]-math.Floor(head[2])
	f := math.Max(-100, math.Min(100, wrap(180-mgl64.RadToDeg(math.Atan2(dx, dz))-yaw)))
	gg := math.Hypot(dx, dz) + 1
	d.YRotA = d.YRotA*0.8 + f/(math.Min(gg, 40)*gg)
	m.SetYaw(wrap(yaw + d.YRotA))
}

func (g *scanning) AppendDebugInfo(b *strings.Builder) { b.WriteString("Dragon scanning") }

// --- behavior.dragonflaming ---

// flaming is DragonFlamingGoal: the perched dragon roars for roar_time,
// breathes for flame_time and leaves a cloud of dragon's breath (harming)
// in front of its head, then waits out cooldown_time; after
// ground_flame_count flames it takes off.
//
//	ground_flame_count 4, cooldown_time 10 s, flame_time 0.5 s,
//	roar_time 2 s, smoke_time 10 s, smoke_radius 5, smoke_color #DC00EF
type flaming struct {
	mob.GoalBase
	count                         int
	cooldown, flameTime, roarTime int
	smokeTime                     int
	radius                        float64

	flameTicks, roarTicks int
}

func newFlaming(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Count    *int     `json:"ground_flame_count"`
		Cooldown *float64 `json:"cooldown_time"`
		Flame    *float64 `json:"flame_time"`
		Roar     *float64 `json:"roar_time"`
		Smoke    *float64 `json:"smoke_time"`
		Radius   *float64 `json:"smoke_radius"`
	}
	if err := spec.Decode(&v); err != nil {
		return nil, err
	}
	return &flaming{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)},
		count: or(v.Count, 4), cooldown: bc.Ticks(or(v.Cooldown, 10)), flameTime: bc.Ticks(or(v.Flame, 0.5)),
		roarTime: bc.Ticks(or(v.Roar, 2)), smokeTime: bc.Ticks(or(v.Smoke, 10)), radius: or(v.Radius, 5)}, nil
}

func (g *flaming) CanUse(m *mob.Mob) bool {
	_, has := m.Target()
	return has
}

func (g *flaming) CanContinueToUse(m *mob.Mob) bool {
	if g.flameTicks < g.cooldown {
		return true
	}
	if bc.DragonOf(m).FlameCount >= g.count {
		bc.FireTrigger(m, bc.OnStartTakeoff)
	}
	return false
}

func (g *flaming) Start(m *mob.Mob) {
	d := bc.DragonOf(m)
	g.roarTicks = 0
	d.TargetPos = mgl64.Vec3{}
	bc.SetSitting(m, true)
	d.FlameCount++
}

func (g *flaming) Stop(m *mob.Mob) {
	g.flameTicks, g.roarTicks = 0, 0
	bc.DragonOf(m).TargetPos = mgl64.Vec3{}
	m.ResetTarget()
}

func (g *flaming) Tick(m *mob.Mob) {
	if g.roarTicks < g.roarTime {
		if g.roarTicks%10 == 0 {
			m.PlaySound("mad")
		}
		g.roarTicks++
		return
	}
	g.flameTicks++
	if g.flameTicks%2 == 0 && g.flameTicks < g.flameTime {
		bc.ShowActorEvent(m.Tx(), m, packet.ActorEventPuke)
	}
	if g.flameTicks == g.flameTime {
		g.breath(m)
	}
}

// breath leaves the cloud half a radius in front of the head, on the
// ground below it.
func (g *flaming) breath(m *mob.Mob) {
	tx := m.Tx()
	d := bc.DragonOf(m)
	head := d.HeadPos()
	if head == (mgl64.Vec3{}) {
		head = m.Position()
	}
	head = mgl64.Vec3{math.Floor(head[0]), math.Floor(head[1]), math.Floor(head[2])}
	dir := mgl64.Vec3{head[0] - m.Position()[0], 0, head[2] - m.Position()[2]}
	if dir.Len() > 0 {
		dir = dir.Normalize()
	}
	x, z := head[0]+dir[0]*g.radius*0.5, head[2]+dir[2]*g.radius*0.5
	y := head[1]
	for {
		p := cube.Pos{int(math.Floor(x)), int(math.Floor(y)), int(math.Floor(z))}
		if _, air := tx.Block(p).(block.Air); !air {
			break
		}
		if y--; y < float64(tx.Range()[0]) {
			y = head[1]
			break
		}
	}
	pos := mgl64.Vec3{x, math.Floor(y) + 1, z}
	dur := time.Duration(g.smokeTime) * time.Second / 20
	cloud := entity.NewAreaEffectCloudWith(world.EntitySpawnOpts{Position: pos}, potion.Harming(),
		dur, time.Second, 0, g.radius, -0.5, -g.radius/float64(max(g.smokeTime, 1)))
	tx.AddEntity(cloud)
	tx.AddParticle(pos, particle.Named{Bedrock: "minecraft:dragon_breath_fire", Java: "dragon_breath"})
}

func (g *flaming) AppendDebugInfo(b *strings.Builder) { b.WriteString("Dragon flaming") }

// --- behavior.dragondeath ---

// death is DragonDeathGoal: the dying dragon (held at 1 health) flies to
// the podium at speed 3 with explosions every 10 ticks; there (or when it
// is stuck or 150 away) the death animation starts.
type death struct {
	mob.GoalBase
	counter int
}

func newDeath(spec mob.GoalSpec) (mob.Goal, error) {
	return &death{GoalBase: mob.GoalBase{Control: spec.FlagsOr(0)}}, nil
}

func (g *death) CanUse(m *mob.Mob) bool { return m.Health() > 0 }

func (g *death) Start(m *mob.Mob) {
	d := bc.DragonOf(m)
	d.TargetPos = mgl64.Vec3{}
	d.AirSpeed, d.TurnSpeed = 3, 0.7
	g.counter = 0
}

func (g *death) Stop(m *mob.Mob) {
	d := bc.DragonOf(m)
	d.TargetPos = mgl64.Vec3{}
	d.AirSpeed = 0.6
}

func (g *death) Tick(m *mob.Mob) {
	tx := m.Tx()
	d := bc.DragonOf(m)
	g.counter++
	if g.counter%10 == 0 {
		r := m.Rand()
		tx.AddParticle(m.Position().Add(mgl64.Vec3{(r.Float64() - 0.5) * 8, (r.Float64()-0.5)*4 + 2, (r.Float64() - 0.5) * 8}), particle.HugeExplosion{})
	}
	if d.TargetPos == (mgl64.Vec3{}) {
		d.TargetPos = bc.Podium(tx)
	}
	dd := d.TargetDistSq(m)
	if dd < 100 || dd > 22500 || d.Collided(m) {
		d.StartDeathAnimation(m)
		return
	}
	m.SetHealth(1)
}

func (g *death) AppendDebugInfo(b *strings.Builder) { b.WriteString("Dragon death") }

// wrap wraps an angle to [-180, 180).
func wrap(a float64) float64 {
	a = math.Mod(a+180, 360)
	if a < 0 {
		a += 360
	}
	return a - 180
}
