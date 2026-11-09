//go:build crossfly_full

package entity

import (
	"context"
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The expected numbers in these tests are Java Edition 26.3's, read from a vanilla server tick by tick
// (minecart_tick_compare.py in the redstone harness).

func minecartWorld(t *testing.T) *world.World {
	t.Helper()
	reg := WithJavaRedstone(DefaultRegistry.Config()).New(append(DefaultRegistry.Types(), JavaRedstoneTypes()...))
	w := world.Config{Synchronous: true, Entities: reg, SettleSynchronousTransactions: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func doWorld(w *world.World, f func(tx *world.Tx)) { w.Do(f).Wait(context.Background()) }

// railLine lays a stone floor at y-1 and a rail at y from x0 to x1, at z 0.
func railLine(tx *world.Tx, x0, x1, y int, rail func(x int) world.Block) {
	for x := x0 - 2; x <= x1+2; x++ {
		for z := -2; z <= 2; z++ {
			tx.SetBlock(cube.Pos{x, y - 1, z}, block.Stone{}, &world.SetOpts{DisableBlockUpdates: true})
		}
	}
	for x := x0; x <= x1; x++ {
		tx.SetBlock(cube.Pos{x, y, 0}, rail(x), nil)
	}
}

func spawnCart(w *world.World, pos, vel mgl64.Vec3, kind MinecartKind) *world.EntityHandle {
	h := NewMinecart(world.EntitySpawnOpts{Position: pos, Velocity: vel}, kind)
	doWorld(w, func(tx *world.Tx) { tx.AddEntity(h) })
	return h
}

func cartState(w *world.World, h *world.EntityHandle) (pos, vel mgl64.Vec3, ok bool) {
	doWorld(w, func(tx *world.Tx) {
		if e, found := h.Entity(tx); found {
			pos, vel, ok = e.Position(), e.(*Minecart).Velocity(), true
		}
	})
	return
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestMinecartRailPos(t *testing.T) {
	w := minecartWorld(t)
	doWorld(w, func(tx *world.Tx) {
		railLine(tx, 0, 0, 0, func(int) world.Block { return block.Rail{Shape: block.RailEastWest} })
		tx.SetBlock(cube.Pos{1, 0, 0}, block.Stone{}, &world.SetOpts{DisableBlockUpdates: true})
		tx.SetBlock(cube.Pos{0, 0, 3}, block.Stone{}, &world.SetOpts{DisableBlockUpdates: true})
		tx.SetBlock(cube.Pos{1, -1, 3}, block.Stone{}, &world.SetOpts{DisableBlockUpdates: true})

		p, ok := minecartRailPos(tx, 0.3, 0.0625, 0.7)
		if !ok || !near(p[0], 0.3) || !near(p[1], 0.0625) || !near(p[2], 0.5) {
			t.Errorf("flat rail pos = %v %v, want (0.3, 0.0625, 0.5)", p, ok)
		}
		if _, ok := minecartRailPos(tx, 5.5, 0, 5.5); ok {
			t.Error("rail pos found where there is no rail")
		}
		// A rail sloping up to the east: half way along it the minecart is half a block higher than at its foot.
		tx.SetBlock(cube.Pos{1, 0, 3}, block.Rail{Shape: block.RailAscendingEast}, &world.SetOpts{DisableBlockUpdates: true})
		p, ok = minecartRailPos(tx, 1.5, 0.5, 3.5)
		if !ok || !near(p[1], 0.5625) {
			t.Errorf("slope rail pos = %v %v, want y 0.5625", p, ok)
		}
	})
}

func TestMinecartCoast(t *testing.T) {
	w := minecartWorld(t)
	doWorld(w, func(tx *world.Tx) {
		railLine(tx, 0, 9, 0, func(int) world.Block { return block.Rail{Shape: block.RailEastWest} })
	})
	h := spawnCart(w, mgl64.Vec3{0.5, 0.0625, 0.5}, mgl64.Vec3{0.3, 0, 0}, MinecartPlain)
	// Java Edition: the minecart moves by its velocity, which then keeps 0.96 of itself on plain rails.
	want := [][2]float64{{0.8, 0.288}, {1.088, 0.27647999999999995}, {1.36448, 0.26542079999999996}}
	for i, wnt := range want {
		w.AdvanceTick()
		pos, vel, _ := cartState(w, h)
		if !near(pos[0], wnt[0]) || !near(vel[0], wnt[1]) || !near(pos[1], 0.0625) || pos[2] != 0.5 {
			t.Fatalf("tick %d: pos %v vel %v, want x %v vx %v", i+1, pos, vel, wnt[0], wnt[1])
		}
	}
}

func TestMinecartSpeedLimit(t *testing.T) {
	w := minecartWorld(t)
	doWorld(w, func(tx *world.Tx) {
		railLine(tx, 0, 20, 0, func(int) world.Block { return block.Rail{Shape: block.RailEastWest} })
	})
	h := spawnCart(w, mgl64.Vec3{0.5, 0.0625, 0.5}, mgl64.Vec3{1, 0, 0}, MinecartPlain)
	w.AdvanceTick()
	if pos, vel, _ := cartState(w, h); !near(pos[0], 0.9) || !near(vel[0], 0.96) {
		t.Fatalf("pos %v vel %v, want x 0.9 (0.4 a tick at most) and vx 0.96", pos, vel)
	}
}

func TestMinecartPoweredRail(t *testing.T) {
	w := minecartWorld(t)
	doWorld(w, func(tx *world.Tx) {
		railLine(tx, 0, 20, 0, func(x int) world.Block {
			if x <= 8 {
				return block.PoweredRail{Shape: block.RailEastWest}
			}
			return block.Rail{Shape: block.RailEastWest}
		})
		tx.SetBlock(cube.Pos{0, 0, 1}, block.RedstoneBlock{}, nil)
	})
	w.AdvanceTick()
	doWorld(w, func(tx *world.Tx) {
		if r, ok := tx.Block(cube.Pos{8, 0, 0}).(block.PoweredRail); !ok || !r.Powered {
			t.Fatalf("powered rail 8 blocks from the redstone block = %#v, want powered", tx.Block(cube.Pos{8, 0, 0}))
		}
	})
	h := spawnCart(w, mgl64.Vec3{0.5, 0.0625, 0.5}, mgl64.Vec3{0.05, 0, 0}, MinecartPlain)
	// Java Edition: vx 0.108, 0.16368, 0.2171328 after the first three ticks (0.96 then +0.06).
	for i, want := range []float64{0.108, 0.16368, 0.2171328} {
		w.AdvanceTick()
		if _, vel, _ := cartState(w, h); !near(vel[0], want) {
			t.Fatalf("tick %d: vx %v, want %v", i+1, vel[0], want)
		}
	}
}

func TestMinecartPoweredRailBrake(t *testing.T) {
	w := minecartWorld(t)
	doWorld(w, func(tx *world.Tx) {
		railLine(tx, 0, 10, 0, func(int) world.Block { return block.PoweredRail{Shape: block.RailEastWest} })
	})
	h := spawnCart(w, mgl64.Vec3{0.5, 0.0625, 0.5}, mgl64.Vec3{0.4, 0, 0}, MinecartPlain)
	for i := 0; i < 10; i++ {
		w.AdvanceTick()
	}
	if _, vel, _ := cartState(w, h); vel != (mgl64.Vec3{}) {
		t.Fatalf("velocity after braking %v, want zero", vel)
	}
}

func TestDetectorRail(t *testing.T) {
	w := minecartWorld(t)
	det := cube.Pos{5, 0, 0}
	lamp := cube.Pos{5, -1, 0}
	doWorld(w, func(tx *world.Tx) {
		railLine(tx, 0, 13, 0, func(x int) world.Block {
			if x == 5 {
				return block.DetectorRail{Shape: block.RailEastWest}
			}
			return block.Rail{Shape: block.RailEastWest}
		})
		tx.SetBlock(lamp, block.RedstoneLamp{}, nil)
	})
	h := spawnCart(w, mgl64.Vec3{0.5, 0.0625, 0.5}, mgl64.Vec3{0.3, 0, 0}, MinecartPlain)
	powered := func() (rail, lit bool) {
		doWorld(w, func(tx *world.Tx) {
			r, _ := tx.Block(det).(block.DetectorRail)
			l, _ := tx.Block(lamp).(block.RedstoneLamp)
			rail, lit = r.Powered, l.Lit
		})
		return
	}
	// Java Edition: on at tick 21 (the minecart's centre past x 4.71), off again at the next 20 tick check, tick 41,
	// after the minecart left it.
	on := -1
	for tick := 1; tick <= 70; tick++ {
		w.AdvanceTick()
		r, l := powered()
		if r && on < 0 {
			if on = tick; on != 21 {
				t.Fatalf("detector rail turned on at tick %d, want 21", tick)
			}
		}
		if on > 0 && !r {
			if tick != 41 {
				t.Fatalf("detector rail turned off at tick %d, want 41", tick)
			}
			return
		}
		if r != l {
			t.Fatalf("tick %d: detector rail %v, lamp under it %v", tick, r, l)
		}
	}
	_, _, _ = cartState(w, h)
	t.Fatalf("detector rail on at %d never turned off", on)
}

func TestDetectorRailComparatorChestMinecart(t *testing.T) {
	w := minecartWorld(t)
	det := cube.Pos{5, 0, 0}
	doWorld(w, func(tx *world.Tx) {
		railLine(tx, 4, 7, 0, func(x int) world.Block {
			if x == 5 {
				return block.DetectorRail{Shape: block.RailEastWest}
			}
			return block.Rail{Shape: block.RailEastWest}
		})
	})
	h := spawnCart(w, mgl64.Vec3{5.5, 0.0625, 0.5}, mgl64.Vec3{}, MinecartChest)
	doWorld(w, func(tx *world.Tx) {
		e, _ := h.Entity(tx)
		inv := e.(*Minecart).MinecartInventory()
		for i := 0; i < inv.Size(); i++ {
			_ = inv.SetItem(i, item.NewStack(block.Stone{}, 64))
		}
	})
	w.AdvanceTick()
	doWorld(w, func(tx *world.Tx) {
		if r, _ := tx.Block(det).(block.DetectorRail); !r.Powered {
			t.Fatal("detector rail under a chest minecart is not powered")
		}
		// A comparator behind the rail reads the full chest minecart as 15, once the rail's next 20 tick check tells
		// it to read the rail.
		tx.SetBlock(cube.Pos{5, 0, 1}, block.RedstoneComparator{Facing: cube.North}, nil)
	})
	for i := 0; i < 24; i++ {
		w.AdvanceTick()
	}
	doWorld(w, func(tx *world.Tx) {
		if c, _ := tx.Block(cube.Pos{5, 0, 1}).(block.RedstoneComparator); c.Output != 15 {
			t.Fatalf("comparator reading a full chest minecart on a detector rail = %d, want 15", c.Output)
		}
	})
}

func TestMinecartItemPlacesOnRail(t *testing.T) {
	w := minecartWorld(t)
	doWorld(w, func(tx *world.Tx) {
		railLine(tx, 0, 0, 0, func(int) world.Block { return block.Rail{Shape: block.RailEastWest} })
		tx.SetBlock(cube.Pos{3, 0, 0}, block.Stone{}, nil)
		user := &testStandUser{Ent: tx.AddEntity(NewArmourStand(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 0, 2.5}})).(*Ent)}
		ctx := &item.UseContext{}
		if (item.Minecart{}).UseOnBlock(cube.Pos{3, 0, 0}, cube.FaceUp, mgl64.Vec3{}, tx, user, ctx) {
			t.Error("minecart placed on stone")
		}
		if !(item.Minecart{}).UseOnBlock(cube.Pos{0, 0, 0}, cube.FaceUp, mgl64.Vec3{}, tx, user, ctx) {
			t.Fatal("minecart not placed on a rail")
		}
		found := false
		for e := range tx.Entities() {
			if c, ok := e.(*Minecart); ok {
				found = true
				if c.Position() != (mgl64.Vec3{0.5, 0.0625, 0.5}) {
					t.Errorf("placed minecart at %v, want (0.5, 0.0625, 0.5)", c.Position())
				}
			}
		}
		if !found {
			t.Error("no minecart entity after placing")
		}
	})
}

func TestMinecartBreaksAfterFiveHits(t *testing.T) {
	w := minecartWorld(t)
	doWorld(w, func(tx *world.Tx) {
		railLine(tx, 0, 0, 0, func(int) world.Block { return block.Rail{Shape: block.RailEastWest} })
	})
	h := spawnCart(w, mgl64.Vec3{0.5, 0.0625, 0.5}, mgl64.Vec3{}, MinecartPlain)
	doWorld(w, func(tx *world.Tx) {
		e, _ := h.Entity(tx)
		for i := 0; i < 4; i++ {
			HurtEntity(e, 1, AttackDamageSource{})
		}
		if _, ok := h.Entity(tx); !ok {
			t.Fatal("minecart broke after 4 hits of 1")
		}
		HurtEntity(e, 1, AttackDamageSource{})
		if _, ok := h.Entity(tx); ok {
			t.Fatal("minecart not broken after 5 hits of 1")
		}
		items := 0
		for e := range tx.Entities() {
			if ent, ok := e.(*Ent); ok {
				if ib, ok := ent.Behaviour().(*ItemBehaviour); ok {
					if _, ok := ib.Item().Item().(item.Minecart); ok {
						items++
					}
				}
			}
		}
		if items != 1 {
			t.Fatalf("minecart items dropped = %d, want 1", items)
		}
	})
}

func TestMinecartNBT(t *testing.T) {
	data := &world.EntityData{}
	MinecartConfig{Kind: MinecartHopper}.Apply(data)
	b := data.Data.(*minecartBehaviour)
	b.flipped, b.hopperEnabled = true, false
	_ = b.inv.SetItem(2, item.NewStack(block.Stone{}, 5))
	m := HopperMinecartType.EncodeNBT(data)
	back := &world.EntityData{}
	HopperMinecartType.DecodeNBT(m, back)
	nb := back.Data.(*minecartBehaviour)
	if !nb.flipped || nb.hopperEnabled {
		t.Errorf("decoded flipped %v enabled %v, want true false", nb.flipped, nb.hopperEnabled)
	}
	if s, _ := nb.inv.Item(2); s.Count() != 5 {
		t.Errorf("decoded hopper slot 2 = %v, want 5 stone", s)
	}
}
