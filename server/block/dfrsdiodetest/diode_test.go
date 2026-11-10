//go:build crossfly_full

// Package dfrsdiodetest tests repeaters, comparators, observers and the sensors with redstone=dragonfly (Bedrock
// Edition's rules on Dragonfly's engine). It is a package of its own: the redstone mode is set once per process.
package dfrsdiodetest

import (
	"context"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func init() {
	world.SetJavaRedstone(false)
	block.RegisterJavaRedstone()
	item.RegisterJavaRedstone()
	block.RegisterMobBlocks()
}

// rig is a test world with a stone floor at y 63.
type rig struct {
	t *testing.T
	w *world.World
}

func newRig(t *testing.T) *rig {
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	r := &rig{t: t, w: w}
	r.do(func(tx *world.Tx) {
		for x := -8; x <= 8; x++ {
			for z := -8; z <= 8; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
	})
	r.tick(2)
	return r
}

func (r *rig) do(f func(tx *world.Tx)) { r.w.Do(f).Wait(context.Background()) }

func (r *rig) set(pos cube.Pos, b world.Block) {
	r.do(func(tx *world.Tx) { tx.SetBlock(pos, b, nil) })
}

func (r *rig) block(pos cube.Pos) world.Block {
	var b world.Block
	r.do(func(tx *world.Tx) { b = tx.Block(pos) })
	return b
}

func (r *rig) tick(n int) {
	for range n {
		r.w.AdvanceTick()
	}
}

// ticksUntil advances the world until ok holds and returns how many ticks that took, or fails after max ticks.
func (r *rig) ticksUntil(max int, ok func() bool) int {
	r.t.Helper()
	for i := 1; i <= max; i++ {
		r.w.AdvanceTick()
		if ok() {
			return i
		}
	}
	r.t.Fatalf("condition not met within %d ticks", max)
	return 0
}

func (r *rig) repeaterOn(pos cube.Pos) bool {
	rep, ok := r.block(pos).(block.RedstoneRepeater)
	if !ok {
		r.t.Fatalf("no repeater at %v: %T", pos, r.block(pos))
	}
	return rep.Powered
}

// trace records whether the repeater at pos is on over n ticks.
func (r *rig) trace(pos cube.Pos, n int) []bool {
	out := make([]bool, n)
	for i := range out {
		r.w.AdvanceTick()
		out[i] = r.repeaterOn(pos)
	}
	return out
}

// runs returns the lengths of the runs of on in a trace.
func runs(trace []bool) (on []int) {
	n := 0
	for _, v := range trace {
		if v {
			n++
		} else if n > 0 {
			on, n = append(on, n), 0
		}
	}
	if n > 0 {
		on = append(on, n)
	}
	return on
}

// The repeater at (0,64,0) takes its input from the west (x -1) and powers the east.
var (
	repPos    = cube.Pos{0, 64, 0}
	repInput  = cube.Pos{-1, 64, 0}
	repOutput = cube.Pos{1, 64, 0}
)

func placeRepeater(r *rig, delay int) {
	r.set(repPos, block.RedstoneRepeater{Facing: cube.West, Delay: delay})
	r.tick(2)
}

func TestRepeaterDelayInGameTicks(t *testing.T) {
	for delay := range 4 {
		r := newRig(t)
		placeRepeater(r, delay)
		r.set(repInput, block.RedstoneBlock{})
		// The engine sees the new input in the next tick; the repeater turns on its delay later.
		got := r.ticksUntil(20, func() bool { return r.repeaterOn(repPos) })
		if want := 1 + 2*(delay+1); got != want {
			t.Errorf("delay %d: on after %d ticks, want %d", delay+1, got, want)
		}
		r.set(repInput, nil)
		got = r.ticksUntil(20, func() bool { return !r.repeaterOn(repPos) })
		if want := 1 + 2*(delay+1); got != want {
			t.Errorf("delay %d: off after %d ticks, want %d", delay+1, got, want)
		}
	}
}

func TestRepeaterChainAddsDelays(t *testing.T) {
	r := newRig(t)
	placeRepeater(r, 0)
	second := repOutput
	r.set(second, block.RedstoneRepeater{Facing: cube.West, Delay: 2})
	r.tick(2)
	r.set(repInput, block.RedstoneBlock{})
	first := r.ticksUntil(30, func() bool { return r.repeaterOn(repPos) })
	more := r.ticksUntil(30, func() bool { return r.repeaterOn(second) })
	if first != 3 || more != 6 {
		t.Fatalf("first on after %d ticks, second %d ticks later; want 3 and 6", first, more)
	}
}

func TestRepeaterLengthensShortPulse(t *testing.T) {
	r := newRig(t)
	placeRepeater(r, 3) // 8 game ticks
	r.set(repInput, block.RedstoneBlock{})
	r.tick(1)
	r.set(repInput, nil)
	if got := runs(r.trace(repPos, 30)); len(got) != 1 || got[0] != 8 {
		t.Fatalf("on runs %v, want one of 8 ticks", got)
	}
}

func TestRepeaterFillsShortGap(t *testing.T) {
	for _, test := range []struct {
		gap  int
		want int // runs of on
	}{{gap: 3, want: 1}, {gap: 5, want: 1}, {gap: 7, want: 2}, {gap: 10, want: 2}} {
		r := newRig(t)
		placeRepeater(r, 2) // 6 game ticks
		var trace []bool
		step := func(n int) {
			for range n {
				r.w.AdvanceTick()
				trace = append(trace, r.repeaterOn(repPos))
			}
		}
		r.set(repInput, block.RedstoneBlock{})
		step(12)
		r.set(repInput, nil)
		step(test.gap)
		r.set(repInput, block.RedstoneBlock{})
		step(10)
		r.set(repInput, nil)
		step(30)
		if got := runs(trace); len(got) != test.want {
			t.Errorf("gap %d: on runs %v, want %d", test.gap, got, test.want)
		}
	}
}

func TestRepeaterDelayOnePassesRedstoneTickPulse(t *testing.T) {
	r := newRig(t)
	placeRepeater(r, 0)
	r.set(repInput, block.RedstoneBlock{})
	r.tick(2)
	r.set(repInput, nil)
	if got := runs(r.trace(repPos, 12)); len(got) != 1 || got[0] != 2 {
		t.Fatalf("on runs %v, want one of 2 ticks", got)
	}
}

func TestRepeaterFastClockBecomesDelayClock(t *testing.T) {
	r := newRig(t)
	placeRepeater(r, 2) // delay 3: 6 game ticks
	var trace []bool
	on := false
	for i := 0; i < 60; i++ {
		if i%2 == 0 {
			on = !on
			if on {
				r.set(repInput, block.RedstoneBlock{})
			} else {
				r.set(repInput, nil)
			}
		}
		r.w.AdvanceTick()
		trace = append(trace, r.repeaterOn(repPos))
	}
	got := runs(trace[20:])
	if len(got) < 3 {
		t.Fatalf("on runs %v: a one-redstone-tick clock into a delay-3 repeater must give a clock", got)
	}
	for _, n := range got[1 : len(got)-1] {
		if n != 6 {
			t.Fatalf("on runs %v, want runs of 6 ticks (the delay)", got)
		}
	}
}

func TestRepeaterLockedBySidePoweredRepeater(t *testing.T) {
	r := newRig(t)
	placeRepeater(r, 0)
	// A repeater north of it, pointing south into its side, powered from the north.
	side := cube.Pos{0, 64, -1}
	r.set(side, block.RedstoneRepeater{Facing: cube.North})
	r.set(cube.Pos{0, 64, -2}, block.RedstoneBlock{})
	r.tick(6)
	if !r.repeaterOn(side) {
		t.Fatal("side repeater did not turn on")
	}
	r.set(repInput, block.RedstoneBlock{})
	r.tick(10)
	if r.repeaterOn(repPos) {
		t.Fatal("a locked repeater turned on")
	}
	// Unlocked, it passes the input on.
	r.set(cube.Pos{0, 64, -2}, nil)
	r.ticksUntil(20, func() bool { return r.repeaterOn(repPos) })
}

func TestRepeaterReadsDustOfAnyShapeAndPoweredBlock(t *testing.T) {
	r := newRig(t)
	placeRepeater(r, 0)
	// Dust behind it, fed from the north: it points north-south, not into the repeater.
	r.set(repInput, block.RedstoneWire{})
	r.set(cube.Pos{-1, 64, -1}, block.RedstoneBlock{})
	r.ticksUntil(10, func() bool { return r.repeaterOn(repPos) })

	r2 := newRig(t)
	placeRepeater(r2, 0)
	// A stone block behind it, powered by a lever on its far side.
	r2.set(repInput, block.Stone{})
	r2.set(cube.Pos{-2, 64, 0}, block.Lever{Facing: cube.FaceWest, Powered: true})
	r2.ticksUntil(10, func() bool { return r2.repeaterOn(repPos) })
}

func comparatorAt(r *rig, pos cube.Pos) block.RedstoneComparator {
	c, ok := r.block(pos).(block.RedstoneComparator)
	if !ok {
		r.t.Fatalf("no comparator at %v: %T", pos, r.block(pos))
	}
	return c
}

func TestComparatorReadsCakeAndSubtractsSide(t *testing.T) {
	r := newRig(t)
	r.set(repInput, block.Cake{Bites: 2}) // 2*(7-2) = 10
	r.set(repPos, block.RedstoneComparator{Facing: cube.West})
	r.tick(4)
	if c := comparatorAt(r, repPos); c.Output != 10 || !c.Powered {
		t.Fatalf("compare mode reading a cake with 2 bites: output %d powered %t, want 10 true", c.Output, c.Powered)
	}
	// Dust at its north side with power 14 (one step from a redstone block).
	r.set(cube.Pos{0, 64, -1}, block.RedstoneWire{})
	r.set(cube.Pos{0, 64, -2}, block.RedstoneWire{})
	r.set(cube.Pos{0, 64, -3}, block.RedstoneBlock{})
	r.tick(4)
	if c := comparatorAt(r, repPos); c.Output != 0 || c.Powered {
		t.Fatalf("compare mode, side 14 > rear 10: output %d powered %t, want 0 false", c.Output, c.Powered)
	}
	r.do(func(tx *world.Tx) {
		c := tx.Block(repPos).(block.RedstoneComparator)
		c.Activate(repPos, cube.FaceUp, tx, nil, nil)
	})
	r.set(cube.Pos{0, 64, -3}, nil)
	r.set(cube.Pos{0, 64, -3}, block.RedstoneWire{})
	r.set(cube.Pos{0, 64, -4}, block.RedstoneWire{})
	r.set(cube.Pos{0, 64, -5}, block.RedstoneWire{})
	r.set(cube.Pos{0, 64, -6}, block.RedstoneWire{})
	r.set(cube.Pos{0, 64, -7}, block.RedstoneWire{})
	r.set(cube.Pos{0, 64, -8}, block.RedstoneBlock{})
	r.tick(6)
	// Side dust now has 15-6 = 9 (seven dust from the block): subtract gives 10-9.
	if c := comparatorAt(r, repPos); c.Output != 1 || !c.Powered || !c.Subtract {
		t.Fatalf("subtract mode, rear 10 side 9: output %d powered %t subtract %t, want 1 true true", c.Output, c.Powered, c.Subtract)
	}
}

func TestComparatorDelayIsOneRedstoneTick(t *testing.T) {
	r := newRig(t)
	r.set(repPos, block.RedstoneComparator{Facing: cube.West})
	r.tick(4)
	r.set(repInput, block.RedstoneBlock{})
	if got := r.ticksUntil(10, func() bool { return comparatorAt(r, repPos).Powered }); got != 3 {
		t.Fatalf("on after %d ticks, want 3", got)
	}
	if c := comparatorAt(r, repPos); c.Output != 15 {
		t.Fatalf("output %d, want 15", c.Output)
	}
}

func TestComparatorReadsContainerThroughSolidBlock(t *testing.T) {
	r := newRig(t)
	chestPos := cube.Pos{-2, 64, 0}
	r.do(func(tx *world.Tx) {
		tx.SetBlock(chestPos, block.NewChest(), nil)
		inv := tx.Block(chestPos).(block.Chest).Inventory(tx, chestPos)
		for range 14 {
			_, _ = inv.AddItem(item.NewStack(block.Stone{}, 64))
		}
	})
	r.set(repInput, block.Stone{})
	r.set(repPos, block.RedstoneComparator{Facing: cube.West})
	r.tick(4)
	// 14 full slots of 27: floor(14/27*14)+1 = 8.
	if c := comparatorAt(r, repPos); c.Output != 8 {
		t.Fatalf("reading a chest with 14 of 27 slots full through stone: %d, want 8", c.Output)
	}
}

func TestComparatorItemFrameMustFaceAway(t *testing.T) {
	for _, test := range []struct {
		facing cube.Face
		want   int
	}{{facing: cube.FaceEast, want: 4}, {facing: cube.FaceSouth, want: 0}} {
		r := newRig(t)
		// comparator <- stone <- frame on the far side of the stone (or on a block south of it). Facing is the
		// direction from the frame to its block.
		r.set(repInput, block.Stone{})
		r.set(cube.Pos{-2, 64, 1}, block.Stone{})
		r.set(cube.Pos{-2, 64, 0}, block.ItemFrame{Facing: test.facing, Item: item.NewStack(item.Stick{}, 1), Rotations: 3})
		r.set(repPos, block.RedstoneComparator{Facing: cube.West})
		r.tick(4)
		if c := comparatorAt(r, repPos); c.Output != test.want {
			t.Errorf("item frame facing %v: %d, want %d", test.facing, c.Output, test.want)
		}
	}
}

func TestObserverPulse(t *testing.T) {
	r := newRig(t)
	obs := cube.Pos{0, 64, 0}
	front := cube.Pos{1, 64, 0}
	r.set(obs, block.Observer{Facing: cube.FaceEast})
	r.tick(4)
	r.set(front, block.Stone{})
	var trace []bool
	for range 12 {
		r.w.AdvanceTick()
		trace = append(trace, r.block(obs).(block.Observer).Powered)
		if len(trace) == 3 {
			// A change during the pulse is passed by.
			r.set(front, block.Dirt{})
		}
	}
	want := []bool{false, false, true, true, false}
	for i, v := range want {
		if trace[i] != v {
			t.Fatalf("observer trace %v, want it to start %v (fires 2 ticks after, 2-tick pulse)", trace, want)
		}
	}
	if got := runs(trace); len(got) != 1 {
		t.Fatalf("observer pulses %v, want one", got)
	}
}

func TestObserverPlacedGivesNoPulse(t *testing.T) {
	r := newRig(t)
	r.set(cube.Pos{0, 64, 0}, block.Observer{Facing: cube.FaceEast})
	for range 8 {
		r.w.AdvanceTick()
		if r.block(cube.Pos{0, 64, 0}).(block.Observer).Powered {
			t.Fatal("a placed observer pulsed")
		}
	}
}

func spawnItem(tx *world.Tx, pos mgl64.Vec3) world.Entity {
	return tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: pos}, item.NewStack(block.Stone{}, 1)))
}

func TestPressurePlates(t *testing.T) {
	for _, test := range []struct {
		name    string
		plate   block.PressurePlateType
		pressed bool
		release int
	}{
		{name: "stone ignores items", plate: block.StonePressurePlate()},
		{name: "wood takes items", plate: block.OakPressurePlate(), pressed: true, release: 20},
		{name: "light weighted", plate: block.LightWeightedPressurePlate(), pressed: true, release: 10},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newRig(t)
			pos := cube.Pos{0, 64, 0}
			r.set(pos, block.PressurePlate{Type: test.plate})
			var it world.Entity
			r.do(func(tx *world.Tx) {
				it = spawnItem(tx, pos.Vec3Middle().Add(mgl64.Vec3{0, 0.05, 0}))
				tx.Block(pos).(block.PressurePlate).EntityInside(pos, tx, it)
			})
			p := r.block(pos).(block.PressurePlate)
			if (p.Power > 0) != test.pressed {
				t.Fatalf("pressed %t, want %t", p.Power > 0, test.pressed)
			}
			if !test.pressed {
				return
			}
			r.do(func(tx *world.Tx) {
				if e, ok := it.H().Entity(tx); ok {
					_ = e.Close()
				}
			})
			got := r.ticksUntil(40, func() bool { return r.block(pos).(block.PressurePlate).Power == 0 })
			if got != test.release {
				t.Fatalf("released %d ticks after, want %d", got, test.release)
			}
		})
	}
}

func TestTripwire(t *testing.T) {
	r := newRig(t)
	west, east := cube.Pos{-2, 64, 0}, cube.Pos{2, 64, 0}
	r.set(cube.Pos{-3, 64, 0}, block.Stone{})
	r.set(cube.Pos{3, 64, 0}, block.Stone{})
	r.set(west, block.TripwireHook{Facing: cube.East})
	r.set(east, block.TripwireHook{Facing: cube.West})
	for x := -1; x <= 1; x++ {
		r.do(func(tx *world.Tx) {
			pos := cube.Pos{x, 64, 0}
			ctx := &item.UseContext{}
			block.String{}.UseOnBlock(pos.Side(cube.FaceDown), cube.FaceUp, mgl64.Vec3{}, tx, nil, ctx)
		})
	}
	r.tick(2)
	for _, p := range []cube.Pos{west, east} {
		if h := r.block(p).(block.TripwireHook); !h.Attached || h.Powered {
			t.Fatalf("hook at %v attached %t powered %t, want attached and not powered", p, h.Attached, h.Powered)
		}
	}
	mid := cube.Pos{0, 64, 0}
	var it world.Entity
	r.do(func(tx *world.Tx) {
		it = spawnItem(tx, mid.Vec3Middle().Add(mgl64.Vec3{0, 0.02, 0}))
		tx.Block(mid).(block.String).EntityInside(mid, tx, it)
	})
	for _, p := range []cube.Pos{west, east} {
		if h := r.block(p).(block.TripwireHook); !h.Powered {
			t.Fatalf("hook at %v not powered after the wire was tripped", p)
		}
	}
	r.do(func(tx *world.Tx) {
		if e, ok := it.H().Entity(tx); ok {
			_ = e.Close()
		}
	})
	if got := r.ticksUntil(30, func() bool { return !r.block(west).(block.TripwireHook).Powered }); got > 10 {
		t.Fatalf("hook released %d ticks after, want at most 10", got)
	}
	// Cut with shears: the hooks detach without a signal.
	r.do(func(tx *world.Tx) {
		s := tx.Block(mid).(block.String)
		s.PlayerWillBreak(mid, item.NewStack(item.Shears{}, 1), tx)
		tx.SetBlock(mid, nil, nil)
	})
	r.tick(2)
	for _, p := range []cube.Pos{west, east} {
		if h := r.block(p).(block.TripwireHook); h.Attached || h.Powered {
			t.Fatalf("hook at %v attached %t powered %t after the wire was cut, want neither", p, h.Attached, h.Powered)
		}
	}
}

func TestButtonPressTimes(t *testing.T) {
	for _, test := range []struct {
		typ  block.ButtonType
		want int
	}{{typ: block.StoneButton(), want: 20}, {typ: block.PolishedBlackstoneButton(), want: 20}, {typ: block.OakButton(), want: 30}} {
		r := newRig(t)
		pos := cube.Pos{0, 64, 0}
		r.set(pos, block.Button{Type: test.typ, Facing: cube.FaceUp})
		r.do(func(tx *world.Tx) {
			tx.Block(pos).(block.Button).Activate(pos, cube.FaceUp, tx, nil, nil)
		})
		if !r.block(pos).(block.Button).Pressed {
			t.Fatalf("%v not pressed", test.typ)
		}
		if got := r.ticksUntil(40, func() bool { return !r.block(pos).(block.Button).Pressed }); got != test.want {
			t.Errorf("%v released after %d ticks, want %d", test.typ, got, test.want)
		}
	}
}

func TestActiveSculkSensorPowersCircuit(t *testing.T) {
	r := newRig(t)
	// An active sensor beside a repeater's input powers it, and its signal ends with the active phase.
	r.set(repInput, block.SculkSensor{Phase: 1, Power: 6, Frequency: 3})
	placeRepeater(r, 0)
	r.ticksUntil(10, func() bool { return r.repeaterOn(repPos) })
	r.set(repInput, block.SculkSensor{Phase: 2})
	r.ticksUntil(10, func() bool { return !r.repeaterOn(repPos) })
}
