//go:build crossfly_full

package spawn

import (
	"testing"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// tickMob ticks one mob n times (all stages, so the despawn stage runs) and
// returns after how many ticks it was removed, or -1.
func tickMob(tx *world.Tx, h *world.EntityHandle, n int) int {
	for i := 0; i < n; i++ {
		m, ok := mob.OfHandle(tx, h)
		if !ok || h.Closed() {
			return i
		}
		m.Tick(tx, int64(i))
	}
	if _, ok := mob.OfHandle(tx, h); !ok || h.Closed() {
		return n
	}
	return -1
}

func spawnCow(t *testing.T, tx *world.Tx, x float64) *mob.Mob {
	t.Helper()
	m, err := mob.Spawn(tx, "minecraft:cow", mgl64.Vec3{x, floorY + 1, 0.5}, mob.SpawnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !m.HasComponent("minecraft:despawn") {
		t.Fatal("cow has no despawn component")
	}
	m.SetRandSeed(1, 2)
	return m
}

func TestDespawnFarAway(t *testing.T) {
	w, _ := newWorld(t, nil)
	run(w, func(tx *world.Tx) {
		// Beyond 44 blocks (simulation distance 4): removed on its first tick.
		far := spawnCow(t, tx, 60.5)
		if got := tickMob(tx, far.H(), 5); got != 1 {
			t.Errorf("far cow removed after %d ticks, want 1", got)
		}
		// Within 32 blocks: never, however long it idles.
		near := spawnCow(t, tx, 20.5)
		if got := tickMob(tx, near.H(), 3000); got != -1 {
			t.Errorf("near cow removed after %d ticks", got)
		}
		if InactivityTicks(near) != 0 {
			t.Errorf("inactivity not reset with a player nearby: %d", InactivityTicks(near))
		}
	})
}

func TestDespawnFromInactivity(t *testing.T) {
	w, _ := newWorld(t, nil)
	run(w, func(tx *world.Tx) {
		// Between 32 and 44 blocks: only after 30 s of inactivity, then
		// with a 1/800 chance per tick.
		m := spawnCow(t, tx, 38.5)
		got := tickMob(tx, m.H(), 40000)
		if got < 600 || got == -1 {
			t.Fatalf("idle cow removed after %d ticks, want > 600", got)
		}
	})
}

func TestNameTagAndPersistentStay(t *testing.T) {
	w, _ := newWorld(t, nil)
	run(w, func(tx *world.Tx) {
		named := spawnCow(t, tx, 60.5)
		named.SetNameTag("Bessie")
		if got := tickMob(tx, named.H(), 2000); got != -1 {
			t.Errorf("name-tagged cow removed after %d ticks", got)
		}
		if !named.Persistent() {
			t.Error("name-tagged cow not made persistent")
		}
		kept := spawnCow(t, tx, 60.5)
		p, _ := newPersistent("minecraft:persistent", nil)
		p.Attach(kept)
		if got := tickMob(tx, kept.H(), 100); got != -1 {
			t.Errorf("persistent cow removed after %d ticks", got)
		}
		p.Detach(kept)
		if got := tickMob(tx, kept.H(), 5); got != 1 {
			t.Errorf("cow no longer persistent removed after %d ticks, want 1", got)
		}
	})
}

func TestDespawningOffAndNoPlayers(t *testing.T) {
	w, ph := newWorld(t, nil)
	SetDespawning(w, false)
	run(w, func(tx *world.Tx) {
		m := spawnCow(t, tx, 60.5)
		if got := tickMob(tx, m.H(), 10); got != -1 {
			t.Errorf("cow removed with despawning off")
		}
	})
	SetDespawning(w, true)
	run(w, func(tx *world.Tx) {
		p, _ := ph.Entity(tx)
		tx.RemoveEntity(p)
	})
	w.AdvanceTick()
	run(w, func(tx *world.Tx) {
		m := spawnCow(t, tx, 60.5)
		if got := tickMob(tx, m.H(), 10); got != -1 {
			t.Errorf("cow removed with no player in the world")
		}
	})
}

func TestParseDespawn(t *testing.T) {
	d, err := parseDespawn([]byte(`{"despawn_from_distance":{"max_distance":40,"min_distance":32}}`))
	if err != nil || !d.fromDistance || d.minDist != 32 || d.maxDist != 40 || !d.fromChance || !d.fromInactivity ||
		d.randomChance != 800 || d.inactivityTimer != 30 || !d.fromSimulationEdge || d.hasFilter {
		t.Fatalf("%+v %v", d, err)
	}
	d, _ = parseDespawn([]byte(`{}`))
	if d.fromDistance {
		t.Fatal("despawn_from_distance set without the key")
	}
}
