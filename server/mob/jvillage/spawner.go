//go:build crossfly_full

package jvillage

import (
	"math/rand/v2"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javaspawn"
	"github.com/df-mc/dragonfly/server/mob/jpoi"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Paper's entities.spawning.wandering-trader defaults (vanilla values).
const (
	traderSpawnMinuteLength      = 1200
	traderSpawnDayLength         = 24000
	traderSpawnChanceMin         = 25
	traderSpawnChanceMax         = 75
	traderSpawnChanceFailureIncr = 25
)

// traderSpawner is WanderingTraderSpawner with its WanderingTraderData.
type traderSpawner struct {
	tickDelay   int
	spawnDelay  int
	spawnChance int
	started     bool
	r           *rand.Rand
}

var (
	spawnersMu sync.Mutex
	spawners   = map[*world.World]*traderSpawner{}
)

func init() {
	// The hook returns at once unless the Java mob mode is installed.
	world.RegisterTickHook(tickTraderSpawner)
}

// TraderSpawning reports whether the doTraderSpawning game rule is on.
var TraderSpawning = func(tx *world.Tx) bool { return true }

func spawnerOf(w *world.World) *traderSpawner {
	spawnersMu.Lock()
	defer spawnersMu.Unlock()
	s := spawners[w]
	if s == nil {
		s = &traderSpawner{r: rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))}
		spawners[w] = s
	}
	return s
}

// tickTraderSpawner is WanderingTraderSpawner.tick, run in the overworld
// where Java spawning is on.
func tickTraderSpawner(tx *world.Tx) {
	if !javamob.Installed() {
		return
	}
	w := tx.World()
	if w.Dimension() != world.Overworld || !javaspawn.Enabled(w) {
		return
	}
	s := spawnerOf(w)
	if !s.started {
		s.started = true
		s.tickDelay = traderSpawnMinuteLength
		s.spawnDelay = traderSpawnDayLength
		s.spawnChance = traderSpawnChanceMin
	}
	if !TraderSpawning(tx) {
		return
	}
	if s.tickDelay-1 > 0 {
		s.tickDelay--
		return
	}
	s.tickDelay = traderSpawnMinuteLength
	s.spawnDelay -= traderSpawnMinuteLength
	if s.spawnDelay > 0 {
		return
	}
	s.spawnDelay = traderSpawnDayLength
	chance := s.spawnChance
	s.spawnChance = min(max(chance+traderSpawnChanceFailureIncr, traderSpawnChanceMin), traderSpawnChanceMax)
	if s.r.IntN(100) <= chance && s.spawn(tx) {
		s.spawnChance = traderSpawnChanceMin
	}
}

// spawn is WanderingTraderSpawner.spawn: near a random player (or the
// meeting point within 48 blocks of them), with two trader llamas.
func (s *traderSpawner) spawn(tx *world.Tx) bool {
	var players []*player.Player
	for e := range tx.Players() {
		if p, ok := e.(*player.Player); ok && !p.Dead() {
			players = append(players, p)
		}
	}
	if len(players) == 0 {
		return true
	}
	p := players[s.r.IntN(len(players))]
	if s.r.IntN(10) != 0 {
		return false
	}
	ppos := cube.PosFromVec3(p.Position())
	ref := ppos
	if mp, ok := jpoi.For(tx).Find(tx, jpoi.Is(jpoi.Meeting), nil, ppos, 48, jpoi.AnyOccupancy); ok {
		ref = mp
	}
	pos, ok := s.findSpawnPositionNear(tx, ref, 48)
	if !ok || !hasEnoughSpace(tx, pos) {
		return false
	}
	if withoutTraderSpawns(tx, pos) {
		return false
	}
	m, err := javamob.Spawn(tx, WanderingTraderID, mgl64.Vec3{float64(pos[0]) + .5, float64(pos[1]), float64(pos[2]) + .5}, javamob.SpawnEvent, javamob.SpawnOptions{
		Configure: func(m *javamob.Mob) {
			if t, ok := TraderOf(m); ok {
				t.DespawnDelay = 48000
			}
		},
	})
	if err != nil {
		return false
	}
	for range 2 {
		s.tryToSpawnLlama(tx, m, 4)
	}
	if t, ok := TraderOf(m); ok {
		r := ref
		t.SetWanderTarget(&r)
	}
	m.SetHomeTo(ref, 16)
	return true
}

func (s *traderSpawner) tryToSpawnLlama(tx *world.Tx, trader *javamob.Mob, radius int) {
	pos, ok := s.findSpawnPositionNear(tx, BlockPos(trader), radius)
	if !ok {
		return
	}
	if TraderLlamaHook != nil {
		TraderLlamaHook(tx, trader, mgl64.Vec3{float64(pos[0]) + .5, float64(pos[1]), float64(pos[2]) + .5})
	}
}

// TraderLlamaHook spawns a trader llama leashed to the trader (set by the
// llama's package; nil: no llamas).
var TraderLlamaHook func(tx *world.Tx, trader *javamob.Mob, pos mgl64.Vec3)

// findSpawnPositionNear is WanderingTraderSpawner.findSpawnPositionNear.
func (s *traderSpawner) findSpawnPositionNear(tx *world.Tx, ref cube.Pos, radius int) (cube.Pos, bool) {
	for range 10 {
		x := ref[0] + s.r.IntN(radius*2) - radius
		z := ref[2] + s.r.IntN(radius*2) - radius
		y := tx.HighestBlock(x, z) + 1
		p := cube.Pos{x, y, z}
		if javaspawn.IsSpawnPositionOk(tx, p, WanderingTraderID) {
			return p, true
		}
	}
	return cube.Pos{}, false
}

// hasEnoughSpace is WanderingTraderSpawner.hasEnoughSpace: a 2x3x2 block of
// space without collision.
func hasEnoughSpace(tx *world.Tx, p cube.Pos) bool {
	for x := 0; x <= 1; x++ {
		for y := 0; y <= 2; y++ {
			for z := 0; z <= 1; z++ {
				q := p.Add(cube.Pos{x, y, z})
				if len(boxesOf(tx, q, tx.Block(q))) > 0 {
					return false
				}
			}
		}
	}
	return true
}

// withoutTraderSpawns is #minecraft:without_wandering_trader_spawns
// (the_void).
func withoutTraderSpawns(tx *world.Tx, p cube.Pos) bool {
	return javaspawn.BiomeName(tx.Biome(p)) == "the_void"
}
