//go:build crossfly_full

package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// CoralFan is a coral fan, standing on a block or hanging on its side (Wall). A living fan dies out of water.
// It follows Java Edition's BaseCoralFanBlock, CoralFanBlock and CoralWallFanBlock: it has no collision, it
// breaks when the block it stands or hangs on goes, and pistons push a dead fan but pop a living one.
type CoralFan struct {
	empty
	transparent
	bassDrum
	sourceWaterDisplacer

	// Type is the type of coral of the fan.
	Type CoralType
	// Dead is whether the fan is dead.
	Dead bool
	// Wall is true for a fan hanging on the side of a block, facing away from it along Facing.
	Wall bool
	// Facing is the direction a wall fan faces: the block it hangs on is behind it.
	Facing cube.Direction
	// Rotation is the turn of a standing fan on Bedrock (coral_fan_direction 0 or 1); Java has none.
	Rotation int
}

// support is the position of the block the fan stands or hangs on, and the face of it that holds the fan.
func (c CoralFan) support(pos cube.Pos) (cube.Pos, cube.Face) {
	if c.Wall {
		return pos.Side(c.Facing.Face().Opposite()), c.Facing.Face()
	}
	return pos.Side(cube.FaceDown), cube.FaceUp
}

// canSurvive is Java's canSurvive: the supporting face is sturdy.
func (c CoralFan) canSurvive(pos cube.Pos, tx *world.Tx) bool {
	at, face := c.support(pos)
	return tx.Block(at).Model().FaceSolid(at, face, tx)
}

// UseOnBlock places a standing fan on a top face and a wall fan on a side face.
func (c CoralFan) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, face, used := firstReplaceable(tx, pos, face, c)
	if !used || face == cube.FaceDown {
		return false
	}
	c.Wall = face != cube.FaceUp
	if c.Wall {
		c.Facing = face.Direction()
	}
	if !c.canSurvive(pos, tx) {
		return false
	}
	place(tx, pos, c, user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick breaks the fan when what holds it is gone (Java's updateShape), and lets a living fan
// out of water die.
func (c CoralFan) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !c.canSurvive(pos, tx) {
		breakBlock(c, pos, tx)
		return
	}
	c.scheduleDie(pos, tx)
}

// BlockStateChanged breaks the fan at once when the block it stands or hangs on changes into one that cannot hold it,
// as Java Edition's updateShape turns it into air (Block.updateOrDestroy destroys it). The neighbour updates of that
// run right away, as in Java, even while a piston places its moving blocks: TNT duplicators rely on it.
func (c CoralFan) BlockStateChanged(pos, changed cube.Pos, tx *world.Tx) {
	if at, _ := c.support(pos); at != changed || c.canSurvive(pos, tx) {
		return
	}
	tx.RunNeighbourUpdatesNow(func() { breakBlock(c, pos, tx) })
}

// BlockPlaced ...
func (c CoralFan) BlockPlaced(pos cube.Pos, _ world.Block, tx *world.Tx) {
	c.scheduleDie(pos, tx)
}

// scheduleDie is Java's tryScheduleDieTick: a living fan with no water at it dies 60 to 99 ticks later.
func (c CoralFan) scheduleDie(pos cube.Pos, tx *world.Tx) {
	if c.Dead || coralFanInWater(pos, tx) {
		return
	}
	tx.ScheduleBlockUpdate(pos, c, time.Duration(60+rand.IntN(40))*time.Second/20)
}

// ScheduledTick kills a living fan that is still out of water.
func (c CoralFan) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if c.Dead || coralFanInWater(pos, tx) {
		return
	}
	c.Dead = true
	tx.SetBlock(pos, c, nil)
}

// coralFanInWater is Java's scanForWater: the fan is waterlogged or touches water.
func coralFanInWater(pos cube.Pos, tx *world.Tx) bool {
	if l, ok := tx.Liquid(pos); ok {
		if _, ok := l.(Water); ok {
			return true
		}
	}
	for _, f := range cube.Faces() {
		if l, ok := tx.Liquid(pos.Side(f)); ok {
			if _, ok := l.(Water); ok {
				return true
			}
		}
	}
	return false
}

// HasLiquidDrops ...
func (CoralFan) HasLiquidDrops() bool { return false }

// SideClosed ...
func (CoralFan) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool { return false }

// BreakInfo ...
func (c CoralFan) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, silkTouchOnlyDrop(CoralFan{Type: c.Type, Dead: c.Dead}))
}

// name is the fan's Bedrock name without the wall part.
func (c CoralFan) name() string {
	n := "minecraft:" + c.Type.String() + "_coral"
	if c.Dead {
		n = "minecraft:dead_" + c.Type.String() + "_coral"
	}
	return n
}

// EncodeItem ...
func (c CoralFan) EncodeItem() (name string, meta int16) {
	return c.name() + "_fan", 0
}

// EncodeBlock ...
func (c CoralFan) EncodeBlock() (string, map[string]any) {
	if c.Wall {
		return c.name() + "_wall_fan", map[string]any{"coral_direction": int32(coralFanDirection(c.Facing))}
	}
	return c.name() + "_fan", map[string]any{"coral_fan_direction": int32(c.Rotation)}
}

// coralFanDirection is Bedrock's coral_direction of a wall fan facing d: west 0, east 1, north 2, south 3.
func coralFanDirection(d cube.Direction) int {
	switch d {
	case cube.West:
		return 0
	case cube.East:
		return 1
	case cube.North:
		return 2
	}
	return 3
}

// allCoralFans returns every coral fan state.
func allCoralFans() (fans []world.Block) {
	for _, dead := range []bool{false, true} {
		for _, t := range CoralTypes() {
			for r := 0; r < 2; r++ {
				fans = append(fans, CoralFan{Type: t, Dead: dead, Rotation: r})
			}
			for _, d := range []cube.Direction{cube.North, cube.South, cube.West, cube.East} {
				fans = append(fans, CoralFan{Type: t, Dead: dead, Wall: true, Facing: d})
			}
		}
	}
	return
}
