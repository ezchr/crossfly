package javasession

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// TestBatMobData: the Bedrock RESTING flag is Bat.DATA_ID_FLAGS 0x01 (16).
func TestBatMobData(t *testing.T) {
	s := mobTestSession()
	for _, tc := range []struct {
		name  string
		flags []int
		want  string
	}{
		{"flying", nil, mob(10, 0).b(16, 0).end()},
		{"hanging", []int{protocol.EntityDataFlagResting}, mob(10, 0).b(16, 0x01).end()},
	} {
		b := (&bossFake{fakeMob: newFakeMob("minecraft:bat")}).setFlags(tc.flags...)
		s.mobData = nil
		if got, _ := viewData(t, s, b); got != tc.want {
			t.Errorf("%v: data %v, want %v", tc.name, got, tc.want)
		}
	}
}
