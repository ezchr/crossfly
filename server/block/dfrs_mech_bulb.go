//go:build crossfly_full

package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// Copper bulbs with redstone=dragonfly follow Bedrock Edition's rules (CopperBulbBlock in BDS): when the power the
// redstone engine gives a bulb no longer matches its powered state, it looks again a tick later. If the power then
// still differs it takes it over, and when the power has just turned on, the bulb toggles its light. A bulb gives
// light 15, 12, 8 or 4 by how far it has oxidised, and comparators read 15 from it while it is lit.

// dfPowerUpdate schedules the bulb to look at its power when it no longer matches its powered state.
func (b CopperBulb) dfPowerUpdate(pos cube.Pos, tx *world.Tx, power int) {
	cur, ok := tx.Block(pos).(CopperBulb)
	if !ok || (power > 0) == cur.Powered {
		return
	}
	tx.ScheduleBlockUpdate(pos, cur, time.Second/20)
}

// ScheduledTick takes over the power the bulb gets with redstone=dragonfly, toggling the light when it turns on.
func (b CopperBulb) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if world.JavaRedstone() {
		return
	}
	powered := tx.RedstonePower(pos) > 0
	if powered == b.Powered {
		return
	}
	b.Powered = powered
	if powered {
		b.Lit = !b.Lit
		if b.Lit {
			tx.PlaySound(pos.Vec3Centre(), sound.CopperBulbTurnOn{})
		} else {
			tx.PlaySound(pos.Vec3Centre(), sound.CopperBulbTurnOff{})
		}
	}
	// Only the bulb changes: the power the engine holds for it stays, so it does not look again for this change.
	tx.SetBlock(pos, b, &world.SetOpts{KeepRedstonePower: true})
}

// CopperBulbComparatorOutput is what a comparator reads from a copper bulb (CopperBulbBlock::getComparatorSignal): 15
// while it is lit.
func CopperBulbComparatorOutput(b CopperBulb) int {
	if b.Lit {
		return 15
	}
	return 0
}
