//go:build crossfly_full

package spawn

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func init() { block.SetSpawnerHost(SpawnerHost{}) }

// SpawnerHost spawns the Bedrock mobs of monster spawners (block.MobSpawner) as BDS's BaseMobSpawner::tick
// and Spawner::spawnMob do. The package installs it.
type SpawnerHost struct{}

// SpawnerMob reports whether id is a registered mob type.
func (SpawnerHost) SpawnerMob(id string) bool {
	_, ok := mob.TypeFor(id)
	return ok
}

// SpawnerTry is one try of BaseMobSpawner::tick: the block at the spot must not block motion and the spot
// must be within SpawnRange blocks (taxicab) of the spawner. Fewer than MaxNearbyEntities mobs of the type may
// be in the box around the spawner (2*SpawnRange blocks either way horizontally, 4 vertically), else the spawner
// is full. Spawner::spawnMob then spawns a natural mob, if Mob::checkSpawnRules(true) passes: the mob's box
// collides with no block, it does not stand on magma unless fire immune, and a monster needs
// Monster::isDarkEnoughToSpawn (the same as for natural spawns: BDS has no spawner exception).
func (SpawnerHost) SpawnerTry(tx *world.Tx, pos cube.Pos, s block.MobSpawner, at mgl64.Vec3) (world.Entity, bool) {
	id := s.EntityID
	t, ok := mob.TypeFor(id)
	if !ok {
		return nil, false
	}
	bp := cube.PosFromVec3(at)
	if bp.OutOfBounds(tx.Range()) || motionBlocking(tx, bp) {
		return nil, false
	}
	off := cube.PosFromVec3(at.Sub(pos.Vec3()))
	if abs(off[0])+abs(off[1])+abs(off[2]) > int(s.SpawnRange) {
		return nil, false
	}
	r := 2 * float64(s.SpawnRange)
	c := pos.Vec3()
	box := cube.Box(c[0]-r, c[1]-4, c[2]-r, c[0]+r, c[1]+4, c[2]+r)
	n := 0
	for e := range tx.EntitiesWithin(box) {
		if e.H().Type().EncodeEntity() == id {
			n++
		}
	}
	if n >= int(s.MaxNearbyEntities) {
		return nil, true
	}
	if !checkSpawnRules(tx, t, at, bp.Side(cube.FaceDown)) {
		return nil, false
	}
	if monsterSpawnTypes[id] {
		w := tx.World()
		rain, thunder := tx.Raining(), tx.Thundering()
		e := &env{tx: tx, skyDarken: skyDarken(w.Time(), rain, thunder), thunder: rain && thunder, rng: tx.Range()}
		if !monsterDarkEnough(e, bp) {
			return nil, false
		}
	}
	m, err := mob.Spawn(tx, id, at, mob.SpawnOptions{Configure: func(m *mob.Mob) {
		in := infoOf(m)
		in.surface, in.natural = true, true
	}})
	if err != nil {
		return nil, false
	}
	return m, false
}

// motionBlocking is Block::isMotionBlockingBlock as far as the spawner needs it: a block with a collision box.
func motionBlocking(tx *world.Tx, pos cube.Pos) bool {
	b := tx.Block(pos)
	return len(b.Model().BBox(pos, tx)) > 0
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
