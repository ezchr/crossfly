//go:build crossfly_full

package mobtest

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/data"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// Time of day presets for Options.Time (world ticks).
const (
	Noon     = 6000
	Midnight = 18000
)

// Options configure New. The zero value is a night-time (no daylight
// burning), Normal difficulty world without persistence.
type Options struct {
	// Time is the fixed time of day. 0 means Midnight; use Noon (or any
	// other value but 0) for daylight. Time never cycles unless Cycle is set.
	Time  int
	Cycle bool
	// Difficulty defaults to world.DifficultyNormal.
	Difficulty world.Difficulty
	// Persistent stores the world in a temporary mcdb directory so Reload
	// can save and load it again.
	Persistent bool
	// Seed seeds the world's random source and every spawned mob's (each
	// mob gets Seed and its spawn index), so a test is repeatable. 0 = 1.
	Seed uint64
	// Log receives world logs; nil discards them.
	Log *slog.Logger
	// Dim is the world's dimension (nil: the Overworld).
	Dim world.Dimension
	// Generator generates the chunks not built by the test (nil: empty,
	// as world.Config). A generated world has its light worked out, which
	// natural spawning needs.
	Generator world.Generator
}

// World is a synchronous test world. All methods must be called from the
// test goroutine, never from inside Do (they open their own transactions).
type World struct {
	T    testing.TB
	W    *world.World
	opts Options
	dir  string

	ticks int
	seq   uint64

	// area is the box of blocks built and entities spawned through the
	// kit, so Reload knows which chunks to load again.
	area    cube.BBox
	hasArea bool

	refs    []*Ref
	players []*Player
}

var (
	typesOnce sync.Once
	typesErr  error
)

// The server registers the blocks and items of the mobs before any world
// exists (Edition.register in full.go, Mobs on): the saddle, horse armour,
// the seeds sniffers dig up, eggs that hatch. Without them a saddle dropped
// from a horse is an empty item entity that vanishes on its first tick.
func init() {
	block.RegisterMobBlocks()
	item.RegisterMobItems()
}

// RegisterTypes registers the embedded vanilla mobs (definitions with at
// least one minecraft:behavior.*) as mob types, parsed with whatever
// definition parser is linked, skipping identifiers Dragonfly's own entity
// registry has. It runs once per test binary; New calls it.
func RegisterTypes() error {
	typesOnce.Do(func() {
		existing := map[string]bool{"minecraft:player": true}
		for _, t := range entity.DefaultRegistry.Types() {
			existing[t.EncodeEntity()] = true
		}
		for _, f := range data.Entities() {
			d, err := mob.ParseDefinition(f.Data)
			if err != nil {
				typesErr = fmt.Errorf("mobtest: %v: %w", f.Name, err)
				return
			}
			if existing[d.Identifier()] || !isMob(d) {
				continue
			}
			mob.RegisterType(d)
		}
	})
	return typesErr
}

func isMob(d mob.Definition) bool {
	for _, k := range d.ComponentKeys() {
		if strings.HasPrefix(k, "minecraft:behavior.") {
			return true
		}
	}
	return false
}

func entityRegistry() world.EntityRegistry {
	types := append(entity.DefaultRegistry.Types(), player.Type)
	types = append(types, mob.Types()...)
	return entity.DefaultRegistry.Config().New(types)
}

// New creates a synchronous world. It is closed when the test ends.
func New(t testing.TB, opts Options) *World {
	t.Helper()
	if err := RegisterTypes(); err != nil {
		t.Fatal(err)
	}
	if opts.Seed == 0 {
		opts.Seed = 1
	}
	if opts.Time == 0 {
		opts.Time = Midnight
	}
	if opts.Difficulty == nil {
		opts.Difficulty = world.DifficultyNormal
	}
	if opts.Log == nil {
		opts.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	w := &World{T: t, opts: opts}
	if opts.Persistent {
		w.dir = t.TempDir()
	}
	w.open()
	t.Cleanup(w.Close)
	return w
}

func (w *World) open() {
	w.T.Helper()
	conf := world.Config{
		Log:          w.opts.Log,
		Entities:     entityRegistry(),
		Synchronous:  true,
		RandSource:   rand.NewPCG(w.opts.Seed, 0x6d6f6274657374),
		SaveInterval: -1,
		Generator:    w.opts.Generator,
		Dim:          w.opts.Dim,
	}
	if w.dir != "" {
		db, err := mcdb.Config{Log: w.opts.Log}.Open(w.dir)
		if err != nil {
			w.T.Fatalf("mobtest: open world: %v", err)
		}
		conf.Provider = db
	}
	w.W = conf.New()
	w.W.SetDifficulty(w.opts.Difficulty)
	w.W.SetTime(w.opts.Time)
	if w.opts.Cycle {
		w.W.StartTime()
	} else {
		w.W.StopTime()
	}
}

// Close closes the world (also done automatically at the end of the test).
func (w *World) Close() {
	if w.W != nil {
		_ = w.W.Close()
		w.W = nil
	}
}

// Do runs f in a transaction of the world.
func (w *World) Do(f func(tx *world.Tx)) {
	w.T.Helper()
	if w.W == nil {
		w.T.Fatal("mobtest: world is closed")
	}
	if _, err := world.Call(context.Background(), w.W, func(tx *world.Tx) (struct{}, error) {
		f(tx)
		return struct{}{}, nil
	}); err != nil {
		w.T.Fatalf("mobtest: transaction: %v", err)
	}
}

// Ticks is how many ticks Tick and TickUntil advanced in total.
func (w *World) Ticks() int { return w.ticks }

// Tick advances the world n ticks: every entity (mobs, the players,
// arrows, items...), scheduled block updates and time.
func (w *World) Tick(n int) {
	w.T.Helper()
	for range n {
		w.W.AdvanceTick()
		w.ticks++
	}
}

// TickUntil advances tick by tick, checking cond after each, until cond is
// true or max ticks passed. It returns how many ticks it advanced and
// whether cond became true.
func (w *World) TickUntil(max int, cond func(tx *world.Tx) bool) (int, bool) {
	w.T.Helper()
	for i := 1; i <= max; i++ {
		w.Tick(1)
		ok := false
		w.Do(func(tx *world.Tx) { ok = cond(tx) })
		if ok {
			return i, true
		}
	}
	return max, false
}

// Each advances n ticks and calls f in a transaction after every tick (to
// record a trace). f returning false stops early.
func (w *World) Each(n int, f func(tx *world.Tx, tick int) bool) {
	w.T.Helper()
	for i := 1; i <= n; i++ {
		w.Tick(1)
		cont := true
		w.Do(func(tx *world.Tx) { cont = f(tx, i) })
		if !cont {
			return
		}
	}
}

// grow extends the remembered area by box.
func (w *World) grow(box cube.BBox) {
	if !w.hasArea {
		w.area, w.hasArea = box, true
		return
	}
	mn, mx := w.area.Min(), w.area.Max()
	bmn, bmx := box.Min(), box.Max()
	for i := range 3 {
		mn[i], mx[i] = min(mn[i], bmn[i]), max(mx[i], bmx[i])
	}
	w.area = cube.Box(mn[0], mn[1], mn[2], mx[0], mx[1], mx[2])
}

// Mark makes Reload load the chunks of the area between a and b again (the
// kit's builders and spawns mark their areas themselves).
func (w *World) Mark(a, b cube.Pos) {
	w.grow(cube.Box(float64(a[0]), float64(a[1]), float64(a[2]), float64(b[0]), float64(b[1]), float64(b[2])))
}

// Reload saves the world, closes it and opens it again from disk, loading
// the marked area; mobs come back through their NBT (they initialise again
// on their next tick, without a spawn event). Refs follow their entity by
// UUID; Players are removed before saving and added again at their last
// position with what they held. It needs Options.Persistent.
func (w *World) Reload() {
	w.T.Helper()
	if w.dir == "" {
		w.T.Fatal("mobtest: Reload needs Options.Persistent")
	}
	w.Do(func(tx *world.Tx) {
		for e := range tx.Entities() {
			p := e.Position()
			w.grow(cube.Box(p[0], p[1], p[2], p[0], p[1], p[2]))
		}
	})
	for _, p := range w.players {
		p.detach(w)
	}
	w.Close()
	w.open()
	if w.hasArea {
		mn, mx := w.area.Min(), w.area.Max()
		w.Do(func(tx *world.Tx) {
			for x := int(mn[0]) &^ 15; x <= int(mx[0]); x += 16 {
				for z := int(mn[2]) &^ 15; z <= int(mx[2]); z += 16 {
					tx.Block(cube.Pos{x, 0, z})
				}
			}
		})
	}
	byID := map[uuid.UUID]*world.EntityHandle{}
	w.Do(func(tx *world.Tx) {
		for e := range tx.Entities() {
			byID[e.H().UUID()] = e.H()
		}
	})
	for _, r := range w.refs {
		if h, ok := byID[r.id]; ok {
			r.h = h
		} else {
			r.h = nil
		}
	}
	for _, p := range w.players {
		p.attach(w)
	}
}

// Spawn spawns a mob by identifier through mob.Spawn (spawn event
// included) and returns a Ref to it. The mob's random source is seeded from
// Options.Seed and the spawn index.
func (w *World) Spawn(id string, pos mgl64.Vec3, opts ...mob.SpawnOptions) *Ref {
	w.T.Helper()
	var o mob.SpawnOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	var ref *Ref
	w.Do(func(tx *world.Tx) { ref = w.SpawnTx(tx, id, pos, o) })
	return ref
}

// SpawnTx is Spawn inside a transaction.
func (w *World) SpawnTx(tx *world.Tx, id string, pos mgl64.Vec3, o mob.SpawnOptions) *Ref {
	w.T.Helper()
	w.seq++
	seq := w.seq
	conf := o.Configure
	o.Configure = func(m *mob.Mob) {
		m.SetRandSeed(w.opts.Seed, seq)
		if conf != nil {
			conf(m)
		}
	}
	m, err := mob.Spawn(tx, id, pos, o)
	if err != nil {
		w.T.Fatalf("mobtest: spawn %v: %v", id, err)
	}
	w.grow(cube.Box(pos[0]-1, pos[1]-1, pos[2]-1, pos[0]+1, pos[1]+1, pos[2]+1))
	return w.track(m.H())
}

// Track returns a Ref for an entity the test added itself.
func (w *World) Track(h *world.EntityHandle) *Ref { return w.track(h) }

func (w *World) track(h *world.EntityHandle) *Ref {
	r := &Ref{w: w, id: h.UUID(), h: h}
	w.refs = append(w.refs, r)
	return r
}

// Entities returns Refs to every entity in the world for which f is true.
func (w *World) Entities(f func(e world.Entity) bool) []*Ref {
	w.T.Helper()
	var hs []*world.EntityHandle
	w.Do(func(tx *world.Tx) {
		for e := range tx.Entities() {
			if f == nil || f(e) {
				hs = append(hs, e.H())
			}
		}
	})
	out := make([]*Ref, len(hs))
	for i, h := range hs {
		out[i] = w.findRef(h)
	}
	return out
}

func (w *World) findRef(h *world.EntityHandle) *Ref {
	for _, r := range w.refs {
		if r.h == h {
			return r
		}
	}
	return w.track(h)
}

// Mobs returns Refs to every mob of the identifier ("" = all mobs).
func (w *World) Mobs(id string) []*Ref {
	return w.Entities(func(e world.Entity) bool {
		m, ok := mob.Of(e)
		return ok && (id == "" || m.Identifier() == id)
	})
}

// Count counts the entities for which f is true.
func (w *World) Count(f func(e world.Entity) bool) int {
	w.T.Helper()
	n := 0
	w.Do(func(tx *world.Tx) {
		for e := range tx.Entities() {
			if f(e) {
				n++
			}
		}
	})
	return n
}
