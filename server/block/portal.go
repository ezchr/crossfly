package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/portal"
)

// Portal is the translucent part of the nether portal that teleports the player to and from the Nether.
type Portal struct {
	transparent

	// Axis is the axis which the portal faces.
	Axis cube.Axis
}

// portalTraveller represents an entity that can handle touching a portal block.
type portalTraveller interface {
	TravelThroughPortal(tx *world.Tx, target world.Dimension)
}

// Model ...
func (p Portal) Model() world.BlockModel {
	return model.Portal{Axis: p.Axis}
}

// Portal ...
func (Portal) Portal() world.Dimension {
	return world.Nether
}

// LightEmissionLevel returns 11.
func (Portal) LightEmissionLevel() uint8 {
	return 11
}

// HasLiquidDrops ...
func (p Portal) HasLiquidDrops() bool {
	return false
}

// EncodeBlock ...
func (p Portal) EncodeBlock() (string, map[string]any) {
	return "minecraft:portal", map[string]any{"portal_axis": p.Axis.String()}
}

// NeighbourUpdateTick ...
func (p Portal) NeighbourUpdateTick(pos, neighbour cube.Pos, tx *world.Tx) {
	face, ok := pos.NeighbourFace(neighbour)
	if !ok {
		return
	}
	axis := face.Axis()
	if axis != cube.Y && axis != p.Axis {
		return
	}
	if n, ok := portal.NetherPortalFromPos(tx, pos); ok && n.Framed() && n.Activated() {
		return
	}
	portal.DeactivateNetherPortal(tx, pos)
}

// EntityInside ...
func (p Portal) EntityInside(_ cube.Pos, tx *world.Tx, e world.Entity) {
	if t, ok := e.(portalTraveller); ok {
		t.TravelThroughPortal(tx, p.Portal())
	}
}

// PortalRandomTick, when set, runs on every random tick of a nether portal block. Java mob mode sets it to
// NetherPortalBlock.randomTick (zombified piglins spawning from portals in the Overworld). Set it only before any
// world ticks; nil (the default) does nothing.
var PortalRandomTick func(pos cube.Pos, tx *world.Tx, r *rand.Rand)

// RandomTick ...
func (p Portal) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	if f := PortalRandomTick; f != nil {
		f(pos, tx, r)
	}
}
