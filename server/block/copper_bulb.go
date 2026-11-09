//go:build crossfly_full

package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// CopperBulb is a light that toggles each time it starts to be powered. Comparators read 15 from it while it is
// lit. Its light dims as it oxidises. It follows Java Edition's CopperBulbBlock.
type CopperBulb struct {
	solid
	bassDrum

	// Oxidation is the level of oxidation of the bulb.
	Oxidation OxidationType
	// Waxed bulbs do not oxidise.
	Waxed bool
	// Lit is true while the bulb gives light.
	Lit bool
	// Powered is true while the bulb is powered: it toggles only when this turns on.
	Powered bool
}

var _ world.RedstonePowerContextAction = CopperBulb{}

// LightEmissionLevel ...
func (b CopperBulb) LightEmissionLevel() uint8 {
	if !b.Lit {
		return 0
	}
	switch b.Oxidation {
	case ExposedOxidation():
		return 12
	case WeatheredOxidation():
		return 8
	case OxidisedOxidation():
		return 4
	}
	return 15
}

// BlockPlaced is Java Edition's onPlace: a bulb placed into power turns on.
func (b CopperBulb) BlockPlaced(pos cube.Pos, previous world.Block, tx *world.Tx) {
	if _, same := previous.(CopperBulb); !same {
		b.checkAndFlip(pos, tx)
	}
}

// NeighbourUpdateTick ...
func (b CopperBulb) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	b.checkAndFlip(pos, tx)
}

// RedstonePowerActionUpdate ...
func (b CopperBulb) RedstonePowerActionUpdate(pos cube.Pos, tx *world.Tx, _ world.RedstoneUpdate) {
	b.checkAndFlip(pos, tx)
}

// checkAndFlip is Java Edition's checkAndFlip: when the power switches on, the light toggles.
func (b CopperBulb) checkAndFlip(pos cube.Pos, tx *world.Tx) {
	cur, ok := tx.Block(pos).(CopperBulb)
	if !ok {
		return
	}
	b = cur
	signal := railNeighbourSignal(tx, pos)
	if signal == b.Powered {
		return
	}
	if !b.Powered {
		b.Lit = !b.Lit
		if b.Lit {
			tx.PlaySound(pos.Vec3Centre(), sound.PowerOn{})
		} else {
			tx.PlaySound(pos.Vec3Centre(), sound.PowerOff{})
		}
	}
	b.Powered = signal
	tx.SetBlock(pos, b, nil)
}

// Wax ...
func (b CopperBulb) Wax(cube.Pos, mgl64.Vec3) (world.Block, bool) {
	if b.Waxed {
		return b, false
	}
	b.Waxed = true
	return b, true
}

// Strip ...
func (b CopperBulb) Strip() (world.Block, world.Sound, bool) {
	if b.Waxed {
		b.Waxed = false
		return b, sound.WaxRemoved{}, true
	} else if o, ok := b.Oxidation.Decrease(); ok {
		b.Oxidation = o
		return b, sound.CopperScraped{}, true
	}
	return b, nil, false
}

// CanOxidate ...
func (b CopperBulb) CanOxidate() bool { return !b.Waxed }

// OxidationLevel ...
func (b CopperBulb) OxidationLevel() OxidationType { return b.Oxidation }

// WithOxidationLevel ...
func (b CopperBulb) WithOxidationLevel(o OxidationType) Oxidisable {
	b.Oxidation = o
	return b
}

// RandomTick ...
func (b CopperBulb) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	attemptOxidation(pos, tx, r, b)
}

// BreakInfo ...
func (b CopperBulb) BreakInfo() BreakInfo {
	return newBreakInfo(3, pickaxeHarvestable, pickaxeEffective, oneOf(CopperBulb{Oxidation: b.Oxidation, Waxed: b.Waxed})).withBlastResistance(6)
}

// EncodeItem ...
func (b CopperBulb) EncodeItem() (name string, meta int16) {
	return copperBlockName("copper_bulb", b.Oxidation, b.Waxed), 0
}

// EncodeBlock ...
func (b CopperBulb) EncodeBlock() (string, map[string]any) {
	return copperBlockName("copper_bulb", b.Oxidation, b.Waxed), map[string]any{"lit": boolByte(b.Lit), "powered_bit": boolByte(b.Powered)}
}

// allCopperBulbs ...
func allCopperBulbs() (bulbs []world.Block) {
	for _, waxed := range []bool{false, true} {
		for _, o := range OxidationTypes() {
			for _, lit := range []bool{false, true} {
				for _, powered := range []bool{false, true} {
					bulbs = append(bulbs, CopperBulb{Oxidation: o, Waxed: waxed, Lit: lit, Powered: powered})
				}
			}
		}
	}
	return
}
