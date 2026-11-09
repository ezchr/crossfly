package world

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world/chunk"
)

func TestEmptyBlockEntity(t *testing.T) {
	if !emptyBlockEntity(map[string]any{"x": int32(1), "y": int32(2), "z": int32(3)}) || !emptyBlockEntity(nil) {
		t.Error("position-only block entity data not seen as empty")
	}
	if emptyBlockEntity(map[string]any{"x": int32(1), "y": int32(2), "z": int32(3), "id": "Chest"}) {
		t.Error("block entity with an id seen as empty")
	}
}

// Entities of a type the World does not have are saved again with their chunk.
func TestUnknownEntityKeptAcrossSave(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()
	in := &chunk.Column{Chunk: chunk.New(w.conf.Blocks, w.Range()), Entities: []chunk.Entity{
		{ID: 42, Data: map[string]any{"identifier": "test:not_registered", "Pos": []float32{1, 64, 1}, "Custom": int32(7)}},
	}}
	col := w.columnFrom(in, ChunkPos{})
	if len(col.Entities) != 0 {
		t.Fatalf("unknown entity loaded as %v", col.Entities)
	}
	out := w.columnTo(col, ChunkPos{})
	if len(out.Entities) != 1 || out.Entities[0].ID != 42 || out.Entities[0].Data["Custom"] != int32(7) {
		t.Fatalf("unknown entity not saved again: %+v", out.Entities)
	}
}
