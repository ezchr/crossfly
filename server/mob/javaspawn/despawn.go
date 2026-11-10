//go:build crossfly_full

package javaspawn

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// checkDespawn is Mob.checkDespawn with Paper's despawn ranges (defaults:
// hard = the category's despawn distance, soft 32, ellipsoid = sphere):
//
//   - on peaceful, mobs not allowed in peaceful are removed;
//   - with no player that affects spawning in the world, nothing happens;
//   - a mob that is not persistent and further than the hard range from the
//     nearest player is removed if removeWhenFarAway;
//   - beyond the soft range, after 600 ticks without action, 1 in 800 per
//     tick; within it, the no-action timer restarts.
func (st *worldState) checkDespawn(e *env, m Mob) {
	t, ok := entityTypes[m.JavaType()]
	if !ok {
		return
	}
	if e.peaceful() && !t.AllowedInPeaceful {
		st.discard(m, t)
		return
	}
	persistent := isPersistent(m)
	pos := m.Position()
	var nearest *playerInfo
	bestD := math.Inf(1)
	for _, p := range st.players {
		if d := p.pos.Sub(pos).LenSqr(); d < bestD {
			nearest, bestD = p, d
		}
	}
	if nearest == nil {
		return
	}
	hard, soft := e.cfg.despawnRanges(t.Category)
	if !persistent && bestD > hard*hard && removeWhenFarAway(m, t.ID, bestD, false) {
		st.discard(m, t)
		return
	}
	na, hasTimer := m.(noActionMob)
	if bestD > soft*soft {
		if !persistent && hasTimer && na.NoActionTime() > 600 && e.rnd.IntN(800) == 0 && removeWhenFarAway(m, t.ID, bestD, false) {
			st.discard(m, t)
		}
	} else if hasTimer {
		na.SetNoActionTime(0)
	}
}

// discard is Entity.discard(DESPAWN).
func (st *worldState) discard(m Mob, t *EntityType) {
	st.Stats.Despawned[t.Category]++
	if d, ok := m.(discardMob); ok {
		d.Discard()
		return
	}
	_ = m.Close()
}

// tickPhantoms is PhantomSpawner.tick with Paper's defaults (an attempt
// every 60..119 seconds, insomnia after 72000 ticks, none over creative
// players): when the sky is dark (sky darkening 5+) every survival player
// under open sky at or above sea level whose local difficulty beats
// rand(3) and who has not slept for long enough (rand(time since rest) >=
// 72000) gets 1..difficulty+1 phantoms 20..34 blocks above.
func (st *worldState) tickPhantoms(e *env) {
	if e.peaceful() || e.cfg.NoPhantoms {
		return
	}
	st.phantomNext--
	if st.phantomNext > 0 {
		return
	}
	st.phantomNext += (60 + e.rnd.IntN(119-60+1)) * 20
	if e.skyDarken < 5 && e.dim.hasSkyLight {
		return
	}
	t := entityTypes["minecraft:phantom"]
	if !e.reg.Has(t.ID) {
		return
	}
	for _, p := range st.players {
		if p.creative {
			continue
		}
		pp := cube.PosFromVec3(p.pos)
		if e.dim.hasSkyLight && (pp[1] < e.seaLevel || !e.canSeeSky(pp)) {
			continue
		}
		if !(localDifficulty(e.difficulty, e.dayTime) > e.rnd.Float32()*3) {
			continue
		}
		rest := int(min(max(p.p.TimeSinceRest(), 1), math.MaxInt32))
		if e.rnd.IntN(rest) < 72000 {
			continue
		}
		dy := 20 + e.rnd.IntN(15)
		dx := -10 + e.rnd.IntN(21)
		dz := -10 + e.rnd.IntN(21)
		sp := pp.Add(cube.Pos{dx, dy, dz})
		if !e.loaded(sp) || !isValidEmptySpawnBlock(e.tx, sp, t) {
			continue
		}
		var group any
		n := 1 + e.rnd.IntN(e.difficulty+1)
		for range n {
			m, g, ok := e.reg.Spawn(e.tx, t.ID, mgl64.Vec3{float64(sp[0]) + .5, float64(sp[1]), float64(sp[2]) + .5}, 0, group)
			if !ok || m == nil {
				break
			}
			group = g
			st.afterSpawn(e, m, t)
		}
	}
}

// localDifficulty is DifficultyInstance.calculateDifficulty without
// inhabited time (Crossfly does not track it): the base difficulty times
// 0.75, up to 0.25 more over the first 21 days and up to 0.25 more by the
// moon (halved on easy).
func localDifficulty(diff int, dayTime int) float32 {
	if diff == 0 {
		return 0
	}
	f := float32(.75)
	g := min(max((float32(dayTime)-72000)/1440000, 0), 1) * .25
	f += g
	moon := [8]float32{1, .75, .5, .25, 0, .25, .5, .75}[((dayTime/24000)%8+8)%8]
	h := min(max(moon*.25, 0), g)
	if diff == 1 {
		h *= .5
	}
	return float32(diff) * (f + h)
}

// insomniaTick counts the players' time since rest (Stats.TIME_SINCE_REST):
// up every tick while alive and not sleeping, back to 0 while sleeping or
// dead.
func insomniaTick(tx *world.Tx) {
	for ent := range tx.Players() {
		p, ok := ent.(*player.Player)
		if !ok {
			continue
		}
		if _, sleeping := p.Sleeping(); sleeping || p.Dead() {
			p.SetTimeSinceRest(0)
			continue
		}
		p.SetTimeSinceRest(p.TimeSinceRest() + 1)
	}
}
