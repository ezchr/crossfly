//go:build crossfly_full

package mobtest_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// projectileArena is a closed stone room with a shooter mob of id at one
// end and a survival player 8 blocks away.
func projectileArena(t *testing.T, id string) (*mobtest.World, *mobtest.Ref, *mobtest.Player) {
	t.Helper()
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Fill(tx, cube.Pos{-14, 0, -14}, cube.Pos{14, 14, 14}, block.Stone{})
		w.Clear(tx, cube.Pos{-13, 1, -13}, cube.Pos{13, 13, 13})
	})
	shooter := w.Spawn(id, mgl64.Vec3{0.5, 1, -4.5})
	p := w.AddPlayer("target", mgl64.Vec3{0.5, 1, 3.5})
	return w, shooter, p
}

// shoot fires projectile id from the shooter at the player.
func shoot(t *testing.T, w *mobtest.World, shooter *mobtest.Ref, p *mobtest.Player, id string) {
	t.Helper()
	ok := false
	w.Do(func(tx *world.Tx) {
		m, mok := shooter.Mob(tx)
		e, eok := p.Entity(tx)
		if mok && eok {
			ok = combat.ShootProjectile(m, e, id, 0, 0)
		}
	})
	if !ok {
		t.Fatalf("%v was not fired", id)
	}
}

// projectiles counts the entities of type id.
func projectiles(w *mobtest.World, id string) int {
	return w.Count(func(e world.Entity) bool { return e.H().Type().EncodeEntity() == id })
}

func onFire(w *mobtest.World, p *mobtest.Player) bool {
	burning := false
	p.With(func(pl *player.Player) { burning = pl.OnFireDuration() > 0 })
	return burning
}

// A blaze's small fireball (small_fireball.json: power 1.3, no gravity,
// impact_damage 5 with catch_fire) hits the player for 5 and sets it on
// fire, without an explosion.
func TestSmallFireballHitsAndBurns(t *testing.T) {
	needs(t, true, false)
	w, blaze, p := projectileArena(t, "minecraft:blaze")
	shoot(t, w, blaze, p, "minecraft:small_fireball")
	if n := projectiles(w, "minecraft:small_fireball"); n != 1 {
		t.Fatalf("%d small fireballs in the world, want 1", n)
	}
	if _, ok := w.TickUntil(40, func(*world.Tx) bool { return p.Health() < 20 }); !ok {
		t.Fatalf("the small fireball never hit")
	}
	if h := p.Health(); h != 15 {
		t.Fatalf("player health %v after the hit, want 15", h)
	}
	if !onFire(w, p) {
		t.Fatalf("the player is not on fire")
	}
	w.Tick(1)
	if n := projectiles(w, "minecraft:small_fireball"); n != 0 {
		t.Fatalf("the small fireball is still there")
	}
	w.Do(func(tx *world.Tx) {
		for x := -12; x <= 12; x++ {
			for z := -12; z <= 12; z++ {
				if _, air := tx.Block(cube.Pos{x, 0, z}).(block.Air); air {
					t.Fatalf("the floor at %v,%v is gone: something exploded", x, z)
				}
			}
		}
	})
}

// A ghast's fireball explodes where it lands (fireball.json definition_event
// minecraft:explode, power 1): the player takes the impact and the blast,
// and the fireball is gone.
func TestGhastFireballExplodes(t *testing.T) {
	needs(t, true, false)
	w, ghast, p := projectileArena(t, "minecraft:ghast")
	shoot(t, w, ghast, p, "minecraft:fireball")
	if _, ok := w.TickUntil(60, func(*world.Tx) bool { return projectiles(w, "minecraft:fireball") == 0 }); !ok {
		t.Fatalf("the fireball never landed")
	}
	if h := p.Health(); h > 14 {
		t.Fatalf("player health %v, want at most 14 (6 impact + the explosion)", h)
	}
}

// Hitting a ghast fireball sends it where the player looks and makes the
// player its owner (reflect_on_hurt); back at the ghast it kills it
// (Ghast::_hurt: 1000 damage from a fireball a player owns).
func TestGhastFireballReflects(t *testing.T) {
	needs(t, true, false)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Fill(tx, cube.Pos{-16, 0, -16}, cube.Pos{16, 15, 16}, block.Stone{})
		w.Clear(tx, cube.Pos{-15, 1, -15}, cube.Pos{15, 14, 15})
	})
	ghast := w.Spawn("minecraft:ghast", mgl64.Vec3{0.5, 1, -10.5})
	p := w.AddPlayer("batter", mgl64.Vec3{0.5, 1, 3.5})
	shoot(t, w, ghast, p, "minecraft:fireball")
	var ball *mobtest.Ref
	if _, ok := w.TickUntil(40, func(tx *world.Tx) bool {
		for _, r := range w.Entities(func(e world.Entity) bool { return e.H().Type().EncodeEntity() == "minecraft:fireball" }) {
			if pe, ok := p.Entity(tx); ok && mobtest.DistTx(tx, r, p) < 6 && pe != nil {
				ball = r
				return true
			}
		}
		return false
	}); !ok {
		t.Fatalf("the fireball never came close")
	}
	p.With(func(pl *player.Player) {
		// Face the ghast.
		g, _ := ghast.Entity(pl.Tx())
		d := g.Position().Add(mgl64.Vec3{0, 2, 0}).Sub(entity.EyePosition(pl))
		yaw := mgl64.RadToDeg(math.Atan2(-d[0], d[2]))
		pitch := -mgl64.RadToDeg(math.Atan2(d[1], math.Hypot(d[0], d[2])))
		pl.Move(mgl64.Vec3{}, yaw-pl.Rotation().Yaw(), pitch-pl.Rotation().Pitch())
		e, ok := ball.Entity(pl.Tx())
		if !ok {
			t.Fatalf("fireball gone")
		}
		if !pl.AttackEntity(e) {
			t.Fatalf("the player could not hit the fireball")
		}
		v := e.(*entity.Ent).Velocity()
		if want := pl.Rotation().Vec3(); v.Sub(want).Len() > 1e-6 {
			t.Fatalf("reflected fireball velocity %v, want the look direction %v", v, want)
		}
		if o := e.(*entity.Ent).ProjectileOwner(); o != pl.H() {
			t.Fatalf("reflected fireball owner is not the player")
		}
	})
	if _, ok := w.TickUntil(60, func(*world.Tx) bool { return !ghast.Alive() }); !ok {
		t.Fatalf("the reflected fireball did not kill the ghast: %v", w.Dump(ghast))
	}
}

// Llama spit (llama_spit.json: gravity 0.06, aimed in an arc) hits for 1
// and sets nothing on fire.
func TestLlamaSpitHits(t *testing.T) {
	needs(t, true, false)
	w, llama, p := projectileArena(t, "minecraft:llama")
	shoot(t, w, llama, p, "minecraft:llama_spit")
	if _, ok := w.TickUntil(60, func(*world.Tx) bool { return p.Health() < 20 }); !ok {
		t.Fatalf("the spit never hit")
	}
	if h := p.Health(); h != 19 {
		t.Fatalf("player health %v, want 19", h)
	}
	if onFire(w, p) {
		t.Fatalf("llama spit set the player on fire")
	}
}

// A shulker bullet follows its target when it moves away (it steers block
// by block) and gives it levitation for 10 seconds.
func TestShulkerBulletHomesAndLevitates(t *testing.T) {
	needs(t, true, false)
	w, shulker, p := projectileArena(t, "minecraft:shulker")
	shoot(t, w, shulker, p, "minecraft:shulker_bullet")
	w.Tick(3)
	p.Teleport(mgl64.Vec3{8.5, 1, 6.5})
	if _, ok := w.TickUntil(400, func(*world.Tx) bool { return p.Health() < 20 }); !ok {
		t.Fatalf("the bullet never reached the player")
	}
	p.With(func(pl *player.Player) {
		e, ok := pl.Effect(effect.Levitation)
		if !ok {
			t.Fatalf("no levitation after the bullet hit")
		}
		if d := e.Duration().Seconds(); math.Abs(d-10) > 0.2 {
			t.Fatalf("levitation for %v s, want 10", d)
		}
	})
}

// A shulker bullet is destroyed by a hit (destroy_on_hurt).
func TestShulkerBulletShotDown(t *testing.T) {
	needs(t, true, false)
	w, shulker, p := projectileArena(t, "minecraft:shulker")
	shoot(t, w, shulker, p, "minecraft:shulker_bullet")
	w.Tick(2)
	var bullet *mobtest.Ref
	for _, r := range w.Entities(func(e world.Entity) bool { return e.H().Type().EncodeEntity() == "minecraft:shulker_bullet" }) {
		bullet = r
	}
	if bullet == nil {
		t.Fatalf("no bullet")
	}
	w.Do(func(tx *world.Tx) {
		e, _ := bullet.Entity(tx)
		pe, _ := p.Entity(tx)
		if _, vulnerable, ok := entity.HurtEntity(e, 1, entity.AttackDamageSource{Attacker: pe}); !ok || !vulnerable {
			t.Fatalf("the bullet cannot be hit")
		}
	})
	w.Tick(1)
	if !bullet.Gone() {
		t.Fatalf("the bullet survived a hit")
	}
}

// The ghast, blaze, llama and shulker fire their projectiles on their own
// once they target a player.
func TestMobsShootTheirProjectiles(t *testing.T) {
	needs(t, true, true)
	for _, c := range []struct {
		mob, projectile string
		hurt            bool
	}{
		{"minecraft:blaze", "minecraft:small_fireball", false},
		{"minecraft:ghast", "minecraft:fireball", false},
		{"minecraft:shulker", "minecraft:shulker_bullet", false},
		{"minecraft:llama", "minecraft:llama_spit", true},
	} {
		t.Run(c.mob, func(t *testing.T) {
			w, m, p := projectileArena(t, c.mob)
			m.With(func(mm *mob.Mob) {
				pe, _ := p.Entity(mm.Tx())
				if c.hurt {
					// Llamas spit at whoever hurts them (damage_sensor
					// minecraft:become_angry, hurt_by_target).
					mm.Hurt(1, entity.AttackDamageSource{Attacker: pe})
					return
				}
				mm.SetTarget(pe)
			})
			if _, ok := w.TickUntil(300, func(*world.Tx) bool { return projectiles(w, c.projectile) > 0 }); !ok {
				t.Fatalf("%v fired no %v: %v", c.mob, c.projectile, w.Dump(m))
			}
		})
	}
}

// A blaze charges for charge_shoot_trigger (4 s) while it sees its target,
// shows itself charged (the Bedrock charged flag, Java's on fire), then
// fires a burst of 3 small fireballs 0.3 s apart (blaze.json ranged_attack).
func TestBlazeChargesThenFiresBurst(t *testing.T) {
	needs(t, true, true)
	w, blaze, p := projectileArena(t, "minecraft:blaze")
	blaze.With(func(m *mob.Mob) {
		pe, _ := p.Entity(m.Tx())
		m.SetTarget(pe)
	})
	charged := false
	first := -1
	seen := map[*world.EntityHandle]bool{}
	w.Each(140, func(tx *world.Tx, tick int) bool {
		if m, ok := blaze.Mob(tx); ok && m.ActorFlag(protocol.EntityDataFlagCharged) {
			charged = true
		}
		for _, r := range w.Entities(func(e world.Entity) bool { return e.H().Type().EncodeEntity() == "minecraft:small_fireball" }) {
			seen[r.H()] = true
			if first < 0 {
				first = tick
			}
		}
		return true
	})
	if first < 70 || first > 100 {
		t.Fatalf("first fireball at tick %d, want after about 80 ticks of charging", first)
	}
	if !charged {
		t.Fatalf("the blaze never showed itself charged")
	}
	if len(seen) != 3 {
		t.Fatalf("%d small fireballs in the first burst, want 3", len(seen))
	}
}
