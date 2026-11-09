//go:build crossfly_full

package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Observer detects block-state changes at its sensing face and emits a two-game-tick pulse on the opposite face.
type Observer struct {
	solid
	sourceWaterDisplacer
	// Facing is the sensing face. Output is through Facing.Opposite().
	Facing  cube.Face
	Powered bool
}

var (
	_ world.BlockStateChangeListener  = Observer{}
	_ world.RedstonePowerSource       = Observer{}
	_ world.RedstoneStrongPowerSource = Observer{}
)

func (o Observer) EncodeBlock() (string, map[string]any) {
	return "minecraft:observer", map[string]any{"minecraft:facing_direction": o.Facing.String(), "powered_bit": boolByte(o.Powered)}
}
func (Observer) EncodeItem() (string, int16) { return "minecraft:observer", 0 }
func (Observer) BreakInfo() BreakInfo {
	return newBreakInfo(3, pickaxeHarvestable, pickaxeEffective, oneOf(Observer{}))
}
func (o Observer) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, o)
	if !used {
		return false
	}
	// Observers sense away from the placing player, unlike dispensers.
	o.Facing, o.Powered = calculateFace(user, pos).Opposite(), false
	place(tx, pos, o, user, ctx)
	return placed(ctx)
}

// BlockStateChanged reacts only to shape/state updates at the sensing face. Ordinary neighbour notifications do
// not trigger a Java observer. Changes arriving while the pulse is pending or active are ignored.
func (o Observer) BlockStateChanged(pos, changed cube.Pos, tx *world.Tx) {
	if changed == pos.Side(o.Facing) && !o.Powered {
		o.schedule(pos, tx)
	}
}
func (o Observer) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	o.Powered = !o.Powered
	tx.SetBlock(pos, o, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true})
	if o.Powered {
		o.schedule(pos, tx)
	}
	tx.Redstone().ScheduleUpdate(pos)
	output := pos.Side(o.Facing.Opposite())
	tx.NotifyNeighbour(output, pos)
	for _, f := range cube.Faces() {
		if f != o.Facing {
			tx.NotifyNeighbour(output.Side(f), output)
		}
	}
}

// schedule is Java Edition's scheduleTick for observers: one tick per position whatever the state, so nothing is
// scheduled while a tick is on its way, and the tick is scheduled for both states.
func (o Observer) schedule(pos cube.Pos, tx *world.Tx) {
	if observerScheduled(pos, o, tx) {
		return
	}
	for _, p := range []bool{false, true} {
		v := o
		v.Powered = p
		tx.ScheduleBlockUpdate(pos, v, time.Second/10)
	}
}

// observerScheduled reports whether a tick of the observer at pos is on its way, in either state.
func observerScheduled(pos cube.Pos, o Observer, tx *world.Tx) bool {
	for _, p := range []bool{false, true} {
		v := o
		v.Powered = p
		if tx.BlockUpdateScheduled(pos, v) {
			return true
		}
	}
	return false
}

func (o Observer) RedstonePower(_ cube.Pos, _ *world.Tx, face cube.Face) int {
	if o.Powered && face == o.Facing.Opposite() {
		return 15
	}
	return 0
}
func (o Observer) RedstoneStrongPower(pos cube.Pos, tx *world.Tx, face cube.Face) int {
	return o.RedstonePower(pos, tx, face)
}
func allObservers() (blocks []world.Block) {
	for _, face := range cube.Faces() {
		blocks = append(blocks, Observer{Facing: face}, Observer{Facing: face, Powered: true})
	}
	return
}

// BlockRemoved is Java Edition's affectNeighborsAfterRemoval: a powered observer that is replaced by another block (a
// moving block when a piston pushes it) while its turn-off tick is pending updates the block behind it and that
// block's neighbours at once, as if it turned off. A piston cancels the ticks of what it moves first, which Java
// does not, so a moving block counts as pending.
func (o Observer) BlockRemoved(pos cube.Pos, replacement world.Block, tx *world.Tx) {
	if _, same := replacement.(Observer); same || !o.Powered {
		return
	}
	if _, moved := replacement.(MovingPiston); !moved && !observerScheduled(pos, o, tx) {
		return
	}
	tx.RunNeighbourUpdatesNow(func() {
		tx.Redstone().ScheduleUpdate(pos)
		output := pos.Side(o.Facing.Opposite())
		tx.NotifyNeighbour(output, pos)
		for _, f := range cube.Faces() {
			if f != o.Facing {
				tx.NotifyNeighbour(output.Side(f), output)
			}
		}
	})
}

// BlockPlaced clears an imported powered state with no scheduled tick.
func (o Observer) BlockPlaced(pos cube.Pos, previous world.Block, tx *world.Tx) {

	if _, sameType := previous.(Observer); sameType {
		return
	}
	if observerScheduled(pos, o, tx) {
		return
	}
	if o.Powered {
		o.ScheduledTick(pos, tx, nil)
	}
	// A player placing an observer makes no pulse, as in Java Edition: BlockItem places it without a shape update of
	// its own. (/setblock does update its shape, and so pulses: see ObserverSetblockPulse.)
}

// ObserverSetblockPulse starts the pulse Java Edition's /setblock gives an observer it places: the command updates
// the placed block's shape from its neighbours, which the observer takes for a change in front of it. Tools that
// imitate /setblock call it; normal placement does not.
func ObserverSetblockPulse(pos cube.Pos, tx *world.Tx) {
	if o, ok := tx.Block(pos).(Observer); ok && !o.Powered {
		o.schedule(pos, tx)
	}
}
