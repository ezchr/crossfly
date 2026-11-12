//go:build crossfly_full

package spawn

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
)

// The server registers the mob blocks (the spawner among them) before the registry is finalised.
func init() { block.RegisterMobBlocks() }

// spawnerTicks places sp at pos and runs its Tick n times, on consecutive ticks starting after the world's
// current tick. It returns the mobs of id in the world afterwards.
func spawnerTicks(t *testing.T, w *world.World, pos cube.Pos, sp block.MobSpawner, n int, id string) []*world.EntityHandle {
	t.Helper()
	var out []*world.EntityHandle
	run(w, func(tx *world.Tx) {
		tx.SetBlock(pos, sp, nil)
		start := tx.CurrentTick() + 1
		for i := range int64(n) {
			s, ok := tx.Block(pos).(block.MobSpawner)
			if !ok {
				t.Fatalf("no spawner at %v: %v", pos, tx.Block(pos))
			}
			s.Tick(start+i, pos, tx)
		}
		for e := range tx.Entities() {
			if m, ok := mob.Of(e); ok && m.Identifier() == id {
				out = append(out, m.H())
			}
		}
	})
	return out
}

// fastSpawner is a spawner of id with BDS's defaults but no delay at all, so every tick spawns.
func fastSpawner(id string) block.MobSpawner {
	s := block.NewMobSpawner(id)
	s.Delay, s.MinSpawnDelay, s.MaxSpawnDelay = 0, 0, 0
	return s
}

// TestSpawnerSpawnsUpToMaxNearby: a cow spawner next to a player spawns cows around it, natural ones (they
// despawn), each within SpawnRange (taxicab) of it, and stops at MaxNearbyEntities (6).
func TestSpawnerSpawnsUpToMaxNearby(t *testing.T) {
	w, _ := newWorld(t, nil)
	pos := cube.Pos{3, floorY + 2, 3}
	cows := spawnerTicks(t, w, pos, fastSpawner("minecraft:cow"), 40, "minecraft:cow")
	if len(cows) != 6 {
		t.Fatalf("%d cows after 40 spawner ticks, want 6 (MaxNearbyEntities)", len(cows))
	}
	run(w, func(tx *world.Tx) {
		for _, h := range cows {
			e, ok := h.Entity(tx)
			if !ok {
				t.Fatal("cow gone")
			}
			m, _ := mob.Of(e)
			if !NaturallySpawned(m) {
				t.Error("a spawner cow is not naturally spawned (BDS spawnMob(naturalSpawn = true))")
			}
			off := cube.PosFromVec3(m.Position().Sub(pos.Vec3()))
			if abs(off[0])+abs(off[1])+abs(off[2]) > 4 {
				t.Errorf("cow at %v is more than SpawnRange from the spawner at %v", m.Position(), pos)
			}
		}
	})
}

// TestSpawnerDelay: after spawning the spawner picks a delay in MinSpawnDelay..MaxSpawnDelay, stores it in the
// block (so clients and saves get it) and spawns nothing until it ran out.
func TestSpawnerDelay(t *testing.T) {
	w, _ := newWorld(t, nil)
	pos := cube.Pos{3, floorY + 2, 3}
	s := block.NewMobSpawner("minecraft:cow")
	s.Delay = 0
	first := len(spawnerTicks(t, w, pos, s, 1, "minecraft:cow"))
	if first == 0 || first > 4 {
		t.Fatalf("%d cows on the first spawn, want 1-4 (SpawnCount)", first)
	}
	run(w, func(tx *world.Tx) {
		s = tx.Block(pos).(block.MobSpawner)
		if s.Delay < 200 || s.Delay >= 800 {
			t.Errorf("delay after a spawn = %d, want 200-799", s.Delay)
		}
	})
	// Starting again from the stored delay: 150 ticks are too few.
	if n := len(spawnerTicks(t, w, pos, s, 150, "minecraft:cow")); n != first {
		t.Errorf("the spawner spawned during its delay: %d -> %d cows", first, n)
	}
	if n := len(spawnerTicks(t, w, pos, s, int(s.Delay)+1, "minecraft:cow")); n == first {
		t.Errorf("no spawn after the delay of %d ticks ran out", s.Delay)
	}
}

// TestSpawnerNeedsPlayer: without a player within RequiredPlayerRange (16) the spawner does nothing.
func TestSpawnerNeedsPlayer(t *testing.T) {
	w, _ := newWorld(t, nil)
	pos := cube.Pos{20, floorY + 2, 0} // the player is at the origin
	if n := len(spawnerTicks(t, w, pos, fastSpawner("minecraft:cow"), 20, "minecraft:cow")); n != 0 {
		t.Errorf("%d cows spawned with the player 20 blocks away", n)
	}
	s := fastSpawner("minecraft:cow")
	s.RequiredPlayerRange = 24
	if n := len(spawnerTicks(t, w, pos, s, 5, "minecraft:cow")); n == 0 {
		t.Error("no cows with RequiredPlayerRange 24 and the player 20 blocks away")
	}
}

// TestSpawnerMonsterDarkness: zombies need Monster::isDarkEnoughToSpawn, BDS has no spawner exception: none in
// daylight, some under a roof.
func TestSpawnerMonsterDarkness(t *testing.T) {
	w, _ := newWorld(t, nil)
	w.SetTime(6000)
	pos := cube.Pos{3, floorY + 2, 3}
	if n := len(spawnerTicks(t, w, pos, fastSpawner("minecraft:zombie"), 20, "minecraft:zombie")); n != 0 {
		t.Errorf("%d zombies spawned at noon in the open", n)
	}
	dark, _ := newWorldRoof(t, nil, true)
	dark.SetTime(6000)
	if n := len(spawnerTicks(t, dark, pos, fastSpawner("minecraft:zombie"), 20, "minecraft:zombie")); n == 0 {
		t.Error("no zombies spawned under a roof")
	}
}

// TestSpawnerUnknownMob: a spawner of no registered mob, or of none, does nothing (and does not crash).
func TestSpawnerUnknownMob(t *testing.T) {
	w, _ := newWorld(t, nil)
	pos := cube.Pos{3, floorY + 2, 3}
	for _, id := range []string{"", "minecraft:no_such_mob"} {
		spawnerTicks(t, w, pos, fastSpawner(id), 5, id)
		run(w, func(tx *world.Tx) {
			for e := range tx.Entities() {
				if _, ok := mob.Of(e); ok {
					t.Errorf("spawner of %q spawned %v", id, e.H().Type().EncodeEntity())
				}
			}
		})
	}
}
