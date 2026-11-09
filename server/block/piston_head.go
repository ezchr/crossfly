//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

type PistonHead struct {
	solid
	transparent
	sourceWaterDisplacer
	Facing cube.Face
	Sticky bool
}

func (h PistonHead) EncodeBlock() (string, map[string]any) {
	n := "minecraft:piston_arm_collision"
	if h.Sticky {
		n = "minecraft:sticky_piston_arm_collision"
	}
	return n, map[string]any{"facing_direction": int32(pistonBedrockFace(h.Facing))}
}
func (h PistonHead) Model() world.BlockModel { return pistonHeadModel{h.Facing} }
func (h PistonHead) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	p, ok := tx.Block(pos.Side(h.Facing.Opposite())).(Piston)
	if !ok || p.Facing != h.Facing || !p.Extended {
		tx.SetBlock(pos, nil, nil)
	}
}
func (h PistonHead) BreakInfo() BreakInfo {
	return newBreakInfo(1.5, alwaysHarvestable, pickaxeEffective, simpleDrops()).withBreakHandler(func(pos cube.Pos, tx *world.Tx, user item.User) {
		base := pos.Side(h.Facing.Opposite())
		if p, ok := tx.Block(base).(Piston); ok && p.Facing == h.Facing {
			if g, ok := user.(interface{ GameMode() world.GameMode }); ok && g.GameMode().CreativeInventory() {
				breakBlockNoDrops(p, base, tx)
			} else {
				breakBlock(p, base, tx)
			}
		}
	})
}
func allPistonHeads() (blocks []world.Block) {
	for _, f := range cube.Faces() {
		for _, sticky := range []bool{false, true} {
			blocks = append(blocks, PistonHead{Facing: f, Sticky: sticky})
		}
	}
	return
}

func (h PistonHead) BlockRemoved(pos cube.Pos, replacement world.Block, tx *world.Tx) {
	if _, moving := replacement.(MovingPiston); moving {
		return
	}
	base := pos.Side(h.Facing.Opposite())
	if p, ok := tx.Block(base).(Piston); ok && p.Extended && p.Facing == h.Facing && p.Sticky == h.Sticky {
		breakBlock(p, base, tx)
	}
}
