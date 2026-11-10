//go:build crossfly_full

package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/gameevent"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// TripwireHook is hung on the side of a block. Two hooks facing each other with tripwire between them form a
// tripwire circuit: the hooks give power while an entity is in the wire. It follows Java Edition's tripwire hook.
type TripwireHook struct {
	empty
	transparent

	// Facing is the direction the hook faces, away from the block it hangs on.
	Facing cube.Direction
	// Attached is true while the hook is part of a complete circuit.
	Attached bool
	// Powered is true while the circuit is tripped.
	Powered bool
}

var (
	_ world.RedstonePowerSource       = TripwireHook{}
	_ world.RedstoneStrongPowerSource = TripwireHook{}
)

const (
	// tripwireMaxLength is how far a hook looks for the hook facing it, as in Java Edition.
	tripwireMaxLength = 42
	// tripwireRecheck is how often a tripped wire and its hooks look again: 10 game ticks.
	tripwireRecheck = time.Second / 2
)

// UseOnBlock hangs the hook on the side of a block with a sturdy face.
func (h TripwireHook) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, face, used := firstReplaceable(tx, pos, face, h)
	if !used {
		return false
	}
	h = TripwireHook{}
	switch {
	case face.Axis() != cube.Y && tripwireHookSupported(pos, face.Direction(), tx):
		h.Facing = face.Direction()
	case tripwireHookSupported(pos, user.Rotation().Direction().Opposite(), tx):
		h.Facing = user.Rotation().Direction().Opposite()
	default:
		return false
	}
	place(tx, pos, h, user, ctx)
	if !placed(ctx) {
		return false
	}
	h.calculateState(pos, tx, false, false, -1, nil)
	return true
}

// tripwireHookSupported reports whether a hook facing f at pos would have a sturdy face to hang on.
func tripwireHookSupported(pos cube.Pos, f cube.Direction, tx *world.Tx) bool {
	support := pos.Side(f.Opposite().Face())
	return tx.Block(support).Model().FaceSolid(support, f.Face(), tx)
}

// NeighbourUpdateTick breaks the hook off when the block it hangs on is gone.
func (h TripwireHook) NeighbourUpdateTick(pos, changed cube.Pos, tx *world.Tx) {
	if changed == pos.Side(h.Facing.Opposite().Face()) && !tripwireHookSupported(pos, h.Facing, tx) {
		breakBlock(h, pos, tx)
	}
}

// ScheduledTick looks at the circuit again, as a tripped hook does every 10 ticks.
func (h TripwireHook) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	h.calculateState(pos, tx, false, true, -1, nil)
}

// BlockRemoved detaches the hook facing this one, and turns the circuit off.
func (h TripwireHook) BlockRemoved(pos cube.Pos, replacement world.Block, tx *world.Tx) {
	if _, same := replacement.(TripwireHook); same {
		return
	}
	h.removed(pos, tx)
}

func (h TripwireHook) removed(pos cube.Pos, tx *world.Tx) {
	if h.Attached || h.Powered {
		h.calculateState(pos, tx, true, false, -1, nil)
	}
}

// calculateState is Java Edition's TripWireHookBlock.calculateState: it walks the wire in front of the hook to the
// hook facing it, and sets both hooks and the wire between them. wireSource is the distance of a wire that changed
// (-1 for none), with its new state in wireSourceState.
func (h TripwireHook) calculateState(pos cube.Pos, tx *world.Tx, destroyed, _ bool, wireSource int, wireSourceState *String) {
	dir := h.Facing
	wasAttached, wasPowered := h.Attached, h.Powered
	attached, powered, receiver, recheck := !destroyed, false, 0, false
	var wires [tripwireMaxLength]*String

	for i := 1; i < tripwireMaxLength; i++ {
		testPos := tripwireStep(pos, dir, i)
		b := tx.Block(testPos)
		if hook, ok := b.(TripwireHook); ok {
			if hook.Facing == dir.Opposite() {
				receiver = i
			}
			break
		}
		wire, isWire := b.(String)
		if !isWire && i != wireSource {
			attached = false
			continue
		}
		if i == wireSource && wireSourceState != nil {
			wire = *wireSourceState
		}
		armed := !wire.Disarmed
		powered = powered || (armed && wire.Powered)
		w := wire
		wires[i] = &w
		if i == wireSource {
			recheck = true
			attached = attached && armed
		}
	}
	attached = attached && receiver > 1
	powered = powered && attached

	state := TripwireHook{Attached: attached, Powered: powered}
	if receiver > 0 {
		testPos := tripwireStep(pos, dir, receiver)
		other := state
		other.Facing = dir.Opposite()
		tripwireSet(testPos, other, tx)
		if _, ok := tx.Block(pos).(TripwireHook); !ok {
			state.Facing = dir
			state.removed(pos, tx)
			return
		}
		tripwireHookSound(testPos, tx, attached, powered, wasAttached, wasPowered)
	}
	tripwireHookSound(pos, tx, attached, powered, wasAttached, wasPowered)
	if !destroyed {
		state.Facing = dir
		tripwireSet(pos, state, tx)
		if recheck {
			tripwireHookSchedule(pos, dir, tx)
		}
	}
	if wasAttached != attached {
		for i := 1; i < receiver; i++ {
			if wires[i] == nil {
				continue
			}
			testPos := tripwireStep(pos, dir, i)
			switch tx.Block(testPos).(type) {
			case String:
				w := *wires[i]
				w.Attached = attached
				tripwireSet(testPos, w, tx)
			}
		}
	}
}

// tripwireHookSchedule schedules the hook's recheck. Java Edition keeps one scheduled tick per block type and
// position, whatever its state, and ignores a second one; scheduled ticks here are kept per state, so every state the
// hook can take before the tick is scheduled, and only the one it has then runs.
func tripwireHookSchedule(pos cube.Pos, facing cube.Direction, tx *world.Tx) {
	var states []TripwireHook
	for _, a := range []bool{false, true} {
		for _, p := range []bool{false, true} {
			h := TripwireHook{Facing: facing, Attached: a, Powered: p}
			if tx.BlockUpdateScheduled(pos, h) {
				return
			}
			states = append(states, h)
		}
	}
	for _, h := range states {
		tx.ScheduleBlockUpdate(pos, h, tripwireRecheck)
	}
}

// tripwireStep is the position i blocks from pos towards d.
func tripwireStep(pos cube.Pos, d cube.Direction, i int) cube.Pos {
	for ; i > 0; i-- {
		pos = pos.Side(d.Face())
	}
	return pos
}

// tripwireSet sets b at pos with block updates, unless it is already there (Java Edition's setBlock changes nothing
// then).
func tripwireSet(pos cube.Pos, b world.Block, tx *world.Tx) {
	if tx.Block(pos) == b {
		return
	}
	tx.SetBlock(pos, b, nil)
}

// tripwireHookSound plays the click of a hook turning on or off.
func tripwireHookSound(pos cube.Pos, tx *world.Tx, attached, powered, wasAttached, wasPowered bool) {
	switch {
	case powered && !wasPowered:
		tx.PlaySound(pos.Vec3Centre(), sound.PowerOn{})
	case !powered && wasPowered:
		tx.PlaySound(pos.Vec3Centre(), sound.PowerOff{})
	}
	// TripWireHookBlock::_emitState: block_activate or block_deactivate when it switches on or off, else
	// block_attach or block_detach when the wire is hooked up or cut.
	switch {
	case powered != wasPowered:
		postActivate(tx, pos, powered, nil)
	case attached && !wasAttached:
		postBlockEvent(tx, gameevent.BlockAttach, pos, nil)
	case !attached && wasAttached:
		postBlockEvent(tx, gameevent.BlockDetach, pos, nil)
	}
}

// RedstonePower gives power to every side while the hook is tripped.
func (h TripwireHook) RedstonePower(cube.Pos, *world.Tx, cube.Face) int {
	if h.Powered {
		return 15
	}
	return 0
}

// RedstoneStrongPower strongly powers the block the hook hangs on.
func (h TripwireHook) RedstoneStrongPower(_ cube.Pos, _ *world.Tx, face cube.Face) int {
	if h.Powered && face == h.Facing.Opposite().Face() {
		return 15
	}
	return 0
}

// SideClosed ...
func (TripwireHook) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool { return false }

// BreakInfo ...
func (h TripwireHook) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(TripwireHook{}))
}

// EncodeItem ...
func (TripwireHook) EncodeItem() (name string, meta int16) { return "minecraft:tripwire_hook", 0 }

// EncodeBlock ...
func (h TripwireHook) EncodeBlock() (string, map[string]any) {
	return "minecraft:tripwire_hook", map[string]any{
		"attached_bit": boolByte(h.Attached),
		"powered_bit":  boolByte(h.Powered),
		"direction":    int32(horizontalDirection(h.Facing)),
	}
}

// allTripwireHooks ...
func allTripwireHooks() (hooks []world.Block) {
	for _, d := range cube.Directions() {
		for _, a := range []bool{false, true} {
			for _, p := range []bool{false, true} {
				hooks = append(hooks, TripwireHook{Facing: d, Attached: a, Powered: p})
			}
		}
	}
	return
}
