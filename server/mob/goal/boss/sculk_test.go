//go:build crossfly_full

package boss_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	bc "github.com/df-mc/dragonfly/server/mob/component/boss"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// The sculk blocks register with the mob blocks.
func init() { block.RegisterMobBlocks() }

func sculkWorld(t *testing.T) *mobtest.World {
	t.Helper()
	return setup(t, mobtest.Options{}, "minecraft:vibration_listener", "minecraft:anger_level", "minecraft:suspect_tracking")
}

// walk moves the player on the ground by d each tick for n ticks.
func walk(w *mobtest.World, p *mobtest.Player, d mgl64.Vec3, n int) {
	for range n {
		p.With(func(pl *player.Player) { pl.Move(d, 0, 0) })
		w.Tick(1)
	}
}

func sensorAt(t *testing.T, w *mobtest.World, pos cube.Pos) block.SculkSensor {
	t.Helper()
	var s block.SculkSensor
	w.Do(func(tx *world.Tx) {
		b, ok := tx.Block(pos).(block.SculkSensor)
		if !ok {
			t.Fatalf("no sculk sensor at %v: %T", pos, tx.Block(pos))
		}
		s = b
	})
	return s
}

func shriekerAt(t *testing.T, w *mobtest.World, pos cube.Pos) block.SculkShrieker {
	t.Helper()
	var s block.SculkShrieker
	w.Do(func(tx *world.Tx) {
		b, ok := tx.Block(pos).(block.SculkShrieker)
		if !ok {
			t.Fatalf("no sculk shrieker at %v: %T", pos, tx.Block(pos))
		}
		s = b
	})
	return s
}

// TestSculkSensorHearsSteps: a player walking 5 blocks from a sculk sensor activates it (phase 1) with a
// signal that falls with the distance; a sneaking one does not.
func TestSculkSensorHearsSteps(t *testing.T) {
	w := sculkWorld(t)
	at := cube.Pos{5, 1, 0}
	w.Do(func(tx *world.Tx) { tx.SetBlock(at, block.SculkSensor{}, nil) })
	p := w.AddPlayer("Steve", mgl64.Vec3{0.5, 1, -2.5})
	w.Tick(3)
	walk(w, p, mgl64.Vec3{0, 0, 0.2}, 15)
	s := sensorAt(t, w, at)
	if s.Phase != 1 {
		t.Fatalf("sensor phase %d after a walk 5 blocks away, want 1", s.Phase)
	}
	if s.Power < 4 || s.Power > 8 || s.Frequency != 1 {
		t.Errorf("power %d frequency %d, want a step's frequency 1 and power 15-floor(d/8*15) for d about 5", s.Power, s.Frequency)
	}
	w.Tick(45)
	if s := sensorAt(t, w, at); s.Phase != 0 {
		t.Errorf("phase %d 45 ticks later, want 0 (30 active, 10 cooldown)", s.Phase)
	}

	p.With(func(pl *player.Player) { pl.StartSneaking() })
	walk(w, p, mgl64.Vec3{0, 0, -0.1}, 20)
	if s := sensorAt(t, w, at); s.Phase != 0 {
		t.Errorf("a sneaking player activated the sensor (phase %d)", s.Phase)
	}
}

// TestSculkSensorWool: wool between the step and the sensor blocks the vibration.
func TestSculkSensorWool(t *testing.T) {
	w := sculkWorld(t)
	at := cube.Pos{5, 1, 0}
	w.Do(func(tx *world.Tx) {
		tx.SetBlock(at, block.SculkSensor{}, nil)
		for y := 1; y <= 3; y++ {
			for z := -3; z <= 3; z++ {
				tx.SetBlock(cube.Pos{3, y, z}, block.Wool{}, nil)
			}
		}
	})
	p := w.AddPlayer("Alex", mgl64.Vec3{0.5, 1, -1.5})
	w.Tick(3)
	walk(w, p, mgl64.Vec3{0, 0, 0.2}, 15)
	if s := sensorAt(t, w, at); s.Phase != 0 {
		t.Errorf("the sensor heard a step through wool (phase %d)", s.Phase)
	}
}

// TestSculkShriekerWarns: a player stepping on a shrieker that can summon raises their warning level to
// 1 with a 200 tick cooldown, the shrieker shrieks for 90 ticks, then answers with darkness. Stepping
// again during the cooldown does nothing.
func TestSculkShriekerWarns(t *testing.T) {
	w := sculkWorld(t)
	at := cube.Pos{0, 1, 4}
	w.Do(func(tx *world.Tx) { tx.SetBlock(at, block.SculkShrieker{CanSummon: true}, nil) })
	p := w.AddPlayer("Steve", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(2)
	step := func() {
		w.Do(func(tx *world.Tx) {
			pl, _ := p.Entity(tx)
			tx.Block(at).(block.SculkShrieker).EntityStepOn(at, tx, pl)
		})
	}
	step()
	if s := shriekerAt(t, w, at); !s.Active || s.WarningLevel != 1 {
		t.Fatalf("shrieker %+v after a step, want active with warning level 1", s)
	}
	p.With(func(pl *player.Player) {
		if l, c, _ := pl.WardenThreat(); l != 1 || c != 200 {
			t.Errorf("warning level %d cooldown %d, want 1 and 200", l, c)
		}
	})
	w.Tick(92)
	if s := shriekerAt(t, w, at); s.Active {
		t.Fatal("still shrieking after 90 ticks")
	}
	p.With(func(pl *player.Player) {
		if _, ok := pl.Effect(effect.Darkness); !ok {
			t.Error("no darkness after the shriek")
		}
	})
	step()
	if s := shriekerAt(t, w, at); s.Active {
		t.Error("shrieked again while the warning was cooling down")
	}
	p.With(func(pl *player.Player) {
		if l, _, _ := pl.WardenThreat(); l != 1 {
			t.Errorf("warning level %d after a step during the cooldown, want 1", l)
		}
	})
}

// TestSculkShriekerSummonsWarden: the fourth warning summons a warden that emerges within 5 blocks
// across of the shrieker; while it is near, shriekers stay silent.
func TestSculkShriekerSummonsWarden(t *testing.T) {
	w := sculkWorld(t)
	at := cube.Pos{0, 1, 0}
	w.Do(func(tx *world.Tx) { tx.SetBlock(at, block.SculkShrieker{CanSummon: true}, nil) })
	p := w.AddPlayer("Alex", mgl64.Vec3{3.5, 1, 3.5})
	w.Tick(2)
	p.With(func(pl *player.Player) { pl.SetWardenThreat(3, 0, 12000) })
	w.Do(func(tx *world.Tx) {
		pl, _ := p.Entity(tx)
		tx.Block(at).(block.SculkShrieker).EntityStepOn(at, tx, pl)
	})
	w.Tick(92)
	var found bool
	w.Do(func(tx *world.Tx) {
		for e := range tx.EntitiesWithin(cube.Box(-8, -8, -8, 8, 8, 8)) {
			m, ok := mob.Of(e)
			if !ok || m.Identifier() != bc.WardenID {
				continue
			}
			found = true
			if d := e.Position().Sub(at.Vec3Centre()); d[0] > 5.6 || d[0] < -5.6 || d[2] > 5.6 || d[2] < -5.6 {
				t.Errorf("warden at %v, more than 5 blocks from the shrieker", e.Position())
			}
			if !m.ActorFlag(protocol.EntityDataFlagEmerging) {
				t.Error("the summoned warden is not emerging")
			}
		}
	})
	if !found {
		t.Fatal("no warden after the fourth warning")
	}
	p.With(func(pl *player.Player) { pl.SetWardenThreat(1, 0, 12000) })
	w.Do(func(tx *world.Tx) {
		pl, _ := p.Entity(tx)
		tx.Block(at).(block.SculkShrieker).EntityStepOn(at, tx, pl)
	})
	if s := shriekerAt(t, w, at); s.Active {
		t.Error("shrieked with a warden near")
	}
}

// TestSculkSensorSetsOffShrieker: a sculk sensor clicking because of a player's steps sets off a shrieker
// 3 blocks from it.
func TestSculkSensorSetsOffShrieker(t *testing.T) {
	w := sculkWorld(t)
	sensor, shrieker := cube.Pos{4, 1, 0}, cube.Pos{7, 1, 0}
	w.Do(func(tx *world.Tx) {
		tx.SetBlock(sensor, block.SculkSensor{}, nil)
		tx.SetBlock(shrieker, block.SculkShrieker{}, nil)
	})
	p := w.AddPlayer("Steve", mgl64.Vec3{0.5, 1, -1.5})
	w.Tick(3)
	walk(w, p, mgl64.Vec3{0, 0, 0.2}, 12)
	w.Tick(6)
	if s := shriekerAt(t, w, shrieker); !s.Active {
		t.Errorf("the shrieker did not shriek after the sensor clicked (sensor %+v)", sensorAt(t, w, sensor))
	}
}

// TestWardenHearsWorldEvents: a warden hears a door opened, a block placed and an explosion, besides
// movement.
func TestWardenHearsWorldEvents(t *testing.T) {
	w := sculkWorld(t)
	r := w.Spawn(bc.WardenID, mgl64.Vec3{0.5, 1, 0.5})
	door := cube.Pos{6, 1, 0}
	w.Do(func(tx *world.Tx) {
		tx.SetBlock(door, block.WoodDoor{Wood: block.OakWood()}, nil)
		tx.SetBlock(door.Side(cube.FaceUp), block.WoodDoor{Wood: block.OakWood(), Top: true}, nil)
	})
	p := w.AddPlayer("Alex", mgl64.Vec3{8.5, 1, 4.5})
	w.Tick(2)
	received := func() int {
		n := 0
		w.Do(func(tx *world.Tx) {
			l, _ := bc.ListenerOf(mobOf(t, tx, r))
			n = l.Received
		})
		return n
	}
	w.Do(func(tx *world.Tx) {
		pl, _ := p.Entity(tx)
		tx.Block(door).(block.WoodDoor).Activate(door, cube.FaceNorth, tx, pl.(*player.Player), nil)
	})
	w.Tick(12)
	if received() != 1 {
		t.Fatalf("the warden heard %d vibrations after a door opened, want 1", received())
	}
	w.Do(func(tx *world.Tx) {
		l, _ := bc.ListenerOf(mobOf(t, tx, r))
		if d := l.LastPos.Sub(door.Vec3Centre()).Len(); d > 1 {
			t.Errorf("heard something %.1f blocks from the door", d)
		}
	})
	w.Tick(45) // the listener's 40 tick cooldown
	w.Do(func(tx *world.Tx) {
		block.ExplosionConfig{}.Explode(tx, testExplosion{pos: mgl64.Vec3{-6.5, 1, 0.5}})
	})
	w.Tick(12)
	if received() != 2 {
		t.Errorf("the warden heard %d vibrations after an explosion, want 2", received())
	}
}

// testExplosion is a small explosion at pos.
type testExplosion struct{ pos mgl64.Vec3 }

func (e testExplosion) Size() float64        { return 1 }
func (e testExplosion) Position() mgl64.Vec3 { return e.pos }

// TestWardenAngerSaved: the warden's anger at a player is saved with it and comes back, matched to the
// player once found.
func TestWardenAngerSaved(t *testing.T) {
	w := sculkWorld(t)
	r := w.Spawn(bc.WardenID, mgl64.Vec3{0.5, 1, 0.5})
	r2 := w.Spawn(bc.WardenID, mgl64.Vec3{-4.5, 1, 0.5})
	p := w.AddPlayer("Steve", mgl64.Vec3{12.5, 1, 0.5})
	w.Tick(2)
	var saved map[string]any
	w.Do(func(tx *world.Tx) {
		m := mobOf(t, tx, r)
		pl, _ := p.Entity(tx)
		a := anger(t, m)
		a.IncreaseAngerAt(m, pl, 60)
		saved = a.EncodeNBT()
	})
	if saved == nil {
		t.Fatal("no anger saved")
	}
	// As stored in the world: through NBT.
	raw, err := nbt.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	saved = nil
	if err := nbt.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	w.Do(func(tx *world.Tx) {
		m := mobOf(t, tx, r2)
		anger(t, m).DecodeNBT(m, saved)
	})
	w.Tick(21)
	w.Do(func(tx *world.Tx) {
		pl, _ := p.Entity(tx)
		if got := anger(t, mobOf(t, tx, r2)).AngerAt(pl); got < 55 || got > 60 {
			t.Errorf("restored anger %d, want about 60", got)
		}
	})
}
