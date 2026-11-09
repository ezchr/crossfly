//go:build crossfly_full

package def_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/def"
	_ "github.com/df-mc/dragonfly/server/mob/filter"
	"github.com/df-mc/dragonfly/server/mob/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/go-gl/mathgl/mgl64"
)

var registerOnce sync.Once

// withWorld runs f in a flat stone world with every vanilla mob type
// registered and packages def + filter linked (through their register.go).
func withWorld(t *testing.T, f func(tx *world.Tx)) {
	t.Helper()
	registerOnce.Do(func() {
		existing := map[string]bool{}
		for _, et := range entity.DefaultRegistry.Types() {
			existing[et.EncodeEntity()] = true
		}
		if _, err := vanilla.Register(vanilla.Options{Skip: func(id string) bool { return existing[id] }}); err != nil {
			t.Fatal(err)
		}
	})
	reg := entity.DefaultRegistry.Config().New(append(entity.DefaultRegistry.Types(), mob.Types()...))
	w := world.Config{Entities: reg, Provider: world.NopProvider{}, Generator: world.NopGenerator{}}.New()
	defer w.Close()
	_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		for x := -8; x <= 8; x++ {
			for z := -8; z <= 8; z++ {
				tx.SetBlock(cube.Pos{x, 9, z}, block.Stone{}, nil)
			}
		}
		f(tx)
		return struct{}{}, nil
	})
}

func spawn(t *testing.T, tx *world.Tx, id string, pos mgl64.Vec3) *mob.Mob {
	t.Helper()
	m, err := mob.Spawn(tx, id, pos, mob.SpawnOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMobIntegration(t *testing.T) {
	withWorld(t, func(tx *world.Tx) {
		tx.World().SetTime(6000) // noon
		// The installed parser is this package's.
		cowType, ok := mob.TypeFor("minecraft:cow")
		if !ok {
			t.Fatal("no cow type")
		}
		if _, ok := cowType.Definition().(def.MobDefinition); !ok {
			t.Fatalf("definition parser not installed: %T", cowType.Definition())
		}

		// Climate variant from the real biome at the spawn position.
		for x := -8; x <= 8; x++ {
			for y := 0; y < 20; y++ {
				for z := -8; z <= 8; z++ {
					tx.SetBiome(cube.Pos{x, y, z}, biome.Desert{})
				}
			}
		}
		cow := spawn(t, tx, "minecraft:cow", mgl64.Vec3{0, 10, 0})
		if v, _ := cow.Property("minecraft:climate_variant"); v != "warm" {
			t.Fatalf("desert cow climate %v", v)
		}
		if !cow.HasComponent("minecraft:behavior.breed") && !cow.HasComponent("minecraft:is_baby") {
			t.Fatalf("spawned cow has neither adult nor baby components: %v", cow.Components())
		}

		// Filters through mob.CompileFilter see real mobs.
		zombie := spawn(t, tx, "minecraft:zombie", mgl64.Vec3{2, 10, 0})
		compile := func(s string) mob.Filter {
			f, err := mob.CompileFilter(json.RawMessage(s))
			if err != nil {
				t.Fatal(err)
			}
			return f
		}
		ctx := zombie.Ctx()
		ctx.Other = cow
		for _, c := range []struct {
			f    string
			want bool
		}{
			{`{"test":"is_family","subject":"other","value":"cow"}`, true},
			{`{"test":"is_family","value":"undead"}`, true},
			{`{"test":"enum_property","subject":"other","domain":"minecraft:climate_variant","value":"warm"}`, true},
			{`{"test":"has_biome_tag","value":"desert"}`, true},
			{`{"test":"has_component","value":"minecraft:transformation"}`, false},
			{`{"test":"in_water"}`, false},
			{`{"test":"is_difficulty","operator":"!=","value":"peaceful"}`, true},
			{`{"test":"is_brightness","operator":">","value":0.49}`, true},
			{`{"test":"is_target","subject":"other"}`, false},
		} {
			if got := compile(c.f).Test(ctx); got != c.want {
				t.Errorf("%s: %v, want %v", c.f, got, c.want)
			}
		}
		zombie.SetTarget(cow)
		if !compile(`{"test":"is_target","subject":"other"}`).Test(ctx) || !compile(`{"test":"has_target"}`).Test(ctx) {
			t.Error("target relation")
		}

		// Events through mob.Fire change the attached components.
		zombie.Fire("minecraft:start_transforming_into_drowned", nil)
		raw, ok := zombie.ComponentJSON("minecraft:timer")
		if !ok {
			t.Fatalf("zombie timer missing after start_transforming: %v", zombie.Components())
		}
		var timer struct {
			Time float64 `json:"time"`
		}
		_ = json.Unmarshal(raw, &timer)
		if timer.Time != 30 {
			t.Fatalf("timer %s", raw)
		}
		zombie.Fire("minecraft:convert_to_drowned", nil)
		if !zombie.HasComponent("minecraft:transformation") || zombie.HasComponent("minecraft:timer") {
			t.Fatalf("converted zombie: %v", zombie.Components())
		}

		// Trigger to "other" re-applies the other mob's components.
		sheep := spawn(t, tx, "minecraft:sheep", mgl64.Vec3{-2, 10, 0})
		sheep.Fire("spawn_adult", nil)
		inst := sheep.Instance().(*def.MobInstance)
		if !inst.HasComponent("minecraft:is_dyeable") {
			t.Fatalf("adult sheep: %v", sheep.Components())
		}
		sheep.Fire("minecraft:on_sheared", nil)
		if !sheep.HasComponent("minecraft:is_sheared") || sheep.HasComponent("minecraft:is_dyeable") {
			t.Fatalf("sheared sheep: %v", sheep.Components())
		}

		// Save and load the definition state.
		saved := sheep.Instance().EncodeNBT()
		fresh := sheep.Definition().NewInstance()
		fresh.DecodeNBT(saved)
		if len(fresh.Components()) != len(sheep.Instance().Components()) {
			t.Fatal("NBT round trip")
		}
		for i, c := range fresh.Components() {
			if c.Key != sheep.Instance().Components()[i].Key {
				t.Fatalf("component order after load: %v vs %v", c.Key, sheep.Instance().Components()[i].Key)
			}
		}

		// Spawn and tick every mob with the real parser and filters.
		var hs []*world.EntityHandle
		for i, id := range mob.Identifiers() {
			m, err := mob.Spawn(tx, id, mgl64.Vec3{float64(i%8) - 4, 10, float64(i/8%8) - 4}, mob.SpawnOptions{})
			if err != nil {
				t.Errorf("%s: %v", id, err)
				continue
			}
			hs = append(hs, m.H())
		}
		for tick := int64(0); tick < 20; tick++ {
			for _, h := range hs {
				if m, ok := mob.OfHandle(tx, h); ok {
					m.Tick(tx, tick)
				}
			}
		}
	})
	for k, err := range mob.FactoryErrors() {
		t.Errorf("factory error %v: %v", k, err)
	}
}
