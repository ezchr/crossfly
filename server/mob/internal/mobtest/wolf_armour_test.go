//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func init() {
	// Wolf armour is an item of the mobs: registered before the first world.
	block.RegisterMobBlocks()
	item.RegisterMobItems()
}

// tamedWolf spawns a tamed adult wolf owned by a survival player next to it.
func tamedWolf(t *testing.T) (*mobtest.World, *mobtest.Ref, *mobtest.Player) {
	t.Helper()
	mobtest.RequireDefinitions(t)
	mobtest.RequireFilters(t)
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	wolf := w.Spawn("minecraft:wolf", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_tame_adult"})
	owner := w.AddPlayer("owner", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(2)
	w.Do(func(tx *world.Tx) {
		pe, _ := owner.Entity(tx)
		m, _ := wolf.Mob(tx)
		m.SetOwner(pe)
	})
	// The environment sensor makes a tamed adult wolf armorable.
	if _, ok := w.TickUntil(100, func(tx *world.Tx) bool {
		m, ok := wolf.Mob(tx)
		return ok && m.HasComponent("minecraft:interact")
	}); !ok {
		t.Fatalf("tamed wolf did not become armorable:\n%v", w.Dump(wolf))
	}
	return w, wolf, owner
}

// hurtWolf hurts the wolf by dmg of src and returns the health it lost and the armour it wears after. It first
// waits out the hurt cooldown of an earlier hit.
func hurtWolf(w *mobtest.World, wolf *mobtest.Ref, dmg float64, src world.DamageSource) (lost float64, armour item.Stack) {
	w.Tick(12)
	wolf.With(func(m *mob.Mob) {
		before := m.Health()
		m.Hurt(dmg, src)
		lost, armour = before-m.Health(), m.BodyArmour()
	})
	return lost, armour
}

// TestWolfArmour: the owner puts wolf armour on his tamed wolf; it takes the damage of attacks and falls in
// place of the wolf until it breaks, not that of drowning, and shears take it off again.
func TestWolfArmour(t *testing.T) {
	w, wolf, owner := tamedWolf(t)
	left, ok := owner.Interact(wolf, item.NewStack(item.WolfArmour{}, 1))
	if !ok {
		t.Fatalf("wolf did not take the armour:\n%v", w.Dump(wolf))
	}
	if !left.Empty() {
		t.Errorf("owner kept %v after putting the armour on", left)
	}
	wolf.With(func(m *mob.Mob) {
		if _, ok := m.BodyArmour().Item().(item.WolfArmour); !ok {
			t.Fatalf("wolf wears %v, want wolf armour", m.BodyArmour())
		}
	})

	if lost, a := hurtWolf(w, wolf, 4.5, entity.AttackDamageSource{}); lost != 0 || a.Durability() != 59 {
		t.Errorf("attack of 4.5: wolf lost %v health, armour durability %v; want 0 and 59", lost, a.Durability())
	}
	if lost, a := hurtWolf(w, wolf, 3, entity.FallDamageSource{}); lost != 0 || a.Durability() != 56 {
		t.Errorf("fall of 3: wolf lost %v health, armour durability %v; want 0 and 56", lost, a.Durability())
	}
	if lost, a := hurtWolf(w, wolf, 2, entity.DrowningDamageSource{}); lost != 2 || a.Durability() != 56 {
		t.Errorf("drowning of 2: wolf lost %v health, armour durability %v; want 2 and 56", lost, a.Durability())
	}

	// Breaking: the last of its durability goes, the hit with it.
	wolf.With(func(m *mob.Mob) { m.SetBodyArmour(m.BodyArmour().WithDurability(3)) })
	if lost, a := hurtWolf(w, wolf, 5, entity.AttackDamageSource{}); lost != 0 || !a.Empty() {
		t.Errorf("breaking hit: wolf lost %v health, wears %v; want 0 and nothing", lost, a)
	}
	if lost, _ := hurtWolf(w, wolf, 1, entity.AttackDamageSource{}); lost != 1 {
		t.Errorf("without armour the wolf lost %v health of 1", lost)
	}

	// Shears take it off: the armour drops, the shears wear.
	if _, ok := owner.Interact(wolf, item.NewStack(item.WolfArmour{}, 1)); !ok {
		t.Fatal("wolf did not take new armour")
	}
	shears, ok := owner.Interact(wolf, item.NewStack(item.Shears{}, 1))
	if !ok {
		t.Fatalf("shears did not take the armour off:\n%v", w.Dump(wolf))
	}
	if shears.Durability() != shears.MaxDurability()-1 {
		t.Errorf("shears durability %v after use, want %v", shears.Durability(), shears.MaxDurability()-1)
	}
	wolf.With(func(m *mob.Mob) {
		if !m.BodyArmour().Empty() {
			t.Errorf("wolf still wears %v", m.BodyArmour())
		}
	})
	w.Tick(2)
	if n := itemsNear[item.WolfArmour](w, mgl64.Vec3{0.5, 1, 0.5}, 4); n != 1 {
		t.Errorf("%v wolf armour dropped, want 1", n)
	}
}

// TestWolfArmourStranger: only the owner can put armour on a wolf.
func TestWolfArmourStranger(t *testing.T) {
	w, wolf, _ := tamedWolf(t)
	other := w.AddPlayer("other", mgl64.Vec3{-1.5, 1, 0.5})
	w.Tick(1)
	other.Interact(wolf, item.NewStack(item.WolfArmour{}, 1))
	wolf.With(func(m *mob.Mob) {
		if !m.BodyArmour().Empty() {
			t.Errorf("a stranger put %v on the wolf", m.BodyArmour())
		}
	})
}

// TestWolfArmourRepairSlot: RepairSlot repairs damaged body armour by the amount (repair_entity_item), up to
// its full durability.
func TestWolfArmourRepairSlot(t *testing.T) {
	_, wolf, _ := tamedWolf(t)
	wolf.With(func(m *mob.Mob) {
		if m.RepairSlot("slot.armor.body", 8) {
			t.Error("repaired an empty slot")
		}
		m.SetBodyArmour(item.NewStack(item.WolfArmour{}, 1).WithDurability(50))
		if !m.RepairSlot("slot.armor.body", 8) || m.BodyArmour().Durability() != 58 {
			t.Errorf("repaired armour has %v durability, want 58", m.BodyArmour().Durability())
		}
		m.RepairSlot("slot.armor.body", 8)
		if m.BodyArmour().Durability() != 64 {
			t.Errorf("armour repaired past full: %v", m.BodyArmour().Durability())
		}
		if m.RepairSlot("slot.armor.body", 8) {
			t.Error("repaired undamaged armour")
		}
	})
}
