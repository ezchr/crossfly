package world

import "testing"

func TestEmptyBlockEntity(t *testing.T) {
	if !emptyBlockEntity(map[string]any{"x": int32(1), "y": int32(2), "z": int32(3)}) || !emptyBlockEntity(nil) {
		t.Error("position-only block entity data not seen as empty")
	}
	if emptyBlockEntity(map[string]any{"x": int32(1), "y": int32(2), "z": int32(3), "id": "Chest"}) {
		t.Error("block entity with an id seen as empty")
	}
}
