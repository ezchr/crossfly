//go:build crossfly_full

package social_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/boss"
	"github.com/df-mc/dragonfly/server/mob/component/village"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/social"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	_ "github.com/df-mc/dragonfly/server/mob/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

func setup(t *testing.T, opts mobtest.Options, keys ...string) *mobtest.World {
	t.Helper()
	mobtest.Require(t, keys...)
	mobtest.RequireDefinitions(t)
	mobtest.RequireFilters(t)
	w := mobtest.New(t, opts)
	w.Do(func(tx *world.Tx) { w.Arena(tx, 24, 4) })
	return w
}

// itemsNear counts item entities within r of pos.
func itemsNear(tx *world.Tx, pos mgl64.Vec3, r float64) int {
	n := 0
	for e := range tx.Entities() {
		if e.H().Type() == entity.ItemType && e.Position().Sub(pos).Len() <= r {
			n++
		}
	}
	return n
}

func mobOf(t *testing.T, tx *world.Tx, r *mobtest.Ref) *mob.Mob {
	t.Helper()
	m, ok := r.Mob(tx)
	if !ok {
		t.Fatal("mob gone")
	}
	return m
}

// TestAllayGivesItemToOwner: an allay holding items flies to its owner and
// throws one item at it.
func TestAllayGivesItemToOwner(t *testing.T) {
	w := setup(t, mobtest.Options{Time: mobtest.Noon}, "minecraft:behavior.go_and_give_items_to_owner", "minecraft:navigation.hover")
	p := w.AddPlayer("owner", mgl64.Vec3{8.5, 1, 0.5})
	a := w.Spawn("minecraft:allay", mgl64.Vec3{0.5, 2, 0.5})
	w.Do(func(tx *world.Tx) {
		m := mobOf(t, tx, a)
		pl, _ := p.Entity(tx)
		social.SetLiked(m, pl)
		m.SetHeldItems(item.NewStack(item.Diamond{}, 3), item.Stack{})
	})
	_, ok := w.TickUntil(800, func(tx *world.Tx) bool {
		m := mobOf(t, tx, a)
		main, _ := m.HeldItems()
		return main.Count() < 3
	})
	if !ok {
		t.Fatalf("allay never threw an item:\n%v", w.Dump(a))
	}
	if d := w.Dist(a, p); d > 4 {
		t.Errorf("threw from %.1f blocks, reach is 3", d)
	}
	w.Tick(20)
	w.Do(func(tx *world.Tx) {
		if itemsNear(tx, p.PosTx(tx), 4) == 0 {
			t.Errorf("no thrown item near the owner")
		}
	})
}

// TestAllayGivesItemToNoteblock: an allay that heard a note block brings
// its item there, with no owner.
func TestAllayGivesItemToNoteblock(t *testing.T) {
	w := setup(t, mobtest.Options{Time: mobtest.Noon}, "minecraft:behavior.go_and_give_items_to_noteblock", "minecraft:navigation.hover")
	nb := cube.Pos{6, 1, 6}
	w.Do(func(tx *world.Tx) { tx.SetBlock(nb, block.Note{}, nil) })
	a := w.Spawn("minecraft:allay", mgl64.Vec3{-3.5, 2, -3.5})
	w.Do(func(tx *world.Tx) {
		mobOf(t, tx, a).SetHeldItems(item.NewStack(item.Emerald{}, 2), item.Stack{})
		social.NoteBlockPlayed(tx, nb)
	})
	_, ok := w.TickUntil(400, func(tx *world.Tx) bool {
		main, _ := mobOf(t, tx, a).HeldItems()
		return main.Count() < 2
	})
	if !ok {
		t.Fatalf("allay never threw at the note block:\n%v", w.Dump(a))
	}
	if d := a.Pos().Sub(nb.Vec3Centre()).Len(); d > 4.5 {
		t.Errorf("threw %.1f blocks from the note block", d)
	}
}

// TestAllayStaysNearNoteblock: an allay far from the note block it heard
// comes back within stop_distance (4).
func TestAllayStaysNearNoteblock(t *testing.T) {
	w := setup(t, mobtest.Options{Time: mobtest.Noon}, "minecraft:behavior.stay_near_noteblock", "minecraft:navigation.hover")
	nb := cube.Pos{-10, 1, 0}
	w.Do(func(tx *world.Tx) { tx.SetBlock(nb, block.Note{}, nil) })
	a := w.Spawn("minecraft:allay", mgl64.Vec3{5.5, 2, 0.5})
	w.Do(func(tx *world.Tx) { social.NoteBlockPlayed(tx, nb) })
	// 15.5 blocks: inside the 16 hearing range but not beyond start_distance
	// 16, so move it out first.
	w.Do(func(tx *world.Tx) { mobOf(t, tx, a).Teleport(mgl64.Vec3{12.5, 2, 0.5}) })
	_, ok := w.TickUntil(600, func(tx *world.Tx) bool {
		return a.PosTx(tx).Sub(nb.Vec3Centre()).Len() <= 5
	})
	if !ok {
		t.Fatalf("allay did not return to the note block (%.1f away):\n%v", a.Pos().Sub(nb.Vec3Centre()).Len(), w.Dump(a))
	}
}

// TestCatGiftAtSunrise: a cat with the gift group walks to the nearby
// player at sunrise, fires cat_gifted_owner (removing the group) and maybe
// drops a gift.
func TestCatGiftAtSunrise(t *testing.T) {
	// Time 1 (not 0, the kit's midnight default): celestial angle ~0.75.
	w := setup(t, mobtest.Options{Time: 1}, "minecraft:behavior.drop_item_for")
	p := w.AddPlayer("owner", mgl64.Vec3{4.5, 1, 0.5})
	c := w.Spawn("minecraft:cat", mgl64.Vec3{-1.5, 1, 0.5})
	w.Do(func(tx *world.Tx) { mobOf(t, tx, c).Fire("minecraft:pet_slept_with_owner", nil) })
	w.Do(func(tx *world.Tx) {
		if !mobOf(t, tx, c).HasComponent("minecraft:behavior.drop_item_for") {
			t.Fatal("gift group not added")
		}
	})
	_, ok := w.TickUntil(300, func(tx *world.Tx) bool {
		return !mobOf(t, tx, c).HasComponent("minecraft:behavior.drop_item_for")
	})
	if !ok {
		t.Fatalf("cat never attempted its gift (dist %.1f):\n%v", w.Dist(c, p), w.Dump(c))
	}
	if d := w.Dist(c, p); d > 2.5 {
		t.Errorf("gift attempt %.1f blocks from the player, offering_distance is 5 in cat.json but it walks up first", d)
	}
}

// TestCatNoGiftAtNoon: outside time_of_day_range the goal never starts.
func TestCatNoGiftAtNoon(t *testing.T) {
	w := setup(t, mobtest.Options{Time: mobtest.Noon}, "minecraft:behavior.drop_item_for")
	w.AddPlayer("owner", mgl64.Vec3{4.5, 1, 0.5})
	c := w.Spawn("minecraft:cat", mgl64.Vec3{-1.5, 1, 0.5})
	w.Do(func(tx *world.Tx) { mobOf(t, tx, c).Fire("minecraft:pet_slept_with_owner", nil) })
	w.Tick(100)
	w.Do(func(tx *world.Tx) {
		if !mobOf(t, tx, c).HasComponent("minecraft:behavior.drop_item_for") {
			t.Error("gift attempted at noon")
		}
	})
}

// TestVillagersMakeBaby: two adult villagers penned in with free beds fall
// in love (make_love on one, receive_love on the other) and a baby is born.
// The pen keeps them within the 8-block mate search while the live
// scheduler swaps their work/wander groups (every group keeps
// make_and_receive_love at noon).
func TestVillagersMakeBaby(t *testing.T) {
	w := setup(t, mobtest.Options{Time: mobtest.Noon}, "minecraft:behavior.make_love", "minecraft:behavior.receive_love", "minecraft:navigation.walk")
	w.Do(func(tx *world.Tx) {
		g := block.Glass{}
		w.Wall(tx, cube.Pos{-2, 1, -2}, cube.Pos{5, 1, -2}, 3, g)
		w.Wall(tx, cube.Pos{-2, 1, 7}, cube.Pos{5, 1, 7}, 3, g)
		w.Wall(tx, cube.Pos{-2, 1, -2}, cube.Pos{-2, 1, 7}, 3, g)
		w.Wall(tx, cube.Pos{5, 1, -2}, cube.Pos{5, 1, 7}, 3, g)
		for x := 0; x < 4; x++ {
			tx.SetBlock(cube.Pos{x, 1, 5}, block.Bed{Facing: cube.North, Head: true}, nil)
			tx.SetBlock(cube.Pos{x, 1, 6}, block.Bed{Facing: cube.North}, nil)
		}
	})
	opts := mob.SpawnOptions{Event: "minecraft:spawn_farmer"}
	a := w.Spawn("minecraft:villager_v2", mgl64.Vec3{0.5, 1, 0.5}, opts)
	b := w.Spawn("minecraft:villager_v2", mgl64.Vec3{3.5, 1, 1.5}, opts)
	w.Do(func(tx *world.Tx) {
		for _, r := range []*mobtest.Ref{a, b} {
			m := mobOf(t, tx, r)
			if !m.HasComponent("minecraft:behavior.make_love") {
				t.Skipf("villager has no make_love group:\n%v", w.Dump(r))
			}
			// The love food (VillagerBase::isWillingToBreed).
			_ = village.Inventory(m).SetItem(0, item.NewStack(item.Bread{}, 3))
		}
	})
	sawLove := false
	_, ok := w.TickUntil(6000, func(tx *world.Tx) bool {
		if mobOf(t, tx, a).InLove() || mobOf(t, tx, b).InLove() {
			sawLove = true
		}
		n := 0
		for _, r := range w.Mobs("minecraft:villager_v2") {
			if m, ok := r.Mob(tx); ok && m.Baby() {
				n++
			}
		}
		return n > 0
	})
	if !ok {
		t.Fatalf("no baby villager after 6000 ticks (love seen %v):\n%v\n%v", sawLove, w.Dump(a), w.Dump(b))
	}
	if !sawLove {
		t.Error("baby born without the parents being in love")
	}
	// Both parents used their 3 bread (VillagerBase::consumeLoveFood).
	w.Do(func(tx *world.Tx) {
		for _, r := range []*mobtest.Ref{a, b} {
			bread := 0
			for _, st := range village.Inventory(mobOf(t, tx, r)).Slots() {
				if _, ok := st.Item().(item.Bread); ok {
					bread += st.Count()
				}
			}
			if bread != 0 {
				t.Errorf("parent still has %d bread", bread)
			}
		}
	})
}

// TestVillagersNoBabyWithoutFood: a villager without 3 bread or 12 crops is
// not willing to breed (VillagerBase::isWillingToBreed), so no baby comes
// even with free beds.
func TestVillagersNoBabyWithoutFood(t *testing.T) {
	w := setup(t, mobtest.Options{Time: mobtest.Noon}, "minecraft:behavior.make_love", "minecraft:behavior.receive_love", "minecraft:navigation.walk")
	w.Do(func(tx *world.Tx) {
		for x := 0; x < 4; x++ {
			tx.SetBlock(cube.Pos{x, 1, 5}, block.Bed{Facing: cube.North, Head: true}, nil)
			tx.SetBlock(cube.Pos{x, 1, 6}, block.Bed{Facing: cube.North}, nil)
		}
	})
	opts := mob.SpawnOptions{Event: "minecraft:spawn_farmer"}
	a := w.Spawn("minecraft:villager_v2", mgl64.Vec3{0.5, 1, 0.5}, opts)
	b := w.Spawn("minecraft:villager_v2", mgl64.Vec3{2.5, 1, 0.5}, opts)
	w.Do(func(tx *world.Tx) {
		// Two bread are not enough.
		_ = village.Inventory(mobOf(t, tx, a)).SetItem(0, item.NewStack(item.Bread{}, 2))
		_ = village.Inventory(mobOf(t, tx, b)).SetItem(0, item.NewStack(item.Bread{}, 2))
	})
	for i := 0; i < 1500; i++ {
		w.Tick(1)
		love := false
		w.Do(func(tx *world.Tx) { love = mobOf(t, tx, a).InLove() || mobOf(t, tx, b).InLove() })
		if love {
			t.Fatal("villager without its love food fell in love")
		}
	}
}

// TestVillagersNoBabyWithoutBeds: without free beds the village needs no
// more villagers.
func TestVillagersNoBabyWithoutBeds(t *testing.T) {
	w := setup(t, mobtest.Options{Time: mobtest.Noon}, "minecraft:behavior.make_love")
	opts := mob.SpawnOptions{Event: "minecraft:spawn_farmer"}
	a := w.Spawn("minecraft:villager_v2", mgl64.Vec3{0.5, 1, 0.5}, opts)
	w.Spawn("minecraft:villager_v2", mgl64.Vec3{2.5, 1, 0.5}, opts)
	for i := 0; i < 1500; i++ {
		w.Tick(1)
		love := false
		w.Do(func(tx *world.Tx) { love = mobOf(t, tx, a).InLove() })
		if love {
			t.Fatal("villager fell in love with no beds around")
		}
	}
}

// TestBabyVillagerPlays: a baby villager starts playing (chance 0.005 a
// tick) and runs around.
func TestBabyVillagerPlays(t *testing.T) {
	w := setup(t, mobtest.Options{Time: mobtest.Noon}, "minecraft:behavior.play", "minecraft:navigation.walk")
	a := w.Spawn("minecraft:villager", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: mob.EventBorn})
	w.Spawn("minecraft:villager", mgl64.Vec3{2.5, 1, 0.5}, mob.SpawnOptions{Event: mob.EventBorn})
	start := a.Pos()
	played := false
	w.Each(2000, func(tx *world.Tx, _ int) bool {
		if mobOf(t, tx, a).Goals().Running("minecraft:behavior.play") {
			played = true
		}
		return !played
	})
	if !played {
		t.Fatalf("baby villager never played:\n%v", w.Dump(a))
	}
	w.Tick(100)
	if d := a.Pos().Sub(start).Len(); d < 1 {
		t.Errorf("playing villager stayed put (moved %.2f)", d)
	}
}

// TestVillagerHidesInBed: an alarmed villager_v2 runs to the nearest bed.
func TestVillagerHidesInBed(t *testing.T) {
	w := setup(t, mobtest.Options{Time: mobtest.Noon}, "minecraft:behavior.hide", "minecraft:navigation.walk")
	bed := cube.Pos{8, 1, 3}
	w.Do(func(tx *world.Tx) {
		tx.SetBlock(bed, block.Bed{Facing: cube.North, Head: true}, nil)
		tx.SetBlock(bed.Add(cube.Pos{0, 0, 1}), block.Bed{Facing: cube.North}, nil)
	})
	v := w.Spawn("minecraft:villager_v2", mgl64.Vec3{-4.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_farmer"})
	w.Do(func(tx *world.Tx) { social.Alarm(tx, v.PosTx(tx), 8) })
	_, ok := w.TickUntil(400, func(tx *world.Tx) bool {
		p := v.PosTx(tx)
		dx, dz := p[0]-(float64(bed[0])+0.5), p[2]-(float64(bed[2])+0.5)
		return dx*dx+dz*dz <= 2.25
	})
	if !ok {
		t.Fatalf("villager did not reach the bed:\n%v", w.Dump(v))
	}
}

// TestWardenSniffs: a warden with a player nearby sniffs (sniffing flag),
// and after three sniffs (35 anger each, 80 to anger) targets the player.
func TestWardenSniffs(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:behavior.sniff")
	p := w.AddPlayer("p", mgl64.Vec3{3.5, 1, 0.5})
	p.SetMaxHealth(1000)
	wd := w.Spawn("minecraft:warden", mgl64.Vec3{0.5, 1, 0.5})
	sniffs, last := 0, false
	targeted := false
	w.Each(1200, func(tx *world.Tx, _ int) bool {
		m := mobOf(t, tx, wd)
		// Keep the player within the suspicion radius (6 blocks) of the
		// strolling warden.
		if pl, ok := p.Entity(tx); ok {
			if tp, ok := pl.(interface{ Teleport(mgl64.Vec3) }); ok && pl.Position().Sub(m.Position()).Len() > 4 {
				tp.Teleport(m.Position().Add(mgl64.Vec3{2, 0, 0}))
			}
		}
		on := m.ActorFlag(protocol.EntityDataFlagSniffing)
		if on && !last {
			sniffs++
		}
		last = on
		if _, ok := m.Target(); ok {
			targeted = true
		}
		return !targeted
	})
	w.Do(func(tx *world.Tx) {
		pl, _ := p.Entity(tx)
		m := mobOf(t, tx, wd)
		t.Logf("anger at player %d", social.SniffAnger(m, pl))
		// Every sniff ending near the player makes its block the
		// suspicious position (SniffGoal::stop).
		st, ok := boss.SuspectOf(m)
		if !ok {
			t.Fatal("warden has no suspect_tracking")
		}
		pos, ok := st.SuspiciousPos()
		if !ok {
			t.Fatal("sniffing set no suspicious position")
		}
		if d := pos.Vec3Centre().Sub(pl.Position()); math.Hypot(d[0], d[2]) > 8 {
			t.Errorf("suspicious position %v is not near the player at %v", pos, pl.Position())
		}
	})
	if sniffs == 0 {
		t.Fatalf("warden never sniffed:\n%v", w.Dump(wd))
	}
	if !targeted {
		t.Errorf("warden sniffed %d times but never targeted the player", sniffs)
	}
}

// TestBreezeJumpsAndFires: a breeze with a player target jumps around it
// and fires wind charges at it (fire_at_target, projectile_def).
func TestBreezeJumpsAndFires(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:behavior.jump_around_target", "minecraft:behavior.fire_at_target", "minecraft:behavior.nearest_attackable_target")
	p := w.AddPlayer("p", mgl64.Vec3{10.5, 1, 0.5})
	p.SetMaxHealth(1000)
	b := w.Spawn("minecraft:breeze", mgl64.Vec3{0.5, 1, 0.5})
	maxY, jumped, flagSeen := 0.0, false, false
	charges := map[*world.EntityHandle]bool{}
	w.Each(600, func(tx *world.Tx, _ int) bool {
		m := mobOf(t, tx, b)
		if y := m.Position()[1]; y > maxY {
			maxY = y
		}
		if m.ActorFlag(protocol.EntityDataFlagJumpGoal) {
			flagSeen = true
		}
		if maxY > 2.5 {
			jumped = true
		}
		for e := range tx.Entities() {
			if e.H().Type().EncodeEntity() == "minecraft:breeze_wind_charge_projectile" {
				charges[e.H()] = true
			}
		}
		return !(jumped && len(charges) > 0)
	})
	if !jumped || !flagSeen {
		t.Errorf("breeze did not jump (max y %.2f, flag %v):\n%v", maxY, flagSeen, w.Dump(b))
	}
	w.Do(func(tx *world.Tx) {
		if len(charges) == 0 {
			t.Errorf("breeze never fired a wind charge:\n%v", w.Dump(b))
		}
		if n := social.ShotsWithoutProjectile(mobOf(t, tx, b)); n != 0 {
			t.Errorf("%d shots went without a projectile", n)
		}
	})
}

// TestBreezeMovesAroundTarget: the breeze keeps moving while it has a
// target (move_around_target), staying a few blocks from it.
func TestBreezeMovesAroundTarget(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:behavior.move_around_target", "minecraft:behavior.nearest_attackable_target")
	p := w.AddPlayer("p", mgl64.Vec3{6.5, 1, 0.5})
	p.SetMaxHealth(1000)
	b := w.Spawn("minecraft:breeze", mgl64.Vec3{0.5, 1, 0.5})
	ran := false
	w.Each(400, func(tx *world.Tx, _ int) bool {
		if mobOf(t, tx, b).Goals().Running("minecraft:behavior.move_around_target") {
			ran = true
		}
		return !ran
	})
	if !ran {
		t.Fatalf("move_around_target never ran:\n%v", w.Dump(b))
	}
}
