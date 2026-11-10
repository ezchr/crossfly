//go:build crossfly_full

package jnether

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob/component/combat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

// The ender dragon of Java mob mode (EnderDragon, Paper 26.3): no physics,
// no goals; its phase manager (EnderDragonPhaseManager, dragon_phase.go)
// picks a point to fly to and aiStep steers there. Eight body parts follow
// it from its flight history; the wings knock back and the head and neck
// bite whatever they touch, and the head, neck and body break every block
// that is not dragon_immune (mobGriefing). The End fight
// (EnderDragonFight) tells it how many crystals still stand on the spikes.

// DragonHost is what the dragon needs of its mob: the Java mob base
// (*javamob.Mob) has all of it.
type DragonHost interface {
	world.Entity
	Tx() *world.Tx
	Velocity() mgl64.Vec3
	SetVelocity(v mgl64.Vec3)
	// SetPosition moves the mob without collisions (noPhysics).
	SetPosition(pos mgl64.Vec3)
	Yaw() float64
	SetYaw(v float64)
	SetBodyYaw(v float64)
	Pitch() float64
	Health() float64
	MaxHealth() float64
	SetHealth(v float64)
	Rand() *rand.Rand
	TickCount() int
	// HurtTime is LivingEntity.hurtTime (10 after a hit, counting down).
	HurtTime() int
}

// DragonFight is the End fight as the dragon sees it (EnderDragonFight).
type DragonFight interface {
	// AliveCrystals is the crystals standing on the spikes (updated every
	// 100 ticks).
	AliveCrystals() int
	// PreviouslyKilled reports whether a dragon died in this world before.
	PreviouslyKilled() bool
	// DragonKilled is called when the death animation ends.
	DragonKilled(tx *world.Tx, dragon world.Entity)
	// UpdateDragon is called every tick (EnderDragonFight.updateDragon: the
	// boss bar).
	UpdateDragon(tx *world.Tx, dragon world.Entity, health, maxHealth float64)
}

// Dragon parts (EnderDragon.subEntities order: the Java client numbers the
// part entities after the dragon's id in this order).
const (
	PartHead = iota
	PartNeck
	PartBody
	PartTail1
	PartTail2
	PartTail3
	PartWing1
	PartWing2
	dragonParts
)

// partSize is the width and height of each part (EnderDragonPart).
var partSize = [dragonParts][2]float64{{1, 1}, {3, 3}, {5, 3}, {2, 2}, {2, 2}, {2, 2}, {4, 2}, {4, 2}}

// Dragon is the state of one Java ender dragon (EnderDragon's fields).
type Dragon struct {
	Fight DragonFight

	history flightHistory
	parts   [dragonParts]mgl64.Vec3
	inWall  bool
	yRotA   float64

	deathTime     int
	sittingDamage float64
	expToDrop     int

	nodes  [24]*dragonNode
	phases phaseManager

	// nearestCrystal is the crystal healing the dragon.
	nearestCrystal *world.EntityHandle
	// crystals are the crystals seen near the dragon, to notice one being
	// destroyed (EnderDragonFight.onCrystalDestroyed).
	crystals map[*world.EntityHandle]mgl64.Vec3
	// lastBitten is when the wings last hurt an entity (the
	// lastHurtByMobTimestamp check of knockBack).
	lastBitten map[*world.EntityHandle]int
	// hitPart is the part the hit being dealt struck, -1 if unknown.
	hitPart int
}

// NewDragon returns a dragon in the HOVERING phase (EnderDragonPhaseManager's
// constructor), as Java creates it; the fight sets HOLDING_PATTERN.
func NewDragon() *Dragon {
	d := &Dragon{hitPart: -1, expToDrop: -1}
	d.history.init()
	d.phases.init(d)
	return d
}

// Phase returns the current phase id (EnderDragon.DATA_PHASE), which the
// Java client animates from.
func (d *Dragon) Phase() int { return d.phases.cur.id() }

// SetPhase sets the phase (EnderDragonPhaseManager.setPhase).
func (d *Dragon) SetPhase(h DragonHost, id int) { d.phases.set(h, id) }

// Sitting reports whether the current phase is a sitting one (the dragon
// perches on the podium; hovering counts too).
func (d *Dragon) Sitting() bool { return d.phases.cur.sitting() }

// DeathTime is EnderDragon.dragonDeathTime.
func (d *Dragon) DeathTime() int { return d.deathTime }

// Part returns the box of part i.
func (d *Dragon) Part(i int) cube.BBox {
	p, s := d.parts[i], partSize[i]
	return cube.Box(p[0]-s[0]/2, p[1], p[2]-s[0]/2, p[0]+s[0]/2, p[1]+s[1], p[2]+s[0]/2)
}

// aliveCrystals is the fight's crystal count; without a fight -1 (no fight
// and none alive behave alike except in the holding pattern's ring choice).
func (d *Dragon) aliveCrystals() int {
	if d.Fight == nil {
		return 0
	}
	return d.Fight.AliveCrystals()
}

// podium is EnderDragon.getPodium: the fight origin, 0 0 0.
func (d *Dragon) podium() cube.Pos { return cube.Pos{} }

// podiumTop is Level.getHeightmapPos(MOTION_BLOCKING_NO_LEAVES, podium).
func (d *Dragon) podiumTop(tx *world.Tx) cube.Pos {
	p := d.podium()
	return cube.Pos{p[0], heightNoLeaves(tx, p[0], p[2]), p[2]}
}

// AiStep is EnderDragon.aiStep (server side).
func (d *Dragon) AiStep(h DragonHost) {
	if h.Health() <= 0 {
		return
	}
	tx := h.Tx()
	d.checkCrystals(h)
	h.SetYaw(wrapDeg(h.Yaw()))
	pos := h.Position()
	d.history.record(pos[1], h.Yaw())

	cur := d.phases.cur
	cur.serverTick(h)
	if d.phases.cur != cur {
		cur = d.phases.cur
		cur.serverTick(h)
	}
	if target, ok := cur.flyTarget(); ok && cur.id() != PhaseHovering {
		d.steer(h, cur, target)
	}
	h.SetBodyYaw(h.Yaw())

	pos = h.Position()
	tilt := (d.history.get(5).y - d.history.get(10).y) * 10 * math.Pi / 180
	ccTilt, ssTilt := math.Cos(tilt), math.Sin(tilt)
	rot1 := h.Yaw() * math.Pi / 180
	ss1, cc1 := math.Sin(rot1), math.Cos(rot1)
	d.parts[PartBody] = pos.Add(mgl64.Vec3{ss1 * 0.5, 0, -cc1 * 0.5})
	d.parts[PartWing1] = pos.Add(mgl64.Vec3{cc1 * 4.5, 2, ss1 * 4.5})
	d.parts[PartWing2] = pos.Add(mgl64.Vec3{cc1 * -4.5, 2, ss1 * -4.5})
	if h.HurtTime() <= 0 {
		d.knockBack(h, d.Part(PartWing1).GrowVec3(mgl64.Vec3{4, 2, 4}).Translate(mgl64.Vec3{0, -2, 0}))
		d.knockBack(h, d.Part(PartWing2).GrowVec3(mgl64.Vec3{4, 2, 4}).Translate(mgl64.Vec3{0, -2, 0}))
		d.bite(h, d.Part(PartHead).Grow(1))
		d.bite(h, d.Part(PartNeck).Grow(1))
	}
	a := h.Yaw()*math.Pi/180 - d.yRotA*0.01
	ss2, cc2 := math.Sin(a), math.Cos(a)
	yOff := d.headYOffset()
	d.parts[PartHead] = pos.Add(mgl64.Vec3{ss2 * 6.5 * ccTilt, yOff + ssTilt*6.5, -cc2 * 6.5 * ccTilt})
	d.parts[PartNeck] = pos.Add(mgl64.Vec3{ss2 * 5.5 * ccTilt, yOff + ssTilt*5.5, -cc2 * 5.5 * ccTilt})
	p1 := d.history.get(5)
	for i := range 3 {
		p0 := d.history.get(12 + i*2)
		rot := h.Yaw()*math.Pi/180 + wrapDeg(p0.yRot-p1.yRot)*math.Pi/180
		ss, cc := math.Sin(rot), math.Cos(rot)
		dd := float64(i+1) * 2
		d.parts[PartTail1+i] = pos.Add(mgl64.Vec3{
			-(ss1*1.5 + ss*dd) * ccTilt,
			p0.y - p1.y - (dd+1.5)*ssTilt + 1.5,
			(cc1*1.5 + cc*dd) * ccTilt,
		})
	}
	d.inWall = d.checkWalls(h, d.Part(PartHead))
	d.inWall = d.checkWalls(h, d.Part(PartNeck)) || d.inWall
	d.inWall = d.checkWalls(h, d.Part(PartBody)) || d.inWall
	if d.Fight != nil {
		d.Fight.UpdateDragon(tx, h, h.Health(), h.MaxHealth())
	}
}

// steer is the flying part of aiStep: climb towards the target (capped by
// the phase's fly speed), turn (the phase's turn speed, at most 50 degrees
// at a time), push forward 0.06 scaled by how well the dragon faces the
// target, move (0.8 of it inside blocks) and slow down by 0.8-0.95
// sideways (more when moving the way it faces) and 0.91 vertically.
func (d *Dragon) steer(h DragonHost, cur dragonPhase, target mgl64.Vec3) {
	pos := h.Position()
	xd, yd, zd := target[0]-pos[0], target[1]-pos[1], target[2]-pos[2]
	dist := xd*xd + yd*yd + zd*zd
	maxSpeed := cur.flySpeed()
	if hd := math.Sqrt(xd*xd + zd*zd); hd > 0 {
		yd = clamp(yd/hd, -maxSpeed, maxSpeed)
	}
	vel := h.Velocity().Add(mgl64.Vec3{0, yd * 0.01, 0})
	h.SetYaw(wrapDeg(h.Yaw()))
	aim := normalize(target.Sub(pos))
	yawRad := h.Yaw() * math.Pi / 180
	dir := normalize(mgl64.Vec3{math.Sin(yawRad), vel[1], -math.Cos(yawRad)})
	dot := math.Max((dir.Dot(aim)+0.5)/1.5, 0)
	if math.Abs(xd) > 1e-5 || math.Abs(zd) > 1e-5 {
		yRotD := clamp(wrapDeg(180-math.Atan2(xd, zd)*180/math.Pi-h.Yaw()), -50, 50)
		d.yRotA *= 0.8
		d.yRotA += yRotD * cur.turnSpeed(vel)
		h.SetYaw(h.Yaw() + d.yRotA*0.1)
	}
	span := 2 / (dist + 1)
	// moveRelative(speed, (0, 0, -1)).
	speed := 0.06 * (dot*span + (1 - span))
	yawRad = h.Yaw() * math.Pi / 180
	vel = vel.Add(mgl64.Vec3{speed * math.Sin(yawRad), 0, -speed * math.Cos(yawRad)})
	step := vel
	if d.inWall {
		step = vel.Mul(0.8)
	}
	h.SetPosition(pos.Add(step))
	actual := normalize(vel)
	slide := 0.8 + 0.15*(actual.Dot(dir)+1)/2
	h.SetVelocity(mgl64.Vec3{vel[0] * slide, vel[1] * 0.91, vel[2] * slide})
}

// headYOffset is getHeadYOffset: -1 while sitting, else the height change
// over the last 5 ticks.
func (d *Dragon) headYOffset() float64 {
	if d.phases.cur.sitting() {
		return -1
	}
	return d.history.get(5).y - d.history.get(0).y
}

// HeadPos is the middle of the head part.
func (d *Dragon) HeadPos() mgl64.Vec3 {
	return d.parts[PartHead].Add(mgl64.Vec3{0, partSize[PartHead][1] / 2, 0})
}

// targetable is EntitySelector.NO_CREATIVE_OR_SPECTATOR (+ alive).
func targetable(e world.Entity) bool {
	if l, ok := e.(entity.Living); ok && l.Dead() {
		return false
	}
	if g, ok := e.(interface{ GameMode() world.GameMode }); ok {
		gm := g.GameMode()
		return gm.AllowsTakingDamage() && gm.Visible()
	}
	return true
}

// entitiesIn yields the living entities whose box meets box, the dragon
// left out.
func entitiesIn(h DragonHost, box cube.BBox) []world.Entity {
	var out []world.Entity
	for e := range h.Tx().EntitiesWithin(box.Grow(3)) {
		if e.H() == h.H() {
			continue
		}
		if _, ok := e.(entity.Living); !ok || !targetable(e) {
			continue
		}
		if e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
			out = append(out, e)
		}
	}
	return out
}

// knockBack is EnderDragon.knockBack: the wings push living entities away
// from the body (4 / distance², 0.2 up) and, unless the dragon sits, hurt
// them for 5 (at most every 3 ticks per entity).
func (d *Dragon) knockBack(h DragonHost, box cube.BBox) {
	body := d.Part(PartBody)
	xm, zm := (body.Min()[0]+body.Max()[0])/2, (body.Min()[2]+body.Max()[2])/2
	for _, e := range entitiesIn(h, box) {
		p := e.Position()
		xd, zd := p[0]-xm, p[2]-zm
		dd := math.Max(xd*xd+zd*zd, 0.1)
		push(e, mgl64.Vec3{xd / dd * 4, 0.2, zd / dd * 4})
		if d.phases.cur.sitting() {
			continue
		}
		if d.lastBitten == nil {
			d.lastBitten = map[*world.EntityHandle]int{}
		}
		if last, ok := d.lastBitten[e.H()]; ok && h.TickCount()-last <= 2 {
			continue
		}
		d.lastBitten[e.H()] = h.TickCount()
		hurt(e, 5, entity.AttackDamageSource{Attacker: h})
	}
}

// bite is EnderDragon.hurt(level, entities): 10 to every living entity the
// head or neck (grown by 1) touches.
func (d *Dragon) bite(h DragonHost, box cube.BBox) {
	for _, e := range entitiesIn(h, box) {
		hurt(e, 10, entity.AttackDamageSource{Attacker: h})
	}
}

// push is Entity.push for a mob or a player.
func push(e world.Entity, v mgl64.Vec3) {
	if p, ok := e.(interface {
		Velocity() mgl64.Vec3
		SetVelocity(mgl64.Vec3)
	}); ok {
		p.SetVelocity(p.Velocity().Add(v))
	}
}

// dragonImmune is BlockTags.DRAGON_IMMUNE (Bedrock names).
var dragonImmune = map[string]bool{
	"barrier": true, "bedrock": true, "end_portal": true, "end_portal_frame": true, "end_gateway": true,
	"command_block": true, "repeating_command_block": true, "chain_command_block": true, "structure_block": true,
	"jigsaw": true, "moving_block": true, "obsidian": true, "crying_obsidian": true, "end_stone": true,
	"iron_bars": true, "respawn_anchor": true, "reinforced_deepslate": true,
}

// dragonTransparent is BlockTags.DRAGON_TRANSPARENT: light and fire.
var dragonTransparent = map[string]bool{"light_block": true, "fire": true, "soul_fire": true}

// checkWalls is EnderDragon.checkWalls: every block in box that is not air
// or dragon_transparent is removed (no drops: Paper's explode event yield is
// 0) when mobGriefing allows and it is not dragon_immune; otherwise it is a
// wall. It reports whether a wall was met.
func (d *Dragon) checkWalls(h DragonHost, box cube.BBox) bool {
	tx := h.Tx()
	mn, mx := box.Min(), box.Max()
	x0, y0, z0 := int(math.Floor(mn[0])), int(math.Floor(mn[1])), int(math.Floor(mn[2]))
	x1, y1, z1 := int(math.Floor(mx[0])), int(math.Floor(mx[1])), int(math.Floor(mx[2]))
	griefing := combat.MobGriefing(tx)
	wall, destroyed := false, false
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				pos := cube.Pos{x, y, z}
				if pos.OutOfBounds(tx.Range()) {
					continue
				}
				name := blockID(tx.Block(pos))
				if name == "air" || dragonTransparent[name] {
					continue
				}
				if griefing && !dragonImmune[name] {
					tx.SetBlock(pos, nil, nil)
					destroyed = true
				} else {
					wall = true
				}
			}
		}
	}
	if destroyed {
		r := h.Rand()
		p := cube.Pos{x0 + r.IntN(x1-x0+1), y0 + r.IntN(y1-y0+1), z0 + r.IntN(z1-z0+1)}
		tx.AddParticle(p.Vec3Centre(), particle.Named{Bedrock: "minecraft:large_explosion", Java: "explosion"})
	}
	return wall
}

// blockID is a block's identifier without the namespace.
func blockID(b world.Block) string {
	n, _ := b.EncodeBlock()
	if len(n) > 10 && n[:10] == "minecraft:" {
		return n[10:]
	}
	return n
}

// checkCrystals is EnderDragon.checkCrystals: the nearest crystal within
// 32 of the dragon's box (looked for on 1 tick in 10) heals it 1 every 10
// ticks. It also notices crystals near the dragon being destroyed
// (EnderDragonFight.onCrystalDestroyed calls onCrystalDestroyed).
func (d *Dragon) checkCrystals(h DragonHost) {
	tx := h.Tx()
	for c, pos := range d.crystals {
		if _, ok := c.Entity(tx); ok {
			continue
		}
		delete(d.crystals, c)
		d.onCrystalDestroyed(h, c, pos)
	}
	if d.nearestCrystal != nil {
		if _, ok := d.nearestCrystal.Entity(tx); !ok {
			d.nearestCrystal = nil
		} else if h.TickCount()%10 == 0 && h.Health() < h.MaxHealth() {
			h.SetHealth(math.Min(h.MaxHealth(), h.Health()+1))
		}
	}
	box := h.H().Type().BBox(h).Translate(h.Position())
	if h.TickCount()%20 == 0 {
		if d.crystals == nil {
			d.crystals = map[*world.EntityHandle]mgl64.Vec3{}
		}
		for e := range tx.EntitiesWithin(box.Grow(160)) {
			if e.H().Type() == entity.EndCrystalType {
				d.crystals[e.H()] = e.Position()
			}
		}
	}
	if h.Rand().IntN(10) == 0 {
		var nearest world.Entity
		best := math.MaxFloat64
		for e := range tx.EntitiesWithin(box.Grow(33)) {
			if e.H().Type() != entity.EndCrystalType {
				continue
			}
			eb := e.H().Type().BBox(e).Translate(e.Position())
			if !eb.IntersectsWith(box.Grow(32)) {
				continue
			}
			if dd := e.Position().Sub(h.Position()).LenSqr(); dd < best {
				best, nearest = dd, e
			}
		}
		d.nearestCrystal = nil
		if nearest != nil {
			d.nearestCrystal = nearest.H()
			if d.crystals == nil {
				d.crystals = map[*world.EntityHandle]mgl64.Vec3{}
			}
			d.crystals[nearest.H()] = nearest.Position()
		}
	}
}

// NearestCrystal is the crystal healing the dragon (clients draw the beam).
func (d *Dragon) NearestCrystal() *world.EntityHandle { return d.nearestCrystal }

// onCrystalDestroyed is EnderDragon.onCrystalDestroyed: the player who did
// it (Dragonfly's crystals do not tell: the nearest attackable player within
// 64 of it, CRYSTAL_DESTROY_TARGETING) is the source; the healing crystal
// hurts the head for 10 (an explosion); the phase hears of it (the holding
// pattern goes after the player).
func (d *Dragon) onCrystalDestroyed(h DragonHost, c *world.EntityHandle, pos mgl64.Vec3) {
	tx := h.Tx()
	player := nearestPlayer(tx, pos, 64, false)
	if c == d.nearestCrystal {
		d.nearestCrystal = nil
		var src world.ExplosionSource = world.BlockExplosionSource{Pos: cube.PosFromVec3(pos), ExplosionSize: 6}
		if player != nil {
			src = world.EntityExplosionSource{Entity: player, ExplosionSize: 6}
		}
		d.hitPart = PartHead
		hurt(h, 10, entity.ExplosionDamageSource{Source: src})
		d.hitPart = -1
	}
	d.phases.cur.crystalDestroyed(h, player)
}

// nearestPlayer is Level.getNearestPlayer with TargetingConditions
// forCombat: alive, attackable players (no creative or spectator) within r
// of pos (any distance when r <= 0); maxY, when set, limits the height
// difference.
func nearestPlayer(tx *world.Tx, pos mgl64.Vec3, r float64, ignoreRange bool) world.Entity {
	var best world.Entity
	bestD := math.MaxFloat64
	for p := range tx.Players() {
		if !targetable(p) {
			continue
		}
		dd := p.Position().Sub(pos).LenSqr()
		if !ignoreRange && r > 0 && dd > r*r {
			continue
		}
		if dd < bestD {
			best, bestD = p, dd
		}
	}
	return best
}

// flightHistory is DragonFlightHistory: the last 64 heights and yaws.
type flightHistory struct {
	samples [64]flightSample
	head    int
}

type flightSample struct{ y, yRot float64 }

func (f *flightHistory) init() { f.head = -1 }

func (f *flightHistory) record(y, yRot float64) {
	s := flightSample{y, yRot}
	if f.head < 0 {
		for i := range f.samples {
			f.samples[i] = s
		}
	}
	if f.head++; f.head == 64 {
		f.head = 0
	}
	f.samples[f.head] = s
}

func (f *flightHistory) get(delay int) flightSample { return f.samples[(f.head-delay)&63] }

// wrapDeg is Mth.wrapDegrees.
func wrapDeg(a float64) float64 {
	a = math.Mod(a, 360)
	if a >= 180 {
		a -= 360
	}
	if a < -180 {
		a += 360
	}
	return a
}
