package session

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// Item NBT a client sends is untrusted: stackToItem turns it into an item for every item stack in
// MobEquipment, InventoryTransaction and PlayerAuthInput. These tests give every registered item
// NBT of the wrong shape and range, at the top level and inside items held by other items, and
// require that nothing panics while the item is decoded, compared, sent back, saved or placed.

// hostileKeys are the keys item and block item NBT decoders read.
var hostileKeys = strings.Fields(`ActorIdentifier Age AttachedBlocks BackText Base BurnDuration BurnTime Color CookTime
	Count CustomColor CustomName Damage DecoratedPot Delay DisplayEntityHeight DisplayEntityScale DisplayEntityWidth
	EntityIdentifier ExitPortal Expires Explosions FireworkColor FireworkFade FireworkFlicker FireworkTrail
	FireworkType Fireworks FireworksItem Flight FrontText FuelAmount FuelTotal HoneyLevel IgnoreLighting IsWaxed Item
	Item1 Item2 Item3 Item4 ItemDropChance ItemRotation ItemTime Items LastProgress Levels MaxNearbyEntities
	MaxSpawnDelay MinSpawnDelay Name Occupants OutputSignal Pattern Patterns Pose Power Primary Progress RecordItem
	RepairCost RequiredPlayerRange Rotation SaveData Secondary ShouldSpawnBees SignTextColor SkullType Slot SpawnCount
	SpawnRange State StoredXPInt Text TextIgnoreLegacyBugResolved TextOwner TicksLeftToStay TransferCooldown Type
	Unbreakable author book chargedItem color customColor dfFacing dfLastProgress dfPowered dfProgress dfSource
	dfStarted dfWaterlogged df_dust_dot display dragonflyData ench expanding facing flower_pos generation id item
	last_vibration_frequency lvl map_uuid min_ticks_in_hive movingBlock movingEntity note page pages pairx pairz
	pistonPosX pistonPosY pistonPosZ power powered sherds tag text ticks_in_hive title warning_level`)

// commonItemKeys are the tags item.ReadNBT reads for every item.
var commonItemKeys = []string{"Damage", "RepairCost", "display", "dragonflyData", "ench", "Unbreakable"}

// hostileValues are values of every NBT type, in and out of the ranges decoders expect.
func hostileValues() []any {
	return []any{
		"x", "minecraft:stone", "", uint8(0), uint8(7), uint8(16), uint8(255), int16(-1), int16(32767),
		int32(-1), int32(4), int32(6), int32(7), int32(99), int32(-2147483648), int32(2147483647),
		int64(-1), int64(1 << 40), float32(-1), float32(1e30), float64(-1),
		[]any{}, []any{"x"}, []any{uint8(1)}, []any{int32(1), int32(2)}, []any{[]any{"x"}},
		[]any{map[string]any{}}, []any{map[string]any{"text": int32(1), "Pattern": "zz", "Slot": "x", "Name": int32(1)}},
		[]any{map[string]any{"Name": "minecraft:written_book", "Count": uint8(1), "tag": "x"}},
		map[string]any{}, map[string]any{"Lore": []any{int32(1)}, "Name": int32(1), "Explosions": []any{"x"}, "text": int32(1)},
		map[string]any{"Name": "minecraft:stone", "Count": "x", "tag": []any{"x"}, "Block": "x"},
		map[string]any{"Name": int32(1), "Block": map[string]any{"name": int32(1), "states": "x"}},
		[]byte{}, []byte{0, 1, 2}, [3]byte{1, 2, 3}, []int32{1}, [2]int32{1, 2}, [3]int32{1, 2, 3}, [0]int32{},
		[]int64{1}, [4]int64{1, 2, 3, 4},
	}
}

// hostileFailures collects distinct panics: phase and message -> the first input that caused it.
type hostileFailures map[string]string

func (f hostileFailures) try(typ, what, phase string, fn func()) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			msg := fmt.Sprint(r)
			if len(msg) > 120 {
				msg = msg[:120]
			}
			key := phase + " " + typ + ": " + msg
			if _, seen := f[key]; !seen {
				f[key] = what
			}
			ok = false
		}
	}()
	fn()
	return true
}

func (f hostileFailures) report(t *testing.T) {
	t.Helper()
	if len(f) == 0 {
		return
	}
	keys := make([]string, 0, len(f))
	for k := range f {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Errorf("%s\n\tfirst: %s", k, f[k])
	}
	t.Fatalf("%d distinct panics", len(f))
}

// checkHostileStack converts the network stack and uses the result like the server does.
func checkHostileStack(f hostileFailures, what string, st protocol.ItemStack) {
	var s item.Stack
	typ := "?"
	if it, ok := world.ItemByRuntimeID(st.NetworkID, int16(st.MetadataValue)); ok {
		typ = fmt.Sprintf("%T", it)
	}
	if !f.try(typ, what, "decode", func() { s = stackToItem(world.DefaultBlockRegistry, st) }) {
		return
	}
	if s.Empty() {
		return
	}
	f.try(typ, what, "compare", func() { _ = s.Comparable(s); _ = s.Equal(s) })
	f.try(typ, what, "send", func() { _ = instanceFromItem(world.DefaultBlockRegistry, s) })
	f.try(typ, what, "save", func() { _ = item.ReadNBT(item.WriteNBT(s, true), nil) })
	if b, ok := s.Item().(world.Block); ok {
		f.try(typ, what, "place", func() {
			_, _ = b.EncodeBlock()
			_ = world.BlockRuntimeID(b)
			if n, ok := b.(world.NBTer); ok {
				_ = n.EncodeNBT()
			}
			if n, ok := b.(interface{ EncodeNetworkNBT() map[string]any }); ok {
				_ = n.EncodeNetworkNBT()
			}
		})
	}
}

func networkStack(it world.Item, tag map[string]any) (protocol.ItemStack, bool) {
	id, meta, ok := world.ItemRuntimeID(it)
	if !ok {
		return protocol.ItemStack{}, false
	}
	return protocol.ItemStack{ItemType: protocol.ItemType{NetworkID: id, MetadataValue: uint32(meta)}, Count: 1, NBTData: tag}, true
}

// nbtItems returns one registered item of each type that decodes NBT of its own (colours and other
// variants decode the same), sorted by name.
func nbtItems() []world.Item {
	var out []world.Item
	seen := map[string]bool{}
	for _, it := range world.Items() {
		if _, ok := it.(world.NBTer); ok && !seen[fmt.Sprintf("%T", it)] {
			seen[fmt.Sprintf("%T", it)] = true
			out = append(out, it)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, am := out[i].EncodeItem()
		b, bm := out[j].EncodeItem()
		return a < b || a == b && am < bm
	})
	return out
}

func hostileItemName(it world.Item) string {
	n, m := it.EncodeItem()
	if m != 0 {
		return fmt.Sprintf("%s:%d", n, m)
	}
	return n
}

func TestHostileItemNBTTopLevel(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	f := hostileFailures{}
	vals := hostileValues()
	for _, it := range world.Items() {
		keys := hostileKeys
		if _, ok := it.(world.NBTer); !ok {
			// Items without NBT of their own only read the tags every item has.
			keys = commonItemKeys
		}
		for _, k := range keys {
			for _, v := range vals {
				st, ok := networkStack(it, map[string]any{k: v})
				if !ok {
					continue
				}
				checkHostileStack(f, fmt.Sprintf("%s {%s: %T %v}", hostileItemName(it), k, v, v), st)
			}
		}
	}
	f.report(t)
}

// hostileInner are item tags known to have broken an item decoder, for items held in items.
func hostileInner() []map[string]any {
	return []map[string]any{
		{"dragonflyData": []byte{0}},
		{"dragonflyData": []byte{0x7f, 0xff, 0xff, 0xff, 0xff}},
		{"dragonflyData": "x"},
		{"display": map[string]any{"Lore": []any{map[string]any{"text": "x"}}}},
		{"display": map[string]any{"Lore": []any{int32(1)}, "Name": int32(2)}},
		{"Fireworks": map[string]any{"Explosions": []any{"x"}, "Flight": "x"}},
		{"Fireworks": map[string]any{"Explosions": []any{map[string]any{"FireworkType": uint8(200), "FireworkColor": "x"}}}},
		{"FireworksItem": map[string]any{"FireworkType": uint8(200), "FireworkColor": []any{"x"}}},
		{"pages": []any{"x"}},
		{"pages": []any{map[string]any{"text": int32(1)}}},
		{"Patterns": []any{"x"}},
		{"Patterns": []any{map[string]any{"Pattern": "zz", "Color": int32(99)}}},
		{"Base": int32(99)},
		{"sherds": []any{"minecraft:stone", int32(1), "x", "minecraft:diamond"}},
		{"sherds": []any{"x"}},
		{"color": uint8(200)},
		{"SkullType": uint8(99)},
		{"Primary": int32(6), "Secondary": int32(7)},
		{"Primary": int32(99), "Secondary": int32(-1)},
		{"Pose": int32(9)},
		{"facing": uint8(9)},
		{"ench": []any{map[string]any{"id": int16(9), "lvl": int16(-5)}}},
		{"Items": []any{"x", map[string]any{"Slot": uint8(255), "Name": "minecraft:stone", "Count": uint8(1)}}},
	}
}

var nestKeys = []string{"Items", "chargedItem", "book", "RecordItem", "Item", "item", "Item1", "Item2", "Item3", "Item4"}

func TestHostileItemNBTNested(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	f := hostileFailures{}
	items := nbtItems()
	inner := hostileInner()
	for _, outer := range items {
		for _, k := range nestKeys {
			for _, in := range items {
				name, meta := in.EncodeItem()
				for i, tag := range inner {
					nested := map[string]any{"Name": name, "Damage": meta, "Count": uint8(1), "Slot": uint8(0), "tag": tag}
					var v any = nested
					if k == "Items" {
						v = []any{nested}
					}
					st, ok := networkStack(outer, map[string]any{k: v})
					if !ok {
						continue
					}
					checkHostileStack(f, fmt.Sprintf("%s {%s: %s with hostile tag %d}", hostileItemName(outer), k, hostileItemName(in), i), st)
				}
			}
		}
	}
	f.report(t)
}

// TestHostileItemNBTDeep checks that items nested in items many levels deep neither panic nor
// overflow the stack.
func TestHostileItemNBTDeep(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	f := hostileFailures{}
	tag := map[string]any{}
	for range 2000 {
		tag = map[string]any{"chargedItem": map[string]any{"Name": "minecraft:crossbow", "Count": uint8(1), "tag": tag}}
	}
	for _, it := range nbtItems() {
		if st, ok := networkStack(it, tag); ok {
			checkHostileStack(f, hostileItemName(it)+" nested 2000 deep", st)
		}
	}
	f.report(t)
}
