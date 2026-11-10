//go:build crossfly_full

package block

import (
	"context"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// dragonflyRedstone switches Crossfly Full to redstone=dragonfly for the test and back to Java redstone after it.
// Worlds must be made after the call.
func dragonflyRedstone(t *testing.T) {
	t.Helper()
	world.SetJavaRedstone(false)
	t.Cleanup(func() { world.SetJavaRedstone(true) })
}

// dfRedstoneWorld returns a synchronous End world with Dragonfly's redstone, its chunks around the origin loaded and
// ticking, and a function that runs f in a transaction.
func dfRedstoneWorld(t *testing.T) (*world.World, func(f func(tx *world.Tx))) {
	t.Helper()
	dragonflyRedstone(t)
	w := world.Config{Dim: world.End, Synchronous: true}.New()
	loader := world.NewLoader(2, w, world.NopViewer{})
	run := func(f func(tx *world.Tx)) { w.Do(f).Wait(context.Background()) }
	run(func(tx *world.Tx) {
		loader.Move(tx, mgl64.Vec3{0, 64, 0})
		loader.Load(tx, 16)
		for x := -4; x <= 6; x++ {
			for z := -4; z <= 4; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, Stone{}, nil)
			}
		}
	})
	t.Cleanup(func() {
		run(func(tx *world.Tx) { loader.Close(tx) })
		_ = w.Close()
	})
	return w, run
}

// dfrsTicks advances the world n ticks.
func dfrsTicks(w *world.World, n int) {
	for range n {
		w.AdvanceTick()
	}
}

// dfLever sets a lever on the floor at pos, on or off.
func dfLever(tx *world.Tx, pos cube.Pos, on bool) {
	tx.SetBlock(pos, Lever{Facing: cube.FaceUp, Powered: on}, nil)
}

func TestDragonflyRedstoneLampLightsAndTurnsOffAfterTwoRedstoneTicks(t *testing.T) {
	w, run := dfRedstoneWorld(t)
	lamp, lever := cube.Pos{0, 64, 0}, cube.Pos{1, 64, 0}
	lit := func() (l bool) {
		run(func(tx *world.Tx) { l = tx.Block(lamp).(RedstoneLamp).Lit })
		return
	}
	run(func(tx *world.Tx) {
		tx.SetBlock(lamp, RedstoneLamp{}, nil)
		dfLever(tx, lever, true)
	})
	dfrsTicks(w, 1)
	if !lit() {
		t.Fatal("lamp did not light the tick its lever was switched on")
	}
	run(func(tx *world.Tx) { dfLever(tx, lever, false) })
	dfrsTicks(w, 3)
	if !lit() {
		t.Fatal("lamp went off before its off delay (4 game ticks, RedstoneLampBlock::onRedstoneUpdate)")
	}
	dfrsTicks(w, 3)
	if lit() {
		t.Fatal("lamp still lit after its off delay")
	}
	// A pulse shorter than the delay: the lamp stays lit through it.
	run(func(tx *world.Tx) { dfLever(tx, lever, true) })
	dfrsTicks(w, 1)
	run(func(tx *world.Tx) { dfLever(tx, lever, false) })
	dfrsTicks(w, 2)
	run(func(tx *world.Tx) { dfLever(tx, lever, true) })
	dfrsTicks(w, 6)
	if !lit() {
		t.Fatal("lamp went off although its power came back within the off delay")
	}
}

func TestDragonflyRedstoneDoorFollowsPowerChangesOnly(t *testing.T) {
	w, run := dfRedstoneWorld(t)
	bottom, top, lever := cube.Pos{0, 64, 0}, cube.Pos{0, 65, 0}, cube.Pos{1, 64, 0}
	open := func() (b, tp bool) {
		run(func(tx *world.Tx) {
			b, tp = tx.Block(bottom).(WoodDoor).Open, tx.Block(top).(WoodDoor).Open
		})
		return
	}
	run(func(tx *world.Tx) {
		tx.SetBlock(bottom, WoodDoor{Wood: OakWood()}, nil)
		tx.SetBlock(top, WoodDoor{Wood: OakWood(), Top: true}, nil)
	})
	dfrsTicks(w, 2)
	run(func(tx *world.Tx) { dfLever(tx, lever, true) })
	dfrsTicks(w, 2)
	if b, tp := open(); !b || !tp {
		t.Fatalf("door not opened by power: bottom=%t top=%t", b, tp)
	}
	// Closed by hand while powered: it stays closed, as the power does not change.
	run(func(tx *world.Tx) { tx.Block(bottom).(WoodDoor).Activate(bottom, cube.FaceUp, tx, nil, nil) })
	dfrsTicks(w, 5)
	if b, tp := open(); b || tp {
		t.Fatalf("door closed by hand reopened without a power change: bottom=%t top=%t", b, tp)
	}
	run(func(tx *world.Tx) { dfLever(tx, lever, false) })
	dfrsTicks(w, 3)
	if b, _ := open(); b {
		t.Fatal("door opened when its power went off")
	}
	run(func(tx *world.Tx) { dfLever(tx, lever, true) })
	dfrsTicks(w, 2)
	if b, tp := open(); !b || !tp {
		t.Fatalf("door not opened by power again: bottom=%t top=%t", b, tp)
	}
	run(func(tx *world.Tx) { dfLever(tx, lever, false) })
	dfrsTicks(w, 2)
	if b, tp := open(); b || tp {
		t.Fatalf("door not closed when power went off: bottom=%t top=%t", b, tp)
	}
	// Power at the top half opens both halves too.
	run(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{1, 65, 0}, RedstoneBlock{}, nil)
	})
	dfrsTicks(w, 2)
	if b, tp := open(); !b || !tp {
		t.Fatalf("door not opened by power at its top half: bottom=%t top=%t", b, tp)
	}
}

func TestDragonflyRedstoneTrapdoorAndFenceGateFollowPower(t *testing.T) {
	w, run := dfRedstoneWorld(t)
	trapdoor, gate, lever := cube.Pos{0, 64, 0}, cube.Pos{2, 64, 0}, cube.Pos{1, 64, 0}
	state := func() (td, fg bool) {
		run(func(tx *world.Tx) {
			td, fg = tx.Block(trapdoor).(WoodTrapdoor).Open, tx.Block(gate).(WoodFenceGate).Open
		})
		return
	}
	run(func(tx *world.Tx) {
		tx.SetBlock(trapdoor, WoodTrapdoor{Wood: OakWood()}, nil)
		tx.SetBlock(gate, WoodFenceGate{Wood: OakWood(), Facing: cube.North}, nil)
	})
	dfrsTicks(w, 2)
	run(func(tx *world.Tx) { dfLever(tx, lever, true) })
	dfrsTicks(w, 2)
	if td, fg := state(); !td || !fg {
		t.Fatalf("not opened by power: trapdoor=%t gate=%t", td, fg)
	}
	run(func(tx *world.Tx) { dfLever(tx, lever, false) })
	dfrsTicks(w, 2)
	if td, fg := state(); td || fg {
		t.Fatalf("not closed when power went off: trapdoor=%t gate=%t", td, fg)
	}
}

func TestDragonflyRedstoneHopperLock(t *testing.T) {
	w, run := dfRedstoneWorld(t)
	hopper, lever := cube.Pos{0, 64, 0}, cube.Pos{1, 64, 0}
	powered := func() (p bool) {
		run(func(tx *world.Tx) { p = tx.Block(hopper).(Hopper).Powered })
		return
	}
	run(func(tx *world.Tx) {
		h := NewHopper()
		h.Facing = cube.FaceDown
		tx.SetBlock(hopper, h, nil)
		dfLever(tx, lever, true)
	})
	dfrsTicks(w, 2)
	if !powered() {
		t.Fatal("powered hopper not locked")
	}
	run(func(tx *world.Tx) { dfLever(tx, lever, false) })
	dfrsTicks(w, 2)
	if powered() {
		t.Fatal("hopper still locked without power")
	}
}

// Bedrock Edition's dust powers a consumer it runs towards or that a dot sits next to, not one it runs past: a
// trapdoor is no power flow direction for dust (TransporterComponent), unlike a power source or a piston.
func TestDragonflyRedstoneDustRunningPastAConsumerDoesNotPowerIt(t *testing.T) {
	w, run := dfRedstoneWorld(t)
	beside, end := cube.Pos{1, 64, 1}, cube.Pos{3, 64, 0}
	run(func(tx *world.Tx) {
		for x := 0; x <= 2; x++ {
			tx.SetBlock(cube.Pos{x, 64, 0}, RedstoneWire{}, nil)
		}
		tx.SetBlock(beside, WoodTrapdoor{Wood: OakWood()}, nil)
		tx.SetBlock(end, WoodTrapdoor{Wood: OakWood()}, nil)
		dfLever(tx, cube.Pos{-1, 64, 0}, true)
	})
	dfrsTicks(w, 3)
	var besideOpen, endOpen bool
	var shape WireConnections
	run(func(tx *world.Tx) {
		besideOpen, endOpen = tx.Block(beside).(WoodTrapdoor).Open, tx.Block(end).(WoodTrapdoor).Open
		shape = tx.Block(cube.Pos{1, 64, 0}).(RedstoneWire).Connections
	})
	if besideOpen {
		t.Fatalf("trapdoor beside a line of dust opened; dust shape %+v", shape)
	}
	if !endOpen {
		t.Fatal("trapdoor at the end of a line of dust did not open")
	}
}

func TestDragonflyRedstoneTorchLoopBurnsOutAfterFifteenSwitches(t *testing.T) {
	w, run := dfRedstoneWorld(t)
	torch, attachment := cube.Pos{1, 65, 0}, cube.Pos{0, 65, 0}
	loopWire := cube.Pos{0, 66, 0}
	run(func(tx *world.Tx) {
		tx.SetBlock(attachment, Stone{}, nil)
		tx.SetBlock(torch.Side(cube.FaceUp), Stone{}, nil)
		tx.SetBlock(torch.Side(cube.FaceDown), Stone{}, nil)
		tx.SetBlock(cube.Pos{1, 67, 0}, RedstoneWire{}, nil)
		tx.SetBlock(loopWire, RedstoneWire{}, nil)
		tx.SetBlock(torch, RedstoneTorch{Facing: cube.FaceWest, Lit: true}, nil)
	})
	switches, last := 0, true
	var burnedOut bool
	state := func() (lit, burned bool) {
		run(func(tx *world.Tx) {
			lit = tx.Block(torch).(RedstoneTorch).Lit
			burned, _ = tx.Redstone().Torch(torch).BurnoutStatus()
		})
		return
	}
	for range 200 {
		w.AdvanceTick()
		lit, burned := state()
		if lit != last {
			switches++
			last = lit
		}
		if burned {
			burnedOut = true
			break
		}
	}
	if !burnedOut {
		t.Fatalf("torch loop did not burn out; %d switches", switches)
	}
	if switches != redstoneTorchTestSwitches {
		t.Fatalf("torch burned out after %d switches, want %d", switches, redstoneTorchTestSwitches)
	}
	dfrsTicks(w, 100)
	if lit, burned := state(); lit || !burned {
		t.Fatalf("burned-out torch recovered with its loop intact: lit=%t burned=%t", lit, burned)
	}
	// Something unrelated beside it does not relight it: the loop is still there.
	run(func(tx *world.Tx) { tx.SetBlock(torch.Side(cube.FaceEast), Stone{}, nil) })
	dfrsTicks(w, 10)
	if lit, _ := state(); lit {
		t.Fatal("burned-out torch relit from an unrelated block beside it")
	}
	run(func(tx *world.Tx) { tx.SetBlock(loopWire, Air{}, nil) })
	dfrsTicks(w, 5)
	if lit, burned := state(); !lit || burned {
		t.Fatalf("torch did not relight once its loop was broken: lit=%t burned=%t", lit, burned)
	}
}

// redstoneTorchTestSwitches is the number of switches a torch placed lit in a loop makes: it goes off because it was
// placed powered (not counted), then switches fifteen times because of its own output and goes off at the sixteenth.
const redstoneTorchTestSwitches = 17

// Every redstone block of Crossfly Full exists with redstone=dragonfly and survives power going on and off around it.
func TestDragonflyRedstoneEveryComponentSurvivesPower(t *testing.T) {
	blocks := []world.Block{
		allRedstoneRepeaters()[0], allRedstoneComparators()[0], allObservers()[0], allPistons()[0],
		Piston{Sticky: true, Facing: cube.FaceUp}, Button{Facing: cube.FaceUp}, allPressurePlates()[0],
		allDaylightDetectors()[0], NewDispenser(), NewDropper(), allRails()[0], RedstoneLamp{}, allIronDoors()[0],
		allCopperBulbs()[0], Target{}, NewHopper(), Note{}, RedstoneWire{}, RedstoneTorch{Facing: cube.FaceDown},
		WoodDoor{Wood: OakWood()}, WoodTrapdoor{Wood: OakWood()}, WoodFenceGate{Wood: OakWood()},
	}
	for _, b := range blocks {
		name, _ := b.EncodeBlock()
		t.Run(name, func(t *testing.T) {
			w, run := dfRedstoneWorld(t)
			pos, lever := cube.Pos{0, 64, 0}, cube.Pos{1, 64, 0}
			run(func(tx *world.Tx) { tx.SetBlock(pos, b, nil) })
			for i := range 6 {
				run(func(tx *world.Tx) { dfLever(tx, lever, i%2 == 0) })
				dfrsTicks(w, 5)
			}
		})
	}
}

// A lamp beside a line of dust is not powered by it: it neither lights nor stays lit (its off tick reads the power
// the way the compiled graph does).
func TestDragonflyRedstoneLampBesideDustLine(t *testing.T) {
	w, run := dfRedstoneWorld(t)
	lamp := cube.Pos{1, 64, 1}
	run(func(tx *world.Tx) {
		for x := 0; x <= 2; x++ {
			tx.SetBlock(cube.Pos{x, 64, 0}, RedstoneWire{}, nil)
		}
		tx.SetBlock(lamp, RedstoneLamp{Lit: true}, nil)
		dfLever(tx, cube.Pos{-1, 64, 0}, true)
	})
	dfrsTicks(w, 10)
	var lit bool
	var power int
	run(func(tx *world.Tx) {
		lit = tx.Block(lamp).(RedstoneLamp).Lit
		power = tx.RedstonePower(lamp)
	})
	if lit || power != 0 {
		t.Fatalf("lamp beside a dust line: lit=%t power=%d, want off and 0", lit, power)
	}
}
