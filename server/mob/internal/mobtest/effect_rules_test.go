//go:build crossfly_full

package mobtest_test

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestEffectImmunities: Actor::canBeAffected and its Spider, Skeleton and
// WitherBoss overrides.
func TestEffectImmunities(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 12, block.Stone{}) })
	ids := []string{"minecraft:zombie", "minecraft:skeleton", "minecraft:spider", "minecraft:cave_spider",
		"minecraft:wither_skeleton", "minecraft:wither", "minecraft:cow"}
	refs := map[string]*mobtest.Ref{}
	for i, id := range ids {
		refs[id] = w.Spawn(id, mgl64.Vec3{float64(i*3) - 9, 1, 0.5})
	}
	lasting := map[string]effect.LastingType{
		"poison": effect.Poison, "regeneration": effect.Regeneration, "wither": effect.Wither,
		"speed": effect.Speed, "slowness": effect.Slowness,
	}
	immune := map[string][]string{
		"minecraft:zombie":          {"poison", "regeneration"},
		"minecraft:skeleton":        {"poison", "regeneration"},
		"minecraft:spider":          {"poison"},
		"minecraft:cave_spider":     {"poison"},
		"minecraft:wither_skeleton": {"poison", "regeneration", "wither"},
		"minecraft:wither":          {"poison", "regeneration", "wither", "speed", "slowness"},
		"minecraft:cow":             {},
	}
	w.Do(func(tx *world.Tx) {
		for _, id := range ids {
			m, ok := refs[id].Mob(tx)
			if !ok {
				t.Fatalf("%s did not spawn", id)
			}
			for name, typ := range lasting {
				m.AddEffect(effect.New(typ, 1, 10*time.Second))
				_, has := m.Effect(typ)
				want := true
				for _, n := range immune[id] {
					if n == name {
						want = false
					}
				}
				if has != want {
					t.Errorf("%s with %s: has %v, want %v", id, name, has, want)
				}
				m.RemoveEffect(typ)
			}
		}
	})
}

// TestInvertedHealAndHarm: instant health hurts undead mobs and instant
// damage heals them (Actor::isInvertedHealAndHarm); other mobs are healed
// and hurt as usual.
func TestInvertedHealAndHarm(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 12, block.Stone{}) })
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{0.5, 1, 0.5})
	c := w.Spawn("minecraft:cow", mgl64.Vec3{4.5, 1, 0.5})
	wi := w.Spawn("minecraft:wither", mgl64.Vec3{-6.5, 1, 0.5})
	w.Do(func(tx *world.Tx) {
		zm, _ := z.Mob(tx)
		cm, _ := c.Mob(tx)
		wm, _ := wi.Mob(tx)
		if !zm.InvertedHealAndHarm() || cm.InvertedHealAndHarm() || !wm.InvertedHealAndHarm() {
			t.Fatal("InvertedHealAndHarm: zombie and wither undead, cow not")
		}
		zm.AddEffect(effect.NewInstant(effect.InstantHealth, 1)) // 4
		if got := zm.MaxHealth() - zm.Health(); got != 4 {
			t.Errorf("instant health I took %v from a zombie, want 4", got)
		}
		zm.AddEffect(effect.NewInstant(effect.InstantDamage, 1)) // heals 6
		if zm.Health() != zm.MaxHealth() {
			t.Errorf("instant damage left the zombie at %v/%v, want healed", zm.Health(), zm.MaxHealth())
		}
		cm.AddEffect(effect.NewInstant(effect.InstantDamage, 1))
		if got := cm.MaxHealth() - cm.Health(); got != 6 {
			t.Errorf("instant damage I took %v from a cow, want 6", got)
		}
	})
}
