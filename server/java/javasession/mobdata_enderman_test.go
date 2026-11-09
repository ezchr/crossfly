package javasession

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/java/protocol/wire"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// TestEndermanCarryState: the Bedrock carried block runtime id becomes the
// Java optional block state at 16 (0 for none or air), next to the angry
// flag at 17.
func TestEndermanCarryState(t *testing.T) {
	grass, _ := javaState(block.Grass{})
	if grass == 0 {
		t.Fatal("no Java state for grass")
	}
	for _, tc := range []struct {
		name string
		data map[uint32]any
		want int32
	}{
		{"nothing", nil, 0},
		{"air", map[uint32]any{protocol.EntityDataKeyCarryBlockRuntimeID: int32(world.BlockRuntimeID(block.Air{}))}, 0},
		{"grass", map[uint32]any{protocol.EntityDataKeyCarryBlockRuntimeID: int32(world.BlockRuntimeID(block.Grass{}))}, grass},
	} {
		e := &bossFake{fakeMob: newFakeMob("minecraft:enderman"), data: tc.data}
		var w wire.Writer
		d := mobData{w: &w}
		endermanData(nil, &d, e, "minecraft:enderman")
		var want meta
		want.raw(16, dataTypeOptionalBlockState, tc.want).z(17, false)
		if got, exp := string(w.B), string(want.w.B); got != exp || d.n != 2 {
			t.Errorf("%s: got %x (%d entries), want %x", tc.name, w.B, d.n, want.w.B)
		}
	}
}
