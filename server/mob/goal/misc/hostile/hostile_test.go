//go:build crossfly_full

package hostile_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/hostile"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	_ "github.com/df-mc/dragonfly/server/mob/vanilla"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func check(t *testing.T, name string, got map[string]float64, want map[string]float64) {
	t.Helper()
	for k, v := range want {
		if !near(got[k], v) {
			t.Errorf("%v %v = %v, want %v", name, k, got[k], v)
		}
	}
}

// TestDefaults: BDS defaults with empty JSON (TeleportDescription,
// 1.26 docs for looked_at, IllagerBeastBlockedDescription) and the vanilla
// JSON values.
func TestDefaults(t *testing.T) {
	c, err := hostile.Build(hostile.NewTeleport, "minecraft:teleport", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	check(t, "teleport{}", hostile.Params(c), map[string]float64{"random": 1, "min": 0, "max": 400, "cube_x": 32, "cube_y": 32,
		"target_distance": 16, "target_chance": 1, "light": 0.01, "dark": 0.01, "projectile": 0})
	c, err = hostile.Build(hostile.NewTeleport, "minecraft:teleport", string(mobtest.Component(t, "enderman.json", "", "minecraft:teleport")))
	if err != nil {
		t.Fatal(err)
	}
	check(t, "enderman teleport", hostile.Params(c), map[string]float64{"max": 600, "target_chance": 0.05, "light": 0.05, "dark": 0.01, "projectile": 1})

	c, err = hostile.Build(hostile.NewLookedAt, "minecraft:looked_at", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	check(t, "looked_at{}", hostile.Params(c), map[string]float64{"radius": 10, "fov": 26, "scale": 1, "min": 0, "locations": 1})
	if m := hostile.SetTargetMode(c.(*hostile.LookedAt)); m != "once_and_stop_scanning" {
		t.Errorf("set_target default %q", m)
	}
	c, err = hostile.Build(hostile.NewLookedAt, "minecraft:looked_at", string(mobtest.Component(t, "enderman.json", "", "minecraft:looked_at")))
	if err != nil {
		t.Fatal(err)
	}
	check(t, "enderman looked_at", hostile.Params(c), map[string]float64{"radius": 64, "min": 5, "players_only": 1})

	c, _ = hostile.Build(hostile.NewRavagerBlocked, "minecraft:ravager_blocked", `{}`)
	check(t, "ravager_blocked{}", hostile.Params(c), map[string]float64{"strength": 3, "choices": 0})
	c, _ = hostile.Build(hostile.NewGroupSize, "minecraft:group_size", `{}`)
	check(t, "group_size{}", hostile.Params(c), map[string]float64{"radius": 16})
	c, err = hostile.Build(hostile.NewBreakBlocks, "minecraft:break_blocks", string(mobtest.Component(t, "ravager.json", "", "minecraft:break_blocks")))
	if err != nil {
		t.Fatal(err)
	}
	if hostile.Params(c)["blocks"] < 30 {
		t.Errorf("ravager break_blocks parsed %v blocks", hostile.Params(c)["blocks"])
	}
}

func arena(t *testing.T, opts mobtest.Options, r int) *mobtest.World {
	w := mobtest.New(t, opts)
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, r, block.Stone{}) })
	return w
}

// TestEndermanRandomTeleport: in the dark an enderman teleports with
// dark_teleport_chance (0.01/tick) or its random timer, landing on the
// floor.
func TestEndermanRandomTeleport(t *testing.T) {
	w := arena(t, mobtest.Options{}, 24)
	e := w.Spawn("minecraft:enderman", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(2)
	last := e.Pos()
	jumped := false
	w.Each(600, func(tx *world.Tx, _ int) bool {
		p := e.PosTx(tx)
		if p.Sub(last).Len() > 3 {
			jumped = true
			if math.Abs(p[1]-1) > 0.01 {
				t.Errorf("teleported to y %.2f, not onto the floor", p[1])
			}
			return false
		}
		last = p
		return true
	})
	if !jumped {
		t.Fatalf("enderman never teleported:\n%v", w.Dump(e))
	}
}

// TestEndermanDodgesProjectiles: teleports_on_projectile_hit cancels the
// damage and moves the enderman.
func TestEndermanDodgesProjectiles(t *testing.T) {
	w := arena(t, mobtest.Options{}, 24)
	e := w.Spawn("minecraft:enderman", mgl64.Vec3{0.5, 1, 0.5})
	p := w.AddPlayer("shooter", mgl64.Vec3{8.5, 1, 0.5})
	w.Tick(1)
	before := e.Pos()
	w.Do(func(tx *world.Tx) {
		m, _ := e.Mob(tx)
		pl, _ := p.H().Entity(tx)
		hp := m.Health()
		m.Hurt(6, entity.ProjectileDamageSource{Projectile: pl, Owner: pl})
		if m.Health() != hp {
			t.Errorf("projectile damage not cancelled: %v -> %v", hp, m.Health())
		}
	})
	if e.Pos().Sub(before).Len() < 1 {
		t.Errorf("enderman did not teleport away from the projectile")
	}
}

// aim turns pl to look at pos.
func aim(pl *player.Player, pos mgl64.Vec3) {
	d := pos.Sub(entity.EyePosition(pl))
	yaw := mgl64.RadToDeg(math.Atan2(-d[0], d[2]))
	pitch := -mgl64.RadToDeg(math.Atan2(d[1], math.Hypot(d[0], d[2])))
	r := pl.Rotation()
	pl.Move(mgl64.Vec3{}, yaw-r.Yaw(), pitch-r.Pitch())
}

// TestEndermanStare: a player looking at the enderman's head for
// min_looked_at_duration becomes its target; one looking away does not.
func TestEndermanStare(t *testing.T) {
	w := arena(t, mobtest.Options{}, 24)
	e := w.Spawn("minecraft:enderman", mgl64.Vec3{0.5, 1, 0.5})
	p := w.AddPlayer("starer", mgl64.Vec3{0.5, 1, 6.5})
	look := func(at bool) bool {
		targeted := false
		w.Each(40, func(tx *world.Tx, _ int) bool {
			m, ok := e.Mob(tx)
			if !ok {
				return false
			}
			pl, _ := p.H().Entity(tx)
			if at {
				aim(pl.(*player.Player), m.EyePosition())
			} else {
				aim(pl.(*player.Player), m.EyePosition().Add(mgl64.Vec3{20, 0, 0}))
			}
			if tg, ok := m.Target(); ok && tg.H() == pl.H() {
				targeted = true
				return false
			}
			return true
		})
		return targeted
	}
	if look(false) {
		t.Fatal("enderman targeted a player looking away")
	}
	if !look(true) {
		t.Fatalf("enderman did not target the staring player:\n%v", w.Dump(e))
	}
}

// TestLookedAtFOV: scale_fov_by_distance shrinks the allowed angle with
// distance (Java 1 - 0.025/d).
func TestLookedAtFOV(t *testing.T) {
	w := arena(t, mobtest.Options{}, 24)
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{0.5, 1, 0.5})
	p := w.AddPlayer("p", mgl64.Vec3{0.5, 1, 20.5})
	c, _ := hostile.Build(hostile.NewLookedAt, "minecraft:looked_at", `{}`)
	la := c.(*hostile.LookedAt)
	w.Do(func(tx *world.Tx) {
		m, _ := z.Mob(tx)
		e, _ := p.H().Entity(tx)
		pl := e.(*player.Player)
		aim(pl, m.EyePosition())
		if !la.IsLookingAt(m, pl) {
			t.Error("not looking when aiming at the head")
		}
		// 3 degrees off at 20 blocks: outside the scaled cone (~2.9 deg), inside 13.
		aim(pl, m.EyePosition().Add(mgl64.Vec3{math.Tan(mgl64.DegToRad(4)) * 20, 0, 0}))
		if la.IsLookingAt(m, pl) {
			t.Error("4 degrees off at 20 blocks still counts with scale_fov_by_distance")
		}
	})
}

// TestRavagerBreaksLeaves: break_blocks destroys listed blocks the ravager
// touches and leaves others.
func TestRavagerBreaksLeaves(t *testing.T) {
	w := arena(t, mobtest.Options{}, 8)
	w.Do(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{1, 1, 0}, block.Leaves{Type: block.OakLeaves(), Persistent: true}, nil)
		tx.SetBlock(cube.Pos{-1, 2, 0}, block.Stone{}, nil)
	})
	r := w.Spawn("minecraft:ravager", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(3)
	w.Do(func(tx *world.Tx) {
		if _, ok := tx.Block(cube.Pos{1, 1, 0}).(block.Leaves); ok {
			t.Errorf("leaves next to the ravager were not broken:\n%v", w.Dump(r))
		}
		if _, ok := tx.Block(cube.Pos{-1, 2, 0}).(block.Stone); !ok {
			t.Error("stone was broken")
		}
	})
}

// TestRavagerBlocked: a blocked hit pushes the target away and picks one
// of the reaction choices (become_stunned or nothing, 1:1).
func TestRavagerBlocked(t *testing.T) {
	w := arena(t, mobtest.Options{}, 16)
	r := w.Spawn("minecraft:ravager", mgl64.Vec3{0.5, 1, 0.5})
	p := w.AddPlayer("blocker", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(1)
	stunned := false
	for i := 0; i < 20 && !stunned; i++ {
		w.Do(func(tx *world.Tx) {
			m, _ := r.Mob(tx)
			pl, _ := p.H().Entity(tx)
			c, ok := mob.ComponentOf[*hostile.RavagerBlocked](m)
			if !ok {
				t.Fatal("ravager has no ravager_blocked")
			}
			pl.(*player.Player).SetVelocity(mgl64.Vec3{})
			c.Blocked(m, pl)
			if v := pl.(*player.Player).Velocity(); v[0] <= 0 {
				t.Errorf("blocker not pushed away: %v", v)
			}
			stunned = m.HasComponent("minecraft:is_stunned")
		})
	}
	if !stunned {
		t.Fatal("become_stunned never chosen in 20 blocked hits")
	}
}

// TestHoglinHitBoxes: custom_hit_test boxes (adult 2 x 1.75, pivot y 1).
func TestHoglinHitBoxes(t *testing.T) {
	w := arena(t, mobtest.Options{}, 8)
	h := w.Spawn("minecraft:hoglin", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "spawn_adult"})
	w.Do(func(tx *world.Tx) {
		m, _ := h.Mob(tx)
		c, ok := mob.ComponentOf[*hostile.CustomHitTest](m)
		if !ok {
			t.Fatal("adult hoglin has no custom_hit_test")
		}
		boxes := c.HitBoxes(m)
		if len(boxes) != 1 {
			t.Fatalf("%d boxes", len(boxes))
		}
		b, p := boxes[0], m.Position()
		if !near(b.Width(), 2) || !near(b.Height(), 1.75) || !near(b.Min()[1], p[1]+1-0.875) {
			t.Errorf("hitbox %v at %v", b, p)
		}
	})
}

// TestHoglinGroupSize: group_size counts the adult hoglins within radius.
func TestHoglinGroupSize(t *testing.T) {
	w := arena(t, mobtest.Options{}, 16)
	var hs []*mobtest.Ref
	for i := 0; i < 3; i++ {
		hs = append(hs, w.Spawn("minecraft:hoglin", mgl64.Vec3{float64(i*3) + 0.5, 1, 0.5}, mob.SpawnOptions{Event: "spawn_adult"}))
	}
	w.Spawn("minecraft:hoglin", mgl64.Vec3{0.5, 1, 6.5}, mob.SpawnOptions{Event: "spawn_baby"})
	w.Tick(12)
	w.Do(func(tx *world.Tx) {
		for _, h := range hs {
			m, _ := h.Mob(tx)
			if n := hostile.GroupSizeOf(m); n != 3 {
				t.Errorf("group size %d, want 3 (babies excluded)", n)
			}
		}
	})
}

// TestReflectedVelocity: azimuth 180 sends a projectile straight back,
// scaled by reflection_scale.
func TestReflectedVelocity(t *testing.T) {
	w := arena(t, mobtest.Options{}, 8)
	b := w.Spawn("minecraft:breeze", mgl64.Vec3{0.5, 1, 0.5})
	c, err := hostile.Build(hostile.NewReflectProjectiles, "minecraft:reflect_projectiles", `{"azimuth_angle":"180.0","reflection_scale":"0.5","reflected_projectiles":["arrow"]}`)
	if err != nil {
		t.Fatal(err)
	}
	rp := c.(*hostile.ReflectProjectiles)
	w.Do(func(tx *world.Tx) {
		m, _ := b.Mob(tx)
		v := rp.Reflected(m, mgl64.Vec3{1, 0.2, 0})
		if !near(v[0], -0.5) || !near(v[1], 0.1) || math.Abs(v[2]) > 1e-9 {
			t.Errorf("reflected %v", v)
		}
		if !rp.Reflects("minecraft:arrow") || rp.Reflects("minecraft:snowball") {
			t.Error("reflected_projectiles not parsed")
		}
	})
}

// TestBreezeReflectsArrow: an arrow flying at a breeze is turned around
// before it hits and the breeze takes no damage.
func TestBreezeReflectsArrow(t *testing.T) {
	w := arena(t, mobtest.Options{}, 16)
	b := w.Spawn("minecraft:breeze", mgl64.Vec3{0.5, 1, 0.5})
	shooter := w.AddPlayer("shooter", mgl64.Vec3{-12.5, 1, 5.5})
	var arrow *mobtest.Ref
	w.Do(func(tx *world.Tx) {
		owner, _ := shooter.H().Entity(tx)
		h := entity.NewArrow(world.EntitySpawnOpts{Position: mgl64.Vec3{-6.5, 1.8, 0.5}, Velocity: mgl64.Vec3{1.5, 0, 0}}, owner)
		tx.AddEntity(h)
		arrow = w.Track(h)
	})
	var hp float64
	w.Do(func(tx *world.Tx) { m, _ := b.Mob(tx); hp = m.Health() })
	turned := false
	w.Each(20, func(tx *world.Tx, _ int) bool {
		e, ok := arrow.Entity(tx)
		if !ok {
			return false
		}
		if v := e.(interface{ Velocity() mgl64.Vec3 }).Velocity(); v[0] < 0 {
			turned = true
			return false
		}
		return true
	})
	if !turned {
		t.Fatalf("arrow was not reflected:\n%v", w.Dump(b))
	}
	w.Do(func(tx *world.Tx) {
		if m, _ := b.Mob(tx); m.Health() < hp {
			t.Errorf("breeze took damage: %v -> %v", hp, m.Health())
		}
	})
}
