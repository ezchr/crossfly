//go:build crossfly_full

package spawn

import (
	"math"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/data"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

var phantomOnce sync.Once

// phantomWorld is newWorld with the phantom type registered.
func phantomWorld(t *testing.T) (*world.World, *world.EntityHandle) {
	testTypes(t)
	phantomOnce.Do(func() {
		raw, _ := data.Entity("phantom.json")
		d, err := mob.ParseDefinition(raw)
		if err != nil {
			panic(err)
		}
		mob.RegisterType(d)
	})
	return newWorld(t, nil)
}

func playerOf(tx *world.Tx, h *world.EntityHandle) *player.Player {
	e, _ := h.Entity(tx)
	return e.(*player.Player)
}

// TestInsomniaCounting: InsomniaSystem counts every tick for players that
// are awake and not spectating; sleeping and dying restart it.
func TestInsomniaCounting(t *testing.T) {
	w, h := newWorld(t, nil)
	tick := func(n int) {
		for range n {
			run(w, insomniaTick)
		}
	}
	rest := func() (v int64) {
		run(w, func(tx *world.Tx) { v = playerOf(tx, h).TimeSinceRest() })
		return v
	}
	base := rest()
	tick(100)
	if got := rest() - base; got != 100 {
		t.Fatalf("awake: counted %d in 100 ticks", got)
	}
	run(w, func(tx *world.Tx) { playerOf(tx, h).SetGameMode(world.GameModeSpectator) })
	before := rest()
	tick(50)
	if rest() != before {
		t.Fatal("spectator: insomnia counted")
	}
	run(w, func(tx *world.Tx) { playerOf(tx, h).SetGameMode(world.GameModeCreative) })
	tick(10)
	if rest() != before+10 {
		t.Fatal("creative: insomnia not counted (BDS only skips spectators)")
	}
	bed := cube.Pos{3, floorY + 1, 0}
	run(w, func(tx *world.Tx) {
		tx.SetBlock(bed, block.Bed{Facing: cube.North, Head: true}, nil)
		tx.SetBlock(bed.Side(cube.FaceSouth), block.Bed{Facing: cube.North}, nil)
		playerOf(tx, h).Sleep(bed)
	})
	tick(5)
	if got := rest(); got != 0 {
		t.Fatalf("sleeping: time since rest %d, want 0", got)
	}
	run(w, func(tx *world.Tx) { playerOf(tx, h).Wake() })
	tick(30)
	if got := rest(); got != 30 {
		t.Fatalf("after waking: %d, want 30", got)
	}
	run(w, func(tx *world.Tx) {
		p := playerOf(tx, h)
		p.SetGameMode(world.GameModeSurvival)
		p.Hurt(1000, entity.VoidDamageSource{})
	})
	tick(1)
	if got := rest(); got != 0 {
		t.Fatalf("dead: time since rest %d, want 0", got)
	}
}

// TestPhantomSpawnRule: at night phantoms spawn 20-34 blocks above a
// player who has not slept for 3 days; never by day or over a rested
// player.
func TestPhantomSpawnRule(t *testing.T) {
	setRest := func(w *world.World, h *world.EntityHandle, v int64) {
		run(w, func(tx *world.Tx) { playerOf(tx, h).SetTimeSinceRest(v) })
	}
	// Rested: random(rest) is below 72000.
	w, h := phantomWorld(t)
	w.SetTime(18000)
	setRest(w, h, 72000)
	Enable(w, Config{ManualTick: true, Seed: 7, Categories: []string{"monster"}})
	if got := spawnNoTicks(w, 3000); len(got[phantomID]) != 0 {
		t.Fatalf("%d phantoms over a rested player", len(got[phantomID]))
	}

	// Insomnia by day: too bright up there.
	w, h = phantomWorld(t)
	w.SetTime(6000)
	setRest(w, h, 1<<30)
	Enable(w, Config{ManualTick: true, Seed: 7, Categories: []string{"monster"}})
	if got := spawnNoTicks(w, 3000); len(got[phantomID]) != 0 {
		t.Fatalf("%d phantoms at noon", len(got[phantomID]))
	}

	// Insomnia at night.
	w, h = phantomWorld(t)
	w.SetTime(18000)
	setRest(w, h, 1<<30)
	Enable(w, Config{ManualTick: true, Seed: 7, Categories: []string{"monster"}})
	got := spawnNoTicks(w, 3000)
	if len(got[phantomID]) == 0 {
		t.Fatal("no phantoms over a player with insomnia at midnight")
	}
	t.Logf("%d phantoms spawned", len(got[phantomID]))
	run(w, func(tx *world.Tx) {
		eye := playerOf(tx, h).Position()[1] + playerEyeHeight
		for _, ph := range got[phantomID] {
			e, _ := ph.Entity(tx)
			pos := e.Position()
			if pos[0] != math.Floor(pos[0]) || pos[2] != math.Floor(pos[2]) {
				t.Errorf("phantom not at a block corner: %v", pos)
			}
			if dy := pos[1] - math.Floor(eye); dy < 20 || dy > 34 {
				t.Errorf("phantom %v is %.0f blocks above the eye block, want 20-34", pos, dy)
			}
			if math.Abs(pos[0]) > 11 || math.Abs(pos[2]) > 11 {
				t.Errorf("phantom %v not within 10 blocks of the player horizontally", pos)
			}
		}
		if n := phantomsAround(tx, playerOf(tx, h).Position()); n > 5 {
			t.Errorf("%d phantoms around the player, the rule stops at 5", n)
		}
	})
}
