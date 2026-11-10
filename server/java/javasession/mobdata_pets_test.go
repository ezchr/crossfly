package javasession

import (
	"encoding/hex"
	"testing"

	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// TestPetMobData checks the pet and mount entries that come from extra actor
// flags (mobdata_pets.go) against the Java 26.3 layout.
func TestPetMobData(t *testing.T) {
	flagged := func(id string, flags ...uint8) flaggedMob {
		m := flaggedMob{fakeMob: newFakeMob(id)}
		for _, f := range flags {
			m.flags = withFlag(m.flags, f)
		}
		return m
	}
	sittingCamel := flagged("minecraft:camel")
	sittingCamel.sitting = true
	tests := []struct {
		name string
		m    flaggedMob
		want string
	}{
		{"begging wolf", flagged("minecraft:wolf", protocol.EntityDataFlagInterested),
			mob(10, 0).z(16, false).b(18, 0).i(21, 0).l(22, -1).h(23, 25, 3).z(20, true).end()},
		{"cat lying on a bed", flagged("minecraft:cat", protocol.EntityDataFlagLayingDown),
			mob(10, 0).z(16, false).b(18, 0).h(20, 21, 10).i(23, 0).z(21, true).z(22, false).end()},
		{"horse eating grass and rearing", flagged("minecraft:horse", protocol.EntityDataFlagEating, protocol.EntityDataFlagStanding),
			mob(10, 0).z(16, false).b(18, 0x30).i(19, 0).end()},
		{"sitting camel", sittingCamel, mob(10, 0).z(16, false).b(18, 0).z(19, false).l(20, -1).end()},
		{"dashing camel", flagged("minecraft:camel", protocol.EntityDataFlagDash), mob(10, 0).z(16, false).b(18, 0).z(19, true).l(20, 0).end()},
		{"standing polar bear", flagged("minecraft:polar_bear", protocol.EntityDataFlagStanding), mob(10, 0).z(16, false).z(18, true).end()},
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
	if p, ok := mobFlagPose(sittingCamel); !ok || p != poseSitting {
		t.Errorf("sitting camel pose %v %v", p, ok)
	}
	if _, ok := mobFlagPose(flagged("minecraft:camel")); ok {
		t.Error("standing camel has a pose")
	}
}
