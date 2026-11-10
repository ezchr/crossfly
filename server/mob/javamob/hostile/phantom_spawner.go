//go:build crossfly_full

package hostile

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// Paper's phantom spawning settings, at their defaults (entities.behavior in
// paper-world-defaults.yml): attempts every 60..119 s, players start to
// count as insomniac after 72000 ticks, creative players get none.
const (
	phantomSpawnAttemptMinSeconds  = 60
	phantomSpawnAttemptMaxSeconds  = 119
	phantomsDoNotSpawnOnCreative   = true
	phantomSpawnInsomniaStartTicks = insomniacStartTicks
)

// PhantomSpawner is PhantomSpawner (Paper's version) for one dimension: a
// CustomSpawner the server ticks once a world tick.
type PhantomSpawner struct {
	nextTick int
	// HasSkyLight is DimensionType.hasSkyLight (the overworld).
	HasSkyLight bool
	// Spawn adds one phantom at pos for the player (it is finalised with the
	// local difficulty; the group shares spawn group data, which phantoms
	// do not use).
	Spawn func(tx *world.Tx, pos cube.Pos, p *player.Player)
	// ValidEmptySpawnBlock is NaturalSpawner.isValidEmptySpawnBlock for
	// phantoms; nil uses validEmptySpawnBlock.
	ValidEmptySpawnBlock func(tx *world.Tx, pos cube.Pos) bool
}

// phantomSpawnPlan is one spawn decided by Tick, for tests.
type phantomSpawnPlan struct {
	pos   cube.Pos
	count int
	p     *player.Player
}

// Tick is PhantomSpawner.tick with spawnEnemies (monsters may spawn: not on
// peaceful) and the doInsomnia game rule on.
func (s *PhantomSpawner) Tick(tx *world.Tx, r *rand.Rand, spawnEnemies bool) {
	for _, plan := range s.plan(tx, r, spawnEnemies) {
		for i := 0; i < plan.count; i++ {
			if s.Spawn != nil {
				s.Spawn(tx, plan.pos, plan.p)
			}
		}
	}
}

func (s *PhantomSpawner) plan(tx *world.Tx, r *rand.Rand, spawnEnemies bool) []phantomSpawnPlan {
	if !spawnEnemies {
		return nil
	}
	s.nextTick--
	if s.nextTick > 0 {
		return nil
	}
	s.nextTick += (phantomSpawnAttemptMinSeconds + nextInt(r, phantomSpawnAttemptMaxSeconds-phantomSpawnAttemptMinSeconds+1)) * 20
	if skyDarken(tx) < 5 && s.HasSkyLight {
		return nil
	}
	var out []phantomSpawnPlan
	for e := range tx.Players() {
		p, ok := e.(*player.Player)
		if !ok {
			continue
		}
		gm := p.GameMode()
		if gm == world.GameModeSpectator || (phantomsDoNotSpawnOnCreative && gm == world.GameModeCreative) {
			continue
		}
		pos := blockPos(p.Position())
		if s.HasSkyLight && (pos[1] < seaLevel || !canSeeSky(tx, pos)) {
			continue
		}
		diff := difficultyAt(tx, pos)
		if !diff.harderThan(nextFloat(r) * 3) {
			continue
		}
		rest := int(min(max(p.TimeSinceRest(), 1), math.MaxInt32))
		if nextInt(r, rest) < phantomSpawnInsomniaStartTicks {
			continue
		}
		at := pos.Add(cube.Pos{0, 20 + nextInt(r, 15), 0})
		at[0] += -10 + nextInt(r, 21)
		at[2] += -10 + nextInt(r, 21)
		valid := s.ValidEmptySpawnBlock
		if valid == nil {
			valid = validEmptySpawnBlock
		}
		if !valid(tx, at) {
			continue
		}
		out = append(out, phantomSpawnPlan{pos: at, count: 1 + nextInt(r, diff.base+1), p: p})
	}
	return out
}

// canSeeSky is Level.canSeeSky: nothing that blocks light above pos.
func canSeeSky(tx *world.Tx, pos cube.Pos) bool {
	return pos[1] > tx.HighestLightBlocker(pos[0], pos[2])
}

// validEmptySpawnBlock is an approximation of NaturalSpawner.
// isValidEmptySpawnBlock for a phantom: an air block (no full collision, no
// signal source, no liquid, nothing that hurts).
func validEmptySpawnBlock(tx *world.Tx, pos cube.Pos) bool { return isAir(tx, pos) }

// skyDarken is Level.getSkyDarken: 0 at noon, 11 at night, rain and thunder
// darkening it (the 26.3 sky light level timeline of the overworld follows
// the same curve).
func skyDarken(tx *world.Tx) int {
	w := tx.World()
	t := timeOfDay(w.Time())
	f := 1 - (math.Cos(t*2*math.Pi)*2 + 0.5)
	f = clamp(f, 0, 1)
	f = 1 - f
	rain, thunder := w.WeatherIntensity() // Level.getRainLevel/getThunderLevel
	f *= 1 - float64(rain)*5/16
	f *= 1 - float64(thunder)*5/16
	f = 1 - f
	return int(f * 11)
}

// timeOfDay is DimensionType.timeOfDay: the sun angle (0 at noon... 0.5 at
// midnight) of a day time.
func timeOfDay(dayTime int) float64 {
	frac := float64(dayTime)/24000 - 0.25
	frac -= math.Floor(frac)
	angle := 0.5 - math.Cos(frac*math.Pi)/2
	return (frac*2 + angle) / 3
}
