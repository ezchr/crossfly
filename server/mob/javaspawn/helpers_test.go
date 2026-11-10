//go:build crossfly_full

package javaspawn

import (
	"context"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// testMob is a stand-in for a Java mode mob: just the state spawning reads.
type testMob struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData
}

type testMobState struct {
	typ        string
	natural    bool
	persistent bool
	noAction   int
}

func (m *testMob) st() *testMobState         { return m.data.Data.(*testMobState) }
func (m *testMob) H() *world.EntityHandle    { return m.handle }
func (m *testMob) Position() mgl64.Vec3      { return m.data.Pos }
func (m *testMob) Rotation() cube.Rotation   { return m.data.Rot }
func (m *testMob) JavaType() string          { return m.st().typ }
func (m *testMob) NaturallySpawned() bool    { return m.st().natural }
func (m *testMob) PersistenceRequired() bool { return m.st().persistent }
func (m *testMob) NoActionTime() int         { return m.st().noAction }
func (m *testMob) SetNoActionTime(t int)     { m.st().noAction = t }
func (m *testMob) Close() error {
	m.tx.RemoveEntity(m)
	_ = m.handle.Close()
	return nil
}

type testMobType struct{}

func (testMobType) Open(tx *world.Tx, h *world.EntityHandle, data *world.EntityData) world.Entity {
	return &testMob{tx: tx, handle: h, data: data}
}
func (testMobType) EncodeEntity() string { return "crossfly:test_java_mob" }
func (testMobType) BBox(e world.Entity) cube.BBox {
	t := entityTypes[e.(*testMob).JavaType()]
	return spawnAABB(t, 0, 0, 0)
}
func (testMobType) DecodeNBT(map[string]any, *world.EntityData) {}
func (testMobType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

type testMobConfig struct{ st testMobState }

func (c testMobConfig) Apply(data *world.EntityData) { s := c.st; data.Data = &s }

// testRegistry implements every type unless only is set.
type testRegistry struct{ only map[string]bool }

func (r testRegistry) Has(typ string) bool { return r.only == nil || r.only[typ] }
func (r testRegistry) Spawn(tx *world.Tx, typ string, pos mgl64.Vec3, yaw float64, group any) (Mob, any, bool) {
	m := addMob(tx, typ, pos, testMobState{natural: true})
	m.data.Rot = cube.Rotation{yaw, 0}
	return m, group, true
}
func (testRegistry) Mob(e world.Entity) (Mob, bool) {
	m, ok := e.(*testMob)
	return m, ok
}

func addMob(tx *world.Tx, typ string, pos mgl64.Vec3, st testMobState) *testMob {
	st.typ = typ
	h := world.EntitySpawnOpts{Position: pos}.New(testMobType{}, testMobConfig{st})
	return tx.AddEntity(h).(*testMob)
}

// floorY is the grass layer of the test worlds (layers from -64 up).
const floorY = -61

// roofGen is a flat world with a stone (or glass) roof 3 blocks above the
// grass: one layer of 2-high dark air, lit by the generator.
type roofGen struct {
	generator.Flat
	roof world.Block
}

func (g roofGen) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	g.Flat.GenerateChunk(pos, c)
	rid := world.DefaultBlockRegistry.BlockRuntimeID(g.roof)
	for x := range uint8(16) {
		for z := range uint8(16) {
			c.SetBlock(x, floorY+3, z, 0, rid)
		}
	}
}

type worldOpts struct {
	biome     world.Biome
	roof      world.Block // nil: open sky
	playerPos mgl64.Vec3
	radius    int // chunks loaded around the player
}

// newWorld returns a synchronous flat grass world with a survival player.
// The world spawn is moved far away (its 24-block rule) and the time stopped
// at noon.
func newWorld(t *testing.T, o worldOpts) (*world.World, *world.EntityHandle) {
	t.Helper()
	world.DefaultBlockRegistry.Finalize()
	if o.biome == nil {
		o.biome = biome.Plains{}
	}
	if o.radius == 0 {
		o.radius = 9
	}
	if o.playerPos == (mgl64.Vec3{}) {
		o.playerPos = mgl64.Vec3{8.5, floorY + 1, 8.5}
	}
	var gen world.Generator = generator.NewFlat(o.biome, []world.Block{block.Grass{}, block.Dirt{}, block.Dirt{}, block.Bedrock{}})
	if o.roof != nil {
		gen = roofGen{gen.(generator.Flat), o.roof}
	}
	types := append(entity.DefaultRegistry.Types(), player.Type, testMobType{})
	w := world.Config{
		Entities:    entity.DefaultRegistry.Config().New(types),
		Provider:    world.NopProvider{},
		Generator:   gen,
		Synchronous: true,
	}.New()
	t.Cleanup(func() { _ = w.Close() })
	w.StopTime()
	w.SetTime(6000)
	w.StopWeatherCycle()
	w.SetDifficulty(world.DifficultyNormal)
	w.SetSpawn(cube.Pos{100000, 0, 100000})
	SetRegistry(testRegistry{})
	var h *world.EntityHandle
	run(w, func(tx *world.Tx) {
		pc := chunkOf(cube.PosFromVec3(o.playerPos))
		for cx := -o.radius; cx <= o.radius; cx++ {
			for cz := -o.radius; cz <= o.radius; cz++ {
				tx.Block(cube.Pos{(int(pc[0]) + cx) * 16, 0, (int(pc[1]) + cz) * 16})
			}
		}
		id := uuid.New()
		h = world.EntitySpawnOpts{Position: o.playerPos, ID: id}.New(player.Type, player.Config{
			Name: "tester", UUID: id, Position: o.playerPos, GameMode: world.GameModeSurvival,
		})
		tx.AddEntity(h)
	})
	return w, h
}

func run(w *world.World, f func(tx *world.Tx)) {
	_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		f(tx)
		return struct{}{}, nil
	})
}

// tickN runs Tick n times in the same world tick (no entity ticks, so
// mobs stay where they spawned).
func tickN(w *world.World, n int) {
	run(w, func(tx *world.Tx) {
		st := stateOf(w)
		for range n {
			st.ran = false
			Tick(tx)
		}
	})
}

// mobs returns the test mobs by type.
func mobs(w *world.World) map[string][]*testMob {
	out := map[string][]*testMob{}
	run(w, func(tx *world.Tx) {
		for e := range tx.Entities() {
			if m, ok := e.(*testMob); ok {
				out[m.JavaType()] = append(out[m.JavaType()], m)
			}
		}
	})
	return out
}

func clearMobs(w *world.World) {
	run(w, func(tx *world.Tx) {
		var all []*testMob
		for e := range tx.Entities() {
			if m, ok := e.(*testMob); ok {
				all = append(all, m)
			}
		}
		for _, m := range all {
			_ = m.Close()
		}
	})
}

// testEnv returns a spawning env for tx with the default config.
func testEnv(tx *world.Tx) *env {
	st := stateOf(tx.World())
	if st.rnd == nil {
		st.rnd = newTestRand(1)
	}
	e := st.newEnv(tx, defaultConfig, testRegistry{})
	st.collectPlayers(tx)
	return e
}
