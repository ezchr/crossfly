//go:build crossfly_full

package village

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Shareable is one entry of minecraft:shareables (BDS Shareable).
type Shareable struct {
	Item              core.ItemDescriptor
	WantAmount        int
	SurplusAmount     int
	MaxAmount         int
	PickupLimit       int
	CraftInto         string
	Priority          int
	Admire, Barter    bool
	StoredInInventory bool
	ConsumeItem       bool
	PickupOnly        bool
}

// Shareables is minecraft:shareables (BDS ShareableComponent): which items
// a mob wants, how many, which it has spare to share (share_items), which
// it admires or barters (piglins).
type Shareables struct {
	Items          []Shareable
	AllItems       bool
	AllWant        int
	AllSurplus     int
	AllMax         int
	SingularPickup bool
}

func newShareables(_ string, raw json.RawMessage) (mob.Component, error) {
	j := struct {
		Items []struct {
			Item        string `json:"item"`
			Want        *int   `json:"want_amount"`
			Surplus     *int   `json:"surplus_amount"`
			Max         *int   `json:"max_amount"`
			PickupLimit *int   `json:"pickup_limit"`
			CraftInto   string `json:"craft_into"`
			Priority    int    `json:"priority"`
			Admire      bool   `json:"admire"`
			Barter      bool   `json:"barter"`
			Stored      bool   `json:"stored_in_inventory"`
			Consume     bool   `json:"consume_item"`
			PickupOnly  bool   `json:"pickup_only"`
		} `json:"items"`
		AllItems   bool `json:"all_items"`
		AllWant    *int `json:"all_items_want_amount"`
		AllSurplus *int `json:"all_items_surplus_amount"`
		AllMax     *int `json:"all_items_max_amount"`
		Singular   bool `json:"singular_pickup"`
	}{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &j); err != nil {
			return nil, fmt.Errorf("shareables: %w", err)
		}
	}
	s := &Shareables{AllItems: j.AllItems, AllWant: or(j.AllWant, -1), AllSurplus: or(j.AllSurplus, -1), AllMax: or(j.AllMax, -1), SingularPickup: j.Singular}
	for _, it := range j.Items {
		s.Items = append(s.Items, Shareable{Item: core.ParseItemDescriptor(it.Item), WantAmount: or(it.Want, -1), SurplusAmount: or(it.Surplus, -1),
			MaxAmount: or(it.Max, -1), PickupLimit: or(it.PickupLimit, -1), CraftInto: it.CraftInto, Priority: it.Priority, Admire: it.Admire,
			Barter: it.Barter, StoredInInventory: it.Stored, ConsumeItem: it.Consume, PickupOnly: it.PickupOnly})
	}
	return s, nil
}

func (*Shareables) Attach(*mob.Mob) {}
func (*Shareables) Detach(*mob.Mob) {}

// ShareablesOf returns m's shareables.
func ShareablesOf(m *mob.Mob) (*Shareables, bool) { return mob.ComponentOf[*Shareables](m) }

// Entry returns the entry for s.
func (sh *Shareables) Entry(s item.Stack) (*Shareable, bool) {
	for i := range sh.Items {
		if sh.Items[i].Item.Matches(s) {
			return &sh.Items[i], true
		}
	}
	return nil, false
}

// WantsMore is ShareableComponent::wantsMore: how many more of s the mob
// wants (want_amount - what it has).
func (sh *Shareables) WantsMore(m *mob.Mob, s item.Stack) int {
	want := -1
	if e, ok := sh.Entry(s); ok {
		want = e.WantAmount
	} else if sh.AllItems {
		want = sh.AllWant
	}
	if want <= 0 {
		return 0
	}
	return want - CountItems(m, core.ItemDescriptor{Name: nameOf(s), Data: metaOf(s), HasData: true})
}

// Surplus is ShareableComponent::hasSurplus: what the mob has to spare of
// the items in its inventory: the stack it would share (crafted into
// craft_into when set) and how many of the item it takes.
func (sh *Shareables) Surplus(m *mob.Mob, s item.Stack) (item.Stack, int) {
	e, ok := sh.Entry(s)
	if !ok || e.PickupOnly {
		return item.Stack{}, 0
	}
	threshold := e.SurplusAmount
	if threshold < 0 {
		return item.Stack{}, 0
	}
	have := CountItems(m, e.Item)
	if have <= threshold {
		return item.Stack{}, 0
	}
	share := min(have-threshold, max(1, threshold/3))
	if e.CraftInto != "" {
		// Wheat -> bread: three wheat a loaf (crafting table recipe).
		loaves := share / 3
		if loaves <= 0 {
			return item.Stack{}, 0
		}
		out, ok := core.ParseItemDescriptor(e.CraftInto).Stack(loaves)
		if !ok {
			return item.Stack{}, 0
		}
		return out, loaves * 3
	}
	share = min(share, s.MaxCount())
	return s.Grow(share - s.Count()), share
}

// WantsPickup reports whether the mob would pick s up (willPickup +
// getCountToPickUp > 0).
func (sh *Shareables) WantsPickup(m *mob.Mob, s item.Stack) bool {
	return sh.CountToPickUp(m, s) > 0
}

// CountToPickUp is ShareableComponent::getCountToPickUp.
func (sh *Shareables) CountToPickUp(m *mob.Mob, s item.Stack) int {
	e, ok := sh.Entry(s)
	maxAmount, limit := sh.AllMax, -1
	if ok {
		maxAmount, limit = e.MaxAmount, e.PickupLimit
	} else if !sh.AllItems {
		return 0
	}
	count := s.Count()
	if maxAmount >= 0 {
		have := CountItems(m, core.ItemDescriptor{Name: nameOf(s), Data: metaOf(s), HasData: true})
		count = min(count, maxAmount-have)
	} else if ok && e.WantAmount > 0 && !e.Admire && !e.Barter {
		count = min(count, max(0, e.WantAmount-CountItems(m, e.Item)+e.SurplusAmount))
	}
	if limit >= 0 {
		count = min(count, limit)
	}
	if ok && (e.Admire || e.Barter) {
		count = min(count, 1)
	}
	return max(0, count)
}

// PickedUp takes count of s from an item entity thrown by thrower
// (PickupItemsGoal::_pickItemUp): admire/barter items go to those
// components, stored items into the inventory. It returns how many it took.
func (sh *Shareables) PickedUp(m *mob.Mob, s item.Stack, thrower world.Entity) int {
	n := sh.CountToPickUp(m, s)
	if n <= 0 {
		return 0
	}
	s = s.Grow(n - s.Count())
	e, ok := sh.Entry(s)
	if ok && e.Admire {
		if a, ok := AdmireOf(m); ok {
			a.PickedUp(m, s, thrower)
		}
	}
	if ok && e.Barter {
		if b, ok := BarterOf(m); ok {
			b.PickedUp(m, thrower)
			return n
		}
	}
	if ok && e.Admire {
		return n
	}
	left, _ := Inventory(m).AddItem(s)
	return n - left
}

func nameOf(s item.Stack) string {
	if s.Empty() {
		return ""
	}
	n, _ := s.Item().EncodeItem()
	return n
}

func metaOf(s item.Stack) int16 {
	if s.Empty() {
		return 0
	}
	_, m := s.Item().EncodeItem()
	return m
}

// RemoveItems takes n items matching d from the inventory.
func RemoveItems(m *mob.Mob, d core.ItemDescriptor, n int) int {
	inv := Inventory(m)
	took := 0
	for i, s := range inv.Slots() {
		if took >= n {
			break
		}
		if s.Empty() || !d.Matches(s) {
			continue
		}
		k := min(n-took, s.Count())
		_ = inv.SetItem(i, s.Grow(-k))
		took += k
	}
	return took
}

// dropItem adds an item entity at pos with velocity v.
func dropItem(tx *world.Tx, pos, v mgl64.Vec3, s item.Stack) {
	tx.AddEntity(entity.NewItemPickupDelay(world.EntitySpawnOpts{Position: pos, Velocity: v}, s, time.Second/2))
}

// DropTowards throws s from m towards target (Mob::dropTowards).
func DropTowards(m *mob.Mob, s item.Stack, target mgl64.Vec3) {
	from := m.EyePosition().Sub(mgl64.Vec3{0, 0.3})
	d := target.Sub(from)
	h := math.Hypot(d[0], d[2])
	v := mgl64.Vec3{}
	if h > 0 {
		v = mgl64.Vec3{d[0] / h * 0.3, 0.1 + h*0.02, d[2] / h * 0.3}
	}
	dropItem(m.Tx(), from, v, s)
}

// Caravans (Mob::joinCaravan/leaveCaravan/getCaravanHead) and leashes (the
// leads of package mob/component/core), as far as follow_caravan and the
// wandering trader need them.
type caravanKey struct{}
type tailKey struct{}

// CaravanHead returns the mob m follows in a caravan.
func CaravanHead(m *mob.Mob) (*mob.Mob, bool) {
	v, ok := m.Data(caravanKey{})
	if !ok {
		return nil, false
	}
	h, ok := mob.OfHandle(m.Tx(), v.(*world.EntityHandle))
	if !ok || h.Dead() {
		m.SetData(caravanKey{}, nil)
		return nil, false
	}
	return h, true
}

// HasCaravanTail reports whether a mob follows m.
func HasCaravanTail(m *mob.Mob) bool {
	v, ok := m.Data(tailKey{})
	if !ok {
		return false
	}
	t, ok := mob.OfHandle(m.Tx(), v.(*world.EntityHandle))
	if !ok || t.Dead() {
		m.SetData(tailKey{}, nil)
		return false
	}
	if hv, ok := t.Data(caravanKey{}); !ok || hv.(*world.EntityHandle) != m.H() {
		m.SetData(tailKey{}, nil)
		return false
	}
	return true
}

// JoinCaravan makes m follow head.
func JoinCaravan(m, head *mob.Mob) {
	m.SetData(caravanKey{}, head.H())
	head.SetData(tailKey{}, m.H())
}

// LeaveCaravan stops m following its head.
func LeaveCaravan(m *mob.Mob) {
	if h, ok := CaravanHead(m); ok {
		h.SetData(tailKey{}, nil)
	}
	m.SetData(caravanKey{}, nil)
}

// SetLeashHolder leashes m to holder (nil unleashes, dropping the lead).
func SetLeashHolder(m *mob.Mob, holder world.Entity) {
	if holder == nil {
		core.Unleash(m)
		return
	}
	core.Leash(m, holder)
}

// Leashed reports whether m is leashed (Actor::isLeashed).
func Leashed(m *mob.Mob) bool { return core.Leashed(m) }
