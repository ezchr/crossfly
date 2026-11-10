//go:build crossfly_full

package block

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Rails with redstone=dragonfly follow Bedrock Edition's rules (PoweredRailBlock, ActivatorRailBlock,
// BaseRailTransporter and DetectorRailBlock in BDS):
//
//   - A powered or activator rail is on while the redstone engine gives it power, or while a rail of the same kind
//     on the same line, at most 8 rails away and connected through rails of that kind, gets power. A line runs
//     straight along one axis and may go up and down slopes.
//   - When one rail of a line changes, the whole line is worked out again at once, so a line turns on and off in one
//     go, in a fixed order.
//   - A detector rail turns on when a minecart enters it (the box 0.2 inside its sides and 0.2 below its top) and
//     checks again every 20 ticks while on. Comparators read the first container minecart on it.

// dfRailReach is how many rails beyond a powered one a line of powered or activator rails carries the power.
const dfRailReach = 8

// dfRailLineLimit caps how far one change looks along a line in each direction.
const dfRailLineLimit = 64

// dfRailAxis returns the axis a straight rail shape runs along: true for east-west.
func dfRailAxis(s RailShape) (eastWest bool) {
	return s == RailEastWest || s == RailAscendingEast || s == RailAscendingWest
}

// dfRailNext returns the rail of the same kind that continues the line of r at pos in one direction, as BDS's rail
// transporters connect them: the next block along the axis, or the one below it, or, up a slope, the one above it.
func dfRailNext(kind poweredRailBlock, pos cube.Pos, shape RailShape, forward bool, tx *world.Tx) (cube.Pos, poweredRailBlock, bool) {
	x, y, z := pos.X(), pos.Y(), pos.Z()
	below := true
	switch shape {
	case RailNorthSouth:
		if forward {
			z++
		} else {
			z--
		}
	case RailEastWest:
		if forward {
			x--
		} else {
			x++
		}
	case RailAscendingEast:
		if forward {
			x--
		} else {
			x, y, below = x+1, y+1, false
		}
	case RailAscendingWest:
		if forward {
			x, y, below = x-1, y+1, false
		} else {
			x++
		}
	case RailAscendingNorth:
		if forward {
			z++
		} else {
			z, y, below = z-1, y+1, false
		}
	case RailAscendingSouth:
		if forward {
			z, y, below = z+1, y+1, false
		} else {
			z--
		}
	default:
		return cube.Pos{}, nil, false
	}
	axis := dfRailAxis(shape)
	for _, p := range [2]cube.Pos{{x, y, z}, {x, y - 1, z}} {
		if p.OutOfBounds(tx.Range()) {
			continue
		}
		if b, ok := tx.Block(p).(poweredRailBlock); ok && sameRailKind(b, kind) && dfRailAxis(b.railShape()) == axis {
			return p, b, true
		}
		if !below {
			break
		}
	}
	return cube.Pos{}, nil, false
}

// dfRailLine works out the power of the line of powered or activator rails through pos again and sets every rail of
// it that changed.
func dfRailLine(r poweredRailBlock, pos cube.Pos, tx *world.Tx) {
	type entry struct {
		pos  cube.Pos
		rail poweredRailBlock
	}
	line := []entry{{pos, r}}
	seen := map[cube.Pos]struct{}{pos: {}}
	// Walk both ways: backwards first (prepended, so the line is in order), then forwards.
	for _, forward := range [2]bool{false, true} {
		cur, curRail := pos, r
		var part []entry
		for range dfRailLineLimit {
			next, nextRail, ok := dfRailNext(r, cur, curRail.railShape(), forward, tx)
			if !ok {
				break
			}
			if _, dup := seen[next]; dup {
				break
			}
			seen[next] = struct{}{}
			part = append(part, entry{next, nextRail})
			cur, curRail = next, nextRail
		}
		if !forward {
			for i, j := 0, len(part)-1; i < j; i, j = i+1, j-1 {
				part[i], part[j] = part[j], part[i]
			}
			line = append(part, line...)
		} else {
			line = append(line, part...)
		}
	}
	// The distance of each rail to the nearest rail of the line that gets power itself.
	dist := make([]int, len(line))
	last := -1 - dfRailReach
	for i, e := range line {
		if tx.RedstonePower(e.pos) > 0 {
			last = i
		}
		dist[i] = i - last
	}
	last = len(line) + dfRailReach
	for i := len(line) - 1; i >= 0; i-- {
		if dist[i] == 0 {
			last = i
		}
		dist[i] = min(dist[i], last-i)
	}
	for i, e := range line {
		on := dist[i] <= dfRailReach
		if e.rail.railPowered() == on {
			continue
		}
		// Only the rail's state changes: rails give no power, and the line is done here as a whole.
		tx.SetBlock(e.pos, e.rail.withRailPowered(on), &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true, KeepRedstonePower: true})
	}
}

// dfCheckPressed is DetectorRailBlock::_checkPressed: the rail is on while a minecart is on it, and while on it looks
// again in 20 ticks.
func (r DetectorRail) dfCheckPressed(pos cube.Pos, tx *world.Tx) {
	cur, ok := tx.Block(pos).(DetectorRail)
	if !ok {
		return
	}
	pressed := len(detectorMinecarts(pos, tx, false)) > 0
	if pressed != cur.Powered {
		cur.Powered = pressed
		tx.SetBlock(pos, cur, nil)
	}
	if pressed {
		tx.ScheduleBlockUpdate(pos, cur, time.Second)
	}
}

// DetectorRailComparatorOutput is what a comparator reads from a detector rail with redstone=dragonfly
// (DetectorRailBlock::getComparatorSignal): how full the first container minecart on it is, or 0.
func DetectorRailComparatorOutput(pos cube.Pos, tx *world.Tx) int {
	for _, c := range detectorMinecarts(pos, tx, false) {
		if inv := c.MinecartInventory(); inv != nil {
			return containerSignal(inv)
		}
	}
	return 0
}
