//go:build crossfly_full

package javaspawn

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// spawnerEnv is an env for the checks of a monster spawner (EntitySpawnReason.SPAWNER).
func spawnerEnv(tx *world.Tx) *env {
	st := stateOf(tx.World())
	cfg := st.cfg.Load()
	if cfg == nil {
		cfg = defaultConfig
	}
	e := st.newEnv(tx, cfg, nil)
	if r, ok := e.rnd.(*rand.Rand); e.rnd == nil || (ok && r == nil) {
		// The world's spawning source exists only once natural spawning ran.
		e.rnd = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	}
	e.spawner = true
	return e
}

// SpawnerPlacementOK is the first half of a BaseSpawner.serverTick try for the Java type id typ at pos: the
// type's spawn box at pos must not collide with blocks (Level.noCollision) and
// SpawnPlacements.checkSpawnRules(type, level, SPAWNER, pos) must pass. False for types unknown here.
func SpawnerPlacementOK(tx *world.Tx, typ string, pos mgl64.Vec3) bool {
	t, ok := entityTypes[typ]
	if !ok {
		return false
	}
	e := spawnerEnv(tx)
	if !e.noBlockCollision(spawnAABB(t, pos[0], pos[1], pos[2]).Grow(-1e-7)) {
		return false
	}
	return e.checkSpawnRules(t, cube.PosFromVec3(pos))
}

// SpawnerMobOK is the second half: Mob.checkSpawnRules(level, SPAWNER) (the walk target value of a
// PathfinderMob) and Mob.checkSpawnObstruction of the fresh mob at pos.
func SpawnerMobOK(tx *world.Tx, typ string, pos mgl64.Vec3) bool {
	t, ok := entityTypes[typ]
	if !ok {
		return false
	}
	e := spawnerEnv(tx)
	bp := cube.PosFromVec3(pos)
	return e.mobCheckSpawnRules(t, bp) && e.checkSpawnObstruction(t, bp, spawnAABB(t, pos[0], pos[1], pos[2]).Grow(-1e-7))
}
