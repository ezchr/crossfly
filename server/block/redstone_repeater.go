//go:build crossfly_full

package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// RedstoneRepeater repeats the power behind it at full strength in front of it, after a delay of one to four redstone
// ticks. A powered repeater or comparator pointing into its side locks it in its state. It follows Java Edition's
// repeater: the input is only what is behind it, a pulse shorter than the delay is lengthened to the delay, and a
// change of input while a change is already on its way is ignored.
type RedstoneRepeater struct {
	transparent
	flowingWaterDisplacer

	// Facing is the side the input comes from: the repeater powers the block on the other side.
	Facing cube.Direction
	// Powered is true while the repeater gives power.
	Powered bool
	// Delay is the delay in redstone ticks, less one: 0 to 3.
	Delay int
}

var (
	_ world.RedstonePowerSource        = RedstoneRepeater{}
	_ world.RedstoneStrongPowerSource  = RedstoneRepeater{}
	_ world.RedstonePowerContextAction = RedstoneRepeater{}
)

// front is the face the repeater gives power through.
func (r RedstoneRepeater) front() cube.Face { return r.Facing.Opposite().Face() }

// back is the face the repeater takes power through.
func (r RedstoneRepeater) back() cube.Face { return r.Facing.Face() }

// delay is the repeater's delay as a duration.
func (r RedstoneRepeater) delay() time.Duration { return redstoneTicks(r.Delay + 1) }

// Model ...
func (RedstoneRepeater) Model() world.BlockModel { return model.Diode{} }

// SideClosed ...
func (RedstoneRepeater) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool { return false }

// BreakInfo ...
func (r RedstoneRepeater) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(RedstoneRepeater{}))
}

// EncodeItem ...
func (RedstoneRepeater) EncodeItem() (name string, meta int16) { return "minecraft:repeater", 0 }

// EncodeBlock ...
func (r RedstoneRepeater) EncodeBlock() (string, map[string]any) {
	name := "minecraft:unpowered_repeater"
	if r.Powered {
		name = "minecraft:powered_repeater"
	}
	return name, map[string]any{"minecraft:cardinal_direction": r.Facing.String(), "repeater_delay": int32(r.Delay)}
}

// UseOnBlock places the repeater on a full top face, pointing away from the player.
func (r RedstoneRepeater) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, r)
	if !used || !diodeSupported(pos, tx) {
		return false
	}
	r.Facing = user.Rotation().Direction().Opposite()
	r.Powered, r.Delay = false, 0
	place(tx, pos, r, user, ctx)
	if !placed(ctx) {
		return false
	}
	if world.JavaRedstone() && r.input(pos, tx) > 0 {
		// Java Edition: a repeater placed with power behind it turns on after a game tick.
		tx.ScheduleBlockUpdate(pos, r, time.Second/20)
	}
	return true
}

// Activate cycles the delay through one to four redstone ticks.
func (r RedstoneRepeater) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, _ item.User, _ *item.UseContext) bool {
	r.Delay = (r.Delay + 1) % 4
	tx.SetBlock(pos, r, &world.SetOpts{DisableRedstoneUpdates: true, KeepRedstonePower: true})
	if !world.JavaRedstone() {
		// Bedrock keeps what is on its way and only changes the delay.
		r.dfTick(pos, tx)
	}
	return true
}

// NeighbourUpdateTick breaks the repeater off a support that no longer holds it, and checks its input otherwise.
func (r RedstoneRepeater) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !diodeSupported(pos, tx) {
		breakBlock(r, pos, tx)
		return
	}
	if !world.JavaRedstone() {
		return // the redstone engine updates its input
	}
	r.checkInput(pos, tx)
}

// RedstonePowerActionUpdate checks the input when the circuit around the repeater changed.
func (r RedstoneRepeater) RedstonePowerActionUpdate(pos cube.Pos, tx *world.Tx, _ world.RedstoneUpdate) {
	if !world.JavaRedstone() {
		r.dfUpdate(pos, tx)
		return
	}
	r.checkInput(pos, tx)
}

// checkInput schedules the repeater to follow its input, unless it is locked or already about to change.
func (r RedstoneRepeater) checkInput(pos cube.Pos, tx *world.Tx) {
	if r.locked(pos, tx) || r.Powered == (r.input(pos, tx) > 0) || tx.BlockUpdateScheduled(pos, r) {
		return
	}
	priority := world.TickPriorityHigh
	if diodePrioritizes(pos, r.Facing, tx) {
		priority = world.TickPriorityExtremelyHigh
	} else if r.Powered {
		priority = world.TickPriorityVeryHigh
	}
	tx.ScheduleBlockUpdatePriority(pos, r, r.delay(), priority)
}

// ScheduledTick turns the repeater on or off. Turned on by a pulse that is already over, it stays on for its delay.
func (r RedstoneRepeater) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if !world.JavaRedstone() {
		r.dfTick(pos, tx)
		return
	}
	if r.locked(pos, tx) {
		return
	}
	on := r.input(pos, tx) > 0
	switch {
	case r.Powered && !on:
		r.Powered = false
		r.set(pos, tx)
	case !r.Powered:
		r.Powered = true
		r.set(pos, tx)
		if !on {
			// Scheduled ticks run for the state they were scheduled for: the powered one now.
			tx.ScheduleBlockUpdatePriority(pos, r, r.delay(), world.TickPriorityVeryHigh)
		}
	}
}

// set writes the repeater's new state and updates what is in front of it, as Java Edition does: the block in front
// and the blocks around that one.
func (r RedstoneRepeater) set(pos cube.Pos, tx *world.Tx) {
	tx.SetBlock(pos, r, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true})
	tx.Redstone().ScheduleUpdate(pos)
	front := pos.Side(r.front())
	tx.NotifyNeighbour(front, pos)
	for _, f := range cube.Faces() {
		if f != r.back() {
			tx.NotifyNeighbour(front.Side(f), front)
		}
	}
}

// input is the power behind the repeater, as Java Edition reads it: dust behind it gives its power whichever way it
// points, any other source the power it gives towards the repeater, and a solid block the power conducted into it.
func (r RedstoneRepeater) input(pos cube.Pos, tx *world.Tx) int {
	behindPos := pos.Side(r.back())
	behind := tx.Block(behindPos)
	if wire, ok := behind.(RedstoneWire); ok {
		return wire.Power
	}
	power := 0
	if source, ok := behind.(world.RedstonePowerSource); ok {
		power = world.ClampRedstonePower(source.RedstonePower(behindPos, tx, r.back().Opposite()))
	}
	if world.RedstoneFullPowerConductor(behindPos, behind, tx) {
		power = max(power, tx.RedstoneConductivePower(behindPos))
	}
	return power
}

// locked reports whether a powered repeater (or comparator) points into either side of the repeater.
func (r RedstoneRepeater) locked(pos cube.Pos, tx *world.Tx) bool {
	for _, side := range []cube.Direction{r.Facing.RotateLeft(), r.Facing.RotateRight()} {
		sidePos := pos.Side(side.Face())
		if d, ok := tx.Block(sidePos).(redstoneDiode); ok && d.diodePowersInto(sidePos, tx, side.Face().Opposite()) {
			return true
		}
	}
	return false
}

var _ redstoneDiode = RedstoneComparator{}

// redstoneDiode is a repeater or comparator: the blocks that lock a repeater from its side.
type redstoneDiode interface {
	// diodePowersInto reports whether the diode at pos gives power through face.
	diodePowersInto(pos cube.Pos, tx *world.Tx, face cube.Face) bool
}

func (r RedstoneRepeater) diodePowersInto(_ cube.Pos, _ *world.Tx, face cube.Face) bool {
	return r.Powered && face == r.front()
}

// RedstonePower gives 15 through the front while the repeater is on.
func (r RedstoneRepeater) RedstonePower(_ cube.Pos, _ *world.Tx, face cube.Face) int {
	if r.Powered && face == r.front() {
		return 15
	}
	return 0
}

// RedstoneStrongPower: a repeater strongly powers the block in front of it.
func (r RedstoneRepeater) RedstoneStrongPower(pos cube.Pos, tx *world.Tx, face cube.Face) int {
	return r.RedstonePower(pos, tx, face)
}

// diodeSupported reports whether a repeater or comparator at pos stands on a full top face.
func diodeSupported(pos cube.Pos, tx *world.Tx) bool {
	below := pos.Side(cube.FaceDown)
	return tx.Block(below).Model().FaceSolid(below, cube.FaceUp, tx)
}

// allRedstoneRepeaters ...
func allRedstoneRepeaters() (repeaters []world.Block) {
	for _, d := range cube.Directions() {
		for _, p := range []bool{false, true} {
			for delay := range 4 {
				repeaters = append(repeaters, RedstoneRepeater{Facing: d, Powered: p, Delay: delay})
			}
		}
	}
	return
}

// Java prioritises diodes feeding another diode unless that diode faces back into this one.
func diodePrioritizes(pos cube.Pos, facing cube.Direction, tx *world.Tx) bool {
	front := facing.Opposite()
	switch b := tx.Block(pos.Side(front.Face())).(type) {
	case RedstoneRepeater:
		return b.Facing != front
	case RedstoneComparator:
		return b.Facing != front
	}
	return false
}
