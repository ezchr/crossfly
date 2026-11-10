//go:build crossfly_full

package mob

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func init() {
	item.SetDispenserShear(func(tx *world.Tx, e world.Entity) bool {
		m, ok := Of(e)
		return ok && m.ShearByDispenser(tx)
	})
}

// dispenserShear is the shearing of a mob by a dispenser, installed by package mob/component/core (which has the
// loot tables) with SetDispenserShear.
var dispenserShear func(m *Mob) bool

// SetDispenserShear installs the function that shears a mob in front of a dispenser holding shears and reports
// whether it did. Call it once, from an init function.
func SetDispenserShear(f func(m *Mob) bool) { dispenserShear = f }

// ShearByDispenser shears the mob for a dispenser holding shears (ShearsItem::_tryShearActor) and reports whether it
// was sheared. Dispensers reach it through item.SetDispenserShear.
func (m *Mob) ShearByDispenser(tx *world.Tx) bool {
	if dispenserShear == nil || m.Dead() || m.Tx() != tx {
		return false
	}
	return dispenserShear(m)
}
