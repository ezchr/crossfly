//go:build crossfly_full

package item

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// SpawnFromDispenser spawns the egg's mob at pos for a dispenser (ActorPlacerItem::dispense, redstone=dragonfly),
// turned towards the point towards and named nameTag if it is not empty. It reports whether a mob was spawned:
// without mobs, nothing is.
func (s SpawnEgg) SpawnFromDispenser(tx *world.Tx, pos, towards mgl64.Vec3, nameTag string) bool {
	return mobSpawner != nil && mobSpawner.SpawnFromEgg(tx, s.Mob, pos, towards, nameTag)
}
