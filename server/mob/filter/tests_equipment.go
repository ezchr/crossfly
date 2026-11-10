//go:build crossfly_full

package filter

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/item"
)

// Equipment tests. has_equipment follows ActorHasEquipmentTest: it collects
// the stacks in the domain and passes when any stack is the named item
// (ItemDescriptor::sameItem), then compares that with true.

// itemMatcher is a parsed item descriptor value ("shears", "minecraft:bucket:0").
type itemMatcher struct {
	name   string
	aux    int
	hasAux bool
}

func parseItem(s string) itemMatcher {
	m := itemMatcher{}
	parts := strings.Split(s, ":")
	// name, ns:name, name:aux, ns:name:aux
	if len(parts) >= 2 {
		if n, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
			m.aux, m.hasAux = n, true
			parts = parts[:len(parts)-1]
		}
	}
	m.name = ns(strings.Join(parts, ":"))
	return m
}

func (m itemMatcher) match(it Item) bool {
	name := ns(it.Name)
	if name != m.name && name != itemAliases[m.name] {
		return m.matchColourAlias(name)
	}
	// Aux 0 in a descriptor matches the default item, as ItemDescriptor with
	// aux value 0 does for items without variants.
	return !m.hasAux || m.aux == it.Aux || m.aux == 0x7fff
}

// matchColourAlias matches the items of a legacy name BDS registers as a
// complex alias split by colour (ComplexAliasDescriptor::sameItem: any item
// of the alias, so "carpet" is every colour of carpet but not moss carpet);
// an aux picks the colour of that legacy data value.
func (m itemMatcher) matchColourAlias(name string) bool {
	suffix, ok := colourItemAliases[m.name]
	if !ok {
		return false
	}
	for i, c := range item.Colours() {
		if name == "minecraft:"+c.String()+"_"+suffix {
			return !m.hasAux || m.aux == i || m.aux == 0x7fff
		}
	}
	return false
}

// itemAliases are legacy item names of the entity files that BDS resolves
// to a renamed item (ItemRegistry aliases).
var itemAliases = map[string]string{
	"minecraft:fireball":       "minecraft:fire_charge",
	"minecraft:carrotonastick": "minecraft:carrot_on_a_stick",
	"minecraft:lily_pad":       "minecraft:waterlily",
	"minecraft:melon":          "minecraft:melon_slice",
	"minecraft:speckled_melon": "minecraft:glistering_melon_slice",
	"minecraft:totem":          "minecraft:totem_of_undying",
	"minecraft:netherstar":     "minecraft:nether_star",
	"minecraft:tallgrass":      "minecraft:short_grass",
}

// colourItemAliases are the legacy names split into one item per colour, with
// the suffix of the colour items.
var colourItemAliases = map[string]string{
	"minecraft:wool": "wool", "minecraft:carpet": "carpet", "minecraft:concrete": "concrete",
	"minecraft:concrete_powder": "concrete_powder", "minecraft:stained_glass": "stained_glass",
	"minecraft:stained_glass_pane": "stained_glass_pane", "minecraft:stained_hardened_clay": "terracotta",
}

func setupItem(in *Input) error {
	in.Extra = parseItem(in.Str)
	return nil
}

func setupDomain(in *Input) error {
	if ParseDomain(in) == domainInvalid {
		return fmt.Errorf("unknown equipment domain %q", in.Domain)
	}
	return nil
}

func hasTag(it Item, tag string) bool {
	for _, t := range it.Tags {
		if t == tag || ns(t) == ns(tag) {
			return true
		}
	}
	return false
}

func hasEnchant(it Item, e string) bool {
	for _, x := range it.Enchantments {
		if stripNS(x) == stripNS(e) {
			return true
		}
	}
	return false
}

func init() {
	Register(&Def{Name: "has_equipment", Kind: KindString, Source: "bds 1.21.3 ActorHasEquipmentTest",
		Setup: func(in *Input) error {
			if err := setupDomain(in); err != nil {
				return err
			}
			return setupItem(in)
		},
		Eval: func(in *Input, s *Scope) bool {
			m := in.Extra.(itemMatcher)
			found := false
			for _, it := range itemsIn(s.Actor, ParseDomain(in)) {
				if m.match(it) {
					found = true
					break
				}
			}
			return in.CmpTrue(found)
		}})
	Register(&Def{Name: "has_equipment_tag", Kind: KindString, Source: "docs (1.21.30+)", Setup: setupDomain,
		Eval: func(in *Input, s *Scope) bool {
			found := false
			for _, it := range itemsIn(s.Actor, ParseDomain(in)) {
				if hasTag(it, in.Str) {
					found = true
					break
				}
			}
			return in.CmpTrue(found)
		}})
	Register(&Def{Name: "has_damaged_equipment", Kind: KindString, Source: "bds 1.21.3 ActorHasDamagedEquipmentTest",
		Setup: func(in *Input) error {
			if err := setupDomain(in); err != nil {
				return err
			}
			return setupItem(in)
		},
		Eval: func(in *Input, s *Scope) bool {
			m := in.Extra.(itemMatcher)
			found := false
			for _, it := range itemsIn(s.Actor, ParseDomain(in)) {
				if it.Damage > 0 && m.match(it) {
					found = true
					break
				}
			}
			return in.CmpTrue(found)
		}})
	// all_slots_empty / any_slot_empty take the location in "value"
	// (vanilla: {"test":"all_slots_empty","value":"hand","operator":"not"}).
	for _, name := range []string{"all_slots_empty", "any_slot_empty"} {
		all := name == "all_slots_empty"
		Register(&Def{Name: name, Kind: KindString, Source: "bds 1.21.3 ActorHasAllSlotsEmptyTest/ActorHasAnySlotEmptyTest",
			Setup: func(in *Input) error {
				d := domainByName(in.Str)
				if d == domainInvalid {
					return fmt.Errorf("unknown equipment location %q", in.Str)
				}
				in.Extra = d
				return nil
			},
			Eval: func(in *Input, s *Scope) bool {
				d := in.Extra.(Domain)
				if sc, ok := s.Actor.(SlotChecker); ok {
					return in.CmpTrue(sc.SlotsEmpty(d, all))
				}
				items := itemsIn(s.Actor, d)
				if all {
					return in.CmpTrue(len(items) == 0)
				}
				// Without slot knowledge only single-slot locations can be
				// answered: empty means the one slot is empty.
				return in.CmpTrue(len(items) == 0)
			}})
	}
	Register(&Def{Name: "has_silk_touch", Kind: KindBool, Source: "bds 1.21.3: held item has silk_touch",
		Eval: func(in *Input, s *Scope) bool {
			found := false
			for _, it := range itemsIn(s.Actor, DomainMainHand) {
				if hasEnchant(it, "silk_touch") {
					found = true
				}
			}
			return in.CmpBool(found)
		}})
	// actor_has_item_with_enchantment_in_slot is newer than the 1.21.3 dump.
	// The only vanilla use (sulfur_cube, frost_walker) gives domain 7, which
	// can only be the feet slot; string domains use the equipment names.
	Register(&Def{Name: "actor_has_item_with_enchantment_in_slot", Kind: KindString, Source: "inferred (1.26 data)",
		Setup: func(in *Input) error {
			d := domainByName(in.Domain)
			if in.DomainIsNum {
				d = domainInvalid
				if in.DomainNum == 7 {
					d = DomainFeet
				}
			}
			in.Extra = d
			return nil
		},
		Eval: func(in *Input, s *Scope) bool {
			found := false
			for _, it := range itemsIn(s.Actor, in.Extra.(Domain)) {
				if hasEnchant(it, in.Str) {
					found = true
				}
			}
			return in.CmpTrue(found)
		}})
	Register(&Def{Name: "has_same_equipment_in_slot_as", Kind: KindBool, Source: "docs: subject holds the same item as the calling entity in the slot", Setup: setupDomain,
		Eval: func(in *Input, s *Scope) bool {
			d := ParseDomain(in)
			a, b := itemsIn(s.Actor, d), itemsIn(s.Ctx.Self, d)
			same := len(a) > 0 && len(b) > 0 && ns(a[0].Name) == ns(b[0].Name) && a[0].Aux == b[0].Aux
			return in.CmpBool(same)
		}})
}
