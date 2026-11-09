//go:build crossfly_full

package mobtest_test

// Integration checks added with the integration pass: a cross-package
// conversion (sensor -> groups -> timer -> transformation) and a soak run
// of many mixed mobs that reports the per-tick cost.

import (
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestZombieConvertsToDrowned: a zombie held under water starts its drowned
// transformation (environment_sensor -> start_drowned_transformation, 30 s
// timer), shakes (convert_to_drowned, 15 s delay) and becomes a drowned.
func TestZombieConvertsToDrowned(t *testing.T) {
	needs(t, true, true, "minecraft:environment_sensor", "minecraft:timer", "minecraft:transformation")
	long(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 4, block.Stone{})
		w.Wall(tx, cube.Pos{-2, 1, -2}, cube.Pos{2, 1, -2}, 4, block.Stone{})
		w.Wall(tx, cube.Pos{-2, 1, 2}, cube.Pos{2, 1, 2}, 4, block.Stone{})
		w.Wall(tx, cube.Pos{-2, 1, -2}, cube.Pos{-2, 1, 2}, 4, block.Stone{})
		w.Wall(tx, cube.Pos{2, 1, -2}, cube.Pos{2, 1, 2}, 4, block.Stone{})
		w.Water(tx, cube.Pos{-1, 1, -1}, cube.Pos{1, 4, 1})
	})
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:as_adult"})
	shook := -1
	n, ok := w.TickUntil(1300, func(tx *world.Tx) bool {
		if m, ok := z.Mob(tx); ok && shook < 0 && m.Shaking() {
			shook = w.Ticks()
		}
		for e := range tx.Entities() {
			if e.H().Type().EncodeEntity() == "minecraft:drowned" {
				return true
			}
		}
		return false
	})
	if !ok {
		t.Fatalf("zombie did not become a drowned in %d ticks (shaking at %d):\n%v", n, shook, w.Dump(z))
	}
	// BDS: 30 s under water, then 15 s of shaking.
	if shook < 590 || shook > 640 {
		t.Errorf("shaking started at tick %d, want about 600 (30 s timer)", shook)
	}
	if d := n - shook; d < 290 || d > 320 {
		t.Errorf("converted %d ticks after shaking started, want 300 (15 s delay)", d)
	}
	t.Logf("shaking at %d, drowned at %d", shook, n)
}

// Soak timing: two systems bracket each mob's tick (first in StageSensors,
// last in StageSend), so the mob work is measured apart from the rest of the
// world tick (the world package's redstone engine costs more than the mobs in
// this arena). They only count while soakTiming is set.
var (
	soakTiming  bool
	soakMobTime time.Duration
)

type soakKey struct{}

func init() {
	mob.RegisterSystem(mob.System{Name: "mobtest/soak_start", Stage: mob.StageSensors, Order: -1 << 30, Tick: func(m *mob.Mob) {
		if soakTiming {
			m.SetData(soakKey{}, time.Now())
		}
	}})
	mob.RegisterSystem(mob.System{Name: "mobtest/soak_end", Stage: mob.StageSend, Order: 1 << 30, Tick: func(m *mob.Mob) {
		if v, ok := m.Data(soakKey{}); ok && soakTiming {
			soakMobTime += time.Since(v.(time.Time))
			m.SetData(soakKey{}, nil)
		}
	}})
}

// TestSoak50Mobs: 50 mixed Phase 1 mobs in a 64x64 arena with a moving
// player for 2 simulated minutes. Reports the average and worst world tick
// and the allocations per tick; fails on panics (the test binary dies).
func TestSoak50Mobs(t *testing.T) {
	needs(t, true, true, walkers...)
	long(t)
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Midnight})
	w.Do(func(tx *world.Tx) {
		w.Fill(tx, cube.Pos{-33, -3, -33}, cube.Pos{33, -2, 33}, block.Stone{})
		w.Grass(tx, 0, 33)
		g := block.Glass{}
		w.Wall(tx, cube.Pos{-33, 1, -33}, cube.Pos{33, 1, -33}, 3, g)
		w.Wall(tx, cube.Pos{-33, 1, 33}, cube.Pos{33, 1, 33}, 3, g)
		w.Wall(tx, cube.Pos{-33, 1, -33}, cube.Pos{-33, 1, 33}, 3, g)
		w.Wall(tx, cube.Pos{33, 1, -33}, cube.Pos{33, 1, 33}, 3, g)
		// Some relief so paths are not all straight lines.
		w.Platform(tx, cube.Pos{-6, 1, -6}, cube.Pos{6, 1, 6}, 2, block.Stone{})
		w.Stairs(tx, cube.Pos{0, 1, 9}, cube.North, 2, 3, block.Planks{}, block.Stone{})
		w.Wall(tx, cube.Pos{-20, 1, 15}, cube.Pos{20, 1, 15}, 2, block.Stone{})
	})
	p := w.AddPlayer("soak", mgl64.Vec3{20.5, 1, -20.5})
	p.SetMaxHealth(1e9)

	mix := []struct {
		id string
		n  int
	}{
		{"minecraft:zombie", 8}, {"minecraft:skeleton", 6}, {"minecraft:creeper", 4}, {"minecraft:spider", 6},
		{"minecraft:cow", 7}, {"minecraft:sheep", 7}, {"minecraft:pig", 6}, {"minecraft:chicken", 6},
	}
	i := 0
	for _, m := range mix {
		for range m.n {
			x, z := float64(-28+(i%10)*6)+0.5, float64(-28+(i/10)*12)+0.5
			if x > -8 && x < 8 && z > -8 && z < 8 {
				z += 16
			}
			// Persistent: the arena is wider than the despawn distances
			// (44 blocks instant, 32 random), which would empty it.
			w.Spawn(m.id, mgl64.Vec3{x, 1, z}, mob.SpawnOptions{Configure: func(m *mob.Mob) { m.SetPersistent(true) }})
			i++
		}
	}

	const ticks = 2400
	spots := []mgl64.Vec3{{20.5, 1, -20.5}, {-20.5, 1, -20.5}, {0.5, 3, 0.5}, {-20.5, 1, 25.5}, {20.5, 1, 25.5}}
	var (
		total, worst time.Duration
		worstAt      int
		durs         = make([]time.Duration, 0, ticks)
		ms0, ms1     runtime.MemStats
	)
	var mobTotal, mobWorst time.Duration
	mobDurs := make([]time.Duration, 0, ticks)
	soakTiming = true
	defer func() { soakTiming = false }()
	runtime.GC()
	runtime.ReadMemStats(&ms0)
	for tick := 1; tick <= ticks; tick++ {
		if tick%400 == 0 {
			p.Teleport(spots[(tick/400)%len(spots)])
		}
		start := time.Now()
		w.W.AdvanceTick()
		d := time.Since(start)
		total += d
		durs = append(durs, d)
		if d > worst {
			worst, worstAt = d, tick
		}
		md := soakMobTime
		soakMobTime = 0
		mobTotal += md
		mobDurs = append(mobDurs, md)
		mobWorst = max(mobWorst, md)
	}
	runtime.ReadMemStats(&ms1)

	alive, arrows := map[string]int{}, 0
	w.Do(func(tx *world.Tx) {
		for e := range tx.Entities() {
			if e.H().Type() == entity.ArrowType {
				arrows++
			}
			if m, ok := mob.Of(e); ok && !m.Dead() {
				alive[m.Identifier()]++
			}
		}
	})
	slices.Sort(durs)
	p99 := durs[len(durs)*99/100]
	slices.Sort(mobDurs)
	t.Logf("%d ticks, 50 mobs spawned, alive at end %v, arrows in flight %d, player health lost %.0f",
		ticks, alive, arrows, 1e9-p.Health())
	t.Logf("world tick: avg %v, p99 %v, worst %v (tick %d); per mob avg %v",
		total/ticks, p99, worst, worstAt, total/ticks/50)
	t.Logf("mob work only: avg %v per tick (%v per mob), p99 %v, worst %v",
		mobTotal/ticks, mobTotal/ticks/50, mobDurs[len(mobDurs)*99/100], mobWorst)
	t.Logf("allocations (whole world tick): %d per tick, %.1f KiB per tick (GC cycles %d)",
		(ms1.Mallocs-ms0.Mallocs)/ticks, float64(ms1.TotalAlloc-ms0.TotalAlloc)/ticks/1024, ms1.NumGC-ms0.NumGC)
	if errs := mob.FactoryErrors(); len(errs) > 0 {
		t.Errorf("factory errors: %v", errs)
	}
}

// TestPhase1MovementSpeeds: every Phase 1 mob moves at its minecraft:movement
// value once spawned, never at the 0.7 attribute default.
func TestPhase1MovementSpeeds(t *testing.T) {
	needs(t, true, true, "minecraft:movement")
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 12, block.Stone{}) })
	for i, c := range []struct{ id, group, event string }{
		{"cow", "", ""}, {"pig", "", ""}, {"sheep", "", ""}, {"chicken", "", ""},
		{"zombie", "minecraft:zombie_adult", "minecraft:as_adult"}, {"zombie", "minecraft:zombie_baby", "minecraft:as_baby"},
		{"skeleton", "", ""}, {"spider", "", ""}, {"creeper", "", ""},
	} {
		var mv struct {
			Value float64 `json:"value"`
		}
		mobtest.Decode(t, mobtest.Component(t, c.id+".json", c.group, "minecraft:movement"), &mv)
		r := w.Spawn("minecraft:"+c.id, mgl64.Vec3{float64(i*2-9) + 0.5, 1, 0.5}, mob.SpawnOptions{Event: c.event})
		w.Tick(1)
		r.With(func(m *mob.Mob) {
			if got := m.Speed(); got != mv.Value {
				t.Errorf("%v %v: speed %v, want %v (minecraft:movement)", c.id, c.group, got, mv.Value)
			}
		})
	}
}

// TestReaddedGroupReinitialises: firing an event that adds a group which is
// already active rebuilds its components (BDS re-initialises them: the
// drowned conversion timer starts over), components of other groups keep
// their state.
func TestReaddedGroupReinitialises(t *testing.T) {
	needs(t, true, true, "minecraft:timer")
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 4, block.Stone{}) })
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:as_adult"})
	var timer, health mob.Component
	z.With(func(m *mob.Mob) {
		m.Fire("minecraft:start_transforming_into_drowned", nil)
		timer, _ = m.Component("minecraft:timer")
		health, _ = m.Component("minecraft:health")
	})
	if timer == nil {
		t.Fatal("start_drowned_transformation added no minecraft:timer")
	}
	w.Tick(5)
	z.With(func(m *mob.Mob) {
		m.Fire("minecraft:start_transforming_into_drowned", nil)
		again, _ := m.Component("minecraft:timer")
		if again == timer {
			t.Errorf("re-added group kept its old minecraft:timer instance")
		}
		if h, _ := m.Component("minecraft:health"); h != health {
			t.Errorf("minecraft:health was rebuilt although its group was not re-added")
		}
	})
}

// TestArmourChangeInLaterTransaction: equipping a mob in a later transaction
// than the one it spawned in (spawn gear rolled on its first tick, a
// dispenser, a plugin) must not touch the finished spawn transaction. The
// demo server crashed on this (zombie helmet from minecraft:equipment).
func TestArmourChangeInLaterTransaction(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 4, block.Stone{}) })
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:as_adult"})
	w.Tick(2)
	w.Do(func(tx *world.Tx) {
		e, ok := z.Entity(tx)
		if !ok {
			t.Fatal("zombie gone")
		}
		// Through the entity (behaviour forwarding), not a fresh *mob.Mob.
		a, ok := e.(interface{ Armour() *inventory.Armour })
		if !ok || a.Armour() == nil {
			t.Fatal("zombie has no armour inventory")
		}
		a.Armour().SetHelmet(item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 1))
	})
	w.Tick(2)
	z.With(func(m *mob.Mob) {
		if m.Armour().Helmet().Empty() {
			t.Error("helmet not kept")
		}
	})
}
