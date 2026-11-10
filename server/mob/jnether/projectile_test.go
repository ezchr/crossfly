//go:build crossfly_full

package jnether

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestHurtingInertia: AbstractHurtingProjectile.applyInertia, (v +
// dir x 0.1) x 0.95 each tick, so the speed tends to 1.9 blocks a tick
// (0.73 inertia for the blue skull: 0.27).
func TestHurtingInertia(t *testing.T) {
	w := newTestWorld(t, world.Overworld, world.DifficultyNormal)
	var h, blue *world.EntityHandle
	w.do(func(tx *world.Tx) {
		h = SpawnHurting(tx, SmallFireballType, HurtingOpts{Pos: mgl64.Vec3{0, 100, 0}, Dir: mgl64.Vec3{1, 0, 0}})
		blue = SpawnHurting(tx, DangerousWitherSkullType, HurtingOpts{Pos: mgl64.Vec3{0, 100, 5}, Dir: mgl64.Vec3{1, 0, 0}})
	})
	speed, blueSpeed := 0.1, 0.1
	for i := 1; i <= 30; i++ {
		w.tick(1)
		speed = (speed + 0.1) * 0.95
		blueSpeed = (blueSpeed + 0.1) * 0.73
		w.do(func(tx *world.Tx) {
			e, _ := projectileOf(tx, h)
			if e == nil {
				t.Fatalf("tick %d: fireball gone", i)
			}
			if got := e.Velocity().Len(); math.Abs(got-speed) > 1e-9 {
				t.Fatalf("tick %d: speed %v, want %v", i, got, speed)
			}
			b, _ := projectileOf(tx, blue)
			if got := b.Velocity().Len(); math.Abs(got-blueSpeed) > 1e-9 {
				t.Fatalf("tick %d: blue skull speed %v, want %v", i, got, blueSpeed)
			}
		})
	}
}

// TestSmallFireballSetsFire: a blaze's fireball hitting a wall sets fire on
// the face it hit and is gone.
func TestSmallFireballSetsFire(t *testing.T) {
	w := newTestWorld(t, world.Overworld, world.DifficultyNormal)
	w.fill(cube.Pos{5, 9, -2}, cube.Pos{5, 12, 2}, block.Stone{})
	// Fire needs something under it (BaseFireBlock.canSurvive).
	w.fill(cube.Pos{0, 9, -2}, cube.Pos{4, 9, 2}, block.Stone{})
	var h *world.EntityHandle
	w.do(func(tx *world.Tx) {
		h = SpawnHurting(tx, SmallFireballType, HurtingOpts{Pos: mgl64.Vec3{0, 10.2, 0.5}, Dir: mgl64.Vec3{1, 0, 0}, Incendiary: true})
	})
	w.tick(20)
	w.do(func(tx *world.Tx) {
		if e, _ := projectileOf(tx, h); e != nil {
			t.Fatal("fireball still flying")
		}
		if _, ok := tx.Block(cube.Pos{4, 10, 0}).(block.Fire); !ok {
			t.Fatalf("no fire in front of the wall: %#v", tx.Block(cube.Pos{4, 10, 0}))
		}
	})
}

// TestSmallFireballNoFireWithoutGriefing: isIncendiary false (a mob's
// fireball without mobGriefing) leaves no fire.
func TestSmallFireballNoFireWithoutGriefing(t *testing.T) {
	w := newTestWorld(t, world.Overworld, world.DifficultyNormal)
	w.fill(cube.Pos{5, 9, -2}, cube.Pos{5, 12, 2}, block.Stone{})
	w.do(func(tx *world.Tx) {
		SpawnHurting(tx, SmallFireballType, HurtingOpts{Pos: mgl64.Vec3{0, 10.2, 0.5}, Dir: mgl64.Vec3{1, 0, 0}})
	})
	w.tick(20)
	w.do(func(tx *world.Tx) {
		if _, ok := tx.Block(cube.Pos{4, 10, 0}).(block.Fire); ok {
			t.Fatal("fire from a non-incendiary fireball")
		}
	})
}

// TestSmallFireballHitsPlayer: 5 fire damage and 5 s of fire.
func TestSmallFireballHitsPlayer(t *testing.T) {
	w := newTestWorld(t, world.Overworld, world.DifficultyNormal)
	w.fill(cube.Pos{-10, 9, -10}, cube.Pos{10, 9, 10}, block.Stone{})
	p := w.addPlayer(mgl64.Vec3{5.5, 10, 0.5})
	w.do(func(tx *world.Tx) {
		SpawnHurting(tx, SmallFireballType, HurtingOpts{Pos: mgl64.Vec3{0, 11, 0.5}, Dir: mgl64.Vec3{1, 0, 0}})
	})
	w.tick(15)
	w.do(func(tx *world.Tx) {
		pl := w.player(tx, p)
		if pl.Health() > 15.01 || pl.Health() < 14.99 {
			t.Errorf("health %v, want 15", pl.Health())
		}
		if pl.OnFireDuration() <= 0 {
			t.Error("player not burning")
		}
	})
}

// TestLargeFireballExplodes: a ghast fireball breaks blocks where it lands
// (power 1, mobGriefing on).
func TestLargeFireballExplodes(t *testing.T) {
	w := newTestWorld(t, world.Overworld, world.DifficultyNormal)
	w.fill(cube.Pos{8, 7, -3}, cube.Pos{10, 13, 3}, block.Dirt{})
	w.do(func(tx *world.Tx) {
		SpawnHurting(tx, LargeFireballType, HurtingOpts{Pos: mgl64.Vec3{0, 10, 0.5}, Dir: mgl64.Vec3{1, 0, 0}, ExplosionPower: 1, Incendiary: true})
	})
	w.tick(20)
	broken := 0
	w.do(func(tx *world.Tx) {
		for y := 7; y <= 13; y++ {
			for z := -3; z <= 3; z++ {
				if _, ok := tx.Block(cube.Pos{8, y, z}).(block.Dirt); !ok {
					broken++
				}
			}
		}
	})
	if broken == 0 {
		t.Fatal("the explosion broke nothing")
	}
}

// TestLargeFireballDeflect: a player's hit sends the fireball where the
// player looks, makes the player its owner and resets the acceleration
// (Player.deflectProjectile, AbstractHurtingProjectile.onDeflection).
func TestLargeFireballDeflect(t *testing.T) {
	w := newTestWorld(t, world.Overworld, world.DifficultyNormal)
	w.fill(cube.Pos{-3, 9, -3}, cube.Pos{3, 9, 3}, block.Stone{})
	p := w.addPlayer(mgl64.Vec3{0.5, 10, 0.5})
	var h *world.EntityHandle
	w.do(func(tx *world.Tx) {
		h = SpawnHurting(tx, LargeFireballType, HurtingOpts{Pos: mgl64.Vec3{0.5, 11, 3}, Dir: mgl64.Vec3{0, 0, -1}})
		e, b := projectileOf(tx, h)
		b.accel = 0.05
		pl := w.player(tx, p)
		if _, ok, _ := entity.HurtEntity(e, 1, entity.AttackDamageSource{Attacker: pl}); !ok {
			t.Fatal("hit refused")
		}
		look := pl.Rotation().Vec3()
		if e.Velocity().Sub(look).Len() > 1e-9 {
			t.Errorf("velocity %v, want the look %v", e.Velocity(), look)
		}
		if b.owner != pl.H() {
			t.Error("owner is not the player")
		}
		if b.accel != 0.1 {
			t.Errorf("acceleration %v, want 0.1", b.accel)
		}
	})
}

// TestShulkerBulletHoming: the bullet finds a player around a corner and
// gives 4 damage and Levitation.
func TestShulkerBulletHoming(t *testing.T) {
	w := newTestWorld(t, world.Overworld, world.DifficultyNormal)
	w.fill(cube.Pos{-12, 9, -12}, cube.Pos{12, 9, 12}, block.Stone{})
	p := w.addPlayer(mgl64.Vec3{6.5, 10, 6.5})
	var h *world.EntityHandle
	w.do(func(tx *world.Tx) {
		pl := w.player(tx, p)
		h = SpawnShulkerBullet(tx, pl, pl, cube.Y)
		_, b := projectileOf(tx, h)
		b.owner = nil
		// Move it away from the player.
		b.data.Pos = mgl64.Vec3{-4.5, 11, -4.5}
		b.selectNextMoveDirection(tx, b.data.Pos, nil, pl)
	})
	hit := false
	for i := 0; i < 400 && !hit; i++ {
		w.tick(1)
		w.do(func(tx *world.Tx) {
			if e, _ := projectileOf(tx, h); e == nil {
				hit = true
			}
		})
	}
	if !hit {
		t.Fatal("bullet never arrived")
	}
	w.do(func(tx *world.Tx) {
		pl := w.player(tx, p)
		if _, ok := pl.Effect(effect.Levitation); !ok {
			t.Error("no levitation")
		}
		if pl.Health() > 16.01 {
			t.Errorf("health %v, want 16", pl.Health())
		}
	})
}

// TestComputeMargin: 0 for the first 2 ticks, then +0.05 a tick up to 0.3.
func TestComputeMargin(t *testing.T) {
	for _, c := range [][2]float64{{0, 0}, {2, 0}, {3, 0.05}, {8, 0.3}, {100, 0.3}} {
		if got := computeMargin(int(c[0])); math.Abs(got-c[1]) > 1e-9 {
			t.Errorf("margin(%v) = %v, want %v", c[0], got, c[1])
		}
	}
}
