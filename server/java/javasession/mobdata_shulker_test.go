package javasession

import (
	"encoding/hex"
	"testing"

	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// TestShulkerAndChargeMobData: a shulker's attach face (Direction), peek and
// colour from its Bedrock actor data; a charging ghast's open mouth and a
// charging blaze's fire from the Bedrock charged flag.
func TestShulkerAndChargeMobData(t *testing.T) {
	charged := [3]uint64{}
	charged[protocol.EntityDataFlagCharged/64] |= 1 << (protocol.EntityDataFlagCharged % 64)
	tests := []struct {
		name string
		e    world.Entity
		want string
	}{
		{"red shulker on the ceiling, peeking", &bossFake{fakeMob: newFakeMob("minecraft:shulker").setVariant(1, 0), data: map[uint32]any{
			protocol.EntityDataKeyAttachFace: byte(1), protocol.EntityDataKeyPeekID: int32(30),
		}}, mob(10, 0).raw(16, 12, 1).b(17, 30).b(18, 14).end()},
		{"charging ghast", &bossFake{fakeMob: newFakeMob("minecraft:ghast"), flags: charged}, mob(10, 0).z(16, true).end()},
		{"charging blaze", &bossFake{fakeMob: newFakeMob("minecraft:blaze"), flags: charged}, mob(10, 0).b(16, 1).end()},
		{"blaze", &bossFake{fakeMob: newFakeMob("minecraft:blaze")}, mob(10, 0).b(16, 0).end()},
	}
	for _, tc := range tests {
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
