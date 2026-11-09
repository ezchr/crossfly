//go:build crossfly_full

package core

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
)

// InteractPickupFunc handles the admire/barter flags of a minecraft:interact
// interaction (the piglin taking a gold ingot from a player's hand). BDS
// InteractComponent::_runInteraction runs them in its interaction lambda as
// if the mob had picked the item up: admire hands one of the held item to
// AdmireItemComponent::onAdmireItemPickedUp (unless the mob admires something
// already) and barter calls BarterComponent::onBarterItemPickedUp with the
// player as the thrower. Package village installs it.
type InteractPickupFunc func(m *mob.Mob, user item.User, held item.Stack, admire, barter bool)

var interactPickup InteractPickupFunc

// SetInteractPickup installs the admire/barter handler. Call it from init
// only.
func SetInteractPickup(f InteractPickupFunc) { interactPickup = f }

// runPickup runs the admire/barter part of in with the item the user held
// before the interaction used it up.
func (in *interaction) runPickup(m *mob.Mob, user item.User, held item.Stack) {
	if (in.Admire || in.Barter) && interactPickup != nil && !held.Empty() {
		interactPickup(m, user, held, in.Admire, in.Barter)
	}
}
