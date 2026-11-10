//go:build crossfly_full

package block

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Repeaters with redstone=dragonfly: Bedrock Edition's rules (BDS 1.21.3 RepeaterCapacitor, RepeaterBlock) on
// Dragonfly's per-tick engine, with a redstone tick being two game ticks.
//
// A repeater is a delay line as long as its delay in game ticks: each game tick it takes in its input, and what went in
// comes out at the other end after the delay. Bedrock's rules for the end of the line:
//   - an input that turns on there turns the repeater on and keeps it on for at least the delay (a short pulse is
//     lengthened to the delay);
//   - an input that turns off there turns it off only if the whole line is off; else it stays on for another delay
//     (a gap shorter than the delay is filled);
//   - an input that has switched every redstone tick for longer than the delay (a fast clock, delay 2 or more) gives a
//     clock of the delay on and the delay off, instead of a steady on;
//   - a powered repeater or comparator pointing into a side locks it: the line holds its output from the next tick.
// The input is the power reaching the repeater from behind: dust of any shape, a source pointing into it, or a block
// powered directly (dust on or into it, a lever or button on it, a diode pointing into it).
// The line is kept in the engine's component state, and the repeater only ticks while something is on its way.

// Delay-line entries: Bedrock's values. A fresh entry is an input not yet handled at the end of the line, a settled
// one an input the end of the line has taken.
const (
	dfEntryOff = iota
	dfEntryOn
	dfEntryOffSettled
	dfEntryOnSettled
)

// dfRepeaterLine is the state of a repeater in dragonfly mode, packed in a uint64:
//
//	bits  0-15  the line: eight 2-bit entries, entry 0 is the end (the output)
//	bit  16     in: the input as last seen
//	bit  17     locked
//	bit  18     valid
//	bit  19     out: the output last set
//	bits 20-21  facing
//	bits 22-23  delay the line is laid out for (0-3)
//	bits 24-27  flicker: input switches in a row, each at most a redstone tick after the one before
//	bit  28     flicker phase: whether the next fast-clock output is on
//	bits 32-47  the game tick the line last moved (mod 1<<16)
//	bits 48-55  the game tick the input last switched (mod 256)
type dfRepeaterLine uint64

func (l dfRepeaterLine) entry(i int) int { return int(l>>(2*i)) & 3 }
func (l *dfRepeaterLine) setEntry(i, v int) {
	*l = *l&^(3<<(2*i)) | dfRepeaterLine(v&3)<<(2*i)
}
func (l dfRepeaterLine) flag(bit int) bool { return l&(1<<bit) != 0 }
func (l *dfRepeaterLine) setFlag(bit int, v bool) {
	if v {
		*l |= 1 << bit
	} else {
		*l &^= 1 << bit
	}
}
func (l dfRepeaterLine) field(shift, bits int) int { return int(l>>shift) & (1<<bits - 1) }
func (l *dfRepeaterLine) setField(shift, bits, v int) {
	mask := dfRepeaterLine(1<<bits-1) << shift
	*l = *l&^mask | dfRepeaterLine(v)<<shift&mask
}

const (
	dfLineIn      = 16
	dfLineLocked  = 17
	dfLineValid   = 18
	dfLineOut     = 19
	dfLinePhase   = 28
	dfLineFacing  = 20
	dfLineDelay   = 22
	dfLineFlicker = 24
	dfLineMoved   = 32
	dfLineEdge    = 48
)

// dfLength is the repeater's delay in game ticks: the length of its line.
func (r RedstoneRepeater) dfLength() int { return 2 * (min(max(r.Delay, 0), 3) + 1) }

// fill sets the first n entries to v.
func (l *dfRepeaterLine) fill(n, v int) {
	for i := 0; i < n; i++ {
		l.setEntry(i, v)
	}
}

// settledEntry is the settled entry for an input.
func settledEntry(on bool) int {
	if on {
		return dfEntryOnSettled
	}
	return dfEntryOffSettled
}

// dfLoad returns the repeater's line, laid out for its current state: a new one, all settled on its output, if there
// is none or the one there is for another repeater.
func (r RedstoneRepeater) dfLoad(pos cube.Pos, tx *world.Tx) dfRepeaterLine {
	v, ok := tx.Redstone().State(pos)
	l := dfRepeaterLine(v)
	n := r.dfLength()
	if !ok || !l.flag(dfLineValid) || l.field(dfLineFacing, 2) != int(r.Facing) || l.flag(dfLineOut) != r.Powered {
		l = 0
		l.setFlag(dfLineValid, true)
		l.setField(dfLineFacing, 2, int(r.Facing))
		l.setField(dfLineDelay, 2, r.Delay)
		l.setFlag(dfLineOut, r.Powered)
		l.setFlag(dfLineIn, r.Powered)
		l.fill(n, settledEntry(r.Powered))
		l.setField(dfLineMoved, 16, int(tx.CurrentTick()))
		return l
	}
	if old := 2 * (l.field(dfLineDelay, 2) + 1); old != n {
		// The delay changed: Bedrock keeps the line and only changes its length.
		last := l.entry(old - 1)
		for i := old; i < n; i++ {
			l.setEntry(i, last)
		}
		for i := n; i < 8; i++ {
			l.setEntry(i, 0)
		}
		l.setField(dfLineDelay, 2, r.Delay)
	}
	return l
}

// dfAdvance moves the line on to the game tick now, taking in the input as last seen.
func (r RedstoneRepeater) dfAdvance(l *dfRepeaterLine, now int64) {
	n := r.dfLength()
	steps := (int(now) - l.field(dfLineMoved, 16)) & 0xffff
	// After this many steps without a new input the line is settled: more change nothing.
	steps = min(steps, 2*n+4)
	start := now - int64(steps)
	for i := int64(1); i <= int64(steps); i++ {
		r.dfStep(l, n, start+i)
	}
	l.setField(dfLineMoved, 16, int(now))
}

// dfStep moves the line by one game tick, at tick t.
func (r RedstoneRepeater) dfStep(l *dfRepeaterLine, n int, t int64) {
	front := l.entry(0)
	if l.flag(dfLineLocked) {
		// Locked: the output holds, and the line with it.
		l.fill(n, settledEntry(front == dfEntryOn || front == dfEntryOnSettled))
		return
	}
	if (int(t)-l.field(dfLineEdge, 8))&0xff > 2 {
		// The input stayed the same for longer than a redstone tick: no fast clock.
		l.setField(dfLineFlicker, 4, 0)
	}
	for i := 0; i < n-1; i++ {
		l.setEntry(i, l.entry(i+1))
	}
	in := l.flag(dfLineIn)
	if in {
		l.setEntry(n-1, dfEntryOn)
	} else {
		l.setEntry(n-1, dfEntryOff)
	}
	// An input the same as the settled one before it in the line is settled too: only a change reaches the end of
	// the line fresh. (Bedrock settles the entry behind the end each redstone tick, which comes to the same with its
	// shorter line.)
	for i := 1; i < n; i++ {
		if prev, e := l.entry(i-1), l.entry(i); prev >= dfEntryOffSettled && e == prev-2 {
			l.setEntry(i, prev)
		}
	}

	front = l.entry(0)
	d, flicker := min(max(r.Delay, 0), 3), l.field(dfLineFlicker, 4)
	if d > 0 && flicker > d && !(flicker == 2 && in) {
		// A fast clock: alternate full delays on and off.
		if front <= dfEntryOn {
			on := l.flag(dfLinePhase)
			l.fill(n, settledEntry(on))
			l.setFlag(dfLinePhase, !on)
		}
		return
	}
	switch {
	case front == dfEntryOffSettled && l.entry(1) == dfEntryOff:
		l.setEntry(1, dfEntryOffSettled)
	case front == dfEntryOnSettled && l.entry(1) == dfEntryOn:
		l.setEntry(1, dfEntryOnSettled)
	case front == dfEntryOn:
		l.fill(n, dfEntryOnSettled)
	case front == dfEntryOff:
		allOff := true
		for i := 0; i < n; i++ {
			if l.entry(i) != dfEntryOff {
				allOff = false
				break
			}
		}
		if allOff {
			l.fill(n, dfEntryOffSettled)
		} else {
			l.fill(n, dfEntryOnSettled)
		}
	}
}

// dfOutput reports whether the end of the line is on.
func (l dfRepeaterLine) dfOutput() bool {
	e := l.entry(0)
	return e == dfEntryOn || e == dfEntryOnSettled
}

// dfSettled reports whether nothing is on its way through the line: it needs no ticks until its input changes.
func (r RedstoneRepeater) dfSettled(l dfRepeaterLine) bool {
	want := settledEntry(l.flag(dfLineIn))
	if l.flag(dfLineLocked) {
		return true
	}
	for i := 0; i < r.dfLength(); i++ {
		if l.entry(i) != want {
			return false
		}
	}
	return true
}

// dfRecordInput notes the input and lock seen at tick now.
func (l *dfRepeaterLine) dfRecordInput(in, locked bool, now int64) {
	if in != l.flag(dfLineIn) && !l.flag(dfLineLocked) {
		// Bedrock counts switches in a row only while unlocked.
		flicker := l.field(dfLineFlicker, 4)
		if flicker > 0 && (int(now)-l.field(dfLineEdge, 8))&0xff <= 2 {
			flicker = min(flicker+1, 15)
		} else {
			flicker = 1
		}
		if flicker == 1 {
			l.setFlag(dfLinePhase, in)
		}
		l.setField(dfLineFlicker, 4, flicker)
		l.setField(dfLineEdge, 8, int(now))
	}
	l.setFlag(dfLineIn, in)
	l.setFlag(dfLineLocked, locked)
}

// dfInputOn reports whether power reaches the repeater from behind.
func (r RedstoneRepeater) dfInputOn(pos cube.Pos, tx *world.Tx) bool {
	return tx.RedstonePowerFrom(pos, r.back()) > 0
}

// dfUpdate is the repeater's reaction to a change around it: it takes in its input and lock.
func (r RedstoneRepeater) dfUpdate(pos cube.Pos, tx *world.Tx) {
	now := tx.CurrentTick()
	l := r.dfLoad(pos, tx)
	r.dfAdvance(&l, now)
	l.dfRecordInput(r.dfInputOn(pos, tx), r.locked(pos, tx), now)
	r.dfApply(pos, tx, l)
}

// dfTick moves the line on in a scheduled tick.
func (r RedstoneRepeater) dfTick(pos cube.Pos, tx *world.Tx) {
	l := r.dfLoad(pos, tx)
	r.dfAdvance(&l, tx.CurrentTick())
	r.dfApply(pos, tx, l)
}

// dfApply stores the line, sets the repeater's output from it and schedules the next tick while something is on its
// way.
func (r RedstoneRepeater) dfApply(pos cube.Pos, tx *world.Tx, l dfRepeaterLine) {
	if r.dfSettled(l) {
		l.setField(dfLineFlicker, 4, 0)
	}
	if out := l.dfOutput(); out != r.Powered {
		r.Powered = out
		l.setFlag(dfLineOut, out)
		dfSetOutput(pos, r, tx)
	}
	tx.Redstone().SetState(pos, uint64(l))
	if !r.dfSettled(l) {
		tx.ScheduleBlockUpdate(pos, r, time.Second/20)
	}
}

// dfSetOutput writes the new state of a component whose output changed, and has the engine evaluate the circuit
// around it in its next phase, without forgetting the power the component takes in.
func dfSetOutput(pos cube.Pos, b world.Block, tx *world.Tx) {
	tx.SetBlock(pos, b, &world.SetOpts{DisableRedstoneUpdates: true, KeepRedstonePower: true})
	tx.Redstone().ScheduleUpdate(pos)
}
