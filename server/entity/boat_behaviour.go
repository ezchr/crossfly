//go:build crossfly_full

package entity

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// boatIntegrity is the structural integrity a boat starts with (Boat::Boat's setStructuralIntegrity(40)). Every
// hurt takes its damage off; at 0 the boat breaks. It does not come back.
const boatIntegrity = 40

// boatAttackScale scales the damage of hits by players and projectiles. Assumption: Boat::_hurt takes the damage
// off as it is, which would take 40 punches; the scale of Java Edition's VehicleEntity (damage * 10) is used for
// hits instead, so a fist breaks a boat in 4 hits. Lava and cactus hurt by their own damage, as in BDS.
const boatAttackScale = 10

// boatStatus is where a boat is: Java Edition's AbstractBoat.Status.
type boatStatus uint8

const (
	boatInWater boatStatus = iota
	boatUnderWater
	boatUnderFlowingWater
	boatOnLand
	boatInAir
)

// boatJavaHeight is Java Edition's boat height, which its buoyancy is worked out from.
const boatJavaHeight = 0.5625

// boatBehaviour is the behaviour of a boat.
type boatBehaviour struct {
	wood BoatWood
	inv  *inventory.Inventory

	riders [2]*world.EntityHandle

	hurtTime, hurtDir int
	integrity         int

	paddleLeft, paddleRight bool
	rowLeft, rowRight       float32
	rowing                  bool

	ticks, clientMovedAt int64

	status     boatStatus
	waterLevel float64
	onGround   bool
	removed    bool

	sentPos                          mgl64.Vec3
	sentRot                          cube.Rotation
	sentVel                          mgl64.Vec3
	sentHurt, sentDir, sentIntegrity int
	sentLeft, sentRight              bool
}

func newBoatBehaviour(wood BoatWood, chest bool) *boatBehaviour {
	b := &boatBehaviour{wood: wood, hurtDir: 1, integrity: boatIntegrity, status: boatInAir, clientMovedAt: -100}
	b.sentDir, b.sentIntegrity = 1, boatIntegrity
	if chest {
		b.inv = inventory.New(27, nil)
	}
	return b
}

// controller is the rider of the first seat, who steers.
func (b *boatBehaviour) controller() *world.EntityHandle { return b.riders[0] }

// driven reports whether the driver's client moves the boat: it sent a position in the last few ticks.
func (b *boatBehaviour) driven() bool {
	return b.riders[0] != nil && b.ticks-b.clientMovedAt <= 5
}

// clientMove takes a position the driver's client moved the boat to.
func (b *boatBehaviour) clientMove(e *Ent, rider world.Entity, pos mgl64.Vec3, yaw float64) bool {
	if b.removed || b.riders[0] == nil || b.riders[0] != rider.H() {
		return false
	}
	if math.IsNaN(pos.Len()) || math.IsInf(pos.Len(), 0) || pos.Sub(e.data.Pos).Len() > 8 || math.IsNaN(yaw) {
		return false
	}
	e.data.Vel = pos.Sub(e.data.Pos)
	e.data.Pos = pos
	e.data.Rot = cube.Rotation{yaw, 0}
	b.clientMovedAt = b.ticks
	return true
}

// Tick ...
func (b *boatBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if b.removed {
		return nil
	}
	b.ticks++
	b.checkRiders(e, tx)
	if b.hurtTime > 0 {
		b.hurtTime--
	}
	if b.hurtByBlocks(e, tx); b.removed {
		return nil
	}
	if !b.driven() {
		b.float(e, tx)
		b.pushedByEntities(e, tx)
	}
	b.paddle()
	b.positionRiders(e, tx)
	b.send(e, tx)
	return nil
}

// checkRiders frees the seats of riders that are gone or got off.
func (b *boatBehaviour) checkRiders(e *Ent, tx *world.Tx) {
	for i, h := range b.riders {
		if h == nil {
			continue
		}
		ent, ok := h.Entity(tx)
		if !ok {
			b.riders[i] = nil
			continue
		}
		if r, ok := RiderOf(ent); !ok || r.RidingEntityHandle() != e.H() {
			b.riders[i] = nil
		}
	}
	if b.riders[0] == nil && b.riders[1] != nil {
		b.riders[0], b.riders[1] = b.riders[1], nil
	}
	if b.riders[0] == nil {
		b.paddleLeft, b.paddleRight = false, false
	}
}

// paddle turns the paddles the driver works. Java Edition fallback: a paddle in use turns by pi/8 a tick and goes
// back to 0 when let go (AbstractBoat.tick); the 1.21.3 dump keeps the times in BoatPaddleComponent without the rate.
func (b *boatBehaviour) paddle() {
	const step = float32(math.Pi / 8)
	b.rowing = b.paddleLeft || b.paddleRight || b.rowLeft != 0 || b.rowRight != 0
	if b.paddleLeft {
		b.rowLeft += step
	} else {
		b.rowLeft = 0
	}
	if b.paddleRight {
		b.rowRight += step
	} else {
		b.rowRight = 0
	}
}

// hurtByBlocks hurts the boat in lava (4 a tick, hurt_on_condition of boat.json) and against a cactus (1 a tick, the
// cactus contact damage).
func (b *boatBehaviour) hurtByBlocks(e *Ent, tx *world.Tx) {
	box := BoatType.BBox(nil).Translate(e.data.Pos)
	inner := box.Grow(-1e-4)
	for pos := range cube.Range3D(cube.PosFromVec3(inner.Min()), cube.PosFromVec3(inner.Max())) {
		if pos.OutOfBounds(tx.Range()) {
			continue
		}
		if l, ok := tx.Liquid(pos); ok {
			if _, lava := l.(block.Lava); lava {
				b.Hurt(e, 4, block.LavaDamageSource{})
				return
			}
		}
	}
	outer := box.Grow(0.01)
	for pos := range cube.Range3D(cube.PosFromVec3(outer.Min()), cube.PosFromVec3(outer.Max())) {
		if pos.OutOfBounds(tx.Range()) {
			continue
		}
		if c, ok := tx.Block(pos).(block.Cactus); ok {
			b.Hurt(e, 1, block.DamageSource{Block: c})
			return
		}
	}
}

// float moves a boat nobody steers. Java Edition fallback (AbstractBoat.floatBoat with its getStatus): BDS floats
// boats with the buoyant component of boat.json, whose maths is not in the 1.21.3 dump. The boat rests on the water
// as a Java boat does; waves are left out.
func (b *boatBehaviour) float(e *Ent, tx *world.Tx) {
	last := b.status
	b.status = b.statusAt(tx, e.data.Pos)
	v := e.data.Vel
	if last == boatInAir && b.status != boatInAir && b.status != boatOnLand {
		e.data.Pos[1] = b.waterLevelAbove(tx, e.data.Pos) - boatJavaHeight + 0.101
		v[1] = 0
		b.status = boatInWater
	} else {
		gravity, buoyancy, friction := -0.04, 0.0, 0.05
		switch b.status {
		case boatInWater:
			buoyancy = (b.waterLevel - e.data.Pos[1]) / boatJavaHeight
			friction = 0.9
		case boatUnderFlowingWater:
			gravity, friction = -7.0e-4, 0.9
		case boatUnderWater:
			buoyancy, friction = 0.01, 0.45
		case boatInAir:
			friction = 0.9
		case boatOnLand:
			friction = boatGroundFriction(tx, e.data.Pos)
		}
		v = mgl64.Vec3{v[0] * friction, v[1] + gravity, v[2] * friction}
		if buoyancy > 0 {
			v[1] = (v[1] + buoyancy*0.06153846) * 0.75
		}
	}
	box := BoatType.BBox(nil).Translate(e.data.Pos)
	moved := minecartCollide(tx, box, v)
	e.data.Pos = e.data.Pos.Add(moved)
	b.onGround = v[1] < 0 && moved[1] != v[1]
	for i := range 3 {
		if moved[i] != v[i] {
			v[i] = 0
		}
	}
	if horizontalLenSqr(v) < 1e-8 {
		v[0], v[2] = 0, 0
	}
	if math.Abs(v[1]) < 1e-6 {
		v[1] = 0
	}
	e.data.Vel = v
}

// waterHeight is the height of the water at pos, or false if there is none: Java Edition's fluid height (amount / 9,
// 1 under more water).
func waterHeight(tx *world.Tx, pos cube.Pos) (float64, bool) {
	if pos.OutOfBounds(tx.Range()) {
		return 0, false
	}
	l, ok := tx.Liquid(pos)
	if !ok || !isWaterLiquid(l) {
		return 0, false
	}
	if above, ok := tx.Liquid(pos.Side(cube.FaceUp)); ok && isWaterLiquid(above) {
		return 1, true
	}
	if l.LiquidFalling() {
		return 1, true
	}
	return float64(l.LiquidDepth()) / 9, true
}

// isWaterLiquid reports whether l is water.
func isWaterLiquid(l world.Liquid) bool {
	_, ok := l.(block.Water)
	return ok
}

// statusAt is AbstractBoat.getStatus for a boat at pos, keeping the water level it finds.
func (b *boatBehaviour) statusAt(tx *world.Tx, pos mgl64.Vec3) boatStatus {
	box := BoatType.BBox(nil).Translate(pos)
	lo, hi := box.Min(), box.Max()
	x0, x1 := int(math.Floor(lo[0])), int(math.Ceil(hi[0]))
	z0, z1 := int(math.Floor(lo[2])), int(math.Ceil(hi[2]))

	// isUnderwater: water over the top of the boat.
	under, flowing := false, true
	for x := x0; x < x1; x++ {
		for y := int(math.Floor(hi[1])); y < int(math.Ceil(hi[1]+0.001)); y++ {
			for z := z0; z < z1; z++ {
				p := cube.Pos{x, y, z}
				if h, ok := waterHeight(tx, p); ok && hi[1] < float64(y)+h {
					under = true
					if l, _ := tx.Liquid(p); l.LiquidDepth() == 8 && !l.LiquidFalling() {
						flowing = false
					}
				}
			}
		}
	}
	if under {
		if flowing {
			return boatUnderFlowingWater
		}
		return boatUnderWater
	}
	// checkInWater: the highest water the bottom of the boat is in.
	b.waterLevel = -math.MaxFloat64
	in := false
	for x := x0; x < x1; x++ {
		for y := int(math.Floor(lo[1])); y < int(math.Ceil(lo[1]+0.001)); y++ {
			for z := z0; z < z1; z++ {
				if h, ok := waterHeight(tx, cube.Pos{x, y, z}); ok {
					top := float64(y) + h
					b.waterLevel = math.Max(b.waterLevel, top)
					in = in || lo[1] < top
				}
			}
		}
	}
	if in {
		return boatInWater
	}
	below := cube.Box(lo[0], lo[1]-0.001, lo[2], hi[0], lo[1], hi[2])
	if len(minecartColliders(tx, below)) > 0 {
		for _, bb := range minecartColliders(tx, below) {
			if boxesIntersect(bb, below) {
				return boatOnLand
			}
		}
	}
	return boatInAir
}

// waterLevelAbove is AbstractBoat.getWaterLevelAbove for the column under the middle of the boat.
func (b *boatBehaviour) waterLevelAbove(tx *world.Tx, pos mgl64.Vec3) float64 {
	top := BoatType.BBox(nil).Translate(pos).Max()[1]
	p := cube.Pos{int(math.Floor(pos[0])), int(math.Floor(top)), int(math.Floor(pos[2]))}
	for i := 0; i < 16; i++ {
		h, ok := waterHeight(tx, p)
		if !ok {
			return float64(p[1])
		}
		if h < 1 {
			return float64(p[1]) + h
		}
		p[1]++
	}
	return float64(p[1])
}

// boatGroundFriction is the friction of the block under a boat on land. Java Edition fallback
// (AbstractBoat.getGroundFriction, without its averaging over the blocks under the boat): ice keeps it going.
func boatGroundFriction(tx *world.Tx, pos mgl64.Vec3) float64 {
	p := cube.PosFromVec3(pos.Sub(mgl64.Vec3{0, 0.001, 0}))
	if p.OutOfBounds(tx.Range()) {
		return 0.6
	}
	switch name, _ := tx.Block(p).EncodeBlock(); name {
	case "minecraft:ice", "minecraft:packed_ice", "minecraft:frosted_ice":
		return 0.98
	case "minecraft:blue_ice":
		return 0.989
	case "minecraft:slime":
		return 0.8
	}
	return 0.6
}

// pushedByEntities lets the players and boats touching a boat push it away: pushable_by_entity's legacy_boat push
// (strength 0.1, push_scale_self 0.5).
func (b *boatBehaviour) pushedByEntities(e *Ent, tx *world.Tx) {
	box := BoatType.BBox(nil).Translate(e.data.Pos)
	for other := range tx.EntitiesWithin(box.Grow(3)) {
		if other.H() == e.H() || b.carries(other.H()) {
			continue
		}
		switch o := other.(type) {
		case *Boat:
			if o.behaviour().removed {
				continue
			}
		case interface {
			GameMode() world.GameMode
			RidingEntityHandle() *world.EntityHandle
		}:
			if o.RidingEntityHandle() != nil || !o.GameMode().HasCollision() {
				continue
			}
		default:
			continue
		}
		if !boxesIntersect(other.H().Type().BBox(other).Translate(other.Position()), box) {
			continue
		}
		xa, za := other.Position()[0]-e.data.Pos[0], other.Position()[2]-e.data.Pos[2]
		dd := xa*xa + za*za
		if dd < 1e-4 {
			continue
		}
		dd = math.Sqrt(dd)
		pow := math.Min(1/dd, 1)
		e.data.Vel = e.data.Vel.Sub(mgl64.Vec3{xa / dd * pow * 0.1 * 0.5, 0, za / dd * pow * 0.1 * 0.5})
	}
}

// carries reports whether h rides the boat.
func (b *boatBehaviour) carries(h *world.EntityHandle) bool {
	return h != nil && (b.riders[0] == h || b.riders[1] == h)
}

// positionRiders puts the riders the server places (players) on their seats, where a Bedrock client puts itself:
// the seat turned with the boat, from the boat's network position, less the 1.62 eye height.
func (b *boatBehaviour) positionRiders(e *Ent, tx *world.Tx) {
	c := &Boat{Ent: e}
	seats := c.SeatPositions()
	sin, cos := math.Sincos(e.data.Rot.Yaw() * math.Pi / 180)
	for i, h := range b.riders {
		if h == nil || i >= len(seats) {
			continue
		}
		r, ok := h.Entity(tx)
		if !ok {
			continue
		}
		p, ok := r.(vehiclePositioned)
		if !ok {
			continue
		}
		s := seats[i]
		off := mgl64.Vec3{s[0]*cos - s[2]*sin, boatNetworkOffset + s[1] - 1.62, s[2]*cos + s[0]*sin}
		p.SetVehiclePosition(e.data.Pos.Add(off))
	}
}

// BoatRowAction shows viewers the paddles of a boat turning: BDS's BoatPaddleAnimationSystem sends the row times as
// Animate packets 128 (right) and 129 (left).
type BoatRowAction struct {
	action
	Left, Right float32
}

// BoatRowTimes returns the row times of the paddles.
func (a BoatRowAction) BoatRowTimes() (left, right float32) { return a.Left, a.Right }

// send shows viewers what changed.
func (b *boatBehaviour) send(e *Ent, tx *world.Tx) {
	moved := e.data.Pos != b.sentPos || e.data.Rot != b.sentRot
	velChanged := e.data.Vel != b.sentVel && !b.driven()
	stateChanged := b.hurtTime != b.sentHurt || b.hurtDir != b.sentDir || b.integrity != b.sentIntegrity ||
		b.paddleLeft != b.sentLeft || b.paddleRight != b.sentRight
	b.sentPos, b.sentRot, b.sentVel = e.data.Pos, e.data.Rot, e.data.Vel
	b.sentHurt, b.sentDir, b.sentIntegrity = b.hurtTime, b.hurtDir, b.integrity
	b.sentLeft, b.sentRight = b.paddleLeft, b.paddleRight
	if !moved && !velChanged && !stateChanged && !b.rowing {
		return
	}
	for _, v := range tx.Viewers(e.data.Pos) {
		if moved {
			v.ViewEntityMovement(e, e.data.Pos, e.data.Rot, b.onGround)
		}
		if velChanged {
			v.ViewEntityVelocity(e, e.data.Vel)
		}
		if stateChanged {
			v.ViewEntityState(e)
		}
		if b.rowing {
			v.ViewEntityAction(e, BoatRowAction{Left: b.rowLeft, Right: b.rowRight})
		}
	}
}

// Hurt is Boat::_hurt: the boat shakes and loses integrity, and breaks at 0 or when a creative player hits it.
func (b *boatBehaviour) Hurt(e *Ent, damage float64, src world.DamageSource) (float64, bool) {
	if b.removed {
		return 0, false
	}
	damage = max(damage, 0)
	var attacker world.Entity
	scale := 1.0
	switch s := src.(type) {
	case VoidDamageSource:
		b.remove(e, e.tx)
		return damage, true
	case AttackDamageSource:
		attacker, scale = s.Attacker, boatAttackScale
	case ProjectileDamageSource:
		attacker, scale = s.Owner, boatAttackScale
	}
	creative := false
	if g, ok := attacker.(interface{ GameMode() world.GameMode }); ok {
		if !g.GameMode().AllowsEditing() {
			return 0, false
		}
		creative = g.GameMode().CreativeInventory()
	}
	b.hurtDir = -b.hurtDir
	b.hurtTime = 10
	b.integrity = int(float64(b.integrity) - damage*scale)
	if creative || b.integrity <= 0 {
		b.destroy(e, e.tx, !creative)
		return damage, true
	}
	e.updateState()
	return damage, true
}

// Explode hurts the boat with the damage an explosion deals.
func (b *boatBehaviour) Explode(e *Ent, src world.ExplosionSource, impact float64) {
	if impact <= 0 || b.removed {
		return
	}
	b.Hurt(e, math.Floor((impact*impact+impact)/2*7*src.Size()*2+1), ExplosionDamageSource{Source: src})
}

// destroy is Boat::destroy and ChestBoat::destroy: the boat drops itself, named as it was, and a chest boat what it
// holds. Creative players break it without drops.
func (b *boatBehaviour) destroy(e *Ent, tx *world.Tx, drop bool) {
	if b.removed {
		return
	}
	pos := e.data.Pos
	if drop {
		stack := item.NewStack(item.Boat{Wood: b.wood, Chest: b.inv != nil}, 1)
		if name := e.NameTag(); name != "" {
			stack = stack.WithCustomName(name)
		}
		tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: pos, Velocity: boatDropVelocity()}, stack))
	}
	if b.inv != nil {
		// ChestBoat::_dropChestContents drops the chest even for creative players.
		for _, s := range b.inv.Clear() {
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: pos, Velocity: boatDropVelocity()}, s))
		}
	}
	b.remove(e, tx)
}

func boatDropVelocity() mgl64.Vec3 {
	return mgl64.Vec3{rand.Float64()*0.2 - 0.1, 0.2, rand.Float64()*0.2 - 0.1}
}

// remove gets the riders off and removes the boat.
func (b *boatBehaviour) remove(e *Ent, tx *world.Tx) {
	if b.removed {
		return
	}
	b.removed = true
	for i, h := range b.riders {
		b.riders[i] = nil
		if h == nil {
			continue
		}
		if ent, ok := h.Entity(tx); ok {
			if r, ok := RiderOf(ent); ok && r.RidingEntityHandle() == e.H() {
				r.DismountEntity(tx)
			}
		}
	}
	_ = e.Close()
}

// EntityWindow is the chest of a chest boat: 27 slots (minecraft:inventory of chest_boat.json).
func (b *boatBehaviour) EntityWindow(e *Ent, viewer world.Entity, _ WindowLayout) (EntityWindow, bool) {
	if b.inv == nil || b.removed || viewer.Position().Sub(e.data.Pos).Len() > 8 {
		return EntityWindow{}, false
	}
	return EntityWindow{Inventory: b.inv, ChestSlots: 27, Chest: true}, true
}
