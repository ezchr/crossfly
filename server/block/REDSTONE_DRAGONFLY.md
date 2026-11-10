# Redstone with `redstone=dragonfly`: adding a component

Crossfly has two redstone modes, chosen once at start (`[Redstone] Behaviour` in config.toml,
`world.JavaRedstone()` at run time):

- `java`: Java Edition's tick order, update order and rules (`*_java.go`, `server/world/redstone_java.go`).
  It must not change: the parity suite (`perfprof/check.sh`) compares it tick by tick with vanilla.
- `dragonfly`: Dragonfly's own engine (`server/world/redstone.go`): per tick, only what changed, deterministic
  order, with **Bedrock Edition's rules** where they are cleaner (no quasi-connectivity, no BUD, pistons move
  block entities, push limit 12, Bedrock slime and honey). It is not a port of Bedrock's 2-tick CircuitSystem.

Every redstone block is registered in both modes (`block.RegisterRedstone`). A block has one type and one
encoding; what it does is chosen by the mode at the top of each redstone method:

```go
func (r Thing) RedstonePowerActionUpdate(pos cube.Pos, tx *world.Tx, u world.RedstoneUpdate) {
	if world.JavaRedstone() {
		r.javaRedstoneUpdate(pos, tx, u) // the Java Edition code, unchanged
		return
	}
	// Dragonfly / Bedrock behaviour
}
```

Keep the Java path byte for byte as it was (rename it, do not edit it). Code shared with Crossfly Core goes
in untagged files; a Java-only helper it calls needs a stub in `server/block/crossfly_core.go`.
`world.JavaRedstoneExtension` (a block that is a component only in Java mode) is for blocks that do nothing at
all in Dragonfly mode yet; remove it from a block once it has Dragonfly behaviour.

## How the engine works

A block change (or `tx.Redstone().ScheduleUpdate`) marks the position and its six neighbours dirty. Once a
tick, after scheduled ticks and neighbour updates, the engine compiles the redstone blocks around the dirty
positions (through conductors and along dust) into a graph, works out the power every node takes in, and calls:

| Interface (package world) | Called with | Use it for |
|---|---|---|
| `RedstonePowerSource.RedstonePower(pos, tx, face)` | while evaluating | the power the block gives through `face` (the face of the block itself) |
| `RedstoneStrongPowerSource.RedstoneStrongPower` | while evaluating | strong power: passes into a conductor and out of its other sides |
| `RedstoneWeakBlockPowerer` | while evaluating | weak power that powers a conductor for mechanisms but not for dust (dust) |
| `RedstonePowerConsumer.RedstonePowerUpdate(pos, tx, power)` | every evaluation | return a new state that follows the power (the engine writes it) |
| `RedstonePowerPostUpdater` | after the engine wrote a consumer's new state | side effects: the other half of a door, sounds |
| `RedstonePowerAction.RedstonePowerAction(pos, tx, old, new)` | when the power changed | edge actions (TNT priming) |
| `RedstonePowerContextAction.RedstonePowerActionUpdate(pos, tx, u)` | every evaluation, with the full `RedstoneUpdate` | everything else: diodes, doors, torches |
| `RedstoneAnalogWatcher.RedstoneAnalogCheck(pos, tx)` | once a tick, after block entities ticked, for watchers that are block entities; and on `tx.Redstone().AnalogChanged` | comparators reading containers |
| `RedstoneAnalogOutputter.RedstoneAnalogOutput(pos, tx)` | by the comparator | the reading of a block (0-15) not in the block package's list |
| `RedstoneNonConductive` | | a full solid block that does not conduct (TNT) |
| `RedstoneComponentSwitch.RedstoneComponent()` | when the engine first sees the runtime ID | a block that is a component in some states or modes only (with Dragonfly redstone only trapped chests are) |

`RedstoneUpdate` tells why: `OldPower`/`NewPower` (the engine remembers the last power it saw at the position),
`PowerChanged()`, `Rising()`, `Falling()`, and `SelfCaused()` (the update comes from the component's own output
change, such as a torch whose output runs back into its input).

Reading power from a block (all in `world.Tx`): `RedstonePower(pos)` (what a mechanism at pos takes in from
every side), `RedstonePowerFrom(pos, face)`, `RedstoneDirectPower[From]` (not through conductors),
`RedstoneStrongPower[From]`, `RedstoneConductivePower(pos)` (the power a conductor holds),
`world.RedstoneFullPowerConductor(pos, b, src)`.

## The component kinds

**Consumers** (lamp, door, trapdoor, fence gate, hopper lock, note block, TNT). Bedrock Edition's consumers react
only when their power *changes* (`CircuitSystem::updateBlocks` calls `onRedstoneUpdate` for components whose
strength changed). Either return the new state from `RedstonePowerUpdate` if the state is a pure function of
power (hopper `toggle_bit`, iron door), or use `RedstonePowerActionUpdate` and return early unless
`u.PowerChanged()` when a player can also change the state (a wooden door opened by hand stays open until its
power changes). When a player or a shape change sets such a block, set it with
`&world.SetOpts{KeepRedstonePower: true}` in Dragonfly mode, or the engine forgets the power and sees a change
on the next evaluation (`openerSetOpts` in `redstone_openers.go`).

**Delayed outputs** (repeater, comparator, observer). Take the input change in `RedstonePowerActionUpdate`
(or `NeighbourUpdateTick`), schedule the change with `tx.Redstone().Delay(pos, b, redstoneTicks)` (it does
nothing if a tick is already waiting) or `tx.ScheduleBlockUpdate` with `world.RedstoneTicks(n)`, and in
`ScheduledTick` read the live block and inputs again, then write the new state with
`tx.Redstone().SetOutput(pos, newState)`. `SetOutput` keeps the power the block takes in and has the engine
evaluate around it in its next phase, so the change propagates within the same tick.

**Component state**. What a component remembers between ticks that its block state does not hold (the delay line of
a repeater, the last readings of a comparator): `tx.Redstone().State(pos)`, `SetState(pos, v uint64)`,
`ClearState(pos)`. The engine drops it when the block at pos is set without `KeepRedstonePower` (`SetOutput` keeps
it) and when the chunk unloads. It is not saved: rebuild it from the block when it is missing.

**Pulse sources** (buttons, pressure plates, tripwire hooks, target, lectern page turns). Turn on with
`SetOutput`, schedule the end with `Delay`/`ScheduleBlockUpdate`, turn off with `SetOutput` in `ScheduledTick`
(after checking that nothing still holds it on, such as an entity on a plate). Give strong power to the block
the source is attached to with `RedstoneStrongPower` on that face.

**Analog inputs** (comparator). The engine evaluates a comparator when blocks near it change, which covers block
states (cake, composter, cauldron, end portal frame). Item counts change without a block write: a comparator
that is a block entity and implements `RedstoneAnalogWatcher` checks once a tick after the block entities ticked
(in a fixed position order), and a block may call `tx.Redstone().AnalogChanged(pos)` to wake the comparators
reading it at once (next to it, or behind one full conductor). Both do nothing in Java mode, which has its own.

**Torches**. Bedrock Edition's `RedstoneTorchCapacitor`: a torch counts the switches in a row caused by its own
output (`SelfCaused`); at the sixteenth it goes out (`fizz`) and stays out, with no timer, until its own output
no longer runs back into its input (the dust loop is broken, or something else powers the block it hangs on).

**Dust**. Bedrock Edition's `TransporterComponent` powers the block below it, never the block above, and a
horizontal neighbour if the dust runs towards it or is a dot (no connections). It connects (is a power flow
direction) to dust (also one block up or down), power sources, repeaters along their axis, observers at their
output, comparators on every side and pistons, but not to other consumers: dust running past a lamp or door does
not power it. The Java client draws the dust shape from these connections.

## Checklist for a new component

1. One block type and encoding for both modes; registered in `RegisterRedstone`.
2. Each redstone method starts with the mode switch; the Java path unchanged.
3. Dragonfly behaviour from the Bedrock rules (BDS 1.21.3 dump: `*Block.cpp`, `*Capacitor.cpp`,
   `ConsumerComponent.cpp`); write the rule, not a translation.
4. Tests in Dragonfly mode: in Crossfly Full tests call `dragonflyRedstone(t)` (it switches the mode for the
   test and back), then build the circuit in a synchronous world and `AdvanceTick`. Run `go test` with and
   without `-tags crossfly_full`, and `perfprof/check.sh` for Java mode (92 same, 0 diff).
5. Both clients: the Java client gets block states through `server/java/javasession`, which does not depend on
   the mode.
