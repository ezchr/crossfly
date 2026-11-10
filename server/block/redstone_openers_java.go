//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// Doors, trapdoors and fence gates that redstone opens and closes, as in Java Edition (Crossfly Full, Java
// redstone).

func (WoodDoor) JavaRedstoneExtension()       {}
func (CopperDoor) JavaRedstoneExtension()     {}
func (WoodTrapdoor) JavaRedstoneExtension()   {}
func (CopperTrapdoor) JavaRedstoneExtension() {}
func (WoodFenceGate) JavaRedstoneExtension()  {}

// RedstonePowerActionUpdate opens or closes the door when its power switches on or off, as Java Edition's DoorBlock:
// the door is powered when either half is, and a player may still open or close it by hand while it stays powered.
func (d WoodDoor) RedstonePowerActionUpdate(pos cube.Pos, tx *world.Tx, u world.RedstoneUpdate) {
	if !world.JavaRedstone() || (u.OldPower > 0) == (u.NewPower > 0) {
		return
	}
	otherPos := pos.Side(cube.FaceDown)
	if !d.Top {
		otherPos = pos.Side(cube.FaceUp)
	}
	powered := railNeighbourSignal(tx, pos) || railNeighbourSignal(tx, otherPos)
	if powered == d.Open {
		return
	}
	d.Open = powered
	tx.SetBlock(pos, d, nil)
	if other, ok := tx.Block(otherPos).(WoodDoor); ok {
		other.Open = powered
		tx.SetBlock(otherPos, other, nil)
	}
	if powered {
		tx.PlaySound(pos.Vec3Centre(), sound.DoorOpen{Block: d})
	} else {
		tx.PlaySound(pos.Vec3Centre(), sound.DoorClose{Block: d})
	}
	postToggle(tx, pos, powered, nil)
}

// RedstonePowerActionUpdate opens or closes the door when its power switches on or off, as Java Edition's DoorBlock:
// the door is powered when either half is, and a player may still open or close it by hand while it stays powered.
func (d CopperDoor) RedstonePowerActionUpdate(pos cube.Pos, tx *world.Tx, u world.RedstoneUpdate) {
	if !world.JavaRedstone() || (u.OldPower > 0) == (u.NewPower > 0) {
		return
	}
	otherPos := pos.Side(cube.FaceDown)
	if !d.Top {
		otherPos = pos.Side(cube.FaceUp)
	}
	powered := railNeighbourSignal(tx, pos) || railNeighbourSignal(tx, otherPos)
	if powered == d.Open {
		return
	}
	d.Open = powered
	tx.SetBlock(pos, d, nil)
	if other, ok := tx.Block(otherPos).(CopperDoor); ok {
		other.Open = powered
		tx.SetBlock(otherPos, other, nil)
	}
	if powered {
		tx.PlaySound(pos.Vec3Centre(), sound.DoorOpen{Block: d})
	} else {
		tx.PlaySound(pos.Vec3Centre(), sound.DoorClose{Block: d})
	}
	postToggle(tx, pos, powered, nil)
}

// RedstonePowerActionUpdate opens or closes the trapdoor when its power switches on or off, as Java Edition's TrapDoorBlock:
// a player may still open or close it by hand while it stays powered.
func (t CopperTrapdoor) RedstonePowerActionUpdate(pos cube.Pos, tx *world.Tx, u world.RedstoneUpdate) {
	if !world.JavaRedstone() || (u.OldPower > 0) == (u.NewPower > 0) {
		return
	}
	powered := railNeighbourSignal(tx, pos)
	if powered == t.Open {
		return
	}
	t.Open = powered
	tx.SetBlock(pos, t, nil)
	if powered {
		tx.PlaySound(pos.Vec3Centre(), sound.TrapdoorOpen{Block: t})
	} else {
		tx.PlaySound(pos.Vec3Centre(), sound.TrapdoorClose{Block: t})
	}
	postToggle(tx, pos, powered, nil)
}

// RedstonePowerActionUpdate opens or closes the trapdoor when its power switches on or off, as Java Edition's TrapDoorBlock:
// a player may still open or close it by hand while it stays powered.
func (t WoodTrapdoor) RedstonePowerActionUpdate(pos cube.Pos, tx *world.Tx, u world.RedstoneUpdate) {
	if !world.JavaRedstone() || (u.OldPower > 0) == (u.NewPower > 0) {
		return
	}
	powered := railNeighbourSignal(tx, pos)
	if powered == t.Open {
		return
	}
	t.Open = powered
	tx.SetBlock(pos, t, nil)
	if powered {
		tx.PlaySound(pos.Vec3Centre(), sound.TrapdoorOpen{Block: t})
	} else {
		tx.PlaySound(pos.Vec3Centre(), sound.TrapdoorClose{Block: t})
	}
	postToggle(tx, pos, powered, nil)
}

// RedstonePowerActionUpdate opens or closes the fence gate when its power switches on or off, as Java Edition's FenceGateBlock:
// a player may still open or close it by hand while it stays powered.
func (f WoodFenceGate) RedstonePowerActionUpdate(pos cube.Pos, tx *world.Tx, u world.RedstoneUpdate) {
	if !world.JavaRedstone() || (u.OldPower > 0) == (u.NewPower > 0) {
		return
	}
	powered := railNeighbourSignal(tx, pos)
	if powered == f.Open {
		return
	}
	f.Open = powered
	tx.SetBlock(pos, f, nil)
	if powered {
		tx.PlaySound(pos.Vec3Centre(), sound.FenceGateOpen{Block: f})
	} else {
		tx.PlaySound(pos.Vec3Centre(), sound.FenceGateClose{Block: f})
	}
	postToggle(tx, pos, powered, nil)
}
