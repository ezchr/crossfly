//go:build crossfly_full

// Package coretest is the test kit of server/mob/component/core and
// server/mob/goal/core: hand-written definitions with groups and events, a
// world, players, and fake navigation/look/jump controls, so the passive
// components and goals can be tested without packages mob/def, mob/filter,
// mob/nav and mob/control. Only tests import it.
package coretest

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// Def is a test definition: base components, groups and events that add and
// remove groups (in that order: remove first).
type Def struct {
	ID       string
	Category string
	Base     []mob.RawComponent
	Groups   map[string][]mob.RawComponent
	Events   map[string]Event
	Props    []mob.PropertyDecl
}

// Event is what a test event does.
type Event struct {
	Add, Remove []string
	// Then fires another event afterwards (like "trigger").
	Then string
}

// C is shorthand for a component.
func C(key, raw string) mob.RawComponent {
	return mob.RawComponent{Key: key, Raw: json.RawMessage(raw)}
}

func (d *Def) Identifier() string { return d.ID }
func (d *Def) Description() mob.Description {
	return mob.Description{Identifier: d.ID, SpawnCategory: d.Category, Properties: d.Props}
}
func (d *Def) ComponentKeys() []string {
	var out []string
	for _, c := range d.Base {
		out = append(out, c.Key)
	}
	return out
}
func (d *Def) NewInstance() mob.Instance {
	in := &Inst{d: d, props: map[string]any{}}
	for _, p := range d.Props {
		in.props[p.Name] = p.Default
	}
	return in
}

// Inst is a Def's live instance.
type Inst struct {
	d      *Def
	groups []string
	props  map[string]any
	// Fired lists the events fired, in order.
	Fired []string
}

// Components ...
func (i *Inst) Components() []mob.RawComponent {
	out := slices.Clone(i.d.Base)
	for _, g := range i.groups {
		for _, c := range i.d.Groups[g] {
			if j := slices.IndexFunc(out, func(o mob.RawComponent) bool { return o.Key == c.Key }); j >= 0 {
				out[j] = c
				continue
			}
			out = append(out, c)
		}
	}
	return out
}

// Fire ...
func (i *Inst) Fire(m *mob.Mob, ev string, _ *mob.FilterContext) bool {
	i.Fired = append(i.Fired, ev)
	e, ok := i.d.Events[ev]
	if !ok {
		return false
	}
	changed := false
	for _, g := range e.Remove {
		if j := slices.Index(i.groups, g); j >= 0 {
			i.groups = slices.Delete(i.groups, j, j+1)
			changed = true
		}
	}
	for _, g := range e.Add {
		if !slices.Contains(i.groups, g) {
			i.groups = append(i.groups, g)
			changed = true
		}
	}
	if e.Then != "" {
		if i.Fire(m, e.Then, nil) {
			changed = true
		}
	}
	return changed
}

// HasGroup reports whether a group was added.
func (i *Inst) HasGroup(g string) bool { return slices.Contains(i.groups, g) }

func (i *Inst) Property(n string) (any, bool) { v, ok := i.props[n]; return v, ok }
func (i *Inst) SetProperty(n string, v any) error {
	i.props[n] = v
	return nil
}
func (i *Inst) Properties() []mob.PropertyValue { return nil }
func (i *Inst) EncodeNBT() map[string]any {
	g := make([]any, len(i.groups))
	for j, s := range i.groups {
		g[j] = s
	}
	return map[string]any{"groups": g}
}
func (i *Inst) DecodeNBT(d map[string]any) {
	if g, ok := d["groups"].([]any); ok {
		for _, s := range g {
			i.groups = append(i.groups, s.(string))
		}
	}
}

// InstOf returns the test instance of m.
func InstOf(m *mob.Mob) *Inst { return m.Instance().(*Inst) }

var registerMu sync.Mutex

// Register registers test definitions as mob types (call before World).
func Register(defs ...*Def) {
	registerMu.Lock()
	defer registerMu.Unlock()
	for _, d := range defs {
		mob.RegisterType(d)
	}
}

// World returns a function running f in a transaction of a new empty world.
func World(t testing.TB) func(f func(tx *world.Tx)) {
	t.Helper()
	types := append(entity.DefaultRegistry.Types(), player.Type)
	types = append(types, mob.Types()...)
	reg := entity.DefaultRegistry.Config().New(types)
	w := world.Config{Entities: reg, Provider: world.NopProvider{}, Generator: world.NopGenerator{}}.New()
	t.Cleanup(func() { _ = w.Close() })
	return func(f func(tx *world.Tx)) {
		t.Helper()
		var stopped bool
		var crash any
		_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
			defer func() {
				if r := recover(); r != nil {
					if _, ok := r.(stop); ok {
						stopped = true
					} else {
						crash = r
					}
				}
			}()
			f(tx)
			return struct{}{}, nil
		})
		if crash != nil {
			panic(crash)
		}
		if stopped {
			t.FailNow()
		}
	}
}

// stop ends a test function running in a world transaction (t.Fatal would
// end the world's goroutine and hang the test).
type stop struct{}

// Fatalf reports a failure and ends the function World runs. Use it instead
// of t.Fatalf inside World.
func Fatalf(t testing.TB, format string, args ...any) {
	t.Helper()
	t.Errorf(format, args...)
	panic(stop{})
}

// Fatal is Fatalf without formatting.
func Fatal(t testing.TB, args ...any) {
	t.Helper()
	t.Error(args...)
	panic(stop{})
}

// Floor fills the square of radius r at height y with b.
func Floor(tx *world.Tx, y, r int, b world.Block) {
	for x := -r; x <= r; x++ {
		for z := -r; z <= r; z++ {
			tx.SetBlock(cube.Pos{x, y, z}, b, nil)
		}
	}
}

// Stone is the floor block tests use.
var Stone = block.Stone{}

// Spawn spawns a test mob with fake controls and a fixed seed. ev is the
// spawn event ("" = entity_spawned).
func Spawn(t testing.TB, tx *world.Tx, id string, pos mgl64.Vec3, ev string) (*mob.Mob, *Controls) {
	t.Helper()
	c := &Controls{}
	m, err := mob.Spawn(tx, id, pos, mob.SpawnOptions{Event: ev, Configure: nil})
	if err != nil {
		Fatal(t, err)
	}
	c.Install(m)
	m.SetRandSeed(1, 2)
	return m, c
}

// Player adds a player at pos holding held.
func Player(tx *world.Tx, pos mgl64.Vec3, held item.Stack) *player.Player {
	h := world.EntitySpawnOpts{Position: pos}.New(player.Type, player.Config{Name: "Tester", UUID: uuid.New(), GameMode: world.GameModeSurvival, Position: pos})
	p := tx.AddEntity(h).(*player.Player)
	p.SetHeldItems(held, item.Stack{})
	return p
}

// TickAll ticks the mobs n times.
func TickAll(tx *world.Tx, n int, ms ...*mob.Mob) {
	for i := 0; i < n; i++ {
		for _, m := range ms {
			if mm, ok := mob.OfHandle(tx, m.H()); ok {
				mm.Tick(tx, int64(i))
			}
		}
	}
}

// Mobs returns the mobs of identifier id in the world.
func Mobs(tx *world.Tx, id string) []*mob.Mob {
	var out []*mob.Mob
	for e := range tx.Entities() {
		if m, ok := mob.Of(e); ok && m.Identifier() == id {
			out = append(out, m)
		}
	}
	return out
}

// Items returns the item stacks lying in the world.
func Items(tx *world.Tx) []item.Stack {
	var out []item.Stack
	for e := range tx.Entities() {
		if ent, ok := e.(*entity.Ent); ok {
			if b, ok := ent.Behaviour().(*entity.ItemBehaviour); ok {
				out = append(out, b.Item())
			}
		}
	}
	return out
}

// Experience returns the experience lying in the world as orbs.
func Experience(tx *world.Tx) int {
	n := 0
	for e := range tx.Entities() {
		if ent, ok := e.(*entity.Ent); ok {
			if b, ok := ent.Behaviour().(*entity.ExperienceOrbBehaviour); ok {
				n += b.Experience()
			}
		}
	}
	return n
}
