package entity

// PickupReady reports whether the item's pickup delay is over (ItemEntity.hasPickUpDelay false): mobs that pick
// items up (a Java fox) wait for it as players do.
func (i *ItemBehaviour) PickupReady() bool { return i.pickupDelay <= 0 }
