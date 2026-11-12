package item

import (
	"bytes"
	"runtime"
	"testing"

	"github.com/df-mc/dragonfly/server/java/protocol/version"
	"github.com/df-mc/dragonfly/server/java/protocol/wire"
)

// untrusted builds a client-to-server stack (count 1 of item 1) with the given added components,
// each a type and its length-prefixed value, and removed components.
func untrusted(added [][2]any, removed ...int32) []byte {
	var w wire.Writer
	w.VarInt(1)
	w.VarInt(1)
	w.VarInt(int32(len(added)))
	w.VarInt(int32(len(removed)))
	for _, c := range added {
		w.VarInt(c[0].(int32))
		v := c[1].([]byte)
		w.VarInt(int32(len(v)))
		w.Raw(v)
	}
	for _, t := range removed {
		w.VarInt(t)
	}
	return w.B
}

// repeated is n copies of one component.
func repeated(t int32, v []byte, n int) [][2]any {
	out := make([][2]any, n)
	for i := range out {
		out[i] = [2]any{t, v}
	}
	return out
}

// allocated is how many bytes f allocates.
func allocated(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// A stack repeating one component many times (vanilla keeps one value per type) is refused instead
// of growing the stack by the packet: a few MB of repeats used to cost tens of MB of slices.
func TestUntrustedRepeatedComponents(t *testing.T) {
	var list []byte
	list = append(list, 1, nbtString, 0, 0) // one empty lore line
	for _, c := range []struct {
		name string
		t    int32
		v    []byte
	}{
		{"raw rarity", CompRarity, []byte{0}},
		{"raw unit", CompGlider, nil},
		{"lore", CompLore, list},
		{"custom model data", CompCustomModelData, []byte{0, 0, 1, 0, 0}},
		{"tooltip display", CompTooltipDisplay, []byte{0, 1, 3}},
	} {
		b := untrusted(repeated(c.t, c.v, 1<<19))
		var s Stack
		var err error
		n := allocated(func() {
			r := wire.NewReader(b)
			s.DecodeUntrusted(r)
			err = r.Err
		})
		if err == nil {
			t.Errorf("%s: %d repeats accepted (%d lore lines, %d raw)", c.name, 1<<19, len(s.Lore), len(s.Raw))
		}
		if n > 1<<20 {
			t.Errorf("%s: %d KB allocated for a %d KB stack", c.name, n>>10, len(b)>>10)
		}
	}
	// One of each is fine.
	r := wire.NewReader(untrusted([][2]any{{CompRarity, []byte{1}}, {CompLore, list}}, CompDamage))
	var s Stack
	s.DecodeUntrusted(r)
	if r.Err != nil || len(s.Lore) != 1 || s.rawIndex(CompRarity) < 0 || !s.Removed.Has(CompDamage) {
		t.Errorf("one of each: %v", r.Err)
	}
}

// Whatever decodes encodes to about its own size. A lore repeated n times used to be kept in Order
// n times, each writing all n*lines lines: 1024 one-line repeats (5 KB) encoded to 3 MB.
func TestUntrustedEncodeLinear(t *testing.T) {
	b := untrusted(repeated(CompLore, []byte{1, nbtString, 0, 0}, 1024))
	var s Stack
	r := wire.NewReader(b)
	s.DecodeUntrusted(r)
	if r.Err != nil {
		return // refused: nothing to encode
	}
	var w wire.Writer
	s.EncodeUntrusted(&w)
	if len(w.B) > 4*len(b) {
		t.Errorf("a %d byte stack encodes to %d bytes", len(b), len(w.B))
	}
}

// A component count larger than there are component types is refused before anything is read.
func TestUntrustedComponentCount(t *testing.T) {
	for _, n := range [][2]int32{{ComponentTypes + 1, 0}, {0, ComponentTypes + 1}, {0x7fffffff, 0}, {-1, 0}} {
		var w wire.Writer
		w.VarInt(1)
		w.VarInt(1)
		w.VarInt(n[0])
		w.VarInt(n[1])
		w.Raw(make([]byte, 4*(ComponentTypes+2)))
		var s Stack
		r := wire.NewReader(w.B)
		s.DecodeUntrusted(r)
		if r.Err == nil {
			t.Errorf("%d/%d components accepted", n[0], n[1])
		}
	}
}

// Malformed values of every modeled component, and of the raw ones the session reads, are errors
// (or ignored), never panics, and cost about what they weigh.
func TestUntrustedHostileValues(t *testing.T) {
	deep := append([]byte{nbtList}, bytes.Repeat([]byte{nbtList, 0, 0, 0, 1}, 600)...) // lists nested 600 deep
	deepCompound := []byte{nbtCompound}
	for range 600 {
		deepCompound = append(deepCompound, nbtCompound, 0, 1, 'a')
	}
	hidden := []byte{0, 0, 1}
	for range 40 { // hidden effects nested 40 deep
		hidden = append(hidden, 0, 0, 0, 0, 0, 1)
	}
	values := []struct {
		t int32
		v []byte
	}{
		{CompCustomData, []byte{nbtCompound, nbtByteArray, 0, 1, 'a', 0x7f, 0xff, 0xff, 0xff}},
		{CompCustomData, []byte{nbtCompound, nbtList, 0, 1, 'a', nbtLong, 0x7f, 0xff, 0xff, 0xff}},
		{CompCustomData, []byte{nbtCompound, nbtList, 0, 1, 'a', nbtEnd, 0, 0, 0, 5}},
		{CompCustomData, []byte{nbtCompound, nbtIntArray, 0, 1, 'a', 0x40, 0, 0, 0}},
		{CompCustomData, []byte{nbtCompound, nbtString, 0xff, 0xff}},
		{CompCustomData, []byte{nbtCompound, 99}},
		{CompCustomData, deep},
		{CompCustomData, deepCompound},
		{CompCustomData, nil},
		{CompMaxStackSize, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}},
		{CompDamage, []byte{0x80}},
		{CompCustomName, []byte{nbtString, 0xff, 0xff, 'a'}},
		{CompCustomName, []byte{nbtString, 0, 2, 0xc0, 0x80}},
		{CompCustomName, []byte{nbtString, 0, 3, 0xed, 0xa0, 0x80}}, // a lone surrogate
		{CompCustomName, []byte{nbtString, 0, 2, 0xff, 0xfe}},
		{CompCustomName, append([]byte{nbtCompound}, deep...)},
		{CompItemName, []byte{nbtList, nbtCompound, 0x7f, 0xff, 0xff, 0xff}},
		{CompItemModel, []byte{0xff, 0xff, 0xff, 0xff, 0x07}},
		{CompItemModel, []byte{2, 0xff, 0xfe}},
		{CompLore, []byte{0xff, 0xff, 0xff, 0xff, 0x0f}},
		{CompLore, []byte{0x80, 0x80, 0x04}}, // 65536 lines
		{CompLore, []byte{2, nbtString, 0, 0}},
		{CompEnchantments, []byte{0xff, 0xff, 0x03, 1, 1}},
		{CompEnchantments, []byte{1, 0xff, 0xff, 0xff, 0xff, 0x0f, 0x80, 0x80, 0x80, 0x80, 0x08}},
		{CompStoredEnchantments, []byte{0xff, 0xff, 0xff, 0xff, 0x07}},
		{CompCustomModelData, []byte{0xff, 0xff, 0x03}},
		{CompCustomModelData, []byte{0, 0, 0xff, 0xff, 0x03, 0}},
		{CompTooltipDisplay, []byte{1, 0xff, 0xff, 0xff, 0xff, 0x07}},
		{CompPotionContents, []byte{1, 0xff, 0xff, 0xff, 0xff, 0x0f, 1, 0, 0, 0, 0, 0xff, 0xff, 0x03}},
		{CompPotionContents, hidden},
		{CompDyedColor, []byte{1, 2}},
		{CompEquippable, []byte{0xff, 0xff, 0xff, 0xff, 0x0f, 0, 0x7f, 0xff, 0xff}},
		{CompEquippable, []byte{4, 0, 0, 0, 0, 0, 1, 0xff, 0xff, 0xff, 0xff, 0x07}},
		{CompEnchantmentGlintOverride, nil},
	}
	for _, v := range values {
		b := untrusted([][2]any{{v.t, v.v}})
		var s Stack
		var err error
		n := allocated(func() {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("%s %x: panic %v", ComponentName(v.t), v.v, p)
				}
			}()
			r := wire.NewReader(b)
			s.DecodeUntrusted(r)
			err = r.Err
			if err == nil {
				// What decodes must encode again, for every version.
				var w wire.Writer
				s.EncodeUntrusted(&w)
				s.Encode(&w)
			}
		})
		if n > 64<<10 {
			t.Errorf("%s %x: %d KB allocated (err %v)", ComponentName(v.t), v.v, n>>10, err)
		}
	}
	// The same through every older version's decoder.
	for _, v := range values {
		b := untrusted([][2]any{{v.t, v.v}})
		func() {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("26.2 %s %x: panic %v", ComponentName(v.t), v.v, p)
				}
			}()
			p := For(version.ByProtocol(776))
			var s Stack
			r := wire.NewReader(b)
			s.DecodeUntrustedFor(r, p)
			if r.Err == nil {
				var w wire.Writer
				s.EncodeUntrustedFor(&w, p)
				s.EncodeFor(&w, p)
			}
		}()
	}
}

// A hashed stack (container_click) with hostile counts is an error, never a panic or a big
// allocation.
func TestHashedStackHostile(t *testing.T) {
	for _, h := range [][]byte{
		{1, 1, 1, 0xff, 0xff, 0xff, 0xff, 0x07},
		{1, 1, 1, 0xff, 0xff, 0x03, 0},
		{1, 0xff, 0xff, 0xff, 0xff, 0x0f, 1, 0, 0},
		{1},
	} {
		var hs HashedStack
		r := wire.NewReader(h)
		if n := allocated(func() { hs.Decode(r) }); n > 4<<10 {
			t.Errorf("%x: %d KB allocated", h, n>>10)
		}
	}
}
