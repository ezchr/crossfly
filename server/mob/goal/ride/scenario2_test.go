//go:build crossfly_full

package ride_test

import (
	"math"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	cride "github.com/df-mc/dragonfly/server/mob/component/ride"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func window(t *testing.T, w *mobtest.World, r *mobtest.Ref, p *mobtest.Player, layout entity.WindowLayout) entity.EntityWindow {
	t.Helper()
	var win entity.EntityWindow
	var ok bool
	w.Do(func(tx *world.Tx) {
		e, _ := r.Entity(tx)
		pe, _ := p.Entity(tx)
		win, ok = entity.WindowOf(e, pe, layout)
	})
	if !ok {
		t.Fatal("no entity window")
	}
	return win
}

// A tamed horse's window: saddle and armour slots (Bedrock HORSE_EQUIP 0
// and 1, Java 0 and 1); a saddle put in the window saddles the horse, armour
// shows as body armour, taking the saddle out unsaddles it.
func TestHorseWindow(t *testing.T) {
	w := newWorld(t, mobtest.Options{})
	h := w.Spawn("minecraft:horse", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_tame_adult"})
	p := w.AddPlayer("p", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(2)
	win := window(t, w, h, p, entity.WindowLayoutBedrock)
	if win.Inventory.Size() != 2 || win.ChestSlots != 0 || len(win.Equip) != 2 {
		t.Fatalf("horse window: size %d, chest %d, equip %d", win.Inventory.Size(), win.ChestSlots, len(win.Equip))
	}
	if win.Equip[0].Slot != 0 || win.Equip[1].Slot != 1 || len(win.Equip[1].Accepted) < 4 {
		t.Fatalf("equip slots %+v", win.Equip)
	}
	w.Do(func(*world.Tx) {
		_ = win.Inventory.SetItem(0, item.NewStack(item.Saddle{}, 1))
		_ = win.Inventory.SetItem(1, item.NewStack(item.HorseArmour{Tier: item.HorseArmourDiamond}, 1))
	})
	w.Tick(2)
	h.With(func(m *mob.Mob) {
		if !m.Saddled() || !m.HasComponent("minecraft:input_ground_controlled") {
			t.Fatal("saddle from the window does not saddle the horse")
		}
		if _, ok := m.BodyArmour().Item().(item.HorseArmour); !ok {
			t.Fatal("armour from the window is not shown")
		}
	})
	// A Java window of the same horse has the items in slots 0 and 1.
	jw := window(t, w, h, p, entity.WindowLayoutJava)
	if s, _ := jw.Inventory.Item(0); s.Empty() {
		t.Fatal("java window lacks the saddle")
	}
	w.Do(func(*world.Tx) { _ = jw.Inventory.SetItem(0, item.Stack{}) })
	w.Tick(2)
	h.With(func(m *mob.Mob) {
		if m.Saddled() {
			t.Fatal("taking the saddle out did not unsaddle the horse")
		}
	})
	// A cheating client's dirt in the saddle slot is dropped.
	w.Do(func(*world.Tx) { _ = jw.Inventory.SetItem(0, item.NewStack(block.Dirt{}, 1)) })
	w.Tick(2)
	h.With(func(m *mob.Mob) {
		e, _ := mob.ComponentOf[*cride.Equippable](m)
		if !e.Item(0).Empty() || m.Saddled() {
			t.Fatal("dirt was taken as a saddle")
		}
	})
}

// A chested donkey: Bedrock window = saddle + 15 chest slots; Java window =
// saddle, an unused body slot, then the chest. Chest items are kept.
func TestDonkeyChestWindow(t *testing.T) {
	w := newWorld(t, mobtest.Options{})
	d := w.Spawn("minecraft:donkey", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_tame_adult"})
	p := w.AddPlayer("p", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(2)
	if win := window(t, w, d, p, entity.WindowLayoutBedrock); win.ChestSlots != 0 {
		t.Fatalf("unchested donkey shows %d chest slots", win.ChestSlots)
	}
	if _, ok := p.Interact(d, item.NewStack(block.NewChest(), 1)); !ok {
		t.Fatal("donkey took no chest")
	}
	w.Tick(2)
	win := window(t, w, d, p, entity.WindowLayoutBedrock)
	if win.ChestSlots != 15 || win.Inventory.Size() != 16 || len(win.Equip) != 1 {
		t.Fatalf("chested donkey window: size %d chest %d equip %d", win.Inventory.Size(), win.ChestSlots, len(win.Equip))
	}
	w.Do(func(*world.Tx) { _ = win.Inventory.SetItem(5, item.NewStack(item.Wheat{}, 7)) })
	jw := window(t, w, d, p, entity.WindowLayoutJava)
	if jw.Inventory.Size() != 17 {
		t.Fatalf("java donkey window size %d, want 17", jw.Inventory.Size())
	}
	if s, _ := jw.Inventory.Item(6); s.Count() != 7 {
		t.Fatalf("chest item not in java slot 6: %v", s)
	}
}

// An untamed horse has no window; a sneaking stranger opens a tamed one only
// through a session (a session-less player has none).
func TestWindowNeedsTame(t *testing.T) {
	w := newWorld(t, mobtest.Options{})
	h := w.Spawn("minecraft:horse", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_adult"})
	p := w.AddPlayer("p", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(2)
	w.Do(func(tx *world.Tx) {
		e, _ := h.Entity(tx)
		pe, _ := p.Entity(tx)
		if _, ok := entity.WindowOf(e, pe, entity.WindowLayoutBedrock); ok {
			t.Fatal("wild horse has a window")
		}
	})
}

// The seat offset of a player rider is the seat plus the player's riding
// height 1.62001 (VehicleServerSeatPositionSystem); the server keeps the
// player's feet on the seat.
func TestPlayerSeatOffset(t *testing.T) {
	w := newWorld(t, mobtest.Options{})
	h := w.Spawn("minecraft:horse", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_tame_adult"})
	h.With(func(m *mob.Mob) { m.Fire("minecraft:horse_saddled", nil) })
	p := w.AddPlayer("rider", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(2)
	p.Interact(h, item.Stack{})
	w.Tick(3)
	p.With(func(pl *player.Player) {
		off, ok := pl.SeatOffset()
		if !ok || math.Abs(off[1]-2.72001) > 1e-6 || math.Abs(off[2]+0.2) > 1e-6 {
			t.Fatalf("seat offset %v, want (0, 2.72001, -0.2)", off)
		}
	})
	if d := p.Pos()[1] - h.Pos()[1]; math.Abs(d-1.1) > 0.05 {
		t.Fatalf("rider feet %v above the horse, want 1.1", d)
	}
}

// A vehicle the rider's client moves (ClientVehicleMove) counts as driven by
// that client for a tick, so sessions send its movement back to everyone
// else only.
func TestClientDrivenVehicle(t *testing.T) {
	w := newWorld(t, mobtest.Options{})
	h := w.Spawn("minecraft:horse", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_tame_adult"})
	h.With(func(m *mob.Mob) { m.Fire("minecraft:horse_saddled", nil) })
	p := w.AddPlayer("rider", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(2)
	p.Interact(h, item.Stack{})
	w.Tick(2)
	w.Do(func(tx *world.Tx) {
		e, _ := h.Entity(tx)
		pe, _ := p.Entity(tx)
		r, _ := entity.RideableOf(e)
		if entity.DrivenBy(e, pe.H()) {
			t.Fatal("driven before the client moved it")
		}
		if !r.(entity.ClientVehicle).ClientVehicleMove(pe, e.Position().Add(mgl64.Vec3{0, 0, 0.5}), 10, 0) {
			t.Fatal("client move refused")
		}
		if !entity.DrivenBy(e, pe.H()) {
			t.Fatal("not driven by the client that moved it")
		}
		if r.(entity.ClientVehicle).ClientVehicleMove(pe, e.Position().Add(mgl64.Vec3{20, 0, 0}), 0, 0) {
			t.Fatal("a 20 block client move was taken")
		}
	})
	w.Tick(4)
	w.Do(func(tx *world.Tx) {
		e, _ := h.Entity(tx)
		pe, _ := p.Entity(tx)
		if entity.DrivenBy(e, pe.H()) {
			t.Fatal("still driven without client moves")
		}
	})
	// The clients read the horse's speed and jump strength off its behaviour.
	w.Do(func(tx *world.Tx) {
		e, _ := h.Entity(tx)
		b := e.(interface{ Behaviour() entity.Behaviour }).Behaviour()
		sp, ok := b.(interface{ Speed() float64 })
		js, ok2 := b.(interface{ JumpStrength() float64 })
		if !ok || !ok2 || sp.Speed() <= 0 || js.JumpStrength() <= 0 {
			t.Fatal("horse attributes not readable for the sessions")
		}
	})
}

// A tamed parrot gets on the shoulder of a nearby player (player.json
// rideable, find_mount) and jumps off when the player falls.
func TestParrotShoulder(t *testing.T) {
	w := newWorld(t, mobtest.Options{Seed: 5})
	par := w.Spawn("minecraft:parrot", mgl64.Vec3{0.5, 1, 0.5})
	p := w.AddPlayer("p", mgl64.Vec3{1.5, 1, 0.5})
	w.Tick(2)
	par.With(func(m *mob.Mob) {
		m.Fire("minecraft:on_tame", nil)
		m.SetOwner(mustPlayer(t, w, p))
	})
	if _, ok := w.TickUntil(600, func(tx *world.Tx) bool {
		e, _ := par.Entity(tx)
		m, _ := mob.Of(e)
		return m != nil && m.VehicleHandle() == p.H()
	}); !ok {
		par.With(func(m *mob.Mob) { t.Fatalf("tamed parrot did not get on the shoulder: %v", w.Dump(par)) })
	}
	w.Do(func(tx *world.Tx) {
		e, _ := par.Entity(tx)
		r, _ := entity.RiderOf(e)
		if off, ok := r.SeatOffset(); !ok || off != (mgl64.Vec3{0.4, -0.2, -0.1}) {
			t.Fatalf("parrot seat offset %v, want the right shoulder (0.4, -0.2, -0.1)", off)
		}
	})
	if d := par.Pos().Sub(p.Pos()); math.Abs(d[1]-1.42001) > 0.05 {
		t.Errorf("parrot %v above the feet, want 1.42", d[1])
	}
	// Falling: off the shoulder.
	p.Teleport(mgl64.Vec3{1.5, 12, 0.5})
	if _, ok := w.TickUntil(60, func(tx *world.Tx) bool {
		e, _ := par.Entity(tx)
		m, _ := mob.Of(e)
		return m != nil && !m.Riding()
	}); !ok {
		t.Fatal("parrot stayed on a falling player")
	}
}

// A sprung skeleton horse trap is gone for good: it does not fire again
// after a reload.
func TestSkeletonTrapOnce(t *testing.T) {
	w := newWorld(t, mobtest.Options{Persistent: true})
	h := w.Spawn("minecraft:skeleton_horse", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:set_trap"})
	w.Tick(2)
	h.With(func(m *mob.Mob) {
		if !m.HasComponent("minecraft:behavior.skeleton_horse_trap") {
			t.Fatal("no trap")
		}
	})
	w.AddPlayer("p", mgl64.Vec3{3.5, 1, 0.5})
	w.Tick(5)
	horses := len(w.Mobs("minecraft:skeleton_horse"))
	if horses != 4 {
		t.Fatalf("%d skeleton horses after the trap, want 4", horses)
	}
	h.With(func(m *mob.Mob) {
		if m.HasComponent("minecraft:behavior.skeleton_horse_trap") {
			t.Fatal("trap still set after springing")
		}
	})
	w.Reload()
	w.Tick(5)
	if n := len(w.Mobs("minecraft:skeleton_horse")); n != 4 {
		t.Fatalf("%d skeleton horses after a reload, want 4", n)
	}
}

// Wolves defend their owner from a player (owner_hurt_by_target through
// player.LastAttacker).
func TestWolfDefendsFromPlayer(t *testing.T) {
	w := newWorld(t, mobtest.Options{})
	wolf := w.Spawn("minecraft:wolf", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_tame_adult"})
	owner := w.AddPlayer("owner", mgl64.Vec3{2.5, 1, 0.5})
	bully := w.AddPlayer("bully", mgl64.Vec3{4.5, 1, 0.5})
	w.Tick(2)
	wolf.With(func(m *mob.Mob) { m.SetOwner(mustPlayer(t, w, owner)) })
	w.Do(func(tx *world.Tx) {
		o, _ := owner.Entity(tx)
		b, _ := bully.Entity(tx)
		o.(*player.Player).Hurt(1, entity.AttackDamageSource{Attacker: b})
		h, at, ok := o.(*player.Player).LastAttacker()
		if !ok || h != b.H() || time.Since(at) > time.Second {
			t.Fatal("player does not record its attacker")
		}
	})
	if _, ok := w.TickUntil(60, func(tx *world.Tx) bool {
		e, _ := wolf.Entity(tx)
		m, _ := mob.Of(e)
		return m.TargetHandle() == bully.H()
	}); !ok {
		t.Fatal("wolf did not target the player hurting its owner")
	}
}
