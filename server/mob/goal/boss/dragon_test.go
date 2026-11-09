//go:build crossfly_full

package boss_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	bc "github.com/df-mc/dragonfly/server/mob/component/boss"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func spawnDragon(t *testing.T, w *mobtest.World, pos mgl64.Vec3) *mobtest.Ref {
	t.Helper()
	return w.Spawn(bc.DragonID, pos)
}

// TestDragonNodes: 24 path nodes on rings of 60, 40 and 20 at least 10
// above the sea, joined so that A* finds a path along the ring.
func TestDragonNodes(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:boss")
	r := spawnDragon(t, w, mgl64.Vec3{0.5, 80, 0.5})
	with(t, w, r, func(tx *world.Tx, m *mob.Mob) {
		d := bc.DragonOf(m)
		d.NumCrystals = 0
		for i, want := range map[int]float64{0: 60, 5: 60, 12: 40, 19: 40, 20: 20, 23: 20} {
			n := d.Node(tx, i)
			if r := math.Hypot(float64(n[0]), float64(n[2])); math.Abs(r-want) > 1.5 {
				t.Errorf("node %d at radius %.1f, want %v", i, r, want)
			}
			if n[1] < 73 || n[1] > 128 {
				t.Errorf("node %d at y %d", i, n[1])
			}
		}
		p := d.FindPath(tx, 12, 16, nil)
		if p == nil || len(p.Nodes) < 2 || p.Nodes[len(p.Nodes)-1] != d.Node(tx, 16) {
			t.Fatalf("path 12->16: %+v", p)
		}
		if got := d.ClosestNode(tx, mgl64.Vec3{40, 75, 0}); got != 12 {
			t.Errorf("closest node to (40, 75, 0) = %d, want 12", got)
		}
		cw := false
		if j := d.RingNext(12, false, &cw); j < 12 || j > 19 {
			t.Errorf("ring next %d off the middle ring", j)
		}
	})
}

// TestDragonFlies: without players the dragon circles: it moves on its own
// (no gravity), keeps a target position and its holding pattern.
func TestDragonFlies(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:boss", "minecraft:behavior.dragonholdingpattern")
	r := spawnDragon(t, w, mgl64.Vec3{0.5, 80, 0.5})
	start := r.Pos()
	w.Tick(200)
	with(t, w, r, func(tx *world.Tx, m *mob.Mob) {
		if !m.Goals().Running("minecraft:behavior.dragonholdingpattern") {
			t.Errorf("holding pattern not running: %v", m.Goals().RunningGoals())
		}
		if bc.DragonOf(m).TargetPos == (mgl64.Vec3{}) {
			t.Errorf("no target position")
		}
		if m.Position()[1] < 40 {
			t.Errorf("the dragon fell to y %v", m.Position()[1])
		}
	})
	if d := r.Pos().Sub(start).Len(); d < 5 {
		t.Fatalf("dragon moved %.1f blocks in 200 ticks", d)
	}
}

// TestDragonLandsAndPerches: on start_land the dragon flies to the podium
// and sits there; a player near it is scanned, then breathed on.
func TestDragonLandsAndPerches(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:boss", "minecraft:behavior.dragonlanding", "minecraft:behavior.dragonflaming")
	r := spawnDragon(t, w, mgl64.Vec3{0.5, 60, 30.5})
	w.Tick(2)
	with(t, w, r, func(tx *world.Tx, m *mob.Mob) { m.Fire("minecraft:start_land", nil) })
	n, ok := w.TickUntil(3000, func(tx *world.Tx) bool {
		m, ok := r.Mob(tx)
		return ok && bc.Sitting(m)
	})
	t.Logf("perched after %d ticks at %v", n, r.Pos())
	if !ok {
		t.Fatalf("dragon never perched:\n%v", w.Dump(r))
	}
	with(t, w, r, func(tx *world.Tx, m *mob.Mob) {
		if d := math.Hypot(m.Position()[0], m.Position()[2]); d > 3 {
			t.Errorf("perched %.1f blocks from the podium", d)
		}
	})
	w.AddPlayer("near", mgl64.Vec3{6.5, 1, 0.5})
	n, ok = w.TickUntil(400, func(tx *world.Tx) bool {
		for e := range tx.Entities() {
			if e.H().Type().EncodeEntity() == "minecraft:area_effect_cloud" {
				return true
			}
		}
		return false
	})
	if !ok {
		t.Fatalf("no dragon breath:\n%v", w.Dump(r))
	}
}

// TestDragonStrafeFireball: a flying dragon with a target strafes it and
// shoots one dragon fireball.
func TestDragonStrafeFireball(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:boss", "minecraft:behavior.dragonstrafeplayer", "minecraft:shooter")
	p := w.AddPlayer("target", mgl64.Vec3{0.5, 1, 0.5})
	r := spawnDragon(t, w, mgl64.Vec3{40.5, 60, 0.5})
	w.Tick(2)
	w.Do(func(tx *world.Tx) {
		m := mobOf(t, tx, r)
		pl, _ := p.Entity(tx)
		m.SetTarget(pl)
	})
	n, ok := w.TickUntil(2000, func(tx *world.Tx) bool {
		for e := range tx.Entities() {
			if e.H().Type() == bc.DragonFireballType {
				t.Logf("fireball after %d ticks at %v, dragon at %v", w.Ticks(), e.Position(), r.PosTx(tx))
				return true
			}
		}
		return false
	})
	_ = n
	if !ok {
		t.Fatalf("no fireball:\n%v", w.Dump(r))
	}
}

// TestDragonDamage: only players and explosions hurt the dragon; body hits
// deal min(d,1)+d/4; a lethal hit leaves 1 health and starts the death,
// which ends in rising, experience and removal.
func TestDragonDamage(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:boss", "minecraft:behavior.dragondeath")
	p := w.AddPlayer("hunter", mgl64.Vec3{0.5, 1, 0.5})
	r := spawnDragon(t, w, mgl64.Vec3{0.5, 20, 0.5})
	w.Tick(2)
	w.Do(func(tx *world.Tx) {
		m := mobOf(t, tx, r)
		pl, _ := p.Entity(tx)
		if _, res := m.Hurt(10, entity.AttackDamageSource{}); res.Accepted() {
			t.Errorf("damage without a player was dealt")
		}
		before := m.Health()
		m.Hurt(10, entity.AttackDamageSource{Attacker: pl})
		if got := before - m.Health(); math.Abs(got-3.5) > 1e-6 {
			t.Errorf("body hit dealt %v, want 3.5", got)
		}
	})
	w.Tick(11)
	w.Do(func(tx *world.Tx) {
		m := mobOf(t, tx, r)
		pl, _ := p.Entity(tx)
		m.SetHealth(2)
		m.Hurt(100, entity.AttackDamageSource{Attacker: pl})
		if m.Health() != 1 {
			t.Errorf("health %v after a lethal hit, want 1", m.Health())
		}
	})
	w.Tick(2)
	with(t, w, r, func(tx *world.Tx, m *mob.Mob) {
		if !m.HasComponent("minecraft:behavior.dragondeath") {
			t.Fatalf("no death group: %v", m.Components())
		}
	})
	n, ok := w.TickUntil(1500, func(*world.Tx) bool { return r.Gone() })
	if !ok {
		t.Fatalf("dragon never went:\n%v", w.Dump(r))
	}
	t.Logf("dragon gone after %d ticks", n)
	orbs := 0
	w.Do(func(tx *world.Tx) {
		for e := range tx.Entities() {
			if e.H().Type() == entity.ExperienceOrbType {
				orbs++
			}
		}
	})
	if orbs == 0 {
		t.Errorf("no experience")
	}
}

// TestDragonCrystalHeals: an end crystal within 32 heals the dragon 1 every
// 10 ticks.
func TestDragonCrystalHeals(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:boss")
	r := spawnDragon(t, w, mgl64.Vec3{0.5, 10, 0.5})
	w.Do(func(tx *world.Tx) {
		tx.AddEntity(entity.NewEndCrystal(world.EntitySpawnOpts{Position: mgl64.Vec3{8.5, 1, 0.5}}))
		mobOf(t, tx, r).SetHealth(100)
	})
	w.Tick(200)
	with(t, w, r, func(tx *world.Tx, m *mob.Mob) {
		if m.Health() <= 100 {
			t.Errorf("health %v: the crystal did not heal", m.Health())
		}
	})
}
