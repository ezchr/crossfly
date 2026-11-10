//go:build crossfly_full

package block

import (
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

var registerJavaRedstone, registerMobBlocks sync.Once

// mobBlocks holds the functions that register the blocks of Crossfly Full's mobs, appended to from the init
// functions of the files that define them.
var mobBlocks []func()

// RegisterMobBlocks registers the blocks of Crossfly Full's mobs: the blocks that only work with mobs, such
// as eggs that hatch. Call it before the server is configured, with item.RegisterMobItems.
func RegisterMobBlocks() {
	registerMobBlocks.Do(func() {
		for _, f := range mobBlocks {
			f()
		}
	})
}

// RegisterJavaRedstone registers the redstone components of Crossfly Full (pistons, observers, repeaters,
// comparators, buttons, pressure plates, dispensers, droppers, rails, lamps, targets, daylight detectors, tripwire
// hooks, copper bulbs, iron doors, trapped chests, coral fans) and the redstone states of lecterns. Call it once Java
// redstone is on (world.SetJavaRedstone), before the server is configured: the block registry is finalised then.
// Without it these blocks stay unknown blocks, kept as they are in worlds that have them.
func RegisterJavaRedstone() {
	registerJavaRedstone.Do(func() {
		registerAll(allButtons())
		registerAll(allCoralFans())
		registerAll(allPressurePlates())
		registerAll(allRedstoneLamps())
		registerAll(allRedstoneRepeaters())
		registerAll(allRedstoneComparators())
		registerAll(allTripwireHooks())
		registerAll(allRails())
		registerAll(allObservers())
		registerAll(allPistons())
		registerAll(allPistonHeads())
		world.RegisterBlock(MovingPiston{})
		world.RegisterBlock(Target{})
		registerAll(allDaylightDetectors())
		registerAll(allDispensers())
		registerAll(allIronDoors())
		registerAll(allCopperBulbs())
		registerAll(allBubbleColumns())
		for _, d := range cube.Directions() {
			world.RegisterBlock(Chest{Facing: d, Trapped: true})
			world.RegisterBlock(Lectern{Facing: d, Powered: true})
		}

		world.RegisterItem(IronDoor{})
		for _, t := range ButtonTypes() {
			world.RegisterItem(Button{Type: t})
		}
		world.RegisterItem(Chest{Trapped: true})
		world.RegisterItem(Dispenser{})
		world.RegisterItem(Dispenser{Dropper: true})
		for _, t := range PressurePlateTypes() {
			world.RegisterItem(PressurePlate{Type: t})
		}
		world.RegisterItem(RedstoneLamp{})
		world.RegisterItem(RedstoneRepeater{})
		world.RegisterItem(RedstoneComparator{})
		world.RegisterItem(TripwireHook{})
		world.RegisterItem(Rail{})
		world.RegisterItem(PoweredRail{})
		world.RegisterItem(ActivatorRail{})
		world.RegisterItem(DetectorRail{})
		world.RegisterItem(Observer{})
		world.RegisterItem(Target{})
		world.RegisterItem(DaylightDetector{})
		world.RegisterItem(Piston{})
		world.RegisterItem(Piston{Sticky: true})
		for _, t := range CoralTypes() {
			world.RegisterItem(CoralFan{Type: t})
			world.RegisterItem(CoralFan{Type: t, Dead: true})
		}
		for _, o := range OxidationTypes() {
			world.RegisterItem(CopperBulb{Oxidation: o})
			world.RegisterItem(CopperBulb{Oxidation: o, Waxed: true})
		}
	})
}
