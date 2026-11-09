//go:build crossfly_full

package entity

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// minecartBehaviour is Java Edition 26.3's AbstractMinecart with OldMinecartBehavior, ported tick for tick.
type minecartBehaviour struct {
	kind MinecartKind

	rider                 *world.EntityHandle
	moveLeft, moveForward float64
	moveYaw               float32

	hurtTime, hurtDir int
	damage            float32

	yRot                                    float32
	flipped, onRails, onGround, inWater     bool
	horizontalCollision                     bool
	fallDistance                            float64
	removed                                 bool
	tickStart                               mgl64.Vec3
	tickedAt                                int64
	fuse                                    int
	hopperEnabled                           bool
	inv                                     *inventory.Inventory
	lastSentHurt, lastSentDir, lastSentDmgI int
}

func newMinecartBehaviour(kind MinecartKind) *minecartBehaviour {
	b := &minecartBehaviour{kind: kind, hurtDir: 1, fuse: -1, hopperEnabled: true, tickedAt: -1}
	b.lastSentDir = 1
	switch kind {
	case MinecartChest:
		b.inv = inventory.New(27, nil)
	case MinecartHopper:
		b.inv = inventory.New(5, nil)
	}
	return b
}

// MinecartWobble returns the hurt time, hurt direction and damage of the minecart.
func (b *minecartBehaviour) MinecartWobble() (hurtTime, hurtDir int, damage float32) {
	return b.hurtTime, b.hurtDir, b.damage
}

// TNTFuse returns the fuse of a TNT minecart and whether it is primed.
func (b *minecartBehaviour) TNTFuse() (int, bool) {
	return b.fuse, b.kind == MinecartTNT && b.fuse >= 0
}

// MinecartPrimeAction is shown when a TNT minecart is primed: Java Edition's entity event 70.
type MinecartPrimeAction struct{ action }

// TNTMinecartPrimed marks the action for viewers, which do not know the type (it is part of Crossfly Full only).
func (MinecartPrimeAction) TNTMinecartPrimed() {}

// minecartRailKind is the kind of rail a minecart is on.
type minecartRailKind uint8

const (
	railPlain minecartRailKind = iota
	railPowered
	railActivator
	railDetector
)

// minecartRail is the rail at a position, as a minecart reads it.
type minecartRail struct {
	shape   block.RailShape
	kind    minecartRailKind
	powered bool
}

// minecartRailAt returns the rail at pos (Java Edition's BaseRailBlock.isRail), if any.
func minecartRailAt(tx *world.Tx, pos cube.Pos) (minecartRail, bool) {
	if pos.OutOfBounds(tx.Range()) {
		return minecartRail{}, false
	}
	switch r := tx.Block(pos).(type) {
	case block.Rail:
		return minecartRail{shape: r.Shape}, true
	case block.PoweredRail:
		return minecartRail{shape: r.Shape, kind: railPowered, powered: r.Powered}, true
	case block.ActivatorRail:
		return minecartRail{shape: r.Shape, kind: railActivator, powered: r.Powered}, true
	case block.DetectorRail:
		return minecartRail{shape: r.Shape, kind: railDetector, powered: r.Powered}, true
	}
	return minecartRail{}, false
}

// railShapeAt reports whether a rail is at pos.
func railShapeAt(tx *world.Tx, pos cube.Pos) (block.RailShape, bool) {
	r, ok := minecartRailAt(tx, pos)
	return r.shape, ok
}

// minecartExits is AbstractMinecart.EXITS: the two ends of every rail shape.
var minecartExits = [10][2][3]int{
	{{0, 0, -1}, {0, 0, 1}},  // north_south
	{{-1, 0, 0}, {1, 0, 0}},  // east_west
	{{-1, -1, 0}, {1, 0, 0}}, // ascending_east
	{{-1, 0, 0}, {1, -1, 0}}, // ascending_west
	{{0, 0, -1}, {0, -1, 1}}, // ascending_north
	{{0, -1, -1}, {0, 0, 1}}, // ascending_south
	{{0, 0, 1}, {1, 0, 0}},   // south_east
	{{0, 0, 1}, {-1, 0, 0}},  // south_west
	{{0, 0, -1}, {-1, 0, 0}}, // north_west
	{{0, 0, -1}, {1, 0, 0}},  // north_east
}

// floorPos is Mth.floor of a position.
func floorPos(v mgl64.Vec3) cube.Pos {
	return cube.Pos{int(math.Floor(v[0])), int(math.Floor(v[1])), int(math.Floor(v[2]))}
}

// currentBlockPosOrRailBelow is AbstractMinecart.getCurrentBlockPosOrRailBelow.
func currentBlockPosOrRailBelow(tx *world.Tx, v mgl64.Vec3) cube.Pos {
	pos := floorPos(v)
	if _, ok := railShapeAt(tx, pos.Side(cube.FaceDown)); ok {
		pos[1]--
	}
	return pos
}

// minecartRailPos is OldMinecartBehavior.getPos: the point on the rail under x, y, z.
func minecartRailPos(tx *world.Tx, x, y, z float64) (mgl64.Vec3, bool) {
	pos := currentBlockPosOrRailBelow(tx, mgl64.Vec3{x, y, z})
	shape, ok := railShapeAt(tx, pos)
	if !ok {
		return mgl64.Vec3{}, false
	}
	e0, e1 := minecartExits[shape][0], minecartExits[shape][1]
	xt, yt, zt := float64(pos[0]), float64(pos[1]), float64(pos[2])
	x0, y0, z0 := xt+0.5+float64(e0[0])*0.5, yt+0.0625+float64(e0[1])*0.5, zt+0.5+float64(e0[2])*0.5
	x1, y1, z1 := xt+0.5+float64(e1[0])*0.5, yt+0.0625+float64(e1[1])*0.5, zt+0.5+float64(e1[2])*0.5
	xD, yD, zD := x1-x0, (y1-y0)*2, z1-z0
	var progress float64
	switch {
	case xD == 0:
		progress = z - zt
	case zD == 0:
		progress = x - xt
	default:
		progress = ((x-x0)*xD + (z-z0)*zD) * 2
	}
	x, y, z = x0+xD*progress, y0+yD*progress, z0+zD*progress
	if yD < 0 {
		y++
	} else if yD > 0 {
		y += 0.5
	}
	return mgl64.Vec3{x, y, z}, true
}

func (b *minecartBehaviour) gravity() float64 {
	if b.inWater {
		return 0.005
	}
	return 0.04
}

func (b *minecartBehaviour) maxSpeed() float64 {
	if b.inWater {
		return 0.2
	}
	return 0.4
}

// Tick is AbstractMinecart.tick with OldMinecartBehavior.tick, and the tick of the minecart kinds after it.
func (b *minecartBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if b.removed {
		return nil
	}
	b.checkRider(e, tx)
	start, startVel, startRot := e.data.Pos, e.data.Vel, e.data.Rot
	b.tickStart, b.tickedAt = start, tx.CurrentTick()
	yRotO := b.yRot

	if b.hurtTime > 0 {
		b.hurtTime--
	}
	if b.damage > 0 {
		b.damage -= 1
	}

	e.data.Vel[1] -= b.gravity()
	pos := currentBlockPosOrRailBelow(tx, e.data.Pos)
	if rail, ok := minecartRailAt(tx, pos); ok {
		b.onRails = true
		b.moveAlongTrack(e, tx, pos, rail)
		if rail.kind == railActivator && !b.removed {
			b.activate(e, tx, rail.powered)
		}
	} else {
		b.onRails = false
		b.comeOffTrack(e, tx)
	}
	if b.removed {
		return nil
	}
	b.insideBlocks(e, tx)

	yRot := b.yRot
	dx, dz := start[0]-e.data.Pos[0], start[2]-e.data.Pos[2]
	if dx*dx+dz*dz > 0.001 {
		yRot = float32(mthAtan2(dz, dx) * 180 / math.Pi)
		if b.flipped {
			yRot += 180
		}
	}
	if rd := mthWrapDegrees(yRot - yRotO); rd < -170 || rd >= 170 {
		yRot += 180
		b.flipped = !b.flipped
	}
	b.yRot = float32(math.Mod(float64(yRot), 360))
	e.data.Rot = cube.Rotation{float64(b.yRot), 0}

	b.pushAndPickupEntities(e, tx)
	b.pushedByPlayers(e, tx)
	b.inWater = minecartInWater(tx, e.data.Pos)

	switch b.kind {
	case MinecartTNT:
		if b.fuse > 0 {
			b.fuse--
		} else if b.fuse == 0 {
			b.explode(e, tx, horizontalLenSqr(e.data.Vel))
		}
		if !b.removed && b.horizontalCollision {
			if s := horizontalLenSqr(e.data.Vel); s >= float64(float32(0.01)) {
				b.explode(e, tx, s)
			}
		}
		if b.removed {
			return nil
		}
	case MinecartHopper:
		if b.hopperEnabled {
			b.suckInItems(e, tx)
		}
	}
	b.positionRider(e, tx)
	b.send(e, tx, start, startVel, startRot)
	return nil
}

// send shows viewers the minecart's movement and state. Unlike Movement.Send, any change is sent: a minecart may move
// by less than 0.001 a tick.
func (b *minecartBehaviour) send(e *Ent, tx *world.Tx, start, startVel mgl64.Vec3, startRot cube.Rotation) {
	posChanged := e.data.Pos != start || e.data.Rot != startRot
	velChanged := e.data.Vel != startVel
	dmg := int(b.damage)
	stateChanged := b.hurtTime != b.lastSentHurt || b.hurtDir != b.lastSentDir || dmg != b.lastSentDmgI
	b.lastSentHurt, b.lastSentDir, b.lastSentDmgI = b.hurtTime, b.hurtDir, dmg
	if !posChanged && !velChanged && !stateChanged {
		return
	}
	for _, v := range tx.Viewers(e.data.Pos) {
		if posChanged {
			v.ViewEntityMovement(e, e.data.Pos, e.data.Rot, b.onGround)
		}
		if velChanged {
			v.ViewEntityVelocity(e, e.data.Vel)
		}
		if stateChanged {
			v.ViewEntityState(e)
		}
	}
}

// moveAlongTrack is OldMinecartBehavior.moveAlongTrack.
func (b *minecartBehaviour) moveAlongTrack(e *Ent, tx *world.Tx, pos cube.Pos, rail minecartRail) {
	b.fallDistance = 0
	x, y, z := e.data.Pos[0], e.data.Pos[1], e.data.Pos[2]
	oldPos, oldOk := minecartRailPos(tx, x, y, z)
	y = float64(pos[1])
	powerTrack, haltTrack := false, false
	if rail.kind == railPowered {
		powerTrack, haltTrack = rail.powered, !rail.powered
	}
	slide := 0.0078125
	if b.inWater {
		slide *= 0.2
	}
	v := e.data.Vel
	switch rail.shape {
	case block.RailAscendingEast:
		v[0] -= slide
		y++
	case block.RailAscendingWest:
		v[0] += slide
		y++
	case block.RailAscendingNorth:
		v[2] += slide
		y++
	case block.RailAscendingSouth:
		v[2] -= slide
		y++
	}
	exit0, exit1 := minecartExits[rail.shape][0], minecartExits[rail.shape][1]
	xD, zD := float64(exit1[0]-exit0[0]), float64(exit1[2]-exit0[2])
	length := math.Sqrt(xD*xD + zD*zD)
	if v[0]*xD+v[2]*zD < 0 {
		xD, zD = -xD, -zD
	}
	pow := math.Min(2, horizontalLen(v))
	v = mgl64.Vec3{pow * xD / length, v[1], pow * zD / length}

	if intent := b.moveIntent(tx); intent.LenSqr() > 0 && horizontalLenSqr(v) < 0.01 {
		v = v.Add(mgl64.Vec3{intent[0] * 0.001, 0, intent[2] * 0.001})
		haltTrack = false
	}
	if haltTrack {
		if horizontalLen(v) < 0.03 {
			v = mgl64.Vec3{}
		} else {
			v = mgl64.Vec3{v[0] * 0.5, v[1] * 0, v[2] * 0.5}
		}
	}
	px, pz := float64(pos[0]), float64(pos[2])
	x0, z0 := px+0.5+float64(exit0[0])*0.5, pz+0.5+float64(exit0[2])*0.5
	x1, z1 := px+0.5+float64(exit1[0])*0.5, pz+0.5+float64(exit1[2])*0.5
	xD, zD = x1-x0, z1-z0
	var progress float64
	switch {
	case xD == 0:
		progress = z - pz
	case zD == 0:
		progress = x - px
	default:
		progress = ((x-x0)*xD + (z-z0)*zD) * 2
	}
	e.data.Pos = mgl64.Vec3{x0 + xD*progress, y, z0 + zD*progress}
	e.data.Vel = v
	scale := 1.0
	if b.rider != nil {
		scale = 0.75
	}
	maxSpeed := b.maxSpeed()
	b.move(e, tx, mgl64.Vec3{clampF(scale*v[0], -maxSpeed, maxSpeed), 0, clampF(scale*v[2], -maxSpeed, maxSpeed)})
	if b.removed {
		return
	}
	cur := e.data.Pos
	if exit0[1] != 0 && int(math.Floor(cur[0]))-pos[0] == exit0[0] && int(math.Floor(cur[2]))-pos[2] == exit0[2] {
		e.data.Pos[1] += float64(exit0[1])
	} else if exit1[1] != 0 && int(math.Floor(cur[0]))-pos[0] == exit1[0] && int(math.Floor(cur[2]))-pos[2] == exit1[2] {
		e.data.Pos[1] += float64(exit1[1])
	}
	e.data.Vel = b.naturalSlowdown(e.data.Vel)
	if newPos, ok := minecartRailPos(tx, e.data.Pos[0], e.data.Pos[1], e.data.Pos[2]); ok && oldOk {
		speed := (oldPos[1] - newPos[1]) * 0.05
		vel := e.data.Vel
		if h := horizontalLen(vel); h > 0 {
			e.data.Vel = mgl64.Vec3{vel[0] * ((h + speed) / h), vel[1], vel[2] * ((h + speed) / h)}
		}
		e.data.Pos[1] = newPos[1]
	}
	xn, zn := int(math.Floor(e.data.Pos[0])), int(math.Floor(e.data.Pos[2]))
	if xn != pos[0] || zn != pos[2] {
		h := horizontalLen(e.data.Vel)
		e.data.Vel = mgl64.Vec3{h * float64(xn-pos[0]), e.data.Vel[1], h * float64(zn-pos[2])}
	}
	if !powerTrack {
		return
	}
	vel := e.data.Vel
	if speed := horizontalLen(vel); speed > 0.01 {
		e.data.Vel = vel.Add(mgl64.Vec3{vel[0] / speed * 0.06, 0, vel[2] / speed * 0.06})
		return
	}
	dx, dz := vel[0], vel[2]
	switch rail.shape {
	case block.RailEastWest:
		if minecartConductor(tx, pos.Side(cube.FaceWest)) {
			dx = 0.02
		} else if minecartConductor(tx, pos.Side(cube.FaceEast)) {
			dx = -0.02
		}
	case block.RailNorthSouth:
		if minecartConductor(tx, pos.Side(cube.FaceNorth)) {
			dz = 0.02
		} else if minecartConductor(tx, pos.Side(cube.FaceSouth)) {
			dz = -0.02
		}
	default:
		return
	}
	e.data.Vel = mgl64.Vec3{dx, vel[1], dz}
}

// minecartConductor is Java Edition's isRedstoneConductor of the block at pos.
func minecartConductor(tx *world.Tx, pos cube.Pos) bool {
	if pos.OutOfBounds(tx.Range()) {
		return false
	}
	return world.RedstoneFullPowerConductor(pos, tx.Block(pos), tx)
}

// naturalSlowdown is AbstractMinecart.applyNaturalSlowdown, and AbstractMinecartContainer's for chest and hopper
// minecarts, which slow down less the fuller they are.
func (b *minecartBehaviour) naturalSlowdown(v mgl64.Vec3) mgl64.Vec3 {
	if b.inv != nil {
		keep := float32(0.98)
		keep += float32(15-javaContainerSignal(b.inv)) * float32(0.001)
		if b.inWater {
			keep *= 0.95
		}
		k := float64(keep)
		return mgl64.Vec3{v[0] * k, v[1] * 0, v[2] * k}
	}
	f := 0.96
	if b.rider != nil {
		f = 0.997
	}
	v = mgl64.Vec3{v[0] * f, v[1] * 0, v[2] * f}
	if b.inWater {
		v = v.Mul(float64(float32(0.95)))
	}
	return v
}

// javaContainerSignal is AbstractContainerMenu.getRedstoneSignalFromContainer, in Java Edition's float maths.
func javaContainerSignal(inv *inventory.Inventory) int {
	if inv == nil || inv.Size() == 0 {
		return 0
	}
	var total float32
	for _, s := range inv.Slots() {
		if !s.Empty() {
			total += float32(s.Count()) / float32(min(99, s.MaxCount()))
		}
	}
	total /= float32(inv.Size())
	n := int(math.Floor(float64(total * 14)))
	if total > 0 {
		n++
	}
	return n
}

// comeOffTrack is AbstractMinecart.comeOffTrack.
func (b *minecartBehaviour) comeOffTrack(e *Ent, tx *world.Tx) {
	maxSpeed := b.maxSpeed()
	v := e.data.Vel
	v = mgl64.Vec3{clampF(v[0], -maxSpeed, maxSpeed), v[1], clampF(v[2], -maxSpeed, maxSpeed)}
	if b.onGround {
		v = v.Mul(0.5)
	}
	e.data.Vel = v
	b.move(e, tx, v)
	if !b.removed && !b.onGround {
		e.data.Vel = e.data.Vel.Mul(float64(float32(0.95)))
	}
}

// moveIntent is ServerPlayer.getLastClientMoveIntent of the rider.
func (b *minecartBehaviour) moveIntent(tx *world.Tx) mgl64.Vec3 {
	if b.rider == nil || (b.moveLeft == 0 && b.moveForward == 0) {
		return mgl64.Vec3{}
	}
	yaw := b.moveYaw
	if r, ok := b.rider.Entity(tx); ok {
		yaw = float32(r.Rotation().Yaw())
	}
	in := mgl64.Vec3{b.moveLeft, 0, b.moveForward}
	if l := in.LenSqr(); l > 1 {
		in = javaNormalize(in)
	}
	rad := float64(yaw * float32(math.Pi/180))
	sin, cos := float64(mthSin(rad)), float64(mthCos(rad))
	return mgl64.Vec3{in[0]*cos - in[2]*sin, 0, in[2]*cos + in[0]*sin}
}

// insideBlocks gives the blocks a minecart is in the inside-block callbacks the redstone parts need, as Java
// Edition's applyEffectsFromBlocks does after every move of a minecart and at the end of its tick.
func (b *minecartBehaviour) insideBlocks(e *Ent, tx *world.Tx) {
	box := minecartBox(e.data.Pos).Grow(-float64(float32(1e-5)))
	for pos := range cube.Range3D(cube.PosFromVec3(box.Min()), cube.PosFromVec3(box.Max())) {
		if pos.OutOfBounds(tx.Range()) {
			continue
		}
		switch bl := tx.Block(pos).(type) {
		case block.DetectorRail:
			bl.EntityInside(pos, tx, e)
		case block.String:
			bl.EntityInside(pos, tx, e)
		case block.PressurePlate:
			bl.EntityInside(pos, tx, e)
		case block.Button:
			bl.EntityInside(pos, tx, e)
		}
	}
}

// minecartInWater is Java Edition's updateInWaterStateAndDoFluidPushing check for water around a minecart.
func minecartInWater(tx *world.Tx, pos mgl64.Vec3) bool {
	box := minecartBox(pos).Grow(-0.001)
	lo, hi := box.Min(), box.Max()
	for x := int(math.Floor(lo[0])); x < int(math.Ceil(hi[0])); x++ {
		for y := int(math.Floor(lo[1])); y < int(math.Ceil(hi[1])); y++ {
			for z := int(math.Floor(lo[2])); z < int(math.Ceil(hi[2])); z++ {
				p := cube.Pos{x, y, z}
				if p.OutOfBounds(tx.Range()) {
					continue
				}
				l, ok := tx.Liquid(p)
				if !ok {
					continue
				}
				if _, water := l.(block.Water); !water {
					continue
				}
				height := float64(l.LiquidDepth()) / 9
				if above, ok := tx.Liquid(p.Side(cube.FaceUp)); ok {
					if _, water := above.(block.Water); water {
						height = 1
					}
				}
				if l.LiquidFalling() {
					height = 1
				}
				if float64(y)+height >= lo[1] {
					return true
				}
			}
		}
	}
	return false
}

// pushAndPickupEntities is OldMinecartBehavior.pushAndPickupEntities. Only minecarts are pushed: Java Edition pushes
// players only in the server's copy of them, which it never sends, and Dragonfly mobs do not ride minecarts.
func (b *minecartBehaviour) pushAndPickupEntities(e *Ent, tx *world.Tx) {
	box := minecartBox(e.data.Pos)
	g := float64(float32(0.2))
	box = cube.Box(box.Min()[0]-g, box.Min()[1], box.Min()[2]-g, box.Max()[0]+g, box.Max()[1], box.Max()[2]+g)
	for other := range tx.EntitiesWithin(box.Grow(2)) {
		c, ok := other.(*Minecart)
		if !ok || c.H() == e.H() || !boxesIntersect(minecartBox(c.Position()), box) {
			continue
		}
		cb := c.behaviour()
		if cb.removed || (b.rider != nil && b.rider == c.H()) {
			continue
		}
		cb.pushedBy(c.Ent, e)
	}
}

// pushedByPlayers is LivingEntity.pushEntities of the players touching the minecart: they push it away.
func (b *minecartBehaviour) pushedByPlayers(e *Ent, tx *world.Tx) {
	box := minecartBox(e.data.Pos)
	for other := range tx.EntitiesWithin(box.Grow(3)) {
		p, ok := other.(interface {
			GameMode() world.GameMode
			RidingEntityHandle() *world.EntityHandle
		})
		if !ok || other.H() == e.H() || p.RidingEntityHandle() != nil || !p.GameMode().HasCollision() {
			continue
		}
		if !boxesIntersect(other.H().Type().BBox(other).Translate(other.Position()), box) {
			continue
		}
		b.pushedBy(e, other)
	}
}

// pushedBy is AbstractMinecart.push(Entity): pusher pushes the minecart e.
func (b *minecartBehaviour) pushedBy(e *Ent, pusher world.Entity) {
	if b.rider != nil && b.rider == pusher.H() {
		return
	}
	xa, za := pusher.Position()[0]-e.data.Pos[0], pusher.Position()[2]-e.data.Pos[2]
	dd := xa*xa + za*za
	if dd < float64(float32(1e-4)) {
		return
	}
	dd = math.Sqrt(dd)
	xa /= dd
	za /= dd
	pow := 1 / dd
	if pow > 1 {
		pow = 1
	}
	xa *= pow
	za *= pow
	xa *= float64(float32(0.1))
	za *= float64(float32(0.1))
	xa *= 0.5
	za *= 0.5
	if other, ok := asMinecart(pusher); ok {
		b.pushOtherMinecart(e, other, xa, za)
		return
	}
	e.data.Vel = e.data.Vel.Add(mgl64.Vec3{-xa, 0, -za})
}

// asMinecart returns the minecart behind an entity, if it is one.
func asMinecart(e world.Entity) (*Ent, bool) {
	ent, ok := asEnt(e)
	if !ok {
		return nil, false
	}
	if _, ok := ent.data.Data.(*minecartBehaviour); !ok {
		return nil, false
	}
	return ent, true
}

// pushOtherMinecart is AbstractMinecart.pushOtherMinecart in the old movement: two minecarts in line share their
// velocity.
func (b *minecartBehaviour) pushOtherMinecart(e, other *Ent, xa, za float64) {
	dir := javaNormalize(mgl64.Vec3{other.data.Pos[0] - e.data.Pos[0], 0, other.data.Pos[2] - e.data.Pos[2]})
	rad := float64(b.yRot * float32(math.Pi/180))
	facing := javaNormalize(mgl64.Vec3{float64(mthCos(rad)), 0, float64(mthSin(rad))})
	if math.Abs(dir[0]*facing[0]+dir[1]*facing[1]+dir[2]*facing[2]) < float64(float32(0.8)) {
		return
	}
	v, ov := e.data.Vel, other.data.Vel
	xdd, zdd := (ov[0]+v[0])/2, (ov[2]+v[2])/2
	e.data.Vel = mgl64.Vec3{v[0] * 0.2, v[1], v[2] * 0.2}.Add(mgl64.Vec3{xdd - xa, 0, zdd - za})
	other.data.Vel = mgl64.Vec3{ov[0] * 0.2, ov[1], ov[2] * 0.2}.Add(mgl64.Vec3{xdd + xa, 0, zdd + za})
}

// javaNormalize is Vec3.normalize.
func javaNormalize(v mgl64.Vec3) mgl64.Vec3 {
	l := math.Sqrt(v[0]*v[0] + v[1]*v[1] + v[2]*v[2])
	if l < float64(float32(1e-5)) {
		return mgl64.Vec3{}
	}
	return mgl64.Vec3{v[0] / l, v[1] / l, v[2] / l}
}

// boxesIntersect is AABB.intersects.
func boxesIntersect(a, b cube.BBox) bool {
	alo, ahi, blo, bhi := a.Min(), a.Max(), b.Min(), b.Max()
	return alo[0] < bhi[0] && ahi[0] > blo[0] && alo[1] < bhi[1] && ahi[1] > blo[1] && alo[2] < bhi[2] && ahi[2] > blo[2]
}

// checkRider frees the seat if the rider is gone or no longer riding the minecart.
func (b *minecartBehaviour) checkRider(e *Ent, tx *world.Tx) {
	if b.rider == nil {
		return
	}
	ent, ok := b.rider.Entity(tx)
	if !ok {
		b.rider = nil
		return
	}
	if r, ok := ent.(Rider); !ok || r.RidingEntityHandle() != e.H() {
		b.rider = nil
	}
}

// vehiclePositioned is a rider the server places on its vehicle (a player).
type vehiclePositioned interface {
	SetVehiclePosition(pos mgl64.Vec3)
}

// positionRider is Entity.positionRider: a player rider's feet are 0.1875 above the minecart, less the player's 0.6
// vehicle attachment.
func (b *minecartBehaviour) positionRider(e *Ent, tx *world.Tx) {
	if b.rider == nil {
		return
	}
	if r, ok := b.rider.Entity(tx); ok {
		if p, ok := r.(vehiclePositioned); ok {
			p.SetVehiclePosition(e.data.Pos.Add(mgl64.Vec3{0, 0.1875 - 0.6, 0}))
		}
	}
}

// ejectRider gets the rider off.
func (b *minecartBehaviour) ejectRider(e *Ent, tx *world.Tx) {
	if b.rider == nil {
		return
	}
	if ent, ok := b.rider.Entity(tx); ok {
		if r, ok := ent.(Rider); ok && r.RidingEntityHandle() == e.H() {
			r.DismountEntity(tx)
		}
	}
	b.rider = nil
}

// activate is activateMinecart, run by a minecart on an activator rail.
func (b *minecartBehaviour) activate(e *Ent, tx *world.Tx, powered bool) {
	switch b.kind {
	case MinecartPlain:
		if !powered {
			return
		}
		b.ejectRider(e, tx)
		if b.hurtTime == 0 {
			b.hurtDir = -b.hurtDir
			b.hurtTime = 10
			b.damage = 50
		}
	case MinecartTNT:
		if powered && b.fuse < 0 {
			b.primeFuse(e, tx)
		}
	case MinecartHopper:
		b.hopperEnabled = !powered
	}
}

// primeFuse is MinecartTNT.primeFuse.
func (b *minecartBehaviour) primeFuse(e *Ent, tx *world.Tx) {
	b.fuse = 80
	for _, v := range tx.Viewers(e.data.Pos) {
		v.ViewEntityAction(e, MinecartPrimeAction{})
		v.ViewEntityState(e)
	}
	tx.PlaySound(e.data.Pos, sound.TNT{})
}

// explode is MinecartTNT.explode.
func (b *minecartBehaviour) explode(e *Ent, tx *world.Tx, speedSqr float64) {
	speed := math.Min(math.Sqrt(speedSqr), 5)
	power := float64(float32(4 + 1*rand.Float64()*1.5*speed))
	pos := e.data.Pos
	b.remove(e, tx)
	block.ExplosionConfig{ItemDropChance: 1}.Explode(tx, minecartExplosion{pos: pos, size: power})
}

// minecartExplosion is the explosion of a TNT minecart, which is gone by the time it explodes.
type minecartExplosion struct {
	pos  mgl64.Vec3
	size float64
}

func (m minecartExplosion) Position() mgl64.Vec3 { return m.pos }
func (m minecartExplosion) Size() float64        { return m.size }

// fallOn is Block.fallOn and causeFallDamage: a TNT minecart that falls 3 blocks explodes.
func (b *minecartBehaviour) fallOn(e *Ent, tx *world.Tx, distance float64) {
	if b.kind == MinecartTNT && distance >= 3 {
		p := distance / 10
		b.explode(e, tx, p*p)
	}
}

// suckInItems is MinecartHopper.suckInItems for item entities: the hopper minecart picks up items around it.
func (b *minecartBehaviour) suckInItems(e *Ent, tx *world.Tx) {
	box := minecartBox(e.data.Pos)
	box = cube.Box(box.Min()[0]-0.25, box.Min()[1], box.Min()[2]-0.25, box.Max()[0]+0.25, box.Max()[1], box.Max()[2]+0.25)
	for other := range tx.EntitiesWithin(box.Grow(1)) {
		ent, ok := asEnt(other)
		if !ok {
			continue
		}
		ib, ok := ent.Behaviour().(*ItemBehaviour)
		if !ok || !boxesIntersect(other.H().Type().BBox(other).Translate(other.Position()), box) {
			continue
		}
		stack := ib.Item()
		n, err := b.inv.AddItem(stack)
		if n == 0 && err != nil {
			continue
		}
		_ = ent.Close()
		if n < stack.Count() {
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: other.Position()}, stack.Grow(-n)))
		}
		return
	}
}

// Hurt is VehicleEntity.hurtServer: hits make the minecart shake and add damage; over 40 it breaks.
func (b *minecartBehaviour) Hurt(e *Ent, damage float64, src world.DamageSource) (float64, bool) {
	if b.removed {
		return 0, false
	}
	if _, ok := src.(VoidDamageSource); ok {
		b.remove(e, e.tx)
		return damage, true
	}
	var attacker world.Entity
	switch s := src.(type) {
	case AttackDamageSource:
		attacker = s.Attacker
	case ProjectileDamageSource:
		attacker = s.Owner
	}
	creative := false
	if g, ok := attacker.(interface{ GameMode() world.GameMode }); ok {
		creative = g.GameMode().CreativeInventory()
	}
	b.hurtDir = -b.hurtDir
	b.hurtTime = 10
	b.damage += float32(damage) * 10
	ignites := b.kind == MinecartTNT && minecartIgnites(src)
	switch {
	case (creative || !(b.damage > 40)) && !ignites:
		if creative {
			b.remove(e, e.tx)
			return damage, true
		}
		e.updateState()
	default:
		b.destroy(e, e.tx, src)
	}
	return damage, true
}

// minecartIgnites is MinecartTNT.damageSourceIgnitesTnt.
func minecartIgnites(src world.DamageSource) bool {
	if s, ok := src.(ProjectileDamageSource); ok {
		if f, ok := s.Projectile.(interface{ OnFireDuration() time.Duration }); ok {
			return f.OnFireDuration() > 0
		}
		return false
	}
	if _, ok := src.(ExplosionDamageSource); ok {
		return true
	}
	return src.Fire()
}

// Explode hurts the minecart with the explosion's damage, as Java Edition's ServerExplosion does.
func (b *minecartBehaviour) Explode(e *Ent, src world.ExplosionSource, impact float64) {
	if impact <= 0 || b.removed {
		return
	}
	dmg := math.Floor((impact*impact+impact)/2*7*src.Size()*2 + 1)
	b.Hurt(e, dmg, ExplosionDamageSource{Source: src})
}

// destroy is VehicleEntity.destroy: the minecart breaks and drops itself and what it carries.
func (b *minecartBehaviour) destroy(e *Ent, tx *world.Tx, src world.DamageSource) {
	if b.kind == MinecartTNT {
		if !minecartIgnites(src) && horizontalLenSqr(e.data.Vel) < float64(float32(0.01)) {
			b.drop(e, tx)
			b.remove(e, tx)
		} else if b.fuse < 0 {
			b.primeFuse(e, tx)
			b.fuse = rand.IntN(20) + rand.IntN(20)
		}
		return
	}
	b.drop(e, tx)
	b.remove(e, tx)
}

// drop drops the minecart item, with the minecart's name, and the items a container minecart holds.
func (b *minecartBehaviour) drop(e *Ent, tx *world.Tx) {
	pos := e.data.Pos
	stack := item.NewStack(item.Minecart{Kind: b.kind}, 1)
	if name := e.NameTag(); name != "" {
		stack = stack.WithCustomName(name)
	}
	vel := mgl64.Vec3{rand.Float64()*0.2 - 0.1, 0.2, rand.Float64()*0.2 - 0.1}
	tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: pos, Velocity: vel}, stack))
	if b.inv == nil {
		return
	}
	for _, s := range b.inv.Clear() {
		vel := mgl64.Vec3{rand.Float64()*0.2 - 0.1, 0.2, rand.Float64()*0.2 - 0.1}
		tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: pos, Velocity: vel}, s))
	}
}

// remove gets the rider off and removes the minecart.
func (b *minecartBehaviour) remove(e *Ent, tx *world.Tx) {
	if b.removed {
		return
	}
	b.removed = true
	b.ejectRider(e, tx)
	_ = e.Close()
}

// minecartDismountPosition is AbstractMinecart.getDismountLocationForPassenger for a standing player.
func minecartDismountPosition(e *Ent, rider world.Entity) mgl64.Vec3 {
	b := e.data.Data.(*minecartBehaviour)
	tx := e.tx
	// Direction.fromYRot, then getMotionDirection: the direction turned clockwise, or the opposite one if flipped.
	dirs := [4][2]int{{0, 1}, {-1, 0}, {0, -1}, {1, 0}} // south, west, north, east: Java Edition's 2D data order
	d := int(math.Floor(float64(b.yRot)/90+0.5)) & 3
	if b.flipped {
		d = (d + 2) & 3
	}
	forward := dirs[(d+1)&3] // clockwise
	right := dirs[(d+2)&3]
	left := [2]int{-right[0], -right[1]}
	back := [2]int{-forward[0], -forward[1]}
	offsets := [][2]int{right, left, {back[0] + right[0], back[1] + right[1]}, {back[0] + left[0], back[1] + left[1]},
		{forward[0] + right[0], forward[1] + right[1]}, {forward[0] + left[0], forward[1] + left[1]}, back, forward}
	at := floorPos(e.data.Pos)
	width, height := 0.6, 1.8
	if bb := rider.H().Type().BBox(rider); bb.Width() > 0 {
		width, height = bb.Width(), bb.Height()
	}
	reach := math.Min(width, 1) / 2
	for _, dy := range []int{0, 1, -1} {
		for _, o := range offsets {
			target := cube.Pos{at[0] + o[0], at[1] + dy, at[2] + o[1]}
			floor, ok := blockFloorHeight(tx, target)
			if !ok {
				continue
			}
			loc := mgl64.Vec3{float64(target[0]) + 0.5, float64(target[1]) + floor, float64(target[2]) + 0.5}
			box := cube.Box(loc[0]-reach, loc[1], loc[2]-reach, loc[0]+reach, loc[1]+height, loc[2]+reach)
			if len(minecartBlocking(tx, box)) == 0 {
				return loc
			}
		}
	}
	return mgl64.Vec3{e.data.Pos[0], e.data.Pos[1] + minecartHeight, e.data.Pos[2]}
}

// blockFloorHeight is DismountHelper's floor height at pos, and whether a rider can stand there.
func blockFloorHeight(tx *world.Tx, pos cube.Pos) (float64, bool) {
	top := func(p cube.Pos) (float64, bool) {
		if p.OutOfBounds(tx.Range()) {
			return 0, false
		}
		boxes := tx.Block(p).Model().BBox(p, tx)
		if len(boxes) == 0 {
			return 0, false
		}
		h := 0.0
		for _, bb := range boxes {
			h = math.Max(h, bb.Max()[1])
		}
		return h, true
	}
	if h, ok := top(pos); ok {
		return h, h < 1
	}
	if h, ok := top(pos.Side(cube.FaceDown)); ok && h >= 1 {
		return h - 1, h-1 < 1
	}
	return 0, false
}

// minecartBlocking returns the block boxes overlapping box.
func minecartBlocking(tx *world.Tx, box cube.BBox) []cube.BBox {
	var hits []cube.BBox
	for _, bb := range minecartColliders(tx, box) {
		if bb.Min()[0] < box.Max()[0]-minecartCollideEpsilon && bb.Max()[0] > box.Min()[0]+minecartCollideEpsilon &&
			bb.Min()[1] < box.Max()[1]-minecartCollideEpsilon && bb.Max()[1] > box.Min()[1]+minecartCollideEpsilon &&
			bb.Min()[2] < box.Max()[2]-minecartCollideEpsilon && bb.Max()[2] > box.Min()[2]+minecartCollideEpsilon {
			hits = append(hits, bb)
		}
	}
	return hits
}
