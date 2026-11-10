//go:build crossfly_full

package social_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/tame"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/social"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// ambientOf asks the ambient override for the parrot behind r.
func ambientOf(t *testing.T, w *mobtest.World, r *mobtest.Ref) (mob.Sound, bool) {
	t.Helper()
	var (
		s  mob.Sound
		ok bool
	)
	w.Do(func(tx *world.Tx) { s, ok = mob.AmbientOverride(mobOf(t, tx, r), "ambient") })
	return s, ok
}

// TestParrotImitates: a parrot's ambient sound for Java viewers is, one time in 50, the imitation
// of a random monster within 20 blocks (ImitateMobSoundsComponent::imitateNearbyMobs), else one
// time in 1000 a random imitation (Parrot::getAmbientSound), else the plain sound.
func TestParrotImitates(t *testing.T) {
	w := setup(t, mobtest.Options{Time: mobtest.Noon})
	par := w.Spawn("minecraft:parrot", mgl64.Vec3{0.5, 1, 0.5})
	cow := w.Spawn("minecraft:cow", mgl64.Vec3{4.5, 1, 0.5})
	w.Tick(1)

	// Every roll hits: no monster near, so the 1 in 1000 random imitation (the first of the table).
	restore := social.SetParrotRand(func(int) int { return 0 })
	defer restore()
	if s, ok := ambientOf(t, w, par); !ok || s.Event != "imitate.blaze" || !s.ClientSide || s.Identifier != "minecraft:parrot" {
		t.Fatalf("parrot with only a cow near plays %+v (%v), want the random imitate.blaze for Java only", s, ok)
	}

	// A zombie 10 blocks away is imitated.
	w.Spawn("minecraft:zombie", mgl64.Vec3{10.5, 1, 0.5})
	w.Tick(1)
	if s, ok := ambientOf(t, w, par); !ok || s.Event != "imitate.zombie" {
		t.Fatalf("parrot near a zombie plays %+v (%v), want imitate.zombie", s, ok)
	}

	// No roll hits: the plain ambient sound.
	social.SetParrotRand(func(n int) int { return n - 1 })
	if s, ok := ambientOf(t, w, par); ok {
		t.Fatalf("parrot imitated %v although no roll hit", s.Event)
	}
	// Other mobs are left alone.
	if _, ok := ambientOf(t, w, cow); ok {
		t.Fatal("cow got an ambient override")
	}
}

// tameParrot spawns a parrot tamed by the player behind p.
func tameParrot(t *testing.T, w *mobtest.World, p *mobtest.Player, pos mgl64.Vec3) *mobtest.Ref {
	t.Helper()
	par := w.Spawn("minecraft:parrot", pos)
	w.Tick(2)
	w.Do(func(tx *world.Tx) {
		m := mobOf(t, tx, par)
		e, _ := p.Entity(tx)
		m.Fire("minecraft:on_tame", nil)
		m.SetOwner(e)
	})
	return par
}

// TestParrotShoulderInvulnerable: a parrot on a shoulder takes no damage but the void's
// (Parrot::isInvulnerableTo).
func TestParrotShoulderInvulnerable(t *testing.T) {
	w := setup(t, mobtest.Options{Time: mobtest.Noon})
	p := w.AddPlayer("p", mgl64.Vec3{1.5, 1, 0.5})
	par := tameParrot(t, w, p, mgl64.Vec3{0.5, 1, 0.5})
	w.Do(func(tx *world.Tx) {
		m := mobOf(t, tx, par)
		e, _ := p.Entity(tx)
		if !m.StartRiding(e) {
			t.Fatal("parrot did not get on the shoulder")
		}
		pl := e.(*player.Player)
		before := m.Health()
		m.Hurt(4, entity.AttackDamageSource{Attacker: pl})
		m.Hurt(4, entity.FallDamageSource{})
		if m.Health() != before {
			t.Fatalf("parrot on a shoulder went from %v to %v health", before, m.Health())
		}
		m.Hurt(4, entity.VoidDamageSource{})
		if m.Health() >= before {
			t.Fatal("void did not hurt the parrot on a shoulder")
		}
		m.StopRiding()
	})
	w.Tick(20) // past the hurt cooldown
	w.Do(func(tx *world.Tx) {
		m := mobOf(t, tx, par)
		e, _ := p.Entity(tx)
		before := m.Health()
		m.Hurt(10, entity.AttackDamageSource{Attacker: e.(*player.Player)})
		if m.Health() >= before {
			t.Fatal("parrot off the shoulder took no damage")
		}
	})
}

// TestParrotSitsOnlyOnTheGround: a sitting parrot stands up once it is off the ground
// (Parrot::postAiStep), but keeps sitting on the ground.
func TestParrotSitsOnlyOnTheGround(t *testing.T) {
	w := setup(t, mobtest.Options{Time: mobtest.Noon})
	p := w.AddPlayer("p", mgl64.Vec3{3.5, 1, 0.5})
	par := tameParrot(t, w, p, mgl64.Vec3{0.5, 1, 0.5})
	w.Do(func(tx *world.Tx) {
		m := mobOf(t, tx, par)
		s, ok := tame.SittableOf(m)
		if !ok {
			t.Fatal("tamed parrot has no sittable")
		}
		s.SetSitting(m, true)
	})
	w.Tick(40)
	par.With(func(m *mob.Mob) {
		if !m.Sitting() {
			t.Fatalf("parrot sitting on the ground stood up: %v", w.Dump(par))
		}
		m.Teleport(mgl64.Vec3{0.5, 3.5, 0.5})
	})
	w.Tick(1)
	par.With(func(m *mob.Mob) {
		if m.Sitting() {
			t.Fatal("parrot sits in mid-air")
		}
		if s, _ := tame.SittableOf(m); s.Sitting() {
			t.Error("sittable still sitting in mid-air")
		}
	})
}
