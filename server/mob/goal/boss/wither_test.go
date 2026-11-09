//go:build crossfly_full

package boss_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	bc "github.com/df-mc/dragonfly/server/mob/component/boss"
	"github.com/df-mc/dragonfly/server/mob/component/combat"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// TestWitherSpawnCharge: a new wither starts at 1 health, cannot be hurt
// and heals ceil(max/200) a tick for 200 ticks (max 450 on normal), then
// explodes and is vulnerable.
func TestWitherSpawnCharge(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:boss")
	r := w.Spawn(bc.WitherID, mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(1)
	with(t, w, r, func(tx *world.Tx, m *mob.Mob) {
		if m.MaxHealth() != 450 {
			t.Errorf("max health %v on normal, want 450", m.MaxHealth())
		}
		if f := bc.WitherOf(m).SpawnFrames; f != bc.WitherSpawnFrames-1 {
			t.Errorf("spawn frames %v after a tick", f)
		}
		if h := m.Health(); h > 4 {
			t.Errorf("health %v after one tick of charging, want about 1+3", h)
		}
		if _, res := m.Hurt(20, entity.AttackDamageSource{}); res.Accepted() {
			t.Errorf("charging wither took damage")
		}
		if v, _ := m.ActorData(protocol.EntityDataKeyInvulnerableTicks); v.(int32) <= 0 {
			t.Errorf("invulnerable ticks %v", v)
		}
	})
	w.Tick(bc.WitherSpawnFrames)
	with(t, w, r, func(tx *world.Tx, m *mob.Mob) {
		if m.Health() != m.MaxHealth() {
			t.Errorf("health %v after the charge, want %v", m.Health(), m.MaxHealth())
		}
		if bc.WitherOf(m).Invulnerable() {
			t.Errorf("still invulnerable")
		}
		before := m.Health()
		m.Hurt(10, entity.ExplosionDamageSource{Source: world.EntityExplosionSource{ExplosionSize: 4}})
		if m.Health() >= before {
			t.Errorf("explosion did not hurt the wither")
		}
	})
}

// TestWitherShootsSkulls: after the charge the wither shoots skulls at a
// player and every fourth one of the middle head is blue.
func TestWitherShootsSkulls(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:boss", "minecraft:behavior.nearest_attackable_target")
	p := w.AddPlayer("target", mgl64.Vec3{10.5, 1, 0.5})
	r := w.Spawn(bc.WitherID, mgl64.Vec3{-5.5, 1, 0.5})
	w.Tick(bc.WitherSpawnFrames + 1)
	black, blue := 0, 0
	seen := map[*world.EntityHandle]bool{}
	w.Each(200, func(tx *world.Tx, _ int) bool {
		for e := range tx.Entities() {
			if seen[e.H()] {
				continue
			}
			switch e.H().Type() {
			case bc.WitherSkullType:
				black++
				seen[e.H()] = true
			case bc.BlueWitherSkullType:
				blue++
				seen[e.H()] = true
			}
		}
		return true
	})
	if black == 0 || blue == 0 {
		t.Fatalf("skulls: %d black, %d blue\n%v", black, blue, w.Dump(r))
	}
	if p.Health() >= 20 {
		t.Errorf("the player was never hurt (health %v)", p.Health())
	}
	with(t, w, r, func(tx *world.Tx, m *mob.Mob) {
		if v, ok := m.ActorData(protocol.EntityDataKeyTargetA); !ok || v.(*world.EntityHandle) == nil {
			t.Errorf("target A not synced: %v", v)
		}
	})
}

// TestWitherHalfHealth: at half health the armour comes (aerial attack 0),
// arrows stop hurting and the fire rate is back to 20.
func TestWitherHalfHealth(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:boss")
	r := w.Spawn(bc.WitherID, mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(bc.WitherSpawnFrames + 1)
	p := w.AddPlayer("attacker", mgl64.Vec3{5.5, 1, 0.5})
	w.Do(func(tx *world.Tx) {
		m := mobOf(t, tx, r)
		pl, _ := p.Entity(tx)
		m.SetHealth(m.MaxHealth()/2 + 1)
		m.Hurt(5, entity.AttackDamageSource{Attacker: pl})
		wi := bc.WitherOf(m)
		if wi.Phase1 {
			t.Fatalf("still phase 1 at %v/%v", m.Health(), m.MaxHealth())
		}
		if wi.Damage[pl.H()] != 5 {
			t.Errorf("player damage %v, want 5", wi.Damage[pl.H()])
		}
	})
	w.Tick(25)
	w.Do(func(tx *world.Tx) {
		m := mobOf(t, tx, r)
		if v, _ := m.ActorData(protocol.EntityDataKeyAerialAttack); v.(int16) != 0 {
			t.Errorf("aerial attack %v in phase 2, want 0", v)
		}
		pl, _ := p.Entity(tx)
		arrow := tx.AddEntity(world.EntitySpawnOpts{Position: m.Position()}.New(entity.ArrowType, entity.ProjectileBehaviourConfig{}))
		before := m.Health()
		m.Hurt(10, entity.ProjectileDamageSource{Projectile: arrow, Owner: pl})
		if m.Health() != before {
			t.Errorf("an arrow hurt the armoured wither")
		}
	})
}

// TestWitherDeath: a lethal hit starts the 200-frame death animation at 1
// health; then it explodes and dies.
func TestWitherDeath(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:boss")
	r := w.Spawn(bc.WitherID, mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(bc.WitherSpawnFrames + 1)
	p := w.AddPlayer("killer", mgl64.Vec3{3.5, 1, 0.5})
	w.Do(func(tx *world.Tx) {
		m := mobOf(t, tx, r)
		pl, _ := p.Entity(tx)
		m.SetHealth(5)
		m.Hurt(50, entity.AttackDamageSource{Attacker: pl})
		if m.Dead() || m.Health() != 1 {
			t.Fatalf("health %v after a lethal hit, want 1 (dying)", m.Health())
		}
		if !bc.WitherOf(m).Dying() {
			t.Fatalf("no death animation")
		}
	})
	w.Tick(150)
	if r.Gone() {
		t.Fatalf("gone before the animation ended")
	}
	if _, ok := w.TickUntil(100, func(tx *world.Tx) bool { return r.Gone() || !r.Alive() }); !ok {
		t.Fatalf("wither never died: %v", w.Dump(r))
	}
}

// TestWitherHighestDamageTarget: the player who dealt most damage becomes
// the target.
func TestWitherHighestDamageTarget(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:boss", "minecraft:behavior.wither_target_highest_damage")
	r := w.Spawn(bc.WitherID, mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(bc.WitherSpawnFrames + 1)
	a := w.AddPlayer("a", mgl64.Vec3{8.5, 1, 0.5})
	b := w.AddPlayer("b", mgl64.Vec3{-8.5, 1, 0.5})
	w.Do(func(tx *world.Tx) {
		m := mobOf(t, tx, r)
		pa, _ := a.Entity(tx)
		pb, _ := b.Entity(tx)
		wi := bc.WitherOf(m)
		wi.Damage[pa.H()] = 5
		wi.Damage[pb.H()] = 30
	})
	w.Tick(3)
	w.Do(func(tx *world.Tx) {
		m := mobOf(t, tx, r)
		pb, _ := b.Entity(tx)
		if m.TargetHandle() != pb.H() {
			t.Errorf("target is not the player with the most damage")
		}
	})
}

// buildWitherT builds the soul sand T with wither skeleton skulls on top
// along axis (x or z) around (0, 1..3, 0), all but the skull at skip.
func buildWitherT(tx *world.Tx, axis cube.Pos, skull block.SkullType, skip int) {
	sk := block.Skull{Type: skull, Attach: block.StandingAttachment(cube.OrientationFromYaw(0))}
	tx.SetBlock(cube.Pos{0, 1, 0}, block.SoulSand{}, nil)
	for k := -1; k <= 1; k++ {
		p := cube.Pos{axis[0] * k, 2, axis[2] * k}
		tx.SetBlock(p, block.SoulSoil{}, nil)
		if k != skip {
			tx.SetBlock(p.Side(cube.FaceUp), sk, nil)
		}
	}
}

// TestWitherSpawnPattern: placing the last wither skeleton skull on a T of
// soul sand / soul soil (SkullBlock::checkMobSpawn through the skull's
// placement) makes a wither, clears the nine pattern blocks and turns the
// wither for a T along x; the wither stands where BuildMatch puts the centre
// (first skull + (1, -2, 1)).
func TestWitherSpawnPattern(t *testing.T) {
	for _, tc := range []struct {
		name string
		axis cube.Pos
		at   mgl64.Vec3
		yaw  float64
	}{
		{"along x", cube.Pos{1, 0, 0}, mgl64.Vec3{0.5, 1.05, 1.5}, 90},
		{"along z", cube.Pos{0, 0, 1}, mgl64.Vec3{1.5, 1.05, 0.5}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := setup(t, mobtest.Options{}, "minecraft:boss")
			p := w.AddPlayer("builder", mgl64.Vec3{5.5, 1, 5.5})
			w.Do(func(tx *world.Tx) {
				buildWitherT(tx, tc.axis, block.WitherSkeletonSkull(), 1)
				user, _ := p.Entity(tx)
				last := cube.Pos{tc.axis[0], 2, tc.axis[2]}
				block.Skull{Type: block.WitherSkeletonSkull()}.UseOnBlock(last, cube.FaceUp, mgl64.Vec3{}, tx, user.(item.User), &item.UseContext{})
				for _, q := range []cube.Pos{{0, 1, 0}, {0, 2, 0}, {0, 3, 0}, last, last.Side(cube.FaceUp)} {
					if _, ok := tx.Block(q).(block.Air); !ok {
						t.Errorf("pattern block %v not cleared: %T", q, tx.Block(q))
					}
				}
			})
			withers := w.Mobs(bc.WitherID)
			if len(withers) != 1 {
				t.Fatalf("withers %d, want 1", len(withers))
			}
			with(t, w, withers[0], func(tx *world.Tx, m *mob.Mob) {
				if !near(m.Position(), tc.at, 1e-6) {
					t.Errorf("wither at %v, want %v", m.Position(), tc.at)
				}
				if m.Yaw() != tc.yaw {
					t.Errorf("wither yaw %v, want %v", m.Yaw(), tc.yaw)
				}
			})
		})
	}
}

// TestWitherSpawnPatternRules: other skulls, a block where the T's lower
// corners must be air, peaceful, or a skull not yet placed make no wither;
// the T lying on the ground works too (the pattern is matched any way
// round).
func TestWitherSpawnPatternRules(t *testing.T) {
	cases := []struct {
		name  string
		opts  mobtest.Options
		build func(tx *world.Tx)
		want  int
	}{
		{"skeleton skulls", mobtest.Options{}, func(tx *world.Tx) {
			buildWitherT(tx, cube.Pos{1, 0, 0}, block.SkeletonSkull(), 9)
		}, 0},
		{"corner filled", mobtest.Options{}, func(tx *world.Tx) {
			buildWitherT(tx, cube.Pos{1, 0, 0}, block.WitherSkeletonSkull(), 9)
			tx.SetBlock(cube.Pos{1, 1, 0}, block.Dirt{}, nil)
		}, 0},
		{"peaceful", mobtest.Options{Difficulty: world.DifficultyPeaceful}, func(tx *world.Tx) {
			buildWitherT(tx, cube.Pos{1, 0, 0}, block.WitherSkeletonSkull(), 9)
		}, 0},
		{"two skulls", mobtest.Options{}, func(tx *world.Tx) {
			buildWitherT(tx, cube.Pos{1, 0, 0}, block.WitherSkeletonSkull(), -1)
		}, 0},
		{"lying flat", mobtest.Options{}, func(tx *world.Tx) {
			sk := block.Skull{Type: block.WitherSkeletonSkull(), Attach: block.StandingAttachment(0)}
			for x := -1; x <= 1; x++ {
				tx.SetBlock(cube.Pos{x, 1, 0}, sk, nil)
				tx.SetBlock(cube.Pos{x, 1, 1}, block.SoulSand{}, nil)
			}
			tx.SetBlock(cube.Pos{0, 1, 2}, block.SoulSand{}, nil)
		}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := setup(t, tc.opts, "minecraft:boss")
			w.Do(func(tx *world.Tx) {
				tc.build(tx)
				for x := -1; x <= 1; x++ {
					for _, p := range []cube.Pos{{x, 3, 0}, {x, 1, 0}} {
						bc.TrySpawnWither(tx, p)
					}
				}
			})
			if n := len(w.Mobs(bc.WitherID)); n != tc.want {
				t.Fatalf("withers %d, want %d", n, tc.want)
			}
		})
	}
}

// TestWindChargeFromShooter: combat.ShootProjectile fires the breeze wind
// charge registered by component/boss; it hurts the target for 1 and its
// wind burst pushes it.
func TestWindChargeFromShooter(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:boss")
	p := w.AddPlayer("target", mgl64.Vec3{6.5, 1, 0.5})
	r := w.Spawn("minecraft:breeze", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(1)
	w.Do(func(tx *world.Tx) {
		pl, _ := p.Entity(tx)
		if !combat.ShootProjectile(mobOf(t, tx, r), pl, "minecraft:breeze_wind_charge_projectile", 0, 0) {
			t.Fatal("not fired")
		}
	})
	start := p.Health()
	if _, ok := w.TickUntil(40, func(tx *world.Tx) bool { return p.HealthTx(tx) < start }); !ok {
		t.Fatalf("the wind charge never hit")
	}
	if got := start - p.Health(); got != 1 {
		t.Errorf("wind charge dealt %v, want 1", got)
	}
}

// TestWitherChargeKnockback: a mob meleeing the wither during its spawn
// charge is knocked back (WitherBoss::_hurt); the wither takes nothing.
func TestWitherChargeKnockback(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:boss")
	wr := w.Spawn(bc.WitherID, mgl64.Vec3{0.5, 1, 0.5})
	zr := w.Spawn("minecraft:zombie", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(2)
	w.Do(func(tx *world.Tx) {
		wm, z := mobOf(t, tx, wr), mobOf(t, tx, zr)
		z.SetVelocity(mgl64.Vec3{})
		h := wm.Health()
		wm.Hurt(5, entity.AttackDamageSource{Attacker: z})
		if wm.Health() != h {
			t.Errorf("charging wither took damage: %v -> %v", h, wm.Health())
		}
		if v := z.Velocity(); v[0] <= 0 || v[1] <= 0 {
			t.Errorf("zombie velocity %v, want pushed away (+x) and up", v)
		}
	})
}
