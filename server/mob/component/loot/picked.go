//go:build crossfly_full

package loot

import "github.com/df-mc/dragonfly/server/item"

// pickedUpKey is the item.Stack value set on items a mob picked up (BDS
// ItemStackBase::mWasPickedUp, saved as WasPickedUp). Such items always drop
// when the mob dies, whatever the slot's drop chance.
const pickedUpKey = "mob:picked_up"

// MarkPickedUp returns s marked as picked up by a mob.
func MarkPickedUp(s item.Stack) item.Stack {
	if s.Empty() {
		return s
	}
	return s.WithValue(pickedUpKey, true)
}

// WasPickedUp reports whether s carries the picked-up mark.
func WasPickedUp(s item.Stack) bool {
	v, ok := s.Value(pickedUpKey)
	b, _ := v.(bool)
	return ok && b
}

// ClearPickedUp removes the mark, so a dropped item stacks with others.
func ClearPickedUp(s item.Stack) item.Stack {
	if !WasPickedUp(s) {
		return s
	}
	return s.WithValue(pickedUpKey, nil)
}
