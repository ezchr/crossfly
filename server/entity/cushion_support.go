package entity

import (
	"math"
	"slices"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

const (
	// cushionAnchorDepth is how far below a cushion the block it rests on is looked for (Cushion.hasAnchorBelow).
	cushionAnchorDepth = 1.0 / 64
	// cushionDeflate is how much vanilla shrinks boxes so that touching blocks do not count (AABB.nextDeflated).
	cushionDeflate = 1e-7
)

// cushionPlacement returns where a cushion used on the face of a block at the point clicked (relative to the
// block) goes, as vanilla's CushionItem: On the top face only, centred on the block at the height clicked, and
// only where it is supported and not buried.
func cushionPlacement(pos cube.Pos, face cube.Face, clickPos mgl64.Vec3, tx *world.Tx) (mgl64.Vec3, bool) {
	b := tx.Block(pos)
	if cushionUsesCollisionShape(b) {
		face = cushionCollisionFace(b, face, clickPos)
	}
	if face != cube.FaceUp {
		return mgl64.Vec3{}, false
	}
	y := clickPos[1]
	if _, maxY, ok := cushionOutlineY(b, pos, tx); ok && y > maxY {
		// Bedrock clients click on Bedrock's boxes, some of which are taller than the Java outline the cushion
		// rests on (chests, lecterns): Rest on the Java surface instead of above it. A Java client never clicks
		// above its outline.
		y = maxY
	}
	spawn := mgl64.Vec3{float64(pos[0]) + 0.5, float64(pos[1]) + y, float64(pos[2]) + 0.5}
	return spawn, cushionCanSurvive(spawn, tx) && !cushionBuried(spawn, tx)
}

// cushionUsesCollisionShape reports if a block is in vanilla's cushion_uses_collision_shape tag: Cushions used
// on it go by its collision shape rather than its outline.
func cushionUsesCollisionShape(b world.Block) bool {
	switch b.(type) {
	case block.Cauldron, block.Hopper, block.Composter:
		return true
	}
	return false
}

// cushionCollisionFace returns the face of the collision shape of a block that lies at the point clicked, or the
// face clicked if the collision shape is not there, like vanilla's recalculateContextForSpecialCollisionShapes.
// The outline of these blocks is more than their collision shape, and a Java client reports the inner walls of a
// cauldron as its top face: The collision shape tells them apart.
func cushionCollisionFace(b world.Block, face cube.Face, clickPos mgl64.Vec3) cube.Face {
	const eps = 1e-4
	found, foundFace := false, face
	for _, box := range cushionTaggedCollision(b) {
		lo, hi := box.Min(), box.Max()
		inside := func(axis int) bool { return clickPos[axis] >= lo[axis]-eps && clickPos[axis] <= hi[axis]+eps }
		if !inside(0) || !inside(1) || !inside(2) {
			continue
		}
		switch {
		case math.Abs(clickPos[1]-hi[1]) <= eps:
			return cube.FaceUp
		case math.Abs(clickPos[1]-lo[1]) <= eps:
			found, foundFace = true, cube.FaceDown
		case math.Abs(clickPos[0]-lo[0]) <= eps:
			found, foundFace = true, cube.FaceWest
		case math.Abs(clickPos[0]-hi[0]) <= eps:
			found, foundFace = true, cube.FaceEast
		case math.Abs(clickPos[2]-lo[2]) <= eps:
			found, foundFace = true, cube.FaceNorth
		case math.Abs(clickPos[2]-hi[2]) <= eps:
			found, foundFace = true, cube.FaceSouth
		}
	}
	if found {
		return foundFace
	}
	return face
}

// cushionTaggedCollision returns the vanilla collision shape of a cauldron, hopper or composter: A floor and walls
// two pixels thick.
func cushionTaggedCollision(b world.Block) []cube.BBox {
	floor := 0.25
	switch b.(type) {
	case block.Hopper:
		floor = 0.6875
	case block.Composter:
		floor = 0.125
	}
	return []cube.BBox{
		cube.Box(0, 0, 0, 1, floor, 1),
		cube.Box(0, 0, 0, 0.125, 1, 1), cube.Box(0.875, 0, 0, 1, 1, 1),
		cube.Box(0, 0, 0, 1, 1, 0.125), cube.Box(0, 0, 0.875, 1, 1, 1),
	}
}

// cushionBox returns the box of a cushion at pos as vanilla has it: One block wide and a quarter high.
func cushionBox(pos mgl64.Vec3) cube.BBox {
	return cube.Box(pos[0]-0.5, pos[1], pos[2]-0.5, pos[0]+0.5, pos[1]+0.25, pos[2]+0.5)
}

// cushionCanSurvive reports if a cushion rests on a block and is not fully covered by suffocating blocks
// (Cushion.wouldSurviveAt).
func cushionCanSurvive(pos mgl64.Vec3, tx *world.Tx) bool {
	return cushionAnchored(pos, tx) && !cushionCovered(pos, tx)
}

// cushionAnchored reports if the outline of a block reaches into the 1/64 thick slice right below the cushion
// (Cushion.hasAnchorBelow). Like vanilla, it goes by the bounds of the outline: The outline players click on.
func cushionAnchored(pos mgl64.Vec3, tx *world.Tx) bool {
	box := cushionBox(pos)
	lo, hi := box.Min(), box.Max()
	anchor := cube.Box(lo[0], lo[1]-cushionAnchorDepth, lo[2], math.Nextafter(hi[0], math.Inf(-1)), lo[1], math.Nextafter(hi[2], math.Inf(-1)))
	alo, ahi := anchor.Min(), anchor.Max()
	from := cube.PosFromVec3(mgl64.Vec3{alo[0], alo[1] - 0.125, alo[2]})
	for p := range cube.Range3D(from, cube.PosFromVec3(ahi)) {
		b := tx.Block(p)
		bounds, ok := cushionBounds(cushionOutline(b, p, tx))
		if ok && cushionIntersects(bounds.Translate(p.Vec3()), anchor) {
			return true
		}
	}
	return false
}

// cushionCovered reports if every block the cushion is in suffocates (Cushion.isCoveredBySuffocatingBlocks).
func cushionCovered(pos mgl64.Vec3, tx *world.Tx) bool {
	box := cushionBox(pos).Grow(-cushionDeflate)
	for p := range cube.Range3D(cube.PosFromVec3(box.Min()), cube.PosFromVec3(box.Max())) {
		if !cushionCovers(tx.Block(p), p, tx) {
			return false
		}
	}
	return true
}

// cushionBuried reports if the collision boxes of blocks fill the 1/64 thick slice at the bottom of a cushion, so
// that it would have nothing to rest on (Cushion.isAnchorBuried). Only placing a cushion checks this.
func cushionBuried(pos mgl64.Vec3, tx *world.Tx) bool {
	box := cushionBox(pos)
	lo, hi := box.Min(), box.Max()
	slice := cube.Box(lo[0], lo[1], lo[2], hi[0], lo[1]+cushionAnchorDepth, hi[2]).Grow(-cushionDeflate)
	var boxes []cube.BBox
	for p := range cube.Range3D(cube.PosFromVec3(slice.Min()), cube.PosFromVec3(slice.Max())) {
		for _, bb := range tx.Block(p).Model().BBox(p, tx) {
			if bb = bb.Translate(p.Vec3()); cushionIntersects(bb, slice) {
				boxes = append(boxes, bb)
			}
		}
	}
	return cushionFilled(slice, boxes)
}

// cushionFilled reports if the boxes passed together fill all of area.
func cushionFilled(area cube.BBox, boxes []cube.BBox) bool {
	if len(boxes) == 0 {
		return false
	}
	alo, ahi := area.Min(), area.Max()
	var cuts [3][]float64
	for axis := range 3 {
		cuts[axis] = []float64{alo[axis], ahi[axis]}
		for _, bb := range boxes {
			for _, v := range []float64{bb.Min()[axis], bb.Max()[axis]} {
				if v > alo[axis] && v < ahi[axis] {
					cuts[axis] = append(cuts[axis], v)
				}
			}
		}
		slices.Sort(cuts[axis])
		cuts[axis] = slices.Compact(cuts[axis])
	}
	for i := 1; i < len(cuts[0]); i++ {
		for j := 1; j < len(cuts[1]); j++ {
			for k := 1; k < len(cuts[2]); k++ {
				mid := mgl64.Vec3{(cuts[0][i-1] + cuts[0][i]) / 2, (cuts[1][j-1] + cuts[1][j]) / 2, (cuts[2][k-1] + cuts[2][k]) / 2}
				if !slices.ContainsFunc(boxes, func(bb cube.BBox) bool { return bb.Vec3Within(mid) }) {
					return false
				}
			}
		}
	}
	return true
}

// cushionIntersects reports if two boxes overlap, not counting boxes that only touch (AABB.intersects).
func cushionIntersects(a, b cube.BBox) bool {
	alo, ahi, blo, bhi := a.Min(), a.Max(), b.Min(), b.Max()
	return alo[0] < bhi[0] && ahi[0] > blo[0] && alo[1] < bhi[1] && ahi[1] > blo[1] && alo[2] < bhi[2] && ahi[2] > blo[2]
}

// cushionBounds returns the box around all boxes passed, or false if there are none.
func cushionBounds(boxes []cube.BBox) (cube.BBox, bool) {
	if len(boxes) == 0 {
		return cube.BBox{}, false
	}
	bounds := boxes[0]
	for _, bb := range boxes[1:] {
		bounds = cube.Box(
			min(bounds.Min()[0], bb.Min()[0]), min(bounds.Min()[1], bb.Min()[1]), min(bounds.Min()[2], bb.Min()[2]),
			max(bounds.Max()[0], bb.Max()[0]), max(bounds.Max()[1], bb.Max()[1]), max(bounds.Max()[2], bb.Max()[2]),
		)
	}
	return bounds, true
}

// cushionOutlineY returns the lowest and highest point of the vanilla outline of a block, or false if it has none.
func cushionOutlineY(b world.Block, pos cube.Pos, tx *world.Tx) (minY, maxY float64, ok bool) {
	bounds, ok := cushionBounds(cushionOutline(b, pos, tx))
	return bounds.Min()[1], bounds.Max()[1], ok
}

// cushionOutline returns the vanilla outline of a block, the shape players click on, as far as cushions care: How
// high it reaches. It is the Dragonfly model where that is as high as the Java 26.3 outline, and the Java outline
// for blocks without a collision box or with one of another height (fences, chests, lanterns and so on).
func cushionOutline(b world.Block, pos cube.Pos, tx *world.Tx) []cube.BBox {
	switch b := b.(type) {
	case block.SnowLayer:
		// The layers players see: One layer has no collision box at all.
		return cushionShape(0, float64(b.Height+1)/8)
	case block.WoodFence, block.NetherBrickFence:
		return cushionShape(0, 1)
	case block.WoodFenceGate:
		if b.Lowered {
			return cushionShape(0, 0.8125)
		}
		return cushionShape(0, 1)
	case block.Wall:
		return cushionWallShape(b)
	case block.Chest, block.EnderChest, block.CopperGolemStatue, block.BrewingStand:
		return cushionShape(0, 0.875)
	case block.Cake:
		if b.Candle {
			return cushionShape(0, 0.875)
		}
	case block.Lantern:
		return cushionLanternShape(b.Hanging)
	case block.CopperLantern:
		return cushionLanternShape(b.Hanging)
	case block.EndPortalFrame:
		if b.Eye {
			return cushionShape(0, 1)
		}
		return cushionShape(0, 0.8125)
	case block.Hopper:
		if b.Facing.Axis() != cube.Y {
			return cushionShape(0.25, 1)
		}
		return cushionShape(0, 1)
	case block.Lectern:
		return cushionShape(0, 1.125)
	case block.Grindstone:
		if b.Attach == block.WallGrindstoneAttachment() {
			return cushionShape(0.125, 0.875)
		}
		return cushionShape(0, 1)
	case block.StrawBed:
		if b.Head {
			return cushionShape(0, 0.3125)
		}
		return cushionShape(0, 0.25)
	case block.Skull:
		if !cushionStanding(b.Attach) {
			return cushionShape(0.25, 0.75)
		}
		if b.Type == block.DragonHead() {
			return cushionShape(0, 0.53125)
		}
		return cushionShape(0, 0.5)
	case block.ShelfMushroom:
		if b.Large {
			return cushionShape(0.375, 0.6875)
		}
		return cushionShape(0.5, 0.6875)
	case block.LilyPad:
		return cushionShape(0, 0.09375)
	case block.Cobweb, block.Portal, block.DoubleFlower, block.DoubleTallGrass, block.SugarCane, block.Vines:
		return cushionShape(0, 1)
	case block.Flower:
		return cushionShape(0, 0.625)
	case block.ShortGrass, block.Fern, block.DeadBush, block.RedShrub:
		return cushionShape(0, 0.8125)
	case block.Sapling, block.BambooSapling:
		return cushionShape(0, 0.75)
	case block.Coral:
		return cushionShape(0, 0.9375)
	case block.Kelp:
		if _, below := tx.Block(pos.Side(cube.FaceUp)).(block.Kelp); below {
			return cushionShape(0, 1)
		}
		return cushionShape(0, 0.5625)
	case block.NetherSprouts, block.PinkPetals:
		return cushionShape(0, 0.1875)
	case block.SporeBlossom:
		return cushionShape(0.8125, 1)
	case block.SeaPickle:
		if b.AdditionalCount >= 3 {
			return cushionShape(0, 0.4375)
		}
		return cushionShape(0, 0.375)
	case block.Torch:
		return cushionTorchShape(b.Facing)
	case block.CopperTorch:
		return cushionTorchShape(b.Facing)
	case block.RedstoneTorch:
		return cushionTorchShape(b.Facing)
	case block.Lever:
		switch b.Facing {
		case cube.FaceUp:
			return cushionShape(0, 0.375)
		case cube.FaceDown:
			return cushionShape(0.625, 1)
		}
		return cushionShape(0.25, 0.75)
	case block.RedstoneWire:
		return cushionShape(0, 0.0625)
	case block.String:
		if b.Attached {
			return cushionShape(0.0625, 0.15625)
		}
		return cushionShape(0, 0.5)
	case block.EndPortal:
		return cushionShape(0.375, 0.75)
	case block.Sign:
		if cushionStanding(b.Attach) {
			return cushionShape(0, 1)
		}
		return cushionShape(0.28125, 0.78125)
	case block.Banner:
		if cushionStanding(b.Attach) {
			return cushionShape(0, 1)
		}
		return cushionShape(0, 0.78125)
	case block.ItemFrame:
		switch b.Facing {
		case cube.FaceDown:
			return cushionShape(0, 0.0625)
		case cube.FaceUp:
			return cushionShape(0.9375, 1)
		}
		return cushionShape(0, 1)
	case block.WheatSeeds:
		return cushionShape(0, float64(min(b.Growth, 7)+1)/8)
	case block.Carrot:
		return cushionShape(0, float64(min(b.Growth, 7)+2)/16)
	case block.Potato:
		return cushionShape(0, float64(min(b.Growth, 7)+2)/16)
	case block.BeetrootSeeds:
		return cushionShape(0, float64(min(b.Growth/2, 3)+1)/8)
	case block.MelonSeeds:
		return cushionStemShape(b.Growth, b.Direction)
	case block.PumpkinSeeds:
		return cushionStemShape(b.Growth, b.Direction)
	case block.NetherWart:
		return cushionShape(0, float64(5+3*min(b.Age, 3))/16)
	}
	if shape, ok := cushionFullOutline(b); ok {
		return shape
	}
	return b.Model().BBox(pos, tx)
}

// cushionShape returns a full width shape between the heights passed.
func cushionShape(minY, maxY float64) []cube.BBox {
	return []cube.BBox{cube.Box(0, minY, 0, 1, maxY, 1)}
}

// cushionTorchShape returns the outline of a torch: Standing up, or on a wall.
func cushionTorchShape(facing cube.Face) []cube.BBox {
	if facing == cube.FaceDown {
		return cushionShape(0, 0.625)
	}
	return cushionShape(0.1875, 0.8125)
}

// cushionLanternShape returns the outline of a lantern, standing or hanging.
func cushionLanternShape(hanging bool) []cube.BBox {
	if hanging {
		return cushionShape(0.0625, 0.625)
	}
	return cushionShape(0, 0.5625)
}

// cushionStemShape returns the outline of a melon or pumpkin stem, which grows two pixels per stage and is ten
// pixels high once a fruit is attached.
func cushionStemShape(growth int, direction cube.Face) []cube.BBox {
	if direction.Axis() != cube.Y {
		return cushionShape(0, 0.625)
	}
	return cushionShape(0, float64(min(growth, 7)+1)/8)
}

// cushionWallShape returns the outline of a wall: As high as its post, or its highest side. A wall with neither has
// none.
func cushionWallShape(w block.Wall) []cube.BBox {
	if w.Post {
		return cushionShape(0, 1)
	}
	height := 0.0
	for _, c := range []block.WallConnectionType{w.NorthConnection, w.EastConnection, w.SouthConnection, w.WestConnection} {
		switch c {
		case block.TallWallConnection():
			height = 1
		case block.ShortWallConnection():
			height = max(height, 0.875)
		}
	}
	if height == 0 {
		return nil
	}
	return cushionShape(0, height)
}

// cushionStanding reports if the attachment is to the ground rather than a wall.
func cushionStanding(a block.Attachment) bool {
	return a.FaceUint8() == 1
}

// cushionCovers reports if a block suffocates, as vanilla's causes_suffocation tag and isSuffocating predicates have
// it: Blocks with a full collision box, except glass, leaves and copper grates, and a few blocks a little lower.
func cushionCovers(b world.Block, pos cube.Pos, tx *world.Tx) bool {
	switch b.(type) {
	case block.Farmland, block.DirtPath, block.Mud, block.SoulSand:
		return true
	case block.Glass, block.StainedGlass, block.TintedGlass, block.Leaves, block.CopperGrate, block.DragonEgg,
		block.CopperGolemStatue:
		return false
	}
	boxes := b.Model().BBox(pos, tx)
	return len(boxes) == 1 && boxes[0] == cube.Box(0, 0, 0, 1, 1, 1)
}
