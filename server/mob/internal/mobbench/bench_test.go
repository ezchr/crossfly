//go:build crossfly_full

package mobbench_test

// Benchmarks of the Bedrock mob AI at server scale: a world with a realistic
// mix of mobs (grazing animals, hostiles chasing a player, villagers in a
// village, fish in a pond, bees and parrots flying), natural spawning around
// a few players, and the viewers mobs are shown to.
//
//	go test -tags crossfly_full -run '^$' -bench . -benchtime 300x ./server/mob/internal/mobbench/
//
// Each op is one world tick. Metrics: ms/tick (wall time per tick),
// cpu-ms/tick (CPU time of the process per tick, GC included: steadier than
// the wall time on a busy machine), allocs/op and B/op (per tick), KiB/mob
// (heap held per mob after warm-up), */tick/viewer (viewer calls per tick).

import (
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/mob/spawn"
	_ "github.com/df-mc/dragonfly/server/mob/vanilla" // links every mob package
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// arena is the half-width of the benchmark world's floor.
const arena = 48

// mix is the share of each mob in the benchmark world, in percent.
var mix = []struct {
	id    string
	share int
	kind  kind
}{
	{"minecraft:cow", 12, grazer}, {"minecraft:sheep", 10, grazer}, {"minecraft:pig", 10, grazer}, {"minecraft:chicken", 10, grazer},
	{"minecraft:zombie", 12, hunter}, {"minecraft:skeleton", 8, hunter},
	{"minecraft:villager_v2", 10, villager},
	{"minecraft:cod", 6, fish}, {"minecraft:salmon", 4, fish}, {"minecraft:tropicalfish", 4, fish},
	{"minecraft:bee", 7, flier}, {"minecraft:parrot", 7, flier},
}

type kind uint8

const (
	grazer kind = iota
	hunter
	villager
	fish
	flier
)

var villagerEvents = []string{"minecraft:spawn_farmer", "minecraft:spawn_librarian", "minecraft:spawn_fisherman", "minecraft:spawn_cleric"}

// scene is a built benchmark world.
type scene struct {
	w       *mobtest.World
	players []*mobtest.Player
	mobs    int
	heap    uint64
}

// build makes the benchmark world: a grass floor with glass walls, a pond in
// the north-west, a village (beds and job sites) in the south-east, flowers,
// and one player in the middle that the hostiles chase.
func build(b *testing.B) *mobtest.World {
	w := mobtest.New(b, mobtest.Options{Time: mobtest.Midnight, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	w.Do(func(tx *world.Tx) {
		w.Fill(tx, cube.Pos{-arena - 1, -5, -arena - 1}, cube.Pos{arena + 1, -2, arena + 1}, block.Stone{})
		w.Grass(tx, 0, arena)
		g := block.Glass{}
		w.Wall(tx, cube.Pos{-arena - 1, 1, -arena - 1}, cube.Pos{arena + 1, 1, -arena - 1}, 4, g)
		w.Wall(tx, cube.Pos{-arena - 1, 1, arena + 1}, cube.Pos{arena + 1, 1, arena + 1}, 4, g)
		w.Wall(tx, cube.Pos{-arena - 1, 1, -arena - 1}, cube.Pos{-arena - 1, 1, arena + 1}, 4, g)
		w.Wall(tx, cube.Pos{arena + 1, 1, -arena - 1}, cube.Pos{arena + 1, 1, arena + 1}, 4, g)
		// Pond, 4 deep.
		w.Water(tx, cube.Pos{-44, -3, -44}, cube.Pos{-20, 0, -20})
		// Village: rows of beds and job sites.
		jobs := []world.Block{block.Composter{}, block.Lectern{}, block.NewBarrel(), block.NewSmoker(cube.North), block.NewBlastFurnace(cube.North)}
		for i := range 30 {
			x, z := 20+(i%10)*2, 22+(i/10)*6
			tx.SetBlock(cube.Pos{x, 1, z}, block.Bed{Facing: cube.North, Head: true}, nil)
			tx.SetBlock(cube.Pos{x, 1, z + 1}, block.Bed{Facing: cube.North}, nil)
			if i%2 == 0 {
				tx.SetBlock(cube.Pos{x, 1, z + 3}, jobs[i/2%len(jobs)], nil)
			}
		}
		// Flowers for the bees, rocks for the paths.
		r := rand.New(rand.NewPCG(7, 7))
		for range 60 {
			x, z := r.IntN(2*arena)-arena, r.IntN(2*arena)-arena
			if x < -18 && z < -18 || x > 18 && z > 18 {
				continue
			}
			tx.SetBlock(cube.Pos{x, 1, z}, block.Flower{Type: block.Poppy()}, nil)
		}
		for range 25 {
			x, z := r.IntN(2*arena-4)-arena+2, r.IntN(2*arena-4)-arena+2
			if x < -18 && z < -18 || x > 18 && z > 18 || x*x+z*z < 100 {
				continue
			}
			w.Fill(tx, cube.Pos{x, 1, z}, cube.Pos{x + 1, 1 + r.IntN(2), z + 1}, block.Cobblestone{})
		}
	})
	return w
}

// spawnMix spawns n mobs of the mix.
func spawnMix(w *mobtest.World, n int, seed uint64) {
	r := rand.New(rand.NewPCG(seed, 99))
	counts := make([]int, len(mix))
	total := 0
	for i, m := range mix {
		counts[i] = n * m.share / 100
		total += counts[i]
	}
	for i := 0; total < n; i = (i + 1) % len(mix) {
		counts[i]++
		total++
	}
	persistent := mob.SpawnOptions{Configure: func(m *mob.Mob) { m.SetPersistent(true) }}
	w.Do(func(tx *world.Tx) {
		for i, m := range mix {
			for j := range counts[i] {
				var pos mgl64.Vec3
				o := persistent
				switch m.kind {
				case grazer:
					pos = landSpot(r)
				case hunter:
					// Within follow range (16) of the player.
					a, d := r.Float64()*6.283, 6+r.Float64()*9
					pos = mgl64.Vec3{d * math.Cos(a), 1, d * math.Sin(a)}
				case villager:
					pos = mgl64.Vec3{20 + r.Float64()*20, 1, 20 + r.Float64()*12}
					ev := villagerEvents[j%len(villagerEvents)]
					o.Event = ev
				case fish:
					pos = mgl64.Vec3{-43 + r.Float64()*22, -2.5 + r.Float64()*2, -43 + r.Float64()*22}
				case flier:
					pos = landSpot(r).Add(mgl64.Vec3{0, 2 + r.Float64()*3, 0})
				}
				w.SpawnTx(tx, m.id, pos, o)
			}
		}
	})
}

// landSpot is a random spot on the grass outside the pond and the village.
func landSpot(r *rand.Rand) mgl64.Vec3 {
	for {
		x, z := r.Float64()*2*(arena-2)-(arena-2), r.Float64()*2*(arena-2)-(arena-2)
		if x < -18 && z < -18 || x > 18 && z > 18 {
			continue
		}
		return mgl64.Vec3{x, 3, z}
	}
}

func heap() uint64 {
	runtime.GC()
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapAlloc
}

// newScene builds the world with n mobs, one player in the middle, and warms
// it up for warm ticks.
func newScene(b *testing.B, n int, warm int) *scene {
	w := build(b)
	p := w.AddPlayer("bench", mgl64.Vec3{0.5, 1, 0.5})
	p.SetMaxHealth(1e12)
	w.Tick(5)
	before := heap()
	spawnMix(w, n, 1)
	w.Tick(warm)
	s := &scene{w: w, players: []*mobtest.Player{p}, mobs: n}
	if h := heap(); n > 0 && h > before {
		s.heap = (h - before) / uint64(n)
	}
	return s
}

// run ticks the scene b.N times and reports ms/tick.
func (s *scene) run(b *testing.B) {
	b.ReportAllocs()
	runtime.GC()
	b.ResetTimer()
	start, cpu := time.Now(), cpuTime()
	for range b.N {
		s.w.W.AdvanceTick()
	}
	el, elCPU := time.Since(start), cpuTime()-cpu
	b.StopTimer()
	b.ReportMetric(float64(el.Microseconds())/1000/float64(b.N), "ms/tick")
	if elCPU > 0 {
		b.ReportMetric(float64(elCPU.Microseconds())/1000/float64(b.N), "cpu-ms/tick")
	}
	if s.mobs > 0 {
		b.ReportMetric(float64(s.heap)/1024, "KiB/mob")
	}
	if errs := mob.FactoryErrors(); len(errs) > 0 {
		b.Fatalf("factory errors: %v", errs)
	}
}

// BenchmarkMobs: a world tick with 0 (the world alone), 50, 200 and 500 mobs.
func BenchmarkMobs(b *testing.B) {
	for _, n := range []int{0, 50, 200, 500} {
		b.Run(fmt.Sprintf("mobs=%d", n), func(b *testing.B) {
			s := newScene(b, n, 200)
			s.run(b)
		})
	}
}

// BenchmarkMobKinds: 100 mobs of one kind of the mix, to tell which costs
// what.
func BenchmarkMobKinds(b *testing.B) {
	for _, id := range []string{"minecraft:cow", "minecraft:chicken", "minecraft:zombie", "minecraft:skeleton",
		"minecraft:villager_v2", "minecraft:cod", "minecraft:bee", "minecraft:parrot"} {
		b.Run(id[len("minecraft:"):], func(b *testing.B) {
			w := build(b)
			p := w.AddPlayer("bench", mgl64.Vec3{0.5, 1, 0.5})
			p.SetMaxHealth(1e12)
			w.Tick(5)
			old := mix
			for _, m := range old {
				if m.id == id {
					mix = []struct {
						id    string
						share int
						kind  kind
					}{{m.id, 100, m.kind}}
				}
			}
			spawnMix(w, 100, 1)
			mix = old
			w.Tick(200)
			(&scene{w: w, mobs: 0}).run(b)
		})
	}
}

// countingViewer counts what mobs show it.
type countingViewer struct {
	world.NopViewer
	move, vel, state, other atomic.Int64
}

func (v *countingViewer) ViewEntityMovement(world.Entity, mgl64.Vec3, cube.Rotation, bool) {
	v.move.Add(1)
}
func (v *countingViewer) ViewEntityVelocity(world.Entity, mgl64.Vec3)       { v.vel.Add(1) }
func (v *countingViewer) ViewEntityState(world.Entity)                      { v.state.Add(1) }
func (v *countingViewer) ViewEntityAction(world.Entity, world.EntityAction) { v.other.Add(1) }
func (v *countingViewer) ViewEntityArmour(world.Entity)                     { v.other.Add(1) }
func (v *countingViewer) ViewEntityItems(world.Entity)                      { v.other.Add(1) }
func (v *countingViewer) ViewSound(mgl64.Vec3, world.Sound)                 { v.other.Add(1) }

// BenchmarkMobViewers: 200 mobs shown to 1, 5 and 10 viewers (chunk loaders
// over the arena). Reports the viewer calls per tick and viewer: these are
// the packets each client gets for the mobs (see the session and javasession
// benchmarks for what one costs to encode).
func BenchmarkMobViewers(b *testing.B) {
	for _, nv := range []int{1, 5, 10} {
		b.Run(fmt.Sprintf("viewers=%d", nv), func(b *testing.B) {
			s := newScene(b, 200, 0)
			vs := make([]*countingViewer, nv)
			s.w.Do(func(tx *world.Tx) {
				for i := range vs {
					vs[i] = &countingViewer{}
					l := world.NewLoader(5, tx.World(), vs[i])
					l.Move(tx, mgl64.Vec3{float64(i), 1, 0})
					l.Load(tx, 1000)
				}
			})
			s.w.Tick(200)
			for _, v := range vs {
				v.move.Store(0)
				v.vel.Store(0)
				v.state.Store(0)
				v.other.Store(0)
			}
			s.run(b)
			var mv, vl, st, ot int64
			for _, v := range vs {
				mv, vl, st, ot = mv+v.move.Load(), vl+v.vel.Load(), st+v.state.Load(), ot+v.other.Load()
			}
			d := float64(b.N) * float64(nv)
			b.ReportMetric(float64(mv)/d, "moves/tick/viewer")
			b.ReportMetric(float64(vl)/d, "motions/tick/viewer")
			b.ReportMetric(float64(st)/d, "states/tick/viewer")
			b.ReportMetric(float64(ot)/d, "other/tick/viewer")
		})
	}
}

// BenchmarkNaturalSpawn: natural spawning around 1 and 4 players on a big
// grass floor at night, until the caps fill. Reports the cost of the
// spawner's own tick (spawn.Tick) apart from the world tick.
func BenchmarkNaturalSpawn(b *testing.B) {
	for _, np := range []int{1, 4} {
		b.Run(fmt.Sprintf("players=%d", np), func(b *testing.B) {
			w := mobtest.New(b, mobtest.Options{Time: mobtest.Midnight})
			const r = 96
			w.Do(func(tx *world.Tx) {
				w.Fill(tx, cube.Pos{-r, -3, -r}, cube.Pos{r, -2, r}, block.Stone{})
				w.Grass(tx, 0, r)
			})
			for i := range np {
				p := w.AddPlayer(fmt.Sprintf("p%d", i), mgl64.Vec3{float64(i%2)*80 - 40, 1, float64(i/2)*80 - 40})
				p.SetMaxHealth(1e12)
			}
			spawn.Enable(w.W, spawn.Config{ManualTick: true, Seed: 3})
			var spawnTime time.Duration
			tick := func() {
				w.W.AdvanceTick()
				w.Do(func(tx *world.Tx) {
					t := time.Now()
					spawn.Tick(tx)
					spawnTime += time.Since(t)
				})
			}
			for range 400 {
				tick()
			}
			spawnTime = 0
			b.ReportAllocs()
			b.ResetTimer()
			start := time.Now()
			for range b.N {
				tick()
			}
			el := time.Since(start)
			b.StopTimer()
			mobs := w.Count(func(e world.Entity) bool { _, ok := mob.Of(e); return ok })
			b.ReportMetric(float64(el.Microseconds())/1000/float64(b.N), "ms/tick")
			b.ReportMetric(float64(spawnTime.Microseconds())/1000/float64(b.N), "spawn-ms/tick")
			b.ReportMetric(float64(mobs), "mobs")
		})
	}
}
