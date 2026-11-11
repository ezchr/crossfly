//go:build crossfly_full

package jnether

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/jbrain"
	"github.com/df-mc/dragonfly/server/mob/jbrain/jmob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

func spawnHoglin(t *testing.T, w *mobtest.World, pos mgl64.Vec3, immune bool) *mobtest.Ref {
	r := spawnJ(t, w, HoglinID, pos)
	withJ(t, w, r, func(m *javamob.Mob) {
		h := m.Behaviour().(*Hoglin)
		h.A.SetBaby(false)
		m.SetBaby(false)
		h.SetImmuneToZombification(immune)
	})
	return r
}

// TestHoglinAttacksPlayer: an adult hoglin hunts a nearby player.
func TestHoglinAttacksPlayer(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 24, block.Stone{}) })
	p := w.AddPlayer("tester", mgl64.Vec3{0.5, 1, 0.5})
	p.SetMaxHealth(1000)
	spawnHoglin(t, w, mgl64.Vec3{8.5, 1, 0.5}, true)
	start := p.Health()
	if _, ok := w.TickUntil(300, func(*world.Tx) bool { return p.Health() < start }); !ok {
		t.Fatal("the hoglin never hit the player")
	}
}

// TestHoglinRepellent: a repellent block near the hoglin pacifies it.
func TestHoglinRepellent(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 24, block.Stone{})
		// Crossfly has no warped fungus block; a respawn anchor is in the
		// same tag.
		tx.SetBlock(cube.Pos{2, 1, 0}, block.RespawnAnchor{}, nil)
	})
	p := w.AddPlayer("tester", mgl64.Vec3{0.5, 1, 6.5})
	p.SetMaxHealth(1000)
	r := spawnHoglin(t, w, mgl64.Vec3{0.5, 1, 0.5}, true)
	w.Tick(60)
	withJ(t, w, r, func(m *javamob.Mob) {
		mem := jmob.Mem(m)
		// It walks away from the repellent, so NEAREST_REPELLENT may be
		// gone again; PACIFIED (200 ticks) shows it was sensed.
		if !mem.Has(jbrain.MemPacified.ID()) {
			t.Error("not pacified next to a repellent")
		}
		if mem.Has(jbrain.MemAttackTarget.ID()) {
			t.Error("a pacified hoglin targets the player")
		}
	})
}

// TestHoglinBreeding: two hoglins in love make a persistent baby
// (AnimalMakeLove on the hoglin brain). Crossfly has no crimson fungus item
// yet, so love is set directly.
func TestHoglinBreeding(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 24, block.Stone{}) })
	a := spawnHoglin(t, w, mgl64.Vec3{0.5, 1, 0.5}, true)
	b := spawnHoglin(t, w, mgl64.Vec3{2.5, 1, 0.5}, true)
	for _, r := range []*mobtest.Ref{a, b} {
		withJ(t, w, r, func(m *javamob.Mob) { m.Behaviour().(*Hoglin).A.SetInLove(uuid.New()) })
	}
	var baby *javamob.Mob
	find := func() {
		for _, r := range w.Entities(func(e world.Entity) bool { return e.H().Type().EncodeEntity() == HoglinID }) {
			withJ(t, w, r, func(m *javamob.Mob) {
				if m.Baby() {
					baby = m
					if !m.PersistenceRequired() {
						t.Error("bred hoglin is not persistent")
					}
				}
			})
		}
	}
	for i := 0; i < 400 && baby == nil; i++ {
		w.Tick(1)
		find()
	}
	if baby == nil {
		t.Fatal("no baby hoglin")
	}
}

// TestHoglinZombifies: outside the Nether a hoglin turns into a zoglin
// after 300 ticks; an immune one does not.
func TestHoglinZombifies(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	spawnHoglin(t, w, mgl64.Vec3{0.5, 1, 0.5}, false)
	spawnHoglin(t, w, mgl64.Vec3{4.5, 1, 4.5}, true)
	w.Tick(310)
	count := func(id string) int {
		return len(w.Entities(func(e world.Entity) bool { return e.H().Type().EncodeEntity() == id }))
	}
	if count(ZoglinID) != 1 || count(HoglinID) != 1 {
		t.Errorf("zoglins %d hoglins %d, want 1 and 1", count(ZoglinID), count(HoglinID))
	}
}
