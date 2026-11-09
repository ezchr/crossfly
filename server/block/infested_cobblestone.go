package block

// InfestedCobblestone is a block that hides a silverfish. It looks identical to cobblestone.
// Broken without silk touch, it lets out a silverfish (releaseSilverfish).
type InfestedCobblestone struct {
	solid
	flute
}

// BreakInfo ...
func (i InfestedCobblestone) BreakInfo() BreakInfo {
	return newBreakInfo(1, pickaxeHarvestable, pickaxeEffective, silkTouchOnlyDrop(i)).withBlastResistance(0.75).withBreakHandler(releaseSilverfish)
}

// EncodeItem ...
func (InfestedCobblestone) EncodeItem() (name string, meta int16) {
	return "minecraft:infested_cobblestone", 0
}

// EncodeBlock ...
func (InfestedCobblestone) EncodeBlock() (string, map[string]any) {
	return "minecraft:infested_cobblestone", nil
}
