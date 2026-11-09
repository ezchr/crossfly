//go:build crossfly_full

package spawn

import (
	"context"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/data"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

var registerOnce sync.Once

// testTypes registers the mob types the tests spawn from the embedded
// definitions (without package vanilla, which imports this package).
func testTypes(t *testing.T) {
	registerOnce.Do(func() {
		world.DefaultBlockRegistry.Finalize()
		for _, name := range []string{"cow.json", "zombie.json", "sheep.json"} {
			raw, ok := data.Entity(name)
			if !ok {
				panic(name)
			}
			d, err := mob.ParseDefinition(raw)
			if err != nil {
				panic(err)
			}
			mob.RegisterType(d)
		}
	})
}

// newWorld returns a synchronous flat grass world (plains unless b is set)
// with a survival player standing at the origin.
func newWorld(t *testing.T, b world.Biome) (*world.World, *world.EntityHandle) {
	return newWorldRoof(t, b, false)
}

// roofGen is the flat world with a stone roof 5 blocks above the grass,
// generated so the light under it is computed (SetBlock does not relight
// in a test world).
type roofGen struct{ generator.Flat }

func (g roofGen) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	g.Flat.GenerateChunk(pos, c)
	rid := world.DefaultBlockRegistry.BlockRuntimeID(block.Stone{})
	for x := range uint8(16) {
		for z := range uint8(16) {
			c.SetBlock(x, floorY+6, z, 0, rid)
		}
	}
}

func newWorldRoof(t *testing.T, b world.Biome, roof bool) (*world.World, *world.EntityHandle) {
	t.Helper()
	testTypes(t)
	if b == nil {
		b = biome.Plains{}
	}
	var gen world.Generator = generator.NewFlat(b, []world.Block{block.Grass{}, block.Dirt{}, block.Dirt{}, block.Bedrock{}})
	if roof {
		gen = roofGen{gen.(generator.Flat)}
	}
	types := append(entity.DefaultRegistry.Types(), mob.Types()...)
	types = append(types, player.Type)
	reg := entity.DefaultRegistry.Config().New(types)
	w := world.Config{
		Entities:    reg,
		Provider:    world.NopProvider{},
		Generator:   gen,
		Synchronous: true,
	}.New()
	t.Cleanup(func() { Disable(w); _ = w.Close() })
	w.StopTime()
	w.StopWeatherCycle()
	w.SetDifficulty(world.DifficultyNormal)
	var h *world.EntityHandle
	run(w, func(tx *world.Tx) {
		// Load the chunks around the player.
		for cx := -6; cx <= 6; cx++ {
			for cz := -6; cz <= 6; cz++ {
				tx.Block(cube.Pos{cx * 16, 0, cz * 16})
			}
		}
		id := uuid.New()
		pos := mgl64.Vec3{0.5, floorY + 1, 0.5}
		h = world.EntitySpawnOpts{Position: pos, ID: id}.New(player.Type, player.Config{
			Name: "tester", UUID: id, Position: pos, GameMode: world.GameModeSurvival,
		})
		tx.AddEntity(h)
	})
	// World age must pass the first second: canSpawnInConditions requires
	// age > delay end (0 for rules without a delay).
	for range 25 {
		w.AdvanceTick()
	}
	return w, h
}

// floorY is the grass layer of the flat world (layers from -64 up).
const floorY = -61

func run(w *world.World, f func(tx *world.Tx)) {
	_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		f(tx)
		return struct{}{}, nil
	})
}

// spawnTicks runs n ticks of the spawner (no mob ticks, so herds stay where
// they spawned) and returns the naturally spawned mobs by identifier.
func spawnTicks(w *world.World, n int) map[string][]*world.EntityHandle {
	out := map[string][]*world.EntityHandle{}
	for i := 0; i < n; i++ {
		w.AdvanceTick()
		run(w, Tick)
	}
	run(w, func(tx *world.Tx) {
		for e := range tx.Entities() {
			if m, ok := mob.Of(e); ok && NaturallySpawned(m) {
				out[m.Identifier()] = append(out[m.Identifier()], m.H())
			}
		}
	})
	return out
}

// spawnNoTicks runs the spawner n times without advancing the world.
func spawnNoTicks(w *world.World, n int) map[string][]*world.EntityHandle {
	out := map[string][]*world.EntityHandle{}
	run(w, func(tx *world.Tx) {
		st := stateOf(w)
		for range n {
			st.ran = false
			Tick(tx)
		}
		for e := range tx.Entities() {
			if m, ok := mob.Of(e); ok && NaturallySpawned(m) {
				out[m.Identifier()] = append(out[m.Identifier()], m.H())
			}
		}
	})
	return out
}
