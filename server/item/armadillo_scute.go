package item

import "github.com/df-mc/dragonfly/server/world"

// ArmadilloScute is a scute an adult armadillo sheds now and then, or drops when it is brushed. It is crafted
// into wolf armour and repairs wolf armour worn by a tamed wolf.
type ArmadilloScute struct{}

// EncodeItem ...
func (ArmadilloScute) EncodeItem() (name string, meta int16) {
	return "minecraft:armadillo_scute", 0
}

func init() {
	world.RegisterItem(ArmadilloScute{})
}
