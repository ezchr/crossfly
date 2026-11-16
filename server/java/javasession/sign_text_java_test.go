package javasession

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

func TestSignBlockEntityText(t *testing.T) {
	s := block.Sign{Front: block.SignText{Text: "§aWelcome\nto\nZID\nline4\nline5", BaseColour: item.ColourRed().SignRGBA(), Glowing: true}}
	typ, data, ok := signBlockEntity(s)
	if !ok || typ != "minecraft:sign" {
		t.Fatalf("got %q %v", typ, ok)
	}
	// Back to a named compound to read it.
	full := append([]byte{data[0], 0, 0}, data[1:]...)
	var m map[string]any
	if err := nbt.UnmarshalEncoding(full, &m, nbt.BigEndian); err != nil {
		t.Fatal(err)
	}
	front := m["front_text"].(map[string]any)
	msgs := front["messages"].([]any)
	if len(msgs) != 4 || msgs[0] != "§aWelcome" || msgs[3] != "line4" {
		t.Fatalf("messages %v", msgs)
	}
	if front["color"] != "red" || front["has_glowing_text"] != uint8(1) {
		t.Fatalf("front %v", front)
	}
	back := m["back_text"].(map[string]any)
	if back["color"] != "black" || len(back["messages"].([]any)) != 4 {
		t.Fatalf("back %v", back)
	}
	if _, _, ok := signBlockEntity(block.Stone{}); ok {
		t.Fatal("stone is not a sign")
	}
}
