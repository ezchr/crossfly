//go:build crossfly_full

package mobtest_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/shulker"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// shulkerRoom is a closed stone room with a shulker on the floor at (0,1,0).
func shulkerRoom(t *testing.T) (*mobtest.World, *mobtest.Ref) {
	t.Helper()
	needs(t, true, false, "minecraft:peek")
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Fill(tx, cube.Pos{-10, 0, -10}, cube.Pos{10, 10, 10}, block.Stone{})
		w.Clear(tx, cube.Pos{-9, 1, -9}, cube.Pos{9, 9, 9})
	})
	s := w.Spawn(shulker.ID, mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(1)
	return w, s
}

func peekOf(t *testing.T, s *mobtest.Ref) (p *shulker.Peek) {
	t.Helper()
	s.With(func(m *mob.Mob) {
		var ok bool
		if p, ok = shulker.Of(m); !ok {
			t.Fatalf("shulker has no minecraft:peek")
		}
	})
	return p
}

// Hits, explosions and gravity never move an attached shulker; it does not
// get a velocity a client could predict either.
func TestShulkerStaysAttached(t *testing.T) {
	w, s := shulkerRoom(t)
	p := w.AddPlayer("hitter", mgl64.Vec3{2.5, 1, 0.5})
	s.With(func(m *mob.Mob) {
		m.KnockBack(m.Position().Add(mgl64.Vec3{1, 0, 0}), 2, 1)
		if v := m.Velocity(); v != (mgl64.Vec3{}) {
			t.Fatalf("knocked back: velocity %v", v)
		}
		m.SetVelocity(mgl64.Vec3{1, 0.5, 0}) // an explosion's push
	})
	_ = p
	w.Tick(10)
	if pos := s.Pos(); pos != (mgl64.Vec3{0.5, 1, 0.5}) {
		t.Fatalf("shulker moved to %v", pos)
	}
	s.With(func(m *mob.Mob) {
		if v := m.Velocity(); v != (mgl64.Vec3{}) {
			t.Fatalf("velocity %v after the push", v)
		}
	})

	// On the ceiling it does not fall.
	c := w.Spawn(shulker.ID, mgl64.Vec3{4.5, 9, 4.5})
	w.Tick(20)
	if pos := c.Pos(); pos != (mgl64.Vec3{4.5, 9, 4.5}) {
		t.Fatalf("ceiling shulker moved to %v", pos)
	}
	if f := peekOf(t, c).AttachFace(); f != cube.FaceUp {
		t.Fatalf("ceiling shulker attached to face %v, want up", f)
	}
	c.With(func(m *mob.Mob) {
		if v, _ := m.ActorData(protocol.EntityDataKeyAttachFace); v != byte(cube.FaceUp) {
			t.Fatalf("attach face actor data %v", v)
		}
	})
}

// Shut, the shulker ignores projectiles and has 20 armour (80% less damage
// from hits); open it takes the full hit.
func TestShulkerClosedShell(t *testing.T) {
	w, s := shulkerRoom(t)
	pl := w.AddPlayer("hitter", mgl64.Vec3{2.5, 1, 0.5})
	s.With(func(m *mob.Mob) {
		if !peekOf(t, s).Closed() {
			t.Skip("shulker peeked on its first tick")
		}
		pe, _ := pl.Entity(m.Tx())
		if n, _ := m.Hurt(5, entity.ProjectileDamageSource{Owner: pe}); n != 0 {
			t.Fatalf("closed shulker took %v from a projectile", n)
		}
		if n, _ := m.Hurt(10, entity.AttackDamageSource{Attacker: pe}); math.Abs(n-2) > 1e-9 {
			t.Fatalf("closed shulker took %v of a 10 hit, want 2", n)
		}
	})
	// A target opens it fully (on_target_open, peek 100).
	s.With(func(m *mob.Mob) {
		pe, _ := pl.Entity(m.Tx())
		m.SetTarget(pe)
	})
	w.Tick(1)
	if n := peekOf(t, s).PeekAmount(); n != 100 {
		t.Fatalf("peek %d with a target, want 100", n)
	}
	w.Tick(10) // past the hurt immunity
	s.With(func(m *mob.Mob) {
		pe, _ := pl.Entity(m.Tx())
		if n, _ := m.Hurt(5, entity.ProjectileDamageSource{Owner: pe}); n != 5 {
			t.Fatalf("open shulker took %v from a projectile, want 5", n)
		}
	})
	// The target gone, it shuts.
	s.With(func(m *mob.Mob) { m.ResetTarget() })
	if _, ok := w.TickUntil(5, func(*world.Tx) bool { return peekOf(t, s).Closed() }); !ok {
		t.Fatalf("shulker did not shut when the target went")
	}
}

// When the block it sits on goes, the shulker attaches to another face of
// its block, else teleports to a block it can attach to.
func TestShulkerReattachesAndTeleports(t *testing.T) {
	w, s := shulkerRoom(t)
	w.Do(func(tx *world.Tx) {
		// A pillar next to it, then the floor under it goes.
		tx.SetBlock(cube.Pos{1, 1, 0}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{0, 0, 0}, block.Air{}, nil)
		tx.SetBlock(cube.Pos{0, -1, 0}, block.Air{}, nil)
	})
	w.Tick(1)
	if f := peekOf(t, s).AttachFace(); f != cube.FaceEast {
		t.Fatalf("attached to face %v, want east (the pillar)", f)
	}
	if pos := s.Pos(); pos != (mgl64.Vec3{0.5, 1, 0.5}) {
		t.Fatalf("re-attached shulker moved to %v", pos)
	}
	w.Do(func(tx *world.Tx) { tx.SetBlock(cube.Pos{1, 1, 0}, block.Air{}, nil) })
	if _, ok := w.TickUntil(40, func(*world.Tx) bool { return s.Pos() != (mgl64.Vec3{0.5, 1, 0.5}) }); !ok {
		t.Fatalf("shulker with nothing to attach to did not teleport")
	}
	p := peekOf(t, s)
	pos := s.Pos()
	if b := cube.PosFromVec3(pos); b != p.AttachPos() || math.Abs(pos[0]-float64(b[0])-0.5) > 1e-9 {
		t.Fatalf("teleported to %v, attach position %v", pos, p.AttachPos())
	}
	w.Do(func(tx *world.Tx) {
		on := p.AttachPos().Side(p.AttachFace())
		if _, air := tx.Block(on).(block.Air); air {
			t.Fatalf("teleported next to air at %v (face %v)", on, p.AttachFace())
		}
	})
}

// Below half health a hit makes the shulker teleport 1 in 4 times. (Fire
// would too, but minecraft:fire_immune stops fire damage first.)
func TestShulkerTeleportsWhenHurt(t *testing.T) {
	w, s := shulkerRoom(t)
	s.With(func(m *mob.Mob) { m.SetHealth(m.MaxHealth() * 0.4) })
	for i := 0; i < 60 && s.Pos() == (mgl64.Vec3{0.5, 1, 0.5}); i++ {
		s.With(func(m *mob.Mob) {
			m.Heal(0.5, nil)
			m.Hurt(0.5, entity.AttackDamageSource{})
		})
		w.Tick(11)
	}
	if s.Pos() == (mgl64.Vec3{0.5, 1, 0.5}) {
		t.Fatalf("a hurt shulker below half health never teleported")
	}
}

// An open shulker hit by a shulker bullet teleports and leaves a new
// shulker behind (sure with no other shulker near, Shulker::_trySpawnShulker).
func TestShulkerBulletDuplicates(t *testing.T) {
	w, s := shulkerRoom(t)
	pl := w.AddPlayer("target", mgl64.Vec3{0.5, 1, 6.5})
	s.With(func(m *mob.Mob) {
		pe, _ := pl.Entity(m.Tx())
		m.SetTarget(pe)
	})
	w.Tick(1)
	shoot(t, w, s, pl, "minecraft:shulker_bullet")
	var bullet world.Entity
	w.Do(func(tx *world.Tx) {
		for e := range tx.Entities() {
			if e.H().Type().EncodeEntity() == "minecraft:shulker_bullet" {
				bullet = e
			}
		}
	})
	if bullet == nil {
		t.Fatalf("no bullet")
	}
	for i := 0; i < 40 && len(w.Mobs(shulker.ID)) < 2; i++ {
		s.With(func(m *mob.Mob) {
			pe, _ := pl.Entity(m.Tx())
			m.SetTarget(pe)
			b, _ := bullet.H().Entity(m.Tx())
			if b == nil {
				b = bullet
			}
			m.Heal(1, nil)
			m.Hurt(1, entity.ProjectileDamageSource{Projectile: b, Owner: pe})
		})
		w.Tick(11)
	}
	if n := len(w.Mobs(shulker.ID)); n < 2 {
		t.Fatalf("%d shulkers after bullet hits, want a second one", n)
	}
}
