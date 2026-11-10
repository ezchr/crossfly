//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// ambientData returns actor data 108-110 of m.
func ambientData(m *mob.Mob) (interval, rng float32, event string) {
	v, _ := m.ActorData(protocol.EntityDataKeyAmbientSoundInterval)
	interval, _ = v.(float32)
	v, _ = m.ActorData(protocol.EntityDataKeyAmbientSoundIntervalRange)
	rng, _ = v.(float32)
	v, _ = m.ActorData(protocol.EntityDataKeyAmbientSoundEventName)
	event, _ = v.(string)
	return
}

// TestAmbientActorData: every mob carries the ambient sound actor data the
// Bedrock client plays its ambient sound from: the ambient_sound_interval
// values, or the reloadHardcoded / defineEntityData defaults.
func TestAmbientActorData(t *testing.T) {
	needs(t, true, true, "minecraft:ambient_sound_interval")
	// At noon: at night an overworld bee seeks shelter on its first tick (shelter_detection), which swaps
	// look_for_food's ambient.pollinate for default_sound.
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 12, block.Stone{}) })
	cases := []struct {
		id       string
		interval float32
		rng      float32
		event    string
	}{
		{"minecraft:chicken", 6, 16, "ambient"},      // chicken.json
		{"minecraft:bee", 2, 3, "ambient.pollinate"}, // bee.json
		{"minecraft:cow", 12, 16, "ambient"},         // Animal::reloadHardcoded
		{"minecraft:llama", 12, 16, "ambient"},       // Animal::reloadHardcoded
		{"minecraft:zombie", 8, 16, "ambient"},       // defineEntityData
		{"minecraft:squid", 6, 16, "ambient"},        // Squid::reloadHardcoded
		{"minecraft:ghast", 5, 16, "ambient"},        // Ghast::reloadHardcoded
		{"minecraft:villager_v2", 8, 16, "ambient"},  // defineEntityData
	}
	refs := make([]*mobtest.Ref, len(cases))
	for i, c := range cases {
		refs[i] = w.Spawn(c.id, mgl64.Vec3{float64(i*3) - 10.5, 1, 0.5})
	}
	w.Tick(1)
	for i, c := range cases {
		refs[i].With(func(m *mob.Mob) {
			in, rg, ev := ambientData(m)
			if in != c.interval || rg != c.rng || ev != c.event {
				t.Errorf("%v: ambient data %v/%v/%q, want %v/%v/%q", c.id, in, rg, ev, c.interval, c.rng, c.event)
			}
		})
	}
}

// TestAmbientEventNames: event_names update actor data 110 every
// round(value*10) ticks (AmbientSoundServerSystem): a chick says
// ambient.baby, and every ambient sound the server plays is client-side
// (only Java viewers get it).
func TestAmbientEventNames(t *testing.T) {
	needs(t, true, true, "minecraft:ambient_sound_interval")
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	chick := w.Spawn("minecraft:chicken", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: mob.EventBorn})
	w.Tick(62)
	chick.With(func(m *mob.Mob) {
		if !m.Baby() {
			t.Skip("born chicken is no chick")
		}
		if _, _, ev := ambientData(m); ev != "ambient.baby" {
			t.Errorf("chick ambient event %q, want ambient.baby", ev)
		}
	})
}

// TestIntervalAmbientClientSide: mobs with ambient_sound_interval (chickens:
// 6 + random(16) s, 14 s on average) play their ambient sound only for Java
// viewers too; the Bedrock client plays it from the actor data.
func TestIntervalAmbientClientSide(t *testing.T) {
	needs(t, true, true, "minecraft:ambient_sound_interval")
	n := ambientRun(t, func(w *mobtest.World) {
		for i := range 10 {
			w.Spawn("minecraft:chicken", mgl64.Vec3{float64(i) - 4.5, 1, -3.5}, mob.SpawnOptions{Event: "minecraft:spawn_adult"})
		}
	})
	// Expected 10 chickens * 400 s / 14 s = 285.
	if c := n["minecraft:chicken ambient"] + n["minecraft:chicken ambient.baby"]; c < 200 || c > 380 {
		t.Errorf("chickens clucked %v times in 400 s, want about 285 (all: %v)", c, n)
	}
}
