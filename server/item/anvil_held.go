package item

import (
	"strconv"
	"strings"
)

// AnvilHeldKey is the value (Stack.WithValue) of an item whose enchantments a Bedrock anvil took
// off while it lies in the anvil next to a book that raises them past what the Bedrock client
// accepts (session.AnvilRules.OverMax): every enchantment the item had, "id:level,...".
// RestoreAnvilHeld gives them back, which happens whenever the item leaves the anvil.
const AnvilHeldKey = "crossfly:anvil_held"

// WithAnvilHeld returns s without the enchantments of the types passed, keeping every enchantment s
// had (before any earlier WithAnvilHeld) in AnvilHeldKey.
func WithAnvilHeld(s Stack, types ...EnchantmentType) Stack {
	orig := RestoreAnvilHeld(s)
	parts := make([]string, 0, len(orig.Enchantments()))
	for _, e := range orig.Enchantments() {
		if id, ok := EnchantmentID(e.Type()); ok {
			parts = append(parts, strconv.Itoa(id)+":"+strconv.Itoa(e.Level()))
		}
	}
	return orig.WithoutEnchantments(types...).WithValue(AnvilHeldKey, strings.Join(parts, ","))
}

// RestoreAnvilHeld returns s with the enchantments WithAnvilHeld kept and without AnvilHeldKey. A
// stack without the value is returned as it is.
func RestoreAnvilHeld(s Stack) Stack {
	v, ok := s.Value(AnvilHeldKey)
	if !ok {
		return s
	}
	s = s.WithValue(AnvilHeldKey, nil)
	str, _ := v.(string)
	var ench []Enchantment
	for _, p := range strings.Split(str, ",") {
		id, lvl, ok := strings.Cut(p, ":")
		if !ok {
			continue
		}
		i, err1 := strconv.Atoi(id)
		l, err2 := strconv.Atoi(lvl)
		if err1 != nil || err2 != nil {
			continue
		}
		if t, ok := EnchantmentByID(i); ok {
			ench = append(ench, NewEnchantment(t, l))
		}
	}
	return s.WithForcedEnchantments(ench...)
}

// AnvilHeld reports whether s is an item with enchantments a Bedrock anvil took off (AnvilHeldKey).
func AnvilHeld(s Stack) bool {
	_, ok := s.Value(AnvilHeldKey)
	return ok
}
