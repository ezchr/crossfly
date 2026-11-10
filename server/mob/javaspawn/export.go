//go:build crossfly_full

package javaspawn

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// IsValidSpawn is BlockState.isValidSpawn(level, pos, type) for the block at
// pos and the Java type id (false for unknown types): what a mob of typ
// may stand on (Blocks.java isValidSpawn overrides, else a sturdy top face
// and light emission below 14).
func IsValidSpawn(tx *world.Tx, pos cube.Pos, typ string) bool {
	t, ok := entityTypes[typ]
	return ok && isValidSpawn(tx, pos, t)
}

// IsValidEmptySpawnBlock is NaturalSpawner.isValidEmptySpawnBlock for the
// block at pos and the Java type id.
func IsValidEmptySpawnBlock(tx *world.Tx, pos cube.Pos, typ string) bool {
	t, ok := entityTypes[typ]
	return ok && isValidEmptySpawnBlock(tx, pos, t)
}

// IsSpawnPositionOk is SpawnPlacements.isSpawnPositionOk(type, level, pos).
func IsSpawnPositionOk(tx *world.Tx, pos cube.Pos, typ string) bool {
	t, ok := entityTypes[typ]
	if !ok {
		return false
	}
	e := &env{tx: tx, rng: tx.Range()}
	return e.isSpawnPositionOk(t, pos)
}

// AnyPlayerCloseEnoughForSpawning is ServerLevel.anyPlayerCloseEnoughForSpawning(pos)
// (ChunkMap): a player that is not a spectator within 128 blocks of the
// centre of pos's chunk, horizontally.
func AnyPlayerCloseEnoughForSpawning(tx *world.Tx, pos cube.Pos) bool {
	cx, cz := float64(pos[0]>>4<<4+8), float64(pos[2]>>4<<4+8)
	for ent := range tx.Players() {
		p, ok := ent.(*player.Player)
		if !ok || p.GameMode() == world.GameModeSpectator {
			continue
		}
		pp := p.Position()
		if dx, dz := cx-pp[0], cz-pp[2]; dx*dx+dz*dz < 16384 {
			return true
		}
	}
	return false
}
