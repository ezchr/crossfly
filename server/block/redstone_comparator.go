//go:build crossfly_full

package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// RedstoneComparator compares the power behind it with the power at its sides, or subtracts it, and gives the result
// in front of it after a redstone tick. Behind it, it reads how full a container is (or a cake, a cauldron...), also
// through a solid block. It follows Java Edition's comparator.
type RedstoneComparator struct {
	transparent
	flowingWaterDisplacer

	// Facing is the side the input comes from: the comparator powers the block on the other side.
	Facing cube.Direction
	// Subtract is true in subtract mode (the front torch lit), false in compare mode.
	Subtract bool
	// Powered is true while the comparator gives power.
	Powered bool
	// Output is the power the comparator gives while powered, 0 to 15 (kept in its block entity).
	Output int
}

var (
	_ world.RedstonePowerSource        = RedstoneComparator{}
	_ world.RedstoneStrongPowerSource  = RedstoneComparator{}
	_ world.RedstonePowerContextAction = RedstoneComparator{}
	_ world.RedstoneAnalogWatcher      = RedstoneComparator{}
)

// comparatorDelay is a comparator's delay: one redstone tick.
const comparatorDelay = time.Second / 10

func (c RedstoneComparator) front() cube.Face { return c.Facing.Opposite().Face() }
func (c RedstoneComparator) back() cube.Face  { return c.Facing.Face() }

// Model ...
func (RedstoneComparator) Model() world.BlockModel { return model.Diode{} }

// SideClosed ...
func (RedstoneComparator) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool { return false }

// BreakInfo ...
func (c RedstoneComparator) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(RedstoneComparator{}))
}

// EncodeItem ...
func (RedstoneComparator) EncodeItem() (name string, meta int16) { return "minecraft:comparator", 0 }

// EncodeBlock ...
func (c RedstoneComparator) EncodeBlock() (string, map[string]any) {
	name := "minecraft:unpowered_comparator"
	if c.Powered {
		name = "minecraft:powered_comparator"
	}
	return name, map[string]any{
		"minecraft:cardinal_direction": c.Facing.String(),
		"output_lit_bit":               boolByte(c.Powered),
		"output_subtract_bit":          boolByte(c.Subtract),
	}
}

// EncodeNBT ...
func (c RedstoneComparator) EncodeNBT() map[string]any {
	return map[string]any{"id": "Comparator", "OutputSignal": int32(c.Output)}
}

// DecodeNBT ...
func (c RedstoneComparator) DecodeNBT(data map[string]any) any {
	if v, ok := data["OutputSignal"].(int32); ok {
		c.Output = world.ClampRedstonePower(int(v))
	}
	return c
}

// UseOnBlock places the comparator on a full top face, pointing away from the player.
func (c RedstoneComparator) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, c)
	if !used || !diodeSupported(pos, tx) {
		return false
	}
	c = RedstoneComparator{Facing: user.Rotation().Direction().Opposite()}
	place(tx, pos, c, user, ctx)
	if !placed(ctx) {
		return false
	}
	if c.shouldTurnOn(pos, tx) {
		tx.ScheduleBlockUpdate(pos, c, time.Second/20)
	}
	return true
}

// Activate switches between compare and subtract mode.
func (c RedstoneComparator) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, _ item.User, _ *item.UseContext) bool {
	c.Subtract = !c.Subtract
	tx.PlaySound(pos.Vec3Centre(), sound.Click{})
	tx.SetBlock(pos, c, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true, KeepRedstonePower: true})
	c.refresh(pos, tx)
	return true
}

// NeighbourUpdateTick breaks the comparator off a support that no longer holds it, and checks its inputs otherwise.
func (c RedstoneComparator) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !diodeSupported(pos, tx) {
		breakBlock(c, pos, tx)
		return
	}
	c.check(pos, tx)
}

// RedstonePowerActionUpdate checks the inputs when the circuit around the comparator changed.
func (c RedstoneComparator) RedstonePowerActionUpdate(pos cube.Pos, tx *world.Tx, _ world.RedstoneUpdate) {
	c.check(pos, tx)
}

// RedstoneAnalogCheck checks the inputs once a tick, after the blocks behind it (a hopper, a furnace) had their tick:
// how full a container is changes without a block update, and Java Edition's containers update the comparators
// reading them when they change.
func (c RedstoneComparator) RedstoneAnalogCheck(pos cube.Pos, tx *world.Tx) {
	if c.readsAnalog(pos, tx) && !c.readsDetectorRail(pos, tx) {
		c.check(pos, tx)
	}
}

// readsDetectorRail reports whether the comparator reads a detector rail, which tells it when to read it again.
func (c RedstoneComparator) readsDetectorRail(pos cube.Pos, tx *world.Tx) bool {
	behindPos := pos.Side(c.back())
	behind := tx.Block(behindPos)
	if _, ok := behind.(DetectorRail); ok {
		return true
	}
	if _, ok := redstoneAnalogOutput(behindPos, behind, tx); !ok && world.RedstoneFullPowerConductor(behindPos, behind, tx) {
		_, ok := tx.Block(behindPos.Side(c.back())).(DetectorRail)
		return ok
	}
	return false
}

// check schedules the comparator to update when its output would change, unless an update is already on its way.
func (c RedstoneComparator) check(pos cube.Pos, tx *world.Tx) {
	if tx.BlockUpdateScheduled(pos, c) {
		return
	}
	if c.output(pos, tx) != c.Output || c.Powered != c.shouldTurnOn(pos, tx) {
		priority := world.TickPriorityNormal
		if diodePrioritizes(pos, c.Facing, tx) {
			priority = world.TickPriorityHigh
		}
		tx.ScheduleBlockUpdatePriority(pos, c, comparatorDelay, priority)
	}
}

// ScheduledTick updates the comparator's output.
func (c RedstoneComparator) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	c.refresh(pos, tx)
}

// refresh sets the comparator's output and powered state from its inputs, and updates what is in front of it.
func (c RedstoneComparator) refresh(pos cube.Pos, tx *world.Tx) {
	old := c.Output
	c.Output = c.output(pos, tx)
	if old == c.Output && c.Subtract {
		return // as in Java Edition: an unchanged output in subtract mode changes nothing
	}
	c.Powered = c.shouldTurnOn(pos, tx)
	c.set(pos, tx)
}

// set writes the comparator, lets the circuit follow, and updates the block in front of it and the blocks around that
// one, as Java Edition does.
func (c RedstoneComparator) set(pos cube.Pos, tx *world.Tx) {
	tx.SetBlock(pos, c, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true})
	tx.Redstone().ScheduleUpdate(pos)
	f := pos.Side(c.front())
	tx.NotifyNeighbour(f, pos)
	for _, face := range cube.Faces() {
		if face != c.back() {
			tx.NotifyNeighbour(f.Side(face), f)
		}
	}
}

// output is the power the comparator would give: in compare mode the input if no side is stronger, in subtract mode
// the input less the strongest side.
func (c RedstoneComparator) output(pos cube.Pos, tx *world.Tx) int {
	in := c.input(pos, tx)
	if in == 0 {
		return 0
	}
	side := c.side(pos, tx)
	if side > in {
		return 0
	}
	if c.Subtract {
		return in - side
	}
	return in
}

// shouldTurnOn reports whether the comparator should be powered.
func (c RedstoneComparator) shouldTurnOn(pos cube.Pos, tx *world.Tx) bool {
	in := c.input(pos, tx)
	if in == 0 {
		return false
	}
	side := c.side(pos, tx)
	return in > side || (in == side && !c.Subtract)
}

// input is the power behind the comparator: what a repeater would read there, unless the block behind gives a
// reading of how full it is, or is a solid block with such a block behind it.
func (c RedstoneComparator) input(pos cube.Pos, tx *world.Tx) int {
	power := RedstoneRepeater{Facing: c.Facing}.input(pos, tx)
	behindPos := pos.Side(c.back())
	behind := tx.Block(behindPos)
	if v, ok := redstoneAnalogOutput(behindPos, behind, tx); ok {
		return v
	}
	if power < 15 && world.RedstoneFullPowerConductor(behindPos, behind, tx) {
		farPos := behindPos.Side(c.back())
		if v, ok := redstoneAnalogOutput(farPos, tx.Block(farPos), tx); ok {
			return v
		}
	}
	return power
}

// readsAnalog reports whether the comparator reads how full a block behind it is.
func (c RedstoneComparator) readsAnalog(pos cube.Pos, tx *world.Tx) bool {
	behindPos := pos.Side(c.back())
	behind := tx.Block(behindPos)
	if _, ok := redstoneAnalogOutput(behindPos, behind, tx); ok {
		return true
	}
	if world.RedstoneFullPowerConductor(behindPos, behind, tx) {
		farPos := behindPos.Side(c.back())
		_, ok := redstoneAnalogOutput(farPos, tx.Block(farPos), tx)
		return ok
	}
	return false
}

// side is the strongest power at the comparator's sides: dust, a redstone block, or what a source gives into it
// directly (a repeater or comparator pointing into it).
func (c RedstoneComparator) side(pos cube.Pos, tx *world.Tx) int {
	power := 0
	for _, d := range []cube.Direction{c.Facing.RotateLeft(), c.Facing.RotateRight()} {
		sidePos := pos.Side(d.Face())
		switch b := tx.Block(sidePos).(type) {
		case RedstoneBlock:
			power = max(power, 15)
		case RedstoneWire:
			power = max(power, b.Power)
		case world.RedstoneStrongPowerSource:
			power = max(power, world.ClampRedstonePower(b.RedstoneStrongPower(sidePos, tx, d.Face().Opposite())))
		}
	}
	return power
}

func (c RedstoneComparator) diodePowersInto(_ cube.Pos, _ *world.Tx, face cube.Face) bool {
	return c.Powered && c.Output > 0 && face == c.front()
}

// RedstonePower gives the output through the front while the comparator is powered.
func (c RedstoneComparator) RedstonePower(_ cube.Pos, _ *world.Tx, face cube.Face) int {
	if c.Powered && face == c.front() {
		return c.Output
	}
	return 0
}

// RedstoneStrongPower: a comparator strongly powers the block in front of it.
func (c RedstoneComparator) RedstoneStrongPower(pos cube.Pos, tx *world.Tx, face cube.Face) int {
	return c.RedstonePower(pos, tx, face)
}

// allRedstoneComparators ...
func allRedstoneComparators() (comparators []world.Block) {
	for _, d := range cube.Directions() {
		for _, p := range []bool{false, true} {
			for _, s := range []bool{false, true} {
				comparators = append(comparators, RedstoneComparator{Facing: d, Powered: p, Subtract: s})
			}
		}
	}
	return
}
