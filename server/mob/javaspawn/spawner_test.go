//go:build crossfly_full

package javaspawn

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func onlyMonsters() map[Category]int {
	return map[Category]int{CategoryCreature: 0, CategoryAmbient: 0, CategoryAxolotls: 0,
		CategoryUndergroundWaterCreature: 0, CategoryWaterCreature: 0, CategoryWaterAmbient: 0}
}

func TestNothingWithoutRegistry(t *testing.T) {
	w, _ := newWorld(t, worldOpts{radius: 2})
	SetRegistry(nil)
	defer SetRegistry(testRegistry{})
	Enable(w, Config{ManualTick: true})
	tickN(w, 5)
	run(w, func(tx *world.Tx) {
		if s := WorldStats(w); s.Positions != 0 {
			t.Errorf("spawning ran without a registry: %+v", s)
		}
	})
	n := world.TickHooks()
	RegisterHook()
	RegisterHook()
	if world.TickHooks() != n+1 {
		t.Errorf("RegisterHook registered %d hooks", world.TickHooks()-n)
	}
}

func TestCreatureCycle(t *testing.T) {
	w, _ := newWorld(t, worldOpts{radius: 1})
	run(w, func(tx *world.Tx) {
		e := testEnv(tx)
		st := stateOf(w)
		has := func(cats []Category, c Category) bool {
			for _, v := range cats {
				if v == c {
					return true
				}
			}
			return false
		}
		for _, c := range []struct {
			tick     int64
			diff     int
			creature bool
			monster  bool
		}{{400, 2, true, true}, {401, 2, false, true}, {800, 2, true, true}, {799, 2, false, true}, {401, 0, false, false}, {1200, 0, true, false}} {
			e.gameTime, e.difficulty = c.tick, c.diff
			cats := st.tickCategories(e, nil)
			if has(cats, CategoryCreature) != c.creature || has(cats, CategoryMonster) != c.monster || !has(cats, CategoryWaterAmbient) {
				t.Errorf("tick %d difficulty %d: %v", c.tick, c.diff, cats)
			}
		}
	})
}

// TestPerPlayerCap fills a dark area with zombies: the player's monster cap
// (70) is reached and never passed, persistent mobs and mobs that did not
// spawn naturally do not count.
func TestPerPlayerCap(t *testing.T) {
	w, _ := newWorld(t, worldOpts{roof: block.Stone{}})
	SetRegistry(testRegistry{only: map[string]bool{"minecraft:zombie": true}})
	Enable(w, Config{ManualTick: true, RandSeed: 11, SpawnLimits: onlyMonsters()})
	count := func() (n int) {
		for _, m := range mobs(w)["minecraft:zombie"] {
			if m.st().natural && !m.st().persistent {
				n++
			}
		}
		return
	}
	for i := 0; i < 400 && count() < 70; i++ {
		tickN(w, 1)
	}
	if n := count(); n != 70 {
		t.Fatalf("%d zombies, want the cap of 70", n)
	}
	tickN(w, 50)
	if n := count(); n != 70 {
		t.Fatalf("%d zombies after more ticks, want 70", n)
	}
	for _, m := range mobs(w)["minecraft:zombie"] {
		if !m.st().natural {
			t.Fatalf("a spawned zombie is not natural")
		}
	}
	// Mobs from elsewhere (spawn eggs, spawners) do not take the cap.
	run(w, func(tx *world.Tx) {
		for i := range 50 {
			addMob(tx, "minecraft:zombie", mgl64.Vec3{float64(40 + i), floorY + 1, 40}, testMobState{})
		}
	})
	tickN(w, 30)
	if n := count(); n != 70 {
		t.Fatalf("%d natural zombies with 50 egg zombies around, want 70", n)
	}
	// Persistent ones (named) do not count either: spawning goes on.
	run(w, func(tx *world.Tx) {
		for e := range tx.Entities() {
			if m, ok := e.(*testMob); ok {
				m.st().persistent = true
			}
		}
	})
	for i := 0; i < 400 && count() < 70; i++ {
		tickN(w, 1)
	}
	if n := count(); n != 70 {
		t.Fatalf("%d new zombies after making the others persistent, want 70", n)
	}
}

// TestPlayersHaveOwnCaps: two players 400 blocks apart fill a cap each.
func TestPlayersHaveOwnCaps(t *testing.T) {
	w, _ := newWorld(t, worldOpts{roof: block.Stone{}, radius: 9})
	run(w, func(tx *world.Tx) {
		for cx := 16; cx <= 34; cx++ {
			for cz := -9; cz <= 9; cz++ {
				tx.Block(cube.Pos{cx * 16, 0, cz * 16})
			}
		}
		pos := mgl64.Vec3{400.5, floorY + 1, 8.5}
		h := world.EntitySpawnOpts{Position: pos}.New(player.Type, player.Config{Name: "tester2", Position: pos, GameMode: world.GameModeSurvival})
		tx.AddEntity(h)
	})
	SetRegistry(testRegistry{only: map[string]bool{"minecraft:zombie": true}})
	Enable(w, Config{ManualTick: true, RandSeed: 5, SpawnLimits: onlyMonsters()})
	for range 600 {
		tickN(w, 1)
		if len(mobs(w)["minecraft:zombie"]) >= 140 {
			break
		}
	}
	near := 0
	all := mobs(w)["minecraft:zombie"]
	for _, m := range all {
		if m.Position()[0] < 200 {
			near++
		}
	}
	if len(all) != 140 || near != 70 {
		t.Fatalf("%d zombies, %d near the first player; want 140 and 70", len(all), near)
	}
}

// TestSpawnRateDarkArea compares the spawn rate in a dark witch hut area with
// the Java algorithm's expectation, computed here independently of the
// spawner: every spawning chunk (centre within mob-spawn-range*16 of the
// player) tries one position a tick, whose y is uniform over
// [min y, WORLD_SURFACE]: only 1 of those 8 heights (the dark floor under
// the roof) is valid. From there NaturalSpawner tries 3 packs; a witch
// pack (count 1) spawns one witch at the first step unless an earlier
// witch of the same call stands on the same block.
func TestSpawnRateDarkArea(t *testing.T) {
	player := mgl64.Vec3{8.5, floorY + 31, 8.5} // above the roof: every floor block is 30+ blocks away
	w, _ := newWorld(t, worldOpts{roof: block.Stone{}, playerPos: player})
	SetRegistry(testRegistry{only: map[string]bool{"minecraft:witch": true}})
	hut := func(*world.Tx, cube.Pos) []StructureAt { return []StructureAt{{Name: "swamp_hut", InPiece: true}} }
	Enable(w, Config{ManualTick: true, RandSeed: 99, MobSpawnRange: 4, SpawnLimits: onlyMonsters(), Structures: hut})

	chunks := 0
	for cx := -5; cx <= 5; cx++ {
		for cz := -5; cz <= 5; cz++ {
			dx, dz := float64(cx*16+8)-player[0], float64(cz*16+8)-player[2]
			if dx*dx+dz*dz < 64*64 {
				chunks++
			}
		}
	}
	// q: two packs' first steps (each Δ = nextInt(6)-nextInt(6) per axis)
	// land on the same block.
	sum := 0.0
	for d := -5; d <= 5; d++ {
		p := float64(6-abs(d)) / 36
		sum += p * p
	}
	q := sum * sum
	perPosition := 1 + (1 - q) + (1-q)*(1-2*q) + q*(1-q)
	const ticks = 400
	want := float64(ticks*chunks) / 8 * perPosition

	got := 0
	for range ticks {
		tickN(w, 1)
		got += len(mobs(w)["minecraft:witch"])
		clearMobs(w)
	}
	t.Logf("witches: %d in %d ticks, Java expectation %.0f (%d chunks)", got, ticks, want, chunks)
	if rel := math.Abs(float64(got)-want) / want; rel > .04 {
		t.Errorf("witches: %d in %d ticks, Java expectation %.0f (%d chunks, %.4f per valid position): off by %.1f%%",
			got, ticks, want, chunks, perPosition, rel*100)
	}
	// At noon the roof top is lit and nothing spawns there: every witch is
	// on the dark floor.
	tickN(w, 1)
	for _, m := range mobs(w)["minecraft:witch"] {
		if y := m.Position()[1]; y != floorY+1 {
			t.Fatalf("witch at y %v", y)
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// TestOpenSkyAnimals: cows spawn on lit grass in packs, only on the 400th
// ticks, 24..128 blocks from the player.
func TestOpenSkyAnimals(t *testing.T) {
	w, h := newWorld(t, worldOpts{})
	SetRegistry(testRegistry{only: map[string]bool{"minecraft:cow": true, "minecraft:sheep": true, "minecraft:pig": true, "minecraft:chicken": true}})
	Enable(w, Config{ManualTick: true, RandSeed: 3})
	st := stateOf(w)
	run(w, func(tx *world.Tx) {
		for i := range 40 {
			st.ran = false
			Tick(tx)
			_ = i
		}
	})
	n := 0
	var ppos mgl64.Vec3
	run(w, func(tx *world.Tx) {
		e, _ := h.Entity(tx)
		ppos = e.Position()
	})
	for typ, ms := range mobs(w) {
		for _, m := range ms {
			n++
			d := m.Position().Sub(ppos).Len()
			if d <= 24 || d > 128 {
				t.Errorf("%s at distance %.1f", typ, d)
			}
			if m.Position()[1] != floorY+1 {
				t.Errorf("%s at y %v", typ, m.Position()[1])
			}
		}
	}
	tick := int64(0)
	run(w, func(tx *world.Tx) { tick = tx.CurrentTick() })
	if tick%400 == 0 && n == 0 {
		t.Error("no animals on a creature tick")
	}
	if tick%400 != 0 && n != 0 {
		t.Errorf("%d animals off the creature tick %d", n, tick)
	}
}

func TestDespawn(t *testing.T) {
	w, _ := newWorld(t, worldOpts{})
	SetRegistry(testRegistry{})
	at := func(dx float64) mgl64.Vec3 { return mgl64.Vec3{8.5 + dx, floorY + 1, 8.5} }
	var far, farPersistent, farCow, salmon70, salmon60, near *testMob
	var mid []*testMob
	run(w, func(tx *world.Tx) {
		far = addMob(tx, "minecraft:zombie", at(130), testMobState{natural: true})
		farPersistent = addMob(tx, "minecraft:zombie", at(131), testMobState{persistent: true})
		farCow = addMob(tx, "minecraft:cow", at(132), testMobState{natural: true})
		salmon70 = addMob(tx, "minecraft:salmon", at(70), testMobState{natural: true})
		salmon60 = addMob(tx, "minecraft:salmon", at(-60), testMobState{natural: true})
		near = addMob(tx, "minecraft:zombie", at(20), testMobState{noAction: 700})
		for i := range 300 {
			mid = append(mid, addMob(tx, "minecraft:zombie", mgl64.Vec3{8.5 + 50, floorY + 1, float64(i%60) - 30}, testMobState{noAction: 700}))
		}
	})
	alive := func(m *testMob) bool {
		ok := false
		run(w, func(tx *world.Tx) { _, ok = m.H().Entity(tx) })
		return ok
	}
	tickN(w, 1)
	if alive(far) || !alive(farPersistent) || !alive(farCow) || alive(salmon70) || !alive(salmon60) {
		t.Errorf("hard range: zombie 130 %v (want gone), persistent %v, cow %v, salmon 70 %v (want gone), salmon 60 %v",
			alive(far), alive(farPersistent), alive(farCow), alive(salmon70), alive(salmon60))
	}
	run(w, func(tx *world.Tx) {
		if near.st().noAction != 0 {
			t.Errorf("no-action timer within 32 blocks: %d, want reset", near.st().noAction)
		}
	})
	// Beyond 32 blocks, 600+ ticks idle: 1 in 800 a tick.
	tickN(w, 799)
	gone := 0
	for _, m := range mid {
		if !alive(m) {
			gone++
		}
	}
	want := 1 - math.Pow(799./800, 800)
	if p := float64(gone) / float64(len(mid)); math.Abs(p-want) > .09 {
		t.Errorf("random despawn after 800 ticks: %.3f gone, want %.3f", p, want)
	}
	if !alive(near) {
		t.Error("zombie 20 blocks away despawned")
	}
	// Peaceful removes monsters, not animals.
	w.SetDifficulty(world.DifficultyPeaceful)
	tickN(w, 1)
	if alive(near) || alive(farPersistent) || !alive(farCow) {
		t.Error("peaceful: monsters must go, cows stay")
	}
	// Despawning off.
	run(w, func(tx *world.Tx) { far = addMob(tx, "minecraft:zombie", at(140), testMobState{}) })
	w.SetDifficulty(world.DifficultyNormal)
	SetDespawning(w, false)
	tickN(w, 2)
	if !alive(far) {
		t.Error("despawned with despawning off")
	}
}

func TestPhantoms(t *testing.T) {
	sea := floorY
	w, h := newWorld(t, worldOpts{radius: 3})
	w.SetTime(18000)
	SetRegistry(testRegistry{only: map[string]bool{"minecraft:phantom": true}})
	Enable(w, Config{ManualTick: true, RandSeed: 4, SeaLevel: &sea, SpawnLimits: map[Category]int{CategoryMonster: 0,
		CategoryCreature: 0, CategoryAmbient: 0, CategoryAxolotls: 0, CategoryUndergroundWaterCreature: 0,
		CategoryWaterCreature: 0, CategoryWaterAmbient: 0}})
	attempt := func(n int) int {
		total := 0
		for range n {
			run(w, func(tx *world.Tx) {
				e, _ := h.Entity(tx)
				e.(*player.Player).SetTimeSinceRest(1 << 30)
				st := stateOf(w)
				st.phantomNext, st.ran = 0, false
				Tick(tx)
			})
			total += len(mobs(w)["minecraft:phantom"])
			clearMobs(w)
		}
		return total
	}
	// Normal at day 0: local difficulty 1.5 beats rand(3) half the time;
	// 1..3 phantoms (mean 2).
	if got := attempt(400); got < 330 || got > 470 {
		t.Errorf("%d phantoms in 400 attempts, want about 400", got)
	}
	run(w, func(tx *world.Tx) {
		e, _ := h.Entity(tx)
		e.(*player.Player).SetGameMode(world.GameModeCreative)
	})
	if got := attempt(50); got != 0 {
		t.Errorf("%d phantoms over a creative player", got)
	}
	run(w, func(tx *world.Tx) {
		e, _ := h.Entity(tx)
		e.(*player.Player).SetGameMode(world.GameModeSurvival)
	})
	w.SetTime(6000)
	if got := attempt(50); got != 0 {
		t.Errorf("%d phantoms at noon", got)
	}
	if d := localDifficulty(2, 0); d != 1.5 {
		t.Errorf("local difficulty normal day 0: %v", d)
	}
	if d := localDifficulty(3, 24000*96); math.Abs(float64(d)-3*(0.75+0.25+0.25*1)) > 1e-5 {
		t.Errorf("local difficulty hard full moon day 96: %v", d)
	}
}
