//go:build crossfly_full

package hostile

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// pool fills x, z in [-r, r], y in [1, depth] with still water over a stone
// floor and walls.
func pool(w *mobtest.World, r, depth int) {
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, r+2, block.Stone{})
		for x := -r - 1; x <= r+1; x++ {
			for z := -r - 1; z <= r+1; z++ {
				for y := 1; y <= depth+1; y++ {
					if x < -r || x > r || z < -r || z > r {
						tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
					} else if y <= depth {
						tx.SetBlock(cube.Pos{x, y, z}, block.Water{Still: true, Depth: 8}, nil)
					}
				}
			}
		}
	})
}

func positionOf(t *testing.T, w *mobtest.World, r *mobtest.Ref) (p mgl64.Vec3) {
	withJ(t, w, r, func(m *javamob.Mob) { p = m.Position() })
	return p
}

// TestSquidSwims: a squid in a pool strokes around and stays in the water.
func TestSquidSwims(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	pool(w, 6, 5)
	s := spawnJ(t, w, squidID, mgl64.Vec3{0.5, 2.5, 0.5})
	start := positionOf(t, w, s)
	moved := 0.0
	for i := 0; i < 20; i++ {
		w.Tick(10)
		p := positionOf(t, w, s)
		moved = max(moved, p.Sub(start).Len())
		if p[1] < 1 || p[1] > 6 {
			t.Fatalf("squid left the water: %v", p)
		}
	}
	if moved < 1 {
		t.Errorf("squid moved %.2f blocks in 200 ticks, want it to swim", moved)
	}
}

// TestSquidDriesOut: on land a squid falls and, after its 300 ticks of air
// and 20 more, takes 2 dry-out damage every 20 ticks.
func TestSquidDriesOut(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	s := spawnJ(t, w, squidID, mgl64.Vec3{0.5, 3, 0.5})
	w.Tick(300)
	withJ(t, w, s, func(m *javamob.Mob) {
		if m.Health() != SquidMaxHealth {
			t.Fatalf("hurt before its air ran out: %v", m.Health())
		}
		if m.Position()[1] > 1.01 {
			t.Errorf("squid at y %.2f, want it to fall to the floor", m.Position()[1])
		}
	})
	w.Tick(30)
	withJ(t, w, s, func(m *javamob.Mob) {
		if m.Health() != SquidMaxHealth-2 {
			t.Errorf("health %v after 330 ticks on land, want %v", m.Health(), SquidMaxHealth-2)
		}
	})
}

// TestCodSwimsAndSchools: cod spawned as a group school up behind the first
// and swim about the pool on the water-bound navigation.
func TestCodSwimsAndSchools(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	pool(w, 7, 5)
	var refs []*mobtest.Ref
	w.Do(func(tx *world.Tx) {
		var group any
		for i := 0; i < 3; i++ {
			m, err := javamob.Spawn(tx, codID, mgl64.Vec3{0.5 + float64(i), 2.5, 0.5}, javamob.SpawnNatural, javamob.SpawnOptions{GroupData: &group})
			if err != nil {
				t.Fatal(err)
			}
			refs = append(refs, w.Track(m.H()))
		}
	})
	withJ(t, w, refs[1], func(m *javamob.Mob) {
		if !isFollower(m.Mob) {
			t.Errorf("second cod of the group does not follow the first")
		}
	})
	start := positionOf(t, w, refs[0])
	moved := 0.0
	for i := 0; i < 40; i++ {
		w.Tick(10)
		p := positionOf(t, w, refs[0])
		moved = max(moved, p.Sub(start).Len())
		if p[1] < 0.9 || p[1] > 6 {
			t.Fatalf("cod left the water: %v", p)
		}
	}
	if moved < 1 {
		t.Errorf("leader moved %.2f blocks in 400 ticks, want it to swim", moved)
	}
}

// TestCodFlopsOnLand: a cod on land flops and dries out after 320 ticks.
func TestCodFlopsOnLand(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	c := spawnJ(t, w, codID, mgl64.Vec3{0.5, 1, 0.5})
	maxY := 0.0
	for i := 0; i < 40; i++ {
		w.Tick(1)
		maxY = max(maxY, positionOf(t, w, c)[1])
	}
	if maxY < 1.2 {
		t.Errorf("cod never hopped (max y %.2f)", maxY)
	}
	w.Tick(300)
	withJ(t, w, c, func(m *javamob.Mob) {
		if m.Health() >= FishMaxHealth {
			t.Errorf("cod not dried out after 340 ticks on land")
		}
	})
}

// TestPufferfishPuffsAndStings: a survival player next to a pufferfish makes
// it puff up and gets stung with poison.
func TestPufferfishPuffsAndStings(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	pool(w, 4, 4)
	f := spawnJ(t, w, pufferfishID, mgl64.Vec3{0.5, 2, 0.5})
	withJ(t, w, f, func(m *javamob.Mob) {
		if m.Width() > 0.4 {
			t.Errorf("deflated pufferfish is %.2f wide, want 0.35", m.Width())
		}
	})
	p := w.AddPlayer("tester", mgl64.Vec3{1.2, 2, 0.5})
	stung := false
	for i := 0; i < 60 && !stung; i++ {
		w.Tick(1)
		stung = hasEffect(p, effect.Poison)
	}
	withJ(t, w, f, func(m *javamob.Mob) {
		if fishOf(m.Mob).PuffState() == PuffSmall {
			t.Errorf("pufferfish did not puff up")
		}
	})
	if !stung {
		t.Errorf("player next to a puffed pufferfish was not poisoned")
	}
}

// TestGuardianBeam: a guardian targets a survival player 5 blocks away it
// can see and its beam hurts the player once the attack time runs out.
func TestGuardianBeam(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	pool(w, 7, 5)
	g := spawnJ(t, w, guardianID, mgl64.Vec3{0.5, 2, 0.5})
	p := w.AddPlayer("tester", mgl64.Vec3{5.5, 2, 0.5})
	hurt := false
	for i := 0; i < 160 && !hurt; i++ {
		w.Tick(1)
		p.With(func(pl *player.Player) { hurt = pl.Health() < pl.MaxHealth() })
	}
	if !hurt {
		withJ(t, w, g, func(m *javamob.Mob) {
			_, ok := m.Target()
			t.Errorf("player not hurt by the beam in 160 ticks (guardian has target: %v)", ok)
		})
	}
}

// TestPhantomCirclesAnchor: a phantom spawned in the air does not fall; it
// circles its anchor, 5 blocks above where it spawned.
func TestPhantomCirclesAnchor(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Midnight})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 16, block.Stone{}) })
	var ph *mobtest.Ref
	w.Do(func(tx *world.Tx) {
		m, err := javamob.Spawn(tx, phantomID, mgl64.Vec3{0.5, 20, 0.5}, javamob.SpawnNatural, javamob.SpawnOptions{})
		if err != nil {
			t.Fatal(err)
		}
		ph = w.Track(m.H())
	})
	start := positionOf(t, w, ph)
	moved, minY := 0.0, 100.0
	for i := 0; i < 20; i++ {
		w.Tick(10)
		p := positionOf(t, w, ph)
		moved = max(moved, p.Sub(start).Len())
		minY = min(minY, p[1])
	}
	if minY < 10 {
		t.Errorf("phantom sank to y %.1f, want it flying near its anchor (y 25)", minY)
	}
	if moved < 3 {
		t.Errorf("phantom moved %.2f blocks in 200 ticks, want it circling", moved)
	}
}
