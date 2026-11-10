//go:build crossfly_full

package entity

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// javaItem is the state Java's ItemEntity keeps that Dragonfly's item entity does not.
type javaItem struct {
	// ticks is Entity.tickCount, age is ItemEntity.age.
	ticks, age int
	// water and lava are the heights of those fluids over the item's bottom, from the last fluid update.
	water, lava float64
}

// javaTick ticks an item entity as Java's ItemEntity.tick (26.3): the fluid update of Entity.baseTick (water and lava
// currents), buoyancy in water (x0.99, +0.0005 up while slower than 0.06 up) or lava (x0.95) instead of gravity, the
// move (skipped 3 of 4 ticks while resting on the ground), the effects of the blocks it is in (bubble columns), air
// drag 0.98 and ground friction, a bounce off the ground, merging with nearby items (every 2 ticks after moving to
// another block, every 40 otherwise), the second fluid update at the end of the tick, and despawning at age 6000.
func (i *ItemBehaviour) javaTick(e *Ent, tx *world.Tx) *Movement {
	p, j := i.passive, &i.java
	if p.close || i.i.Empty() {
		_ = e.Close()
		return nil
	}
	j.ticks++
	viewers := tx.Viewers(e.data.Pos)
	posBefore, velBefore := e.data.Pos, e.data.Vel

	j.water, j.lava = applyFluidCurrents(e, tx)
	if i.pickupDelay > 0 && i.pickupDelay < math.MaxInt16*(time.Second/20) {
		i.pickupDelay -= time.Second / 20
	}

	vel := e.data.Vel
	switch {
	case j.water > float64(float32(0.1)):
		vel = mgl64.Vec3{vel[0] * float64(float32(0.99)), vel[1] + itemBuoyancy(vel[1]), vel[2] * float64(float32(0.99))}
	case j.lava > float64(float32(0.1)):
		vel = mgl64.Vec3{vel[0] * float64(float32(0.95)), vel[1] + itemBuoyancy(vel[1]), vel[2] * float64(float32(0.95))}
	default:
		vel[1] -= 0.04
	}

	// An item inside a block (a piston moved one onto it, it was dropped in one) is pushed out towards the nearest
	// open side and moves through blocks until it is out (noPhysics, Entity.moveTowardsClosestSpace).
	box := e.H().Type().BBox(e).Translate(e.data.Pos)
	noPhysics := insideCollision(tx, box.Grow(-1.0e-7))
	if noPhysics {
		e.data.Vel = vel
		moveTowardsClosestSpace(e, tx, mgl64.Vec3{e.data.Pos[0], (box.Min()[1] + box.Max()[1]) / 2, e.data.Pos[2]})
		vel = e.data.Vel
	}

	pos, onGround := e.data.Pos, p.mc.OnGround()
	var dPos mgl64.Vec3
	if onGround && vel[0]*vel[0]+vel[2]*vel[2] <= float64(float32(1.0e-5)) && j.ticks%4 != 0 {
		e.data.Vel = vel
		javaItemBlockEffects(e, tx)
	} else {
		if noPhysics {
			dPos = vel
		} else {
			dPos, vel = p.mc.CheckCollision(tx, e, pos, vel)
		}
		pos = pos.Add(dPos)
		onGround = p.mc.OnGround()
		if f := javaBlockSpeedFactor(tx, pos); f != 1 {
			vel[0] *= float64(f)
			vel[2] *= float64(f)
		}
		e.data.Pos, e.data.Vel = pos, vel
		javaItemBlockEffects(e, tx)
		vel = e.data.Vel
		airDrag := float32(0.98)
		groundFriction := airDrag
		if onGround {
			groundFriction *= javaBlockFriction(tx, pos)
		}
		vel = mgl64.Vec3{vel[0] * float64(groundFriction), vel[1] * float64(airDrag), vel[2] * float64(groundFriction)}
		if onGround && vel[1] < 0 {
			vel[1] *= -0.5
		}
		e.data.Vel = vel
	}
	p.fallDistance = math.Max(p.fallDistance-dPos[1], 0)

	moved := math.Floor(posBefore[0]) != math.Floor(pos[0]) || math.Floor(posBefore[1]) != math.Floor(pos[1]) || math.Floor(posBefore[2]) != math.Floor(pos[2])
	rate := 40
	if moved {
		rate = 2
	}
	if j.ticks%rate == 0 && i.javaMergable() {
		i.javaMergeWithNeighbours(e, tx)
		if _, ok := e.H().Entity(tx); !ok {
			return nil
		}
	}
	j.age++
	j.water, j.lava = applyFluidCurrents(e, tx)

	if i.pickupDelay == 0 && i.javaCollect(e, tx) {
		return nil
	}
	// Interim: until hoppers suck items in themselves (Java's HopperBlockEntity.suckInItems), the item still drops
	// into a hopper under it here, at the end of its tick, where Java's hopper would take it in its own tick.
	if i.intoHopperBelow(e, tx) {
		return nil
	}
	if j.age >= 6000 {
		_ = e.Close()
		return nil
	}
	return &Movement{v: viewers, e: e, pos: e.data.Pos, vel: e.data.Vel, dpos: e.data.Pos.Sub(posBefore), dvel: e.data.Vel.Sub(velBefore), rot: e.data.Rot, onGround: onGround}
}

// javaCollect lets a collector (a player) next to the item pick it up. Java players pick items up in their own tick;
// Dragonfly's collectors are looked for by the item. It reports whether the item is gone.
func (i *ItemBehaviour) javaCollect(e *Ent, tx *world.Tx) bool {
	grown := e.H().Type().BBox(e).GrowVec3(mgl64.Vec3{1, 0.5, 1}).Translate(e.data.Pos)
	for other := range tx.EntitiesWithin(grown.Grow(2)) {
		collector, ok := other.(Collector)
		if !ok || other.H() == e.H() || !other.H().Type().BBox(other).Translate(other.Position()).IntersectsWith(grown) {
			continue
		}
		i.collect(e, collector, tx)
		_, alive := e.H().Entity(tx)
		return !alive
	}
	return false
}

// itemBuoyancy is what ItemEntity.setFluidMovement adds to the Y velocity: 0.0005 while slower than 0.06 upwards.
func itemBuoyancy(vy float64) float64 {
	if vy < float64(float32(0.06)) {
		return float64(float32(5.0e-4))
	}
	return 0
}

// javaItemBlockEffects applies the blocks the item is in, as Entity.applyEffectsFromBlocks: bubble columns push it
// up or pull it down (BubbleColumnBlock.entityInside), harder at the top of the column than inside it.
func javaItemBlockEffects(e *Ent, tx *world.Tx) {
	box := e.H().Type().BBox(e).Translate(e.data.Pos).Grow(-1.0e-7)
	low, high := cube.PosFromVec3(box.Min()), cube.PosFromVec3(box.Max())
	for pos := range cube.Range3D(low, high) {
		c, ok := tx.Block(pos).(block.BubbleColumn)
		if !ok {
			continue
		}
		above := pos.Side(cube.FaceUp)
		_, liquid := tx.Liquid(above)
		nothingAbove := len(tx.Block(above).Model().BBox(above, tx)) == 0 && !liquid
		vel := e.data.Vel
		switch {
		case nothingAbove && c.DragDown:
			vel[1] = math.Max(-0.9, vel[1]-0.03)
		case nothingAbove:
			vel[1] = math.Min(1.8, vel[1]+0.1)
		case c.DragDown:
			vel[1] = math.Max(-0.3, vel[1]-0.03)
		default:
			vel[1] = math.Min(0.7, vel[1]+0.06)
		}
		e.data.Vel = vel
	}
}

// javaBlockSpeedFactor is Entity.getBlockSpeedFactor: the speed factor of the block the entity is in, or of the block
// under it if that one has none (soul sand and honey: 0.4); water and bubble columns keep their own.
func javaBlockSpeedFactor(tx *world.Tx, pos mgl64.Vec3) float32 {
	b := tx.Block(cube.PosFromVec3(pos))
	switch b.(type) {
	case block.Water, block.BubbleColumn:
		return 1
	}
	if f := blockSpeedFactor(b); f != 1 {
		return f
	}
	return blockSpeedFactor(tx.Block(cube.PosFromVec3(pos.Sub(mgl64.Vec3{0, 0.500001, 0}))))
}

func blockSpeedFactor(b world.Block) float32 {
	if _, ok := b.(block.SoulSand); ok {
		return 0.4
	}
	if name, _ := b.EncodeBlock(); name == "minecraft:honey_block" {
		return 0.4
	}
	return 1
}

// javaBlockFriction is Block.getFriction of the block under the entity (getBlockPosBelowThatAffectsMyMovement): 0.6
// unless the block says otherwise (ice, slime).
func javaBlockFriction(tx *world.Tx, pos mgl64.Vec3) float32 {
	if f, ok := tx.Block(cube.PosFromVec3(pos.Sub(mgl64.Vec3{0, 0.500001, 0}))).(interface{ Friction() float64 }); ok {
		return float32(f.Friction())
	}
	return 0.6
}

// javaMergable is ItemEntity.isMergable: picked up some time (not never), not about to despawn, not a full stack.
func (i *ItemBehaviour) javaMergable() bool {
	return i.pickupDelay < math.MaxInt16*(time.Second/20) && i.java.age < 6000 && i.i.Count() < i.i.MaxCount()
}

// javaMergeWithNeighbours is ItemEntity.mergeWithNeighbours: items of the same kind within half a block sideways
// merge, the smaller stack into the larger one, which keeps the longer pickup delay and the lower age.
func (i *ItemBehaviour) javaMergeWithNeighbours(e *Ent, tx *world.Tx) {
	box := e.H().Type().BBox(e).Translate(e.data.Pos).GrowVec3(mgl64.Vec3{0.5, 0, 0.5})
	for other := range tx.EntitiesWithin(box) {
		o, ok := other.(*Ent)
		if !ok || o.H() == e.H() || o.H().Type() != ItemType || !o.H().Type().BBox(o).Translate(o.Position()).IntersectsWith(box) {
			continue
		}
		ob, ok := o.Behaviour().(*ItemBehaviour)
		if !ok || !ob.javaMergable() {
			continue
		}
		if ob.i.Count()+i.i.Count() > ob.i.MaxCount() || !i.i.Comparable(ob.i) {
			continue
		}
		if ob.i.Count() < i.i.Count() {
			javaMergeItems(e, i, o, ob, tx)
		} else {
			javaMergeItems(o, ob, e, i, tx)
			return
		}
	}
}

// javaMergeItems moves the stack of from into to.
func javaMergeItems(to *Ent, tb *ItemBehaviour, from *Ent, fb *ItemBehaviour, tx *world.Tx) {
	tb.i = tb.i.Grow(fb.i.Count())
	tb.pickupDelay = max(tb.pickupDelay, fb.pickupDelay)
	tb.java.age = min(tb.java.age, fb.java.age)
	_ = from.Close()
	for _, v := range tx.Viewers(to.data.Pos) {
		// Item entities show their stack only when they are spawned.
		v.HideEntity(to)
		v.ViewEntity(to)
	}
}

// insideCollision reports whether a block's collision box overlaps box (Level.noCollision, negated).
func insideCollision(tx *world.Tx, box cube.BBox) bool {
	for _, b := range blockBBoxsAround(tx, box) {
		if b.IntersectsWith(box) {
			return true
		}
	}
	return false
}

// moveTowardsClosestSpace is Entity.moveTowardsClosestSpace: of the sides north, south, west, east and up of the
// block at at whose neighbour is not a full collision cube, the closest to at gets the entity's velocity on its
// axis set to a random 0.1-0.3 towards it; the other axes keep 0.75 of theirs.
func moveTowardsClosestSpace(e *Ent, tx *world.Tx, at mgl64.Vec3) {
	pos := cube.PosFromVec3(at)
	delta := at.Sub(pos.Vec3())
	closestFace, closest := cube.FaceUp, math.MaxFloat64
	for _, f := range []cube.Face{cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast, cube.FaceUp} {
		n := pos.Side(f)
		if fullCollisionCube(tx, n) {
			continue
		}
		d := delta[vecIndex(f.Axis())]
		if facePositive(f) {
			d = 1 - d
		}
		if d < closest {
			closest, closestFace = d, f
		}
	}
	speed := float64(rand.Float32()*0.2 + 0.1)
	if !facePositive(closestFace) {
		speed = -speed
	}
	vel := e.data.Vel.Mul(0.75)
	vel[vecIndex(closestFace.Axis())] = speed
	e.data.Vel = vel
}

// fullCollisionCube reports whether the block at pos collides as one full cube (isCollisionShapeFullBlock).
func fullCollisionCube(tx *world.Tx, pos cube.Pos) bool {
	boxes := tx.Block(pos).Model().BBox(pos, tx)
	return len(boxes) == 1 && boxes[0] == cube.Box(0, 0, 0, 1, 1, 1)
}

// facePositive reports whether f points along a positive axis (up, south, east).
func facePositive(f cube.Face) bool {
	return f == cube.FaceUp || f == cube.FaceSouth || f == cube.FaceEast
}

// vecIndex is the index of axis a in an mgl64.Vec3.
func vecIndex(a cube.Axis) int {
	switch a {
	case cube.X:
		return 0
	case cube.Y:
		return 1
	}
	return 2
}
