//go:build crossfly_full

package item

import "github.com/df-mc/dragonfly/server/world"

// Harness is an item an adult happy ghast wears on its body so that players can ride it. It comes in the
// sixteen dye colours. The happy ghast takes it through its minecraft:interact component (equip_item_slot
// slot.armor.body) and drops it when it is sheared; it is only of use with the mobs of Crossfly Full.
type Harness struct {
	// Colour is the colour of the harness.
	Colour Colour
}

// MaxCount ...
func (Harness) MaxCount() int {
	return 1
}

// EncodeItem ...
func (h Harness) EncodeItem() (name string, meta int16) {
	return "minecraft:" + h.Colour.String() + "_harness", 0
}

// Harnesses returns a harness of every colour.
func Harnesses() []Harness {
	hs := make([]Harness, 0, 16)
	for _, c := range Colours() {
		hs = append(hs, Harness{Colour: c})
	}
	return hs
}

func init() {
	mobItems = append(mobItems, func() {
		for _, h := range Harnesses() {
			world.RegisterItem(h)
		}
	})
}
