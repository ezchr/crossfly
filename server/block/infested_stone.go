package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// releaseSilverfish is the break handler of the infested blocks (BDS InfestedBlock::spawnAfterBreak
// and spawnSilverfish): broken without silk touch, and not in creative (which drops no resources),
// the block lets out a persistent silverfish at its integer position, as BDS spawns it. u may be nil
// when no player broke the block (a silverfish waking its friends).
func releaseSilverfish(pos cube.Pos, tx *world.Tx, u item.User) {
	if u != nil {
		if g, ok := u.(interface{ GameMode() world.GameMode }); ok && g.GameMode().CreativeInventory() {
			return
		}
		if held, _ := u.HeldItems(); hasSilkTouch(held.Enchantments()) {
			return
		}
	}
	SpawnMob(tx, "minecraft:silverfish", pos.Vec3(), MobSpawnOptions{Persistent: true})
}

// InfestedStone is a block that hides a silverfish. It looks identical to stone.
// Broken without silk touch, it lets out a silverfish (releaseSilverfish).
type InfestedStone struct {
	solid
	flute
}

// BreakInfo ...
func (i InfestedStone) BreakInfo() BreakInfo {
	return newBreakInfo(0.75, pickaxeHarvestable, pickaxeEffective, silkTouchOnlyDrop(i)).withBlastResistance(0.75).withBreakHandler(releaseSilverfish)
}

// EncodeItem ...
func (InfestedStone) EncodeItem() (name string, meta int16) {
	return "minecraft:infested_stone", 0
}

// EncodeBlock ...
func (InfestedStone) EncodeBlock() (string, map[string]any) {
	return "minecraft:infested_stone", nil
}
