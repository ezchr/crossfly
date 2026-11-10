package jraid

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/google/uuid"
)

// PatrolConfig is Paper's entities.behavior.pillager-patrols world config.
type PatrolConfig struct {
	// Disable turns patrols off.
	Disable bool
	// SpawnChance is the chance a patrol spawns when the delay runs out (vanilla's 1 in 5).
	SpawnChance float64
	// DelayPerPlayer keeps the spawn delay per player instead of per world.
	DelayPerPlayer bool
	// DelayTicks is the delay between spawn attempts (plus up to a minute).
	DelayTicks int
	// StartDay is the first day patrols spawn on.
	StartDay int
	// StartPerPlayer counts StartDay by the player's play time instead of the world's days.
	StartPerPlayer bool
}

// DefaultPatrolConfig is Paper's default pillager-patrols config, which is vanilla's behaviour.
var DefaultPatrolConfig = PatrolConfig{SpawnChance: 0.2, DelayTicks: 12000, StartDay: 5}

// PatrolLevel is what the patrol spawner needs of a level, on top of Level.
type PatrolLevel interface {
	Level
	// SpawnPatrols is the spawn_patrols game rule.
	SpawnPatrols() bool
	// BrightOutside is Level.isBrightOutside: daytime, the sky not darkened (by rain or night) enough.
	BrightOutside() bool
	// OverworldDays is the period count of the overworld day timeline: the days that passed.
	OverworldDays() int64
	// PlayTime is the play time of the player in ticks (Stats.PLAY_TIME).
	PlayTime(p Player) int64
	// CanPillagerPatrolSpawn is the minecraft:gameplay/can_pillager_patrol_spawn environment attribute at pos: off
	// in mushroom fields and in the first five days (the early_game timeline).
	CanPillagerPatrolSpawn(pos cube.Pos) bool
	// MotionBlockingNoLeavesY is the y of Heightmap.Types.MOTION_BLOCKING_NO_LEAVES at x, z.
	MotionBlockingNoLeavesY(x, z int) int
	// PillagerSpawnPositionOK is NaturalSpawner.isValidEmptySpawnBlock for a pillager at pos with
	// PatrollingMonster.checkPatrollingMonsterSpawnRules: block light at most 8 and the monster spawn rules of
	// any light (Monster.checkAnyLightMonsterSpawnRules).
	PillagerSpawnPositionOK(pos cube.Pos) bool
	// SpawnPatrolMember creates a pillager (EntitySpawnReason.PATROL) at pos. A leader is made patrol leader and
	// finds its patrol target before it is finalized and added with its passengers (PatrolSpawner.spawnPatrolMember).
	SpawnPatrolMember(pos cube.Pos, leader bool) bool
}

// PatrolSpawner is net.minecraft.world.level.levelgen.PatrolSpawner with Paper's pillager-patrols config.
type PatrolSpawner struct {
	Config      PatrolConfig
	nextTick    int
	playerDelay map[uuid.UUID]int
}

// NewPatrolSpawner returns a patrol spawner with the config.
func NewPatrolSpawner(c PatrolConfig) *PatrolSpawner {
	return &PatrolSpawner{Config: c, playerDelay: map[uuid.UUID]int{}}
}

// Tick is PatrolSpawner.tick (Paper's): every 10 to 11 minutes, on day 5 or later in daylight, with a chance of
// 20%, a patrol of pillagers spawns 24 to 47 blocks from a random player who is not near a village. spawnEnemies is
// the level's spawn-monsters setting.
func (s *PatrolSpawner) Tick(l PatrolLevel, r *rand.Rand, spawnEnemies bool) {
	c := s.Config
	if c.Disable || c.SpawnChance == 0 || !spawnEnemies || !l.SpawnPatrols() {
		return
	}
	players := l.Players()
	if len(players) < 1 {
		return
	}
	p := players[r.IntN(len(players))]
	if p.Spectator() {
		return
	}
	var delay int
	if c.DelayPerPlayer {
		s.playerDelay[p.UUID()]--
		delay = s.playerDelay[p.UUID()]
	} else {
		s.nextTick--
		delay = s.nextTick
	}
	if delay > 0 {
		return
	}
	var days int64
	if c.StartPerPlayer {
		days = l.PlayTime(p) / 24000
	} else {
		days = l.OverworldDays()
	}
	if c.DelayPerPlayer {
		s.playerDelay[p.UUID()] += c.DelayTicks + r.IntN(1200)
	} else {
		s.nextTick += c.DelayTicks + r.IntN(1200)
	}
	if days < int64(c.StartDay) || !l.BrightOutside() || r.Float64() >= c.SpawnChance || p.Spectator() ||
		IsCloseToVillage(l, p.BlockPos(), 2) {
		return
	}
	x := (24 + r.IntN(24)) * sign(r.IntN(2) == 0)
	z := (24 + r.IntN(24)) * sign(r.IntN(2) == 0)
	pos := p.BlockPos().Add(cube.Pos{x, 0, z})
	const delta = 10
	if !l.ChunksLoaded(pos[0]-delta, pos[2]-delta, pos[0]+delta, pos[2]+delta) || !l.CanPillagerPatrolSpawn(pos) {
		return
	}
	groupSize := int(math.Ceil(float64(l.DifficultyAt(pos).Effective))) + 1
	for i := 0; i < groupSize; i++ {
		pos[1] = l.MotionBlockingNoLeavesY(pos[0], pos[2])
		if i == 0 {
			if !spawnPatrolMember(l, pos, true) {
				break
			}
		} else {
			spawnPatrolMember(l, pos, false)
		}
		pos[0] += r.IntN(5) - r.IntN(5)
		pos[2] += r.IntN(5) - r.IntN(5)
	}
}

// spawnPatrolMember is PatrolSpawner.spawnPatrolMember.
func spawnPatrolMember(l PatrolLevel, pos cube.Pos, leader bool) bool {
	if !l.PillagerSpawnPositionOK(pos) {
		return false
	}
	return l.SpawnPatrolMember(pos, leader)
}

// sign is random.nextBoolean() ? -1 : 1.
func sign(b bool) int {
	if b {
		return -1
	}
	return 1
}
