//go:build crossfly_full

package locomotion_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/locomotion"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	_ "github.com/df-mc/dragonfly/server/mob/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// TestBuoyantHorseFloats: a horse (buoyant, apply_gravity false) dropped in
// deep water comes up to the surface and stays there.
func TestBuoyantHorseFloats(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Fill(tx, cube.Pos{-6, 0, -6}, cube.Pos{6, 10, 6}, block.Stone{})
		w.Clear(tx, cube.Pos{-5, 1, -5}, cube.Pos{5, 10, 5})
		w.Water(tx, cube.Pos{-5, 1, -5}, cube.Pos{5, 7, 5})
	})
	h := w.Spawn("minecraft:horse", mgl64.Vec3{0.5, 2, 0.5})
	w.Tick(200)
	minY := 100.0
	w.Each(100, func(tx *world.Tx, _ int) bool {
		minY = math.Min(minY, h.PosTx(tx)[1])
		return true
	})
	// The water top is y 8 (sources fill 8/9 of the block): the horse floats
	// with its feet just below it.
	if minY < 6.5 {
		t.Errorf("horse sank to y %.2f in water up to y 8:\n%v", minY, w.Dump(h))
	}
}

// TestFrictionAndBounce: friction_modifier makes a sliding mob stop sooner,
// bounciness makes a falling one bounce.
func TestFrictionAndBounce(t *testing.T) {
	mobtest.RequireDefinitions(t)
	slide := func(friction float64) float64 {
		w := mobtest.New(t, mobtest.Options{})
		w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 40, block.Stone{}) })
		s := w.Spawn("minecraft:slime", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "spawn_small"})
		w.Tick(5)
		s.With(func(m *mob.Mob) {
			m.Goals().ClearAll()
			locomotion.SetMoveMods(m, friction, 1, 0)
			m.SetVelocity(mgl64.Vec3{1, 0, 0})
		})
		start := s.Pos()
		w.Tick(40)
		return s.Pos()[0] - start[0]
	}
	normal, sticky := slide(1), slide(0.05)
	if !(sticky < normal*0.5) {
		t.Errorf("friction_modifier 0.05 slid %.2f blocks, 1 slid %.2f", sticky, normal)
	}

	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 10, block.Stone{}) })
	s := w.Spawn("minecraft:slime", mgl64.Vec3{0.5, 4, 0.5}, mob.SpawnOptions{Event: "spawn_large"})
	s.With(func(m *mob.Mob) {
		m.Goals().ClearAll()
		locomotion.SetMoveMods(m, 1, 1, 0.9)
	})
	landed, rose := false, false
	w.Each(80, func(tx *world.Tx, _ int) bool {
		m, ok := s.Mob(tx)
		if !ok {
			t.Fatal("slime gone")
		}
		if m.Pos()[1] < 1.2 {
			landed = true
		}
		if landed && m.Vel()[1] > 0.3 {
			rose = true
		}
		return true
	})
	if !landed || !rose {
		t.Errorf("bounciness 0.9: landed %v, bounced %v", landed, rose)
	}
}

// TestDolphinBribeAndDrying: a dolphin fed a cod is bribed; out of water it
// starts drying out (120 s) and is hurt once dried.
func TestDolphinBribeAndDrying(t *testing.T) {
	mobtest.RequireDefinitions(t)
	mobtest.RequireFilters(t)
	mobtest.Require(t, "minecraft:environment_sensor", "minecraft:damage_over_time")
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 10, block.Stone{}) })
	d := w.Spawn("minecraft:dolphin", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:entity_spawned", Configure: func(m *mob.Mob) {}})
	w.Tick(2)
	var adult bool
	d.With(func(m *mob.Mob) { adult = !m.Baby() })
	if adult {
		p := w.AddPlayer("feeder", mgl64.Vec3{1.5, 1, 0.5})
		left, ok := p.Interact(d, item.NewStack(item.Cod{}, 2))
		if !ok || left.Count() != 1 {
			t.Errorf("feeding a cod: ok %v, %d left", ok, left.Count())
		}
		d.With(func(m *mob.Mob) {
			if !locomotion.Bribed(m) {
				t.Error("dolphin not bribed after a cod")
			}
		})
	}
	w.Tick(20)
	var remaining int
	d.With(func(m *mob.Mob) {
		var ok bool
		if remaining, ok = locomotion.DryingRemaining(m); !ok {
			t.Fatalf("dolphin on land has no drying_out_timer:\n%v", w.Dump(d))
		}
	})
	if remaining < 2300 || remaining > 2400 {
		t.Errorf("drying timer %d ticks left after ~20 ticks on land, want about 2380", remaining)
	}
	w.Tick(remaining + 5)
	d.With(func(m *mob.Mob) {
		if !m.HasComponent("minecraft:damage_over_time") {
			t.Errorf("dried out dolphin has no damage_over_time:\n%v", w.Dump(d))
		}
	})
}

// TestCanFlyFlag: can_fly shows the CANFLY actor flag.
func TestCanFlyFlag(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	b := w.Spawn("minecraft:bee", mgl64.Vec3{0.5, 5, 0.5})
	w.Tick(1)
	b.With(func(m *mob.Mob) {
		if !m.ActorFlag(protocol.EntityDataFlagFly) {
			t.Error("bee without the CANFLY flag")
		}
	})
}

// TestGuardianSpikes: a swimming guardian shows MOVING (spikes in); an
// attacker hitting a still one takes thorns damage.
func TestGuardianSpikes(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 10, block.Stone{}) })
	g := w.Spawn("minecraft:guardian", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(2)
	g.With(func(m *mob.Mob) {
		m.Goals().ClearAll()
		if m.ActorFlag(protocol.EntityDataFlagMoving) {
			t.Error("guardian on land shows MOVING")
		}
	})
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{2.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:as_adult"})
	w.Tick(1)
	var before float64
	w.Do(func(tx *world.Tx) {
		zm, _ := z.Mob(tx)
		gm, _ := g.Mob(tx)
		zm.Goals().ClearAll()
		before = zm.Health()
		gm.Hurt(1, entityAttack(zm))
		if zm.Health() >= before {
			t.Errorf("attacker health %.1f -> %.1f: no thorns", before, zm.Health())
		}
	})
}

func entityAttack(a world.Entity) world.DamageSource { return entity.AttackDamageSource{Attacker: a} }
