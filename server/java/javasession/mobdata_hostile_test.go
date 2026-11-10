package javasession

import (
	"encoding/hex"
	"testing"

	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
)

// TestPhantomSize: Phantom.ID_SIZE (16) from the scale.
func TestPhantomSize(t *testing.T) {
	for _, tc := range []struct {
		name string
		e    *fakeMob
		want string
	}{
		{"phantom (no size data: unchanged for Bedrock-engine phantoms)", newFakeMob("minecraft:phantom"), mob(10, 0).end()},
		{"phantom size 4", newFakeMob("minecraft:phantom").scaled(1.6), mob(10, 0).i(16, 4).end()},
		{"phantom size 64", newFakeMob("minecraft:phantom").scaled(1 + 0.15*64), mob(10, 0).i(16, 64).end()},
	} {
		s := mobTestSession()
		s.viewMobData(tc.e, 7)
		out := s.take()
		if len(out) != 1 || out[0].id != v777.ClientboundPlaySetEntityData {
			t.Errorf("%s: want one set_entity_data, got %d packets", tc.name, len(out))
			continue
		}
		if got := hex.EncodeToString(out[0].w.B); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, got, tc.want)
		}
	}
}
