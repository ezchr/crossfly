//go:build crossfly_full

package jvillage

import (
	"bytes"
	"math/rand/v2"
	"os"
	"strings"
	"testing"

	_ "github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

var steve = uuid.MustParse("00000000-0000-0000-0000-00000000aaaa")
var alex = uuid.MustParse("00000000-0000-0000-0000-00000000bbbb")

func TestGossipAddCapDecay(t *testing.T) {
	g := &Gossips{}
	g.Add(steve, MinorNegative, 25) // hit once (VILLAGER_HURT)
	g.Add(steve, MinorNegative, 25)
	if v := g.Get(steve, MinorNegative); v != 50 {
		t.Fatalf("minor negative %d", v)
	}
	if r := g.Reputation(steve, nil); r != -50 {
		t.Fatalf("reputation %d", r)
	}
	for range 10 {
		g.Add(steve, MinorNegative, 25)
	}
	if v := g.Get(steve, MinorNegative); v != 200 {
		t.Fatalf("minor negative not capped at 200: %d", v)
	}
	// Curing: major positive 20 (cap 20) and minor positive 25 (cap 25).
	g.Add(alex, MajorPositive, 20)
	g.Add(alex, MinorPositive, 25)
	g.Add(alex, MajorPositive, 20)
	if r := g.Reputation(alex, nil); r != 20*5+25 {
		t.Fatalf("cured reputation %d, want 125", r)
	}
	// Decay: minor negative 200 -> 180, major positive never decays,
	// minor positive 25 -> 24.
	g.Decay()
	if g.Get(steve, MinorNegative) != 180 || g.Get(alex, MajorPositive) != 20 || g.Get(alex, MinorPositive) != 24 {
		t.Fatalf("after decay %d %d %d", g.Get(steve, MinorNegative), g.Get(alex, MajorPositive), g.Get(alex, MinorPositive))
	}
	g.Add(alex, Trading, 2)
	g.Decay() // trading 2 - 2 = 0 < 2: dropped
	if g.Get(alex, Trading) != 0 {
		t.Fatal("trading gossip below the threshold kept")
	}
	// A gossip below 2 after an add is removed.
	g.Remove(steve, MinorNegative, 179)
	if g.Get(steve, MinorNegative) != 0 {
		t.Fatal("value 1 kept")
	}
}

func TestGossipTransfer(t *testing.T) {
	src := &Gossips{}
	src.Add(steve, MajorNegative, 25) // weighted 125
	src.Add(alex, Trading, 10)        // weighted 10
	dst := &Gossips{}
	r := rand.New(rand.NewPCG(1, 2))
	n := dst.TransferFrom(src, r, 10)
	if n < 1 || n > 2 {
		t.Fatalf("picked %d", n)
	}
	// major negative transfers decayed by 10; trading 10-20 < 2 never does.
	if v := dst.Get(steve, MajorNegative); v != 15 {
		t.Fatalf("major negative transferred as %d, want 15", v)
	}
	if dst.Get(alex, Trading) != 0 {
		t.Fatal("trading gossip transferred below the threshold")
	}
	// The higher value is kept.
	dst.Add(steve, MajorNegative, 50)
	dst.TransferFrom(src, r, 10)
	if v := dst.Get(steve, MajorNegative); v != 65 {
		t.Fatalf("merge kept %d, want 65", v)
	}
	// NBT round trip.
	data, err := nbt.MarshalEncoding(map[string]any{"Gossips": dst.NBT()}, nbt.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := nbt.NewDecoderWithEncoding(bytes.NewReader(data), nbt.LittleEndian).Decode(&back); err != nil {
		t.Fatal(err)
	}
	l, _ := back["Gossips"].([]any)
	if g2 := GossipsFromNBT(l); g2.Get(steve, MajorNegative) != 65 {
		t.Fatal("gossips lost in NBT")
	}
}

func TestOfferPrices(t *testing.T) {
	// Farmer: 20 wheat -> 1 emerald... use emerald costs: 1 emerald for 6
	// bread, priceMultiplier 0.05, max uses 16.
	o := NewOffer(item.NewStack(item.Emerald{}, 1), item.Stack{}, item.NewStack(item.Bread{}, 6), 16, 1, 0.05)
	if o.CostA().Count() != 1 {
		t.Fatal("base price")
	}
	// Sold out twice without restocks: demand grows by uses - (max - uses).
	o.Uses = 16
	o.UpdateDemand()
	if o.Demand != 16 {
		t.Fatalf("demand %d", o.Demand)
	}
	o.ResetUses()
	o.UpdateDemand() // 16 + 0 - 16 = 0
	if o.Demand != 0 {
		t.Fatalf("demand after an unused day %d", o.Demand)
	}
	// Negative demand is kept (Paper default) and never lowers the price.
	o.UpdateDemand()
	if o.Demand != -16 || o.CostA().Count() != 1 {
		t.Fatalf("demand %d price %d", o.Demand, o.CostA().Count())
	}
	// 64 emerald price with demand: floor(20*demand*0.05) added.
	b := NewOffer(item.NewStack(item.Emerald{}, 20), item.Stack{}, item.NewStack(item.Bread{}, 1), 12, 1, 0.05)
	b.Demand = 10
	if c := b.CostA().Count(); c != 30 {
		t.Fatalf("price with demand %d, want 30", c)
	}
	// Reputation discount: -floor(rep * multiplier); clamp at 1 and at the
	// stack size.
	b.Demand = 0
	b.AddSpecialPrice(-floorF(float64(float32(125) * b.PriceMultiplier)))
	if c := b.CostA().Count(); c != 14 {
		t.Fatalf("price with reputation 125: %d, want 14", c)
	}
	b.SpecialPriceDiff = -100
	if c := b.CostA().Count(); c != 1 {
		t.Fatalf("price floor %d", c)
	}
	b.SpecialPriceDiff = 100
	if c := b.CostA().Count(); c != 64 {
		t.Fatalf("price ceiling %d", c)
	}
	// Paying.
	b.SpecialPriceDiff = 0
	a, _, ok := b.Take(item.NewStack(item.Emerald{}, 25), item.Stack{})
	if !ok || a.Count() != 5 {
		t.Fatal("take")
	}
	if _, _, ok := b.Take(item.NewStack(item.Emerald{}, 19), item.Stack{}); ok {
		t.Fatal("took too little")
	}
}

func TestOffersNBT(t *testing.T) {
	o := NewOffer(item.NewStack(item.Emerald{}, 3), item.NewStack(item.Book{}, 1), item.NewStack(item.Bread{}, 6), 12, 5, 0.2)
	o.Uses, o.Demand, o.SpecialPriceDiff = 4, 7, -2
	m := Offers{o}.NBT()
	data, err := nbt.MarshalEncoding(m, nbt.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := nbt.NewDecoderWithEncoding(bytes.NewReader(data), nbt.LittleEndian).Decode(&back); err != nil {
		t.Fatal(err)
	}
	got := OffersFromNBT(back)
	if len(got) != 1 {
		t.Fatal("offer lost")
	}
	g := got[0]
	if g.BuyA.Count() != 3 || g.BuyB.Count() != 1 || g.Sell.Count() != 6 || g.Uses != 4 || g.MaxUses != 12 || g.Demand != 7 || g.SpecialPriceDiff != -2 || g.XP != 5 || g.PriceMultiplier != 0.2 || !g.RewardExp {
		t.Fatalf("%+v", g)
	}
}

func TestXPLevels(t *testing.T) {
	want := map[int][2]int{1: {0, 10}, 2: {10, 70}, 3: {70, 150}, 4: {150, 250}, 5: {0, 0}}
	for l, w := range want {
		if MinXPPerLevel(l) != w[0] || MaxXPPerLevel(l) != w[1] {
			t.Fatalf("level %d: %d..%d", l, MinXPPerLevel(l), MaxXPPerLevel(l))
		}
	}
}

func TestMain(m *testing.M) {
	item.RegisterMobItems()
	os.Exit(m.Run())
}

func TestAllTradesParse(t *testing.T) {
	d := data()
	for k, err := range d.missing {
		// Only items Crossfly lacks may drop a trade.
		if !strings.Contains(err.Error(), "no item for") {
			t.Errorf("trade %v: %v", k, err)
		}
	}
	if len(d.trades) < 350 {
		t.Fatalf("only %d of %d trades loaded", len(d.trades), len(d.raw.Trades))
	}
	t.Logf("%d of %d trades loaded; left out: %d", len(d.trades), len(d.raw.Trades), len(d.missing))
}

func TestTradeSetsRoll(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 7))
	for _, p := range Professions {
		for level := 1; level <= 5; level++ {
			set, ok := p.TradeSet(level)
			if !ok {
				continue
			}
			for _, vt := range VillagerTypes {
				for range 20 {
					var offers Offers
					if !AddOffersFromTradeSet(&offers, set, -1, vt, r) {
						t.Fatalf("no trade set %v", set)
					}
					// A set with fewer trades than its amount gives them all.
					want := min(2, len(data().sets[set].trades))
					if data().sets[set].incomplete {
						want = min(want, len(offers))
					}
					if len(offers) != want {
						t.Fatalf("%v (%v): %d offers, want 2", set, vt, len(offers))
					}
					for _, o := range offers {
						if o.BuyA.Empty() || o.Sell.Empty() || o.CostA().Count() < 1 || o.MaxUses < 1 {
							t.Fatalf("%v: bad offer %+v", set, o)
						}
					}
				}
			}
		}
	}
}

func TestLibrarianBookPrices(t *testing.T) {
	// Level 1 librarians sell enchanted books for 2+rand(5+10l)+3l emeralds
	// (doubled for treasure), capped at 64, plus a book.
	r := rand.New(rand.NewPCG(3, 9))
	books := 0
	for range 400 {
		var offers Offers
		AddOffersFromTradeSet(&offers, "librarian/level_1", -1, TypePlains, r)
		for _, o := range offers {
			if _, ok := o.Sell.Item().(item.EnchantedBook); !ok {
				continue
			}
			books++
			if _, ok := o.BuyB.Item().(item.Book); !ok {
				t.Fatal("enchanted book offer without a book cost")
			}
			if len(o.Sell.Enchantments()) == 0 {
				// sweeping edge has no Bedrock enchantment
				continue
			}
			lvl := o.Sell.Enchantments()[0].Level()
			lo := 2 + 3*lvl
			if c := o.BuyA.Count(); c < lo || c > 64 {
				t.Fatalf("book with level %d costs %d", lvl, c)
			}
		}
	}
	if books == 0 {
		t.Fatal("no enchanted books rolled")
	}
}

func TestMerchantPredicate(t *testing.T) {
	// Fisherman level 5 boat trades depend on the villager type.
	r := rand.New(rand.NewPCG(5, 5))
	seen := map[string]bool{}
	for range 300 {
		var offers Offers
		AddOffersFromTradeSet(&offers, "fisherman/level_5", -1, TypeSnow, r)
		for _, o := range offers {
			name, _ := o.BuyA.Item().EncodeItem()
			seen[name] = true
		}
	}
	if seen["minecraft:oak_boat"] {
		t.Fatal("snow fisherman bought an oak boat (plains only)")
	}
}
