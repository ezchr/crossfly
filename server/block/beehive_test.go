package block

import (
	"context"
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// fakeBee stands in for a bee: the fake host only reads whether it carries nectar.
type fakeBee struct {
	world.Entity
	nectar bool
	pos    mgl64.Vec3
}

// fakeBeeHost is a BeehiveHost without mobs: it records what the hive does with its bees.
type fakeBeeHost struct {
	released []fakeBee
	homes    []cube.Pos
	events   []string
}

func (*fakeBeeHost) SpawnMob(*world.Tx, string, mgl64.Vec3, MobSpawnOptions) (world.Entity, bool) {
	return nil, false
}

func (*fakeBeeHost) StoreMob(_ *world.Tx, e world.Entity) (string, map[string]any, bool) {
	return "minecraft:bee", map[string]any{"mob:definition": map[string]any{}, "nectar": boolByte(e.(fakeBee).nectar)}, true
}

func (h *fakeBeeHost) ReleaseMob(_ *world.Tx, _ string, data map[string]any, pos mgl64.Vec3, _ float64, home cube.Pos) (world.Entity, bool) {
	b := fakeBee{nectar: data["nectar"] == uint8(1), pos: pos}
	h.released = append(h.released, b)
	h.homes = append(h.homes, home)
	return b, true
}

func (*fakeBeeHost) HasNectar(e world.Entity) bool { return e.(fakeBee).nectar }

func (h *fakeBeeHost) FireEvent(_ world.Entity, event string) { h.events = append(h.events, event) }

// withFakeBeeHost installs a fake bee host for the test.
func withFakeBeeHost(t *testing.T) *fakeBeeHost {
	prev := Mobs()
	h := &fakeBeeHost{}
	SetMobHost(h)
	t.Cleanup(func() { SetMobHost(prev) })
	return h
}

// beeUser is a player-like user: a game mode and a held item.
type beeUser struct {
	cauldronUser
	mode world.GameMode
}

func (u *beeUser) GameMode() world.GameMode { return u.mode }

// beeDrops records the items a test world drops.
var beeDrops []item.Stack

// beeDropType is the entity type of the dropped items of a test world.
type beeDropType struct{}

func (beeDropType) Open(_ *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	return beeDropEntity{h: h, d: d}
}
func (beeDropType) EncodeEntity() string                        { return "test:bee_drop" }
func (beeDropType) BBox(world.Entity) cube.BBox                 { return cube.Box(0, 0, 0, 0.25, 0.25, 0.25) }
func (beeDropType) DecodeNBT(map[string]any, *world.EntityData) {}
func (beeDropType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

type beeDropEntity struct {
	h *world.EntityHandle
	d *world.EntityData
}

func (e beeDropEntity) Close() error            { return nil }
func (e beeDropEntity) H() *world.EntityHandle  { return e.h }
func (e beeDropEntity) Position() mgl64.Vec3    { return e.d.Pos }
func (e beeDropEntity) Rotation() cube.Rotation { return e.d.Rot }

type beeDropConfig struct{}

func (beeDropConfig) Apply(*world.EntityData) {}

func beehiveWorld(t *testing.T, f func(tx *world.Tx)) {
	t.Helper()
	beeDrops = nil
	reg := world.EntityRegistryConfig{
		Item: func(opts world.EntitySpawnOpts, it any) *world.EntityHandle {
			beeDrops = append(beeDrops, it.(item.Stack))
			return opts.New(beeDropType{}, beeDropConfig{})
		},
	}.New([]world.EntityType{beeDropType{}})
	w := world.Config{Entities: reg}.New()
	t.Cleanup(func() { _ = w.Close() })
	w.StopTime()
	w.SetTime(6000)
	if err := w.Do(f).Wait(context.Background()); err != nil {
		t.Error(err)
		return
	}
}

func countEvents(events []string, name string) (n int) {
	for _, e := range events {
		if e == name {
			n++
		}
	}
	return
}

// countItems counts the dropped items of it.
func countItems(it world.Item) (n int) {
	for _, s := range beeDrops {
		if s.Item() == it {
			n += s.Count()
		}
	}
	return
}

// Every state of both blocks is a Bedrock state, and both items exist.
func TestBeehiveRegistered(t *testing.T) {
	beehiveWorld(t, func(*world.Tx) {}) // finalises the block registry
	for _, nest := range []bool{false, true} {
		for _, d := range cube.Directions() {
			for level := 0; level <= 5; level++ {
				b := Beehive{Nest: nest, Facing: d, HoneyLevel: level}
				name, props := b.EncodeBlock()
				got, ok := world.BlockByName(name, props)
				if !ok || got.(Beehive).Facing != d || got.(Beehive).HoneyLevel != level || got.(Beehive).Nest != nest {
					t.Errorf("%v %v: got %#v (ok %v)", name, props, got, ok)
				}
			}
		}
	}
	for _, name := range []string{"minecraft:beehive", "minecraft:bee_nest"} {
		if _, ok := world.ItemByName(name, 0); !ok {
			t.Errorf("item %v not registered", name)
		}
	}
}

// A hive takes three bees (600 ticks, 2400 with nectar); the fourth gets minecraft:hive_full. The block entity
// keeps them through NBT.
func TestBeehiveAdmitAndNBT(t *testing.T) {
	host := withFakeBeeHost(t)
	pos := cube.Pos{0, 64, 0}
	beehiveWorld(t, func(tx *world.Tx) {
		tx.SetBlock(pos, Beehive{Facing: cube.South}, nil)
		for i := range 4 {
			ExecuteMobEvent(tx, pos, "minecraft:bee_returned_to_hive", fakeBee{nectar: i == 0})
		}
		h := tx.Block(pos).(Beehive)
		occ := h.Occupants()
		if len(occ) != 3 || occ[0].TicksLeft != 2400 || occ[1].TicksLeft != 600 || occ[0].Identifier != "minecraft:bee" {
			t.Errorf("occupants %+v", occ)
			return
		}
		if countEvents(host.events, "minecraft:hive_full") != 1 {
			t.Errorf("events %v", host.events)
		}
		ExecuteMobEvent(tx, pos, "minecraft:other_event", fakeBee{})
		m := h.EncodeNBT()
		if m["id"] != "Beehive" || len(m["Occupants"].([]any)) != 3 {
			t.Errorf("nbt %v", m)
			return
		}
		o := m["Occupants"].([]any)[0].(map[string]any)
		if o["ActorIdentifier"] != "minecraft:bee" || o["TicksLeftToStay"] != int32(2400) || o["SaveData"] == nil {
			t.Errorf("occupant nbt %v", o)
		}
		back := (Beehive{}).DecodeNBT(m).(Beehive)
		if got := back.Occupants(); len(got) != 3 || got[0].TicksLeft != 2400 {
			t.Errorf("decoded %+v", got)
		}
	})
}

// Bees stay their time, then leave in the day through the free entrance with the hive as their home; a bee with
// nectar fills the hive with honey. At night or with the entrance blocked they stay.
func TestBeehiveRelease(t *testing.T) {
	host := withFakeBeeHost(t)
	pos := cube.Pos{0, 64, 0}
	beehiveWorld(t, func(tx *world.Tx) {
		tx.SetBlock(pos, Beehive{Facing: cube.East}, nil)
		ExecuteMobEvent(tx, pos, "minecraft:bee_returned_to_hive", fakeBee{nectar: true})
		ExecuteMobEvent(tx, pos, "minecraft:bee_returned_to_hive", fakeBee{})
		tick := func(n int) {
			for i := 0; i < n; i++ {
				tx.Block(pos).(Beehive).Tick(int64(i), pos, tx)
			}
		}
		tx.SetBlock(pos.Side(cube.FaceEast), Stone{}, nil)
		tick(3000)
		if len(host.released) != 0 {
			t.Errorf("bees left through a blocked entrance: %v", host.released)
			return
		}
		tx.SetBlock(pos.Side(cube.FaceEast), nil, nil)
		tx.World().SetTime(18000)
		tick(200)
		if len(host.released) != 0 {
			t.Errorf("bees left at night")
			return
		}
		tx.World().SetTime(6000)
		tick(400)
		if len(host.released) != 2 || len(tx.Block(pos).(Beehive).Occupants()) != 0 {
			t.Errorf("released %d bees", len(host.released))
			return
		}
		want := pos.Side(cube.FaceEast).Vec3().Add(mgl64.Vec3{0.5, 0.5, 0.5})
		for i, b := range host.released {
			if b.pos != want || host.homes[i] != pos {
				t.Errorf("bee left at %v with home %v", b.pos, host.homes[i])
			}
		}
		if countEvents(host.events, "minecraft:exited_hive") != 2 {
			t.Errorf("events %v", host.events)
		}
		if l := tx.Block(pos).(Beehive).HoneyLevel; l < 1 || l > 2 {
			t.Errorf("honey level %d after one bee with nectar", l)
		}
	})
}

// A full hive gives three honeycombs to shears and a honey bottle to a glass bottle. Without a lit campfire below
// the bees come out angry; with one they stay.
func TestBeehiveHarvest(t *testing.T) {
	host := withFakeBeeHost(t)
	pos := cube.Pos{0, 70, 0}
	beehiveWorld(t, func(tx *world.Tx) {
		use := func(held item.Stack) (bool, *item.UseContext) {
			ctx := &item.UseContext{}
			return tx.Block(pos).(Beehive).Activate(pos, cube.FaceSouth, tx, &cauldronUser{held: held}, ctx), ctx
		}
		tx.SetBlock(pos, Beehive{Facing: cube.South, HoneyLevel: 4}, nil)
		if ok, _ := use(item.NewStack(item.Shears{}, 1)); ok {
			t.Error("harvested a hive that is not full")
			return
		}
		tx.SetBlock(pos, Beehive{Facing: cube.South, HoneyLevel: 5}, nil)
		ExecuteMobEvent(tx, pos, "minecraft:bee_returned_to_hive", fakeBee{})
		if ok, _ := use(item.NewStack(item.Shears{}, 1)); !ok || tx.Block(pos).(Beehive).HoneyLevel != 0 {
			t.Error("shears did not harvest")
			return
		}
		if n := countItems(item.Honeycomb{}); n != 3 {
			t.Errorf("%d honeycombs dropped", n)
		}
		if countEvents(host.events, "minecraft:exited_disturbed_hive") != 1 {
			t.Errorf("no campfire: events %v", host.events)
		}

		host.events = nil
		tx.SetBlock(pos.Sub(cube.Pos{0, 3, 0}), Campfire{}, nil)
		tx.SetBlock(pos, Beehive{Facing: cube.South, HoneyLevel: 5, bees: tx.Block(pos).(Beehive).bees}, nil)
		ExecuteMobEvent(tx, pos, "minecraft:bee_returned_to_hive", fakeBee{})
		ok, ctx := use(item.NewStack(item.GlassBottle{}, 1))
		if !ok || ctx.CountSub != 1 || ctx.NewItem.Item() != (item.HoneyBottle{}) || tx.Block(pos).(Beehive).HoneyLevel != 0 {
			t.Errorf("bottle: ok %v ctx %+v", ok, ctx)
			return
		}
		if len(host.events) != 0 || len(tx.Block(pos).(Beehive).Occupants()) != 1 {
			t.Errorf("bees left over a lit campfire: %v", host.events)
		}

		tx.SetBlock(pos.Sub(cube.Pos{0, 3, 0}), Campfire{Extinguished: true}, nil)
		tx.SetBlock(pos, Beehive{Facing: cube.South, HoneyLevel: 5, bees: tx.Block(pos).(Beehive).bees}, nil)
		use(item.NewStack(item.GlassBottle{}, 1))
		if countEvents(host.events, "minecraft:exited_disturbed_hive") != 1 {
			t.Errorf("extinguished campfire: events %v", host.events)
		}
	})
}

// Silk touch keeps the bees and honey in the item, creative mode drops the hive with its bees, any other break lets
// the bees out angry. A bee nest drops nothing without silk touch.
func TestBeehiveBreak(t *testing.T) {
	host := withFakeBeeHost(t)
	pos := cube.Pos{0, 64, 0}
	silk := item.NewStack(item.Axe{Tier: item.ToolTierIron}, 1).WithEnchantments(item.NewEnchantment(enchantment.SilkTouch, 1))
	beehiveWorld(t, func(tx *world.Tx) {
		fill := func(nest bool) Beehive {
			tx.SetBlock(pos, Beehive{Nest: nest, Facing: cube.North, HoneyLevel: 3}, nil)
			ExecuteMobEvent(tx, pos, "minecraft:bee_returned_to_hive", fakeBee{nectar: true})
			ExecuteMobEvent(tx, pos, "minecraft:bee_returned_to_hive", fakeBee{})
			return tx.Block(pos).(Beehive)
		}
		h := fill(false)
		drops := h.BreakInfo().Drops(item.Axe{Tier: item.ToolTierIron}, silk.Enchantments())
		if len(drops) != 1 {
			t.Errorf("silk touch drops %v", drops)
			return
		}
		got := drops[0].Item().(Beehive)
		if got.HoneyLevel != 3 || len(got.Occupants()) != 2 {
			t.Errorf("silk touch item: honey %d, %d bees", got.HoneyLevel, len(got.Occupants()))
		}
		if back := got.DecodeNBT(item.WriteNBT(drops[0], true)["tag"].(map[string]any)).(Beehive); back.HoneyLevel != 3 || len(back.Occupants()) != 2 {
			t.Errorf("item nbt: honey %d, %d bees", back.HoneyLevel, len(back.Occupants()))
		}
		tx.SetBlock(pos, nil, nil)
		h.BreakInfo().BreakHandler(pos, tx, &beeUser{cauldronUser: cauldronUser{held: silk}, mode: world.GameModeSurvival})
		if len(host.released) != 0 {
			t.Errorf("silk touch let the bees out")
		}

		h = fill(false)
		if d := h.BreakInfo().Drops(item.ToolNone{}, nil); len(d) != 1 || len(d[0].Item().(Beehive).Occupants()) != 0 || d[0].Item().(Beehive).HoneyLevel != 0 {
			t.Errorf("plain drops %v", d)
		}
		tx.SetBlock(pos, nil, nil)
		h.BreakInfo().BreakHandler(pos, tx, &beeUser{mode: world.GameModeSurvival})
		if len(host.released) != 2 || countEvents(host.events, "minecraft:exited_disturbed_hive") != 2 {
			t.Errorf("break: released %d, events %v", len(host.released), host.events)
		}

		h = fill(true)
		if d := h.BreakInfo().Drops(item.ToolNone{}, nil); len(d) != 0 {
			t.Errorf("bee nest dropped %v without silk touch", d)
		}
		tx.SetBlock(pos, nil, nil)
		h.BreakInfo().BreakHandler(pos, tx, &beeUser{mode: world.GameModeCreative})
		if len(host.released) != 2 {
			t.Errorf("creative break let the bees out")
		}
		found := false
		for _, d := range beeDrops {
			if n, ok := d.Item().(Beehive); ok && n.Nest && len(n.Occupants()) == 2 && n.HoneyLevel == 3 {
				found = true
			}
		}
		if !found {
			t.Errorf("creative break dropped no bee nest with its bees")
		}
	})
}

// Fire next to a hive drives the bees out (minecraft:exited_hive_on_fire).
func TestBeehiveFire(t *testing.T) {
	host := withFakeBeeHost(t)
	pos := cube.Pos{0, 64, 0}
	beehiveWorld(t, func(tx *world.Tx) {
		tx.SetBlock(pos, Beehive{Facing: cube.West}, nil)
		ExecuteMobEvent(tx, pos, "minecraft:bee_returned_to_hive", fakeBee{})
		Fire{}.burn(pos.Side(cube.FaceUp), pos, tx, rand.New(rand.NewPCG(1, 2)), 300)
		if len(host.released) != 1 || countEvents(host.events, "minecraft:exited_hive_on_fire") != 1 {
			t.Errorf("released %d, events %v", len(host.released), host.events)
		}
	})
}

// A hive placed from an item keeps a copy of its bees and faces the player.
func TestBeehivePlace(t *testing.T) {
	withFakeBeeHost(t)
	pos := cube.Pos{0, 64, 0}
	beehiveWorld(t, func(tx *world.Tx) {
		tx.SetBlock(pos, Beehive{}, nil)
		ExecuteMobEvent(tx, pos, "minecraft:bee_returned_to_hive", fakeBee{})
		it := tx.Block(pos).(Beehive).withOccupantsItem()
		tx.SetBlock(pos.Side(cube.FaceDown), Stone{}, nil)
		tx.SetBlock(pos, nil, nil)
		ctx := &item.UseContext{}
		it.UseOnBlock(pos.Side(cube.FaceDown), cube.FaceUp, mgl64.Vec3{}, tx, &cauldronUser{}, ctx)
		placed, ok := tx.Block(pos).(Beehive)
		if !ok {
			t.Errorf("not placed")
			return
		}
		if len(placed.Occupants()) != 1 || placed.bees == it.bees || placed.Facing != cube.North {
			t.Errorf("placed %+v", placed)
		}
	})
}
