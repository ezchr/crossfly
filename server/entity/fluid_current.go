//go:build crossfly_full

package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// applyFluidCurrents pushes a non-player entity along the flow of the water and lava it is in, as Java's
// EntityFluidInteraction.update and CurrentAccumulator.applyTo (26.3) do: the flow of every liquid block the
// entity's box (shrunk by 0.001) reaches is summed per liquid, scaled down while the entity is less than 0.4 deep,
// and the normalised sum times 0.014 (water) or 0.0023333 (lava; 0.007 in the Nether) is added to the velocity,
// raised to 0.0045 when the entity barely moves.
func applyFluidCurrents(e *Ent, tx *world.Tx) {
	full := e.H().Type().BBox(e).Translate(e.data.Pos)
	box := full.Grow(-0.001)
	x0, y0, z0 := int(math.Floor(box.Min()[0])), int(math.Floor(box.Min()[1])), int(math.Floor(box.Min()[2]))
	x1, y1, z1 := int(math.Ceil(box.Max()[0]))-1, int(math.Ceil(box.Max()[1]))-1, int(math.Ceil(box.Max()[2]))-1
	entityY := full.Min()[1]

	type acc struct {
		tracker, height float64
		sum             mgl64.Vec3
		count           int
	}
	var water, lava acc
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				pos := cube.Pos{x, y, z}
				l, ok := tx.Liquid(pos)
				if !ok {
					continue
				}
				top := float64(y) + float64(liquidHeight(l, pos, tx))
				if top < box.Min()[1] {
					continue
				}
				a := &water
				if l.LiquidType() == "lava" {
					a = &lava
				} else if l.LiquidType() != "water" {
					continue
				}
				a.tracker = math.Max(top-entityY, a.tracker)
				flow := liquidFlow(l, pos, tx)
				a.height = math.Max(a.tracker, a.height)
				if a.height < 0.4 {
					flow = flow.Mul(a.height)
				}
				a.sum = a.sum.Add(flow)
				a.count++
			}
		}
	}
	lavaScale := 0.0023333333333333335
	if tx.World().Dimension() == world.Nether {
		lavaScale = 0.007
	}
	for _, c := range []struct {
		a     acc
		scale float64
	}{{water, 0.014}, {lava, lavaScale}} {
		if c.a.tracker <= 0 || c.a.count == 0 || c.a.sum.LenSqr() < float64(float32(1.0e-5)) {
			continue
		}
		impulse := c.a.sum.Normalize().Mul(c.scale)
		old := e.data.Vel
		if math.Abs(old[0]) < 0.003 && math.Abs(old[2]) < 0.003 && impulse.Len() < 0.0045000000000000005 {
			impulse = impulse.Normalize().Mul(0.0045000000000000005)
		}
		e.data.Vel = old.Add(impulse)
	}
}

// liquidOwnHeight is Java's FlowingFluid.getOwnHeight: amount/9 (a source or falling liquid is 8/9).
func liquidOwnHeight(l world.Liquid) float32 {
	return float32(l.LiquidDepth()) / 9
}

// liquidHeight is Java's FlowingFluid.getHeight: a full block below the same liquid, its own height otherwise.
func liquidHeight(l world.Liquid, pos cube.Pos, tx *world.Tx) float32 {
	if above, ok := tx.Liquid(pos.Side(cube.FaceUp)); ok && above.LiquidType() == l.LiquidType() {
		return 1
	}
	return liquidOwnHeight(l)
}

// liquidFlow is Java's FlowingFluid.getFlow: the direction the liquid at pos flows in, from the height differences
// with its horizontal neighbours (and the liquid below an empty neighbour), turned down along walls if it falls.
func liquidFlow(l world.Liquid, pos cube.Pos, tx *world.Tx) mgl64.Vec3 {
	own := liquidOwnHeight(l)
	affects := func(p cube.Pos) (world.Liquid, bool) {
		n, ok := tx.Liquid(p)
		if !ok {
			return nil, true
		}
		return n, n.LiquidType() == l.LiquidType()
	}
	var fx, fz float64
	for _, face := range cube.HorizontalFaces() {
		n := pos.Side(face)
		nl, ok := affects(n)
		if !ok {
			continue
		}
		var height, dist float32
		if nl != nil {
			height = liquidOwnHeight(nl)
		}
		if height == 0 {
			if !blocksFluidFlow(tx.Block(n), n, tx) {
				if bl, ok := affects(n.Side(cube.FaceDown)); ok && bl != nil {
					if h := liquidOwnHeight(bl); h > 0 {
						dist = own - (h - 0.8888889)
					}
				}
			}
		} else {
			dist = own - height
		}
		if dist != 0 {
			step := cube.Pos{}.Side(face)
			fx += float64(step[0]) * float64(dist)
			fz += float64(step[2]) * float64(dist)
		}
	}
	flow := javaNormalize(mgl64.Vec3{fx, 0, fz})
	if l.LiquidFalling() {
		for _, face := range cube.HorizontalFaces() {
			n := pos.Side(face)
			if solidFaceForLiquid(l, n, face, tx) || solidFaceForLiquid(l, n.Side(cube.FaceUp), face, tx) {
				flow = flow.Add(mgl64.Vec3{0, -6, 0})
				break
			}
		}
	}
	return javaNormalize(flow)
}

// blocksFluidFlow approximates Java's #blocks_fluid_flow tag (blocks_motion, the old solid materials, and signs)
// with blocks that have a collision box.
func blocksFluidFlow(b world.Block, pos cube.Pos, tx *world.Tx) bool {
	return len(b.Model().BBox(pos, tx)) > 0
}

// solidFaceForLiquid is Java's FlowingFluid.isSolidFace for horizontal directions.
func solidFaceForLiquid(l world.Liquid, pos cube.Pos, face cube.Face, tx *world.Tx) bool {
	if n, ok := tx.Liquid(pos); ok && n.LiquidType() == l.LiquidType() {
		return false
	}
	return tx.Block(pos).Model().FaceSolid(pos, face, tx)
}
