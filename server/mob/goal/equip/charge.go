//go:build crossfly_full

// Package equip holds goals about the items a mob holds:
// minecraft:behavior.charge_held_item. Link it through package mob/vanilla.
package equip

import (
	"fmt"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

func init() {
	mob.RegisterGoal("minecraft:behavior.charge_held_item", newChargeHeldItem)
}

// chargeHeldItem is minecraft:behavior.charge_held_item (BDS
// ChargeHeldItemGoal): a mob with a target and an empty crossbow in its main
// hand charges it for the crossbow's use duration (1.25 s, less with Quick
// Charge), using item meanwhile, then loads it with one of "items". The
// ranged attack waits for the charge (RangedAttackGoal::canUse, see
// loot.NeedsCharge) and empties the crossbow when it fires (loot.Discharge).
type chargeHeldItem struct {
	mob.GoalBase
	items       []world.Item
	left, total int
}

func newChargeHeldItem(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Items []string `json:"items"`
	}
	if err := spec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	g := &chargeHeldItem{GoalBase: mob.GoalBase{Control: spec.FlagsOr(0)}}
	for _, n := range v.Items {
		if !strings.Contains(n, ":") {
			n = "minecraft:" + n
		}
		if it, ok := world.ItemByName(n, 0); ok {
			g.items = append(g.items, it)
		}
	}
	return g, nil
}

// CanUse is ChargeHeldItemGoal::canUse.
func (g *chargeHeldItem) CanUse(m *mob.Mob) bool {
	if len(g.items) == 0 || !loot.NeedsCharge(m) {
		return false
	}
	_, ok := m.Target()
	return ok
}

// CanContinueToUse ...
func (g *chargeHeldItem) CanContinueToUse(m *mob.Mob) bool {
	return g.left > 0 && loot.NeedsCharge(m)
}

// Start is ChargeHeldItemGoal::start.
func (g *chargeHeldItem) Start(m *mob.Mob) {
	main, _ := m.HeldItems()
	d, qc := chargeDuration(main)
	g.total = max(int(d/(time.Second/20)), 1)
	g.left = g.total
	m.UpdateMeta(func(meta *mob.Meta) { meta.UsingItem = true })
	m.Tx().PlaySound(m.Position(), sound.CrossbowLoad{Stage: sound.CrossbowLoadingStart, QuickCharge: qc > 0})
}

// Stop is ChargeHeldItemGoal::stop.
func (g *chargeHeldItem) Stop(m *mob.Mob) {
	g.left = 0
	m.UpdateMeta(func(meta *mob.Meta) { meta.UsingItem = false })
}

// Tick is ChargeHeldItemGoal::tick: count down, then load a random item.
func (g *chargeHeldItem) Tick(m *mob.Mob) {
	main, off := m.HeldItems()
	c, ok := main.Item().(item.Crossbow)
	if !ok {
		g.left = 0
		return
	}
	_, qc := chargeDuration(main)
	g.left--
	if g.left == g.total/2 {
		m.Tx().PlaySound(m.Position(), sound.CrossbowLoad{Stage: sound.CrossbowLoadingMiddle, QuickCharge: qc > 0})
	}
	if g.left > 0 {
		return
	}
	c.Item = item.NewStack(g.items[m.Rand().IntN(len(g.items))], 1)
	m.SetHeldItems(loot.WithItem(main, c), off)
	m.Tx().PlaySound(m.Position(), sound.CrossbowLoad{Stage: sound.CrossbowLoadingEnd, QuickCharge: qc > 0})
}

// AppendDebugInfo ...
func (g *chargeHeldItem) AppendDebugInfo(b *strings.Builder) {
	fmt.Fprintf(b, "ChargeHeldItem %d/%d", g.total-g.left, g.total)
}

func chargeDuration(s item.Stack) (time.Duration, int) {
	d, lvl := time.Duration(1.25*float64(time.Second)), 0
	for _, e := range s.Enchantments() {
		if q, ok := e.Type().(interface{ ChargeDuration(int) time.Duration }); ok {
			d = min(d, q.ChargeDuration(e.Level()))
			lvl = e.Level()
		}
	}
	return d, lvl
}
