//go:build crossfly_full

package javaspawn

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
)

func newTestRand(seed uint64) *rand.Rand { return rand.New(rand.NewPCG(seed, seed^0xdeadbeef)) }

func TestDefaults(t *testing.T) {
	// MobCategory (26.3) and the bukkit.yml spawn-limits / ticks-per.
	want := map[Category]struct {
		name       string
		limit, per int
		friendly   bool
		persistent bool
		despawn    int
	}{
		CategoryMonster:                  {"monster", 70, 1, false, false, 128},
		CategoryCreature:                 {"creature", 10, 400, true, true, 128},
		CategoryAmbient:                  {"ambient", 15, 1, true, false, 128},
		CategoryAxolotls:                 {"axolotls", 5, 1, true, false, 128},
		CategoryUndergroundWaterCreature: {"underground_water_creature", 5, 1, true, false, 128},
		CategoryWaterCreature:            {"water_creature", 5, 1, true, false, 128},
		CategoryWaterAmbient:             {"water_ambient", 20, 1, true, false, 64},
	}
	cfg := (&Config{}).withDefaults()
	for c, w := range want {
		if c.String() != w.name || cfg.spawnLimit(c) != w.limit || cfg.ticksPer(c) != w.per ||
			c.Friendly() != w.friendly || c.Persistent() != w.persistent || c.DespawnDistance() != w.despawn {
			t.Errorf("%v: got %s limit %d per %d friendly %v persistent %v despawn %d, want %+v", c, c.String(),
				cfg.spawnLimit(c), cfg.ticksPer(c), c.Friendly(), c.Persistent(), c.DespawnDistance(), w)
		}
		hard, soft := cfg.despawnRanges(c)
		if hard != float64(w.despawn) || soft != 32 {
			t.Errorf("%v despawn ranges %v/%v", c, hard, soft)
		}
	}
	if cfg.SimulationDistance != 10 || cfg.MobSpawnRange != 8 || cfg.SlimeSeed != 987234911 {
		t.Errorf("config defaults %+v", cfg)
	}
	if cfg.seaLevel(world.Overworld) != 63 || cfg.seaLevel(world.Nether) != 32 || cfg.seaLevel(world.End) != 0 {
		t.Error("sea levels")
	}
	custom := (&Config{SpawnLimits: map[Category]int{CategoryMonster: 0}, DespawnRanges: map[Category]DespawnRange{CategoryMonster: {Soft: 40}}}).withDefaults()
	if custom.spawnLimit(CategoryMonster) != 0 {
		t.Error("spawn limit override")
	}
	if h, s := custom.despawnRanges(CategoryMonster); h != 128 || s != 40 {
		t.Errorf("despawn override %v %v", h, s)
	}
}

func TestEntityTypeData(t *testing.T) {
	for id, w := range map[string]struct {
		cat      Category
		w, h     float64
		fire     bool
		peaceful bool
	}{
		"minecraft:zombie":          {CategoryMonster, .6, 1.95, false, false},
		"minecraft:cow":             {CategoryCreature, .9, 1.4, false, true},
		"minecraft:bat":             {CategoryAmbient, .5, .9, false, true},
		"minecraft:glow_squid":      {CategoryUndergroundWaterCreature, .8, .8, false, true},
		"minecraft:salmon":          {CategoryWaterAmbient, .7, .4, false, true},
		"minecraft:axolotl":         {CategoryAxolotls, .75, .42, false, true},
		"minecraft:strider":         {CategoryCreature, .9, 1.7, true, true},
		"minecraft:wither_skeleton": {CategoryMonster, .7, 2.4, true, false},
		"minecraft:villager":        {CategoryMisc, .6, 1.95, false, true},
	} {
		got, ok := Type(id)
		if !ok || got.Category != w.cat || got.Width != w.w || got.Height != w.h || got.FireImmune != w.fire || got.AllowedInPeaceful != w.peaceful {
			t.Errorf("%s: %+v, want %+v", id, got, w)
		}
	}
	if ty, _ := Type("minecraft:stray"); ty.ImmuneTo != "stray_immune_to" {
		t.Errorf("stray immune to %q", ty.ImmuneTo)
	}
	plains := biomeSpawns["plains"]
	if len(plains.Spawns[CategoryCreature]) != 6 || plains.Spawns[CategoryMonster][1] != (Spawner{"minecraft:zombie", 90, 4, 4}) {
		t.Errorf("plains: %+v", plains.Spawns)
	}
	if c, ok := biomeSpawns["soul_sand_valley"].Costs["minecraft:skeleton"]; !ok || c.Charge != .7 || c.EnergyBudget != .15 {
		t.Errorf("soul sand valley costs: %+v", biomeSpawns["soul_sand_valley"].Costs)
	}
	if o := structureOverrides["swamp_hut"][CategoryMonster]; !o.Piece || o.Spawns[0].Type != "minecraft:witch" {
		t.Errorf("swamp hut override %+v", o)
	}
}

func TestBiomeMapping(t *testing.T) {
	for b, want := range map[world.Biome]string{biome.Plains{}: "plains", biome.Swamp{}: "swamp", biome.River{}: "river",
		biome.DeepOcean{}: "deep_ocean", biome.WarpedForest{}: "warped_forest"} {
		in := biomeOf(b)
		if in.name != want {
			t.Errorf("%T: %s, want %s", b, in.name, want)
		}
	}
	if !biomeOf(biome.Swamp{}).tags["allows_surface_slime_spawns"] || !biomeOf(biome.River{}).tags["more_frequent_drowned_spawns"] {
		t.Error("biome tags")
	}
}

func TestSlimeChunks(t *testing.T) {
	// Reference values from java.util.Random with the expression of
	// WorldgenRandom.seedSlimeChunk (int overflow included), salt 987234911.
	for _, c := range slimeChunkRefs {
		if got := IsSlimeChunk(int32(c.x), int32(c.z), c.seed, 987234911); got != c.slime {
			t.Errorf("slime chunk %d,%d seed %d: %v, want %v", c.x, c.z, c.seed, got, c.slime)
		}
	}
}

func TestSkyDarken(t *testing.T) {
	for _, c := range []struct {
		time          int
		rain, thunder bool
		want          int
	}{
		{6000, false, false, 0}, {18000, false, false, 11}, {1000, false, false, 0},
		{6000, true, false, 3}, {6000, true, true, 5}, {18000, true, true, 11},
	} {
		if got := skyDarken(world.Overworld, c.time, c.rain, c.thunder); got != c.want {
			t.Errorf("skyDarken(%d, rain %v, thunder %v) = %d, want %d", c.time, c.rain, c.thunder, got, c.want)
		}
	}
	if skyDarken(world.Nether, 6000, false, false) != 11 || skyDarken(world.End, 18000, false, false) != 0 {
		t.Error("nether/end sky darkening")
	}
}

// TestLightRule checks Monster.isDarkEnoughToSpawn against its exact
// probabilities: P(sky <= rand(32)) * P(brightness <= sample).
func TestLightRule(t *testing.T) {
	rnd := newTestRand(7)
	const n = 200000
	for _, c := range []struct {
		name                   string
		dim                    *dimensionType
		sky, block, brightness int
		want                   float64
	}{
		{"pitch dark", &overworldType, 0, 0, 0, 1},
		{"any block light", &overworldType, 0, 1, 1, 0},
		{"brightness 7", &overworldType, 0, 0, 7, 1. / 8},
		{"brightness 8", &overworldType, 0, 0, 8, 0},
		{"open sky at night", &overworldType, 15, 0, 4, 17. / 32 * 4 / 8},
		{"sky 7 under a roof edge", &overworldType, 7, 0, 0, 25. / 32},
		{"nether: block light up to 7", &netherType, 0, 7, 7, 1},
		{"nether: 8", &netherType, 0, 8, 8, 0},
		{"end: any brightness", &endType, 15, 0, 15, 17. / 32},
	} {
		ok := 0
		for range n {
			if darkEnoughToSpawn(c.dim, c.sky, c.block, func() int { return c.brightness }, rnd) {
				ok++
			}
		}
		got := float64(ok) / n
		if math.Abs(got-c.want) > .005 {
			t.Errorf("%s: P = %.4f, want %.4f", c.name, got, c.want)
		}
	}
}

func TestPlacements(t *testing.T) {
	w, _ := newWorld(t, worldOpts{radius: 2})
	run(w, func(tx *world.Tx) {
		e := testEnv(tx)
		zombie, _ := Type("minecraft:zombie")
		ocelot, _ := Type("minecraft:ocelot")
		blaze, _ := Type("minecraft:blaze")
		polar, _ := Type("minecraft:polar_bear")
		cod, _ := Type("minecraft:cod")
		strider, _ := Type("minecraft:strider")
		p := func(x int) cube.Pos { return cube.Pos{x, floorY + 1, 40} }
		// The floors: x 0 grass, 1 glass, 2 bottom slab, 3 top slab, 4
		// oak leaves, 5 magma, 6 ice, 7 trapdoor, 8 soul sand, 9 bedrock,
		// 10 grass with a rail on it, 11 grass with a carpet, 12 barrier.
		floors := []world.Block{block.Grass{}, block.Glass{}, block.Slab{Block: block.Stone{}},
			block.Slab{Block: block.Stone{}, Top: true}, block.Leaves{Type: block.OakLeaves(), Persistent: true}, block.Magma{},
			block.Ice{}, block.WoodTrapdoor{Wood: block.OakWood()}, block.SoulSand{}, block.Bedrock{}, block.Grass{}, block.Grass{}, block.Barrier{}}
		for x, b := range floors {
			tx.SetBlock(p(x).Side(cube.FaceDown), b, nil)
		}
		tx.SetBlock(p(10), block.Rail{}, nil)
		tx.SetBlock(p(11), block.Carpet{}, nil)
		for _, c := range []struct {
			t    *EntityType
			x    int
			want bool
		}{
			{zombie, 0, true}, {zombie, 1, false}, {zombie, 2, false}, {zombie, 3, true}, {zombie, 4, false},
			{ocelot, 4, true}, {zombie, 5, false}, {blaze, 5, true}, {zombie, 6, false}, {polar, 6, true},
			{zombie, 7, false}, {zombie, 8, true}, {zombie, 9, false}, {zombie, 10, false}, {zombie, 11, true},
			{zombie, 12, false},
		} {
			if got := e.isSpawnPositionOk(c.t, p(c.x)); got != c.want {
				t.Errorf("%s on %T (x %d): %v, want %v", c.t.ID, floors[c.x], c.x, got, c.want)
			}
		}
		// IN_WATER: water there and no conductor above; IN_LAVA: lava.
		wp := cube.Pos{0, floorY + 1, 60}
		tx.SetBlock(wp, block.Water{Still: true, Depth: 8}, nil)
		if !e.isSpawnPositionOk(cod, wp) || e.isSpawnPositionOk(zombie, wp) {
			t.Error("cod in water / zombie in water")
		}
		tx.SetBlock(wp.Side(cube.FaceUp), block.Stone{}, nil)
		if e.isSpawnPositionOk(cod, wp) {
			t.Error("cod under stone")
		}
		tx.SetBlock(wp.Side(cube.FaceUp), block.Glass{}, nil)
		if !e.isSpawnPositionOk(cod, wp) {
			t.Error("cod under glass (no conductor)")
		}
		lp := cube.Pos{4, floorY + 1, 60}
		tx.SetBlock(lp, block.Lava{Still: true, Depth: 8}, nil)
		if !e.isSpawnPositionOk(strider, lp) || e.isSpawnPositionOk(cod, lp) {
			t.Error("strider in lava")
		}
		// NO_RESTRICTIONS.
		fox, _ := Type("minecraft:fox")
		if !e.isSpawnPositionOk(fox, cube.Pos{0, 100, 0}) {
			t.Error("fox has no restrictions")
		}
		// A full block in the two cells or a signal source.
		tx.SetBlock(p(0).Side(cube.FaceUp), block.Stone{}, nil)
		if e.isSpawnPositionOk(zombie, p(0)) {
			t.Error("zombie under a stone block")
		}
		tx.SetBlock(p(0).Side(cube.FaceUp), block.Lever{}, nil)
		if e.isSpawnPositionOk(zombie, p(0)) {
			t.Error("zombie in a lever")
		}
	})
}

func TestTypeRules(t *testing.T) {
	w, _ := newWorld(t, worldOpts{radius: 2})
	run(w, func(tx *world.Tx) {
		e := testEnv(tx)
		cow, _ := Type("minecraft:cow")
		zombie, _ := Type("minecraft:zombie")
		husk, _ := Type("minecraft:husk")
		pos := cube.Pos{20, floorY + 1, 20}
		// Animals: grass below and raw brightness (no time of day) > 8.
		if !e.checkSpawnRules(cow, pos) {
			t.Error("cow on lit grass")
		}
		e.dayTime, e.skyDarken = 18000, 11
		if !e.checkSpawnRules(cow, pos) {
			t.Error("cow on grass at night (Animal ignores the time of day)")
		}
		tx.SetBlock(pos.Side(cube.FaceDown), block.Stone{}, nil)
		if e.checkSpawnRules(cow, pos) {
			t.Error("cow on stone")
		}
		// Monsters at noon under open sky: never (brightness 15 > 7).
		e.dayTime, e.skyDarken = 6000, 0
		for range 200 {
			if e.checkSpawnRules(zombie, pos) {
				t.Fatal("zombie at noon under the sky")
			}
		}
		// Peaceful: monsters never.
		e.difficulty = 0
		e.skyDarken = 15
		for range 200 {
			if e.checkSpawnRules(zombie, pos) || e.checkSpawnRules(husk, pos) {
				t.Fatal("monster on peaceful")
			}
		}
		e.difficulty = 2
		// At night (sky darkened to 0) monsters spawn under open sky about
		// 17/32 of the time; husks need to see the sky.
		ok := 0
		for range 4000 {
			if e.checkSpawnRules(zombie, pos) {
				ok++
			}
		}
		if p := float64(ok) / 4000; math.Abs(p-17./32) > .04 {
			t.Errorf("zombie at full darkness under the sky: %.3f, want %.3f", p, 17./32)
		}
		// Walk target: monsters need cost <= 0 (brightness <= 12 in the
		// overworld), animals grass or brightness >= 12.
		e.skyDarken = 0
		if e.mobCheckSpawnRules(zombie, pos) {
			t.Error("zombie walk target in full light")
		}
		if !e.mobCheckSpawnRules(cow, pos) {
			t.Error("cow walk target in full light")
		}
		e.skyDarken = 11
		if !e.mobCheckSpawnRules(zombie, pos) {
			t.Error("zombie walk target in the dark")
		}
		if e.mobCheckSpawnRules(cow, pos) {
			t.Error("cow walk target on stone in the dark")
		}
	})
}

func TestPackSizes(t *testing.T) {
	rnd := newTestRand(3)
	s := Spawner{"minecraft:horse", 5, 2, 6}
	seen := map[int]int{}
	for range 10000 {
		seen[s.sampleCount(rnd)]++
	}
	for n := 2; n <= 6; n++ {
		if seen[n] < 1700 || seen[n] > 2300 {
			t.Errorf("horse pack %d: %d of 10000", n, seen[n])
		}
	}
	if len(seen) != 5 {
		t.Errorf("horse packs %v", seen)
	}
	list := []Spawner{{"a", 3, 1, 1}, {"b", 1, 1, 1}}
	a := 0
	for range 10000 {
		if p, _ := weightedPick(list, rnd); p.Type == "a" {
			a++
		}
	}
	if a < 7250 || a > 7750 {
		t.Errorf("weighted pick: %d of 10000 a, want 7500", a)
	}
	for typ, want := range map[string]int{"minecraft:zombie": 4, "minecraft:horse": 6, "minecraft:llama": 6, "minecraft:wolf": 8,
		"minecraft:cod": 8, "minecraft:salmon": 5, "minecraft:ghast": 1, "minecraft:pillager": 1} {
		if got := maxSpawnClusterSize(typ); got != want {
			t.Errorf("max cluster %s = %d, want %d", typ, got, want)
		}
	}
}

func TestRemoveWhenFarAway(t *testing.T) {
	for typ, want := range map[string]bool{"minecraft:zombie": true, "minecraft:cow": false, "minecraft:salmon": true,
		"minecraft:villager": false, "minecraft:hoglin": true, "minecraft:zombie_horse": true, "minecraft:shulker": false,
		"minecraft:bat": true, "minecraft:mooshroom": false, "minecraft:axolotl": true, "minecraft:glow_squid": true} {
		if got := defaultRemoveWhenFarAway(typ, false); got != want {
			t.Errorf("removeWhenFarAway %s = %v, want %v", typ, got, want)
		}
	}
	if defaultRemoveWhenFarAway("minecraft:cat", true) || !defaultRemoveWhenFarAway("minecraft:cat", false) {
		t.Error("cats: after 2400 ticks only")
	}
}
