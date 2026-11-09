package javamap

// snowyBlocks are the Bedrock names of the blocks Java Edition draws with snow on their sides (Java's snowy
// property) when snow is on top. Bedrock blocks have no such state: Bedrock clients look at the block above.
var snowyBlocks = [...]string{"minecraft:grass_block", "minecraft:mycelium", "minecraft:podzol"}

// Snowy returns the snowy=true state of a Java grass block, mycelium or podzol state, which the tables map every
// Bedrock state of those blocks to as snowy=false. ok is false for any other state. Java lists snowy=true right
// before snowy=false.
func Snowy(java int32) (snowy int32, ok bool) {
	for _, name := range snowyBlocks {
		if nameDefault[name] == java {
			return java - 1, true
		}
	}
	return 0, false
}

// SnowCover reports whether a Bedrock block name makes the snowy block below it snowy on Java: Java's snow block
// tag (snow layers, snow blocks and powder snow).
func SnowCover(name string) bool {
	switch name {
	case "minecraft:snow_layer", "minecraft:snow", "minecraft:powder_snow":
		return true
	}
	return false
}
