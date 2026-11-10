//go:build crossfly_full

package block

import (
	"strings"

	"github.com/df-mc/dragonfly/server/world"
)

// How a block reacts to a piston with redstone = "dragonfly", from Bedrock's block properties (BlockProperty
// Immovable, BreakOnPush, OnlyPistonPush and PistonBlockGrabber, set by the block classes of BDS 1.21.3). Unlike Java
// Edition, a block with a block entity moves like any other block, its contents with it, unless it is listed here.
const (
	dfPistonNormal = iota
	dfPistonAir
	dfPistonImmovable
	dfPistonBreak
	dfPistonPushOnly
	dfPistonGrabber
)

// dfPistonImmovableNames are the blocks a piston cannot move. The first group has the Immovable property in its
// block class (ObsidianBlock, GrindstoneBlock, JigsawBlock, MobSpawnerBlock, MovingBlock, StructureBlock,
// LodestoneBlock, EnderChestBlock, EndPortalFrameBlock, PortalBlock, EnchantingTableBlock, EndGatewayBlock,
// EndPortalBlock, PistonArmBlock, BorderBlock). The second group gets it where the blocks are registered, which the
// 1.21.3 dump could not decompile; they are the blocks Bedrock players know cannot be pushed.
var dfPistonImmovableNames = map[string]bool{
	"minecraft:obsidian": true, "minecraft:crying_obsidian": true, "minecraft:grindstone": true, "minecraft:jigsaw": true,
	"minecraft:mob_spawner": true, "minecraft:moving_block": true, "minecraft:structure_block": true,
	"minecraft:lodestone": true, "minecraft:ender_chest": true, "minecraft:end_portal_frame": true,
	"minecraft:portal": true, "minecraft:enchanting_table": true, "minecraft:end_gateway": true,
	"minecraft:end_portal": true, "minecraft:piston_arm_collision": true, "minecraft:sticky_piston_arm_collision": true,
	"minecraft:border_block": true,

	"minecraft:bedrock": true, "minecraft:barrier": true, "minecraft:command_block": true,
	"minecraft:repeating_command_block": true, "minecraft:chain_command_block": true, "minecraft:allow": true,
	"minecraft:deny": true, "minecraft:reinforced_deepslate": true, "minecraft:respawn_anchor": true,
	"minecraft:trial_spawner": true, "minecraft:vault": true,
}

// dfPistonBreakNames are the blocks with the BreakOnPush property that suffix rules (dfPistonBreakSuffixes) do not
// cover: they are destroyed, dropping their drops, when a piston pushes them.
var dfPistonBreakNames = map[string]bool{
	// TorchBlock and its colours, UnderwaterTorchBlock; RedStoneWireBlock, RepeaterBlock, ComparatorBlock, LeverBlock.
	"minecraft:torch": true, "minecraft:redstone_wire": true, "minecraft:powered_repeater": true,
	"minecraft:unpowered_repeater": true, "minecraft:powered_comparator": true, "minecraft:unpowered_comparator": true,
	"minecraft:lever": true, "minecraft:ladder": true, "minecraft:trip_wire": true, "minecraft:tripwire_hook": true,
	// LiquidBlock, FireBlock, SoulFireBlock, WebBlock, BubbleColumnBlock, TopSnowBlock, ScaffoldingBlock.
	"minecraft:water": true, "minecraft:flowing_water": true, "minecraft:lava": true, "minecraft:flowing_lava": true,
	"minecraft:fire": true, "minecraft:soul_fire": true, "minecraft:web": true, "minecraft:bubble_column": true,
	"minecraft:snow_layer": true, "minecraft:scaffolding": true,
	// BedBlock, ItemFrameBlock, GlowItemFrameBlock, FlowerPotBlock, CakeBlock, CampfireBlock, ShulkerBox.
	"minecraft:bed": true, "minecraft:frame": true, "minecraft:glow_frame": true, "minecraft:flower_pot": true,
	"minecraft:cake": true, "minecraft:campfire": true, "minecraft:soul_campfire": true,
	"minecraft:undyed_shulker_box": true,
	// PumpkinBlock, MelonBlock, MossBlock, CactusBlock, DragonEggBlock, TurtleEggBlock, FrogSpawnBlock.
	"minecraft:pumpkin": true, "minecraft:carved_pumpkin": true, "minecraft:lit_pumpkin": true,
	"minecraft:melon_block": true, "minecraft:moss_block": true, "minecraft:cactus": true, "minecraft:dragon_egg": true,
	"minecraft:turtle_egg": true, "minecraft:frog_spawn": true,
	// AmethystClusterBlock, BuddingAmethystBlock, ChorusFlowerBlock, ChorusPlantBlock.
	"minecraft:amethyst_cluster": true, "minecraft:large_amethyst_bud": true, "minecraft:medium_amethyst_bud": true,
	"minecraft:small_amethyst_bud": true, "minecraft:budding_amethyst": true, "minecraft:chorus_flower": true,
	"minecraft:chorus_plant": true,
	// Plants: BushBlock and its subclasses (flowers, saplings, crops, stems, grass, mushrooms, nether wart, berry
	// bushes, lily pads, sea pickles, dripleaves, propagules), KelpBlock, SeagrassBlock, SugarCaneBlock, the vines,
	// BambooStalkBlock, BambooSaplingBlock, NetherFungusBlock, NetherSproutsBlock, HangingRootsBlock,
	// SporeBlossomBlock, PinkPetalsBlock, MultifaceBlock (glow lichen, sculk vein), CaveVinesBlock.
	"minecraft:short_grass": true, "minecraft:tall_grass": true, "minecraft:fern": true, "minecraft:large_fern": true,
	"minecraft:deadbush": true, "minecraft:poppy": true, "minecraft:dandelion": true, "minecraft:blue_orchid": true,
	"minecraft:allium": true, "minecraft:azure_bluet": true, "minecraft:oxeye_daisy": true, "minecraft:cornflower": true,
	"minecraft:lily_of_the_valley": true, "minecraft:wither_rose": true, "minecraft:torchflower": true,
	"minecraft:sunflower": true, "minecraft:lilac": true, "minecraft:rose_bush": true, "minecraft:peony": true,
	"minecraft:pitcher_plant": true, "minecraft:pitcher_crop": true, "minecraft:torchflower_crop": true,
	"minecraft:red_flower": true, "minecraft:yellow_flower": true, "minecraft:double_plant": true,
	"minecraft:wheat": true, "minecraft:carrots": true, "minecraft:potatoes": true, "minecraft:beetroot": true,
	"minecraft:pumpkin_stem": true, "minecraft:melon_stem": true, "minecraft:brown_mushroom": true,
	"minecraft:red_mushroom": true, "minecraft:nether_wart": true, "minecraft:sweet_berry_bush": true,
	"minecraft:waterlily": true, "minecraft:sea_pickle": true, "minecraft:small_dripleaf_block": true,
	"minecraft:big_dripleaf": true, "minecraft:mangrove_propagule": true, "minecraft:azalea": true,
	"minecraft:flowering_azalea": true, "minecraft:kelp": true, "minecraft:seagrass": true, "minecraft:reeds": true,
	"minecraft:vine": true, "minecraft:weeping_vines": true, "minecraft:twisting_vines": true, "minecraft:cave_vines": true,
	"minecraft:cave_vines_body_with_berries": true, "minecraft:cave_vines_head_with_berries": true,
	"minecraft:bamboo": true, "minecraft:bamboo_sapling": true, "minecraft:crimson_fungus": true,
	"minecraft:warped_fungus": true, "minecraft:crimson_roots": true, "minecraft:warped_roots": true,
	"minecraft:nether_sprouts": true, "minecraft:hanging_roots": true, "minecraft:spore_blossom": true,
	"minecraft:pink_petals": true, "minecraft:glow_lichen": true, "minecraft:sculk_vein": true,
	"minecraft:azalea_leaves_flowered": true,
}

// dfPistonBreakSuffixes are the families with the BreakOnPush property: leaves (LeavesBlock), saplings, buttons,
// pressure plates, doors, signs and hanging signs, banners, heads, shulker boxes (UndyedShulkerBoxBlock), corals and
// coral fans (CoralPlantBlock, CoralFan), torches, tulips, candle cakes (CandleCakeBlock).
var dfPistonBreakSuffixes = []string{
	"_leaves", "_sapling", "_button", "_pressure_plate", "_door", "_sign", "_banner", "_skull", "_head",
	"_shulker_box", "_coral", "_coral_fan", "_coral_wall_fan", "_torch", "_tulip", "_candle_cake", "candle_cake",
}

// dfPistonReaction returns how the block b reacts to a piston with redstone = "dragonfly".
func dfPistonReaction(b world.Block) int {
	switch b := b.(type) {
	case Air:
		return dfPistonAir
	case Piston:
		// A piston that is extended or moving cannot be moved (its PistonArm block entity is not movable).
		if b.Extended || b.State != 0 {
			return dfPistonImmovable
		}
		return dfPistonNormal
	case PistonHead, MovingPiston:
		return dfPistonImmovable
	case Slime:
		return dfPistonGrabber
	case world.Liquid:
		return dfPistonBreak
	}
	name, _ := b.EncodeBlock()
	switch {
	case name == "minecraft:honey_block":
		return dfPistonGrabber
	case dfPistonImmovableNames[name]:
		return dfPistonImmovable
	case dfPistonBreakNames[name]:
		return dfPistonBreak
	case strings.HasSuffix(name, "_glazed_terracotta"):
		return dfPistonPushOnly
	}
	if strings.HasSuffix(name, "_coral_block") {
		return dfPistonNormal
	}
	for _, suffix := range dfPistonBreakSuffixes {
		if strings.HasSuffix(name, suffix) {
			return dfPistonBreak
		}
	}
	if breakable, ok := b.(Breakable); ok && breakable.BreakInfo().Hardness < 0 {
		// Unbreakable blocks the lists do not name (structure blocks of other kinds, light blocks...).
		return dfPistonImmovable
	}
	return dfPistonNormal
}

// dfPistonNonSticking reports whether a and b are a slime block and a honey block, which do not stick to each
// other: they only push each other.
func dfPistonNonSticking(a, b world.Block) bool {
	an, _ := a.EncodeBlock()
	bn, _ := b.EncodeBlock()
	return an == "minecraft:slime" && bn == "minecraft:honey_block" || an == "minecraft:honey_block" && bn == "minecraft:slime"
}
