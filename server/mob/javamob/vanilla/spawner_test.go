//go:build crossfly_full

package vanilla_test

import (
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestMain(m *testing.M) {
	if err := mobtest.RegisterTypes(); err != nil {
		panic(err)
	}
	vanilla.Install()
	os.Exit(m.Run())
}

// tickSpawner places sp at pos and runs its Tick n times on consecutive ticks, then returns how many Java mobs
// of id are in the world.
func tickSpawner(t *testing.T, w *mobtest.World, pos cube.Pos, sp block.MobSpawner, n int, id string) int {
	t.Helper()
	count := 0
	w.Do(func(tx *world.Tx) {
		tx.SetBlock(pos, sp, nil)
		start := tx.CurrentTick() + 1
		for i := range int64(n) {
			tx.Block(pos).(block.MobSpawner).Tick(start+i, pos, tx)
		}
		for e := range tx.Entities() {
			if _, ok := javamob.Of(e); ok && e.H().Type().EncodeEntity() == id {
				count++
			}
		}
	})
	return count
}

func fast(id string) block.MobSpawner {
	s := block.NewMobSpawner(id)
	s.Delay, s.MinSpawnDelay, s.MaxSpawnDelay = 0, 0, 0
	return s
}

// TestJavaSpawnerPigs: in Java mob mode a pig spawner spawns Java pigs (BaseSpawner with Java's spawn rules)
// and stops at MaxNearbyEntities.
func TestJavaSpawnerPigs(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	w.Do(func(tx *world.Tx) { w.Grass(tx, 0, 12) })
	w.AddPlayer("tester", mgl64.Vec3{0.5, 1, 0.5})
	if n := tickSpawner(t, w, cube.Pos{3, 2, 3}, fast("minecraft:pig"), 40, "minecraft:pig"); n != 6 {
		t.Fatalf("%d Java pigs after 40 spawner ticks, want 6", n)
	}
}

// TestJavaSpawnerZombiesNeedDark: Java zombies keep Monster.checkMonsterSpawnRules from a spawner (dark
// enough); only the floor check is skipped: none at noon in the open.
func TestJavaSpawnerZombiesNeedDark(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 12, block.Stone{}) })
	w.AddPlayer("tester", mgl64.Vec3{0.5, 1, 0.5})
	if n := tickSpawner(t, w, cube.Pos{3, 2, 3}, fast("minecraft:zombie"), 20, "minecraft:zombie"); n != 0 {
		t.Errorf("%d zombies at noon in the open", n)
	}
}
