package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
)

// Punch is saved as a byte, like Bedrock's enchantPunch: levels past 255 are saved as 255 rather
// than wrapping around.
func TestArrowPunchNBT(t *testing.T) {
	for level, want := range map[int]byte{0: 0, 2: 2, 10: 10, 255: 255, 300: 255, 32767: 255} {
		conf := arrowConf
		conf.KnockBackForceAddend = float64(level) * enchantment.Punch.KnockBackMultiplier()
		m := arrowType{}.EncodeNBT(&world.EntityData{Data: conf.New()})
		if got := m["enchantPunch"].(byte); got != want {
			t.Errorf("Punch %d saved as %d, want %d", level, got, want)
		}
	}
}
