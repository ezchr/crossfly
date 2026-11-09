//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	vc "github.com/df-mc/dragonfly/server/mob/component/village"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestPiglinBarterByHand: right-clicking an adult piglin with gold ingots
// uses one up, the piglin admires it (off hand) and then throws barter loot
// (minecraft:interact admire/barter, InteractComponent::_runInteraction).
func TestPiglinBarterByHand(t *testing.T) {
	needs(t, true, true, "minecraft:interact", "minecraft:admire_item", "minecraft:barter", "minecraft:behavior.admire_item", "minecraft:behavior.barter")
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 16, block.Stone{}) })
	pg := w.Spawn("minecraft:piglin", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "spawn_adult"})
	p := w.AddPlayer("p", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(2)
	left, ok := p.Interact(pg, item.NewStack(item.GoldIngot{}, 3))
	if !ok {
		t.Fatalf("interaction refused:\n%v", w.Dump(pg))
	}
	if left.Count() != 2 {
		t.Errorf("player has %d ingots left, want 2", left.Count())
	}
	pg.With(func(m *mob.Mob) {
		a, _ := vc.AdmireOf(m)
		b, _ := vc.BarterOf(m)
		if a == nil || !a.Admiring || a.Item.Count() != 1 {
			t.Errorf("piglin does not admire one ingot: %+v", a)
		}
		if b == nil || !b.Wants {
			t.Errorf("piglin does not want to barter: %+v", b)
		}
	})
	if n, ok := w.TickUntil(10, func(tx *world.Tx) bool {
		m, _ := pg.Mob(tx)
		_, off := m.HeldItems()
		return m.ActorFlag(vc.FlagAdmiring) && !off.Empty()
	}); !ok {
		t.Fatalf("piglin not admiring after %d ticks:\n%v", n, w.Dump(pg))
	}
	if n, ok := w.TickUntil(300, func(tx *world.Tx) bool {
		for e := range tx.Entities() {
			if e.H().Type().EncodeEntity() == "minecraft:item" {
				return true
			}
		}
		return false
	}); !ok {
		t.Fatalf("no barter loot %d ticks later:\n%v", n, w.Dump(pg))
	}
}
