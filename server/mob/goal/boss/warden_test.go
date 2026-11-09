//go:build crossfly_full

package boss_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	bc "github.com/df-mc/dragonfly/server/mob/component/boss"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

func anger(t *testing.T, m *mob.Mob) *bc.AngerLevel {
	t.Helper()
	a, ok := bc.AngerOf(m)
	if !ok {
		t.Fatal("no anger_level")
	}
	return a
}

// TestAngerLevel: the warden's JSON values, increases capped at max_anger,
// and the decay of 1 a second.
func TestAngerLevel(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:anger_level")
	p := w.AddPlayer("nuisance", mgl64.Vec3{12.5, 1, 0.5})
	r := w.Spawn(bc.WardenID, mgl64.Vec3{0.5, 1, 0.5})
	w.Do(func(tx *world.Tx) {
		m := mobOf(t, tx, r)
		a := anger(t, m)
		if a.Threshold != 80 || a.Boost != 20 || a.MaxAnger != 150 || a.Annoyingness != 35 || a.ProjectileAnnoyingness != 10 {
			t.Errorf("anger_level %+v", a)
		}
		pl, _ := p.Entity(tx)
		a.TryIncreaseAngerAt(m, pl, 35, false)
		if a.AngerAt(pl) != 35 || a.IsAngry() {
			t.Errorf("anger %d angry %v", a.AngerAt(pl), a.IsAngry())
		}
		a.TryIncreaseAngerAt(m, pl, 500, false)
		if a.AngerAt(pl) != 150 {
			t.Errorf("anger %d, want the 150 cap", a.AngerAt(pl))
		}
		if !a.IsAngry() {
			t.Errorf("not angry at 150")
		}
	})
	w.Tick(40)
	w.Do(func(tx *world.Tx) {
		m := mobOf(t, tx, r)
		pl, _ := p.Entity(tx)
		if got := anger(t, m).AngerAt(pl); got < 147 || got > 149 {
			t.Errorf("anger %d after 2 s, want about 148", got)
		}
	})
}

// TestWardenHurt: a hit makes the warden furious (threshold + boost) at
// the attacker, who becomes the target.
func TestWardenHurt(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:anger_level")
	p := w.AddPlayer("attacker", mgl64.Vec3{2.5, 1, 0.5})
	r := w.Spawn(bc.WardenID, mgl64.Vec3{0.5, 1, 0.5})
	w.Do(func(tx *world.Tx) {
		m := mobOf(t, tx, r)
		pl, _ := p.Entity(tx)
		m.Hurt(2, entity.AttackDamageSource{Attacker: pl})
		if got := anger(t, m).AngerAt(pl); got != 100 {
			t.Errorf("anger %d after a hit, want 100", got)
		}
		if m.TargetHandle() != pl.H() {
			t.Errorf("attacker is not the target")
		}
	})
}

// TestVibration: a vibration reaches the warden after its distance in ticks;
// the warden shivers, gets annoyed (35) and suspicious of the spot; for 40
// ticks it hears nothing else.
func TestVibration(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:vibration_listener", "minecraft:anger_level", "minecraft:suspect_tracking")
	p := w.AddPlayer("walker", mgl64.Vec3{8.5, 1, 0.5})
	r := w.Spawn(bc.WardenID, mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(2)
	w.Do(func(tx *world.Tx) {
		m := mobOf(t, tx, r)
		pl, _ := p.Entity(tx)
		l, ok := bc.ListenerOf(m)
		if !ok {
			t.Fatal("no listener")
		}
		l.Hear(m, bc.Vibration{Event: bc.VibrationStep, Pos: pl.Position(), Source: pl.H()})
	})
	w.Tick(12)
	w.Do(func(tx *world.Tx) {
		m := mobOf(t, tx, r)
		pl, _ := p.Entity(tx)
		l, _ := bc.ListenerOf(m)
		if l.Received != 1 {
			t.Fatalf("received %d vibrations", l.Received)
		}
		if got := anger(t, m).AngerAt(pl); got != 35 {
			t.Errorf("anger %d after a step, want 35", got)
		}
		s, _ := bc.SuspectOf(m)
		if pos, ok := s.SuspiciousPos(); !ok || pos.Vec3().Sub(pl.Position()).Len() > 2 {
			t.Errorf("suspicious pos %v %v", pos, ok)
		}
		l.Hear(m, bc.Vibration{Event: bc.VibrationStep, Pos: pl.Position(), Source: pl.H()})
	})
	w.Tick(12)
	w.Do(func(tx *world.Tx) {
		l, _ := bc.ListenerOf(mobOf(t, tx, r))
		if l.Received != 1 {
			t.Errorf("a second vibration within the cooldown arrived")
		}
	})
}

// TestVibrationFromMovement: a mob walking near the warden is heard; one
// walking on wool is not.
func TestVibrationFromMovement(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:vibration_listener", "minecraft:anger_level")
	r := w.Spawn(bc.WardenID, mgl64.Vec3{0.5, 1, 0.5})
	pig := w.Spawn("minecraft:pig", mgl64.Vec3{8.5, 1, 0.5})
	w.Tick(2)
	w.Each(60, func(tx *world.Tx, _ int) bool {
		m := mobOf(t, tx, pig)
		m.SetVelocity(mgl64.Vec3{0, m.Velocity()[1], 0.15})
		return true
	})
	w.Do(func(tx *world.Tx) {
		l, _ := bc.ListenerOf(mobOf(t, tx, r))
		if l.Received == 0 {
			t.Errorf("the walking pig was not heard")
		}
	})
}

// TestWardenRoar: angry at a player without a target, the warden roars
// (actor flag roaring) for 4.2 s and then targets it.
func TestWardenRoar(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:behavior.roar", "minecraft:anger_level")
	p := w.AddPlayer("victim", mgl64.Vec3{6.5, 1, 0.5})
	r := w.Spawn(bc.WardenID, mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(2)
	w.Do(func(tx *world.Tx) {
		m := mobOf(t, tx, r)
		pl, _ := p.Entity(tx)
		anger(t, m).TryIncreaseAngerAt(m, pl, 90, false)
	})
	w.Tick(2)
	with(t, w, r, func(tx *world.Tx, m *mob.Mob) {
		if !m.ActorFlag(protocol.EntityDataFlagRoaring) {
			t.Fatalf("not roaring: %v", m.Goals().RunningGoals())
		}
	})
	n, ok := w.TickUntil(120, func(tx *world.Tx) bool {
		m := mobOf(t, tx, r)
		_, has := m.Target()
		return has
	})
	if !ok || n < 70 {
		t.Fatalf("target after %d ticks (%v), want after the 84-tick roar", n, ok)
	}
	with(t, w, r, func(tx *world.Tx, m *mob.Mob) {
		pl, _ := p.Entity(tx)
		if got := anger(t, m).AngerAt(pl); got < 105 {
			t.Errorf("anger %d after the roar, want 90+20-decay", got)
		}
	})
}

// TestSonicBoom: a target out of reach is hit by the sonic boom once the
// 10-second new-target cooldown is over, for 10 through armour, and
// knocked back.
func TestSonicBoom(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:behavior.sonic_boom", "minecraft:anger_level")
	w.Do(func(tx *world.Tx) {
		w.Platform(tx, cube.Pos{8, 1, -1}, cube.Pos{10, 1, 1}, 4, block.Stone{})
	})
	p := w.AddPlayer("target", mgl64.Vec3{9.5, 6, 0.5})
	r := w.Spawn(bc.WardenID, mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(2)
	w.Do(func(tx *world.Tx) {
		pl, _ := p.Entity(tx)
		m := mobOf(t, tx, r)
		anger(t, m).TryIncreaseAngerAt(m, pl, 150, false)
		m.SetTarget(pl)
	})
	start := p.Health()
	n, ok := w.TickUntil(400, func(tx *world.Tx) bool { return p.HealthTx(tx) < start })
	if !ok {
		t.Fatalf("no sonic boom:\n%v", w.Dump(r))
	}
	if n < 200 {
		t.Errorf("boom after %d ticks, before the 200-tick cooldown", n)
	}
	if got := start - p.Health(); math.Abs(got-10) > 0.5 {
		t.Errorf("sonic boom dealt %v, want 10", got)
	}
}

// TestWardenEmerge: spawn_emerging makes the warden emerge for 7 s
// (actor flag emerging, invulnerable), then emerged.
func TestWardenEmerge(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:behavior.emerge")
	r := w.Spawn(bc.WardenID, mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_emerging"})
	w.Tick(2)
	with(t, w, r, func(tx *world.Tx, m *mob.Mob) {
		if !m.ActorFlag(protocol.EntityDataFlagEmerging) {
			t.Fatalf("not emerging: %v", m.Components())
		}
		if _, res := m.Hurt(5, entity.AttackDamageSource{}); res.Accepted() {
			t.Errorf("hurt while emerging")
		}
	})
	w.Tick(141)
	with(t, w, r, func(tx *world.Tx, m *mob.Mob) {
		if m.ActorFlag(protocol.EntityDataFlagEmerging) || m.HasComponent("minecraft:behavior.emerge") {
			t.Errorf("still emerging after 7 s")
		}
	})
}

// TestWardenDig: after 60 s without vibrations or suspicion the warden
// digs down (actor flag digging) and is gone 5.5 s later.
func TestWardenDig(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:behavior.dig")
	r := w.Spawn(bc.WardenID, mgl64.Vec3{0.5, 1, 0.5})
	n, ok := w.TickUntil(1300, func(tx *world.Tx) bool {
		m, ok := r.Mob(tx)
		return ok && m.ActorFlag(protocol.EntityDataFlagDigging)
	})
	if !ok || n < 1190 {
		t.Fatalf("dig after %d ticks (%v), want 1200", n, ok)
	}
	if _, ok := w.TickUntil(120, func(*world.Tx) bool { return r.Gone() }); !ok {
		t.Fatalf("still there after digging")
	}
}

// TestWardenInvestigates: the warden walks to its suspicious position and
// forgets it there.
func TestWardenInvestigates(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:behavior.investigate_suspicious_location", "minecraft:suspect_tracking")
	r := w.Spawn(bc.WardenID, mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(2)
	w.Do(func(tx *world.Tx) {
		m := mobOf(t, tx, r)
		s, _ := bc.SuspectOf(m)
		s.SetSuspiciousPos(m, cube.Pos{10, 1, 0})
	})
	_, ok := w.TickUntil(400, func(tx *world.Tx) bool {
		m := mobOf(t, tx, r)
		s, _ := bc.SuspectOf(m)
		_, has := s.SuspiciousPos()
		return !has
	})
	if !ok {
		t.Fatalf("never got there:\n%v", w.Dump(r))
	}
	if d := r.Pos().Sub(mgl64.Vec3{10.5, 1, 0.5}).Len(); d > 3 {
		t.Errorf("forgot the spot %.1f blocks from it", d)
	}
}

// TestWardenClientData: the heartbeat interval (2 s calm, 0.5 s at 80
// anger) and the step sound offset are actor data.
func TestWardenClientData(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:heartbeat", "minecraft:movement_sound_distance_offset")
	p := w.AddPlayer("p", mgl64.Vec3{6.5, 1, 0.5})
	r := w.Spawn(bc.WardenID, mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(2)
	w.Do(func(tx *world.Tx) {
		m := mobOf(t, tx, r)
		if v, _ := m.ActorData(protocol.EntityDataKeyHeartbeatIntervalTicks); v != int32(40) {
			t.Errorf("calm heartbeat %v, want 40", v)
		}
		if v, _ := m.ActorData(protocol.EntityDataKeyMovementSoundDistanceOffset); v != float32(0.55) {
			t.Errorf("sound offset %v", v)
		}
		pl, _ := p.Entity(tx)
		m.SetTarget(pl)
		anger(t, m).TryIncreaseAngerAt(m, pl, 100, false)
	})
	w.Tick(1)
	with(t, w, r, func(tx *world.Tx, m *mob.Mob) {
		if v, _ := m.ActorData(protocol.EntityDataKeyHeartbeatIntervalTicks); v != int32(10) {
			t.Errorf("angry heartbeat %v, want 10", v)
		}
	})
}

// TestCreakingCrumbles: minecraft:crumble adds instant_despawn and the
// creaking is gone on its next tick.
func TestCreakingCrumbles(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:instant_despawn")
	r := w.Spawn("minecraft:creaking", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(2)
	with(t, w, r, func(tx *world.Tx, m *mob.Mob) { m.Fire("minecraft:crumble", nil) })
	w.Tick(2)
	if !r.Gone() {
		t.Fatalf("creaking still there")
	}
}

// TestBodyRotationBlocked: a twitching creaking (minecraft:immobile) keeps
// its body yaw.
func TestBodyRotationBlocked(t *testing.T) {
	w := setup(t, mobtest.Options{}, "minecraft:body_rotation_blocked")
	r := w.Spawn("minecraft:creaking", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(2)
	var yaw float64
	with(t, w, r, func(tx *world.Tx, m *mob.Mob) {
		m.SetBodyYaw(30)
		m.SetYaw(30)
		m.Fire("minecraft:start_twitching", nil)
		if !m.HasComponent("minecraft:body_rotation_blocked") {
			t.Fatalf("no body_rotation_blocked: %v", m.Components())
		}
		yaw = m.BodyYaw()
	})
	w.Tick(20)
	with(t, w, r, func(tx *world.Tx, m *mob.Mob) {
		if m.BodyYaw() != yaw {
			t.Errorf("body yaw %v, want %v", m.BodyYaw(), yaw)
		}
	})
}
