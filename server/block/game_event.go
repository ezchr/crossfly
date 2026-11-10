package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/gameevent"
)

// postToggle posts block_open or block_close for a door, trapdoor or fence gate that user opened or
// closed, user being nil when redstone did it (DoorBlock::setToggled, TrapDoorBlock, FenceGateBlock).
// Nothing happens in a world without vibration listeners.
func postToggle(tx *world.Tx, pos cube.Pos, open bool, user any) {
	ev := gameevent.BlockClose
	if open {
		ev = gameevent.BlockOpen
	}
	postBlockEvent(tx, ev, pos, user)
}

// postContainer posts container_open when a container's first viewer opens it and container_close
// when its last one closes it (ChestBlockActor, BarrelBlockActor; Java's ContainerOpenersCounter).
func postContainer(tx *world.Tx, pos cube.Pos, open bool) {
	ev := gameevent.ContainerClose
	if open {
		ev = gameevent.ContainerOpen
	}
	postBlockEvent(tx, ev, pos, nil)
}

// postActivate posts block_activate when a lever, button, pressure plate or tripwire hook switches on and
// block_deactivate when it switches off (LeverBlock::toggle, ButtonBlock::buttonPressed and
// _buttonUnpressed, BasePressurePlateBlock::checkPressed, TripWireHookBlock::_emitState). src is who did
// it, if anyone.
func postActivate(tx *world.Tx, pos cube.Pos, on bool, src any) {
	ev := gameevent.BlockDeactivate
	if on {
		ev = gameevent.BlockActivate
	}
	postBlockEvent(tx, ev, pos, src)
}

// postBlockEvent posts ev at the centre of pos, made by src if it is an entity. Nothing happens in a world
// without vibration listeners.
func postBlockEvent(tx *world.Tx, ev gameevent.Event, pos cube.Pos, src any) {
	if !gameevent.Active(tx) {
		return
	}
	e, _ := src.(world.Entity)
	gameevent.Post(tx, ev, pos.Vec3Centre(), e)
}
