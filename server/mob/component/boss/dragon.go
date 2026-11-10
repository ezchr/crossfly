//go:build crossfly_full

package boss

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// DragonID is the ender dragon's identifier.
const DragonID = "minecraft:ender_dragon"

// Dragon constants (EnderDragon statics).
const (
	// SittingAllowedDamage is SITTING_ALLOWED_DAMAGE_PERCENTAGE: a sitting
	// dragon takes off after losing this share of its max health.
	SittingAllowedDamage = 0.25
	// PodiumY is EndPodiumFeature::END_PODIUM_LOCATION's y: the column
	// (0, 0) is searched downwards from here for the podium top.
	PodiumY = 100
	// dragonSeaLevel is the End's sea level the node heights are kept above
	// (+10).
	dragonSeaLevel = 63
)

type dragonKey struct{}

// Dragon is the state BDS keeps on the EnderDragon actor and the dragon
// goals (server/mob/goal/boss) share: the target position it steers to,
// its flight and turn speed, the flame count of a perch, the crystals, the
// node graph of its flight paths, the flight history and the death
// animation. Movement is EnderDragon::postAiStep, run by the system
// "boss/dragon" after travel (dragonTick).
type Dragon struct {
	// TargetPos is the position the dragon steers to; the zero vector is
	// "none".
	TargetPos mgl64.Vec3
	// AirSpeed (MobMovement air speed, 0.6) caps the climb rate; TurnSpeed
	// (0.7) scales turning, 0 turns at full rate (the landing approach).
	AirSpeed, TurnSpeed float64
	// FlameCount is the number of flame attacks during this perch.
	FlameCount int
	// NumCrystals is the number of end crystals alive near the dragon,
	// counted once a second.
	NumCrystals int

	nodes       []cube.Pos
	nodesLoaded bool

	history [64][2]float64
	histPtr int
	YRotA   float64
	delta   mgl64.Vec3
	growl   int
	flap    float64
	prev    mgl64.Vec3
	started bool

	sittingDamage float64
	crystal       *world.EntityHandle
	// crystals are the end crystals around the dragon and where they stand:
	// one of them gone was destroyed (onCrystalDestroyed).
	crystals map[*world.EntityHandle]mgl64.Vec3
	inWall   bool

	// Parts are the hit boxes of the body parts, updated every tick
	// (ActorCollision sub-AABBs: head 1, neck 3, body 5x3x5, three tail
	// segments 2, wings 4x3x4).
	Head, Neck, Body, Wing1, Wing2 cube.BBox
	Tail                           [3]cube.BBox
	// fight is the End fight this dragon belongs to (nil for a dragon
	// summoned outside it).
	fight *EndFight
	// prevKilled is EnderDragon::setHasDragonPreviouslyBeenKilled, set by
	// the fight: such a dragon drops 500 experience instead of 12000.
	prevKilled bool

	dying     bool
	deathTime int
	xp        int

	headHit       bool
	pendingDeath  bool
	pendingTakeof bool
}

// DragonOf returns m's dragon state (made on first use).
func DragonOf(m *mob.Mob) *Dragon {
	if v, ok := m.Data(dragonKey{}); ok {
		return v.(*Dragon)
	}
	d := &Dragon{AirSpeed: 0.6, TurnSpeed: 0.7, growl: 100, histPtr: -1}
	m.SetData(dragonKey{}, d)
	return d
}

// Sitting reports whether the dragon perches (actor flag sitting).
func Sitting(m *mob.Mob) bool { return m.Meta().Sitting }

// SetSitting is EnderDragon::setSitting: the sitting flag, and the motion
// stops.
func SetSitting(m *mob.Mob, v bool) {
	m.UpdateMeta(func(meta *mob.Meta) { meta.Sitting = v })
	DragonOf(m).delta = mgl64.Vec3{}
}

// Dying reports whether the death animation runs.
func (d *Dragon) Dying() bool { return d.dying }

// DeathTime is the death animation tick.
func (d *Dragon) DeathTime() int { return d.deathTime }

// Podium is the top of the exit portal (0, H, 0): the first air above the
// highest solid-topped block of column (0, 0) at or below y 100.
func Podium(tx *world.Tx) mgl64.Vec3 {
	return mgl64.Vec3{0, float64(AboveTopSolid(tx, 0, PodiumY, 0)), 0}
}

// PodiumDistSq is the squared distance of pos to the centre of the podium
// block (BDS distanceSqrToBlockPosCenter).
func PodiumDistSq(tx *world.Tx, pos mgl64.Vec3) float64 {
	p := Podium(tx)
	return pos.Sub(mgl64.Vec3{0.5, p[1] + 0.5, 0.5}).LenSqr()
}

// TargetDistSq is the squared distance to TargetPos (0 when there is none).
func (d *Dragon) TargetDistSq(m *mob.Mob) float64 {
	if d.TargetPos == (mgl64.Vec3{}) {
		return 0
	}
	return m.Position().Sub(d.TargetPos).LenSqr()
}

// Collided reports a horizontal or vertical collision this tick (the dragon
// pushing through unbreakable blocks).
func (d *Dragon) Collided(m *mob.Mob) bool {
	p := m.Physics()
	return d.inWall || p.HorizontalCollision || p.VerticalCollision
}

// HeadLook is EnderDragon::getHeadLookVector: the view vector, pitched
// -45 while sitting.
func HeadLook(m *mob.Mob) mgl64.Vec3 {
	pitch := m.Pitch()
	if Sitting(m) {
		pitch = -45
	}
	return dirOf(m.Yaw(), pitch)
}

// dirOf is the Bedrock view vector of yaw/pitch.
func dirOf(yaw, pitch float64) mgl64.Vec3 {
	return mgl64.Vec3{-sinDeg(yaw) * cosDeg(pitch), -sinDeg(pitch), cosDeg(yaw) * cosDeg(pitch)}
}

// HeadPos is the centre of the head part.
func (d *Dragon) HeadPos() mgl64.Vec3 {
	if d.Head == (cube.BBox{}) {
		return mgl64.Vec3{}
	}
	mn, mx := d.Head.Min(), d.Head.Max()
	return mn.Add(mx).Mul(0.5)
}

// --- Node graph (EnderDragon::findClosestNode / findPath) ---

// dragonNeighbours are the node adjacency masks (identical to Java).
var dragonNeighbours = [24]int{6146, 8197, 8202, 16404, 32808, 32848, 65696, 131392, 131712, 263424, 526848, 525313, 1581057, 3166214, 2138120, 6373424, 4358208, 12910976, 9044480, 9706496, 15216640, 13688832, 11763712, 8257536}

// buildNodes makes the 24 path nodes: 12 on a ring of 60 (5 above the
// ground), 8 on a ring of 40 (15 above) and 4 on a ring of 20 (5 above),
// at least 10 above the sea and at most y 128. They are rebuilt until the
// chunks within 60 of the origin are loaded.
func (d *Dragon) buildNodes(tx *world.Tx) {
	if d.nodes != nil && d.nodesLoaded {
		return
	}
	nodes := make([]cube.Pos, 24)
	loaded := true
	for i := range 24 {
		var a, r float64
		yAdj := 5
		switch {
		case i < 12:
			a, r = float64(i)*math.Pi/6-2*math.Pi, 60
		case i < 20:
			a, r, yAdj = float64(i-12)*math.Pi/4-2*math.Pi, 40, 15
		default:
			a, r = float64(i-20)*math.Pi/2-2*math.Pi, 20
		}
		x, z := int(math.Cos(a)*r), int(math.Sin(a)*r)
		if _, ok := tx.BlockLoaded(cube.Pos{x, 64, z}); !ok {
			loaded = false
		}
		y := min(128, max(dragonSeaLevel+10, AboveTopSolid(tx, x, tx.Range()[1], z)+yAdj))
		nodes[i] = cube.Pos{x, y, z}
	}
	d.nodes, d.nodesLoaded = nodes, loaded
}

// Node returns node i.
func (d *Dragon) Node(tx *world.Tx, i int) cube.Pos {
	d.buildNodes(tx)
	return d.nodes[i]
}

// firstNode is 0 with crystals alive (the outer ring is used), else 12.
func (d *Dragon) firstNode() int {
	if d.NumCrystals > 0 {
		return 0
	}
	return 12
}

// ClosestNode is findClosestNode(pos).
func (d *Dragon) ClosestNode(tx *world.Tx, pos mgl64.Vec3) int {
	d.buildNodes(tx)
	p := cube.PosFromVec3(pos)
	best, bestD := 0, 10000.0
	for i := d.firstNode(); i < 24; i++ {
		n := d.nodes[i]
		dd := mgl64.Vec3{float64(n[0] - p[0]), float64(n[1] - p[1]), float64(n[2] - p[2])}.LenSqr()
		if dd < bestD {
			best, bestD = i, dd
		}
	}
	return best
}

// RingNext picks the next node on the ring from the closest node j (the
// holding pattern, strafe and takeoff): a switch flips the direction and
// jumps 6 ahead; then one step, kept on the outer ring with crystals alive
// and on the middle ring without.
func (d *Dragon) RingNext(j int, switchDir bool, clockwise *bool) int {
	if switchDir {
		*clockwise = !*clockwise
		j += 6
	}
	if *clockwise {
		j++
	} else {
		j--
	}
	if d.NumCrystals > 0 {
		j %= 12
		if j < 0 {
			j += 12
		}
		return j
	}
	return (j-4)&7 + 12
}

// RingEnd maps a node to the ring in use (takeoff: j%12, or the middle
// ring).
func (d *Dragon) RingEnd(j int) int {
	if d.NumCrystals > 0 {
		return j % 12
	}
	return (j-4)&7 + 12
}

// DragonPath is a node path (BDS Path of PathfinderNodes).
type DragonPath struct {
	Nodes []cube.Pos
	I     int
}

// Done reports whether every node was reached.
func (p *DragonPath) Done() bool { return p == nil || p.I >= len(p.Nodes) }

// Next advances to the next node.
func (p *DragonPath) Next() {
	if p != nil {
		p.I++
	}
}

// Finish marks the path done.
func (p *DragonPath) Finish() {
	if p != nil {
		p.I = len(p.Nodes)
	}
}

// FindPath is EnderDragon::findPath: A* over the node graph from node a to
// b (nodes from firstNode on, joined by the adjacency masks), ending with
// final if given. A path that cannot reach b ends at the closest node
// reached; it is nil when that is a.
func (d *Dragon) FindPath(tx *world.Tx, a, b int, final *cube.Pos) *DragonPath {
	d.buildNodes(tx)
	dist := func(i, j int) float64 {
		x, y := d.nodes[i], d.nodes[j]
		return mgl64.Vec3{float64(x[0] - y[0]), float64(x[1] - y[1]), float64(x[2] - y[2])}.Len()
	}
	g := map[int]float64{a: 0}
	from := map[int]int{}
	open := map[int]bool{a: true}
	closed := map[int]bool{}
	closest, closestH := a, dist(a, b)
	end := -1
	for len(open) > 0 {
		cur, curF := -1, math.Inf(1)
		for n := range open {
			if f := g[n] + dist(n, b); f < curF || (f == curF && n < cur) {
				cur, curF = n, f
			}
		}
		delete(open, cur)
		closed[cur] = true
		if cur == b {
			end = b
			break
		}
		if h := dist(cur, b); h < closestH {
			closest, closestH = cur, h
		}
		for j := d.firstNode(); j < 24; j++ {
			if dragonNeighbours[cur]&(1<<j) == 0 || closed[j] {
				continue
			}
			ng := g[cur] + dist(cur, j)
			if old, ok := g[j]; !ok || ng < old {
				g[j], from[j] = ng, cur
				open[j] = true
			}
		}
	}
	if end < 0 {
		if closest == a {
			return nil
		}
		end = closest
	}
	var rev []cube.Pos
	for n := end; ; {
		rev = append(rev, d.nodes[n])
		p, ok := from[n]
		if !ok {
			break
		}
		n = p
	}
	nodes := make([]cube.Pos, 0, len(rev)+1)
	for i := len(rev) - 1; i >= 0; i-- {
		nodes = append(nodes, rev[i])
	}
	if final != nil {
		nodes = append(nodes, *final)
	}
	return &DragonPath{Nodes: nodes}
}

// NavigateTo steers to the next node of p (y raised by up to 20) and
// advances the path.
func (d *Dragon) NavigateTo(m *mob.Mob, p *DragonPath) {
	if p.Done() {
		return
	}
	n := p.Nodes[p.I]
	d.TargetPos = mgl64.Vec3{float64(n[0]), float64(n[1]) + m.Rand().Float64()*20, float64(n[2])}
	p.Next()
}

// --- Movement (EnderDragon::postAiStep) ---

// latency returns the flight history entry n ticks back (yaw, y).
func (d *Dragon) latency(n int) (float64, float64) {
	e := d.history[(d.histPtr-n+128)&63]
	return e[0], e[1]
}

// dragonStill keeps the travel step from moving the dragon (its movement
// is dragonTick's own).
func dragonStill(m *mob.Mob) {
	if m.Identifier() != DragonID {
		return
	}
	m.SetVelocity(mgl64.Vec3{})
	*m.Input() = mob.Input{}
}

// dragonTick is EnderDragon::postAiStep on the server.
func dragonTick(m *mob.Mob) {
	if m.Identifier() != DragonID {
		return
	}
	d := DragonOf(m)
	tx := m.Tx()
	if d.dying {
		d.tickDeath(m)
		return
	}
	if d.pendingDeath {
		d.pendingDeath = false
		FireTrigger(m, OnDeath)
	}
	if d.pendingTakeof {
		d.pendingTakeof = false
		FireTrigger(m, OnStartTakeoff)
	}
	pos := m.Position()
	if !d.started {
		d.prev = pos
	}
	sitting := Sitting(m)

	// Sounds: wing flaps and the growl.
	prevFlap := d.flap
	if !sitting {
		if d.growl--; d.growl < 0 {
			m.PlaySound("mad")
			d.growl = 200 + m.Rand().IntN(200)
		}
	}
	d.checkCrystals(m)
	moved := pos.Sub(d.prev)
	f := 0.2 / (math.Hypot(moved[0], moved[2])*10 + 1) * math.Pow(2, d.prev[1]-pos[1])
	switch {
	case sitting:
		d.flap += 0.1
	case d.inWall:
		d.flap += f * 0.5
	default:
		d.flap += f
	}
	if math.Cos(prevFlap*2*math.Pi) <= -0.3 && math.Cos(d.flap*2*math.Pi) >= -0.3 {
		m.PlaySound("flap")
	}
	d.prev = pos

	// Flight history.
	yaw := wrapDegrees(m.Yaw())
	m.SetYaw(yaw)
	if !d.started {
		for i := range d.history {
			d.history[i] = [2]float64{yaw, pos[1]}
		}
		d.started = true
	}
	d.histPtr = (d.histPtr + 1) & 63
	d.history[d.histPtr] = [2]float64{yaw, pos[1]}

	// Steering towards TargetPos.
	if d.TargetPos != (mgl64.Vec3{}) {
		t := d.TargetPos.Sub(pos)
		distSq := t.LenSqr()
		h := math.Hypot(t[0], t[2])
		v := 0.0
		if h > 0 {
			v = t[1] / h
		}
		v = clamp(v, -d.AirSpeed, d.AirSpeed)
		d.delta[1] += v * 0.1
		rot := clamp(wrapDegrees(180-mgl64.RadToDeg(math.Atan2(t[0], t[2]))-yaw), -50, 50)
		toT := t
		if toT.Len() > 0 {
			toT = toT.Normalize()
		}
		face := mgl64.Vec3{sinDeg(yaw), d.delta[1], -cosDeg(yaw)}
		if face.Len() > 0 {
			face = face.Normalize()
		}
		l := math.Max(face.Dot(toT)*2/3+1.0/3, 0)
		fs := math.Hypot(d.delta[0], d.delta[2]) + 1
		g := math.Min(fs, 40)
		k := g / fs
		if d.TurnSpeed != 0 {
			k = d.TurnSpeed / g / fs
		}
		d.YRotA = d.YRotA*0.8 + k*rot
		yaw = wrapDegrees(yaw + d.YRotA*0.1)
		m.SetYaw(yaw)
		mm := 2 / (distSq + 1)
		speed := 0.06 * (l*mm + (1 - mm))
		d.delta = d.delta.Add(mgl64.Vec3{sinDeg(yaw) * speed, 0, -cosDeg(yaw) * speed})
		step := d.delta
		if d.inWall {
			step = step.Mul(0.8)
		}
		m.SetPosition(pos.Add(step))
		hm := 0.875
		if d.delta.Len() > 0 {
			hm = d.delta.Normalize().Dot(face)*0.075 + 0.875
		}
		d.delta[0] *= hm
		d.delta[2] *= hm
		d.delta[1] *= 0.91
	}
	m.SetVelocity(d.delta)
	m.SetBodyYaw(yaw)
	m.SetHeadYaw(yaw)

	d.updateParts(m)
	if !sitting {
		d.knockBack(m, d.Wing1)
		d.knockBack(m, d.Wing2)
		d.bite(m, d.Head.Grow(1))
		d.bite(m, d.Neck.Grow(1))
	}
	d.inWall = d.checkWalls(tx, d.Head) || d.checkWalls(tx, d.Neck) || d.checkWalls(tx, d.Body)
}

// updateParts places the body part boxes from the position, yaw and flight
// history.
func (d *Dragon) updateParts(m *mob.Mob) {
	pos := m.Position()
	yaw := m.Yaw()
	_, y5 := d.latency(5)
	_, y10 := d.latency(10)
	_, y0 := d.latency(0)
	o := mgl64.DegToRad((y5 - y10) * 10)
	p, q := math.Cos(o), math.Sin(o)
	s, t := sinDeg(yaw), cosDeg(yaw)
	hh := m.BBox().Height() / 2
	d.Body = boxAt(mgl64.Vec3{pos[0] + s*0.5, pos[1] + hh, pos[2] - t*0.5}, 5, 3)
	d.Wing1 = boxAt(mgl64.Vec3{pos[0] + t*4.5, pos[1] + 2 + hh, pos[2] + s*4.5}, 4, 3)
	d.Wing2 = boxAt(mgl64.Vec3{pos[0] - t*4.5, pos[1] + 2 + hh, pos[2] - s*4.5}, 4, 3)
	headY := y5 - y0
	if Sitting(m) {
		headY = -1
	}
	u, v := sinDeg(yaw-d.YRotA*0.01), cosDeg(yaw-d.YRotA*0.01)
	d.Head = boxAt(mgl64.Vec3{pos[0] + u*6.5*p, pos[1] + headY + hh + q*6.5, pos[2] - v*6.5*p}, 1, 1)
	d.Neck = boxAt(mgl64.Vec3{pos[0] + u*5.5*p, pos[1] + headY + hh + q*5.5, pos[2] - v*5.5*p}, 3, 3)
	// The tail: segment k follows the flight history 2k+4 ticks back, 2(k+1)
	// behind the body (BDS adds no half height here).
	yaw5, _ := d.latency(5)
	for k := range d.Tail {
		ly, lyY := d.latency(2*k + 4)
		ang := yaw + wrapDegrees(ly-yaw5)
		sm, cm := sinDeg(ang), cosDeg(ang)
		dist := float64(k+1) * 2
		d.Tail[k] = boxAt(mgl64.Vec3{
			pos[0] - (dist*sm+s*1.5)*p,
			lyY - y5 + pos[1] - (dist+1.5)*q + 1.5,
			pos[2] + (dist*cm+t*1.5)*p,
		}, 2, 2)
	}
}

// Dragon parts in the order of the BDS sub-AABBs and of Java's
// EnderDragon.subEntities (the part entity ids follow the dragon's in it).
const (
	PartHead = iota
	PartNeck
	PartBody
	PartTail1
	PartTail2
	PartTail3
	PartWing1
	PartWing2
	partCount
)

// Part returns the hit box of part i (PartHead...PartWing2).
func (d *Dragon) Part(i int) cube.BBox {
	switch i {
	case PartHead:
		return d.Head
	case PartNeck:
		return d.Neck
	case PartBody:
		return d.Body
	case PartTail1, PartTail2, PartTail3:
		return d.Tail[i-PartTail1]
	case PartWing1:
		return d.Wing1
	case PartWing2:
		return d.Wing2
	}
	return cube.BBox{}
}

// rayPart is the part of the dragon the ray from start to end meets first,
// each box grown by grow, or -1.
func (d *Dragon) rayPart(start, end mgl64.Vec3, grow float64) int {
	best, bestD := -1, math.Inf(1)
	for i := range partCount {
		box := d.Part(i)
		if box == (cube.BBox{}) {
			continue
		}
		box = box.Grow(grow)
		if box.Vec3Within(start) {
			return i
		}
		r, ok := trace.BBoxIntercept(box, start, end)
		if !ok {
			continue
		}
		if dd := r.Position().Sub(start).LenSqr(); dd < bestD {
			best, bestD = i, dd
		}
	}
	return best
}

// hitPart is the part a hit struck (Actor::setLastHitBB): a Java client
// names the part it attacked; a projectile's path is clipped against the
// parts grown by 0.3 (ProjectileSystem); a melee hit follows the attacker's
// line of sight (HitDetection::searchActors, parts grown by 0.01). Other
// damage hits no part, which BDS leaves at the last box hit.
func (d *Dragon) hitPart(m *mob.Mob, dmg *mob.Damage) int {
	if part, ok := m.TakeHitPart(); ok && part >= 0 && part < partCount {
		if _, melee := dmg.Source.(entity.AttackDamageSource); melee {
			return part
		}
	}
	switch src := dmg.Source.(type) {
	case entity.ProjectileDamageSource:
		if src.Projectile == nil {
			return -1
		}
		pos := src.Projectile.Position()
		var vel mgl64.Vec3
		if v, ok := src.Projectile.(interface{ Velocity() mgl64.Vec3 }); ok {
			vel = v.Velocity()
		}
		if vel.LenSqr() < 1e-8 {
			// No path: the part the projectile stands in.
			return d.rayPart(pos, pos, 0.3)
		}
		// The projectile hit the dragon's collision box; follow its path on
		// through the body to the first part it meets.
		return d.rayPart(pos.Sub(vel), pos.Add(vel.Normalize().Mul(24)), 0.3)
	case entity.AttackDamageSource:
		if src.Attacker == nil {
			return -1
		}
		r, ok := src.Attacker.(interface{ Rotation() cube.Rotation })
		if !ok {
			return -1
		}
		eye := sense.EyePos(src.Attacker)
		dir := dirOf(r.Rotation()[0], r.Rotation()[1])
		return d.rayPart(eye, eye.Add(dir.Mul(8)), 0.01)
	}
	return -1
}

// knockBack is EnderDragon::_knockBack: players and mobs under a wing are
// pushed away from the body and, while it flies, hurt for 5.
func (d *Dragon) knockBack(m *mob.Mob, wing cube.BBox) {
	box := wing.GrowVec3(mgl64.Vec3{4, 2, 4}).Translate(mgl64.Vec3{0, -2, 0})
	bmn, bmx := d.Body.Min(), d.Body.Max()
	c := bmn.Add(bmx).Mul(0.5)
	tx := m.Tx()
	var hit []world.Entity
	for e := range tx.EntitiesWithin(box) {
		if e.H() == m.H() || !IsLiving(e) || !Living(e) {
			continue
		}
		hit = append(hit, e)
	}
	for _, e := range hit {
		dx, dz := e.Position()[0]-c[0], e.Position()[2]-c[2]
		r := math.Max(dx*dx+dz*dz, 0.1)
		Push(e, mgl64.Vec3{dx / r * 4, 0.2, dz / r * 4})
		if l, ok := e.(entity.Living); ok && !sense.Invulnerable(e) {
			l.Hurt(5, entity.AttackDamageSource{Attacker: m})
		}
	}
}

// bite is EnderDragon::_hurtEntities: 10 damage to what the head and neck
// touch.
func (d *Dragon) bite(m *mob.Mob, box cube.BBox) {
	tx := m.Tx()
	var hit []world.Entity
	for e := range tx.EntitiesWithin(box) {
		if e.H() == m.H() || !IsLiving(e) || !Living(e) || sense.Invulnerable(e) {
			continue
		}
		hit = append(hit, e)
	}
	for _, e := range hit {
		if l, ok := e.(entity.Living); ok {
			l.Hurt(10, entity.AttackDamageSource{Attacker: m})
		}
	}
}

// dragonImmune are the blocks the dragon cannot break
// (EnderDragon::_isDragonImmuneBlock).
var dragonImmune = map[string]bool{
	"minecraft:bedrock": true, "minecraft:obsidian": true, "minecraft:crying_obsidian": true,
	"minecraft:respawn_anchor": true, "minecraft:end_stone": true, "minecraft:end_portal_frame": true,
	"minecraft:end_portal": true, "minecraft:end_gateway": true, "minecraft:iron_bars": true,
	"minecraft:reinforced_deepslate": true, "minecraft:barrier": true, "minecraft:allow": true,
	"minecraft:deny": true, "minecraft:border_block": true, "minecraft:command_block": true,
	"minecraft:repeating_command_block": true, "minecraft:chain_command_block": true,
	"minecraft:structure_block": true, "minecraft:jigsaw": true,
}

// checkWalls is EnderDragon::_checkWalls: blocks in box are broken
// (mobGriefing), immune ones (or all, without mobGriefing) stop the dragon
// (it flies at 80% through them).
func (d *Dragon) checkWalls(tx *world.Tx, box cube.BBox) bool {
	collided, broke := false, false
	griefing := MobGriefing(tx)
	boxBlocks(box, func(p cube.Pos) {
		b := tx.Block(p)
		switch b.(type) {
		case block.Air, world.Liquid:
			return
		}
		name := blockName(b)
		if name == "minecraft:structure_void" || name == "minecraft:light_block" {
			return
		}
		if !griefing || dragonImmune[name] {
			collided = true
			return
		}
		tx.SetBlock(p, nil, nil)
		broke = true
	})
	if broke {
		mn, mx := box.Min(), box.Max()
		tx.AddParticle(mn.Add(mx).Mul(0.5), particle.HugeExplosion{})
	}
	return collided
}

// checkCrystals is EnderDragon::_checkCrystals: the nearest end crystal
// within 32 (looked for with a 1/10 chance a tick) heals 1 every 10 ticks.
// The crystals within 150 are counted once a second; one of them (or the
// healing one) that is gone was destroyed: onCrystalDestroyed, which BDS
// runs from EndDragonFight::onCrystalDestroyed for every crystal.
func (d *Dragon) checkCrystals(m *mob.Mob) {
	tx := m.Tx()
	for h, pos := range d.crystals {
		if _, ok := h.Entity(tx); ok {
			continue
		}
		delete(d.crystals, h)
		healing := h == d.crystal
		if healing {
			d.crystal = nil
		}
		d.crystalDestroyed(m, pos, healing)
	}
	if d.crystal != nil && m.Ticks()%10 == 0 && m.Health() < m.MaxHealth() {
		m.Heal(1, nil)
	}
	if d.crystals == nil {
		d.crystals = map[*world.EntityHandle]mgl64.Vec3{}
	}
	if m.Ticks()%20 == 0 {
		clear(d.crystals)
		n := 0
		for e := range tx.EntitiesWithin(m.BBox().Grow(150)) {
			if e.H().Type() == entity.EndCrystalType {
				n++
				d.crystals[e.H()] = e.Position()
			}
		}
		if d.fight == nil {
			// The fight's dragon counts the crystals on the spikes
			// (EndDragonFight::_updateCrystalCount).
			d.NumCrystals = n
		}
	}
	if m.Rand().IntN(10) == 0 {
		var best world.Entity
		bestD := 1024.0
		for e := range tx.EntitiesWithin(m.BBox().Grow(32)) {
			if e.H().Type() != entity.EndCrystalType {
				continue
			}
			if dd := e.Position().Sub(m.Position()).LenSqr(); dd < bestD {
				best, bestD = e, dd
			}
		}
		d.crystal = nil
		if best != nil {
			d.crystal = best.H()
			d.crystals[best.H()] = best.Position()
		}
	}
}

// crystalDestroyed is EnderDragon::onCrystalDestroyed: the nearest
// attackable player within 64 of the crystal stands for whoever destroyed it
// (Dragonfly's crystals do not tell). The healing crystal hurts the head
// for 10, by that player (or a block explosion when there is none); after
// any crystal a dragon that is not perching takes that player as its target
// and goes after it (on_start_takeoff).
func (d *Dragon) crystalDestroyed(m *mob.Mob, pos mgl64.Vec3, healing bool) {
	tx := m.Tx()
	if d.fight != nil {
		d.fight.updateCrystalCount(tx)
	}
	p, ok := NearestPlayer(tx, pos, 64)
	if healing {
		var src world.ExplosionSource = world.BlockExplosionSource{Pos: cube.PosFromVec3(pos), ExplosionSize: 6}
		if ok {
			src = world.EntityExplosionSource{Entity: p, ExplosionSize: 6}
		}
		d.headHit = true
		m.Hurt(10, entity.ExplosionDamageSource{Source: src})
		d.headHit = false
		if ok {
			m.SetLastHurtBy(p)
		}
	}
	if ok && !Sitting(m) {
		m.SetTarget(p)
		FireTrigger(m, OnStartTakeoff)
	}
}

// CrystalHealing returns the crystal healing the dragon, if any.
func (d *Dragon) CrystalHealing() *world.EntityHandle { return d.crystal }

// modifyDamage is EnderDragon::_hurt(AABB*): arrows bounce off a sitting
// dragon (they burn), a hit on any part but the head and neck deals
// min(d,1)+d/4, and only players (their hits, projectiles and explosions
// credited to them) and block explosions (beds, respawn anchors) hurt it.
// A lethal hit leaves 1 health and starts the death (on_death).
func (d *Dragon) modifyDamage(m *mob.Mob, dmg *mob.Damage) {
	if d.dying || m.Health() <= 0 || m.HasComponent("minecraft:behavior.dragondeath") {
		if _, void := dmg.Source.(entity.VoidDamageSource); !void {
			dmg.Cancel = true
			return
		}
	}
	if _, void := dmg.Source.(entity.VoidDamageSource); void {
		return
	}
	sitting := Sitting(m)
	if p, ok := dmg.Source.(entity.ProjectileDamageSource); ok && sitting && p.Projectile != nil {
		switch p.Projectile.H().Type().EncodeEntity() {
		case "minecraft:arrow", "minecraft:thrown_trident", "minecraft:wind_charge_projectile", "minecraft:breeze_wind_charge_projectile":
			if f, ok := p.Projectile.(interface{ SetOnFire(time.Duration) }); ok {
				f.SetOnFire(5 * time.Second)
			}
			dmg.Cancel = true
			return
		}
	}
	if !dragonHurtBy(dmg) {
		dmg.Cancel = true
		return
	}
	if !d.headHit {
		if part := d.hitPart(m, dmg); part != PartHead && part != PartNeck {
			dmg.Amount = math.Min(dmg.Amount, 1) + dmg.Amount*0.25
		}
	}
	if dmg.Amount < 0.01 {
		dmg.Cancel = true
		return
	}
	if dmg.Amount >= m.Health() {
		dmg.Amount = math.Max(m.Health()-1, 0)
		d.pendingDeath = true
	}
}

// dragonHurtBy is the source check of EnderDragon::_hurt: the damage
// source's entity is a player (a projectile's or explosion's owner counts),
// or the cause is a block explosion.
func dragonHurtBy(dmg *mob.Damage) bool {
	if dmg.Attacker != nil && sense.IsPlayer(dmg.Attacker) {
		return true
	}
	if ex, ok := dmg.Source.(entity.ExplosionDamageSource); ok {
		switch src := ex.Source.(type) {
		case world.EntityExplosionSource:
			return src.Entity != nil && sense.IsPlayer(src.Entity)
		case world.Entity:
			return sense.IsPlayer(src)
		case world.BlockExplosionSource, *world.BlockExplosionSource:
			return true
		}
	}
	return false
}

// onHurt counts the damage a sitting dragon takes: past a quarter of its
// health it takes off.
func (d *Dragon) onHurt(m *mob.Mob, dealt float64, _ world.DamageSource) {
	if !Sitting(m) {
		return
	}
	if d.sittingDamage += dealt; d.sittingDamage > m.MaxHealth()*SittingAllowedDamage {
		d.sittingDamage = 0
		d.pendingTakeof = true
	}
}

// StartDeathAnimation is the end of DragonDeathGoal: health 0, the death
// animation actor event, then 200 ticks of rising and explosions
// (tickDeath) before the dragon is removed.
func (d *Dragon) StartDeathAnimation(m *mob.Mob) {
	if d.dying {
		return
	}
	d.dying, d.deathTime = true, 0
	// The first dragon of the End fight drops 12000 experience, later ones
	// 500 (EnderDragon::tickDeath, the flag the fight set); a dragon outside
	// the fight always 12000.
	d.xp = 12000
	if d.prevKilled {
		d.xp = 500
	}
	if b, ok := BossOf(m); ok {
		b.SetHealthOverride(0)
	}
	m.SetHealth(0)
	ShowActorEvent(m.Tx(), m, packet.ActorEventDragonStartDeathAnim)
}

// tickDeath is EnderDragon::tickDeath: rising 0.1 a tick, explosions from
// tick 180, experience in bursts of 8% every 5 ticks after 150 and the last
// 20% at 200, when the dragon is removed.
func (d *Dragon) tickDeath(m *mob.Mob) {
	tx := m.Tx()
	d.deathTime++
	r := m.Rand()
	pos := m.Position()
	off := mgl64.Vec3{(r.Float64() - 0.5) * 8, (r.Float64()-0.5)*4 + 2, (r.Float64() - 0.5) * 8}
	if d.deathTime%2 == 0 {
		tx.AddParticle(pos.Add(off), particle.Named{Bedrock: "minecraft:dragon_dying_explosion", Java: "explosion"})
	}
	if d.deathTime >= 180 && d.deathTime <= 200 {
		tx.AddParticle(pos.Add(off), particle.HugeExplosion{})
	}
	if d.deathTime == 2 {
		m.PlaySound(mob.SoundDeath)
	}
	if d.deathTime > 150 && d.deathTime%5 == 0 {
		spawnXP(tx, pos, int(float64(d.xp)*0.08))
	}
	m.SetVelocity(mgl64.Vec3{})
	m.SetPosition(pos.Add(mgl64.Vec3{0, 0.1, 0}))
	if d.deathTime >= 200 {
		spawnXP(tx, pos, int(float64(d.xp)*0.2))
		d.dragonDied(m)
		_ = m.Close()
	}
}

func spawnXP(tx *world.Tx, pos mgl64.Vec3, amount int) {
	for _, o := range entity.NewExperienceOrbs(pos, amount) {
		tx.AddEntity(o)
	}
}

func (d *Dragon) encode() map[string]any {
	return map[string]any{"FlameCount": int32(d.FlameCount), "SittingDamage": float32(d.sittingDamage)}
}

func (d *Dragon) decode(data map[string]any) {
	if v, ok := data["FlameCount"].(int32); ok {
		d.FlameCount = int(v)
	}
	if v, ok := data["SittingDamage"].(float32); ok {
		d.sittingDamage = float64(v)
	}
}
