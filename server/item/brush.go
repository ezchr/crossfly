package item

import "github.com/df-mc/dragonfly/server/world"

// Brush is a tool that brushes scutes off armadillos, losing 16 durability each time (the armadillo's
// minecraft:interact hurt_item). Brushing suspicious sand and gravel is not implemented, as those blocks do not
// exist.
type Brush struct{}

// MaxCount always returns 1.
func (Brush) MaxCount() int {
	return 1
}

// DurabilityInfo ...
func (Brush) DurabilityInfo() DurabilityInfo {
	// 64 durability, as vanilla gives the brush.
	return DurabilityInfo{
		MaxDurability: 64,
		BrokenItem:    simpleItem(Stack{}),
	}
}

// EncodeItem ...
func (Brush) EncodeItem() (name string, meta int16) {
	return "minecraft:brush", 0
}

func init() {
	world.RegisterItem(Brush{})
}
