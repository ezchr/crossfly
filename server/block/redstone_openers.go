package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// Doors, trapdoors and fence gates are redstone consumers in both redstone modes. With Java redstone they follow Java
// Edition (redstone_openers_java.go). With Dragonfly's redstone they follow Bedrock Edition's consumers: when the
// power they take in changes, a trapdoor or fence gate opens if it is powered and closes if not, and a door does the
// same with the power of either half. A door opened or closed by hand stays so until its power changes again.

var (
	_ world.RedstonePowerContextAction = WoodDoor{}
	_ world.RedstonePowerContextAction = CopperDoor{}
	_ world.RedstonePowerContextAction = WoodTrapdoor{}
	_ world.RedstonePowerContextAction = CopperTrapdoor{}
	_ world.RedstonePowerContextAction = WoodFenceGate{}
)

// openerSetOpts are the options doors, trapdoors and fence gates are set with when they change by hand or shape.
// With Dragonfly's redstone the engine keeps the power they take in: a change by hand is no change of power, and
// must not make the next evaluation see one.
func openerSetOpts() *world.SetOpts {
	if world.JavaRedstone() {
		return nil
	}
	return &world.SetOpts{KeepRedstonePower: true}
}

// RedstonePowerActionUpdate opens or closes the door when its power changes.
func (d WoodDoor) RedstonePowerActionUpdate(pos cube.Pos, tx *world.Tx, u world.RedstoneUpdate) {
	if world.JavaRedstone() {
		d.javaRedstoneUpdate(pos, tx, u)
		return
	}
	if !u.PowerChanged() {
		return
	}
	otherPos := doorOtherHalf(pos, d.Top)
	open := u.NewPower > 0 || tx.RedstonePower(otherPos) > 0
	if open == d.Open {
		return
	}
	d.Open = open
	tx.SetBlock(pos, d, openerSetOpts())
	if other, ok := tx.Block(otherPos).(WoodDoor); ok && other.Open != open {
		other.Open = open
		tx.SetBlock(otherPos, other, openerSetOpts())
	}
	playDoorSound(pos, tx, open, d)
}

// RedstonePowerActionUpdate opens or closes the door when its power changes.
func (d CopperDoor) RedstonePowerActionUpdate(pos cube.Pos, tx *world.Tx, u world.RedstoneUpdate) {
	if world.JavaRedstone() {
		d.javaRedstoneUpdate(pos, tx, u)
		return
	}
	if !u.PowerChanged() {
		return
	}
	otherPos := doorOtherHalf(pos, d.Top)
	open := u.NewPower > 0 || tx.RedstonePower(otherPos) > 0
	if open == d.Open {
		return
	}
	d.Open = open
	tx.SetBlock(pos, d, openerSetOpts())
	if other, ok := tx.Block(otherPos).(CopperDoor); ok && other.Open != open {
		other.Open = open
		tx.SetBlock(otherPos, other, openerSetOpts())
	}
	playDoorSound(pos, tx, open, d)
}

// RedstonePowerActionUpdate opens or closes the trapdoor when its power changes.
func (t WoodTrapdoor) RedstonePowerActionUpdate(pos cube.Pos, tx *world.Tx, u world.RedstoneUpdate) {
	if world.JavaRedstone() {
		t.javaRedstoneUpdate(pos, tx, u)
		return
	}
	if !u.PowerChanged() || (u.NewPower > 0) == t.Open {
		return
	}
	t.Open = u.NewPower > 0
	tx.SetBlock(pos, t, openerSetOpts())
	playTrapdoorSound(pos, tx, t.Open, t)
}

// RedstonePowerActionUpdate opens or closes the trapdoor when its power changes.
func (t CopperTrapdoor) RedstonePowerActionUpdate(pos cube.Pos, tx *world.Tx, u world.RedstoneUpdate) {
	if world.JavaRedstone() {
		t.javaRedstoneUpdate(pos, tx, u)
		return
	}
	if !u.PowerChanged() || (u.NewPower > 0) == t.Open {
		return
	}
	t.Open = u.NewPower > 0
	tx.SetBlock(pos, t, openerSetOpts())
	playTrapdoorSound(pos, tx, t.Open, t)
}

// RedstonePowerActionUpdate opens or closes the fence gate when its power changes. It opens the way it faces.
func (f WoodFenceGate) RedstonePowerActionUpdate(pos cube.Pos, tx *world.Tx, u world.RedstoneUpdate) {
	if world.JavaRedstone() {
		f.javaRedstoneUpdate(pos, tx, u)
		return
	}
	if !u.PowerChanged() || (u.NewPower > 0) == f.Open {
		return
	}
	f.Open = u.NewPower > 0
	tx.SetBlock(pos, f, openerSetOpts())
	if f.Open {
		tx.PlaySound(pos.Vec3Centre(), sound.FenceGateOpen{Block: f})
	} else {
		tx.PlaySound(pos.Vec3Centre(), sound.FenceGateClose{Block: f})
	}
	postToggle(tx, pos, f.Open, nil)
}

// doorOtherHalf returns the position of the other half of a door at pos.
func doorOtherHalf(pos cube.Pos, top bool) cube.Pos {
	if top {
		return pos.Side(cube.FaceDown)
	}
	return pos.Side(cube.FaceUp)
}

// playDoorSound plays the sound of a door opening or closing and posts the game event, made by nobody.
func playDoorSound(pos cube.Pos, tx *world.Tx, open bool, b world.Block) {
	if open {
		tx.PlaySound(pos.Vec3Centre(), sound.DoorOpen{Block: b})
	} else {
		tx.PlaySound(pos.Vec3Centre(), sound.DoorClose{Block: b})
	}
	postToggle(tx, pos, open, nil)
}

// playTrapdoorSound plays the sound of a trapdoor opening or closing and posts the game event, made by nobody.
func playTrapdoorSound(pos cube.Pos, tx *world.Tx, open bool, b world.Block) {
	if open {
		tx.PlaySound(pos.Vec3Centre(), sound.TrapdoorOpen{Block: b})
	} else {
		tx.PlaySound(pos.Vec3Centre(), sound.TrapdoorClose{Block: b})
	}
	postToggle(tx, pos, open, nil)
}
