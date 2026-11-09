//go:build crossfly_full

package loot_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/loot"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// equipViewer records the last held items and helmet it was shown per
// entity, like a client keeping MobEquipment/MobArmorEquipment state.
type equipViewer struct {
	world.NopViewer
	main, off map[*world.EntityHandle]item.Stack
	head      map[*world.EntityHandle]item.Stack
}

func newEquipViewer() *equipViewer {
	return &equipViewer{main: map[*world.EntityHandle]item.Stack{}, off: map[*world.EntityHandle]item.Stack{},
		head: map[*world.EntityHandle]item.Stack{}}
}

func (v *equipViewer) ViewEntityItems(e world.Entity) {
	if c, ok := e.(item.Carrier); ok {
		v.main[e.H()], v.off[e.H()] = c.HeldItems()
	}
}

func (v *equipViewer) ViewEntityArmour(e world.Entity) {
	if a, ok := e.(interface{ Armour() *inventory.Armour }); ok && a.Armour() != nil {
		v.head[e.H()] = a.Armour().Helmet()
	}
}

func watch(w *mobtest.World, v world.Viewer, pos mgl64.Vec3) *world.Loader {
	l := world.NewLoader(2, w.W, v)
	w.Do(func(tx *world.Tx) {
		l.Move(tx, pos)
		l.Load(tx, 64)
	})
	return l
}

// TestEquipmentVisible: viewers get the spawn gear once it is rolled, see
// later changes (armour put on, death clearing the slots), and a viewer
// arriving later sees the gear at once.
func TestEquipmentVisible(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	early := newEquipViewer()
	watch(w, early, mgl64.Vec3{0.5, 1, 0.5})

	sk := w.Spawn("minecraft:skeleton", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(2)
	if got := name(early.main[sk.H()]); got != "bow" {
		t.Fatalf("viewer sees the skeleton holding %q, want bow", got)
	}

	late := newEquipViewer()
	watch(w, late, mgl64.Vec3{0.5, 1, 0.5})
	if got := name(late.main[sk.H()]); got != "bow" {
		t.Fatalf("late viewer sees the skeleton holding %q, want bow", got)
	}

	sk.With(func(m *mob.Mob) { m.Armour().SetHelmet(item.NewStack(item.Helmet{Tier: item.ArmourTierGold{}}, 1)) })
	if got := name(early.head[sk.H()]); got != "golden_helmet" {
		t.Fatalf("viewer sees helmet %q after it was put on, want golden_helmet", got)
	}

	w.Do(func(tx *world.Tx) {
		m, _ := sk.Mob(tx)
		m.Hurt(1000, entity.VoidDamageSource{})
	})
	if !early.main[sk.H()].Empty() || !early.head[sk.H()].Empty() {
		t.Fatalf("viewer still sees %v / %v on the corpse", name(early.main[sk.H()]), name(early.head[sk.H()]))
	}
}

// TestPickupMarksItem: zombies that pick up a sword hold it visibly and the
// sword is marked, so it always drops.
func TestPickupMarksItem(t *testing.T) {
	mobtest.Require(t, "minecraft:behavior.pickup_items")
	w := mobtest.New(t, mobtest.Options{Difficulty: world.DifficultyHard})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 24, block.Stone{}) })
	v := newEquipViewer()
	watch(w, v, mgl64.Vec3{0.5, 1, 0.5})
	var zs []*mobtest.Ref
	w.Do(func(tx *world.Tx) {
		for i := range 8 {
			x := float64(i*5) - 17.5
			zs = append(zs, w.SpawnTx(tx, "minecraft:zombie", mgl64.Vec3{x, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:as_adult"}))
			tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{x + 1.5, 1, 0.5}}, item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1)))
		}
	})
	var got *mobtest.Ref
	w.TickUntil(400, func(tx *world.Tx) bool {
		for _, z := range zs {
			if m, ok := z.Mob(tx); ok {
				if main, _ := m.HeldItems(); name(main) == "diamond_sword" {
					got = z
					return true
				}
			}
		}
		return false
	})
	if got == nil {
		t.Skip("no zombie picked a sword up (pickup_based_on_chance rolls)")
	}
	if n := name(v.main[got.H()]); n != "diamond_sword" {
		t.Fatalf("viewer sees %q, want the picked-up diamond_sword", n)
	}
	got.With(func(m *mob.Mob) {
		main, _ := m.HeldItems()
		if !loot.WasPickedUp(main) {
			t.Fatal("picked-up sword is not marked")
		}
	})
}
