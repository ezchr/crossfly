package javasession

import (
	"encoding/hex"
	"testing"

	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// bossFake is a fakeMob with the extra actor data and flags of mob.Mob.
type bossFake struct {
	*fakeMob
	data  map[uint32]any
	flags [3]uint64
}

func (b *bossFake) ExtraActorData() map[uint32]any { return b.data }
func (b *bossFake) ExtraActorFlags() [3]uint64     { return b.flags }

// TestBossMobData: the wither's head targets (Java ids, 0 for none) and
// invulnerable ticks, the dragon's phase and the warden's anger.
func TestBossMobData(t *testing.T) {
	s := mobTestSession()
	target := newFakeMob("minecraft:zombie")
	s.entityIDs[target.H()] = 42
	wither := &bossFake{fakeMob: newFakeMob("minecraft:wither"), data: map[uint32]any{
		protocol.EntityDataKeyTargetA:           target.H(),
		protocol.EntityDataKeyTargetB:           (*world.EntityHandle)(nil),
		protocol.EntityDataKeyInvulnerableTicks: int32(120),
	}}
	dragon := newFakeMob("minecraft:ender_dragon").with(func(m *fakeMob) { m.sitting = true })
	warden := newFakeMob("minecraft:warden").with(func(m *fakeMob) { m.angry = true })
	tests := []struct {
		name string
		e    world.Entity
		want string
	}{
		{"wither charging, head A on a zombie", wither, mob(10, 0).i(16, 42).i(17, 0).i(18, 0).i(19, 120).end()},
		{"perched dragon", dragon, mob(10, 0).i(16, 6).end()},
		{"angry warden", warden, mob(10, 0x04).i(16, 80).end()},
	}
	for _, tc := range tests {
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

// TestBossPoses: warden and breeze actor flags become Java poses.
func TestBossPoses(t *testing.T) {
	w := &bossFake{fakeMob: newFakeMob("minecraft:warden")}
	if _, ok := mobFlagPose(w); ok {
		t.Errorf("idle warden has a pose")
	}
	for flag, pose := range map[int]int32{
		protocol.EntityDataFlagEmerging: poseEmerging, protocol.EntityDataFlagDigging: poseDigging,
		protocol.EntityDataFlagRoaring: poseRoaring, protocol.EntityDataFlagSniffing: poseSniffing,
	} {
		w.flags = [3]uint64{}
		w.flags[flag/64] |= 1 << (flag % 64)
		if got, ok := mobFlagPose(w); !ok || got != pose {
			t.Errorf("flag %d: pose %d %v, want %d", flag, got, ok, pose)
		}
	}
	b := &bossFake{fakeMob: newFakeMob("minecraft:breeze")}
	b.flags[protocol.EntityDataFlagFacingTargetToRangeAttack/64] |= 1 << (protocol.EntityDataFlagFacingTargetToRangeAttack % 64)
	if got, ok := mobFlagPose(b); !ok || got != poseShooting {
		t.Errorf("breeze shooting pose %d %v", got, ok)
	}
}

// TestMobBossBar: one boss_event per boss under its UUID: add, progress,
// remove; the dragon's is pink with music and fog.
func TestMobBossBar(t *testing.T) {
	s := mobTestSession()
	d := newFakeMob("minecraft:ender_dragon")
	s.ShowMobBossBar(d, "", 0.5, 5, false)
	s.UpdateMobBossBar(d, 0.25)
	s.HideMobBossBar(d)
	out := s.take()
	if len(out) != 3 {
		t.Fatalf("%d packets, want 3", len(out))
	}
	for _, p := range out {
		if p.id != v777.ClientboundPlayBossEvent {
			t.Fatalf("packet %v, want boss_event", p.id)
		}
	}
	add := out[0].w.B
	u := d.H().UUID()
	if hex.EncodeToString(add[:16]) != hex.EncodeToString(u[:]) || add[16] != bossAdd {
		t.Errorf("add starts %x", add[:17])
	}
	if flags := add[len(add)-1]; flags != 0x06 {
		t.Errorf("dragon flags %#x, want music and fog", flags)
	}
	if out[1].w.B[16] != bossProgress || out[2].w.B[16] != bossRemove {
		t.Errorf("actions %d %d", out[1].w.B[16], out[2].w.B[16])
	}
}
