//go:build crossfly_full

package boss_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// sensorSeen is what a sculk sensor showed while a test ran: whether it went active, and its power and
// frequency the first time it did.
type sensorSeen struct {
	active           bool
	power, frequency int
}

// watchSensor runs step n times, a tick each, and returns what the plain or calibrated sensor at pos showed.
func watchSensor(t *testing.T, w *mobtest.World, pos cube.Pos, n int, step func()) sensorSeen {
	t.Helper()
	var seen sensorSeen
	for range n {
		if step != nil {
			step()
		}
		w.Tick(1)
		w.Do(func(tx *world.Tx) {
			phase, power, freq := -1, 0, 0
			switch b := tx.Block(pos).(type) {
			case block.SculkSensor:
				phase, power, freq = b.Phase, b.Power, b.Frequency
			case block.CalibratedSculkSensor:
				phase, power, freq = b.Phase, b.Power, b.Frequency
			default:
				t.Fatalf("no sculk sensor at %v: %T", pos, b)
			}
			if phase == 1 && !seen.active {
				seen = sensorSeen{active: true, power: power, frequency: freq}
			}
		})
	}
	return seen
}

// pen walls the cell at pos in with stone two blocks high, so that a mob put there cannot walk off.
func pen(tx *world.Tx, pos cube.Pos) {
	for _, d := range cube.Directions() {
		for y := 0; y < 3; y++ {
			tx.SetBlock(pos.Side(d.Face()).Add(cube.Pos{0, y, 0}), block.Stone{}, nil)
		}
	}
}

// TestCalibratedSculkSensor: a calibrated sensor hears steps 12 blocks away (16 block range) with a signal
// of 15-floor(d/16*15). With a redstone block at its back (signal 15) it no longer hears steps, only
// frequency 15 (an explosion).
func TestCalibratedSculkSensor(t *testing.T) {
	w := sculkWorld(t)
	at := cube.Pos{12, 1, 0}
	w.Do(func(tx *world.Tx) { tx.SetBlock(at, block.CalibratedSculkSensor{Facing: cube.North}, nil) })
	p := w.AddPlayer("Steve", mgl64.Vec3{0.5, 1, -2.5})
	w.Tick(3)
	seen := watchSensor(t, w, at, 30, func() { p.With(func(pl *player.Player) { pl.Move(mgl64.Vec3{0, 0, 0.2}, 0, 0) }) })
	if !seen.active || seen.frequency != 1 || seen.power < 3 || seen.power > 5 {
		t.Fatalf("after a walk 12 blocks away: %+v, want active with frequency 1 and power about 4", seen)
	}
	w.Tick(30)

	w.Do(func(tx *world.Tx) { tx.SetBlock(at.Side(cube.FaceSouth), block.RedstoneBlock{}, nil) })
	seen = watchSensor(t, w, at, 30, func() { p.With(func(pl *player.Player) { pl.Move(mgl64.Vec3{0, 0, -0.2}, 0, 0) }) })
	if seen.active {
		t.Fatalf("a sensor tuned to 15 heard steps: %+v", seen)
	}
	w.Do(func(tx *world.Tx) {
		block.ExplosionConfig{}.Explode(tx, testExplosion{pos: mgl64.Vec3{6.5, 1, 6.5}})
	})
	if seen = watchSensor(t, w, at, 20, nil); !seen.active || seen.frequency != 15 {
		t.Errorf("a sensor tuned to 15 after an explosion: %+v, want active with frequency 15", seen)
	}
}

// TestAmethystResonance: an amethyst block next to a sensor that hears steps re-emits them as resonate_1,
// which a sensor 11 blocks from the player (out of its 8 block range for the steps) hears.
func TestAmethystResonance(t *testing.T) {
	w := sculkWorld(t)
	near, far := cube.Pos{3, 1, 0}, cube.Pos{11, 1, 0}
	w.Do(func(tx *world.Tx) {
		tx.SetBlock(near, block.SculkSensor{}, nil)
		tx.SetBlock(near.Side(cube.FaceEast), block.Amethyst{}, nil)
		tx.SetBlock(far, block.SculkSensor{}, nil)
	})
	p := w.AddPlayer("Alex", mgl64.Vec3{0.5, 1, -1.5})
	w.Tick(3)
	seen := watchSensor(t, w, far, 40, func() { p.With(func(pl *player.Player) { pl.Move(mgl64.Vec3{0, 0, 0.1}, 0, 0) }) })
	if !seen.active || seen.frequency != 1 {
		t.Fatalf("the far sensor: %+v, want active by the resonance with frequency 1 (near sensor %+v)", seen, sensorAt(t, w, near))
	}

	// Without the amethyst block, the far sensor hears nothing.
	w.Tick(60)
	w.Do(func(tx *world.Tx) { tx.SetBlock(near.Side(cube.FaceEast), nil, nil) })
	w.Tick(60)
	if seen := watchSensor(t, w, far, 40, func() { p.With(func(pl *player.Player) { pl.Move(mgl64.Vec3{0, 0, -0.1}, 0, 0) }) }); seen.active {
		t.Errorf("the far sensor went active without the amethyst block: %+v", seen)
	}
}

// TestMobOnSculkSensor: a mob that stands still on a sculk sensor touches it (sculk_touch, frequency 1, the
// strongest signal); a mob alone on a shrieker does not make it shriek, but one a player rides does.
func TestMobOnSculkSensor(t *testing.T) {
	w := sculkWorld(t)
	at := cube.Pos{3, 1, 3}
	w.Do(func(tx *world.Tx) {
		tx.SetBlock(at, block.SculkSensor{}, nil)
		pen(tx, at)
	})
	// A player near keeps the mobs simulated; standing still, it makes no vibration.
	p := w.AddPlayer("Steve", mgl64.Vec3{-2.5, 1, 2.5})
	w.Tick(2)
	w.Spawn("minecraft:pig", mgl64.Vec3{3.5, 1.5, 3.5})
	if seen := watchSensor(t, w, at, 100, nil); !seen.active || seen.frequency != 1 || seen.power != 15 {
		t.Fatalf("sensor under a pig: %+v, want active with frequency 1 and power 15", seen)
	}

	sh := cube.Pos{-3, 1, -3}
	w.Do(func(tx *world.Tx) {
		tx.SetBlock(sh, block.SculkShrieker{}, nil)
		pen(tx, sh)
	})
	pig := w.Spawn("minecraft:pig", mgl64.Vec3{-2.5, 1.5, -2.5})
	w.Tick(20)
	if s := shriekerAt(t, w, sh); s.Active {
		t.Fatal("a shrieker shrieked for a pig")
	}
	p.Teleport(mgl64.Vec3{-2.5, 1, -0.5})
	w.Tick(2)
	// The pig is saddled (its on_saddled event) and the player gets on.
	w.Do(func(tx *world.Tx) {
		m, _ := pig.Mob(tx)
		m.Fire("minecraft:on_saddled", nil)
		rideable, ok := entity.RideableOf(m)
		if !ok {
			t.Fatal("a pig is not rideable")
		}
		pl, _ := p.Entity(tx)
		pl.(*player.Player).MountEntity(tx, rideable, 0)
	})
	p.With(func(pl *player.Player) {
		if pl.RidingEntityHandle() != pig.H() {
			t.Skip("the player could not get on the pig")
		}
	})
	if _, ok := w.TickUntil(20, func(tx *world.Tx) bool {
		s, _ := tx.Block(sh).(block.SculkShrieker)
		return s.Active
	}); !ok {
		t.Error("a shrieker under a pig a player rides did not shriek")
	}
}

// TestBlockEventsWithoutPlayers: a lever flipped by nobody (block_activate, frequency 10) and a torch that
// pops off when its support goes (block_destroy, frequency 12) set off a sculk sensor.
func TestBlockEventsWithoutPlayers(t *testing.T) {
	w := sculkWorld(t)
	at := cube.Pos{0, 1, 0}
	lever, torch := cube.Pos{4, 1, 0}, cube.Pos{0, 2, 4}
	w.Do(func(tx *world.Tx) {
		tx.SetBlock(at, block.SculkSensor{}, nil)
		tx.SetBlock(lever, block.Lever{Facing: cube.FaceUp, Direction: cube.North}, nil)
		tx.SetBlock(torch.Side(cube.FaceDown), block.Stone{}, nil)
		tx.SetBlock(torch, block.Torch{Facing: cube.FaceDown, Type: block.NormalFire()}, nil)
	})
	w.Tick(3)
	w.Do(func(tx *world.Tx) {
		tx.Block(lever).(block.Lever).Activate(lever, cube.FaceUp, tx, nil, nil)
	})
	if seen := watchSensor(t, w, at, 10, nil); !seen.active || seen.frequency != 10 {
		t.Fatalf("after a lever flipped: %+v, want active with frequency 10", seen)
	}
	w.Tick(45)
	w.Do(func(tx *world.Tx) { tx.SetBlock(torch.Side(cube.FaceDown), nil, nil) })
	if seen := watchSensor(t, w, at, 10, nil); !seen.active || seen.frequency != 12 {
		t.Fatalf("after a torch popped off: %+v, want active with frequency 12", seen)
	}
}
