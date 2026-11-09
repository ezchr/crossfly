//go:build crossfly_full

package control

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// testDef is a definition with a fixed component list, so the tests need
// neither package mob/def nor the vanilla data.
type testDef struct {
	id    string
	comps []mob.RawComponent
}

func (d testDef) Identifier() string           { return d.id }
func (d testDef) Description() mob.Description { return mob.Description{Identifier: d.id} }
func (d testDef) ComponentKeys() []string {
	keys := make([]string, len(d.comps))
	for i, c := range d.comps {
		keys[i] = c.Key
	}
	return keys
}
func (d testDef) NewInstance() mob.Instance { return testInst{comps: d.comps} }

type testInst struct{ comps []mob.RawComponent }

func (i testInst) Components() []mob.RawComponent               { return i.comps }
func (testInst) Fire(*mob.Mob, string, *mob.FilterContext) bool { return false }
func (testInst) Property(string) (any, bool)                    { return nil, false }
func (testInst) SetProperty(string, any) error                  { return errors.New("no properties") }
func (testInst) Properties() []mob.PropertyValue                { return nil }
func (testInst) EncodeNBT() map[string]any                      { return nil }
func (testInst) DecodeNBT(map[string]any)                       {}

func comps(kv ...string) []mob.RawComponent {
	out := make([]mob.RawComponent, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		out = append(out, mob.RawComponent{Key: kv[i], Raw: []byte(kv[i+1])})
	}
	return out
}

func init() {
	mob.RegisterType(testDef{id: "test:cow", comps: comps(
		"minecraft:movement.basic", `{}`,
		"minecraft:jump.static", `{}`,
		"minecraft:physics", `{}`,
		"minecraft:can_climb", `{}`,
		"minecraft:pushable_by_entity", `{}`,
	)})
	mob.RegisterType(testDef{id: "test:legless", comps: comps(
		"minecraft:movement.basic", `{"max_turn": 30}`,
		"minecraft:physics", `{}`,
	)})
	mob.RegisterType(testDef{id: "test:parrot", comps: comps(
		"minecraft:movement.fly", `{}`,
		"minecraft:physics", `{}`,
	)})
	mob.RegisterType(testDef{id: "test:drowned", comps: comps(
		"minecraft:movement.generic", `{}`,
		"minecraft:jump.static", `{}`,
		"minecraft:physics", `{}`,
		"minecraft:navigation.walk", `{"can_sink": false}`,
	)})
}

// testWorld returns a function running f in a transaction of a new empty
// world.
func testWorld(t *testing.T) func(f func(tx *world.Tx)) {
	t.Helper()
	reg := entity.DefaultRegistry.Config().New(append(entity.DefaultRegistry.Types(), mob.Types()...))
	w := world.Config{Entities: reg, Provider: world.NopProvider{}, Generator: world.NopGenerator{}}.New()
	t.Cleanup(func() { _ = w.Close() })
	return func(f func(tx *world.Tx)) {
		_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
			// t.Fatal would Goexit the world goroutine and hang Call: fail
			// panics with a fatal instead, ended here.
			defer func() {
				if r := recover(); r != nil {
					if msg, ok := r.(fatal); ok {
						t.Error(string(msg))
						return
					}
					panic(r)
				}
			}()
			f(tx)
			return struct{}{}, nil
		})
	}
}

// fatal is the panic value of fail.
type fatal string

// fail ends the running test transaction with an error (use it instead of
// t.Fatal inside a transaction).
func fail(t *testing.T, format string, args ...any) {
	t.Helper()
	panic(fatal(fmt.Sprintf(format, args...)))
}

// floor fills y=0 from -r to r with stone.
func floor(tx *world.Tx, r int) {
	for x := -r; x <= r; x++ {
		for z := -r; z <= r; z++ {
			tx.SetBlock(cube.Pos{x, 0, z}, block.Stone{}, nil)
		}
	}
}

// spawn adds a mob of id at pos with the box size and speed given.
func spawn(t *testing.T, tx *world.Tx, id string, pos mgl64.Vec3, w, h, speed float64) *mob.Mob {
	t.Helper()
	m, err := mob.Spawn(tx, id, pos, mob.SpawnOptions{NoEvent: true, Configure: func(m *mob.Mob) {
		m.UpdateMeta(func(meta *mob.Meta) { meta.Width, meta.Height = w, h })
		m.SetSpeed(speed)
		m.SetMaxHealth(20)
		m.SetHealth(20)
	}})
	if err != nil {
		fail(t, "spawn %v: %v", id, err)
	}
	return m
}

// tick ticks m n times.
func tick(tx *world.Tx, m *mob.Mob, n int) {
	for i := 0; i < n; i++ {
		m.Tick(tx, m.Ticks()+1)
	}
}
