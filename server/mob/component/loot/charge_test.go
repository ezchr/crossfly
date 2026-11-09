//go:build crossfly_full

package loot_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestPillagerCharges: a pillager charges its crossbow (using item for 25
// ticks) before every shot, and the shot empties it again.
func TestPillagerCharges(t *testing.T) {
	mobtest.Require(t, "minecraft:behavior.ranged_attack", "minecraft:behavior.charge_held_item", "minecraft:shooter")
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 16, block.Stone{}) })
	p := w.AddPlayer("target", mgl64.Vec3{8.5, 1, 0.5})
	pl := w.Spawn("minecraft:pillager", mgl64.Vec3{0.5, 1, 0.5})
	pl.With(func(m *mob.Mob) {
		if e, ok := p.Entity(m.Tx()); ok {
			m.SetTarget(e)
		}
	})
	var (
		charging, loaded, shots int
		lastLoaded              bool
		firstLoad               = -1
	)
	w.Each(300, func(tx *world.Tx, tick int) bool {
		m, ok := pl.Mob(tx)
		if !ok {
			return false
		}
		if e, ok := p.Entity(tx); ok {
			m.SetTarget(e)
		}
		if m.Meta().UsingItem {
			charging++
		}
		main, _ := m.HeldItems()
		c, ok := main.Item().(item.Crossbow)
		if !ok {
			t.Fatalf("pillager holds %v", name(main))
		}
		isLoaded := !c.Item.Empty()
		if isLoaded {
			loaded++
			if firstLoad < 0 {
				firstLoad = tick
			}
		}
		if lastLoaded && !isLoaded {
			shots++
		}
		lastLoaded = isLoaded
		return true
	})
	if shots == 0 {
		t.Fatalf("pillager never fired (charging %d ticks, loaded %d ticks)", charging, loaded)
	}
	if firstLoad < 25 {
		t.Errorf("crossbow loaded at tick %d, before a 25 tick charge", firstLoad)
	}
	if charging < 25*shots {
		t.Errorf("%d shots after only %d ticks of charging", shots, charging)
	}
	t.Logf("%d shots, %d ticks charging, first load at tick %d", shots, charging, firstLoad)
}
