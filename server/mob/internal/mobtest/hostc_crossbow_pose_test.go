//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// TestPillagerCrossbowDrawPose: while the pillager charges its crossbow the
// charge_amount actor data climbs towards 127 with the using_item flag on;
// once loaded the charged flag shows the loaded crossbow
// (ChargeHeldItemGoal::start/tick).
func TestPillagerCrossbowDrawPose(t *testing.T) {
	needs(t, true, true, "minecraft:behavior.charge_held_item")
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 16, block.Stone{}) })
	pg := w.Spawn("minecraft:pillager", mgl64.Vec3{0.5, 1, 0.5})
	p := w.AddPlayer("p", mgl64.Vec3{8.5, 1, 0.5})
	w.Tick(1)
	pg.With(func(m *mob.Mob) {
		_, off := m.HeldItems()
		m.SetHeldItems(item.NewStack(item.Crossbow{}, 1), off)
	})
	const key = "minecraft:behavior.charge_held_item"
	maxAmount, sawPartial := byte(0), false
	n, ok := w.TickUntil(200, func(tx *world.Tx) bool {
		m, _ := pg.Mob(tx)
		if _, has := m.Target(); !has {
			if pe, ok := p.Entity(tx); ok {
				m.SetTarget(pe)
			}
		}
		if v, ok := m.ActorData(protocol.EntityDataKeyChargeAmount); ok && m.Goals().Running(key) {
			a := v.(byte)
			maxAmount = max(maxAmount, a)
			if a > 0 && a < 127 && m.Meta().UsingItem {
				sawPartial = true
			}
		}
		return m.ActorFlag(protocol.EntityDataFlagCharged)
	})
	if !ok {
		t.Fatalf("crossbow never charged in %d ticks:\n%v", n, w.Dump(pg))
	}
	if !sawPartial || maxAmount < 100 {
		t.Errorf("draw not shown: partial %v, max charge_amount %d", sawPartial, maxAmount)
	}
	pg.With(func(m *mob.Mob) {
		if m.Goals().Running(key) {
			return
		}
		if v, _ := m.ActorData(protocol.EntityDataKeyChargeAmount); v != byte(0) {
			t.Errorf("charge_amount %v after the charge", v)
		}
	})
}
