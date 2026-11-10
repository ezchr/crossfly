//go:build crossfly_full

package block

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
)

// Comparators with redstone=dragonfly: Bedrock Edition's rules (BDS 1.21.3 ComparatorCapacitor, ComparatorBlock,
// ComparatorBlockActor) on Dragonfly's per-tick engine.
//
//   - The output follows the inputs one redstone tick (two game ticks) later, pulses as they are: no lengthening.
//   - Compare mode gives the rear input, or 0 if a side is stronger; subtract mode the rear less the strongest side.
//   - The rear input is the reading of the block behind if it gives one (a container, a cake, ...), else, if the block
//     behind is solid and no power source, the reading of the block behind that; a reading replaces the power from
//     behind entirely. Without a reading it is the power reaching the comparator from behind.
//   - A side input is only dust (whatever its shape) or a repeater or comparator pointing into the side.
//   - The comparator is lit (powered_comparator) while its output is above 0.
// The inputs of the last two game ticks they changed in are kept in the engine's component state, so that the output
// two game ticks later uses the inputs of then.

// dfComparatorState packs two input records, the newest in bits 0-16 and the one before in bits 17-33. A record is
// the rear input (bits 0-3), the side input (4-7), the game tick it was taken in (8-15, mod 256) and a valid bit (16).
type dfComparatorState uint64

type dfComparatorRecord struct {
	rear, side int
	tick       int
	valid      bool
}

func (s dfComparatorState) record(i int) dfComparatorRecord {
	v := uint64(s) >> (17 * i)
	return dfComparatorRecord{rear: int(v & 15), side: int(v >> 4 & 15), tick: int(v >> 8 & 0xff), valid: v>>16&1 == 1}
}

func (s *dfComparatorState) setRecord(i int, r dfComparatorRecord) {
	v := uint64(r.rear&15) | uint64(r.side&15)<<4 | uint64(r.tick&0xff)<<8
	if r.valid {
		v |= 1 << 16
	}
	mask := uint64(1<<17-1) << (17 * i)
	*s = dfComparatorState(uint64(*s)&^mask | v<<(17*i))
}

// dfComparatorDelay is a comparator's delay in game ticks: one redstone tick.
const dfComparatorDelay = 2

// dfRecord notes the inputs seen at the current tick, and schedules the comparator to follow them.
func (c RedstoneComparator) dfRecord(pos cube.Pos, tx *world.Tx, rear, side int) {
	v, _ := tx.Redstone().State(pos)
	s := dfComparatorState(v)
	now := int(tx.CurrentTick())
	newest := s.record(0)
	if newest.valid && newest.rear == rear && newest.side == side {
		return
	}
	rec := dfComparatorRecord{rear: rear, side: side, tick: now & 0xff, valid: true}
	if !newest.valid || newest.tick != now&0xff {
		s.setRecord(1, newest)
	}
	s.setRecord(0, rec)
	tx.Redstone().SetState(pos, uint64(s))
	tx.ScheduleBlockUpdate(pos, c, dfComparatorDelay*time.Second/20)
}

// dfUpdate is the comparator's reaction to a change around it.
func (c RedstoneComparator) dfUpdate(pos cube.Pos, tx *world.Tx) {
	c.dfRecord(pos, tx, c.dfRear(pos, tx), c.dfSide(pos, tx))
}

// dfAnalogCheck takes a new reading of the block behind: how full a container is changes without a block update.
func (c RedstoneComparator) dfAnalogCheck(pos cube.Pos, tx *world.Tx) {
	reading, ok := c.dfReading(pos, tx)
	if !ok {
		return
	}
	v, _ := tx.Redstone().State(pos)
	newest := dfComparatorState(v).record(0)
	if newest.valid && newest.rear == reading {
		return
	}
	side := newest.side
	if !newest.valid {
		side = c.dfSide(pos, tx)
	}
	c.dfRecord(pos, tx, reading, side)
}

// dfTick sets the output from the inputs of two game ticks ago.
func (c RedstoneComparator) dfTick(pos cube.Pos, tx *world.Tx) {
	v, ok := tx.Redstone().State(pos)
	if !ok {
		c.dfUpdate(pos, tx)
		return
	}
	s := dfComparatorState(v)
	now := int(tx.CurrentTick())
	rec := s.record(0)
	pendingNewest := rec.valid && (now-rec.tick)&0xff < dfComparatorDelay
	if pendingNewest {
		rec = s.record(1)
	}
	if rec.valid {
		out := c.dfOutput(rec.rear, rec.side)
		if out != c.Output || c.Powered != (out > 0) {
			c.Output, c.Powered = out, out > 0
			dfSetOutput(pos, c, tx)
		}
	}
	if pendingNewest {
		// The tick scheduled for the newest inputs was for the state before this change.
		tx.ScheduleBlockUpdate(pos, c, time.Duration(dfComparatorDelay-(now-s.record(0).tick)&0xff)*time.Second/20)
	}
}

// dfOutput is the output for the rear and side inputs in the comparator's mode.
func (c RedstoneComparator) dfOutput(rear, side int) int {
	if c.Subtract {
		return max(0, rear-side)
	}
	if side > rear {
		return 0
	}
	return rear
}

// dfRear is the rear input: a reading if there is one, else the power reaching the comparator from behind.
func (c RedstoneComparator) dfRear(pos cube.Pos, tx *world.Tx) int {
	if v, ok := c.dfReading(pos, tx); ok {
		return v
	}
	return tx.RedstonePowerFrom(pos, c.back())
}

// dfReading is the reading the comparator takes behind it: from the block behind, or through a solid block that is
// no power source from the block behind that.
func (c RedstoneComparator) dfReading(pos cube.Pos, tx *world.Tx) (int, bool) {
	behindPos := pos.Side(c.back())
	behind := tx.Block(behindPos)
	if v, ok := bedrockAnalogOutput(behindPos, behind, c.back(), tx); ok {
		return world.ClampRedstonePower(v), true
	}
	if _, source := behind.(world.RedstonePowerSource); source || !world.RedstoneFullPowerConductor(behindPos, behind, tx) {
		return 0, false
	}
	farPos := behindPos.Side(c.back())
	if v, ok := bedrockAnalogOutput(farPos, tx.Block(farPos), c.back(), tx); ok {
		return world.ClampRedstonePower(v), true
	}
	return 0, false
}

// dfSide is the strongest side input: dust of any shape, or a repeater or comparator pointing into the side.
func (c RedstoneComparator) dfSide(pos cube.Pos, tx *world.Tx) int {
	power := 0
	for _, d := range []cube.Direction{c.Facing.RotateLeft(), c.Facing.RotateRight()} {
		face := d.Face()
		sidePos := pos.Side(face)
		switch b := tx.Block(sidePos).(type) {
		case RedstoneWire:
			power = max(power, tx.RedstonePowerFrom(pos, face))
		case RedstoneRepeater, RedstoneComparator:
			power = max(power, world.ClampRedstonePower(b.(world.RedstonePowerSource).RedstonePower(sidePos, tx, face.Opposite())))
		}
	}
	return power
}

// bedrockAnalogOutput is the reading a comparator reading towards dir takes from the block b at pos (Bedrock's
// getComparatorSignal), and whether b gives one.
func bedrockAnalogOutput(pos cube.Pos, b world.Block, dir cube.Face, tx *world.Tx) (int, bool) {
	switch b := b.(type) {
	case EnderChest:
		return 0, true // a reading of its own (empty) storage: 0, and nothing is read through it
	case Jukebox:
		if disc, ok := b.Disc(); ok {
			return javaDiscSignal[disc.String()], true // the same values as Bedrock's RecordItem table
		}
		return 0, true
	case Lectern:
		if b.Book.Empty() {
			return 0, true
		}
		book, ok := b.Book.Item().(readableBook)
		if !ok {
			return 0, true
		}
		spreads, ratio := book.TotalPages()/2, 1.0
		if spreads > 1 {
			ratio = float64(b.Page) / float64(spreads-1)
		}
		return int(math.Floor(ratio*14)) + 1, true
	case DetectorRail:
		return DetectorRailComparatorOutput(pos, tx), true // the first container minecart on it (mech)
	case CopperBulb:
		return CopperBulbComparatorOutput(b), true
	case Cake:
		return 2 * (7 - b.Bites), true
	case Cauldron:
		return (b.Level + 1) / 2, true // fill level 0-6, whatever is in it
	case Composter:
		return b.Level, true
	case EndPortalFrame:
		if b.Eye {
			return 15, true
		}
		return 0, true
	case ItemFrame:
		// Only a frame facing away from the comparator gives a reading.
		if b.Item.Empty() || b.Facing.Opposite() != dir {
			return 0, true
		}
		return min(15, b.Rotations%8+1), true
	case sculkSensorBlock:
		return sculkSensorComparatorOutput(b), true
	case Beehive:
		return b.HoneyLevel, true
	case CopperGolemStatue:
		// Not in BDS 1.21.3: Java Edition's reading as a fallback (standing 1, sitting 2, running 3, star 4).
		return int(b.Pose.Uint8()) + 1, true
	case DecoratedPot:
		if b.Item.Empty() {
			return 0, true
		}
		return int(math.Floor(float64(b.Item.Count())/float64(b.Item.MaxCount())*14)) + 1, true
	case interface {
		Inventory(*world.Tx, cube.Pos) *inventory.Inventory
	}:
		return bedrockContainerSignal(b.Inventory(tx, pos)), true
	}
	return 0, false
}

// bedrockContainerSignal is Bedrock's reading of a container (Container::getRedstoneSignalFromContainer): each slot
// counts as full at the most its item stacks to, the sum is divided by the number of slots, times 14 rounded down, plus
// 1 if any slot holds something.
func bedrockContainerSignal(inv *inventory.Inventory) int {
	if inv == nil || inv.Size() == 0 {
		return 0
	}
	full, any := float32(0), false
	for _, s := range inv.Slots() {
		if s.Empty() {
			continue
		}
		any = true
		full += float32(s.Count()) / float32(s.MaxCount())
	}
	if !any {
		return 0
	}
	return int(math.Floor(float64(full/float32(inv.Size())*14))) + 1
}
