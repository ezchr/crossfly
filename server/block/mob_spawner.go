//go:build crossfly_full

package block

import (
	"math"
	"math/rand/v2"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

// The monster spawner only spawns with the mobs: it registers in Crossfly Full.
func init() {
	mobBlocks = append(mobBlocks, func() {
		world.RegisterBlock(MobSpawner{})
		world.RegisterItem(MobSpawner{})
	})
}

// MobSpawner is the monster spawner: a cage that spawns mobs of its type around it while a player is near
// (BDS BaseMobSpawner / MobSpawnerBlockActor; Java Edition BaseSpawner when the mob runs in Java mob mode).
// The zero value is a spawner with BDS's defaults and no mob; NewMobSpawner returns the same.
type MobSpawner struct {
	transparent

	// EntityID is the entity identifier of the mob spawned, e.g. "minecraft:zombie". An empty spawner
	// does nothing.
	EntityID string
	// Delay is the number of ticks until the next spawn. -1 picks a new delay first.
	Delay int16
	// MinSpawnDelay..MaxSpawnDelay is the range the delay is picked from after each spawn.
	MinSpawnDelay, MaxSpawnDelay int16
	// SpawnCount is the number of spawn tries per spawn.
	SpawnCount int16
	// MaxNearbyEntities is how many mobs of the type may be around before the spawner stops.
	MaxNearbyEntities int16
	// RequiredPlayerRange is how close a player must be for the spawner to run.
	RequiredPlayerRange int16
	// SpawnRange is the size of the box the mobs spawn in, in blocks either way.
	SpawnRange int16
	// DisplayWidth, DisplayHeight and DisplayScale size the mob Bedrock clients draw in the cage.
	DisplayWidth, DisplayHeight, DisplayScale float32

	// set is true once the fields hold a spawner's values (its NBT or NewMobSpawner): a spawner made with
	// MobSpawner{} (a placed item, /setblock) gets the defaults.
	set bool
}

// NewMobSpawner returns a spawner of the mob passed with BDS's defaults (BaseMobSpawner's constructor):
// a delay of 20 ticks, then 200-800 ticks between spawns, 4 tries in a box 4 blocks either way, at most 6
// of the mob around and a player within 16 blocks.
func NewMobSpawner(entityID string) MobSpawner {
	return MobSpawner{EntityID: entityID, Delay: 20, MinSpawnDelay: 200, MaxSpawnDelay: 800, SpawnCount: 4,
		MaxNearbyEntities: 6, RequiredPlayerRange: 16, SpawnRange: 4,
		DisplayWidth: 0.8, DisplayHeight: 1.8, DisplayScale: 1, set: true}
}

// withDefaults returns s with the defaults if it never had values.
func (s MobSpawner) withDefaults() MobSpawner {
	if s.set {
		return s
	}
	return NewMobSpawner(s.EntityID)
}

// WithMob returns the spawner set to spawn the mob passed, as a spawn egg used on it does
// (MobSpawnerBlockActor::setMob; Java SpawnEggItem.useOn setting the Spawner's entity).
func (s MobSpawner) WithMob(entityID string) world.Block {
	s = s.withDefaults()
	s.EntityID = entityID
	return s
}

// Model ...
func (MobSpawner) Model() world.BlockModel { return model.Solid{} }

// BreakInfo: a pickaxe mines it, it drops nothing and 15-43 experience (SpawnerBlock.spawnAfterBreak:
// 15 + rand(15) + rand(15); no experience with silk touch, which the player checks).
func (s MobSpawner) BreakInfo() BreakInfo {
	return newBreakInfo(5, pickaxeHarvestable, pickaxeEffective, simpleDrops()).withXPDropRange(15, 43).withBlastResistance(25)
}

// EncodeItem ...
func (MobSpawner) EncodeItem() (name string, meta int16) { return "minecraft:mob_spawner", 0 }

// EncodeBlock ...
func (MobSpawner) EncodeBlock() (string, map[string]any) { return "minecraft:mob_spawner", nil }

// Hash ...
func (MobSpawner) Hash() (uint64, uint64) { return hashMobSpawner, 0 }

// EncodeNBT is BaseMobSpawner::save: the client draws the mob in the cage from EntityIdentifier and the
// display size.
func (s MobSpawner) EncodeNBT() map[string]any {
	s = s.withDefaults()
	return map[string]any{
		"id":                  "MobSpawner",
		"EntityIdentifier":    s.EntityID,
		"Delay":               s.Delay,
		"MinSpawnDelay":       s.MinSpawnDelay,
		"MaxSpawnDelay":       s.MaxSpawnDelay,
		"SpawnCount":          s.SpawnCount,
		"MaxNearbyEntities":   s.MaxNearbyEntities,
		"RequiredPlayerRange": s.RequiredPlayerRange,
		"SpawnRange":          s.SpawnRange,
		"DisplayEntityWidth":  s.DisplayWidth,
		"DisplayEntityHeight": s.DisplayHeight,
		"DisplayEntityScale":  s.DisplayScale,
	}
}

// DecodeNBT is BaseMobSpawner::load: keys that are missing keep the defaults.
func (s MobSpawner) DecodeNBT(data map[string]any) any {
	d := NewMobSpawner(nbtconv.String(data, "EntityIdentifier"))
	short := func(k string, v *int16) {
		if _, ok := data[k]; ok {
			*v = nbtconv.Int16(data, k)
		}
	}
	float := func(k string, v *float32) {
		if _, ok := data[k]; ok {
			*v = nbtconv.Float32(data, k)
		}
	}
	short("Delay", &d.Delay)
	short("MinSpawnDelay", &d.MinSpawnDelay)
	short("MaxSpawnDelay", &d.MaxSpawnDelay)
	short("SpawnCount", &d.SpawnCount)
	short("MaxNearbyEntities", &d.MaxNearbyEntities)
	short("RequiredPlayerRange", &d.RequiredPlayerRange)
	short("SpawnRange", &d.SpawnRange)
	float("DisplayEntityWidth", &d.DisplayWidth)
	float("DisplayEntityHeight", &d.DisplayHeight)
	float("DisplayEntityScale", &d.DisplayScale)
	return d
}

// SpawnerHost spawns the mobs of monster spawners. The mob packages install one with SetSpawnerHost
// (Bedrock mobs) or SetJavaSpawnerHost (Java mob mode); without one a spawner does nothing.
type SpawnerHost interface {
	// SpawnerMob reports whether the host spawns mobs of the entity identifier.
	SpawnerMob(id string) bool
	// SpawnerTry is one of the SpawnCount tries of the spawner s at pos: it spawns s's mob at at if the
	// mob may spawn there. It reports whether a mob spawned, and full if MaxNearbyEntities mobs of the type
	// are around already (the spawner then picks a new delay and stops).
	SpawnerTry(tx *world.Tx, pos cube.Pos, s MobSpawner, at mgl64.Vec3) (spawned world.Entity, full bool)
}

var spawnerHost, javaSpawnerHost SpawnerHost

// SetSpawnerHost installs the host spawners spawn Bedrock mobs through. Package mob/spawn calls it.
func SetSpawnerHost(h SpawnerHost) { spawnerHost = h }

// SetJavaSpawnerHost installs the host spawners spawn Java mob mode's mobs through, with Java Edition's rules.
// Mobs it does not have (not ported to Java yet) go to the Bedrock host.
func SetJavaSpawnerHost(h SpawnerHost) { javaSpawnerHost = h }

// spawnerHostFor returns the host of the mob and whether it follows Java Edition's rules.
func spawnerHostFor(id string) (SpawnerHost, bool) {
	if id == "" {
		return nil, false
	}
	if javaSpawnerHost != nil && javaSpawnerHost.SpawnerMob(id) {
		return javaSpawnerHost, true
	}
	if spawnerHost != nil && spawnerHost.SpawnerMob(id) {
		return spawnerHost, false
	}
	return nil, false
}

// spawnerTimer is the running delay of a spawner. The delay counts down every tick a player is near; the
// block keeps the delay it was last given and is only changed (sent to viewers and saved) when a new delay
// is picked, so a ticking spawner does not resend its block entity every tick.
type spawnerTimer struct {
	delay int
	last  int64
	id    string
}

var spawnerTimers sync.Map // *world.World -> map[cube.Pos]*spawnerTimer, used in the world's transactions only

func timerOf(tx *world.Tx, pos cube.Pos, s MobSpawner, tick int64) *spawnerTimer {
	v, _ := spawnerTimers.LoadOrStore(tx.World(), map[cube.Pos]*spawnerTimer{})
	m := v.(map[cube.Pos]*spawnerTimer)
	if tick%1200 == 0 {
		for p, t := range m {
			if t.last < tick-20 {
				delete(m, p)
			}
		}
	}
	t, ok := m[pos]
	if !ok || t.last < tick-1 || t.id != s.EntityID {
		// New, reloaded or changed: start from the block's delay.
		t = &spawnerTimer{delay: int(s.Delay), id: s.EntityID}
		m[pos] = t
	}
	t.last = tick
	return t
}

// Tick runs the spawner (BDS BaseMobSpawner::tick; Java BaseSpawner.serverTick for Java mob mode's mobs).
func (s MobSpawner) Tick(currentTick int64, pos cube.Pos, tx *world.Tx) {
	s = s.withDefaults()
	host, java := spawnerHostFor(s.EntityID)
	if host == nil {
		return
	}
	t := timerOf(tx, pos, s, currentTick)
	if !spawnerPlayerNear(tx, pos, float64(s.RequiredPlayerRange), java) {
		return
	}
	if java && t.delay == -1 {
		s.resetDelay(tx, pos, t)
	}
	if t.delay > 0 {
		t.delay--
		return
	}
	if t.delay == -1 {
		// BDS: a delay of -1 picks a new one without spawning.
		s.resetDelay(tx, pos, t)
		return
	}
	spawned := false
	r := float64(s.SpawnRange)
	for range max(int(s.SpawnCount), 0) {
		at := mgl64.Vec3{
			float64(pos[0]) + (rand.Float64()-rand.Float64())*r + 0.5,
			float64(pos[1] + rand.IntN(3) - 1),
			float64(pos[2]) + (rand.Float64()-rand.Float64())*r + 0.5,
		}
		e, full := host.SpawnerTry(tx, pos, s, at)
		if full {
			s.resetDelay(tx, pos, t)
			return
		}
		if e != nil {
			spawned = true
			tx.AddParticle(pos.Vec3(), spawnerParticle(e, java))
		}
	}
	if spawned {
		s.resetDelay(tx, pos, t)
	}
}

// spawnerParticle is level event 2004 at the spawner: BDS passes the size of the mob spawned, Java 0.
func spawnerParticle(e world.Entity, java bool) particle.MobSpawnerSpawn {
	if java {
		return particle.MobSpawnerSpawn{}
	}
	box := e.H().Type().BBox(e)
	size := box.Max().Sub(box.Min())
	return particle.MobSpawnerSpawn{Width: int(math.Ceil(size[0])), Height: int(math.Ceil(size[1]))}
}

// resetDelay picks the next delay (BaseMobSpawner::_delay; BaseSpawner.delay) and stores it in the block, which
// sends it to the viewers: their spinning mob slows down again.
func (s MobSpawner) resetDelay(tx *world.Tx, pos cube.Pos, t *spawnerTimer) {
	d := int(s.MinSpawnDelay)
	if s.MaxSpawnDelay > s.MinSpawnDelay {
		d += rand.IntN(int(s.MaxSpawnDelay - s.MinSpawnDelay))
	}
	t.delay = d
	s.Delay = int16(d)
	tx.SetBlock(pos, s, nil)
}

// spawnerPlayerNear reports whether a player that is alive and not a spectator is within dist of the
// spawner's centre (BDS Dimension::fetchNearestInteractablePlayer, inclusive; Java
// Level.hasNearbyAlivePlayer, exclusive).
func spawnerPlayerNear(tx *world.Tx, pos cube.Pos, dist float64, java bool) bool {
	c := pos.Vec3Centre()
	for p := range tx.Players() {
		if g, ok := p.(interface{ GameMode() world.GameMode }); ok && !g.GameMode().HasCollision() {
			continue
		}
		if d, ok := p.(interface{ Dead() bool }); ok && d.Dead() {
			continue
		}
		d2 := p.Position().Sub(c).LenSqr()
		if d2 < dist*dist || (!java && d2 == dist*dist) {
			return true
		}
	}
	return false
}

// Compile time checks.
var (
	_ world.TickerBlock = MobSpawner{}
	_ item.MobSetter    = MobSpawner{}
)
