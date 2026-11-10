package javasession

import (
	"encoding/hex"
	"testing"

	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// TestEvokerSpellData: a casting evoker shows Java the spell its Bedrock
// spell colour stands for; an idle one none.
func TestEvokerSpellData(t *testing.T) {
	s := mobTestSession()
	casting := [3]uint64{}
	casting[protocol.EntityDataFlagCasting/64] |= 1 << (protocol.EntityDataFlagCasting % 64)
	colour := func(c uint32) map[uint32]any {
		return map[uint32]any{protocol.EntityDataKeyDataSpellCastingColor: int32(c)}
	}
	tests := []struct {
		name string
		e    *bossFake
		want string
	}{
		{"idle", &bossFake{fakeMob: newFakeMob("minecraft:evocation_illager"), data: colour(0xFF664D59)}, mob(10, 0).z(16, false).b(17, 0).end()},
		{"fangs", &bossFake{fakeMob: newFakeMob("minecraft:evocation_illager"), data: colour(0xFF664D59), flags: casting}, mob(10, 0).z(16, false).b(17, 2).end()},
		{"vexes", &bossFake{fakeMob: newFakeMob("minecraft:evocation_illager"), data: colour(0xFFB3B3CC), flags: casting}, mob(10, 0).z(16, false).b(17, 1).end()},
		{"wololo", &bossFake{fakeMob: newFakeMob("minecraft:evocation_illager"), data: colour(0xFFB38033), flags: casting}, mob(10, 0).z(16, false).b(17, 3).end()},
	}
	for i, tc := range tests {
		s.viewMobData(tc.e, 7)
		out := s.take()
		if len(out) != 1 || out[0].id != v777.ClientboundPlaySetEntityData {
			t.Errorf("%s (%d): %d packets", tc.name, i, len(out))
			continue
		}
		if got := hex.EncodeToString(out[0].w.B); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, got, tc.want)
		}
	}
}
