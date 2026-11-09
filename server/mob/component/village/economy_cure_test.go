//go:build crossfly_full

package village

import (
	"testing"

	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func economy(t *testing.T, raw string) *Economy {
	t.Helper()
	return comp(t, "minecraft:economy_trade_table", newEconomy, raw).(*Economy)
}

// EconomyTradeableComponent::tryIncrementCuredDiscount: cured_discount adds
// to the low/high tier discounts, floored at max_cured_discount, never above
// 0.
func TestTryIncrementCuredDiscount(t *testing.T) {
	e := economy(t, `{"table":"x","cured_discount":[-25,-20],"max_cured_discount":[-25,-20]}`)
	e.tryIncrementCuredDiscount()
	eq(t, "low", e.low, -25)
	eq(t, "high", e.high, -20)
	e.tryIncrementCuredDiscount()
	eq(t, "low after a second cure", e.low, -25)
	eq(t, "high after a second cure", e.high, -20)

	e = economy(t, `{"table":"x","cured_discount":[-10,-5],"max_cured_discount":[-25,-20]}`)
	for i, want := range [][2]int{{-10, -5}, {-20, -10}, {-25, -15}, {-25, -20}} {
		e.tryIncrementCuredDiscount()
		if e.low != want[0] || e.high != want[1] {
			t.Errorf("cure %d: %d/%d, want %v", i+1, e.low, e.high, want)
		}
	}
	e = economy(t, `{"table":"x","cured_discount":[5,5]}`)
	e.tryIncrementCuredDiscount()
	eq(t, "positive cured_discount", e.low, 0)
}

// tryIncrementNearbyCuredDiscount: the economy formula sets the low tier
// discount to cured_discount.min + nearby_cured_discount (floored); the
// legacy formula adds to the nearby discount (floored at
// max_nearby_cured_discount) and stamps its decay a day ahead.
func TestTryIncrementNearbyCuredDiscount(t *testing.T) {
	e := economy(t, `{"table":"x"}`)
	e.low = -3
	e.tryIncrementNearbyCuredDiscount(100)
	eq(t, "economy low", e.low, -25)
	eq(t, "economy nearby", e.nearby, 0)
	e = economy(t, `{"table":"x","cured_discount":[-5,-5],"nearby_cured_discount":-3,"max_cured_discount":[-25,-20]}`)
	e.tryIncrementNearbyCuredDiscount(100)
	eq(t, "economy low (small)", e.low, -8)

	e = economy(t, `{"table":"x","use_legacy_price_formula":true,"nearby_cured_discount":-20,"max_nearby_cured_discount":-50}`)
	e.tryIncrementNearbyCuredDiscount(100)
	eq(t, "legacy nearby", e.nearby, -20)
	eq(t, "legacy stamp", e.nearbyStamp, int64(24100))
	e.tryIncrementNearbyCuredDiscount(200)
	e.tryIncrementNearbyCuredDiscount(300)
	eq(t, "legacy nearby floored", e.nearby, -50)
	eq(t, "legacy low untouched", e.low, 0)
}

// tickDiscountDegradationTimer: the nearby discount fades by one each day.
func TestNearbyCuredDiscountDecays(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	r := w.Spawn("minecraft:villager_v2", mgl64.Vec3{0.5, 1, 0.5})
	e := economy(t, `{"table":"x","use_legacy_price_formula":true}`)
	w.Do(func(tx *world.Tx) {
		m, _ := r.Mob(tx)
		now := tx.CurrentTick()
		e.setNearby(-3, now-24000)
		e.Tick(m)
		eq(t, "nearby after a day", e.nearby, -2)
		eq(t, "next decay", e.nearbyStamp, now+24000)
		e.Tick(m)
		eq(t, "nearby within the day", e.nearby, -2)
	})
}

// Village::rewardAllPlayers: LARGE_REWARD (5) for every known player, kept
// within -30..10.
func TestRewardAllStanding(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	a := w.Spawn("minecraft:villager_v2", mgl64.Vec3{0.5, 1, 0.5})
	b := w.Spawn("minecraft:villager_v2", mgl64.Vec3{2.5, 1, 0.5})
	c := w.Spawn("minecraft:villager_v2", mgl64.Vec3{4.5, 1, 0.5})
	v := &Village{standing: map[*world.EntityHandle]int{a.H(): 0, b.H(): 8, c.H(): -30}}
	v.rewardAllStanding(standingLargeReward)
	eq(t, "standing 0", v.standing[a.H()], 5)
	eq(t, "standing 8", v.standing[b.H()], 10)
	eq(t, "standing -30", v.standing[c.H()], -25)
}
