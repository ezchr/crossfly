//go:build crossfly_full

package spawn

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

const (
	phantomID = "minecraft:phantom"
	// ticksUntilInsomnia is InsomniaComponents threshold: player.jsons
	// minecraft:insomnia days_until_insomnia (3) x 24000
	// (InsomniaDefinition::initialize).
	ticksUntilInsomnia = 3 * 24000
	// playerEyeHeight: BDS Actor::getPosition of a player is its eye
	// position, which Phantom::checkSpawnRules measures from.
	playerEyeHeight = 1.62
)

// insomniaTick is InsomniaSystem::tick (tickInsomniaComponent) for every
// player of the world: the time since rest counts up each tick, except for
// spectators, and restarts while the player sleeps. Player::respawn
// restarts it after a death, which counting it as 0 while dead matches.
// The doinsomnia game rule is always on (Dragonfly has no game rules).
func insomniaTick(tx *world.Tx) {
	for e := range tx.Players() {
		p, ok := e.(*player.Player)
		if !ok {
			continue
		}
		if p.Dead() {
			p.SetTimeSinceRest(0)
			continue
		}
		if p.GameMode() == world.GameModeSpectator {
			continue
		}
		if _, sleeping := p.Sleeping(); sleeping {
			p.SetTimeSinceRest(0)
			continue
		}
		p.SetTimeSinceRest(p.TimeSinceRest() + 1)
	}
}

// actorSpawnRules are the Actor::checkSpawnRules overrides of mobs whose
// natural spawn position is only a starting point. They return where the
// mob spawns and whether it may; mobs without one use checkSpawnRules
// (Mob::checkSpawnRules).
var actorSpawnRules = map[string]func(e *env, t *mob.Type, pos mgl64.Vec3) (mgl64.Vec3, bool){
	phantomID: phantomSpawnRules,
}

// phantomSpawnRules is Phantom::checkSpawnRules(false) (a natural spawn):
// for each player of the dimension, in order (Dimension::forEachPlayer,
// which stops at the first player the callback returns false for), the
// phantom moves 20-34 blocks above a player that can see the sky, is less
// than 100 blocks away and has not rested for long enough
// (random(timeSinceRest) >= 72000), unless 5 phantoms are within 48
// blocks there; it then has to pass Monster::checkSpawnRules at the new
// position. The result is that of the last player that got that far. Only
// dimensions with sky light have phantoms.
func phantomSpawnRules(e *env, t *mob.Type, pos mgl64.Vec3) (mgl64.Vec3, bool) {
	tx := e.tx
	if d := tx.World().Dimension(); d == world.Nether || d == world.End {
		return pos, false
	}
	result := false
	for pe := range tx.Players() {
		p, ok := pe.(*player.Player)
		if !ok {
			break
		}
		eye := p.Position().Add(mgl64.Vec3{0, playerEyeHeight})
		eyeBlock := cube.PosFromVec3(eye)
		if eyeBlock[1] <= tx.HighestLightBlocker(eyeBlock[0], eyeBlock[2]) {
			break // BlockSource::canSeeSky
		}
		if eye.Sub(pos).LenSqr() >= 100*100 {
			break
		}
		if rest := p.TimeSinceRest(); rest <= 0 || e.rnd.IntN(int(min(rest, 1<<31-1))) < ticksUntilInsomnia {
			break
		}
		dx, dy, dz := e.rnd.IntN(21), e.rnd.IntN(15), e.rnd.IntN(21)
		at := cube.PosFromVec3(eye.Add(mgl64.Vec3{float64(dx - 10), float64(dy + 20), float64(dz - 10)}))
		if phantomsAround(tx, at.Vec3()) >= 5 {
			break
		}
		pos = at.Vec3()
		if result = monsterDarkEnough(e, at) && checkSpawnRules(tx, t, pos, at.Side(cube.FaceDown)); !result {
			break
		}
	}
	return pos, result
}

// phantomsAround counts the phantoms within 48 blocks (a box) of pos.
func phantomsAround(tx *world.Tx, pos mgl64.Vec3) int {
	n := 0
	box := cube.Box(pos[0], pos[1], pos[2], pos[0], pos[1], pos[2]).Grow(48)
	for e := range tx.EntitiesWithin(box) {
		if m, ok := mob.Of(e); ok && m.Identifier() == phantomID {
			n++
		}
	}
	return n
}
