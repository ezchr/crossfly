package javasession

import "github.com/df-mc/dragonfly/server/world"

// A Java client draws a moving block (moving_piston) from its block state alone, so a block it draws only through
// its block entity (a chest, a bell, a decorated pot...) is invisible while it moves. Bedrock pistons move such
// blocks (redstone = "dragonfly"): a Java client is shown the block where it arrives straight away instead.

// javaStandIn is a block that may show a Java client another block in its place.
type javaStandIn interface {
	JavaStandIn() world.Block
}

// javaEntityDrawn are the Java block entity types that draw their block entirely.
var javaEntityDrawn = map[string]bool{
	"minecraft:chest": true, "minecraft:trapped_chest": true, "minecraft:ender_chest": true,
	"minecraft:shulker_box": true, "minecraft:sign": true, "minecraft:hanging_sign": true, "minecraft:skull": true,
	"minecraft:banner": true, "minecraft:bed": true, "minecraft:bell": true, "minecraft:decorated_pot": true,
	"minecraft:conduit": true, "minecraft:copper_golem_statue": true,
}

// javaShown returns the block a Java client is shown for b.
func javaShown(b world.Block) world.Block {
	s, ok := b.(javaStandIn)
	if !ok {
		return b
	}
	in := s.JavaStandIn()
	if in == nil {
		return b
	}
	name, _ := in.EncodeBlock()
	if !javaEntityDrawn[javaBlockEntityName(name)] {
		return b
	}
	return in
}
