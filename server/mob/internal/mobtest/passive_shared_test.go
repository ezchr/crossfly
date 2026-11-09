//go:build crossfly_full

package mobtest_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Shared passive-mob fixes: WaterAnimal flopping and sinking, horse and
// camel healing, rider invulnerability, horse and llama fall damage (also
// ridden), camel sit events.

// TestWaterAnimalFlop: WaterAnimal::preAiStep. Cod, tadpoles and dolphins on
// dry ground hop (0.5 up, tadpole 0.8) and turn; a cow does not. A cod in
// the air (has_gravity false) falls to the ground.
func TestWaterAnimalFlop(t *testing.T) {
	for _, id := range []string{"minecraft:cod", "minecraft:tadpole", "minecraft:dolphin", "minecraft:cow"} {
		t.Run(id, func(t *testing.T) {
			w := mobtest.New(t, mobtest.Options{})
			w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
			r := w.Spawn(id, mgl64.Vec3{0.5, 1, 0.5})
			w.Tick(5)
			maxY, yaw0, turned := 0.0, 0.0, false
			r.With(func(m *mob.Mob) { yaw0 = m.Yaw() })
			w.Each(60, func(tx *world.Tx, _ int) bool {
				m, _ := r.Mob(tx)
				maxY = max(maxY, m.Pos()[1]-1)
				turned = turned || math.Abs(m.Yaw()-yaw0) > 1
				return true
			})
			if id == "minecraft:cow" {
				if maxY > 0.6 {
					t.Fatalf("cow rose %.2f", maxY)
				}
				return
			}
			if maxY < 0.3 || !turned {
				t.Fatalf("%v did not flop: max rise %.2f, turned %v", id, maxY, turned)
			}
			t.Logf("%v max rise %.2f", id, maxY)
		})
	}

	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	r := w.Spawn("minecraft:cod", mgl64.Vec3{0.5, 9, 0.5})
	if _, ok := w.TickUntil(100, func(tx *world.Tx) bool { return r.PosTx(tx)[1] < 2 }); !ok {
		t.Fatalf("a cod in the air did not fall: %v", r.Pos())
	}
}

// TestWaterAnimalSinksBelowSurface: in water, a WaterAnimal whose feet are
// in the top water block is pushed down (0.1 a tick), so fish keep under
// the surface; a dolphin (not pushed until DolphinMoveControl is ported)
// still gets air.
func TestWaterAnimalSinksBelowSurface(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	const top = 8
	w.Do(func(tx *world.Tx) {
		w.Fill(tx, cube.Pos{-7, 0, -7}, cube.Pos{7, top + 3, 7}, block.Stone{})
		w.Water(tx, cube.Pos{-6, 1, -6}, cube.Pos{6, top, 6})
		w.Clear(tx, cube.Pos{-6, top + 1, -6}, cube.Pos{6, top + 3, 6})
	})
	cod := w.Spawn("minecraft:cod", mgl64.Vec3{0.5, top, 0.5})
	w.Tick(40)
	high := 0
	w.Each(200, func(tx *world.Tx, _ int) bool {
		if cod.PosTx(tx)[1] >= top {
			high++
		}
		return true
	})
	if high > 20 {
		t.Fatalf("cod had its feet in the top water block for %d of 200 ticks", high)
	}

	d := w.Spawn("minecraft:dolphin", mgl64.Vec3{0.5, 3, 0.5})
	w.Tick(2)
	air := func() (a, maxAir int) {
		d.With(func(m *mob.Mob) {
			c, _ := m.Component("minecraft:breathable")
			a, maxAir = c.(interface{ Air() (int, int) }).Air()
		})
		return
	}
	_, maxAir := air()
	low := 0
	w.Each(2400, func(tx *world.Tx, _ int) bool {
		m, ok := d.Mob(tx)
		if !ok {
			t.Fatal("the dolphin is gone")
		}
		c, _ := m.Component("minecraft:breathable")
		a, _ := c.(interface{ Air() (int, int) }).Air()
		if a < maxAir/4 {
			low++
		}
		return true
	})
	if d.Gone() || !d.Alive() {
		t.Fatal("the dolphin drowned")
	}
	if a, _ := air(); low > 600 {
		t.Fatalf("dolphin air below a quarter for %d of 2400 ticks (now %d/%d)", low, a, maxAir)
	}
}

// TestHorseCamelNaturalHeal: Horse::postAiStep and Camel::aiStep heal 1
// with chance 1/900 a tick; llamas (Animal in Bedrock) do not heal.
func TestHorseCamelNaturalHeal(t *testing.T) {
	for _, c := range []struct {
		id   string
		heal bool
	}{{"minecraft:horse", true}, {"minecraft:skeleton_horse", true}, {"minecraft:camel", true}, {"minecraft:camel_husk", true}, {"minecraft:llama", false}} {
		t.Run(c.id, func(t *testing.T) {
			w := mobtest.New(t, mobtest.Options{Time: mobtest.Midnight})
			w.Do(func(tx *world.Tx) { w.Arena(tx, 10, 3) })
			r := w.Spawn(c.id, mgl64.Vec3{0.5, 1, 0.5})
			w.Tick(2)
			r.With(func(m *mob.Mob) { m.SetHealth(1) })
			var before, after float64
			r.With(func(m *mob.Mob) { before = m.Health() })
			w.Tick(4500)
			r.With(func(m *mob.Mob) { after = m.Health() })
			healed := after - before
			if c.heal && (healed < 1 || healed > 14) {
				t.Fatalf("healed %v in 4500 ticks, expected about 5", healed)
			}
			if !c.heal && healed != 0 {
				t.Fatalf("healed %v, expected none", healed)
			}
			t.Logf("healed %v", healed)
		})
	}
}

// rideHorse spawns a tamed, saddled adult id at pos and puts a player on it.
func rideHorse(t *testing.T, w *mobtest.World, id string, pos mgl64.Vec3) (*mobtest.Ref, *mobtest.Player) {
	t.Helper()
	h := w.Spawn(id, pos, mob.SpawnOptions{Event: "minecraft:spawn_tame_adult"})
	h.With(func(m *mob.Mob) { m.Fire("minecraft:horse_saddled", nil) })
	p := w.AddPlayer("rider", pos.Add(mgl64.Vec3{1.5, 0, 0}))
	w.Tick(2)
	p.Interact(h, item.Stack{})
	w.Tick(2)
	w.Do(func(tx *world.Tx) {
		m, _ := h.Mob(tx)
		if len(m.PassengerHandles()) == 0 {
			t.Fatal("the player did not get on")
		}
	})
	return h, p
}

// TestRiderCannotHurtMount: Animal::isInvulnerableTo. The first passenger's
// hits and projectiles do nothing to its mount; anyone else's do.
func TestRiderCannotHurtMount(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	h, p := rideHorse(t, w, "minecraft:horse", mgl64.Vec3{0.5, 1, 0.5})
	other := w.AddPlayer("other", mgl64.Vec3{-2.5, 1, 0.5})
	w.Tick(12)
	w.Do(func(tx *world.Tx) {
		m, _ := h.Mob(tx)
		rider, _ := p.Entity(tx)
		o, _ := other.Entity(tx)
		hp := m.Health()
		if _, res := m.Hurt(4, entity.AttackDamageSource{Attacker: rider}); res != world.HurtIgnored || m.Health() != hp {
			t.Fatalf("rider's hit: %v, health %v -> %v", res, hp, m.Health())
		}
		if _, res := m.Hurt(4, entity.ProjectileDamageSource{Owner: rider}); res != world.HurtIgnored || m.Health() != hp {
			t.Fatalf("rider's arrow: %v, health %v -> %v", res, hp, m.Health())
		}
		if _, res := m.Hurt(4, entity.AttackDamageSource{Attacker: o}); res != world.HurtAccepted || m.Health() != hp-4 {
			t.Fatalf("another player's hit: %v, health %v -> %v", res, hp, m.Health())
		}
	})
}

// TestHorseFallDamage: Horse::causeFallDamageToActor is ceil(dist/2 - 3),
// Llama's the same from 6 blocks, other mobs ceil(dist - 3).
func TestHorseFallDamage(t *testing.T) {
	for _, c := range []struct {
		id     string
		height float64
		want   float64
	}{
		{"minecraft:horse", 13, 4},
		{"minecraft:donkey", 7, 1},
		{"minecraft:llama", 5.5, 0},
		{"minecraft:llama", 13, 4},
		{"minecraft:cow", 13, 10},
	} {
		t.Run(c.id, func(t *testing.T) {
			w := mobtest.New(t, mobtest.Options{})
			w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
			r := w.Spawn(c.id, mgl64.Vec3{0.5, 1 + c.height, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_adult"})
			var hp float64
			r.With(func(m *mob.Mob) { hp = m.Health() })
			if _, ok := w.TickUntil(100, func(tx *world.Tx) bool { m, _ := r.Mob(tx); return m.OnGround() }); !ok {
				t.Fatal("never landed")
			}
			w.Tick(1)
			var got float64
			r.With(func(m *mob.Mob) { got = hp - m.Health() })
			if got != c.want {
				t.Fatalf("fall of %v: %v damage, want %v", c.height, got, c.want)
			}
		})
	}
}

// TestRiddenHorseFallDamage: a horse its rider's client moves builds up
// fall distance from the client's moves and lands on the ground with the
// horse fall damage; small drops do nothing.
func TestRiddenHorseFallDamage(t *testing.T) {
	for _, c := range []struct {
		height float64
		want   float64
	}{{13, 4}, {3, 0}} {
		w := mobtest.New(t, mobtest.Options{})
		top := int(c.height)
		w.Do(func(tx *world.Tx) {
			w.Floor(tx, 0, 10, block.Stone{})
			w.Fill(tx, cube.Pos{-2, 1, -2}, cube.Pos{2, top, 2}, block.Stone{})
		})
		start := mgl64.Vec3{0.5, float64(top + 1), 0.5}
		h, p := rideHorse(t, w, "minecraft:horse", start)
		var hp float64
		h.With(func(m *mob.Mob) { hp = m.Health() })
		move := func(to mgl64.Vec3) {
			w.Do(func(tx *world.Tx) {
				e, _ := h.Entity(tx)
				pe, _ := p.Entity(tx)
				r, _ := entity.RideableOf(e)
				if !r.(entity.ClientVehicle).ClientVehicleMove(pe, to, 0, 0) {
					t.Fatalf("client move to %v refused", to)
				}
			})
			w.Tick(1)
		}
		pos := start
		for pos[0] < 5 {
			pos[0] += 0.5
			move(pos)
		}
		for pos[1] > 1 {
			pos[1] = math.Max(pos[1]-0.5, 1)
			move(pos)
		}
		move(pos)
		var got, fall float64
		var ground bool
		h.With(func(m *mob.Mob) { got, fall, ground = hp-m.Health(), m.FallDistance(), m.OnGround() })
		if got != c.want || fall != 0 || !ground {
			t.Fatalf("ridden fall of %v: %v damage (want %v), fall distance %v, on ground %v", c.height, got, c.want, fall, ground)
		}
	}
}

// TestCamelSitEvents: Camel::_monitorSitting fires start_sitting and
// stop_sitting when the sitting flag changes, which swap the collision box.
func TestCamelSitEvents(t *testing.T) {
	for _, id := range []string{"minecraft:camel", "minecraft:camel_husk"} {
		t.Run(id, func(t *testing.T) {
			w := mobtest.New(t, mobtest.Options{})
			w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
			var opts mob.SpawnOptions
			if id == "minecraft:camel" {
				opts.Event = "minecraft:spawn_adult"
			}
			r := w.Spawn(id, mgl64.Vec3{0.5, 1, 0.5}, opts)
			w.Tick(2)
			height := func() (h float64) {
				r.With(func(m *mob.Mob) { h = m.Meta().Height })
				return
			}
			if h := height(); math.Abs(h-2.375) > 1e-6 {
				t.Fatalf("standing height %v", h)
			}
			r.With(func(m *mob.Mob) { m.UpdateMeta(func(meta *mob.Meta) { meta.Sitting = true }) })
			w.Tick(1)
			if h := height(); math.Abs(h-0.945) > 1e-6 {
				t.Fatalf("sitting height %v, want 0.945", h)
			}
			r.With(func(m *mob.Mob) { m.UpdateMeta(func(meta *mob.Meta) { meta.Sitting = false }) })
			w.Tick(1)
			if h := height(); math.Abs(h-2.375) > 1e-6 {
				t.Fatalf("height after standing up %v, want 2.375", h)
			}
		})
	}
}
