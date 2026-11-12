package javasession

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	jitem "github.com/df-mc/dragonfly/server/java/protocol/item"
	"github.com/df-mc/dragonfly/server/java/protocol/version"
	"github.com/df-mc/dragonfly/server/java/protocol/wire"
	"github.com/df-mc/dragonfly/server/world"
)

// creativeBody is a set_creative_mode_slot body: the slot, then the stack in the client-to-server
// form.
func creativeBody(slot int16, js *jitem.Stack, p *jitem.Proto) []byte {
	var w wire.Writer
	w.Int16(slot)
	js.EncodeUntrustedFor(&w, p)
	return w.B
}

// rawCreativeBody is a set_creative_mode_slot body of count 1 of item id with the given components
// (type, then the value, length-prefixed).
func rawCreativeBody(slot int16, id int32, comps ...any) []byte {
	var w wire.Writer
	w.Int16(slot)
	w.VarInt(1)
	w.VarInt(id)
	w.VarInt(int32(len(comps) / 2))
	w.VarInt(0)
	for i := 0; i+1 < len(comps); i += 2 {
		w.VarInt(comps[i].(int32))
		v := comps[i+1].([]byte)
		w.VarInt(int32(len(v)))
		w.Raw(v)
	}
	return w.B
}

// creativePacket handles a set_creative_mode_slot body the way handleInventoryPacket does for a
// creative player, in the player's transaction.
func creativePacket(s *Session, st *itemState, c *itemsFakePlayer, body []byte) error {
	r := wire.NewReader(body)
	slot := int(r.Int16())
	st.in.DecodeUntrustedFor(r, st.proto)
	if r.Err != nil {
		return r.Err
	}
	s.creativeSlot(c, slot, &st.in)
	return nil
}

func allocatedBy(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// checkStoredStack does with a stack what the server does with an item a creative client put in
// its inventory: shows it to Java and Bedrock viewers, saves it and loads it again. None of it may
// panic: that would happen on the world goroutine, which no recover covers.
func checkStoredStack(t *testing.T, what string, ds item.Stack) {
	t.Helper()
	defer func() {
		if p := recover(); p != nil {
			t.Errorf("%s: %v: panic %v", what, ds, p)
		}
	}()
	if ds.Empty() {
		return
	}
	if ds.Count() < 1 || ds.Count() > max(ds.MaxCount(), 1) {
		t.Errorf("%s: count %d of %d", what, ds.Count(), ds.MaxCount())
	}
	var js jitem.Stack
	javaStack(ds, &js)
	var w wire.Writer
	js.Encode(&w)
	ds.Item().EncodeItem()
	if b, ok := ds.Item().(world.Block); ok {
		world.BlockRuntimeID(b)
		b.EncodeBlock()
	}
	if nb, ok := ds.Item().(world.NBTer); ok {
		nb.EncodeNBT()
	}
	back := item.ReadNBT(item.WriteNBT(ds, true), nil)
	if back.Empty() {
		t.Errorf("%s: %v did not survive saving", what, ds)
	}
}

// Every Java item a creative client can name, at any count, becomes a Dragonfly stack that can be
// shown, saved and loaded: nothing invalid reaches the inventory.
func TestCreativeEveryItem(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	for _, count := range []int32{1, 99, 0x7fffffff} {
		for id := int32(0); id < jitem.Items; id++ {
			js := jitem.Stack{Count: count, ID: id}
			ds, ok := dragonflyStack(&js)
			if !ok {
				continue
			}
			checkStoredStack(t, fmt.Sprintf("%s x%d", jitem.Name(id), count), ds)
		}
	}
}

// Out-of-range values in the components dragonflyStack reads never make an invalid stack.
func TestCreativeHostileComponents(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	sword := mustJavaItem("minecraft:diamond_sword")
	book := javaEnchantedBook
	helmet := mustJavaItem("minecraft:leather_helmet")
	head := mustJavaItem("minecraft:player_head")
	var cases []jitem.Stack
	add := func(js jitem.Stack, comps ...int32) {
		for _, c := range comps {
			js.Add(c)
		}
		cases = append(cases, js)
	}
	for _, lvl := range []int32{-0x80000000, -1, 0, 1, 255, 0x7fffffff} {
		for _, eid := range []int32{-1, 0, 5, 41, 1000, 0x7fffffff} {
			add(jitem.Stack{Count: 1, ID: sword, Enchantments: []jitem.Enchantment{{ID: eid, Level: lvl}, {ID: eid, Level: lvl}}}, jitem.CompEnchantments)
			add(jitem.Stack{Count: 1, ID: book, StoredEnchantments: []jitem.Enchantment{{ID: eid, Level: lvl}}}, jitem.CompStoredEnchantments)
		}
	}
	for _, it := range []int32{javaPotion, javaSplashPotion, javaLingering, javaTippedArrow} {
		for p := int32(-2); p < 70; p++ {
			add(jitem.Stack{Count: 64, ID: it, PotionContents: jitem.PotionContents{HasPotion: true, Potion: p}}, jitem.CompPotionContents)
		}
		add(jitem.Stack{Count: 64, ID: it, PotionContents: jitem.PotionContents{HasPotion: true, Potion: 0x7fffffff,
			Effects: []jitem.Effect{{ID: -1, Amplifier: -1, Duration: -1}}}}, jitem.CompPotionContents)
	}
	for _, d := range []int32{-0x80000000, -1, 0, 1560, 1561, 1562, 0x7fffffff} {
		add(jitem.Stack{Count: 1, ID: sword, Damage: d}, jitem.CompDamage)
		add(jitem.Stack{Count: 1, ID: sword, Damage: d, MaxDamage: d}, jitem.CompDamage, jitem.CompMaxDamage)
	}
	for _, c := range []int32{-1, 0, 0x7fffffff} {
		add(jitem.Stack{Count: 1, ID: helmet, DyedColor: c}, jitem.CompDyedColor)
	}
	for _, m := range []string{"", "minecraft:stone", "\x00", "example:does_not_exist"} {
		add(jitem.Stack{Count: 1, ID: sword, ItemModel: m}, jitem.CompItemModel)
	}
	add(jitem.Stack{Count: 1, ID: sword, CustomName: jitem.Text{Text: "\xff\xfe"}, Lore: []jitem.Text{{Text: "\xed\xa0\x80"}}},
		jitem.CompCustomName, jitem.CompLore)
	// Profiles: hostile property counts and strings.
	for _, prof := range [][]byte{
		{0, 0, 0, 0xff, 0xff, 0xff, 0xff, 0x07},
		{0, 0, 0, 0x80, 0x80, 0x80, 0x80, 0x08},
		{1},
		{0, 0, 0, 1, 8, 't', 'e', 'x', 't', 'u', 'r', 'e', 's', 0xff, 0xff, 0xff, 0xff, 0x07},
		{0, 0, 0, 1, 8, 't', 'e', 'x', 't', 'u', 'r', 'e', 's', 2, 0xff, 0xfe, 0},
	} {
		js := jitem.Stack{Count: 1, ID: head}
		js.Raw = []jitem.RawComponent{{Type: jitem.CompProfile, Data: prof}}
		cases = append(cases, js)
	}
	for i := range cases {
		func() {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("case %d: panic %v", i, p)
				}
			}()
			if ds, ok := dragonflyStack(&cases[i]); ok {
				checkStoredStack(t, fmt.Sprintf("case %d", i), ds)
			}
		}()
	}
	// The same stacks sent by a client, of every version, into a creative inventory.
	for _, v := range version.All {
		p := jitem.For(v)
		s, st, c := itemsTestPlayer(world.GameModeCreative)
		st.proto = p
		for i := range cases {
			func() {
				defer func() {
					if p := recover(); p != nil {
						t.Errorf("%s case %d: panic %v", v.Name, i, p)
					}
				}()
				_ = creativePacket(s, st, c, creativeBody(36, &cases[i], p))
				ds, _ := st.inv.Item(0)
				checkStoredStack(t, fmt.Sprintf("%s case %d", v.Name, i), ds)
			}()
		}
	}
}

// A set_creative_mode_slot of a few bytes declaring huge lengths, or a packet of repeated
// components, is dropped without allocating anything like what it declares.
func TestCreativeSlotAllocation(t *testing.T) {
	sword := mustJavaItem("minecraft:diamond_sword")
	huge := []byte{0xff, 0xff, 0xff, 0xff, 0x07}
	small := [][]byte{
		append([]byte{0, 36, 1, 0x9a, 0x08}, append(huge, 0)...),                                             // 2^31 components
		append([]byte{0, 36, 1, 0x9a, 0x08, 1, 0, byte(jitem.CompLore)}, huge...),                            // a lore value of 2 GB
		{0, 36, 1, 0x9a, 0x08, 1, 0, byte(jitem.CompLore), 5, 0xff, 0xff, 0xff, 0xff, 0x07},                  // 2^31 lore lines
		{0, 36, 1, 0x9a, 0x08, 1, 0, byte(jitem.CompCustomData), 6, 10, 7, 0, 0, 0x7f, 0xff},                 // a cut byte array length
		{0, 36, 1, 0x9a, 0x08, 1, 0, byte(jitem.CompCustomData), 9, 10, 9, 0, 0, 10, 0x7f, 0xff, 0xff, 0xff}, // 2^31 compounds
		append([]byte{0, 36, 0xff, 0xff, 0xff, 0xff, 0x07}, huge...),                                         // count and id
	}
	s, st, c := itemsTestPlayer(world.GameModeCreative)
	for _, b := range small {
		var err error
		n := allocatedBy(func() { err = creativePacket(s, st, c, b) })
		if err == nil && b[2] != 0xff {
			t.Errorf("%x: accepted", b)
		}
		if n > 64<<10 {
			t.Errorf("%x: %d KB allocated", b, n>>10)
		}
	}
	// One lore line repeated 2048 times (8 KB): refused, at about no cost. It used to be kept, and
	// matching it against the inventory encoded every line once per repeat (12 MB here; a 1 MB
	// packet asked for terabytes, and the kernel killed the server).
	line := []byte{1, 8, 0, 0}
	comps := make([]any, 0, 2<<11)
	for range 1 << 11 {
		comps = append(comps, jitem.CompLore, line)
	}
	b := rawCreativeBody(36, sword, comps...)
	var err error
	n := allocatedBy(func() { err = creativePacket(s, st, c, b) })
	if err == nil {
		t.Errorf("repeated lore accepted: %d lines", len(st.in.Lore))
	}
	if n > 256<<10 {
		t.Errorf("repeated lore: %d KB allocated for a %d KB packet", n>>10, len(b)>>10)
	}
}
