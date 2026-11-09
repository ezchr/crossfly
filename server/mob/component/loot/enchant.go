//go:build crossfly_full

package loot

import (
	"math"
	"math/rand/v2"
	"slices"

	"github.com/df-mc/dragonfly/server/item"
)

// treasure is implemented by enchantments that never come from an enchanting
// table (Mending, Curse of Vanishing, Soul Speed, Swift Sneak).
type treasure interface{ Treasure() bool }

// RandomlyEnchant is BDS EnchantUtils::randomlyEnchant (the enchanting
// table's selection): the cost is level + 1 + two rolls of
// rand(enchantability/4 + 1), varied by up to 15%; one enchantment is picked
// by rarity weight among those whose cost range holds the cost, then more
// while rand(50) <= cost, halving the cost each time. allowTreasure allows
// treasure enchantments. Items that cannot be enchanted come back as they
// are; a book becomes an enchanted book.
func RandomlyEnchant(r *rand.Rand, s item.Stack, level int, allowTreasure bool) item.Stack {
	if s.Empty() {
		return s
	}
	it := s.Item()
	_, book := it.(item.Book)
	value := 1
	if e, ok := it.(item.Enchantable); ok {
		value = e.EnchantmentValue()
	} else if !book {
		return s
	}
	if value <= 0 {
		return s
	}
	cost := level + 1 + r.IntN(value/4+1) + r.IntN(value/4+1)
	bonus := (r.Float64() + r.Float64() - 1) * 0.15
	cost = max(int(math.Round(float64(cost)+float64(cost)*bonus)), 1)

	var avail []item.Enchantment
	for _, t := range item.Enchantments() {
		if tr, ok := t.(treasure); ok && tr.Treasure() && !allowTreasure {
			continue
		}
		if !book && !t.CompatibleWithItem(it) {
			continue
		}
		for lvl := t.MaxLevel(); lvl > 0; lvl-- {
			if lo, hi := t.Cost(lvl); cost >= lo && cost <= hi {
				avail = append(avail, item.NewEnchantment(t, lvl))
				break
			}
		}
	}
	if len(avail) == 0 {
		return s
	}
	var picked []item.Enchantment
	pick := func() {
		e := weighted(r, avail)
		picked = append(picked, e)
		i := slices.IndexFunc(avail, func(o item.Enchantment) bool { return o.Type() == e.Type() })
		avail = slices.Delete(avail, i, i+1)
	}
	pick()
	for r.IntN(50) <= cost {
		last := picked[len(picked)-1].Type()
		avail = slices.DeleteFunc(avail, func(o item.Enchantment) bool {
			return !last.CompatibleWithEnchantment(o.Type()) || !o.Type().CompatibleWithEnchantment(last)
		})
		if len(avail) == 0 {
			break
		}
		pick()
		cost /= 2
	}
	return s.WithEnchantments(picked...)
}

func weighted(r *rand.Rand, es []item.Enchantment) item.Enchantment {
	total := 0
	for _, e := range es {
		total += e.Type().Rarity().Weight()
	}
	if total <= 0 {
		return es[r.IntN(len(es))]
	}
	n := r.IntN(total)
	for _, e := range es {
		if n -= e.Type().Rarity().Weight(); n < 0 {
			return e
		}
	}
	return es[len(es)-1]
}

// EnchantRandomGear is BDS EnchantRandomEquipmentFunction
// (enchant_random_gear): with chance x the special multiplier, enchant at
// level int(rand(18) x multiplier + 5).
func EnchantRandomGear(r *rand.Rand, s item.Stack, chance, multiplier float64) item.Stack {
	if r.Float32() >= float32(multiplier*chance) {
		return s
	}
	lvl := int(float64(r.IntN(18))*multiplier + 5)
	return RandomlyEnchant(r, s, lvl, false)
}
