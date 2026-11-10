//go:build crossfly_full

package javamap

import (
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// TestCalibratedSculkSensorJava checks that every calibrated sculk sensor state is the Java calibrated sculk
// sensor facing the same way (Bedrock minecraft:cardinal_direction, Java facing) in the same phase, and the
// item the Java calibrated sculk sensor.
func TestCalibratedSculkSensorJava(t *testing.T) {
	rep := javaBlocks(t)
	table := DefaultBlockStates()
	phases := []string{"inactive", "active", "cooldown"}
	for _, f := range cube.Directions() {
		for p, name := range phases {
			b := block.CalibratedSculkSensor{Facing: f, Phase: p}
			want := fmt.Sprintf("minecraft:calibrated_sculk_sensor[facing=%v,power=0,sculk_sensor_phase=%s,waterlogged=false]", f, name)
			if got := rep.byID[table[world.BlockRuntimeID(b)]]; got != want {
				t.Errorf("%+v -> %s, want %s", b, got, want)
			}
		}
	}
	if id := Item(block.CalibratedSculkSensor{}); javaItemIDs(t)["minecraft:calibrated_sculk_sensor"] != id {
		t.Errorf("calibrated sculk sensor item -> Java item %d, want minecraft:calibrated_sculk_sensor", id)
	}
}
