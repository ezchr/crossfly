//go:build crossfly_full

package mob

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// testDef is a hand-written Definition with one group and two events, so the
// tests do not depend on package mob/def.
type testDef struct{}

func (testDef) Identifier() string { return "test:mob" }
func (testDef) Description() Description {
	return Description{Identifier: "test:mob", Properties: []PropertyDecl{{Name: "test:p", Type: "int", Default: 0.0}}}
}
func (testDef) ComponentKeys() []string { return []string{"test:comp", "test:goal", "test:extra"} }
func (testDef) NewInstance() Instance {
	return &testInst{props: map[string]any{"test:p": 0.0}}
}

type testInst struct {
	extra   bool
	compRaw string
	props   map[string]any
}

func (i *testInst) Components() []RawComponent {
	raw := i.compRaw
	if raw == "" {
		raw = `{"v":1}`
	}
	out := []RawComponent{{Key: "test:comp", Raw: []byte(raw)}, {Key: "test:goal", Raw: []byte(`{"priority":1}`)}, {Key: "test:unknown", Raw: []byte(`{}`)}}
	if i.extra {
		out = append(out, RawComponent{Key: "test:extra", Raw: []byte(`{}`)})
	}
	return out
}
func (i *testInst) Fire(m *Mob, ev string, _ *FilterContext) bool {
	switch ev {
	case "add_extra":
		i.extra = true
	case "remove_extra":
		i.extra = false
	case "change_comp":
		i.compRaw = `{"v":2}`
	case EventSpawned:
		i.props["test:p"] = 7.0
		return false
	default:
		return false
	}
	return true
}
func (i *testInst) Property(n string) (any, bool) { v, ok := i.props[n]; return v, ok }
func (i *testInst) SetProperty(n string, v any) error {
	i.props[n] = v
	return nil
}
func (i *testInst) Properties() []PropertyValue { return nil }
func (i *testInst) EncodeNBT() map[string]any {
	return map[string]any{"extra": boolByte(i.extra)}
}
func (i *testInst) DecodeNBT(d map[string]any) { i.extra = d["extra"] == byte(1) }

type logKey struct{}

func logOf(m *Mob) *[]string {
	v, ok := m.Data(logKey{})
	if !ok {
		l := &[]string{}
		m.SetData(logKey{}, l)
		return l
	}
	return v.(*[]string)
}

// testComp logs attach/detach and ticks in the stage it names.
type testComp struct {
	key   string
	v     int
	stage Stage
	saved int32
}

func (c *testComp) Attach(m *Mob) { *logOf(m) = append(*logOf(m), "attach "+c.key) }
func (c *testComp) Detach(m *Mob) { *logOf(m) = append(*logOf(m), "detach "+c.key) }
func (c *testComp) Tick(m *Mob)   { *logOf(m) = append(*logOf(m), "comp "+c.stage.String()) }
func (c *testComp) TickStage() Stage {
	return c.stage
}
func (c *testComp) EncodeNBT() map[string]any { return map[string]any{"saved": c.saved} }
func (c *testComp) DecodeNBT(_ *Mob, d map[string]any) {
	c.saved, _ = d["saved"].(int32)
}

type testGoal struct{ GoalBase }

func (testGoal) CanUse(*Mob) bool { return true }
func (testGoal) Tick(m *Mob)      { *logOf(m) = append(*logOf(m), "goal") }

var recordStages bool

func init() {
	RegisterComponent("test:comp", func(key string, raw json.RawMessage) (Component, error) {
		var v struct{ V int }
		_ = json.Unmarshal(raw, &v)
		return &testComp{key: key, v: v.V, stage: StageEnvironment}, nil
	})
	RegisterComponent("test:extra", func(key string, raw json.RawMessage) (Component, error) {
		return &testComp{key: key, stage: StageSensors}, nil
	})
	RegisterGoal("test:goal", func(spec GoalSpec) (Goal, error) {
		return &testGoal{GoalBase{Control: spec.FlagsOr(FlagMove)}}, nil
	})
	for st := StageSensors; st < stageCount; st++ {
		RegisterSystem(System{Name: "test-" + st.String(), Stage: st, Tick: func(m *Mob) {
			if recordStages {
				*logOf(m) = append(*logOf(m), "sys "+st.String())
			}
		}})
	}
	RegisterType(testDef{})
}

func testWorld(t *testing.T) func(func(tx *world.Tx)) {
	t.Helper()
	reg := entity.DefaultRegistry.Config().New(append(entity.DefaultRegistry.Types(), Types()...))
	w := world.Config{Entities: reg, Provider: world.NopProvider{}, Generator: world.NopGenerator{}}.New()
	t.Cleanup(func() { _ = w.Close() })
	return func(f func(tx *world.Tx)) {
		_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) { f(tx); return struct{}{}, nil })
	}
}

func spawnTest(t *testing.T, tx *world.Tx) *Mob {
	t.Helper()
	for x := -2; x <= 2; x++ {
		for z := -2; z <= 2; z++ {
			tx.SetBlock(cube.Pos{x, 9, z}, block.Stone{}, nil)
		}
	}
	m, err := Spawn(tx, "test:mob", mgl64.Vec3{0.5, 10, 0.5}, SpawnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestTickOrder(t *testing.T) {
	run := testWorld(t)
	run(func(tx *world.Tx) {
		m := spawnTest(t, tx)
		if v, _ := m.Property("test:p"); v != 7.0 {
			t.Errorf("entity_spawned did not fire on Spawn: test:p = %v", v)
		}
		*logOf(m) = nil
		recordStages = true
		m.Tick(tx, 1)
		recordStages = false
		want := []string{
			"sys sensors",
			"goal", "sys goals",
			"sys navigation", "sys controls", "sys travel", "sys body",
			"sys environment", "comp environment",
			"sys despawn", "sys send",
		}
		if got := *logOf(m); !slices.Equal(got, want) {
			t.Errorf("tick order:\n got %q\nwant %q", got, want)
		}
	})
}

func TestComponentDiff(t *testing.T) {
	run := testWorld(t)
	run(func(tx *world.Tx) {
		m := spawnTest(t, tx)
		if got := *logOf(m); !slices.Equal(got, []string{"attach test:comp"}) {
			t.Fatalf("initial attach: %q", got)
		}
		if !m.HasComponent("test:unknown") || m.HasComponent("test:extra") {
			t.Fatal("HasComponent wrong")
		}
		if _, ok := m.Goals().Goal("test:goal"); !ok {
			t.Fatal("goal not added")
		}
		if UnsupportedSeen()["test:unknown"] == 0 {
			t.Fatal("unsupported key not recorded")
		}
		*logOf(m) = nil
		if !m.Fire("add_extra", nil) {
			t.Fatal("Fire reported no change")
		}
		m.Fire("change_comp", nil)
		m.Fire("remove_extra", nil)
		want := []string{"attach test:extra", "detach test:comp", "attach test:comp", "detach test:extra"}
		if got := *logOf(m); !slices.Equal(got, want) {
			t.Fatalf("diff:\n got %q\nwant %q", got, want)
		}
		if c, ok := ComponentOf[*testComp](m); !ok || c.v != 2 {
			t.Fatalf("ComponentOf: %v %v", c, ok)
		}
	})
}

type nbtConfig struct {
	t *Type
	m map[string]any
}

func (c nbtConfig) Apply(data *world.EntityData) { c.t.DecodeNBT(c.m, data) }

func TestNBTRoundTrip(t *testing.T) {
	run := testWorld(t)
	run(func(tx *world.Tx) {
		m := spawnTest(t, tx)
		m.SetMaxHealth(20)
		m.SetHealth(13)
		m.Fire("add_extra", nil)
		c, _ := m.Component("test:comp")
		c.(*testComp).saved = 42
		m.SetHome(cube.Pos{1, 2, 3})
		m.SetPersistent(true)

		enc := m.Type().EncodeNBT(&world.EntityData{Data: m.s})
		h := world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 10, 0.5}}.New(m.Type(), nbtConfig{m.Type(), enc})
		m2, _ := Of(tx.AddEntity(h))
		m2.SetMaxHealth(20) // what a health component would do on attach
		m2.Tick(tx, 1)
		if !m2.HasComponent("test:extra") {
			t.Error("groups were not restored")
		}
		if c, _ := m2.Component("test:comp"); c.(*testComp).saved != 42 {
			t.Error("component state was not restored")
		}
		if home, ok := m2.Home(); !ok || home != (cube.Pos{1, 2, 3}) || !m2.Persistent() {
			t.Error("home/persistent not restored")
		}
		if m2.Health() != 13 {
			t.Errorf("health %v, want 13", m2.Health())
		}
		if v, _ := m2.Property("test:p"); v == 7.0 {
			t.Error("a loaded mob fired entity_spawned")
		}
	})
}

func TestHurtAndDeath(t *testing.T) {
	run := testWorld(t)
	run(func(tx *world.Tx) {
		m := spawnTest(t, tx)
		m.SetMaxHealth(10)
		m.SetHealth(10)
		if n, res := m.Hurt(4, entity.VoidDamageSource{}); n != 4 || !res.Accepted() {
			t.Fatalf("hurt: %v %v", n, res)
		}
		if n, _ := m.Hurt(3, entity.VoidDamageSource{}); n != 0 {
			t.Fatalf("immunity let %v through", n)
		}
		if n, _ := m.Hurt(6, entity.VoidDamageSource{}); n != 2 {
			t.Fatalf("immunity: want only the 2 above the last hit, got %v", n)
		}
		m.Kill(entity.VoidDamageSource{})
		if !m.Dead() {
			t.Fatal("not dead")
		}
		h := m.H()
		for i := 0; i <= deathTicks; i++ {
			if mm, ok := OfHandle(tx, h); ok {
				mm.Tick(tx, int64(i))
			}
		}
		if _, ok := h.Entity(tx); ok {
			t.Fatal("dead mob was not removed")
		}
	})
}

func TestFallsWithoutPhysicsPackage(t *testing.T) {
	run := testWorld(t)
	run(func(tx *world.Tx) {
		m := spawnTest(t, tx)
		m.SetPosition(mgl64.Vec3{0.5, 12, 0.5})
		for i := 0; i < 40; i++ {
			m.Tick(tx, int64(i))
		}
		if y := m.Position()[1]; y < 9.99 || y > 10.01 || !m.OnGround() {
			t.Fatalf("mob did not land on the floor: y=%v onGround=%v", y, m.OnGround())
		}
	})
}

// clientView is the shape server/session and server/java/javasession probe
// (kept here so a rename on either side shows up as a failing build).
type clientView interface {
	Baby() bool
	Variant() int32
	MarkVariant() int32
	Colour() int32
	Colour2() int32
	MobColour() item.Colour
	MobColour2() item.Colour
	Sheared() bool
	Saddled() bool
	Tamed() bool
	Sitting() bool
	Chested() bool
	Charged() bool
	Powered() bool
	Ignited() bool
	FuseLength() time.Duration
	Climbing() bool
	WallClimbing() bool
	Shaking() bool
	Converting() bool
	Angry() bool
	Aggressive() bool
	Sneaking() bool
	UsingItem() bool
	TradeTier() int32
	Invisible() bool
	Gravityless() bool
	Immobile() bool
	Owner() *world.EntityHandle
	HeadYaw() float64
	BodyYaw() float64
	TargetEntity() *world.EntityHandle
	BodyArmour() item.Stack
	Absorption() float64
	ActorProperties() map[string]any
	Scale() float64
	HeldItems() (item.Stack, item.Stack)
}

var (
	_ clientView                                             = (*Mob)(nil)
	_ clientView                                             = (*state)(nil)
	_ interface{ EntityProperties() []world.EntityProperty } = (*Type)(nil)
	_ entity.Behaviour                                       = (*state)(nil)
)
