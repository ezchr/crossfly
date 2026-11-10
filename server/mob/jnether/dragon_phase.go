//go:build crossfly_full

package jnether

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The phases of the ender dragon (EnderDragonPhase, in id order) and the
// phase manager (EnderDragonPhaseManager). Ids are what the Java client
// gets as DATA_PHASE.
const (
	PhaseHoldingPattern = iota
	PhaseStrafePlayer
	PhaseLandingApproach
	PhaseLanding
	PhaseTakeoff
	PhaseSittingFlaming
	PhaseSittingScanning
	PhaseSittingAttacking
	PhaseChargingPlayer
	PhaseDying
	PhaseHovering
	phaseCount
)

// dragonPhase is DragonPhaseInstance.
type dragonPhase interface {
	id() int
	sitting() bool
	begin(h DragonHost)
	end(h DragonHost)
	serverTick(h DragonHost)
	flyTarget() (mgl64.Vec3, bool)
	flySpeed() float64
	turnSpeed(vel mgl64.Vec3) float64
	onHurt(src world.DamageSource, dmg float64) float64
	crystalDestroyed(h DragonHost, player world.Entity)
}

// phaseManager is EnderDragonPhaseManager: one instance per phase, made on
// first use and kept.
type phaseManager struct {
	d      *Dragon
	phases [phaseCount]dragonPhase
	cur    dragonPhase
}

func (m *phaseManager) init(d *Dragon) {
	m.d = d
	m.cur = m.get(PhaseHovering)
	m.cur.begin(nil)
}

// get is getPhase.
func (m *phaseManager) get(id int) dragonPhase {
	if id < 0 || id >= phaseCount {
		id = PhaseHoldingPattern
	}
	if m.phases[id] == nil {
		base := basePhase{d: m.d, phaseID: id}
		var p dragonPhase
		switch id {
		case PhaseHoldingPattern:
			p = &holdingPattern{basePhase: base}
		case PhaseStrafePlayer:
			p = &strafePlayer{basePhase: base}
		case PhaseLandingApproach:
			p = &landingApproach{basePhase: base}
		case PhaseLanding:
			p = &landing{basePhase: base}
		case PhaseTakeoff:
			p = &takeoff{basePhase: base}
		case PhaseSittingFlaming:
			p = &sittingFlaming{sittingPhase: sittingPhase{base}}
		case PhaseSittingScanning:
			p = &sittingScanning{sittingPhase: sittingPhase{base}}
		case PhaseSittingAttacking:
			p = &sittingAttacking{sittingPhase: sittingPhase{base}}
		case PhaseChargingPlayer:
			p = &chargingPlayer{basePhase: base}
		case PhaseDying:
			p = &dying{basePhase: base}
		case PhaseHovering:
			p = &hovering{basePhase: base}
		}
		m.phases[id] = p
	}
	return m.phases[id]
}

// set is setPhase: the old phase ends, the new one begins (nothing when it
// is already current).
func (m *phaseManager) set(h DragonHost, id int) {
	p := m.get(id)
	if m.cur == p {
		return
	}
	if m.cur != nil {
		m.cur.end(h)
	}
	m.cur = p
	p.begin(h)
}

// basePhase is AbstractDragonPhaseInstance.
type basePhase struct {
	d       *Dragon
	phaseID int
}

func (p *basePhase) id() int                                          { return p.phaseID }
func (p *basePhase) sitting() bool                                    { return false }
func (p *basePhase) begin(DragonHost)                                 {}
func (p *basePhase) end(DragonHost)                                   {}
func (p *basePhase) serverTick(DragonHost)                            {}
func (p *basePhase) flyTarget() (mgl64.Vec3, bool)                    { return mgl64.Vec3{}, false }
func (p *basePhase) flySpeed() float64                                { return 0.6 }
func (p *basePhase) onHurt(_ world.DamageSource, dmg float64) float64 { return dmg }
func (p *basePhase) crystalDestroyed(DragonHost, world.Entity)        {}

// turnSpeed is AbstractDragonPhaseInstance.getTurnSpeed.
func (p *basePhase) turnSpeed(vel mgl64.Vec3) float64 {
	rotSpeed := math.Hypot(vel[0], vel[2]) + 1
	dist := math.Min(rotSpeed, 40)
	return 0.7 / dist / rotSpeed
}

// sittingPhase is AbstractDragonSittingPhase: arrows and wind charges burn
// and bounce off.
type sittingPhase struct{ basePhase }

func (p *sittingPhase) sitting() bool { return true }

func (p *sittingPhase) onHurt(src world.DamageSource, dmg float64) float64 {
	if s, ok := src.(entity.ProjectileDamageSource); ok && s.Projectile != nil {
		switch s.Projectile.H().Type().EncodeEntity() {
		case "minecraft:arrow", "minecraft:thrown_trident", "minecraft:wind_charge_projectile":
			igniteProjectile(s.Projectile)
			return 0
		}
	}
	return dmg
}

// pathTarget is the shared navigateToNextPathNode: the next node, at a
// random height up to 20 above it.
func (p *basePhase) pathTarget(h DragonHost, path *dragonPath) (mgl64.Vec3, bool) {
	if path == nil || path.done() {
		return mgl64.Vec3{}, false
	}
	n := path.nextNode()
	path.advance()
	y := float64(n[1]) + float64(h.Rand().Float32())*20
	return mgl64.Vec3{float64(n[0]), y, float64(n[2])}, true
}

// ringTarget is the ring choice of the holding pattern and takeoff: the
// outer ring with a fight (crystals >= min), else the middle ring.
func ringTarget(i int, outer bool) int {
	if outer {
		i %= 12
		if i < 0 {
			i += 12
		}
		return i
	}
	i -= 12
	i &= 7
	return i + 12
}

// holdingPattern is DragonHoldingPatternPhase: circling the rings; at the
// end of each path a 1/(crystals+3) chance to land, else perhaps strafe the
// player nearest the podium.
type holdingPattern struct {
	basePhase
	path      *dragonPath
	target    mgl64.Vec3
	hasTarget bool
	clockwise bool
}

func (p *holdingPattern) begin(DragonHost)              { p.path, p.hasTarget = nil, false }
func (p *holdingPattern) flyTarget() (mgl64.Vec3, bool) { return p.target, p.hasTarget }
func (p *holdingPattern) serverTick(h DragonHost) {
	if dist := p.distToTarget(h); dist < 100 || dist > 22500 {
		p.findNewTarget(h)
	}
}

// distToTarget is the squared distance to the target, 0 without one.
func (p *holdingPattern) distToTarget(h DragonHost) float64 {
	if !p.hasTarget {
		return 0
	}
	return p.target.Sub(h.Position()).LenSqr()
}

func (p *holdingPattern) findNewTarget(h DragonHost) {
	d := p.d
	tx := h.Tx()
	if p.path != nil && p.path.done() {
		egg := d.podiumTop(tx)
		crystals := d.aliveCrystals()
		if h.Rand().IntN(crystals+3) == 0 {
			d.phases.set(h, PhaseLandingApproach)
			return
		}
		pl := nearestPlayer(tx, egg.Vec3(), 0, true)
		dist := 64.0
		if pl != nil {
			dist = pl.Position().Sub(egg.Vec3Centre()).LenSqr() / 512
		}
		if pl != nil && (h.Rand().IntN(int(dist+2)) == 0 || h.Rand().IntN(crystals+2) == 0) {
			strafe(d, h, pl)
			return
		}
	}
	if p.path == nil || p.path.done() {
		cur := d.closestNode(h)
		t := cur
		if h.Rand().IntN(8) == 0 {
			p.clockwise = !p.clockwise
			t += 6
		}
		if p.clockwise {
			t++
		} else {
			t--
		}
		// "aliveCrystals() >= 0": always the outer ring with a fight.
		t = ringTarget(t, d.Fight != nil && d.aliveCrystals() >= 0)
		p.path = d.findPath(cur, t, nil)
		if p.path != nil {
			p.path.advance()
		}
	}
	if t, ok := p.pathTarget(h, p.path); ok {
		p.target, p.hasTarget = t, true
	}
}

func (p *holdingPattern) crystalDestroyed(h DragonHost, player world.Entity) {
	if player != nil {
		strafe(p.d, h, player)
	}
}

// strafe switches to STRAFE_PLAYER at target.
func strafe(d *Dragon, h DragonHost, target world.Entity) {
	d.phases.set(h, PhaseStrafePlayer)
	d.phases.get(PhaseStrafePlayer).(*strafePlayer).setTarget(h, target)
}

// strafePlayer is DragonStrafePlayerPhase: fly at the player and, with
// line of sight and the head within 10 degrees of it, after 5 ticks of
// charge, shoot a dragon fireball and go back to the holding pattern.
type strafePlayer struct {
	basePhase
	charge    int
	path      *dragonPath
	target    mgl64.Vec3
	hasTarget bool
	attack    *world.EntityHandle
	clockwise bool
}

func (p *strafePlayer) begin(DragonHost) {
	p.charge, p.hasTarget, p.path, p.attack = 0, false, nil, nil
}
func (p *strafePlayer) flyTarget() (mgl64.Vec3, bool) { return p.target, p.hasTarget }

func (p *strafePlayer) setTarget(h DragonHost, t world.Entity) {
	d := p.d
	p.attack = t.H()
	cur := d.closestNode(h)
	tp := t.Position()
	to := d.closestNodeTo(tp[0], tp[1], tp[2])
	fx, fz := int(math.Floor(tp[0])), int(math.Floor(tp[2]))
	pos := h.Position()
	xd, zd := float64(fx)-pos[0], float64(fz)-pos[2]
	ho := math.Min(0.4+math.Sqrt(xd*xd+zd*zd)/80-1, 10)
	final := &dragonNode{x: fx, y: int(math.Floor(tp[1] + ho)), z: fz, heapIdx: -1}
	p.path = d.findPath(cur, to, final)
	if p.path != nil {
		p.path.advance()
		if t, ok := p.pathTarget(h, p.path); ok {
			p.target, p.hasTarget = t, true
		}
	}
}

func (p *strafePlayer) serverTick(h DragonHost) {
	d := p.d
	tx := h.Tx()
	var at world.Entity
	if p.attack != nil {
		at, _ = p.attack.Entity(tx)
	}
	if at == nil {
		d.phases.set(h, PhaseHoldingPattern)
		return
	}
	pos := h.Position()
	tp := at.Position()
	if p.path != nil && p.path.done() {
		xd, zd := tp[0]-pos[0], tp[2]-pos[2]
		ho := math.Min(0.4+math.Sqrt(xd*xd+zd*zd)/80-1, 10)
		p.target, p.hasTarget = mgl64.Vec3{tp[0], tp[1] + ho, tp[2]}, true
	}
	dist := 0.0
	if p.hasTarget {
		dist = p.target.Sub(pos).LenSqr()
	}
	if dist < 100 || dist > 22500 {
		p.findNewTarget(h)
	}
	if tp.Sub(pos).LenSqr() >= 4096 || !lineOfSight(tx, entity.EyePosition(h), entity.EyePosition(at)) {
		if p.charge > 0 {
			p.charge--
		}
		return
	}
	p.charge++
	aim := normalize(mgl64.Vec3{tp[0] - pos[0], 0, tp[2] - pos[2]})
	yaw := h.Yaw() * math.Pi / 180
	dir := normalize(mgl64.Vec3{math.Sin(yaw), 0, -math.Cos(yaw)})
	angle := math.Acos(clamp(dir.Dot(aim), -1, 1))*180/math.Pi + 0.5
	if p.charge >= 5 && angle >= 0 && angle < 10 {
		view := h.Rotation().Vec3()
		head := d.parts[PartHead]
		start := mgl64.Vec3{head[0] - view[0], head[1] + partSize[PartHead][1]/2 + 0.5, head[2] - view[2]}
		target := at.Position().Add(mgl64.Vec3{0, at.H().Type().BBox(at).Height() / 2, 0})
		SpawnHurting(tx, DragonFireballType, HurtingOpts{Owner: h, Pos: start, Dir: target.Sub(start)})
		tx.PlaySound(pos, entitySound{event: "shoot", id: DragonID})
		p.charge = 0
		if p.path != nil {
			p.path.next = len(p.path.nodes)
		}
		d.phases.set(h, PhaseHoldingPattern)
	}
}

func (p *strafePlayer) findNewTarget(h DragonHost) {
	d := p.d
	if p.path == nil || p.path.done() {
		cur := d.closestNode(h)
		t := cur
		if h.Rand().IntN(8) == 0 {
			p.clockwise = !p.clockwise
			t += 6
		}
		if p.clockwise {
			t++
		} else {
			t--
		}
		t = ringTarget(t, d.Fight != nil && d.aliveCrystals() > 0)
		p.path = d.findPath(cur, t, nil)
		if p.path != nil {
			p.path.advance()
		}
	}
	if t, ok := p.pathTarget(h, p.path); ok {
		p.target, p.hasTarget = t, true
	}
}

// landingApproach is DragonLandingApproachPhase: a path to the node on the
// far side of the podium from the nearest player, ending over the podium.
type landingApproach struct {
	basePhase
	path      *dragonPath
	target    mgl64.Vec3
	hasTarget bool
}

func (p *landingApproach) begin(DragonHost)              { p.path, p.hasTarget = nil, false }
func (p *landingApproach) flyTarget() (mgl64.Vec3, bool) { return p.target, p.hasTarget }
func (p *landingApproach) serverTick(h DragonHost) {
	dist := 0.0
	if p.hasTarget {
		dist = p.target.Sub(h.Position()).LenSqr()
	}
	if dist < 100 || dist > 22500 {
		p.findNewTarget(h)
	}
}

func (p *landingApproach) findNewTarget(h DragonHost) {
	d := p.d
	tx := h.Tx()
	if p.path == nil || p.path.done() {
		cur := d.closestNode(h)
		egg := d.podiumTop(tx)
		pl := nearestPlayer(tx, egg.Vec3(), 0, true)
		var t int
		if pl != nil {
			pp := pl.Position()
			aim := normalize(mgl64.Vec3{pp[0], 0, pp[2]})
			t = d.closestNodeTo(-aim[0]*40, 105, -aim[2]*40)
		} else {
			t = d.closestNodeTo(40, float64(egg[1]), 0)
		}
		final := &dragonNode{x: egg[0], y: egg[1], z: egg[2], heapIdx: -1}
		p.path = d.findPath(cur, t, final)
		if p.path != nil {
			p.path.advance()
		}
	}
	if t, ok := p.pathTarget(h, p.path); ok {
		p.target, p.hasTarget = t, true
	}
	if p.path != nil && p.path.done() {
		d.phases.set(h, PhaseLanding)
	}
}

// landing is DragonLandingPhase: straight down onto the podium (fly speed
// 1.5, full turning), then SITTING_SCANNING with the flame count reset.
type landing struct {
	basePhase
	target    mgl64.Vec3
	hasTarget bool
}

func (p *landing) begin(DragonHost)              { p.hasTarget = false }
func (p *landing) flyTarget() (mgl64.Vec3, bool) { return p.target, p.hasTarget }
func (p *landing) flySpeed() float64             { return 1.5 }
func (p *landing) turnSpeed(vel mgl64.Vec3) float64 {
	rotSpeed := math.Hypot(vel[0], vel[2]) + 1
	return math.Min(rotSpeed, 40) / rotSpeed
}
func (p *landing) serverTick(h DragonHost) {
	if !p.hasTarget {
		egg := p.d.podiumTop(h.Tx())
		p.target, p.hasTarget = mgl64.Vec3{float64(egg[0]) + 0.5, float64(egg[1]), float64(egg[2]) + 0.5}, true
	}
	if p.target.Sub(h.Position()).LenSqr() < 1 {
		p.d.phases.get(PhaseSittingFlaming).(*sittingFlaming).count = 0
		p.d.phases.set(h, PhaseSittingScanning)
	}
}

// takeoff is DragonTakeoffPhase: a path to the ring node behind the head,
// HOLDING_PATTERN once 10 blocks from the podium.
type takeoff struct {
	basePhase
	first     bool
	path      *dragonPath
	target    mgl64.Vec3
	hasTarget bool
}

func (p *takeoff) begin(DragonHost)              { p.first, p.path, p.hasTarget = true, nil, false }
func (p *takeoff) flyTarget() (mgl64.Vec3, bool) { return p.target, p.hasTarget }
func (p *takeoff) serverTick(h DragonHost) {
	d := p.d
	if !p.first && p.path != nil {
		egg := d.podiumTop(h.Tx())
		if egg.Vec3Centre().Sub(h.Position()).LenSqr() >= 100 {
			d.phases.set(h, PhaseHoldingPattern)
		}
		return
	}
	p.first = false
	cur := d.closestNode(h)
	look := d.headLook(h)
	t := d.closestNodeTo(-look[0]*40, 105, -look[2]*40)
	t = ringTarget(t, d.Fight != nil && d.aliveCrystals() > 0)
	p.path = d.findPath(cur, t, nil)
	if p.path != nil {
		p.path.advance()
		if t, ok := p.pathTarget(h, p.path); ok {
			p.target, p.hasTarget = t, true
		}
	}
}

// headLook is EnderDragon.getHeadLookVector: while landing or taking off
// the head points down by 6 / (distance to the podium / 4) x 7.5 degrees,
// while sitting by 45 degrees, else the view.
func (d *Dragon) headLook(h DragonHost) mgl64.Vec3 {
	pitch := h.Pitch()
	switch {
	case d.Phase() == PhaseLanding || d.Phase() == PhaseTakeoff:
		egg := d.podiumTop(h.Tx())
		dist := math.Max(math.Sqrt(egg.Vec3Centre().Sub(h.Position()).LenSqr())/4, 1)
		pitch = -6 / dist * 1.5 * 5
	case d.Sitting():
		pitch = -45
	}
	return cube.Rotation{h.Yaw(), pitch}.Vec3()
}

// sittingScanning is DragonSittingScanningPhase: turn to a player within
// 20 (10 vertically); after 25 ticks attack; with nobody for 100 ticks take
// off, charging a player within 150 if there is one.
type sittingScanning struct {
	sittingPhase
	time int
}

func (p *sittingScanning) begin(DragonHost) { p.time = 0 }
func (p *sittingScanning) serverTick(h DragonHost) {
	d := p.d
	tx := h.Tx()
	p.time++
	pos := h.Position()
	var at world.Entity
	best := math.MaxFloat64
	for pl := range tx.Players() {
		pp := pl.Position()
		if !targetable(pl) || math.Abs(pp[1]-pos[1]) > 10 {
			continue
		}
		if dd := pp.Sub(pos).LenSqr(); dd <= 400 && dd < best {
			at, best = pl, dd
		}
	}
	if at != nil {
		if p.time > 25 {
			d.phases.set(h, PhaseSittingAttacking)
			return
		}
		tp := at.Position()
		aim := normalize(mgl64.Vec3{tp[0] - pos[0], 0, tp[2] - pos[2]})
		yaw := h.Yaw() * math.Pi / 180
		dir := normalize(mgl64.Vec3{math.Sin(yaw), 0, -math.Cos(yaw)})
		angle := math.Acos(clamp(dir.Dot(aim), -1, 1))*180/math.Pi + 0.5
		if angle < 0 || angle > 10 {
			head := d.parts[PartHead]
			xd, zd := tp[0]-head[0], tp[2]-head[2]
			delta := clamp(wrapDeg(180-math.Atan2(xd, zd)*180/math.Pi-h.Yaw()), -100, 100)
			d.yRotA *= 0.8
			dist := math.Sqrt(xd*xd+zd*zd) + 1
			rotSpeed := dist
			dist = math.Min(dist, 40)
			d.yRotA += delta * (0.7 / dist / rotSpeed)
			h.SetYaw(h.Yaw() + d.yRotA)
		}
		return
	}
	if p.time >= 100 {
		charge := nearestPlayer(tx, pos, 150, false)
		d.phases.set(h, PhaseTakeoff)
		if charge != nil {
			d.phases.set(h, PhaseChargingPlayer)
			c := d.phases.get(PhaseChargingPlayer).(*chargingPlayer)
			c.target, c.hasTarget = charge.Position(), true
		}
	}
}

// sittingAttacking is DragonSittingAttackingPhase: a 40 tick roar, then
// flames.
type sittingAttacking struct {
	sittingPhase
	ticks int
}

func (p *sittingAttacking) begin(h DragonHost) {
	p.ticks = 0
	if h != nil {
		h.Tx().PlaySound(h.Position(), entitySound{event: "mad", id: DragonID})
	}
}
func (p *sittingAttacking) serverTick(h DragonHost) {
	if p.ticks++; p.ticks > 40 {
		p.d.phases.set(h, PhaseSittingFlaming)
	}
}

// sittingFlaming is DragonSittingFlamingPhase: after 10 ticks a cloud of
// dragon's breath (Harming, radius 5, 200 ticks) on the ground in front of
// the head; after 200 ticks scan again, or take off after the fourth.
type sittingFlaming struct {
	sittingPhase
	ticks, count int
	flame        *world.EntityHandle
}

func (p *sittingFlaming) begin(DragonHost) { p.ticks = 0; p.count++ }
func (p *sittingFlaming) end(h DragonHost) {
	if p.flame != nil && h != nil {
		if e, ok := p.flame.Entity(h.Tx()); ok {
			_ = e.Close()
		}
	}
	p.flame = nil
}
func (p *sittingFlaming) serverTick(h DragonHost) {
	d := p.d
	tx := h.Tx()
	p.ticks++
	if p.ticks >= 200 {
		if p.count >= 4 {
			d.phases.set(h, PhaseTakeoff)
		} else {
			d.phases.set(h, PhaseSittingScanning)
		}
		return
	}
	if p.ticks != 10 {
		return
	}
	head, pos := d.parts[PartHead], h.Position()
	look := normalize(mgl64.Vec3{head[0] - pos[0], 0, head[2] - pos[2]})
	x, z := head[0]+look[0]*5/2, head[2]+look[2]*5/2
	initialY := head[1] + partSize[PartHead][1]/2
	y := initialY
	for {
		bp := cube.PosFromVec3(mgl64.Vec3{x, y, z})
		if _, air := tx.Block(bp).(block.Air); !air {
			break
		}
		if y--; y < 0 {
			y = initialY
			break
		}
	}
	y = math.Floor(y) + 1
	p.flame = entity.NewAreaEffectCloudWith(world.EntitySpawnOpts{Position: mgl64.Vec3{x, y, z}}, potion.Harming(),
		10*time.Second, time.Second, 0, 5, 0, 0)
	tx.AddEntity(p.flame)
}

// chargingPlayer is DragonChargePlayerPhase: straight at a point at fly
// speed 3, back to the holding pattern 10 ticks after arriving.
type chargingPlayer struct {
	basePhase
	target    mgl64.Vec3
	hasTarget bool
	since     int
}

func (p *chargingPlayer) begin(DragonHost)              { p.hasTarget, p.since = false, 0 }
func (p *chargingPlayer) flyTarget() (mgl64.Vec3, bool) { return p.target, p.hasTarget }
func (p *chargingPlayer) flySpeed() float64             { return 3 }
func (p *chargingPlayer) serverTick(h DragonHost) {
	if !p.hasTarget {
		p.d.phases.set(h, PhaseHoldingPattern)
		return
	}
	if p.since > 0 {
		p.since++
		if p.since-1 >= 10 {
			p.d.phases.set(h, PhaseHoldingPattern)
		}
		return
	}
	if dist := p.target.Sub(h.Position()).LenSqr(); dist < 100 || dist > 22500 {
		p.since++
	}
}

// dying is DragonDeathPhase: to the podium at speed 3 with 1 health; within
// 10 blocks (or beyond 150) the health drops to 0 and the death animation
// starts.
type dying struct {
	basePhase
	target    mgl64.Vec3
	hasTarget bool
	time      int
}

func (p *dying) begin(DragonHost)              { p.hasTarget, p.time = false, 0 }
func (p *dying) flyTarget() (mgl64.Vec3, bool) { return p.target, p.hasTarget }
func (p *dying) flySpeed() float64             { return 3 }
func (p *dying) serverTick(h DragonHost) {
	p.time++
	if !p.hasTarget {
		pd := p.d.podium()
		// MOTION_BLOCKING (Java counts leaves here; there are none on
		// the podium).
		y := heightNoLeaves(h.Tx(), pd[0], pd[2])
		p.target, p.hasTarget = mgl64.Vec3{float64(pd[0]) + 0.5, float64(y), float64(pd[2]) + 0.5}, true
	}
	if dist := p.target.Sub(h.Position()).LenSqr(); dist >= 100 && dist <= 22500 {
		h.SetHealth(1)
	} else {
		h.SetHealth(0)
	}
}

// hovering is DragonHoverPhase: staying where it is (a new dragon and one
// placed by a command start here).
type hovering struct {
	basePhase
	target    mgl64.Vec3
	hasTarget bool
}

func (p *hovering) sitting() bool                 { return true }
func (p *hovering) begin(DragonHost)              { p.hasTarget = false }
func (p *hovering) flySpeed() float64             { return 1 }
func (p *hovering) flyTarget() (mgl64.Vec3, bool) { return p.target, p.hasTarget }
func (p *hovering) serverTick(h DragonHost) {
	if !p.hasTarget {
		p.target, p.hasTarget = h.Position(), true
	}
}

// lineOfSight is LivingEntity.hasLineOfSight between two points (eyes):
// within 128 blocks and no block collision box on the line (COLLIDER,
// fluids ignored).
func lineOfSight(tx *world.Tx, from, to mgl64.Vec3) bool {
	if from.Sub(to).LenSqr() > 128*128 {
		return false
	}
	clear := true
	trace.TraverseBlocks(from, to, func(pos cube.Pos) bool {
		if _, ok := trace.BlockIntercept(pos, tx, tx.Block(pos), from, to); ok {
			clear = false
			return false
		}
		return true
	})
	return clear
}
