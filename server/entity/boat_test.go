//go:build crossfly_full

package entity

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// boatPool fills a 9x9 pool of still water at y 60 (its surface at 60 + 8/9) over stone.
func boatPool(tx *world.Tx) {
	for x := -4; x <= 4; x++ {
		for z := -4; z <= 4; z++ {
			tx.SetBlock(cube.Pos{x, 59, z}, block.Stone{}, nil)
			tx.SetBlock(cube.Pos{x, 60, z}, block.Water{Still: true, Depth: 8}, nil)
		}
	}
}

func boatAt(tx *world.Tx, pos mgl64.Vec3, wood BoatWood, chest bool) *Boat {
	return tx.AddEntity(NewBoat(world.EntitySpawnOpts{Position: pos}, wood, chest)).(*Boat)
}

func TestBoatItemsRegistered(t *testing.T) {
	item.RegisterRedstone()
	for _, name := range []string{"minecraft:oak_boat", "minecraft:pale_oak_chest_boat", "minecraft:bamboo_raft",
		"minecraft:bamboo_chest_raft", "minecraft:cherry_boat", "minecraft:dark_oak_chest_boat"} {
		it, ok := world.ItemByName(name, 0)
		if !ok {
			t.Fatalf("%s not registered", name)
		}
		if b, ok := it.(item.Boat); !ok || b.MaxCount() != 1 {
			t.Fatalf("%s: %#v", name, it)
		}
	}
}

func TestBoatVariantPerWood(t *testing.T) {
	w := minecartWorld(t)
	doWorld(w, func(tx *world.Tx) {
		for _, wood := range item.BoatWoods() {
			for _, chest := range []bool{false, true} {
				b := boatAt(tx, mgl64.Vec3{0, 70, 0}, wood, chest)
				m := protocol.NewEntityMetadata()
				b.BedrockMetadata(m)
				if m[protocol.EntityDataKeyVariant] != int32(wood) || b.Variant() != int32(wood) {
					t.Fatalf("%v chest=%v: variant %v", wood, chest, m[protocol.EntityDataKeyVariant])
				}
				wantType := "minecraft:boat"
				if chest {
					wantType = "minecraft:chest_boat"
				}
				if b.H().Type().EncodeEntity() != wantType || b.Chest() != chest {
					t.Fatalf("%v: type %s", wood, b.H().Type().EncodeEntity())
				}
				name, _ := b.Pick().Item().EncodeItem()
				if wood == item.BoatBamboo && !chest && name != "minecraft:bamboo_raft" {
					t.Fatalf("bamboo pick %s", name)
				}
				// Saved and loaded, the boat keeps its wood.
				nbt := b.H().Type().EncodeNBT(&world.EntityData{Data: b.behaviour()})
				var data world.EntityData
				b.H().Type().DecodeNBT(nbt, &data)
				if data.Data.(*boatBehaviour).wood != wood {
					t.Fatalf("%v: nbt round trip", wood)
				}
				_ = b.Close()
			}
		}
	})
}

func TestBoatFloats(t *testing.T) {
	w := minecartWorld(t)
	doWorld(w, func(tx *world.Tx) {
		boatPool(tx)
		b := boatAt(tx, mgl64.Vec3{0.5, 63, 0.5}, item.BoatOak, false)
		for i := int64(0); i < 200; i++ {
			b.Tick(tx, i)
		}
		surface := 60 + 8.0/9
		y := b.Position()[1]
		if y < surface-0.5 || y > surface || math.Abs(b.Velocity()[1]) > 0.01 {
			t.Fatalf("boat rests at y %v (vel %v), surface %v", y, b.Velocity(), surface)
		}
		// It stays there.
		for i := int64(200); i < 260; i++ {
			b.Tick(tx, i)
		}
		if math.Abs(b.Position()[1]-y) > 0.01 {
			t.Fatalf("boat drifted from %v to %v", y, b.Position()[1])
		}
		// A boat on land rests on the ground.
		tx.SetBlock(cube.Pos{10, 59, 10}, block.Stone{}, nil)
		l := boatAt(tx, mgl64.Vec3{10.5, 62, 10.5}, item.BoatOak, false)
		for i := int64(0); i < 80; i++ {
			l.Tick(tx, i)
		}
		if math.Abs(l.Position()[1]-60) > 1e-6 {
			t.Fatalf("boat on land at %v", l.Position())
		}
	})
}

func TestBoatBreaksAndDrops(t *testing.T) {
	w := minecartWorld(t)
	doWorld(w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 59, 0}, block.Stone{}, nil)
		b := boatAt(tx, mgl64.Vec3{0.5, 60, 0.5}, item.BoatSpruce, false)
		b.SetNameTag("Dinghy")
		for i := 0; i < 3; i++ {
			HurtEntity(b, 1, AttackDamageSource{})
			if b.behaviour().removed {
				t.Fatalf("broke after %d hits", i+1)
			}
		}
		if hurt, _, _ := b.MinecartWobble(); hurt != 10 {
			t.Fatalf("hurt time %d", hurt)
		}
		HurtEntity(b, 1, AttackDamageSource{})
		if !b.behaviour().removed {
			t.Fatal("not broken after 4 hits")
		}
		drops := boatDrops(tx)
		if len(drops) != 1 || drops[0].Item() != (item.Boat{Wood: item.BoatSpruce}) || drops[0].CustomName() != "Dinghy" {
			t.Fatalf("drops %v", drops)
		}
	})
	doWorld(w, func(tx *world.Tx) {
		for e := range tx.Entities() {
			_ = e.(interface{ Close() error }).Close()
		}
		c := boatAt(tx, mgl64.Vec3{20.5, 60, 0.5}, item.BoatCherry, true)
		_, _ = c.Inventory().AddItem(item.NewStack(item.Apple{}, 5))
		HurtEntity(c, 10, AttackDamageSource{})
		drops := boatDrops(tx)
		if len(drops) != 2 {
			t.Fatalf("chest boat drops %v", drops)
		}
	})
}

func TestBoatLavaBreaks(t *testing.T) {
	w := minecartWorld(t)
	doWorld(w, func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 59, 0}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{0, 60, 0}, block.Lava{Still: true, Depth: 8}, nil)
		b := boatAt(tx, mgl64.Vec3{0.5, 60.1, 0.5}, item.BoatOak, false)
		for i := int64(0); i < 12 && !b.behaviour().removed; i++ {
			b.Tick(tx, i)
		}
		if !b.behaviour().removed {
			t.Fatal("boat survived lava")
		}
	})
}

func TestChestBoatWindow(t *testing.T) {
	w := minecartWorld(t)
	doWorld(w, func(tx *world.Tx) {
		c := boatAt(tx, mgl64.Vec3{0.5, 60, 0.5}, item.BoatOak, true)
		win, ok := WindowOf(c, c, WindowLayoutBedrock)
		if !ok || !win.Chest || win.ChestSlots != 27 || win.Inventory.Size() != 27 || len(win.Equip) != 0 {
			t.Fatalf("window %+v %v", win, ok)
		}
		if len(c.SeatPositions()) != 1 {
			t.Fatalf("chest boat seats %v", c.SeatPositions())
		}
		b := boatAt(tx, mgl64.Vec3{5.5, 60, 0.5}, item.BoatOak, false)
		if _, ok := WindowOf(b, b, WindowLayoutBedrock); ok {
			t.Fatal("plain boat has a window")
		}
	})
}

func boatDrops(tx *world.Tx) []item.Stack {
	var s []item.Stack
	for e := range tx.Entities() {
		if i, ok := e.(*Ent); ok {
			if ib, ok := i.Behaviour().(*ItemBehaviour); ok {
				s = append(s, ib.Item())
			}
		}
	}
	return s
}
