//go:build !crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Crossfly Core has no tripwire hooks: the wire's redstone=dragonfly hooks (dfrs_diode_sensors.go) do nothing.

func (String) dfNeighbourUpdate(cube.Pos, cube.Pos, *world.Tx) {}
func (String) dfPlaced(cube.Pos, *world.Tx)                    {}
