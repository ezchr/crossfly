//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// Dispense places a carved pumpkin in front of a dispenser when it completes a golem pattern there, building the
// golem (PumpkinBlock::dispense). Otherwise the pumpkin is dropped like any item.
func (p Pumpkin) Dispense(pos cube.Pos, face cube.Face, tx *world.Tx, ctx *item.DispenseContext) item.DispenseResult {
	if !p.Carved {
		// A plain pumpkin is no golem head (in worlds of a recent base game version), so BDS doesn't place it.
		return item.DispenseDefault
	}
	return dispenseGolemHead(Pumpkin{Carved: true, Facing: cube.South}, pos.Side(face), tx, ctx)
}

// Dispense places a jack o'lantern in front of a dispenser when it completes a golem pattern there, building the
// golem (PumpkinBlock::dispense). Otherwise it is dropped like any item.
func (l LitPumpkin) Dispense(pos cube.Pos, face cube.Face, tx *world.Tx, ctx *item.DispenseContext) item.DispenseResult {
	return dispenseGolemHead(LitPumpkin{Facing: cube.South}, pos.Side(face), tx, ctx)
}

// dispenseGolemHead is PumpkinBlock::dispense (26.60): the head goes into the air in front of the dispenser, in its
// default state (facing south), if a golem pattern without its head is there, and is then placed as by a player.
// A carved pumpkin that isn't placed would go onto the head of an entity in front (DispensableItemUtils::
// tryDispenseArmor), which dispensers don't do with any armour here, so it is dropped.
func dispenseGolemHead(head world.Block, front cube.Pos, tx *world.Tx, ctx *item.DispenseContext) item.DispenseResult {
	if !isGolemAir(tx.Block(front)) || !golemPatternWithoutHead(tx, front) {
		return item.DispenseDefault
	}
	ctx.SubtractFromCount(1)
	tx.SetBlock(front, head, nil)
	tx.PlaySound(front.Vec3Centre(), sound.Click{})
	spawnGolem(tx, front)
	return item.DispenseSuccess
}

// golemPatternWithoutHead reports whether a golem pattern with air for its head is at pos (the 26.60 dispenser
// patterns of PumpkinBlock::dispense: the snow golem from pos, the iron and copper golems around it).
func golemPatternWithoutHead(tx *world.Tx, pos cube.Pos) bool {
	return snowGolemDispensePattern.matchFrom(tx, pos).ok || ironGolemDispensePattern.find(tx, pos).ok ||
		copperGolemDispensePattern.find(tx, pos).ok
}

var (
	snowGolemDispensePattern = golemPattern{rows: snowGolemPattern.rows, cells: map[byte]func(world.Block) bool{
		'^': isGolemAir,
		'#': snowGolemPattern.cells['#'],
	}}
	ironGolemDispensePattern = golemPattern{rows: ironGolemPattern.rows, cells: map[byte]func(world.Block) bool{
		'~': isGolemAir,
		'^': isGolemAir,
		'#': ironGolemPattern.cells['#'],
	}}
	copperGolemDispensePattern = golemPattern{rows: copperGolemPattern.rows, cells: map[byte]func(world.Block) bool{
		'^': isGolemAir,
		'#': isGolemCopper,
	}}
)
