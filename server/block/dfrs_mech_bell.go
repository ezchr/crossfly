//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/gameevent"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Bell is a block that rings when it is used, hit by a projectile or starts to be powered. It stands on the floor,
// hangs from the ceiling, or hangs from one wall or between two.
//
// With redstone=dragonfly it follows Bedrock Edition (BellBlock, BellBlockActor in BDS): it rings once each time
// the power the redstone engine gives it turns on. With Java redstone it rings when a neighbour update finds it
// powered while it was not (Java Edition's BellBlock.neighborChanged).
type Bell struct {
	transparent
	sourceWaterDisplacer

	// Attach is how the bell hangs.
	Attach BellAttachment
	// Facing is the direction the bell faces: the way its frame opens for a standing bell, away from the wall for
	// one on a wall.
	Facing cube.Direction
	// Powered is true while the bell is powered (toggle_bit).
	Powered bool
}

// BellAttachment is the way a bell hangs.
type BellAttachment uint8

// The ways a bell may hang.
const (
	BellStanding BellAttachment = iota
	BellHanging
	BellSide
	BellMultiple
)

func (a BellAttachment) String() string {
	switch a {
	case BellHanging:
		return "hanging"
	case BellSide:
		return "side"
	case BellMultiple:
		return "multiple"
	}
	return "standing"
}

var _ world.RedstonePowerContextAction = Bell{}

// Model ...
func (b Bell) Model() world.BlockModel { return bellModel{b} }

// UseOnBlock places the bell on the floor, the ceiling or a wall, by the face clicked.
func (b Bell) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, face, used := firstReplaceable(tx, pos, face, b)
	if !used {
		return false
	}
	b.Powered = false
	switch face {
	case cube.FaceUp:
		b.Attach, b.Facing = BellStanding, user.Rotation().Direction().Opposite()
	case cube.FaceDown:
		b.Attach, b.Facing = BellHanging, user.Rotation().Direction().Opposite()
	default:
		b.Attach, b.Facing = BellSide, face.Direction()
		if bellSupport(tx, pos, face) {
			b.Attach = BellMultiple
		}
	}
	if !b.supported(pos, tx) {
		return false
	}
	place(tx, pos, b, user, ctx)
	return placed(ctx)
}

// bellSupport reports whether the block on the face side of pos holds a bell up on that side.
func bellSupport(tx *world.Tx, pos cube.Pos, face cube.Face) bool {
	at := pos.Side(face)
	if at.OutOfBounds(tx.Range()) {
		return false
	}
	return tx.Block(at).Model().FaceSolid(at, face.Opposite(), tx)
}

// supported reports whether the bell still has what it hangs from.
func (b Bell) supported(pos cube.Pos, tx *world.Tx) bool {
	switch b.Attach {
	case BellStanding:
		return bellSupport(tx, pos, cube.FaceDown)
	case BellHanging:
		return bellSupport(tx, pos, cube.FaceUp)
	case BellSide:
		return bellSupport(tx, pos, b.Facing.Face().Opposite())
	}
	return bellSupport(tx, pos, b.Facing.Face()) || bellSupport(tx, pos, b.Facing.Face().Opposite())
}

// NeighbourUpdateTick drops the bell when what it hangs from is gone, and switches a wall bell between one wall and
// two. With Java redstone it also rings when it starts to be powered.
func (b Bell) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !b.supported(pos, tx) {
		breakBlock(b, pos, tx)
		return
	}
	if b.Attach == BellSide || b.Attach == BellMultiple {
		two := bellSupport(tx, pos, b.Facing.Face()) && bellSupport(tx, pos, b.Facing.Face().Opposite())
		attach := BellSide
		if two {
			attach = BellMultiple
		} else if !bellSupport(tx, pos, b.Facing.Face().Opposite()) {
			// Only the wall in front is left: the bell hangs from that one now.
			b.Facing = b.Facing.Opposite()
		}
		if attach != b.Attach {
			b.Attach = attach
			tx.SetBlock(pos, b, &world.SetOpts{DisableBlockUpdates: true, KeepRedstonePower: true})
		}
	}
	if world.JavaRedstone() {
		if powered := railNeighbourSignal(tx, pos); powered != b.Powered {
			if powered {
				b.ring(pos, tx, b.Facing.Face())
			}
			b.Powered = powered
			tx.SetBlock(pos, b, &world.SetOpts{DisableBlockUpdates: true, KeepRedstonePower: true})
		}
	}
}

// RedstonePowerActionUpdate rings the bell when its power turns on, with redstone=dragonfly.
func (b Bell) RedstonePowerActionUpdate(pos cube.Pos, tx *world.Tx, u world.RedstoneUpdate) {
	if world.JavaRedstone() {
		return
	}
	cur, ok := tx.Block(pos).(Bell)
	if !ok {
		return
	}
	powered := u.NewPower > 0
	if powered == cur.Powered {
		return
	}
	if powered {
		cur.ring(pos, tx, cur.Facing.Face())
	}
	cur.Powered = powered
	tx.SetBlock(pos, cur, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true, KeepRedstonePower: true})
}

// Activate rings the bell towards where the user looks.
func (b Bell) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, _ *item.UseContext) bool {
	b.ring(pos, tx, u.Rotation().Direction().Face())
	return true
}

// ProjectileHit rings the bell away from the side hit.
func (b Bell) ProjectileHit(pos cube.Pos, tx *world.Tx, _ world.Entity, face cube.Face) {
	b.ring(pos, tx, face.Opposite())
}

// ring swings the bell towards face for its viewers and sounds it (BellBlockActor::ring).
func (b Bell) ring(pos cube.Pos, tx *world.Tx, face cube.Face) {
	if face == cube.FaceUp || face == cube.FaceDown {
		face = b.Facing.Face()
	}
	action := BellRingAction{Face: face, Direction: int32(horizontalDirection(face.Direction()))}
	for _, v := range tx.Viewers(pos.Vec3Centre()) {
		v.ViewBlockAction(pos, action)
	}
	tx.PlaySound(pos.Vec3Centre(), sound.BellRing{})
	postBlockEvent(tx, gameevent.BlockChange, pos, nil)
	if BellRingHook != nil {
		BellRingHook(tx, pos, face)
	}
}

// BellRingHook, if set, is told when a bell rings, with the side it swings towards. Crossfly Full sets it with
// mobs=java (villagers hear the bell, raiders glow); nil otherwise.
var BellRingHook func(tx *world.Tx, pos cube.Pos, face cube.Face)

// BreakInfo ...
func (b Bell) BreakInfo() BreakInfo {
	return newBreakInfo(5, pickaxeHarvestable, pickaxeEffective, oneOf(Bell{})).withBlastResistance(25)
}

// EncodeItem ...
func (Bell) EncodeItem() (name string, meta int16) { return "minecraft:bell", 0 }

// EncodeBlock ...
func (b Bell) EncodeBlock() (string, map[string]any) {
	return "minecraft:bell", map[string]any{
		"attachment": b.Attach.String(),
		"direction":  int32(horizontalDirection(b.Facing)),
		"toggle_bit": b.Powered,
	}
}

// EncodeNBT gives the bell its block entity, which Bedrock clients swing.
func (Bell) EncodeNBT() map[string]any {
	return map[string]any{"id": "Bell", "Ringing": uint8(0), "Ticks": int32(0), "Direction": int32(0)}
}

// DecodeNBT ...
func (b Bell) DecodeNBT(map[string]any) any { return b }

func allBells() (bells []world.Block) {
	for a := BellStanding; a <= BellMultiple; a++ {
		for _, d := range cube.Directions() {
			bells = append(bells, Bell{Attach: a, Facing: d}, Bell{Attach: a, Facing: d, Powered: true})
		}
	}
	return
}

func init() {
	registerAll(allBells())
	world.RegisterItem(Bell{})
}

// bellModel is the shape of a bell: the bell and the frame or bar it hangs from.
type bellModel struct{ b Bell }

// BBox ...
func (m bellModel) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	body := cube.Box(0.25, 0.25, 0.25, 0.75, 0.8125, 0.75)
	switch m.b.Attach {
	case BellStanding:
		if m.b.Facing == cube.North || m.b.Facing == cube.South {
			return []cube.BBox{cube.Box(0, 0, 0.25, 1, 1, 0.75)}
		}
		return []cube.BBox{cube.Box(0.25, 0, 0, 0.75, 1, 1)}
	case BellHanging:
		return []cube.BBox{body, cube.Box(0.4375, 0.8125, 0.4375, 0.5625, 1, 0.5625)}
	}
	return []cube.BBox{body}
}

// FaceSolid ...
func (bellModel) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool { return false }
