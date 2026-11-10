//go:build !crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Crossfly Core keeps Dragonfly's own hopper and gives no lectern pulse: the Bedrock rules of redstone=dragonfly
// (dfrs_mech_*.go) are part of Crossfly Full.

func (Hopper) dfTick(int64, cube.Pos, *world.Tx) bool { return false }
func lecternPulses() bool                             { return false }
