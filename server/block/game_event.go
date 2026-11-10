package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/gameevent"
)

// postToggle posts block_open or block_close for a door, trapdoor or fence gate that user opened or
// closed (DoorBlock, TrapDoorBlock, FenceGateBlock). Nothing happens in a world without vibration
// listeners.
func postToggle(tx *world.Tx, pos cube.Pos, open bool, user any) {
	if !gameevent.Active(tx) {
		return
	}
	ev := gameevent.BlockClose
	if open {
		ev = gameevent.BlockOpen
	}
	src, _ := user.(world.Entity)
	gameevent.Post(tx, ev, pos.Vec3Centre(), src)
}

// postContainer posts container_open when a container's first viewer opens it and container_close
// when its last one closes it (ChestBlockActor, BarrelBlockActor; Java's ContainerOpenersCounter).
func postContainer(tx *world.Tx, pos cube.Pos, open bool) {
	if !gameevent.Active(tx) {
		return
	}
	ev := gameevent.ContainerClose
	if open {
		ev = gameevent.ContainerOpen
	}
	gameevent.Post(tx, ev, pos.Vec3Centre(), nil)
}
