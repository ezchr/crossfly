package javasession

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// TestGoatMobData: the Bedrock variant 1 is a screaming goat (18) and the
// horn count (actor data 122) the left (19) and right (20) horns.
func TestGoatMobData(t *testing.T) {
	s := mobTestSession()
	for _, tc := range []struct {
		name      string
		variant   int32
		data      map[uint32]any
		scream    bool
		left, rgt bool
	}{
		{"no data", 0, nil, false, true, true},
		{"two horns", 0, map[uint32]any{protocol.EntityDataKeyGoatHornCount: int32(2)}, false, true, true},
		{"screaming, one horn", 1, map[uint32]any{protocol.EntityDataKeyGoatHornCount: int32(1)}, true, true, false},
		{"no horns", 0, map[uint32]any{protocol.EntityDataKeyGoatHornCount: int32(0)}, false, false, false},
	} {
		g := &bossFake{fakeMob: newFakeMob("minecraft:goat").setVariant(tc.variant, 0), data: tc.data}
		s.mobData = nil
		want := mob(10, 0).z(16, false).z(18, tc.scream).z(19, tc.left).z(20, tc.rgt).end()
		if got, _ := viewData(t, s, g); got != want {
			t.Errorf("%v: data %v, want %v", tc.name, got, want)
		}
	}
}
