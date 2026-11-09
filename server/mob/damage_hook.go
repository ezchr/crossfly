//go:build crossfly_full

package mob

import "fmt"

// DamageHook runs for every mob before damage is applied, after the
// DamageModifier components, whatever its components: hard-coded class
// rules of BDS (Witch::getDamageAfterEnchantReduction) that no component of
// the definition carries. Modify may change d.Amount or set d.Cancel.
type DamageHook struct {
	Name   string
	Modify func(m *Mob, d *Damage)
}

var damageHooks []DamageHook

// RegisterDamageHook adds a damage hook (from init only).
func RegisterDamageHook(h DamageHook) {
	if h.Modify == nil || h.Name == "" {
		panic(fmt.Sprintf("mob: invalid damage hook %+v", h))
	}
	for _, o := range damageHooks {
		if o.Name == h.Name {
			panic(fmt.Sprintf("mob: damage hook %q registered twice", h.Name))
		}
	}
	damageHooks = append(damageHooks, h)
}
