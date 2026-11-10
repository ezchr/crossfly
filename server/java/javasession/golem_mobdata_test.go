package javasession

import (
	"encoding/hex"
	"testing"

	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
)

func TestCopperGolemDataFromProperties(t *testing.T) {
	const copperGolemState, weatheringCopperState = 37, 38
	for _, tc := range []struct {
		name               string
		oxidation, chest   string
		weather, golemData int32
	}{
		{"new golem", "unoxidized", "none", 0, 0},
		{"weathered, putting items in", "weathered", "put", 2, 3},
		{"exposed, nothing to take", "exposed", "take_fail", 1, 2},
		{"oxidized, taking", "oxidized", "take", 3, 1},
		{"no room to put", "unoxidized", "put_fail", 0, 4},
	} {
		s := mobTestSession()
		s.viewMobData(newFakeMob("minecraft:copper_golem").props2("minecraft:oxidation_level", tc.oxidation, "minecraft:chest_interaction", tc.chest), 7)
		out := s.take()
		want := mob(10, 0).raw(16, weatheringCopperState, tc.weather).raw(17, copperGolemState, tc.golemData).end()
		if len(out) != 1 || out[0].id != v777.ClientboundPlaySetEntityData {
			t.Fatalf("%s: want one set_entity_data, got %d packets", tc.name, len(out))
		}
		if got := hex.EncodeToString(out[0].w.B); got != want {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, got, want)
		}
	}
}
