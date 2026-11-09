//go:build crossfly_full

package mobtest_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/filter"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestHasComponentEffect: has_component minecraft:effect.X sees the active
// effects (ActorHasComponentTest), as the zombie villager cure needs.
func TestHasComponentEffect(t *testing.T) {
	mobtest.RequireFilters(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	zv := w.Spawn("minecraft:zombie_villager_v2", mgl64.Vec3{0.5, 1, 0.5})
	f, err := filter.Compile(json.RawMessage(`{"test":"has_component","value":"minecraft:effect.weakness"}`))
	if err != nil {
		t.Fatal(err)
	}
	w.Do(func(tx *world.Tx) {
		m, _ := zv.Mob(tx)
		if f.Test(m.Ctx()) {
			t.Fatal("has weakness before it got it")
		}
		m.AddEffect(effect.New(effect.Weakness, 1, 30*time.Second))
		if !f.Test(m.Ctx()) {
			t.Fatal("has_component minecraft:effect.weakness false with weakness")
		}
	})
}

// TestHasDamageFatal: has_damage fatal passes for damage at or above the
// health (DamageSensorComponent::recordGenericDamage), so a zombie killing
// a villager fires villager_v2's become_zombie damage_sensor trigger.
func TestHasDamageFatal(t *testing.T) {
	mobtest.RequireFilters(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{3.5, 1, 0.5})
	f, err := filter.Compile(json.RawMessage(`{"test":"has_damage","value":"fatal"}`))
	if err != nil {
		t.Fatal(err)
	}
	w.Do(func(tx *world.Tx) {
		m, _ := z.Mob(tx)
		ctx := m.Ctx()
		ctx.Damage, ctx.DamageFatal = entity.FallDamageSource{}, true
		if !f.Test(ctx) {
			t.Fatal("has_damage fatal false for fatal damage")
		}
		ctx.DamageFatal = false
		if f.Test(ctx) {
			t.Fatal("has_damage fatal true for non-fatal damage")
		}
	})
	_ = mob.Damage{}
}
