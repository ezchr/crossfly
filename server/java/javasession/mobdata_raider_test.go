package javasession

import (
	"encoding/hex"
	"testing"

	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// TestIllagerPoseData: what the Java client draws an illager's or piglin's
// arms from. Aggressive (attacking, or the Bedrock angry flag a vindicator
// gets from on_target_acquired) sets Mob flag 0x04: the vindicator and the
// melee pillager raise their weapons (IllagerArmPose.ATTACKING), the piglin
// brute its axe (ATTACKING_WITH_MELEE_WEAPON). The Bedrock celebrating flag is
// Raider.IS_CELEBRATING (16) and Piglin.DATA_IS_DANCING (19); a charging
// piglin's using_item is DATA_IS_CHARGING_CROSSBOW (18).
func TestIllagerPoseData(t *testing.T) {
	flag := func(f int) [3]uint64 {
		var v [3]uint64
		v[f/64] |= 1 << (f % 64)
		return v
	}
	fake := func(id string, set func(m *fakeMob)) *bossFake {
		m := newFakeMob(id)
		if set != nil {
			set(m)
		}
		return &bossFake{fakeMob: m}
	}
	celebrating := func(id string) *bossFake {
		b := fake(id, nil)
		b.flags = flag(protocol.EntityDataFlagCelebrating)
		return b
	}
	tests := []struct {
		name string
		e    *bossFake
		want string
	}{
		{"calm vindicator", fake("minecraft:vindicator", nil), mob(10, 0).z(16, false).end()},
		{"angry vindicator", fake("minecraft:vindicator", func(m *fakeMob) { m.angry = true }), mob(10, 0x04).z(16, false).end()},
		{"attacking vindicator", fake("minecraft:vindicator", func(m *fakeMob) { m.aggro = true }), mob(10, 0x04).z(16, false).end()},
		{"celebrating vindicator", celebrating("minecraft:vindicator"), mob(10, 0).z(16, true).end()},
		{"melee pillager", fake("minecraft:pillager", func(m *fakeMob) { m.aggro = true }), mob(10, 0x04).z(16, false).z(17, false).end()},
		{"charging pillager", fake("minecraft:pillager", func(m *fakeMob) { m.aggro, m.using = true, true }), mob(10, 0x04).z(16, false).z(17, true).end()},
		{"celebrating evoker", celebrating("minecraft:evocation_illager"), mob(10, 0).z(16, true).b(17, 0).end()},
		{"celebrating ravager", celebrating("minecraft:ravager"), mob(10, 0).z(16, true).end()},
		{"attacking piglin brute", fake("minecraft:piglin_brute", func(m *fakeMob) { m.aggro = true }), mob(10, 0x04).end()},
		{"charging piglin", fake("minecraft:piglin", func(m *fakeMob) { m.aggro, m.using = true, true }), mob(10, 0x04).z(17, false).z(18, true).z(19, false).end()},
		{"dancing piglin", celebrating("minecraft:piglin"), mob(10, 0).z(17, false).z(18, false).z(19, true).end()},
	}
	for _, tc := range tests {
		s := mobTestSession()
		s.viewMobData(tc.e, 7)
		out := s.take()
		if len(out) != 1 || out[0].id != v777.ClientboundPlaySetEntityData {
			t.Errorf("%s: %d packets", tc.name, len(out))
			continue
		}
		if got := hex.EncodeToString(out[0].w.B); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, got, tc.want)
		}
	}
}
