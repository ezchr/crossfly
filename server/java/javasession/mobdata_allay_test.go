package javasession

import (
	"encoding/hex"
	"testing"

	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// TestAllayDancingData: a dancing allay (Bedrock dancing actor flag) has Allay.DATA_DANCING (16).
func TestAllayDancingData(t *testing.T) {
	tests := []struct {
		name string
		m    flaggedMob
		want string
	}{
		{"idle allay", flaggedMob{fakeMob: newFakeMob("minecraft:allay")}, mob(10, 0).z(16, false).end()},
		{"dancing allay", flaggedMob{fakeMob: newFakeMob("minecraft:allay"), flags: withFlag([3]uint64{}, protocol.EntityDataFlagDancing)},
			mob(10, 0).z(16, true).end()},
	}
	for _, tc := range tests {
		s := mobTestSession()
		s.viewMobData(tc.m, 7)
		out := s.take()
		if len(out) != 1 || out[0].id != v777.ClientboundPlaySetEntityData {
			t.Fatalf("%s: want one set_entity_data, got %d packets", tc.name, len(out))
		}
		if got := hex.EncodeToString(out[0].w.B); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, got, tc.want)
		}
	}
}
