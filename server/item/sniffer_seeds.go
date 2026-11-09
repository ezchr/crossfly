//go:build crossfly_full

package item

// TorchflowerSeeds are the seeds a sniffer digs up (one of the two drops of
// loot_tables/gameplay/entities/sniffer_seeds.json). Sniffers and chickens
// are tempted by them and sniffers breed with them. Dragonfly has no
// torchflower crop, so they cannot be planted yet.
type TorchflowerSeeds struct{}

// EncodeItem ...
func (TorchflowerSeeds) EncodeItem() (name string, meta int16) {
	return "minecraft:torchflower_seeds", 0
}

// PitcherPod is the other seed a sniffer digs up. Dragonfly has no pitcher
// crop, so it cannot be planted yet.
type PitcherPod struct{}

// EncodeItem ...
func (PitcherPod) EncodeItem() (name string, meta int16) {
	return "minecraft:pitcher_pod", 0
}
