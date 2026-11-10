//go:build crossfly_full

package spawn

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/go-gl/mathgl/mgl64"
)

func TestParseVanillaRules(t *testing.T) {
	rs := Rules()
	if len(rs) < 90 {
		t.Fatalf("only %d conditions loaded", len(rs))
	}
	var cow, zombie []*Rule
	for _, r := range rs {
		switch r.Identifier {
		case "minecraft:cow":
			cow = append(cow, r)
		case "minecraft:zombie":
			zombie = append(zombie, r)
		}
	}
	if len(cow) != 1 || len(zombie) != 2 {
		t.Fatalf("cow %d, zombie %d conditions", len(cow), len(zombie))
	}
	c := cow[0]
	if !c.Surface || c.Underground || c.Weight != 8 || c.MinBrightness != 7 || c.MaxBrightness != 15 ||
		len(c.Herds) != 1 || c.Herds[0].MinSize != 2 || c.Herds[0].MaxSize != 3 ||
		len(c.OnBlocks) != 1 || c.OnBlocks[0] != "minecraft:grass_block" || c.Pool != "animal" {
		t.Fatalf("cow rule parsed wrong: %+v", c)
	}
	z := zombie[1]
	if !z.Surface || !z.Underground || z.MaxBrightness != 7 || !z.AdjustForWeather || z.MinDifficulty != 1 ||
		z.MaxDifficulty != 3 || len(z.Permutations) != 2 || z.Permutations[1].EntityType != "minecraft:zombie_villager_v2" {
		t.Fatalf("zombie rule parsed wrong: %+v", z)
	}
	if PopulationControl("minecraft:cod") != "water_animal" || PopulationControl("minecraft:cow") != "animal" {
		t.Fatal("pools")
	}
	// Plains offers cows and zombies; a frozen ocean offers neither.
	has := func(b world.Biome, id string) bool {
		for _, r := range rulesFor(b) {
			if r.Identifier == id {
				return true
			}
		}
		return false
	}
	if !has(biome.Plains{}, "minecraft:cow") || !has(biome.Plains{}, "minecraft:zombie") {
		t.Fatal("plains lacks cow or zombie")
	}
	if has(biome.FrozenOcean{}, "minecraft:cow") || !has(biome.FrozenOcean{}, "minecraft:stray") || has(biome.Plains{}, "minecraft:stray") {
		t.Fatal("biome filters wrong (frozen ocean)")
	}
	if !has(biome.SnowyPlains{}, "minecraft:rabbit") { // is_snow_covered
		t.Fatal("snowy plains lacks rabbits")
	}
}

func TestSkyDarken(t *testing.T) {
	for _, c := range []struct {
		time        int
		rain, thund bool
		want        int
	}{{6000, false, false, 0}, {18000, false, false, 11}, {6000, true, false, 3}, {6000, true, true, 5}} {
		if got := skyDarken(c.time, c.rain, c.thund); got != c.want {
			t.Errorf("skyDarken(%v, %v, %v) = %v, want %v", c.time, c.rain, c.thund, got, c.want)
		}
	}
}

func TestSlimeChunk(t *testing.T) {
	// The MT19937 reference: seed 5489 gives 3499211612 first.
	if v := mt19937First(5489); v != 3499211612 {
		t.Fatalf("mt19937 first output %v", v)
	}
	n := 0
	for x := int32(-50); x < 50; x++ {
		for z := int32(-50); z < 50; z++ {
			if isSlimeChunk(world.ChunkPos{x, z}) {
				n++
			}
		}
	}
	if n < 800 || n > 1200 {
		t.Fatalf("%d of 10000 chunks are slime chunks, want about 1000", n)
	}
}

func TestNothingSpawnsWhenDisabled(t *testing.T) {
	w, _ := newWorld(t, nil)
	w.SetTime(6000)
	if got := spawnTicks(w, 3000); len(got) != 0 {
		t.Fatalf("mobs spawned with spawning off: %v", got)
	}
	Enable(w, Config{ManualTick: true, Seed: 1})
	Disable(w)
	if got := spawnTicks(w, 2000); len(got) != 0 {
		t.Fatalf("mobs spawned after Disable: %v", got)
	}
}

func TestCowsSpawnInDaylightInHerds(t *testing.T) {
	w, _ := newWorld(t, nil)
	w.SetTime(6000)
	Enable(w, Config{ManualTick: true, Seed: 7})
	// The first herd: with nothing around yet the cap does not cut it, and
	// all members spawn on the same block.
	var first map[string][]*world.EntityHandle
	for i := 0; i < 3000 && len(first) == 0; i++ {
		first = spawnTicks(w, 1)
	}
	if len(first) == 0 {
		t.Fatal("no animals spawned")
	}
	run(w, func(tx *world.Tx) {
		byPos := map[[2]float64]int{}
		for _, hs := range first {
			for _, h := range hs {
				if m, ok := mob.OfHandle(tx, h); ok {
					byPos[[2]float64{m.Pos()[0], m.Pos()[2]}]++
				}
			}
		}
		if len(byPos) != 1 {
			t.Fatalf("first spawn not one herd: %v", byPos)
		}
		for _, n := range byPos {
			if n < 2 || n > 3 {
				t.Fatalf("herd of %d, want 2-3", n)
			}
		}
	})
	// Each herd is cows or sheep by the spawn weights, and the animal cap
	// fills after about three herds: now and then they are all sheep (the
	// mobs wander, so the later spawn checks differ from run to run even with
	// the spawner seeded). Then the animals are cleared for another round.
	var cows []*world.EntityHandle
	for round := 0; round < 10 && len(cows) == 0; round++ {
		if round > 0 {
			run(w, func(tx *world.Tx) {
				for e := range tx.Entities() {
					if m, ok := mob.Of(e); ok && NaturallySpawned(m) {
						_ = m.Close()
					}
				}
			})
		}
		got := spawnTicks(w, 6000)
		if len(got["minecraft:zombie"]) != 0 {
			t.Fatalf("zombies in daylight: %d", len(got["minecraft:zombie"]))
		}
		cows = got["minecraft:cow"]
	}
	if len(cows) == 0 {
		t.Fatalf("no cows spawned in 10 rounds")
	}
	run(w, func(tx *world.Tx) {
		for _, h := range cows {
			m, ok := mob.OfHandle(tx, h)
			if !ok {
				continue
			}
			p := m.Pos()
			if _, grass := tx.Block(cube.PosFromVec3(p).Side(cube.FaceDown)).(block.Grass); !grass {
				t.Errorf("cow at %v not on grass", p)
			}
			d := p.Sub(mgl(0.5, floorY+1, 0.5)).Len()
			if d < 24 || d > 44.5 {
				t.Errorf("cow spawned %.1f blocks from the player", d)
			}
		}
	})
}

func TestZombiesNightNotDay(t *testing.T) {
	w, _ := newWorld(t, nil)
	w.SetTime(6000)
	Enable(w, Config{ManualTick: true, Seed: 3})
	day := spawnTicks(w, 4000)
	if len(day["minecraft:zombie"]) != 0 {
		t.Fatalf("%d zombies spawned at noon", len(day["minecraft:zombie"]))
	}
	dayCows := map[*world.EntityHandle]bool{}
	for _, h := range day["minecraft:cow"] {
		dayCows[h] = true
	}
	w.SetTime(18000)
	got := spawnTicks(w, 6000)
	if len(got["minecraft:zombie"]) == 0 {
		t.Fatal("no zombies at midnight")
	}
	for _, h := range got["minecraft:cow"] {
		if !dayCows[h] {
			t.Errorf("cow spawned at night")
		}
	}
	// Peaceful: monsters have difficulty_filter min easy.
	w2, _ := newWorld(t, nil)
	w2.SetTime(18000)
	w2.SetDifficulty(world.DifficultyPeaceful)
	Enable(w2, Config{ManualTick: true, Seed: 3})
	if got := spawnTicks(w2, 4000); len(got["minecraft:zombie"]) != 0 {
		t.Fatalf("zombies spawned on peaceful")
	}
}

func TestZombiesInDarkCaveByDay(t *testing.T) {
	// A stone roof over the whole area: at noon the surface under it is
	// dark, but it is not the surface (the roof is), so only
	// spawns_underground mobs (zombies) may spawn there.
	w, _ := newWorldRoof(t, nil, true)
	w.SetTime(6000)
	Enable(w, Config{ManualTick: true, Seed: 11})
	// The world's redstone engine makes every tick of this world slow, so
	// the spawner runs without world ticks (mobs do not tick either).
	got := spawnNoTicks(w, 6000)
	if len(got["minecraft:zombie"]) == 0 {
		t.Fatal("no zombies under the roof")
	}
	if len(got["minecraft:cow"]) != 0 {
		t.Fatal("cows spawned under the roof (not surface, too dark)")
	}
}

func TestGlobalCap(t *testing.T) {
	w, _ := newWorld(t, nil)
	w.SetTime(6000)
	Enable(w, Config{ManualTick: true, Seed: 5, GlobalCap: 1})
	// Mobs tick during AdvanceTick, so the spawnable count is 1 after the
	// first spawn and a herd of 2-3 is cut to 1.
	got := spawnTicks(w, 6000)
	n := len(got["minecraft:cow"]) + len(got["minecraft:sheep"])
	if n == 0 || n > 2 {
		t.Fatalf("%d animals with a global cap of 1", n)
	}
}

// TestCategoryAndDensityCaps runs many attempts in one chunk: the 9x9
// chunks around it never hold more than the cap of the category.
func TestCategoryAndDensityCaps(t *testing.T) {
	for _, c := range []struct {
		time  int
		id    string
		limit int
	}{{6000, "minecraft:cow", 4}, {18000, "minecraft:zombie", 8}} {
		w, _ := newWorld(t, nil)
		w.SetTime(c.time)
		Enable(w, Config{ManualTick: true, Seed: 9})
		chunk := world.ChunkPos{2, 0}
		run(w, func(tx *world.Tx) {
			st := stateOf(w)
			cfg := st.cfg.Load()
			Tick(tx) // seeds the random source and caches the players
			e := st.newEnv(tx, cfg)
			for range 3000 {
				st.spawnInChunk(e, cfg, chunk)
			}
			n, pool := 0, 0
			box := cube.Box(float64((chunk[0]-4)*16), -64, float64((chunk[1]-4)*16), float64((chunk[0]+5)*16), 320, float64((chunk[1]+5)*16))
			for e := range tx.EntitiesWithin(box) {
				if m, ok := mob.Of(e); ok {
					if m.Identifier() == c.id {
						n++
					}
					if PopulationControl(m.Identifier()) == PopulationControl(c.id) {
						pool++
					}
				}
			}
			if n < 2 || pool > c.limit {
				t.Errorf("%v: %d spawned, %d of the category in 9x9 chunks, cap %d", c.id, n, pool, c.limit)
			}
		})
	}
}

func mgl(x, y, z float64) mgl64.Vec3 { return mgl64.Vec3{x, y, z} }
