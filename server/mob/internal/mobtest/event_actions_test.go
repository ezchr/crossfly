//go:build crossfly_full

package mobtest_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// eaViewer records the mob sounds (event, entity type) and particles it is
// shown.
type eaViewer struct {
	world.NopViewer
	sounds    [][2]string
	particles []world.Particle
}

func (v *eaViewer) ViewSound(_ mgl64.Vec3, s world.Sound) {
	if ls, ok := s.(interface {
		LevelSoundEvent() (string, string, bool)
	}); ok {
		ev, typ, _ := ls.LevelSoundEvent()
		v.sounds = append(v.sounds, [2]string{ev, typ})
	}
}

func (v *eaViewer) ViewParticle(_ mgl64.Vec3, p world.Particle) { v.particles = append(v.particles, p) }

func (v *eaViewer) heard(ev, typ string) bool {
	for _, s := range v.sounds {
		if s == [2]string{ev, typ} {
			return true
		}
	}
	return false
}

// TestEventActions: the play_sound, emit_particle and emit_vibration event
// responses (PlaySoundResponse, EmitVibrationResponse): a saddled horse
// plays saddle, a crumbling creaking plays death, shows the creakingcrumble
// particle and emits entity_die (which component/boss passes to the
// vibration listeners nearby).
func TestEventActions(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 12, block.Stone{}) })
	v := &eaViewer{}
	w.Do(func(tx *world.Tx) {
		l := world.NewLoader(4, tx.World(), v)
		l.Move(tx, mgl64.Vec3{0.5, 1, 0.5})
		l.Load(tx, 1000)
	})
	h := w.Spawn("minecraft:horse", mgl64.Vec3{0.5, 1, 0.5})
	c := w.Spawn("minecraft:creaking", mgl64.Vec3{4.5, 1, 0.5})
	w.Tick(2)
	vibrations = nil
	w.Do(func(tx *world.Tx) {
		hm, _ := h.Mob(tx)
		hm.Fire("minecraft:horse_saddled", nil)
		cm, _ := c.Mob(tx)
		cm.Fire("minecraft:crumble", nil)
	})
	if !v.heard("saddle", "minecraft:horse") {
		t.Errorf("no saddle sound from the horse: %v", v.sounds)
	}
	if !v.heard("death", "minecraft:creaking") {
		t.Errorf("no death sound from the crumbling creaking: %v", v.sounds)
	}
	crumble := false
	for _, p := range v.particles {
		if l, ok := p.(particle.Legacy); ok && l.Bedrock == packet.ParticleTypeCreakingCrumble {
			crumble = true
		}
	}
	if !crumble {
		t.Errorf("no creakingcrumble particle: %v", v.particles)
	}
	found := false
	for _, e := range vibrations {
		if e == "minecraft:creaking entity_die" {
			found = true
		}
	}
	if !found {
		t.Errorf("no entity_die vibration from the creaking: %v", vibrations)
	}
}

// vibrations records the game events mobs emit (Mob.EmitVibration).
var vibrations []string

func init() {
	mob.RegisterVibrationHook(func(m *mob.Mob, event string) {
		vibrations = append(vibrations, m.Identifier()+" "+event)
	})
}
