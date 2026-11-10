//go:build crossfly_full

package jnether

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// portalColumn puts obsidian at y=4 and portal blocks at y=5..7 at x=z=0,
// without block updates (no frame).
func portalColumn(w *testWorld, floor world.Block) {
	w.do(func(tx *world.Tx) {
		opts := &world.SetOpts{DisableBlockUpdates: true, DisableLiquidDisplacement: true}
		tx.SetBlock(cube.Pos{0, 4, 0}, floor, opts)
		for y := 5; y <= 7; y++ {
			tx.SetBlock(cube.Pos{0, y, 0}, block.Portal{Axis: cube.X}, opts)
		}
	})
}

// itemSpawner spawns an item entity as a stand-in zombified piglin and
// records where.
type itemSpawner struct {
	n    int
	last mgl64.Vec3
	h    *world.EntityHandle
}

func (s *itemSpawner) spawn(tx *world.Tx, pos mgl64.Vec3, _ float64) world.Entity {
	s.n++
	s.last = pos
	s.h = world.EntitySpawnOpts{Position: pos}.New(entity.ItemType, entity.ItemBehaviourConfig{Item: item.NewStack(item.GoldNugget{}, 1)})
	e := tx.AddEntity(s.h)
	return e
}

func runPortalTicks(t *testing.T, dim world.Dimension, diff world.Difficulty, floor world.Block, n int, near bool) *itemSpawner {
	t.Helper()
	w := newTestWorld(t, dim, diff)
	portalColumn(w, floor)
	s := &itemSpawner{}
	InstallPortalSpawning(PortalSpawnHooks{
		SpawnZombifiedPiglin: s.spawn,
		PlayerNear:           func(*world.Tx, cube.Pos) bool { return near },
	})
	t.Cleanup(func() { block.PortalRandomTick = nil; portalHooks = PortalSpawnHooks{} })
	r := rand.New(rand.NewPCG(7, 11))
	w.do(func(tx *world.Tx) {
		for range n {
			tx.Block(cube.Pos{0, 6, 0}).(world.RandomTicker).RandomTick(cube.Pos{0, 6, 0}, tx, r)
		}
	})
	return s
}

// TestPortalSpawnChance: random.nextInt(2000) < difficulty id, so 1, 2 and
// 3 in 2000 random ticks on easy, normal and hard, none on peaceful.
func TestPortalSpawnChance(t *testing.T) {
	const n = 400000
	for _, c := range []struct {
		diff world.Difficulty
		id   int
	}{{world.DifficultyPeaceful, 0}, {world.DifficultyEasy, 1}, {world.DifficultyNormal, 2}, {world.DifficultyHard, 3}} {
		s := runPortalTicks(t, world.Overworld, c.diff, block.Obsidian{}, n, true)
		want := float64(n) * float64(c.id) / 2000
		sd := math.Sqrt(want)
		if math.Abs(float64(s.n)-want) > 4*sd+0.5 {
			t.Errorf("difficulty %d: %d spawns in %d ticks, want about %.0f", c.id, s.n, n, want)
		}
	}
}

// TestPortalSpawnPlace: the piglin stands on the block under the portal
// column (in the bottom portal block), centred, with the portal cooldown.
func TestPortalSpawnPlace(t *testing.T) {
	s := runPortalTicks(t, world.Overworld, world.DifficultyHard, block.Obsidian{}, 20000, true)
	if s.n == 0 {
		t.Fatal("no spawn")
	}
	if want := (mgl64.Vec3{0.5, 5, 0.5}); s.last != want {
		t.Fatalf("spawned at %v, want %v", s.last, want)
	}
}

func TestPortalSpawnCooldown(t *testing.T) {
	w := newTestWorld(t, world.Overworld, world.DifficultyHard)
	portalColumn(w, block.Obsidian{})
	s := &itemSpawner{}
	InstallPortalSpawning(PortalSpawnHooks{SpawnZombifiedPiglin: s.spawn, PlayerNear: func(*world.Tx, cube.Pos) bool { return true }})
	t.Cleanup(func() { block.PortalRandomTick = nil; portalHooks = PortalSpawnHooks{} })
	r := rand.New(rand.NewPCG(3, 4))
	w.do(func(tx *world.Tx) {
		for s.n == 0 {
			portalRandomTick(cube.Pos{0, 7, 0}, tx, r)
		}
		e, ok := s.h.Entity(tx)
		if !ok {
			t.Fatal("spawned entity gone")
		}
		pt := e.(*entity.Ent).Behaviour().(interface {
			PortalTravelComputer() *entity.PortalTravelComputer
		}).PortalTravelComputer()
		if !pt.OnCooldown() {
			t.Fatal("no portal cooldown on the spawned entity")
		}
	})
}

// TestPortalSpawnRules: nothing in the Nether, without a player within
// 128 blocks, or on blocks that are no valid spawn (glass, bedrock).
func TestPortalSpawnRules(t *testing.T) {
	if s := runPortalTicks(t, world.Nether, world.DifficultyHard, block.Obsidian{}, 20000, true); s.n != 0 {
		t.Errorf("nether: %d spawns", s.n)
	}
	if s := runPortalTicks(t, world.Overworld, world.DifficultyHard, block.Obsidian{}, 20000, false); s.n != 0 {
		t.Errorf("no player near: %d spawns", s.n)
	}
	if s := runPortalTicks(t, world.Overworld, world.DifficultyHard, block.Glass{}, 20000, true); s.n != 0 {
		t.Errorf("glass floor: %d spawns", s.n)
	}
	if s := runPortalTicks(t, world.Overworld, world.DifficultyHard, block.Bedrock{}, 20000, true); s.n != 0 {
		t.Errorf("bedrock floor: %d spawns", s.n)
	}
	if s := runPortalTicks(t, world.Overworld, world.DifficultyHard, block.Magma{}, 20000, true); s.n == 0 {
		t.Errorf("magma floor: no spawn (zombified piglins are fire immune)")
	}
}

// TestPortalPlayerRange: a player within 128 blocks of the chunk's middle,
// horizontally (ChunkMap.playerIsCloseEnoughForSpawning).
func TestPortalPlayerRange(t *testing.T) {
	w := newTestWorld(t, world.Overworld, world.DifficultyHard)
	w.fill(cube.Pos{100, 3, 7}, cube.Pos{130, 3, 9}, block.Stone{})
	w.addPlayer(mgl64.Vec3{8 + 127.5, 4, 8})
	w.do(func(tx *world.Tx) {
		if !anyPlayerCloseEnoughForSpawning(tx, cube.Pos{0, 5, 0}) {
			t.Error("player at 127.5 blocks: not near")
		}
		if !anyPlayerCloseEnoughForSpawning(tx, cube.Pos{15, 200, 15}) {
			t.Error("height must not count")
		}
		if anyPlayerCloseEnoughForSpawning(tx, cube.Pos{-16, 5, 0}) {
			t.Error("player at 143.5 blocks: near")
		}
	})
}
