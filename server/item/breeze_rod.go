package item

// BreezeRod is an item dropped by breezes, used to craft maces and wind charges and to repair maces.
type BreezeRod struct{}

// EncodeItem ...
func (BreezeRod) EncodeItem() (name string, meta int16) { return "minecraft:breeze_rod", 0 }
