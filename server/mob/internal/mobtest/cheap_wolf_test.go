//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// shakeViewer records the actor events and mob sounds it is shown.
type shakeViewer struct {
	world.NopViewer
	events []uint8
	sounds []string
	java   []bool
}

func (v *shakeViewer) ViewEntityAction(_ world.Entity, a world.EntityAction) {
	if ae, ok := a.(interface{ ActorEvent() uint8 }); ok {
		v.events = append(v.events, ae.ActorEvent())
	}
}

func (v *shakeViewer) ViewSound(_ mgl64.Vec3, s world.Sound) {
	if ls, ok := s.(interface {
		LevelSoundEvent() (string, string, bool)
	}); ok {
		ev, _, _ := ls.LevelSoundEvent()
		v.sounds = append(v.sounds, ev)
		cs, _ := s.(interface{ BedrockClientSide() bool })
		v.java = append(v.java, cs != nil && cs.BedrockClientSide())
	}
}

func (v *shakeViewer) count(ev uint8) int {
	n := 0
	for _, e := range v.events {
		if e == ev {
			n++
		}
	}
	return n
}

// TestWolfShakesDry: Wolf::postNormalTick makes a wolf in water wet;
// Wolf::newServerAiStep makes it shake once it is out on the ground with
// nowhere to walk (ActorEvent 8 and, for Java, the shake sound); after the
// 40-tick shake it is dry and does not shake again.
func TestWolfShakesDry(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	v := &shakeViewer{}
	w.Do(func(tx *world.Tx) {
		// Rain would wet it again.
		tx.World().StopWeatherCycle()
		tx.World().StopRaining()
		w.Floor(tx, 0, 10, block.Stone{})
		// A pool sunk into the floor, so the water does not spread.
		w.Fill(tx, cube.Pos{-8, -1, -2}, cube.Pos{-4, -1, 2}, block.Stone{})
		w.Water(tx, cube.Pos{-8, 0, -2}, cube.Pos{-4, 0, 2})
		l := world.NewLoader(4, tx.World(), v)
		l.Move(tx, mgl64.Vec3{0.5, 1, 0.5})
		l.Load(tx, 1000)
	})
	wolf := w.Spawn("minecraft:wolf", mgl64.Vec3{-5.5, 0, 0.5})
	stay := func(tx *world.Tx) {
		m, _ := wolf.Mob(tx)
		m.Navigation().Stop(m)
	}
	for i := 0; i < 20; i++ {
		w.Do(stay)
		w.Tick(1)
	}
	// Standing on the floor of shallow water it may start shakes that the
	// water stops at once (BDS does the same); count from leaving it.
	n0 := v.count(packet.ActorEventShakeWetness)
	v.sounds, v.java = nil, nil
	wolf.With(func(m *mob.Mob) {
		m.Teleport(mgl64.Vec3{3.5, 1, 0.5})
		m.SetVelocity(mgl64.Vec3{})
	})
	shook := false
	for i := 0; i < 100 && !shook; i++ {
		w.Do(stay)
		w.Tick(1)
		shook = v.count(packet.ActorEventShakeWetness) == n0+1
	}
	if !shook {
		t.Fatalf("wet wolf never shook on dry ground: %v", w.Dump(wolf))
	}
	for i := 0; i < 100; i++ {
		w.Do(stay)
		w.Tick(1)
	}
	if n := v.count(packet.ActorEventShakeWetness) - n0; n != 1 {
		t.Errorf("wolf shook %d times, want once", n)
	}
	found := false
	for i, s := range v.sounds {
		if s == "shake" {
			found = true
			if !v.java[i] {
				t.Errorf("shake sound not marked client-side for Bedrock")
			}
		}
	}
	if !found {
		t.Errorf("no shake sound: %v", v.sounds)
	}
}

// TestWolfCatAmbientSounds: Wolf::getAmbientSound and Cat::getAmbientSound
// pick growl, whine, pant, purr, purreow and the tame meow over the
// definition's ambient sound.
func TestWolfCatAmbientSounds(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 6, block.Stone{}) })
	wolf := w.Spawn("minecraft:wolf", mgl64.Vec3{0.5, 1, 0.5})
	cat := w.Spawn("minecraft:cat", mgl64.Vec3{3.5, 1, 0.5})
	w.Tick(2)
	roll := func(m *mob.Mob) map[string]int {
		seen := map[string]int{}
		for i := 0; i < 400; i++ {
			seen[core.ClassAmbientEvent(m, "ambient")]++
		}
		return seen
	}
	only := func(name string, got map[string]int, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Errorf("%v: got %v, want %v", name, got, want)
			return
		}
		for _, k := range want {
			if got[k] == 0 {
				t.Errorf("%v: got %v, want %v", name, got, want)
			}
		}
	}
	wolf.With(func(m *mob.Mob) {
		// A wild wolf has 8 health, but only tame wolves whine.
		only("wild wolf", roll(m), "ambient", "pant")
		m.UpdateMeta(func(meta *mob.Meta) { meta.Angry = true })
		only("angry wolf", roll(m), "growl")
		m.UpdateMeta(func(meta *mob.Meta) { meta.Angry, meta.Tamed = false, true })
		m.SetMaxHealth(40)
		m.SetHealth(40)
		only("tame wolf", roll(m), "ambient", "pant")
		m.SetHealth(5)
		only("hurt tame wolf", roll(m), "ambient", "whine")
	})
	cat.With(func(m *mob.Mob) {
		only("stray cat", roll(m), "ambient")
		m.UpdateMeta(func(meta *mob.Meta) { meta.Tamed = true })
		only("tame cat", roll(m), "ambient.tame", "purreow")
		m.UpdateMeta(func(meta *mob.Meta) { meta.InLove = true })
		only("cat in love", roll(m), "purr")
	})
}
