package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// TestEggChicks checks the hatching rolls of a thrown egg: 1/8 for a chick, then 1/32 for four.
func TestEggChicks(t *testing.T) {
	for _, c := range []struct {
		r1, r2 float64
		want   int
	}{
		{0.5, 0, 0},
		{0.126, 0, 0},
		{0.125, 0.5, 1},
		{0, 0.04, 1},
		{0.1, 0.03125, 4},
		{0, 0, 4},
	} {
		if got := eggChicks(c.r1, c.r2); got != c.want {
			t.Errorf("eggChicks(%v, %v) = %v, want %v", c.r1, c.r2, got, c.want)
		}
	}
}

// TestEggVariantSaved checks that a thrown egg keeps its colour through saving, shows it as its
// minecraft:climate_variant property, and tells its item.
func TestEggVariantSaved(t *testing.T) {
	props := EggType.EntityProperties()
	if len(props) != 1 || props[0].Name != "minecraft:climate_variant" || !props[0].ClientSync || props[0].Default != "temperate" ||
		len(props[0].Enum) != 3 || props[0].Enum[1] != "warm" || props[0].Enum[2] != "cold" {
		t.Fatalf("egg properties %+v", props)
	}
	for _, v := range item.EggVariants() {
		data := &world.EntityData{}
		conf := eggConf
		conf.PickupItem = item.NewStack(item.Egg{Variant: v}, 1)
		data.Data = conf.New()
		if got := eggOf(data).Variant; got != v {
			t.Errorf("thrown %v egg is %v", v, got)
		}
		if p := data.Data.(*ProjectileBehaviour).ActorProperties(); p["minecraft:climate_variant"] != v.String() {
			t.Errorf("thrown %v egg has properties %v", v, p)
		}
		loaded := &world.EntityData{}
		m := EggType.EncodeNBT(data)
		if m == nil {
			m = map[string]any{}
		}
		EggType.DecodeNBT(m, loaded)
		if got := eggOf(loaded).Variant; got != v {
			t.Errorf("%v egg loads as %v (saved %v)", v, got, m)
		}
	}
	if p := (ProjectileBehaviourConfig{}).New().ActorProperties(); p != nil {
		t.Errorf("other projectiles have properties %v", p)
	}
}

// TestThrownEggItem checks NewEggOf and ThrownEggItem through a world.
func TestThrownEggItem(t *testing.T) {
	w := world.Config{Synchronous: true, Entities: DefaultRegistry, Provider: world.NopProvider{}, Generator: world.NopGenerator{}}.New()
	defer func() { _ = w.Close() }()
	w.Do(func(tx *world.Tx) {
		blue := tx.AddEntity(NewEggOf(world.EntitySpawnOpts{}, item.Egg{Variant: item.EggCold}, nil))
		if s, ok := ThrownEggItem(blue); !ok || s.Item() != (item.Egg{Variant: item.EggCold}) {
			t.Errorf("thrown blue egg has item %v, %v", s, ok)
		}
		white := tx.AddEntity(NewEgg(world.EntitySpawnOpts{}, nil))
		if s, ok := ThrownEggItem(white); !ok || s.Item() != (item.Egg{}) {
			t.Errorf("thrown white egg has item %v, %v", s, ok)
		}
		ball := tx.AddEntity(NewSnowball(world.EntitySpawnOpts{}, nil))
		if _, ok := ThrownEggItem(ball); ok {
			t.Error("a snowball is a thrown egg")
		}
	})
}
