//go:build crossfly_full

package nav

import (
	"math"
	"reflect"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// This file maps Dragonfly blocks onto the Bedrock block facts the pathfinder reads: the
// collision shape (Block::getCollisionShape, one AABB, here the union of the model's boxes), the
// material type, BlockProperty bits and a few BlockLegacy flags (isDoor at +360, isTrapdoor at +359).

// matKind is the subset of MaterialType the pathfinder distinguishes.
type matKind uint8

const (
	matOther matKind = iota
	matAir
	matWater
	matLava
	matFire
	matPortal
	matMetal
)

// prop is the subset of BlockProperty (LeviLamina BlockProperty.h) the pathfinder reads.
type prop uint16

const (
	propStair           prop = 1 << iota // Stair (1<<0)
	propHalfSlab                         // HalfSlab (1<<1)
	propFenceGate                        // FenceGate (1<<4)
	propConnects2D                       // Connects2D (1<<7): fences and walls
	propCarpet                           // Carpet (1<<8)
	propDoor                             // Door (1<<10)
	propTrap                             // Trap (1<<14): trapdoors
	propWalkable                         // Walkable (1<<16): chests, dried kelp blocks
	propCausesDamage                     // CausesDamage (1<<29): cactus, sweet berry bush, magma
	propUnwalkable                       // Unwalkable (1<<38): cactus, sweet berry bush
	propPreventsJumping                  // PreventsJumping (1<<43): honey block
	propHollow                           // Hollow (1<<40): cauldron
)

// staticInfo is the part of blockInfo that only depends on the block state. It is computed once
// per state and shared by all searches.
type staticInfo struct {
	name  string
	air   bool
	mat   matKind // material of the block itself
	props prop

	isDoor, isTrapdoor bool
	open               bool
	thinAxis           cube.Axis      // doors/trapdoors: the axis the panel blocks
	panel              cube.Direction // doors: the side of the cell the panel sits on

	slabBottom  bool
	magma       bool
	campfire    bool
	campfireLit bool
	honey       bool
	powderSnow  bool
	scaffolding bool
	ladder      bool
	berryGrown  bool

	// dynamic is set when the model's boxes depend on the position or the neighbours (fences,
	// panes, bamboo, models from other packages). Otherwise local holds the shape at the origin.
	dynamic  bool
	local    cube.BBox
	hasLocal bool
}

// blockInfo is everything the pathfinder wants to know about one position. It is computed once per
// search and cached.
type blockInfo struct {
	*staticInfo
	b      world.Block
	shape  cube.BBox // world space; only valid when has
	has    bool
	liquid matKind // liquid at the position, in either layer (getLiquidBlock)
	// solid approximates Material::isSolid/blocksMotion: the block has collision and is not a ladder.
	solid        bool
	waterSurface float64 // visual top of water at this position, relative to the block
}

var statics sync.Map // world.BlockHash -> *staticInfo

func staticFor(b world.Block) *staticInfo {
	h := world.BlockHash(b)
	if v, ok := statics.Load(h); ok {
		return v.(*staticInfo)
	}
	s := computeStatic(b)
	statics.Store(h, s)
	return s
}

var modelPkg = reflect.TypeOf(model.Solid{}).PkgPath()

func computeStatic(b world.Block) *staticInfo {
	si := &staticInfo{}
	name, states := b.EncodeBlock()
	si.name = name

	switch v := b.(type) {
	case block.Air:
		si.air, si.mat = true, matAir
	case block.Water:
		si.mat = matWater
	case block.Lava:
		si.mat = matLava
	case block.Fire:
		si.mat = matFire
	case block.Portal, block.EndPortal:
		si.mat = matPortal
	case block.IronDoor, block.CopperDoor:
		si.mat = matMetal
	case block.Magma:
		si.magma = true
		si.props |= propCausesDamage
	case block.Cactus:
		si.props |= propCausesDamage | propUnwalkable
	case block.Campfire:
		si.campfire, si.campfireLit = true, !v.Extinguished
	case block.Chest, block.DriedKelp:
		si.props |= propWalkable
	case block.Cauldron:
		si.props |= propHollow
	case block.Ladder:
		si.ladder = true
	}
	switch name {
	case "minecraft:sweet_berry_bush":
		si.props |= propCausesDamage | propUnwalkable
		if g, ok := states["growth"].(int32); ok && g >= 1 {
			si.berryGrown = true
		}
	case "minecraft:honey_block":
		si.honey = true
		si.props |= propPreventsJumping
	case "minecraft:powder_snow":
		si.powderSnow = true
	case "minecraft:scaffolding":
		si.scaffolding = true
	case "minecraft:iron_trapdoor":
		si.mat = matMetal
	}

	m := b.Model()
	switch mv := m.(type) {
	case model.Stair:
		si.props |= propStair
	case model.Slab:
		if !mv.Double {
			si.props |= propHalfSlab
			si.slabBottom = !mv.Top
		}
	case model.FenceGate:
		si.props |= propFenceGate
		si.open = mv.Open
	case model.Fence:
		si.props |= propConnects2D
		si.dynamic = true
	case model.Wall:
		si.props |= propConnects2D
	case model.Carpet:
		si.props |= propCarpet
	case model.Door:
		si.props |= propDoor
		si.isDoor, si.open = true, mv.Open
	case model.Trapdoor:
		si.props |= propTrap
		si.isTrapdoor, si.open = true, mv.Open
	case model.Thin, model.Bamboo:
		si.dynamic = true
	}
	if t := reflect.TypeOf(m); t == nil || t.PkgPath() != modelPkg {
		si.dynamic = true
	}
	if !si.dynamic {
		si.local, si.hasLocal = unionBoxes(m.BBox(cube.Pos{}, emptySource{}))
	}
	if si.hasLocal && (si.isDoor || si.isTrapdoor) {
		local := si.local
		if local.Width() < local.Length() {
			si.thinAxis = cube.X
			if local.Min().X() > 0.5 {
				si.panel = cube.East
			} else {
				si.panel = cube.West
			}
		} else {
			si.thinAxis = cube.Z
			if local.Min().Z() > 0.5 {
				si.panel = cube.South
			} else {
				si.panel = cube.North
			}
		}
	}
	return si
}

// emptySource is the BlockSource given to position-independent models.
type emptySource struct{}

func (emptySource) Block(cube.Pos) world.Block { return block.Air{} }

func unionBoxes(boxes []cube.BBox) (cube.BBox, bool) {
	if len(boxes) == 0 {
		return cube.BBox{}, false
	}
	u := boxes[0]
	for _, bb := range boxes[1:] {
		u = union(u, bb)
	}
	return u, u.Width() > 0 && u.Height() > 0 && u.Length() > 0
}

func describeBlock(src BlockSource, pos cube.Pos) *blockInfo {
	bi := &blockInfo{}
	describeInto(bi, src, pos)
	return bi
}

func describeInto(bi *blockInfo, src BlockSource, pos cube.Pos) {
	b := src.Block(pos)
	si := staticFor(b)
	bi.staticInfo, bi.b = si, b
	if si.dynamic {
		if u, ok := unionBoxes(b.Model().BBox(pos, src)); ok {
			bi.shape, bi.has = u.Translate(pos.Vec3()), true
		}
	} else if si.hasLocal {
		bi.shape, bi.has = si.local.Translate(pos.Vec3()), true
	}
	switch si.mat {
	case matWater:
		bi.liquid = matWater
		bi.waterSurface = waterSurface(b.(block.Water))
	case matLava:
		bi.liquid = matLava
	default:
		if !si.air {
			if l, ok := src.Liquid(pos); ok {
				switch lv := l.(type) {
				case block.Water:
					bi.liquid = matWater
					bi.waterSurface = waterSurface(lv)
				case block.Lava:
					bi.liquid = matLava
				}
			}
		}
	}
	bi.solid = bi.has && !si.ladder && si.mat != matWater && si.mat != matLava
}

func waterSurface(w block.Water) float64 {
	level := 8 - w.Depth
	if w.Falling || level < 0 {
		level = 0
	}
	return 1 - float64(level+1)/9
}

// waterAt reports whether pos holds water in either layer.
func waterAt(src BlockSource, pos cube.Pos) bool {
	l, ok := src.Liquid(pos)
	if !ok {
		return false
	}
	_, water := l.(block.Water)
	return water
}

func union(a, b cube.BBox) cube.BBox {
	mn, mx := a.Min(), a.Max()
	bn, bx := b.Min(), b.Max()
	return cube.Box(
		math.Min(mn[0], bn[0]), math.Min(mn[1], bn[1]), math.Min(mn[2], bn[2]),
		math.Max(mx[0], bx[0]), math.Max(mx[1], bx[1]), math.Max(mx[2], bx[2]),
	)
}

// partial is Block::isPartialBlock: a collision shape lower than a full block.
func (bi *blockInfo) partial() bool {
	return bi.has && math.Abs(bi.shape.Max().Y()-bi.shape.Min().Y()) < 0.99999988
}

// topPartial is Block::isTopPartialBlock: a partial shape that reaches the top of its cell.
func (bi *blockInfo) topPartial(pos cube.Pos) bool {
	return bi.partial() && math.Abs(bi.shape.Max().Y()-float64(pos.Y())) > 0.99999988
}

// moveAxis is the axis of Direction::getDirection(dx, dz).
func moveAxis(dx, dz int) cube.Axis {
	if abs(dx) > abs(dz) {
		return cube.X
	}
	return cube.Z
}

// direction is Direction::getDirection(dx, dz).
func direction(dx, dz int) cube.Direction {
	if abs(dx) > abs(dz) {
		if dx > 0 {
			return cube.East
		}
		return cube.West
	}
	if dz > 0 {
		return cube.South
	}
	return cube.North
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func floor(f float64) int { return int(math.Floor(f)) }

func ceil(f float64) int { return int(math.Ceil(f)) }

func distSq(a, b mgl64.Vec3) float64 { return a.Sub(b).LenSqr() }

func up(p cube.Pos) cube.Pos   { return cube.Pos{p[0], p[1] + 1, p[2]} }
func down(p cube.Pos) cube.Pos { return cube.Pos{p[0], p[1] - 1, p[2]} }
