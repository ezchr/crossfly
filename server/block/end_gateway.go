package block

import (
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// EndGateway is the portal block that appears in the End when the ender dragon dies: it sends what touches it
// to its exit, on the outer End islands and back (EndGatewayBlockActor).
type EndGateway struct {
	transparent

	// Age is the age of the gateway in ticks. Clients show the beam of a new gateway while it is below 200.
	Age int32
	// Exit is the block the gateway sends entities above. It is unset (HasExit false) for a gateway that
	// was not linked yet.
	Exit    cube.Pos
	HasExit bool
}

// EndGatewayBeamAction is a world.BlockAction that shows the beam of an end gateway that was just used
// (EndGatewayBlockActor::triggerEvent 1).
type EndGatewayBeamAction struct{ action }

// endGatewayLinker is the function SetEndGatewayLinker set.
var endGatewayLinker func(tx *world.Tx, pos cube.Pos) (cube.Pos, bool)

// SetEndGatewayLinker sets the function that finds the exit of a gateway in the End that has none yet
// (EndDragonFight::spawnNewGatewayChunks): Crossfly Full's End fight builds the exit island. Without it an
// unlinked gateway sends nothing anywhere. Call it once, from an init function.
func SetEndGatewayLinker(f func(tx *world.Tx, pos cube.Pos) (cube.Pos, bool)) { endGatewayLinker = f }

// endGatewayCooldowns holds the tick until which a used gateway rests, per world and position
// (EndGatewayBlockActor teleport cooldown).
var endGatewayCooldowns sync.Map

type endGatewayKey struct {
	w   *world.World
	pos cube.Pos
}

// Model ...
func (EndGateway) Model() world.BlockModel {
	return model.Empty{}
}

// LightEmissionLevel returns 15.
func (EndGateway) LightEmissionLevel() uint8 {
	return 15
}

// HasLiquidDrops ...
func (EndGateway) HasLiquidDrops() bool {
	return false
}

// EntityInside teleports the entity to the gateway's exit, like EndGatewayBlockActor::teleportEntity: only in
// the End, once the gateway has rested since its last use (100 ticks, 20 after a teleport), to the block
// above the exit. Thrown ender pearls and riders stay.
func (g EndGateway) EntityInside(pos cube.Pos, tx *world.Tx, e world.Entity) {
	if tx.World().Dimension() != world.End || e.H().Type().EncodeEntity() == "minecraft:ender_pearl" {
		return
	}
	if r, ok := e.(interface{ RidingEntityHandle() *world.EntityHandle }); ok && r.RidingEntityHandle() != nil {
		return
	}
	key := endGatewayKey{w: tx.World(), pos: pos}
	now := tx.CurrentTick()
	if until, ok := endGatewayCooldowns.Load(key); ok && now < until.(int64) {
		return
	}
	endGatewayCooldowns.Store(key, now+100)
	if !g.HasExit {
		if endGatewayLinker == nil {
			return
		}
		exit, ok := endGatewayLinker(tx, pos)
		if !ok {
			return
		}
		g.Exit, g.HasExit = exit, true
		tx.SetBlock(pos, g, nil)
	}
	t, ok := e.(interface{ Teleport(mgl64.Vec3) })
	if !ok {
		return
	}
	t.Teleport(mgl64.Vec3{float64(g.Exit[0]), float64(g.Exit[1] + 1), float64(g.Exit[2])})
	endGatewayCooldowns.Store(key, now+20)
	for _, v := range tx.Viewers(pos.Vec3Centre()) {
		v.ViewBlockAction(pos, EndGatewayBeamAction{})
	}
}

// EncodeNBT encodes the End gateway block actor.
func (g EndGateway) EncodeNBT() map[string]any {
	m := map[string]any{"id": "EndGateway", "Age": g.Age}
	if g.HasExit {
		// A list of ints (ListTag), as BDS writes it.
		m["ExitPortal"] = []any{int32(g.Exit[0]), int32(g.Exit[1]), int32(g.Exit[2])}
	}
	return m
}

// DecodeNBT decodes the End gateway block actor.
func (g EndGateway) DecodeNBT(data map[string]any) any {
	g.Age = nbtconv.Int32(data, "Age")
	g.HasExit = false
	switch exit := data["ExitPortal"].(type) {
	case []int32:
		if len(exit) == 3 {
			g.Exit, g.HasExit = cube.Pos{int(exit[0]), int(exit[1]), int(exit[2])}, true
		}
	case []any:
		if len(exit) == 3 {
			x, okX := exit[0].(int32)
			y, okY := exit[1].(int32)
			z, okZ := exit[2].(int32)
			if okX && okY && okZ {
				g.Exit, g.HasExit = cube.Pos{int(x), int(y), int(z)}, true
			}
		}
	}
	return g
}

// EncodeBlock ...
func (EndGateway) EncodeBlock() (string, map[string]any) {
	return "minecraft:end_gateway", nil
}

// Hash ...
func (EndGateway) Hash() (uint64, uint64) {
	return hashEndGateway, 0
}
