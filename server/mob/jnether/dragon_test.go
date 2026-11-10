//go:build crossfly_full

package jnether

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// fakeDragon is a DragonHost without a mob: enough for the dragon's own
// logic (the mob base runs it for real).
type fakeDragon struct {
	tx       *world.Tx
	h        *world.EntityHandle
	pos, vel mgl64.Vec3
	yaw      float64
	health   float64
	ticks    int
	r        *rand.Rand
}

func newFakeDragon(pos mgl64.Vec3) *fakeDragon {
	return &fakeDragon{
		h:   world.EntitySpawnOpts{Position: pos}.New(SmallFireballType, projectileConfig{newProjectile(SmallFireballType, nil)}),
		pos: pos, health: 200, r: rand.New(rand.NewPCG(5, 6)),
	}
}

func (f *fakeDragon) H() *world.EntityHandle   { return f.h }
func (f *fakeDragon) Position() mgl64.Vec3     { return f.pos }
func (f *fakeDragon) Rotation() cube.Rotation  { return cube.Rotation{f.yaw, 0} }
func (f *fakeDragon) Close() error             { return nil }
func (f *fakeDragon) Tx() *world.Tx            { return f.tx }
func (f *fakeDragon) Velocity() mgl64.Vec3     { return f.vel }
func (f *fakeDragon) SetVelocity(v mgl64.Vec3) { f.vel = v }
func (f *fakeDragon) SetPosition(p mgl64.Vec3) { f.pos = p }
func (f *fakeDragon) Yaw() float64             { return f.yaw }
func (f *fakeDragon) SetYaw(v float64)         { f.yaw = v }
func (f *fakeDragon) SetBodyYaw(float64)       {}
func (f *fakeDragon) Pitch() float64           { return 0 }
func (f *fakeDragon) Health() float64          { return f.health }
func (f *fakeDragon) MaxHealth() float64       { return 200 }
func (f *fakeDragon) SetHealth(v float64)      { f.health = v }
func (f *fakeDragon) Rand() *rand.Rand         { return f.r }
func (f *fakeDragon) TickCount() int           { return f.ticks }
func (f *fakeDragon) HurtTime() int            { return 0 }

// fakeFight is a DragonFight with a fixed crystal count.
type fakeFight struct {
	crystals int
	killed   bool
}

func (f *fakeFight) AliveCrystals() int                                     { return f.crystals }
func (f *fakeFight) PreviouslyKilled() bool                                 { return false }
func (f *fakeFight) DragonKilled(*world.Tx, world.Entity)                   { f.killed = true }
func (f *fakeFight) UpdateDragon(*world.Tx, world.Entity, float64, float64) {}

// endWorld is a world with a bedrock podium column at 0 0 (top y 63) and
// an end stone floor at y 60.
func endWorld(t *testing.T) *testWorld {
	w := newTestWorld(t, world.Overworld, world.DifficultyNormal)
	w.fill(cube.Pos{-3, 60, -3}, cube.Pos{3, 60, 3}, block.EndStone{})
	w.fill(cube.Pos{0, 61, 0}, cube.Pos{0, 63, 0}, block.Bedrock{})
	return w
}

// TestDragonNodes: the 24 nodes on rings 60/40/20, at least y 73.
func TestDragonNodes(t *testing.T) {
	w := endWorld(t)
	d := NewDragon()
	w.do(func(tx *world.Tx) { d.buildNodes(tx) })
	for i, n := range d.nodes {
		r := 60.0
		if i >= 20 {
			r = 20
		} else if i >= 12 {
			r = 40
		}
		if got := math.Hypot(float64(n.x), float64(n.z)); math.Abs(got-r) > 1.5 {
			t.Errorf("node %d at %v %v: radius %v, want %v", i, n.x, n.z, got, r)
		}
		if n.y < 73 {
			t.Errorf("node %d at y %d", i, n.y)
		}
	}
	// A path around the outer ring with crystals, only the inner rings
	// without.
	d.Fight = &fakeFight{crystals: 4}
	p := d.findPath(0, 6, nil)
	if p == nil || p.nodes[len(p.nodes)-1] != d.nodes[6] {
		t.Fatalf("no path 0 -> 6")
	}
	d.Fight = &fakeFight{crystals: 0}
	if got := d.closestNodeTo(60, 80, 0); got < 12 {
		t.Errorf("closest node without crystals: %d (outer ring)", got)
	}
}

// TestDragonDamageRules: d/4 + min(d, 1) off the body, all of it off the
// head, nothing from non-players, a lethal hit leaves 1 health and starts
// the death flight; 25% while perched makes it take off.
func TestDragonDamageRules(t *testing.T) {
	w := endWorld(t)
	p := w.addPlayer(mgl64.Vec3{0.5, 64, 5})
	w.do(func(tx *world.Tx) {
		h := newFakeDragon(mgl64.Vec3{0, 100, 0})
		h.tx = tx
		d := NewDragon()
		d.SetPhase(h, PhaseHoldingPattern)
		pl := w.player(tx, p)
		src := entity.AttackDamageSource{Attacker: pl}
		d.SetHitPart(PartBody)
		if dmg, ok := d.FilterDamage(h, src, 10); !ok || math.Abs(dmg-3.5) > 1e-9 {
			t.Errorf("body hit: %v %v, want 3.5", dmg, ok)
		}
		d.SetHitPart(PartHead)
		if dmg, ok := d.FilterDamage(h, src, 10); !ok || dmg != 10 {
			t.Errorf("head hit: %v %v, want 10", dmg, ok)
		}
		if _, ok := d.FilterDamage(h, entity.AttackDamageSource{Attacker: h}, 10); ok {
			t.Error("a non-player hurt the dragon")
		}
		boom := entity.ExplosionDamageSource{Source: world.BlockExplosionSource{ExplosionSize: 4}}
		d.SetHitPart(PartHead)
		if _, ok := d.FilterDamage(h, boom, 5); !ok {
			t.Error("explosions must always hurt")
		}
		h.health = 5
		d.SetHitPart(PartHead)
		if dmg, ok := d.FilterDamage(h, src, 50); !ok || dmg != 4 || d.Phase() != PhaseDying {
			t.Errorf("lethal hit: %v %v phase %d, want 4 and dying", dmg, ok, d.Phase())
		}
		if _, ok := d.FilterDamage(h, src, 1); ok {
			t.Error("a dying dragon took damage")
		}

		h.health = 200
		d2 := NewDragon()
		d2.SetPhase(h, PhaseSittingScanning)
		for i := 0; i < 5; i++ {
			d2.SetHitPart(PartHead)
			d2.FilterDamage(h, src, 11)
		}
		if d2.Phase() != PhaseTakeoff {
			t.Errorf("after 55 damage perched: phase %d, want takeoff", d2.Phase())
		}
	})
}

// TestDragonFlightCycle: without crystals or players the dragon circles,
// lands on the podium (1 in 3 at each path end), scans for 100 ticks and
// takes off again.
func TestDragonFlightCycle(t *testing.T) {
	w := endWorld(t)
	h := newFakeDragon(mgl64.Vec3{0, 128, 0})
	d := NewDragon()
	d.Fight = &fakeFight{}
	seen := map[int]bool{}
	w.do(func(tx *world.Tx) {
		h.tx = tx
		d.SetPhase(h, PhaseHoldingPattern)
		for i := 0; i < 20*60*5 && !seen[PhaseTakeoff]; i++ {
			h.ticks++
			d.AiStep(h)
			seen[d.Phase()] = true
		}
	})
	// LANDING_APPROACH may last no tick at all: a short path is done in the
	// same tick and LANDING follows at once.
	for _, ph := range []int{PhaseLanding, PhaseSittingScanning, PhaseTakeoff} {
		if !seen[ph] {
			t.Errorf("phase %d never reached (seen %v)", ph, seen)
		}
	}
}

// TestDragonBreaksBlocks: blocks in the head/neck/body boxes go (mobGriefing),
// end stone and obsidian stay (dragon_immune).
func TestDragonBreaksBlocks(t *testing.T) {
	w := endWorld(t)
	w.fill(cube.Pos{-6, 98, -6}, cube.Pos{6, 104, 6}, block.Stone{})
	w.fill(cube.Pos{-6, 98, -6}, cube.Pos{6, 98, 6}, block.Obsidian{})
	h := newFakeDragon(mgl64.Vec3{0, 100, 0})
	d := NewDragon()
	w.do(func(tx *world.Tx) {
		h.tx = tx
		d.SetPhase(h, PhaseHoldingPattern)
		h.ticks++
		d.AiStep(h)
		h.ticks++
		d.AiStep(h)
		if _, ok := tx.Block(cube.Pos{0, 100, 0}).(block.Air); !ok {
			t.Errorf("stone in the body box: %#v", tx.Block(cube.Pos{0, 100, 0}))
		}
		if _, ok := tx.Block(cube.Pos{0, 98, 0}).(block.Obsidian); !ok {
			t.Error("obsidian was broken")
		}
	})
}
