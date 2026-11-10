//go:build crossfly_full

package locomotion_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	cloco "github.com/df-mc/dragonfly/server/mob/component/locomotion"
	"github.com/df-mc/dragonfly/server/mob/goal/locomotion"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	_ "github.com/df-mc/dragonfly/server/mob/vanilla"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// pool builds a stone basin (x/z -r..r, floor at y 0) filled with water from
// y 1 to depth, with air above.
func pool(w *mobtest.World, r, depth int) {
	w.Do(func(tx *world.Tx) {
		w.Fill(tx, cube.Pos{-r - 1, 0, -r - 1}, cube.Pos{r + 1, depth + 2, r + 1}, block.Stone{})
		w.Clear(tx, cube.Pos{-r, 1, -r}, cube.Pos{r, depth + 2, r})
		w.Water(tx, cube.Pos{-r, 1, -r}, cube.Pos{r, depth, r})
	})
}

func inWater(tx *world.Tx, p mgl64.Vec3) bool {
	l, ok := tx.Liquid(cube.PosFromVec3(p))
	if !ok {
		return false
	}
	_, w := l.(block.Water)
	return w
}

// TestFishSwimAndStayInWater: cod (random_swim, swim_idle, swim_wander,
// flocking) swim around a pool and never leave the water.
func TestFishSwimAndStayInWater(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	pool(w, 8, 5)
	var fish []*mobtest.Ref
	for i := 0; i < 5; i++ {
		fish = append(fish, w.Spawn("minecraft:cod", mgl64.Vec3{float64(i) - 2 + 0.5, 3, 0.5}))
	}
	start := fish[0].Pos()
	moved := 0.0
	w.Each(600, func(tx *world.Tx, tick int) bool {
		for _, f := range fish {
			p := f.PosTx(tx)
			if !inWater(tx, p) && !inWater(tx, p.Add(mgl64.Vec3{0, 0.3, 0})) {
				t.Fatalf("tick %d: fish left the water at %v", tick, p)
			}
		}
		moved = math.Max(moved, fish[0].PosTx(tx).Sub(start).Len())
		return true
	})
	if moved < 2 {
		t.Errorf("fish moved at most %.2f blocks in 600 ticks:\n%v", moved, w.Dump(fish[0]))
	}
}

// TestFishFlock: cod close together join a flock with a leader.
func TestFishFlock(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{Seed: 3})
	pool(w, 6, 5)
	var fish []*mobtest.Ref
	for i := 0; i < 6; i++ {
		fish = append(fish, w.Spawn("minecraft:cod", mgl64.Vec3{float64(i%3) + 0.5, 3, float64(i/3) + 0.5}))
	}
	n, ok := w.TickUntil(400, func(tx *world.Tx) bool {
		for _, f := range fish {
			m, ok := f.Mob(tx)
			if !ok {
				continue
			}
			if fl, ok := cloco.FlockOf(m); ok && fl.Leader() && fl.Members() >= 2 {
				return true
			}
		}
		return false
	})
	if !ok {
		t.Fatalf("no flock formed in %d ticks", n)
	}
}

// TestSquidSwimsAndInks: a squid moves around in water, and on land its
// horizontal velocity is cancelled.
func TestSquidSwimsAndInks(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	pool(w, 6, 6)
	s := w.Spawn("minecraft:squid", mgl64.Vec3{0.5, 3, 0.5})
	start := s.Pos()
	w.Tick(400)
	if d := s.Pos().Sub(start).Len(); d < 0.5 {
		t.Errorf("squid moved %.2f blocks in 400 ticks:\n%v", d, w.Dump(s))
	}
	s.With(func(m *mob.Mob) {
		if !m.Physics().InWater {
			t.Errorf("squid left the water")
		}
		if locomotion.SquidMovement(m) == (mgl64.Vec3{}) {
			t.Errorf("squid has no movement vector")
		}
	})

	land := w.Spawn("minecraft:squid", mgl64.Vec3{20.5, 1, 20.5})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 24, block.Stone{}) })
	w.Tick(40)
	land.With(func(m *mob.Mob) {
		if v := m.Vel(); v[0] != 0 || v[2] != 0 {
			t.Errorf("squid on land slides: %v", v)
		}
	})
}

// TestSlimeHopsAndSplits: a large slime hops around; killed, it splits into
// 2-4 medium slimes.
func TestSlimeHopsAndSplits(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Arena(tx, 20, 3) })
	s := w.Spawn("minecraft:slime", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "spawn_large"})
	start := s.Pos()
	jumped := false
	w.Each(300, func(tx *world.Tx, _ int) bool {
		if s.PosTx(tx)[1] > 1.3 {
			jumped = true
		}
		return true
	})
	if !jumped {
		t.Errorf("slime never jumped:\n%v", w.Dump(s))
	}
	if d := s.Pos().Sub(start).Len(); d < 1 {
		t.Errorf("slime moved %.2f blocks in 300 ticks", d)
	}
	s.With(func(m *mob.Mob) {
		if m.Variant() != 4 {
			t.Fatalf("large slime has size %d", m.Variant())
		}
		m.Kill(entity.VoidDamageSource{})
	})
	w.Tick(25)
	kids := w.Mobs("minecraft:slime")
	alive := 0
	w.Do(func(tx *world.Tx) {
		for _, k := range kids {
			if m, ok := k.Mob(tx); ok && !m.Dead() {
				alive++
				if m.Variant() != 2 {
					t.Errorf("child slime size %d, want 2", m.Variant())
				}
			}
		}
	})
	if alive < 2 || alive > 4 {
		t.Errorf("large slime split into %d slimes, want 2-4", alive)
	}
}

// TestGhastFloatsAround: a ghast (float_wander, navigation.float) stays in
// the air and drifts around.
func TestGhastFloatsAround(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 30, block.Stone{}) })
	g := w.Spawn("minecraft:ghast", mgl64.Vec3{0.5, 12, 0.5})
	start := g.Pos()
	// A ghast that does not fly lies on the floor (y 1) from about tick 20; one that flies wanders up and
	// down (float_wander may pick targets just over the floor, as 26.60 RandomPos::getPos does).
	maxY := 0.0
	w.Each(400, func(tx *world.Tx, i int) bool {
		if i >= 100 {
			maxY = math.Max(maxY, g.PosTx(tx)[1])
		}
		return true
	})
	if maxY < 3 {
		t.Errorf("ghast stayed below y %.2f: it does not fly", maxY)
	}
	if d := g.Pos().Sub(start).Len(); d < 1 {
		t.Errorf("ghast drifted %.2f blocks in 400 ticks:\n%v", d, w.Dump(g))
	}
}

// TestBeeHovers: a bee (random_hover, movement.hover) flies around above the
// ground.
func TestBeeHovers(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{Time: 6000})
	w.Do(func(tx *world.Tx) { w.Grass(tx, 0, 20) })
	b := w.Spawn("minecraft:bee", mgl64.Vec3{0.5, 3, 0.5})
	start := b.Pos()
	maxD := 0.0
	w.Each(400, func(tx *world.Tx, _ int) bool {
		maxD = math.Max(maxD, b.PosTx(tx).Sub(start).Len())
		return true
	})
	if maxD < 2 {
		t.Errorf("bee moved at most %.2f blocks in 400 ticks:\n%v", maxD, w.Dump(b))
	}
}

// TestPhantomCirclesAndSwoops: a phantom glides in circles in the air and
// swoops at a player, hurting them.
func TestPhantomCirclesAndSwoops(t *testing.T) {
	mobtest.RequireDefinitions(t)
	mobtest.RequireFilters(t)
	w := mobtest.New(t, mobtest.Options{Difficulty: world.DifficultyNormal})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 63, 40, block.Stone{}) })
	ph := w.Spawn("minecraft:phantom", mgl64.Vec3{0.5, 90, 0.5})
	w.Tick(5)
	ph.With(func(m *mob.Mob) {
		if _, ok := m.MoveControl().(*cloco.GlideControl); !ok {
			t.Fatalf("phantom move control is %T, want *GlideControl", m.MoveControl())
		}
	})
	var path []mgl64.Vec3
	w.Each(200, func(tx *world.Tx, _ int) bool {
		path = append(path, ph.PosTx(tx))
		return true
	})
	lo, hi := path[0], path[0]
	for _, p := range path {
		for i := 0; i < 3; i++ {
			lo[i], hi[i] = math.Min(lo[i], p[i]), math.Max(hi[i], p[i])
		}
	}
	if hi[0]-lo[0] < 3 || hi[2]-lo[2] < 3 {
		t.Errorf("phantom does not circle: x %.1f..%.1f z %.1f..%.1f", lo[0], hi[0], lo[2], hi[2])
	}
	if lo[1] < 65 {
		t.Errorf("phantom fell to y %.1f", lo[1])
	}
	p := w.AddPlayer("target", mgl64.Vec3{0.5, 64, 0.5})
	ph.With(func(m *mob.Mob) {
		for e := range m.Tx().Entities() {
			if pl, ok := e.(*player.Player); ok {
				m.SetTarget(pl)
			}
		}
	})
	if n, ok := w.TickUntil(1200, func(tx *world.Tx) bool { return p.HealthTx(tx) < 20 }); !ok {
		t.Errorf("phantom did not hurt the player in %d ticks:\n%v", n, w.Dump(ph))
	}
}

// TestGuardianBeam: a guardian in water attacks a player in water after the
// 80-tick charge.
func TestGuardianBeam(t *testing.T) {
	mobtest.RequireDefinitions(t)
	mobtest.RequireFilters(t)
	w := mobtest.New(t, mobtest.Options{Difficulty: world.DifficultyNormal})
	pool(w, 8, 6)
	g := w.Spawn("minecraft:guardian", mgl64.Vec3{-4.5, 2, 0.5})
	p := w.AddPlayer("target", mgl64.Vec3{4.5, 2, 0.5})
	n, ok := w.TickUntil(400, func(tx *world.Tx) bool { return p.HealthTx(tx) < 20 })
	if !ok {
		t.Fatalf("guardian did not hurt the player in %d ticks:\n%v", n, w.Dump(g))
	}
	if n < 80 {
		t.Errorf("guardian hurt the player after %d ticks, before the 80 tick charge", n)
	}
}

// TestDolphinBreathes: a dolphin low on air swims up to the surface.
func TestDolphinBreathes(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	pool(w, 6, 10)
	d := w.Spawn("minecraft:dolphin", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:entity_spawned"})
	d.With(func(m *mob.Mob) {
		if m.MoveControl() == nil {
			t.Fatal("dolphin has no move control")
		}
		c, ok := m.Component("minecraft:breathable")
		if !ok {
			t.Fatal("dolphin has no breathable")
		}
		// Drain the air: the dolphin breathes air only.
		for i := 0; i < 4500; i++ {
			c.(mob.Ticker).Tick(m)
		}
	})
	n, ok := w.TickUntil(600, func(tx *world.Tx) bool { return d.PosTx(tx)[1] > 9 })
	if !ok {
		t.Fatalf("dolphin did not swim up for air in %d ticks:\n%v", n, w.Dump(d))
	}
}

// TestAxolotlPlaysDead: hurt below force_below_health in water, an axolotl
// plays dead with regeneration.
func TestAxolotlPlaysDead(t *testing.T) {
	mobtest.RequireDefinitions(t)
	mobtest.RequireFilters(t)
	w := mobtest.New(t, mobtest.Options{})
	pool(w, 4, 3)
	a := w.Spawn("minecraft:axolotl", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(2)
	a.With(func(m *mob.Mob) { m.Hurt(m.Health()-5, entity.AttackDamageSource{}) })
	n, ok := w.TickUntil(20, func(tx *world.Tx) bool {
		m, ok := a.Mob(tx)
		return ok && locomotion.PlayingDead(m)
	})
	if !ok {
		t.Fatalf("axolotl did not play dead in %d ticks:\n%v", n, w.Dump(a))
	}
	a.With(func(m *mob.Mob) {
		if !m.ActorFlag(protocol.EntityDataFlagPlayingDead) {
			t.Error("PLAYING_DEAD flag not shown")
		}
		if _, ok := m.Effect(effect.Regeneration); !ok {
			t.Error("no regeneration while playing dead")
		}
	})
	w.Tick(210)
	a.With(func(m *mob.Mob) {
		if locomotion.PlayingDead(m) {
			t.Error("still playing dead after the 10 s duration")
		}
	})
}

// TestFrogMovesToWater: a frog on land next to a pond walks into it.
func TestTurtleMovesToWater(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{Time: 6000})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, -3, 20, block.Stone{})
		w.Grass(tx, 0, 20)
		w.Clear(tx, cube.Pos{6, 0, -3}, cube.Pos{12, 0, 3})
		w.Fill(tx, cube.Pos{6, -2, -3}, cube.Pos{12, -2, 3}, block.Stone{})
		w.Water(tx, cube.Pos{6, -1, -3}, cube.Pos{12, 0, 3})
	})
	tu := w.Spawn("minecraft:turtle", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:entity_spawned"})
	n, ok := w.TickUntil(1200, func(tx *world.Tx) bool {
		m, ok := tu.Mob(tx)
		return ok && m.Physics().InWater
	})
	if !ok {
		t.Fatalf("turtle did not reach the water in %d ticks:\n%v", n, w.Dump(tu))
	}
}

// TestStriderRisesToLavaSurface: a strider in deep lava rises to the surface.
func TestStriderRisesToLavaSurface(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Fill(tx, cube.Pos{-5, 0, -5}, cube.Pos{5, 8, 5}, block.Stone{})
		w.Fill(tx, cube.Pos{-4, 1, -4}, cube.Pos{4, 6, 4}, block.Lava{Still: true, Depth: 8})
		w.Clear(tx, cube.Pos{-4, 7, -4}, cube.Pos{4, 8, 4})
	})
	s := w.Spawn("minecraft:strider", mgl64.Vec3{0.5, 2, 0.5})
	n, ok := w.TickUntil(600, func(tx *world.Tx) bool { return s.PosTx(tx)[1] > 5.5 })
	if !ok {
		t.Fatalf("strider did not rise to the lava surface in %d ticks (y %.2f):\n%v", n, s.Pos()[1], w.Dump(s))
	}
}
