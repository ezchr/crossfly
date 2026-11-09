//go:build crossfly_full

package village

import (
	"encoding/json"
	"io/fs"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
)

func comp(t *testing.T, key string, f mob.ComponentFactory, raw string) mob.Component {
	t.Helper()
	c, err := f(key, json.RawMessage(raw))
	if err != nil {
		t.Fatalf("%v: %v", key, err)
	}
	return c
}

func eq[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%v = %v, want %v", name, got, want)
	}
}

// TestComponentDefaults checks the BDS 1.21.3 defaults (Description /
// Definition deserialisers) with empty JSON.
func TestComponentDefaults(t *testing.T) {
	d := comp(t, "minecraft:dweller", newDweller, `{}`).(*Dweller)
	eq(t, "dwelling_bounds_tolerance", d.tolerance, 10.0)
	eq(t, "dweller_role", d.role, RoleInhabitant)
	eq(t, "can_find_poi", d.canFindPOI, false)
	d = comp(t, "minecraft:dweller", newDweller, `{"dweller_role":"defender"}`).(*Dweller)
	eq(t, "defender role", d.role, RoleDefender)

	e := comp(t, "minecraft:economy_trade_table", newEconomy, `{}`).(*Economy)
	eq(t, "hero_demand_discount", e.heroDemand, -4)
	eq(t, "cured_discount", [2]int{e.curedMin, e.curedMax}, [2]int{-25, -20})
	eq(t, "max_cured_discount", [2]int{e.maxCuredMin, e.maxCuredMax}, [2]int{-25, -20})
	eq(t, "nearby_cured_discount", e.nearbyCured, -20)
	eq(t, "max_nearby_cured_discount", e.maxNearby, -200)
	eq(t, "show_trade_screen", e.showScreen, true)
	eq(t, "use_legacy_price_formula", e.legacyFormula, false)
	l := comp(t, "minecraft:trade_table", newEconomy, `{}`).(*Economy)
	eq(t, "legacy", l.legacy, true)

	a := comp(t, "minecraft:admire_item", newAdmire, `{}`).(*Admire)
	eq(t, "admire duration", a.duration, 200)
	eq(t, "admire cooldown", a.cooldown, 200)
	b := comp(t, "minecraft:barter", newBarter, `{}`).(*Barter)
	eq(t, "barter cooldown", b.cooldown, 200)
	ch := comp(t, "minecraft:celebrate_hunt", newCelebrateHunt, `{}`).(*celebrateHunt)
	eq(t, "celebrate_hunt broadcast", ch.broadcast, true)
	eq(t, "celebrate_hunt radius", ch.radius, 16.0)
	eq(t, "celebrate_hunt duration", ch.duration, 4)
	h := comp(t, "minecraft:home", newHome, `{}`).(*home)
	eq(t, "restriction_radius", h.radius, -1)
	g := comp(t, "minecraft:grows_crop", newGrowsCrop, `{}`).(*growsCrop)
	eq(t, "charges", g.charges, 10)
	eq(t, "chance", g.chance, 0.0)
	s := comp(t, "minecraft:shareables", newShareables, `{"items":[{"item":"minecraft:bread","want_amount":3,"surplus_amount":6}]}`).(*Shareables)
	eq(t, "all_items_max_amount", s.AllMax, -1)
	eq(t, "max_amount", s.Items[0].MaxAmount, -1)
	eq(t, "pickup_limit", s.Items[0].PickupLimit, -1)
	sc := comp(t, "minecraft:scheduler", newScheduler, `{"scheduled_events":[{"event":"a"},{"event":"b"}]}`).(*scheduler)
	eq(t, "scheduler events", len(sc.events), 2)
	eq(t, "scheduler index", sc.cur, -1)
}

// TestTradeTables parses every embedded table: economy tables have five
// tiers (0, 10, 70, 150, 250 experience); items Dragonfly lacks are logged.
func TestTradeTables(t *testing.T) {
	err := fs.WalkDir(tradingFS, "trading", func(p string, de fs.DirEntry, err error) error {
		if err != nil || de.IsDir() {
			return err
		}
		tbl, err := LoadTradeTable(p)
		if err != nil {
			t.Errorf("%v: %v", p, err)
			return nil
		}
		if len(tbl.Tiers) == 0 {
			t.Errorf("%v: no tiers", p)
		}
		if strings.Contains(p, "economy_trades") && !strings.Contains(p, "wandering") {
			want := []int{0, 10, 70, 150, 250}
			if len(tbl.Tiers) != 5 {
				t.Errorf("%v: %v tiers, want 5", p, len(tbl.Tiers))
			}
			for i, tier := range tbl.Tiers {
				if i < len(want) && tier.ExpRequired != want[i] {
					t.Errorf("%v: tier %v needs %v, want %v", p, i, tier.ExpRequired, want[i])
				}
			}
		}
		unknown := 0
		for _, tier := range tbl.Tiers {
			for _, g := range tier.Groups {
				for _, td := range g.Trades {
					for _, slot := range append(append([][]TradeItemDef{}, td.Wants...), td.Gives...) {
						for _, it := range slot {
							if _, _, ok := makeStackFor(it); !ok {
								unknown++
							}
						}
					}
				}
			}
		}
		if unknown > 0 {
			// Maps, boats, bells, ... Dragonfly lacks: those trades are left out.
			t.Logf("%v: %v items Dragonfly does not know", p, unknown)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func makeStackFor(it TradeItemDef) (item.Stack, int, bool) {
	return makeStack(testRand(), it, it.Min)
}

// TestPricesAndRestock checks MerchantRecipe::calculateDemandPrices
// (cured discounts), resupplyTrades (demand) and the tier experience.
func TestPricesAndRestock(t *testing.T) {
	em := item.NewStack(item.Emerald{}, 1)
	e := &Economy{maxCuredMin: -25, maxCuredMax: -20, tierExp: []int{0, 10, 70, 150, 250}}
	o := &Offer{BuyA: em, BaseA: 10, PriceMulA: 0.05, MaxUses: 12}
	e.offers = []*Offer{o}
	e.low, e.high = -25, -20
	e.pricesFor(false, 0)
	// disc = -25 + 5*-20 = -125; (int)(-125*0.05) = -6.
	eq(t, "cured price", o.PriceA, 4)
	e.low, e.high = 0, 0
	o.Uses, o.Demand = 12, 0
	e.resupply()
	// demand = 0 + 12 - (12 - 12) = 12; price = 10 + floor(10*12*0.05) = 16.
	eq(t, "demand", o.Demand, 12)
	eq(t, "uses", o.Uses, 0)
	eq(t, "restocked price", o.PriceA, 16)
	e.pricesFor(true, 0)
	// hero: -max(1, floor(10*0.3)) = -3; dem = floor(10*12*0.05) = 6.
	eq(t, "hero price", o.PriceA, 13)
	for exp, want := range map[int]int{0: 0, 9: 0, 10: 1, 69: 1, 70: 2, 150: 3, 250: 4, 900: 4} {
		e.exp = exp
		eq(t, "tier", e.tierFromExp(), want)
	}
}
