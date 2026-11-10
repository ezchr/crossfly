package world

import (
	"slices"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// Redstone returns a transaction-scoped handle for redstone engine operations.
func (tx *Tx) Redstone() RedstoneTransaction {
	return RedstoneTransaction{tx: tx}
}

// RedstoneTransaction provides access to redstone engine operations within a transaction.
type RedstoneTransaction struct {
	tx *Tx
}

// ScheduleUpdate marks pos and its neighbours for re-evaluation during the next redstone phase.
func (r RedstoneTransaction) ScheduleUpdate(pos cube.Pos) {
	r.tx.World().redstone.invalidateAround(pos, pos, RedstoneUpdateCauseScheduledTick, r.tx.Range())
}

// SetOutput writes b, the new state of the redstone component at pos, after the component changed what it gives
// (a repeater turning on, a button released), and has the engine evaluate the circuit around it in its next phase.
// Neighbours still get their block updates and shape updates (an observer sees the change), but the write does not
// make the engine forget the power the component takes in: its own output change is no change of its input.
// Components of redstone=dragonfly use it from ScheduledTick for delayed outputs and pulses.
func (r RedstoneTransaction) SetOutput(pos cube.Pos, b Block) {
	r.tx.SetBlock(pos, b, &SetOpts{DisableRedstoneUpdates: true, KeepRedstonePower: true})
	r.ScheduleUpdate(pos)
}

// Delay schedules the ScheduledTick of b at pos after delay redstone ticks (two game ticks each), unless one is
// already waiting. It reports whether it scheduled one. A component that changes its output after a delay (a
// repeater, a comparator, an observer, the end of a button press) schedules it so, and sets its new state with
// SetOutput when the tick runs: the tick reads the world as it is then, so a change of input during the delay is
// seen there.
func (r RedstoneTransaction) Delay(pos cube.Pos, b Block, delay int) bool {
	if r.tx.World().blockUpdateScheduled(pos, b) {
		return false
	}
	r.tx.ScheduleBlockUpdate(pos, b, RedstoneTicks(delay))
	return true
}

// AnalogChanged tells the comparators that read the block at pos that what they read changed without a block being
// set, such as the items in a container. The RedstoneAnalogWatchers next to pos, and those behind a full conductor
// next to it, check their inputs now. With Java redstone it does nothing: Java Edition's comparators look once a
// tick.
func (r RedstoneTransaction) AnalogChanged(pos cube.Pos) {
	if javaRedstone {
		return
	}
	tx := r.tx
	for _, face := range cube.HorizontalFaces() {
		near := pos.Side(face)
		b, ok := tx.World().blockLoaded(near)
		if !ok {
			continue
		}
		if aw, ok := b.(RedstoneAnalogWatcher); ok {
			aw.RedstoneAnalogCheck(near, tx)
			continue
		}
		if !RedstoneFullPowerConductor(near, b, tx) {
			continue
		}
		far := near.Side(face)
		if b, ok := tx.World().blockLoaded(far); ok {
			if aw, ok := b.(RedstoneAnalogWatcher); ok {
				aw.RedstoneAnalogCheck(far, tx)
			}
		}
	}
}

// blockUpdateScheduled reports whether a tick of b waits at pos. With Java redstone a tick of the same kind of block
// counts (Tx.BlockUpdateScheduled); otherwise one of the same block state, as ScheduleBlockUpdate keeps them.
func (w *World) blockUpdateScheduled(pos cube.Pos, b Block) bool {
	queue := w.scheduledUpdates
	if javaRedstone {
		return queue.pending[scheduledTickType{pos: pos, kind: scheduledTickKind(b)}] > 0
	}
	t, ok := queue.furthestTicks[scheduledTickIndex{pos: pos, hash: w.conf.Blocks.BlockHash(b)}]
	return ok && t > queue.currentTick
}

// RedstoneTicks returns n redstone ticks as a duration: a redstone tick is two game ticks (a tenth of a second).
func RedstoneTicks(n int) time.Duration {
	return time.Duration(max(n, 1)) * time.Second / 10
}

// sortRedstonePositions sorts positions in the engine's deterministic order.
func sortRedstonePositions(p []cube.Pos) { slices.SortFunc(p, compareBlockPos) }

// Torch returns a transaction-scoped handle for transient redstone torch state at pos.
func (r RedstoneTransaction) Torch(pos cube.Pos) RedstoneTorchTransaction {
	return RedstoneTorchTransaction{tx: r.tx, pos: pos}
}

// RedstoneTorchTransaction provides access to transient redstone torch state within a transaction.
type RedstoneTorchTransaction struct {
	tx  *Tx
	pos cube.Pos
}

// BurnoutStatus reports whether the redstone torch is burned out. recoverable is false while it is: a burned-out
// torch relights only once the loop through its own output is gone (Bedrock Edition's torch has no timer).
func (t RedstoneTorchTransaction) BurnoutStatus() (burnedOut, recoverable bool) {
	return t.tx.World().redstone.torchBurnoutStatus(t.pos)
}

// RecordToggle records that the redstone torch switched on or off, because of its own output (self) or not, and
// reports whether it burns out: Bedrock Edition's torch burns out at the sixteenth switch in a row caused by its own
// output. A switch with another cause starts the count again.
func (t RedstoneTorchTransaction) RecordToggle(self bool) (burnsOut bool) {
	return t.tx.World().redstone.recordTorchToggle(t.pos, self)
}

// MarkSelfTriggered records that the next switch of the torch is caused by its own output.
func (t RedstoneTorchTransaction) MarkSelfTriggered() {
	t.tx.World().redstone.markTorchSelfTriggered(t.pos)
}

// ConsumeSelfTriggered reports and clears whether the next switch of the torch is caused by its own output.
func (t RedstoneTorchTransaction) ConsumeSelfTriggered() bool {
	return t.tx.World().redstone.consumeTorchSelfTriggered(t.pos)
}

// ClearBurnout removes transient burnout state for the redstone torch.
func (t RedstoneTorchTransaction) ClearBurnout() {
	t.tx.World().redstone.clearTorchBurnout(t.pos)
}

// RedstonePower returns the strongest redstone power currently applied to the position passed. Custom redstone block
// implementations may use this method to query the transaction's current redstone state.
func (tx *Tx) RedstonePower(pos cube.Pos) int {
	return tx.World().redstone.powerTo(pos, tx)
}

// RedstoneDirectPower returns the strongest direct redstone power currently applied to the position passed, excluding
// power conducted through solid blocks. Custom redstone block implementations may use this method to query the
// transaction's current redstone state.
func (tx *Tx) RedstoneDirectPower(pos cube.Pos) int {
	return tx.World().redstone.directPower(pos, tx)
}

// RedstoneStrongPower returns the strongest strong redstone power currently applied to the position passed. Custom
// redstone block implementations may use this method to query the transaction's current redstone state.
func (tx *Tx) RedstoneStrongPower(pos cube.Pos) int {
	return tx.World().redstone.strongPower(pos, tx)
}

// RedstoneConductivePower returns the power held by pos as a conductive block, excluding direct component activation.
// Custom redstone block implementations may use this method to query the transaction's current redstone state.
func (tx *Tx) RedstoneConductivePower(pos cube.Pos) int {
	return tx.World().redstone.conductivePowerTo(pos, tx)
}

// RedstonePowerFrom returns the strongest redstone power reaching pos from the side passed. Custom redstone block
// implementations may use this method to query the transaction's current redstone state.
func (tx *Tx) RedstonePowerFrom(pos cube.Pos, face cube.Face) int {
	return tx.World().redstone.powerFrom(pos, tx, face)
}

// RedstoneDirectPowerFrom returns the strongest direct redstone power reaching pos from the side passed. Custom
// redstone block implementations may use this method to query the transaction's current redstone state.
func (tx *Tx) RedstoneDirectPowerFrom(pos cube.Pos, face cube.Face) int {
	return tx.World().redstone.directPowerFrom(pos, tx, face)
}

// RedstoneStrongPowerFrom returns the strongest strong redstone power reaching pos from the side passed. Custom
// redstone block implementations may use this method to query the transaction's current redstone state.
func (tx *Tx) RedstoneStrongPowerFrom(pos cube.Pos, face cube.Face) int {
	return tx.World().redstone.strongPowerFrom(pos, tx, face)
}
