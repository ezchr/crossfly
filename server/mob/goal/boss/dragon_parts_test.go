//go:build crossfly_full

package boss_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	bc "github.com/df-mc/dragonfly/server/mob/component/boss"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func boxCentre(b cube.BBox) mgl64.Vec3 { return b.Min().Add(b.Max()).Mul(0.5) }

// TestDragonParts: the eight part boxes (EnderDragon sub-AABBs) have the BDS
// sizes, the head and neck lie ahead of the body and the three tail segments
// behind it, each further back.
func TestDragonParts(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:boss")
	r := spawnDragon(t, w, mgl64.Vec3{0.5, 40, 0.5})
	w.Tick(3)
	with(t, w, r, func(tx *world.Tx, m *mob.Mob) {
		d := bc.DragonOf(m)
		sizes := [...]mgl64.Vec3{{1, 1, 1}, {3, 3, 3}, {5, 3, 5}, {2, 2, 2}, {2, 2, 2}, {2, 2, 2}, {4, 3, 4}, {4, 3, 4}}
		for i, want := range sizes {
			b := d.Part(i)
			got := b.Max().Sub(b.Min())
			if got.Sub(want).Len() > 1e-9 {
				t.Errorf("part %d is %v, want %v", i, got, want)
			}
		}
		fwd := mgl64.Vec3{math.Sin(mgl64.DegToRad(m.Yaw())), 0, -math.Cos(mgl64.DegToRad(m.Yaw()))}
		along := func(i int) float64 {
			c := boxCentre(d.Part(i)).Sub(m.Position())
			c[1] = 0
			return c.Dot(fwd)
		}
		if along(bc.PartHead) < 6 || along(bc.PartNeck) < 5 {
			t.Errorf("head %.2f, neck %.2f ahead of the dragon, want 6.5 and 5.5", along(bc.PartHead), along(bc.PartNeck))
		}
		prev := 0.0
		for i := bc.PartTail1; i <= bc.PartTail3; i++ {
			a := along(i)
			if a >= prev {
				t.Errorf("tail segment %d at %.2f is not behind %.2f", i-bc.PartTail1, a, prev)
			}
			prev = a
		}
	})
}

// lookAt puts the player 3 blocks beside target, within reach, looking at it.
func lookAt(pl *player.Player, target mgl64.Vec3) {
	pl.Teleport(target.Add(mgl64.Vec3{3, -pl.EyeHeight(), 0.5}))
	v := target.Sub(pl.Position().Add(mgl64.Vec3{0, pl.EyeHeight()}))
	yaw := mgl64.RadToDeg(math.Atan2(-v[0], v[2]))
	pitch := -mgl64.RadToDeg(math.Asin(v[1] / v.Len()))
	rot := pl.Rotation()
	pl.Move(mgl64.Vec3{}, yaw-rot[0], pitch-rot[1])
}

// TestDragonPartDamage: EnderDragon::_hurt(AABB*) gives the head and neck
// the full damage and every other part min(d,1)+d/4. A Bedrock player's hit
// lands on the part in its line of sight, a Java client's on the part entity
// it attacked (SetHitPart).
func TestDragonPartDamage(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:boss")
	p := w.AddPlayer("hunter", mgl64.Vec3{0.5, 1, 0.5})
	r := spawnDragon(t, w, mgl64.Vec3{0.5, 20, 0.5})
	w.Tick(3)
	hit := func(name string, aim func(tx *world.Tx, m *mob.Mob, pl *player.Player), want float64) {
		t.Helper()
		w.Do(func(tx *world.Tx) {
			m := mobOf(t, tx, r)
			e, _ := p.Entity(tx)
			pl := e.(*player.Player)
			m.SetHealth(200)
			aim(tx, m, pl)
			m.Hurt(10, entity.AttackDamageSource{Attacker: pl})
			if got := 200 - m.Health(); math.Abs(got-want) > 1e-6 {
				t.Errorf("%s: dealt %v, want %v", name, got, want)
			}
			pl.Teleport(mgl64.Vec3{0.5, 1, 0.5})
		})
		w.Tick(11) // the hurt cooldown
	}
	hit("looking at the head", func(_ *world.Tx, m *mob.Mob, pl *player.Player) {
		lookAt(pl, bc.DragonOf(m).HeadPos())
	}, 10)
	hit("looking at a wing", func(_ *world.Tx, m *mob.Mob, pl *player.Player) {
		lookAt(pl, boxCentre(bc.DragonOf(m).Part(bc.PartWing1)))
	}, 3.5)
	hit("Java head part", func(_ *world.Tx, m *mob.Mob, pl *player.Player) {
		lookAt(pl, boxCentre(bc.DragonOf(m).Part(bc.PartWing2)))
		m.SetHitPart(bc.PartHead)
	}, 10)
	hit("Java neck part", func(_ *world.Tx, m *mob.Mob, _ *player.Player) { m.SetHitPart(bc.PartNeck) }, 10)
	hit("Java tail part", func(_ *world.Tx, m *mob.Mob, pl *player.Player) {
		lookAt(pl, bc.DragonOf(m).HeadPos())
		m.SetHitPart(bc.PartTail2)
	}, 3.5)
}

// TestDragonExplosions: only explosions of blocks (beds, respawn anchors) or
// credited to a player hurt the dragon, and then on the body.
func TestDragonExplosions(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:boss")
	p := w.AddPlayer("hunter", mgl64.Vec3{0.5, 1, 0.5})
	r := spawnDragon(t, w, mgl64.Vec3{0.5, 20, 0.5})
	w.Tick(3)
	var crystal *world.EntityHandle
	w.Do(func(tx *world.Tx) {
		crystal = entity.NewEndCrystal(world.EntitySpawnOpts{Position: mgl64.Vec3{20.5, 1, 20.5}})
		tx.AddEntity(crystal)
	})
	cases := []struct {
		name string
		src  func(tx *world.Tx) world.ExplosionSource
		want float64
	}{
		{"bed", func(*world.Tx) world.ExplosionSource {
			return world.BlockExplosionSource{Pos: cube.Pos{0, 20, 0}, ExplosionSize: 5}
		}, 3.5},
		{"end crystal", func(tx *world.Tx) world.ExplosionSource {
			e, _ := crystal.Entity(tx)
			return world.EntityExplosionSource{Entity: e, ExplosionSize: 6}
		}, 0},
		{"player's", func(tx *world.Tx) world.ExplosionSource {
			e, _ := p.Entity(tx)
			return world.EntityExplosionSource{Entity: e, ExplosionSize: 6}
		}, 3.5},
	}
	for _, c := range cases {
		w.Do(func(tx *world.Tx) {
			m := mobOf(t, tx, r)
			m.SetHealth(200)
			m.Hurt(10, entity.ExplosionDamageSource{Source: c.src(tx)})
			if got := 200 - m.Health(); math.Abs(got-c.want) > 1e-6 {
				t.Errorf("%s explosion: dealt %v, want %v", c.name, got, c.want)
			}
		})
		w.Tick(11)
	}
}
