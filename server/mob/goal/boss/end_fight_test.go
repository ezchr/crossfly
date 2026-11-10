//go:build crossfly_full

package boss_test

import (
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	bc "github.com/df-mc/dragonfly/server/mob/component/boss"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator"
	"github.com/go-gl/mathgl/mgl64"
)

// endWorld is an End of flat end stone (y 0-2, as Dragonfly's default End)
// with a creative player near its centre, so the fight runs and the dragon
// is simulated.
func endWorld(t *testing.T) (*mobtest.World, *mobtest.Player) {
	t.Helper()
	mobtest.Require(t, "minecraft:boss")
	mobtest.RequireDefinitions(t)
	bc.EnableEndFight()
	// The test world has no portals: let it have the fight.
	bc.EndFightWorld = func(w *world.World) bool { return w.Dimension() == world.End }
	w := mobtest.New(t, mobtest.Options{
		Dim:        world.End,
		Persistent: true,
		Generator:  &lazyFlat{},
	})
	p := w.AddPlayer("hunter", mgl64.Vec3{12.5, 3, 12.5})
	p.SetGameMode(world.GameModeCreative)
	// The chunks around the centre are loaded, as the player's view would.
	w.Do(func(tx *world.Tx) {
		for x := -1; x <= 1; x++ {
			for z := -1; z <= 1; z++ {
				tx.Block(cube.Pos{x * 16, 0, z * 16})
			}
		}
	})
	return w, p
}

// lazyFlat is Dragonfly's flat End generator, made once the block registry
// is finalised (by the first world).
type lazyFlat struct {
	once sync.Once
	g    world.Generator
}

func (l *lazyFlat) gen() world.Generator {
	l.once.Do(func() {
		l.g = generator.NewFlat(biome.End{}, []world.Block{block.EndStone{}, block.EndStone{}, block.Bedrock{}})
	})
	return l.g
}

func (l *lazyFlat) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) { l.gen().GenerateChunk(pos, c) }
func (l *lazyFlat) DefaultSpawn(dim world.Dimension) cube.Pos        { return l.gen().DefaultSpawn(dim) }

func fight(w *mobtest.World) *bc.EndFight { return bc.FightOf(w.W) }

func countCrystals(tx *world.Tx, box cube.BBox) int {
	n := 0
	for e := range tx.EntitiesWithin(box) {
		if e.H().Type() == entity.EndCrystalType {
			n++
		}
	}
	return n
}

// TestEndFightStarts: the first player near the centre brings the spikes
// with their crystals (Dragonfly's End has none), the closed exit portal and
// the dragon at 0 128 0.
func TestEndFightStarts(t *testing.T) {
	w, _ := endWorld(t)
	w.Tick(2)
	f := fight(w)
	exit, ok := f.ExitPortal()
	if !ok {
		t.Fatal("no exit portal")
	}
	if exit != (cube.Pos{0, 3, 0}) {
		t.Errorf("exit portal at %v, want 0 3 0 on the end stone", exit)
	}
	w.Do(func(tx *world.Tx) {
		spikes := fight(w).Spikes()
		for _, s := range spikes {
			if _, ok := tx.Block(cube.Pos{s.X, s.Height - 1, s.Z}).(block.Obsidian); !ok {
				t.Errorf("spike %v has no obsidian top", s)
			}
			if b, ok := tx.Block(cube.Pos{s.X, s.Height, s.Z}).(block.Bedrock); !ok || !b.InfiniteBurning {
				t.Errorf("spike %v has no burning bedrock", s)
			}
			if _, ok := tx.Block(cube.Pos{s.X + 2, s.Height + 3, s.Z}).(block.IronBars); ok != s.Guarded {
				t.Errorf("spike %v: cage %v, want %v", s, ok, s.Guarded)
			}
		}
		if n := countCrystals(tx, cube.Box(-60, 0, -60, 60, 130, 60)); n != len(spikes) {
			t.Errorf("%d crystals, want one per spike", n)
		}
		if _, ok := tx.Block(exit.Add(cube.Pos{0, 3, 0})).(block.Bedrock); !ok {
			t.Error("no bedrock pillar on the exit portal")
		}
		if _, ok := tx.Block(exit.Add(cube.Pos{1, 0, 0})).(block.EndPortal); ok {
			t.Error("the exit portal is open with the dragon alive")
		}
		if _, ok := tx.Block(exit.Add(cube.Pos{3, 0, 0})).(block.Bedrock); !ok {
			t.Error("no bedrock rim around the exit portal")
		}
	})
	if f.Dragon() == nil {
		t.Fatal("no dragon")
	}
	w.Do(func(tx *world.Tx) {
		e, ok := f.Dragon().Entity(tx)
		if !ok || e.Position().Sub(mgl64.Vec3{0, 128, 0}).Len() > 8 {
			t.Errorf("dragon not near 0 128 0: %v", e)
		}
	})
	// Only one dragon, however long the fight runs.
	w.Tick(40)
	if n := len(w.Mobs(bc.DragonID)); n != 1 {
		t.Errorf("%d dragons", n)
	}
}

// killDragon runs the fight's dragon through its death animation.
func killDragon(t *testing.T, w *mobtest.World) {
	t.Helper()
	f := fight(w)
	w.Do(func(tx *world.Tx) {
		e, ok := f.Dragon().Entity(tx)
		if !ok {
			t.Fatal("no dragon")
		}
		m, _ := mob.Of(e)
		bc.DragonOf(m).StartDeathAnimation(m)
	})
	if _, ok := w.TickUntil(400, func(*world.Tx) bool { return f.DragonKilled() }); !ok {
		t.Fatal("the dragon never died")
	}
}

// gatewayOnRing finds the gateway on the ring of 96 at y 75.
func gatewayOnRing(tx *world.Tx) (cube.Pos, block.EndGateway, bool) {
	for x := -97; x <= 97; x++ {
		for z := -97; z <= 97; z++ {
			if x*x+z*z < 90*90 {
				continue
			}
			if g, ok := tx.Block(cube.Pos{x, 75, z}).(block.EndGateway); ok {
				return cube.Pos{x, 75, z}, g, true
			}
		}
	}
	return cube.Pos{}, block.EndGateway{}, false
}

// TestEndFightArena: an End world no portal leads to (a server's own arena)
// gets no fight.
func TestEndFightArena(t *testing.T) {
	mobtest.Require(t, "minecraft:boss")
	mobtest.RequireDefinitions(t)
	bc.EnableEndFight()
	defer func(f func(*world.World) bool) { bc.EndFightWorld = f }(bc.EndFightWorld)
	bc.EndFightWorld = func(w *world.World) bool {
		return w.Dimension() == world.End && w.PortalDestination(world.End) != w
	}
	w := mobtest.New(t, mobtest.Options{Dim: world.End, Generator: &lazyFlat{}})
	w.AddPlayer("hunter", mgl64.Vec3{12.5, 3, 12.5})
	w.Do(func(tx *world.Tx) { tx.Block(cube.Pos{}) })
	w.Tick(5)
	if n := len(w.Mobs(bc.DragonID)); n != 0 {
		t.Errorf("%d dragons in an arena", n)
	}
	w.Do(func(tx *world.Tx) {
		if n := countCrystals(tx, cube.Box(-60, 0, -60, 60, 130, 60)); n != 0 {
			t.Errorf("%d spike crystals in an arena", n)
		}
	})
}

// TestEndFightKill: the dragon's death opens the exit portal, lays the egg on
// it and opens a gateway linked to one out on the islands; all of it is
// still there after a restart. The gateway sends a player to its exit.
func TestEndFightKill(t *testing.T) {
	w, p := endWorld(t)
	w.Tick(2)
	killDragon(t, w)
	f := fight(w)
	exit, _ := f.ExitPortal()
	var inner cube.Pos
	w.Do(func(tx *world.Tx) {
		if _, ok := tx.Block(exit.Add(cube.Pos{1, 0, 1})).(block.EndPortal); !ok {
			t.Error("the exit portal did not open")
		}
		if _, ok := tx.Block(exit.Add(cube.Pos{0, 4, 0})).(block.DragonEgg); !ok {
			t.Error("no dragon egg on the exit portal")
		}
		pos, g, ok := gatewayOnRing(tx)
		if !ok {
			t.Fatal("no gateway opened")
		}
		inner = pos
		if !g.HasExit {
			t.Fatal("the gateway has no exit")
		}
		if d := g.Exit.Vec3().Len(); d < 600 {
			t.Errorf("the gateway leads %v from the centre, want the outer islands", d)
		}
		// The outer gateway leads back near the inner one.
		var outer *block.EndGateway
		for x := g.Exit[0] - 20; x <= g.Exit[0]+20 && outer == nil; x++ {
			for z := g.Exit[2] - 20; z <= g.Exit[2]+20 && outer == nil; z++ {
				for y := 0; y < 40; y++ {
					if og, ok := tx.Block(cube.Pos{x, y, z}).(block.EndGateway); ok {
						outer = &og
						break
					}
				}
			}
		}
		if outer == nil || !outer.HasExit {
			t.Fatalf("no linked outer gateway near %v", g.Exit)
		}
		// The ground nearest the inner gateway, within 48 columns.
		if dx, dz := outer.Exit[0]-pos[0], outer.Exit[2]-pos[2]; dx < -48 || dx > 48 || dz < -48 || dz > 48 {
			t.Errorf("the outer gateway leads to %v, beyond 48 of the inner one at %v", outer.Exit, pos)
		}
		// Through the gateway.
		e, _ := p.Entity(tx)
		g.EntityInside(pos, tx, e)
		if got, want := e.Position(), (mgl64.Vec3{float64(g.Exit[0]), float64(g.Exit[1] + 1), float64(g.Exit[2])}); got.Sub(want).Len() > 1e-6 {
			t.Errorf("the gateway sent the player to %v, want %v", got, want)
		}
		e.(*player.Player).Teleport(mgl64.Vec3{12.5, 3, 12.5})
		g.EntityInside(pos, tx, e)
		if e.Position()[0] != 12.5 {
			t.Error("the gateway sent the player again within its cooldown")
		}
	})
	if n := fight(w).Gateways(); n != 19 {
		t.Errorf("%d gateways left, want 19", n)
	}

	w.Reload()
	f = fight(w)
	if !f.DragonKilled() || f.Gateways() != 19 {
		t.Errorf("after a restart: killed %v, %d gateways left", f.DragonKilled(), f.Gateways())
	}
	if got, ok := f.ExitPortal(); !ok || got != exit {
		t.Errorf("after a restart the exit portal is at %v, want %v", got, exit)
	}
	if !bc.DragonPreviouslyKilled(w.W) {
		t.Error("the first kill was forgotten")
	}
	w.Do(func(tx *world.Tx) {
		if g, ok := tx.Block(inner).(block.EndGateway); !ok || !g.HasExit {
			t.Error("the gateway lost its exit in the restart")
		}
	})
	w.Tick(5)
	if n := len(w.Mobs(bc.DragonID)); n != 0 {
		t.Errorf("%d dragons came back after the restart", n)
	}
}

// placeRespawnCrystals puts an end crystal on each side of the exit portal.
func placeRespawnCrystals(w *mobtest.World, exit cube.Pos) []*world.EntityHandle {
	var hs []*world.EntityHandle
	w.Do(func(tx *world.Tx) {
		above := exit.Add(cube.Pos{0, 1, 0})
		for _, d := range []cube.Pos{{3, 0, 0}, {-3, 0, 0}, {0, 0, 3}, {0, 0, -3}} {
			h := entity.NewEndCrystal(world.EntitySpawnOpts{Position: above.Add(d).Vec3Middle()})
			tx.AddEntity(h)
			hs = append(hs, h)
		}
	})
	return hs
}

// TestEndFightRespawn: four crystals on the exit portal close it, rebuild the
// spikes one by one with invulnerable crystals and bring a new dragon, which
// drops the smaller experience; the four crystals blow up.
func TestEndFightRespawn(t *testing.T) {
	w, _ := endWorld(t)
	w.Tick(2)
	killDragon(t, w)
	f := fight(w)
	exit, _ := f.ExitPortal()
	crystals := placeRespawnCrystals(w, exit)
	w.Tick(21)
	if !f.Respawning() {
		t.Fatal("four crystals on the exit portal did not start a respawn")
	}
	w.Do(func(tx *world.Tx) {
		if _, ok := tx.Block(exit.Add(cube.Pos{1, 0, 1})).(block.EndPortal); ok {
			t.Error("the exit portal stayed open during the respawn")
		}
	})
	sawInvulnerable := false
	if _, ok := w.TickUntil(900, func(tx *world.Tx) bool {
		for e := range tx.EntitiesWithin(cube.Box(-60, 60, -60, 60, 130, 60)) {
			if c, ok := e.(*entity.Ent); ok {
				if v, ok := c.Behaviour().(interface{ Invulnerable() bool }); ok && v.Invulnerable() {
					sawInvulnerable = true
				}
			}
		}
		return !f.Respawning()
	}); !ok {
		t.Fatal("the respawn never ended")
	}
	if !sawInvulnerable {
		t.Error("the rebuilt spikes had no invulnerable crystals")
	}
	if f.DragonKilled() || f.Dragon() == nil {
		t.Fatal("no new dragon")
	}
	w.Do(func(tx *world.Tx) {
		for _, h := range crystals {
			if _, ok := h.Entity(tx); ok {
				t.Error("a respawn crystal is still there")
			}
		}
		for e := range tx.EntitiesWithin(cube.Box(-60, 60, -60, 60, 130, 60)) {
			if c, ok := e.(*entity.Ent); ok {
				if v, ok := c.Behaviour().(interface{ Invulnerable() bool }); ok && v.Invulnerable() {
					t.Error("a spike crystal stayed invulnerable after the respawn")
				}
			}
		}
	})
}

// TestEndFightRespawnStopped: destroying one of the four crystals stops the
// respawn and opens the exit portal again.
func TestEndFightRespawnStopped(t *testing.T) {
	w, _ := endWorld(t)
	w.Tick(2)
	killDragon(t, w)
	f := fight(w)
	exit, _ := f.ExitPortal()
	crystals := placeRespawnCrystals(w, exit)
	w.Tick(60)
	if !f.Respawning() {
		t.Fatal("no respawn")
	}
	w.Do(func(tx *world.Tx) {
		e, _ := crystals[0].Entity(tx)
		_ = e.(*entity.Ent).Close()
	})
	w.Tick(2)
	if f.Respawning() {
		t.Error("the respawn went on without its crystal")
	}
	w.Do(func(tx *world.Tx) {
		if _, ok := tx.Block(exit.Add(cube.Pos{1, 0, 1})).(block.EndPortal); !ok {
			t.Error("the exit portal did not open again")
		}
	})
}
