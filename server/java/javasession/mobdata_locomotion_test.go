package javasession

import (
	"encoding/hex"
	"testing"

	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// flaggedMob is a fakeMob with the extra actor flags and target of a
// server/mob mob.
type flaggedMob struct {
	*fakeMob
	flags  [3]uint64
	target *world.EntityHandle
}

func (m flaggedMob) ExtraActorFlags() [3]uint64        { return m.flags }
func (m flaggedMob) TargetEntity() *world.EntityHandle { return m.target }

func withFlag(flags [3]uint64, flag uint8) [3]uint64 {
	flags[flag/64] |= 1 << (flag % 64)
	return flags
}

// TestLocomotionMobData checks the guardian beam/spikes, pufferfish puff
// state and axolotl playing dead entries (Java 26.3 layout).
func TestLocomotionMobData(t *testing.T) {
	target := newFakeMob("minecraft:cod")
	tests := []struct {
		name string
		e    world.Entity
		want string
	}{
		{"puffed pufferfish", newFakeMob("minecraft:pufferfish").setVariant(2, 0), mob(10, 0).i(17, 2).end()},
		{"guardian beaming", flaggedMob{fakeMob: newFakeMob("minecraft:guardian"), flags: withFlag([3]uint64{}, protocol.EntityDataFlagMoving), target: target.H()},
			mob(10, 0).z(16, true).i(17, 42).end()},
		{"elder guardian idle", flaggedMob{fakeMob: newFakeMob("minecraft:elder_guardian")}, mob(10, 0).z(16, false).i(17, 0).end()},
		{"axolotl playing dead", flaggedMob{fakeMob: newFakeMob("minecraft:axolotl").setVariant(3, 0), flags: withFlag([3]uint64{}, protocol.EntityDataFlagPlayingDead)},
			mob(10, 0).z(16, false).i(18, 1).z(19, true).end()},
	}
	for _, tc := range tests {
		s := mobTestSession()
		s.entityIDs[target.H()] = 42
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
