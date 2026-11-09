//go:build crossfly_full

package boss_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	bc "github.com/df-mc/dragonfly/server/mob/component/boss"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestBossDefinitions: hud_range and should_darken_sky from the JSON, the
// wither's bar colour 6.
func TestBossDefinitions(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:boss")
	d := w.Spawn(bc.DragonID, mgl64.Vec3{0.5, 20, 0.5})
	wi := w.Spawn(bc.WitherID, mgl64.Vec3{5.5, 1, 0.5})
	w.Do(func(tx *world.Tx) {
		b, ok := bc.BossOf(mobOf(t, tx, d))
		if !ok || b.HudRange() != 125 || b.DarkensSky() || b.Colour() != bc.ColourPurple {
			t.Errorf("dragon boss %+v", b)
		}
		b, ok = bc.BossOf(mobOf(t, tx, wi))
		if !ok || b.HudRange() != 55 || !b.DarkensSky() || b.Colour() != bc.ColourRebeccaPurple {
			t.Errorf("wither boss %+v", b)
		}
	})
}

// TestBossBarViewers: a viewer within hud_range sees the bar with the
// health share, which updates on damage; out of range it is hidden.
func TestBossBarViewers(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:boss")
	p := w.AddPlayer("viewer", mgl64.Vec3{10.5, 1, 0.5})
	v, l := addViewer(t, w, p)
	r := w.Spawn(bc.WitherID, mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(bc.WitherSpawnFrames + 2)
	h := r.H()
	if f, ok := v.bar(h); !ok || f < 0.99 {
		t.Fatalf("bar %v shown %v after the charge", f, ok)
	}
	if v.colour != bc.ColourRebeccaPurple {
		t.Errorf("colour %v", v.colour)
	}
	w.Do(func(tx *world.Tx) {
		m := mobOf(t, tx, r)
		pl, _ := p.Entity(tx)
		m.Hurt(45, entity.AttackDamageSource{Attacker: pl})
	})
	w.Tick(1)
	if f, _ := v.bar(h); f > 0.95 {
		t.Errorf("bar %v after 45 damage of 450", f)
	}
	p.Teleport(mgl64.Vec3{200.5, 1, 0.5})
	w.Do(func(tx *world.Tx) {
		e, _ := p.Entity(tx)
		l.Move(tx, e.Position())
		l.Load(tx, 200)
	})
	w.Tick(25)
	if _, ok := v.bar(h); ok {
		t.Errorf("bar still shown 200 blocks away")
	}
}

// TestBossBarDragonDying: the dragon's bar shows 0 once its death
// animation starts.
func TestBossBarDragonDying(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:boss")
	r := w.Spawn(bc.DragonID, mgl64.Vec3{0.5, 20, 0.5})
	w.Tick(2)
	w.Do(func(tx *world.Tx) {
		m := mobOf(t, tx, r)
		bc.DragonOf(m).StartDeathAnimation(m)
	})
	w.Tick(1)
	w.Do(func(tx *world.Tx) {
		m := mobOf(t, tx, r)
		if !bc.DragonOf(m).Dying() || m.Health() != 0 {
			t.Errorf("dying %v health %v", bc.DragonOf(m).Dying(), m.Health())
		}
	})
	if _, ok := w.TickUntil(220, func(*world.Tx) bool { return r.Gone() }); !ok {
		t.Fatalf("not removed after the animation")
	}
	_ = mob.SoundDeath
}
