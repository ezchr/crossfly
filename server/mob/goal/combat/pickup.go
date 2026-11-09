//go:build crossfly_full

package combat

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/mob/component/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// pickupItems is minecraft:behavior.pickup_items, basic: walk to an item
// entity within max_dist that is better armour or a better weapon than what
// the mob has (any item into an empty hand with can_pickup_any_item),
// pick it up within goal_radius and wear or hold it, dropping the old one.
// Mobs with pickup_based_on_chance only pick up when a roll at creation
// allowed it.
// source: java/paper Mob.pickUpItem/canReplaceCurrentItem (BDS
// PickupItemsGoal was not decompiled further).
type pickupItems struct {
	mob.GoalBase
	maxDist, goalRadius float64
	speed               float64
	anyItem             bool
	basedOnChance       bool
	excluded            []string
	rolled, able        bool
	item                *world.EntityHandle
	nextScan            int64
}

func newPickupItems(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		MaxDist             float64           `json:"max_dist"`
		GoalRadius          *float64          `json:"goal_radius"`
		SpeedMultiplier     *float64          `json:"speed_multiplier"`
		CanPickupAnyItem    bool              `json:"can_pickup_any_item"`
		PickupBasedOnChance bool              `json:"pickup_based_on_chance"`
		ExcludedItems       []json.RawMessage `json:"excluded_items"`
	}
	if err := spec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	g := &pickupItems{
		GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)},
		maxDist:  v.MaxDist, goalRadius: or(v.GoalRadius, 0.5), speed: or(v.SpeedMultiplier, 1),
		anyItem: v.CanPickupAnyItem, basedOnChance: v.PickupBasedOnChance,
	}
	for _, r := range v.ExcludedItems {
		var s string
		if json.Unmarshal(r, &s) == nil {
			g.excluded = append(g.excluded, s)
			continue
		}
		var o struct{ Item string }
		if json.Unmarshal(r, &o) == nil && o.Item != "" {
			g.excluded = append(g.excluded, o.Item)
		}
	}
	return g, nil
}

// pickupChance is the chance by difficulty that a pickup_based_on_chance
// mob may pick up items at all (estimate of Java's 0.55 x special
// multiplier).
var pickupChance = [4]float64{0, 0, 0.275, 0.55}

// CanUse ...
func (g *pickupItems) CanUse(m *mob.Mob) bool {
	if !g.rolled {
		g.rolled = true
		g.able = !g.basedOnChance || m.Rand().Float64() < pickupChance[sense.DifficultyID(m.Tx())]
	}
	if !g.able || g.maxDist <= 0 || m.Ticks() < g.nextScan {
		return false
	}
	// BDS: no pickups while admiring an item (piglins).
	if a, ok := mob.ComponentOf[interface{ IsAdmiring() bool }](m); ok && a.IsAdmiring() {
		return false
	}
	g.nextScan = m.Ticks() + 10
	e := g.findItem(m)
	if e == nil {
		return false
	}
	g.item = e.H()
	return true
}

func (g *pickupItems) findItem(m *mob.Mob) world.Entity {
	pos, r := m.Pos(), g.maxDist
	box := cube.Box(pos[0]-r, pos[1]-r, pos[2]-r, pos[0]+r, pos[1]+r, pos[2]+r)
	var best world.Entity
	bestD := math.MaxFloat64
	for e := range m.Tx().EntitiesWithin(box) {
		s, ok := itemOf(e)
		if !ok || !g.wants(m, s) {
			continue
		}
		if d := e.Position().Sub(pos).LenSqr(); d < bestD {
			best, bestD = e, d
		}
	}
	return best
}

func itemOf(e world.Entity) (item.Stack, bool) {
	if e.H().Type() != entity.ItemType {
		return item.Stack{}, false
	}
	ent, ok := e.(*entity.Ent)
	if !ok {
		return item.Stack{}, false
	}
	b, ok := ent.Behaviour().(*entity.ItemBehaviour)
	if !ok {
		return item.Stack{}, false
	}
	return b.Item(), true
}

func itemName(s item.Stack) string {
	if s.Empty() {
		return ""
	}
	n, _ := s.Item().EncodeItem()
	return n
}

// wants reports whether m would pick s up.
func (g *pickupItems) wants(m *mob.Mob, s item.Stack) bool {
	if s.Empty() {
		return false
	}
	name := itemName(s)
	for _, x := range g.excluded {
		if x == name {
			return false
		}
	}
	if sp, ok := mob.ComponentOf[sharePicker](m); ok && sp.WantsPickup(m, s) {
		return true
	}
	slot, better := equipSlot(m, s)
	if slot >= 0 {
		return better
	}
	main, _ := m.HeldItems()
	return g.anyItem && main.Empty()
}

// equipSlot returns the armour slot (0 helmet..3 boots) or 4 for the main
// hand that s goes into, and whether it is better than what is there; -1
// when s is neither armour nor a weapon.
func equipSlot(m *mob.Mob, s item.Stack) (int, bool) {
	a := m.Armour()
	cur := []item.Stack{a.Helmet(), a.Chestplate(), a.Leggings(), a.Boots()}
	slot := -1
	switch it := s.Item().(type) {
	case item.HelmetType:
		if it.Helmet() {
			slot = 0
		}
	case item.ChestplateType:
		if it.Chestplate() {
			slot = 1
		}
	case item.LeggingsType:
		if it.Leggings() {
			slot = 2
		}
	case item.BootsType:
		if it.Boots() {
			slot = 3
		}
	}
	if slot >= 0 {
		c := cur[slot]
		if c.Empty() {
			return slot, true
		}
		ca, _ := c.Item().(item.Armour)
		na := s.Item().(item.Armour)
		return slot, ca == nil || na.DefencePoints() > ca.DefencePoints()
	}
	if s.AttackDamage() > 1 {
		main, _ := m.HeldItems()
		return 4, main.Empty() || s.AttackDamage() > main.AttackDamage()
	}
	return -1, false
}

// CanContinueToUse ...
func (g *pickupItems) CanContinueToUse(m *mob.Mob) bool {
	if g.item == nil {
		return false
	}
	e, ok := g.item.Entity(m.Tx())
	if !ok {
		return false
	}
	s, ok := itemOf(e)
	return ok && g.wants(m, s) && e.Position().Sub(m.Pos()).LenSqr() <= (g.maxDist+1)*(g.maxDist+1)
}

// Start ...
func (g *pickupItems) Start(m *mob.Mob) {
	if e, ok := g.item.Entity(m.Tx()); ok {
		m.Navigation().MoveTo(m, e.Position(), g.speed)
	}
}

// Stop ...
func (g *pickupItems) Stop(m *mob.Mob) { g.item = nil }

// Tick picks the item up once within reach.
func (g *pickupItems) Tick(m *mob.Mob) {
	e, ok := g.item.Entity(m.Tx())
	if !ok {
		return
	}
	reach := math.Max(g.goalRadius, 1)
	if e.Position().Sub(m.Pos()).LenSqr() > reach*reach+1 {
		if m.Navigation().Done() {
			m.Navigation().MoveTo(m, e.Position(), g.speed)
		}
		return
	}
	s, ok := itemOf(e)
	if !ok {
		return
	}
	if sp, ok := mob.ComponentOf[sharePicker](m); ok && sp.WantsPickup(m, s) {
		if n := sp.PickedUp(m, s, nil); n > 0 {
			if s.Count() > n {
				m.Tx().AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: e.Position()}, s.Grow(-n)))
			}
			for _, v := range m.Tx().Viewers(e.Position()) {
				v.ViewEntityAction(e, entity.PickedUpAction{Collector: collector{m}})
			}
			_ = e.Close()
			g.item = nil
			return
		}
	}
	// Picked-up items always drop on death (BDS ItemStack WasPickedUp).
	Equip(m, loot.MarkPickedUp(s.Grow(1-s.Count())))
	if s.Count() > 1 {
		m.Tx().AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: e.Position()}, s.Grow(-1)))
	}
	for _, v := range m.Tx().Viewers(e.Position()) {
		v.ViewEntityAction(e, entity.PickedUpAction{Collector: collector{m}})
	}
	_ = e.Close()
	m.SetPersistent(true)
	g.item = nil
}

// collector lets the pick up animation name the mob.
type collector struct{ *mob.Mob }

func (collector) Collect(item.Stack) (int, bool) { return 0, false }

// Equip wears or holds s (one item), dropping what was in that slot.
func Equip(m *mob.Mob, s item.Stack) {
	slot, _ := equipSlot(m, s)
	a := m.Armour()
	drop := func(old item.Stack) {
		if !old.Empty() {
			old = loot.ClearPickedUp(old)
			m.Tx().AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: m.Pos().Add(mgl64.Vec3{0, 0.5})}, old))
		}
	}
	switch slot {
	case 0:
		drop(a.Helmet())
		a.SetHelmet(s)
	case 1:
		drop(a.Chestplate())
		a.SetChestplate(s)
	case 2:
		drop(a.Leggings())
		a.SetLeggings(s)
	case 3:
		drop(a.Boots())
		a.SetBoots(s)
	default:
		main, off := m.HeldItems()
		drop(main)
		m.SetHeldItems(s, off)
	}
}

// AppendDebugInfo ...
func (g *pickupItems) AppendDebugInfo(b *strings.Builder) { b.WriteString("PickupItems") }

// equipItem is minecraft:behavior.equip_item: in BDS it equips an item the
// mob picked up into its hand slot. Picking up already equips here, so the
// goal never runs.
type equipItem struct{ mob.GoalBase }

func newEquipItem(spec mob.GoalSpec) (mob.Goal, error) {
	return &equipItem{mob.GoalBase{Control: spec.FlagsOr(0)}}, nil
}

// CanUse ...
func (*equipItem) CanUse(*mob.Mob) bool { return false }

// useKineticWeapon is minecraft:behavior.use_kinetic_weapon (spear charge,
// 1.21.130+), a stub: it never runs, so zombies fall back to melee.
type useKineticWeapon struct{ mob.GoalBase }

func newUseKineticWeapon(spec mob.GoalSpec) (mob.Goal, error) {
	return &useKineticWeapon{mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}}, nil
}

// CanUse ...
func (*useKineticWeapon) CanUse(*mob.Mob) bool { return false }

// sharePicker is a component that decides pickups by its item list
// (minecraft:shareables in package component/village; BDS
// PickupItemsGoal::_pickItemUp): items it wants go to it instead of being
// worn or held.
type sharePicker interface {
	WantsPickup(m *mob.Mob, s item.Stack) bool
	PickedUp(m *mob.Mob, s item.Stack, thrower world.Entity) int
}
