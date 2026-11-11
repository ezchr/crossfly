package javasession

import (
	"encoding/hex"
	"testing"

	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/world"
)

// hookFake is a fishing hook holding target, as entity.FishingHookBehaviour.
type hookFake struct {
	*fakeMob
	target *world.EntityHandle
}

func (h hookFake) TargetEntity() *world.EntityHandle { return h.target }

// TestFishingBobberHooked: FishingHook.DATA_HOOKED_ENTITY is the Java id of the hooked entity + 1, 0 for none or an
// entity the client does not know; DATA_BITING stays false.
func TestFishingBobberHooked(t *testing.T) {
	other := newFakeMob("minecraft:zombie")
	stranger := newFakeMob("minecraft:zombie")
	for _, c := range []struct {
		name   string
		target *world.EntityHandle
		want   int32
	}{
		{"nothing", nil, 0},
		{"a zombie", other.H(), 43},
		{"an unknown entity", stranger.H(), 0},
	} {
		s := mobTestSession()
		s.entityIDs[other.H()] = 42
		s.viewMobData(hookFake{newFakeMob("minecraft:fishing_hook"), c.target}, 7)
		out := s.take()
		if len(out) != 1 || out[0].id != v777.ClientboundPlaySetEntityData {
			t.Fatalf("%s: want one set_entity_data, got %d packets", c.name, len(out))
		}
		if got, want := hex.EncodeToString(out[0].w.B), entityData(7).i(8, c.want).z(9, false).end(); got != want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, want)
		}
	}
	s := mobTestSession()
	s.ent = newFakeMob("minecraft:player").H()
	s.viewMobData(hookFake{newFakeMob("minecraft:fishing_hook"), s.ent}, 7)
	if got, want := hex.EncodeToString(s.take()[0].w.B), entityData(7).i(8, selfEntityID+1).z(9, false).end(); got != want {
		t.Errorf("hooked self:\n got %s\nwant %s", got, want)
	}
}
