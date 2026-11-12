package item

import (
	"bytes"
	"encoding/gob"
	"reflect"
	"slices"
	"testing"
)

func stick() *Stack {
	s := NewStack(Stick{}, 1)
	return &s
}

// Item NBT comes from client packets: a field of the wrong shape is ignored, never a panic.

func TestReadNBTIgnoresBadDragonflyData(t *testing.T) {
	for name, v := range map[string]any{
		"empty":       []byte{},
		"truncated":   []byte{0},
		"garbage":     []byte{0x7f, 0xff, 0xff, 0xff, 0xff, 1, 2, 3},
		"string":      "x",
		"int":         int32(1),
		"list of any": []any{uint8(1), "x"},
		"byte array":  [3]byte{1, 2, 3},
		"int array":   [2]int32{1, 2},
	} {
		t.Run(name, func(t *testing.T) {
			s := ReadNBT(map[string]any{"dragonflyData": v}, stick())
			if len(s.Values()) != 0 {
				t.Fatalf("values read from bad data: %v", s.Values())
			}
		})
	}
}

func TestReadNBTDragonflyDataForms(t *testing.T) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode([]mapValue{{K: "k", V: "v"}}); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	list := make([]any, len(raw))
	for i, b := range raw {
		list[i] = b
	}
	// A TAG_Byte_Array decodes to a [N]byte array.
	arr := reflect.New(reflect.ArrayOf(len(raw), reflect.TypeFor[byte]())).Elem()
	reflect.Copy(arr, reflect.ValueOf(raw))
	for name, v := range map[string]any{"bytes": raw, "list": list, "array": arr.Interface()} {
		s := ReadNBT(map[string]any{"dragonflyData": v}, stick())
		if got, _ := s.Value("k"); got != "v" {
			t.Errorf("%s: value = %v", name, got)
		}
	}
}

func TestReadNBTLoreSkipsNonStrings(t *testing.T) {
	s := ReadNBT(map[string]any{"display": map[string]any{
		"Name": int32(5),
		"Lore": []any{"a", map[string]any{"text": "x"}, int32(1), "b"},
	}}, stick())
	if !slices.Equal(s.Lore(), []string{"a", "b"}) || s.CustomName() != "" {
		t.Fatalf("lore %q, name %q", s.Lore(), s.CustomName())
	}
}

func TestWriteNBTDamageOfAnotherType(t *testing.T) {
	// An item whose own NBT holds Damage of another type must not panic writeDamage.
	m := map[string]any{"Damage": "x"}
	writeDamage(m, NewStack(Sword{Tier: ToolTierIron}, 1).Damage(3), true)
	if m["Damage"] != int16(3) {
		t.Fatalf("damage %v", m["Damage"])
	}
}

func TestWrittenBookBadPages(t *testing.T) {
	b := WrittenBook{}.DecodeNBT(map[string]any{"pages": []any{
		map[string]any{"text": "one"}, "x", map[string]any{}, map[string]any{"text": int32(1)}, uint8(1),
		map[string]any{"text": "six"},
	}}).(WrittenBook)
	if !slices.Equal(b.Pages, []string{"one", "", "", "", "", "six"}) {
		t.Fatalf("pages %q", b.Pages)
	}
}

func TestFireworkBadExplosions(t *testing.T) {
	f := Firework{}.DecodeNBT(map[string]any{"Fireworks": map[string]any{
		"Explosions": []any{"x", int32(1), map[string]any{"FireworkType": uint8(200), "FireworkColor": "x"}, []any{}},
	}}).(Firework)
	if len(f.Explosions) != 1 {
		t.Fatalf("explosions %v", f.Explosions)
	}
}

func TestReadNBTNestedBadItem(t *testing.T) {
	// The disk form, as items inside items are read: a bad tag and a tag of the wrong type.
	for _, tag := range []any{"x", []any{int32(1)}, map[string]any{"dragonflyData": []byte{0}, "display": map[string]any{"Lore": []any{int32(1)}}}} {
		s := ReadNBT(map[string]any{"Name": "minecraft:written_book", "Count": uint8(1), "tag": tag}, nil)
		if _, ok := s.Item().(WrittenBook); !ok {
			t.Fatalf("tag %v: item %#v", tag, s.Item())
		}
	}
}
