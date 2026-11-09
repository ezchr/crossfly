//go:build crossfly_full

package locomotion_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/goal/locomotion"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// keepGoals removes every goal of m but the ones named.
func keepGoals(m *mob.Mob, keys ...string) {
	for _, k := range m.Goals().Keys() {
		keep := false
		for _, want := range keys {
			if k == want {
				keep = true
			}
		}
		if !keep {
			m.Goals().Remove(k)
		}
	}
}

// targetPlayer makes m target the (only) player in the world.
func targetPlayer(m *mob.Mob) {
	for e := range m.Tx().Entities() {
		if p, ok := e.(*player.Player); ok {
			m.SetTarget(p)
		}
	}
}

// TestDolphinBreaches: a dolphin with only random_breach left jumps out of
// the water.
func TestDolphinBreaches(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	pool(w, 8, 5)
	d := w.Spawn("minecraft:dolphin", mgl64.Vec3{0.5, 3, 0.5}, mob.SpawnOptions{Event: "minecraft:entity_spawned"})
	w.Tick(1)
	d.With(func(m *mob.Mob) { keepGoals(m, "minecraft:behavior.random_breach") })
	n, ok := w.TickUntil(2000, func(tx *world.Tx) bool { return d.PosTx(tx)[1] > 6.3 })
	if !ok {
		t.Fatalf("dolphin did not breach in %d ticks:\n%v", n, w.Dump(d))
	}
}

// TestDolphinLeadsToTreasure: a bribed dolphin swims towards the treasure
// TreasureLocator reports.
func TestDolphinLeadsToTreasure(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Fill(tx, cube.Pos{-6, 0, -4}, cube.Pos{70, 7, 4}, block.Stone{})
		w.Clear(tx, cube.Pos{-5, 1, -3}, cube.Pos{69, 7, 3})
		w.Water(tx, cube.Pos{-5, 1, -3}, cube.Pos{69, 5, 3})
	})
	locomotion.TreasureLocator = func(*world.Tx, cube.Pos) (cube.Pos, bool) { return cube.Pos{68, 2, 0}, true }
	defer func() { locomotion.TreasureLocator = nil }()
	d := w.Spawn("minecraft:dolphin", mgl64.Vec3{0.5, 3, 0.5}, mob.SpawnOptions{Event: "minecraft:entity_spawned"})
	w.Tick(1)
	d.With(func(m *mob.Mob) {
		if m.Baby() {
			t.Skip("spawned a baby dolphin (no bribeable)")
		}
		keepGoals(m, "minecraft:behavior.find_underwater_treasure")
	})
	p := w.AddPlayer("feeder", mgl64.Vec3{0.5, 3, 2.5})
	if _, ok := p.Interact(d, item.NewStack(item.Cod{}, 1)); !ok {
		t.Fatal("dolphin did not take the cod")
	}
	n, ok := w.TickUntil(600, func(tx *world.Tx) bool { return d.PosTx(tx)[0] > 15 })
	if !ok {
		t.Fatalf("bribed dolphin did not head for the treasure in %d ticks:\n%v", n, w.Dump(d))
	}
}

// TestSquidFleesAttacker: a squid hit by a player swims away from them.
func TestSquidFleesAttacker(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	pool(w, 12, 6)
	s := w.Spawn("minecraft:squid", mgl64.Vec3{0.5, 3, 0.5})
	p := w.AddPlayer("attacker", mgl64.Vec3{-3.5, 3, 0.5})
	w.Tick(2)
	w.Do(func(tx *world.Tx) {
		m, _ := s.Mob(tx)
		for e := range tx.Entities() {
			if pl, ok := e.(*player.Player); ok {
				m.Hurt(1, entity.AttackDamageSource{Attacker: pl})
			}
		}
	})
	start := w.Dist(s, p)
	w.Tick(60)
	if d := w.Dist(s, p); d < start+1 {
		t.Errorf("squid fled from %.1f to %.1f blocks:\n%v", start, d, w.Dump(s))
	}
}

// TestHappyGhastFollowsSnowball: a happy ghast flies towards a player holding
// a snowball and stops at stop_distance.
func TestHappyGhastFollowsSnowball(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{Time: 6000})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 30, block.Stone{}) })
	g := w.Spawn("minecraft:happy_ghast", mgl64.Vec3{0.5, 4, 0.5})
	p := w.AddPlayer("tempter", mgl64.Vec3{14.5, 1, 0.5})
	p.Hold(item.NewStack(item.Snowball{}, 1))
	start := w.Dist(g, p)
	w.Tick(300)
	if d := w.Dist(g, p); d > start-3 {
		t.Errorf("happy ghast did not come closer: %.1f -> %.1f:\n%v", start, d, w.Dump(g))
	}
}

// TestNautilusCharges: a nautilus dashes at its target in the water.
func TestNautilusCharges(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	pool(w, 10, 6)
	n := w.Spawn("minecraft:nautilus", mgl64.Vec3{-5.5, 3, 0.5})
	p := w.AddPlayer("target", mgl64.Vec3{3.5, 3, 0.5})
	w.Tick(1)
	n.With(func(m *mob.Mob) {
		keepGoals(m, "minecraft:behavior.aquatic_charge_attack")
		targetPlayer(m)
	})
	if k, ok := w.TickUntil(400, func(tx *world.Tx) bool { return p.HealthTx(tx) < 20 }); !ok {
		t.Fatalf("nautilus did not hit its target in %d ticks:\n%v", k, w.Dump(n))
	}
}
