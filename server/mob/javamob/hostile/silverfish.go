//go:build crossfly_full

package hostile

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

const silverfishID = "minecraft:silverfish"

// SilverfishAttributes are Silverfish.createAttributes: health, speed,
// attack damage.
func SilverfishAttributes() (health, speed, attack float64) { return 8, 0.25, 1 }

// Silverfish is the per-mob state of a silverfish (Silverfish).
type Silverfish struct {
	wake silverfishWakeUpFriendsGoal
}

// silverfishOf returns the silverfish state of m.
func silverfishOf(m *mob.Mob) *Silverfish {
	return stateOf(m, func() *Silverfish { return &Silverfish{} })
}

// MobGriefing is the mobGriefing game rule (Crossfly has no game rules: on,
// as by default).
var MobGriefing = func(tx *world.Tx) bool { return true }

// DestroyBlock is Level.destroyBlock(pos, drop, breaker): the block breaks
// with its particles and drops (an infested block lets its silverfish out).
// The base sets it; the default breaks the block through its break handler.
var DestroyBlock = func(tx *world.Tx, pos cube.Pos, breaker world.Entity) {
	b := tx.Block(pos)
	tx.SetBlock(pos, nil, nil)
	tx.AddParticle(pos.Vec3Centre(), particle.BlockBreak{Block: b})
	if h, ok := b.(block.Breakable); ok {
		if fn := h.BreakInfo().BreakHandler; fn != nil {
			fn(pos, tx, nil)
		}
	}
}

// infestedByHost is InfestedBlock.infestedStateByHost: the infested block of
// a host block (stone, cobblestone, the four stone bricks, deepslate), or
// false if the block is no host (isCompatibleHostBlock).
func infestedByHost(b world.Block) (world.Block, bool) {
	switch h := b.(type) {
	case block.Stone:
		if !h.Smooth {
			return block.InfestedStone{}, true
		}
	case block.Cobblestone:
		if !h.Mossy {
			return block.InfestedCobblestone{}, true
		}
	case block.StoneBricks:
		return block.InfestedStoneBricks{Type: h.Type}, true
	case block.Deepslate:
		if h.Type == block.NormalDeepslate() {
			return block.InfestedDeepslate{Axis: h.Axis}, true
		}
	}
	return nil, false
}

// hostByInfested is InfestedBlock.hostStateByInfested.
func hostByInfested(b world.Block) (world.Block, bool) {
	switch h := b.(type) {
	case block.InfestedStone:
		return block.Stone{}, true
	case block.InfestedCobblestone:
		return block.Cobblestone{}, true
	case block.InfestedStoneBricks:
		return block.StoneBricks{Type: h.Type}, true
	case block.InfestedDeepslate:
		return block.Deepslate{Type: block.NormalDeepslate(), Axis: h.Axis}, true
	}
	return nil, false
}

// SilverfishHurt is the start of Silverfish.hurtServer: hurt by an entity (or
// by a damage type of #always_triggers_silverfish: magic), the silverfish
// calls its friends in a second.
func SilverfishHurt(m *mob.Mob, byEntityOrMagic bool) {
	if byEntityOrMagic {
		silverfishOf(m).wake.notifyHurt()
	}
}

// silverfishWakeUpFriendsGoal is Silverfish.SilverfishWakeUpFriendsGoal
// (priority 3, no flags): 20 ticks (halved) after a hurt, break the infested
// blocks around (10 sideways, 5 up and down, nearest first, each further
// one with a coin flip to stop), letting their silverfish out; without
// mobGriefing they turn back into their host blocks instead.
type silverfishWakeUpFriendsGoal struct {
	lookForFriends int
}

func (g *silverfishWakeUpFriendsGoal) notifyHurt() {
	if g.lookForFriends == 0 {
		g.lookForFriends = adjustedTickDelay(false, 20)
	}
}

func (g *silverfishWakeUpFriendsGoal) Flags() mob.ControlFlags { return 0 }

func (g *silverfishWakeUpFriendsGoal) CanUse(*mob.Mob) bool { return g.lookForFriends > 0 }

func (g *silverfishWakeUpFriendsGoal) Tick(m *mob.Mob) {
	g.lookForFriends--
	if g.lookForFriends > 0 {
		return
	}
	tx := m.Tx()
	r := m.Rand()
	base := blockPos(m.Pos())
	griefing := MobGriefing(tx)
	for _, y := range wakeOffsets(5) {
		for _, x := range wakeOffsets(10) {
			for _, z := range wakeOffsets(10) {
				at := base.Add(cube.Pos{x, y, z})
				b := tx.Block(at)
				host, ok := hostByInfested(b)
				if !ok {
					continue
				}
				if griefing {
					DestroyBlock(tx, at, m)
				} else {
					tx.SetBlock(at, host, nil)
				}
				if nextBool(r) {
					return
				}
			}
		}
	}
}

// wakeOffsets is the loop order of SilverfishWakeUpFriendsGoal.tick: 0, 1,
// -1, 2, -2, ... up to ±n.
func wakeOffsets(n int) []int {
	out := make([]int, 0, 2*n+1)
	for o := 0; o <= n && o >= -n; {
		out = append(out, o)
		if o <= 0 {
			o = 1 - o
		} else {
			o = -o
		}
	}
	return out
}

// mergePos is BlockPos.containing(x, y + 0.5, z).relative(dir).
func mergePos(m *mob.Mob, dir cube.Face) cube.Pos {
	return blockPos(m.Pos().Add(mgl64.Vec3{0, 0.5})).Side(dir)
}

// SpawnAnim is Mob.spawnAnim: the poof of particles (entity event 20); the
// base sets it.
var SpawnAnim = func(m *mob.Mob) {}

// SilverfishWalkTargetValue is Silverfish.getWalkTargetValue: 10 on top of a
// host block (it likes to stroll over stone), else the PathfinderMob value.
func SilverfishWalkTargetValue(tx *world.Tx, pos cube.Pos, fallback float64) float64 {
	if _, ok := infestedByHost(tx.Block(pos.Side(cube.FaceDown))); ok {
		return 10
	}
	return fallback
}

// silverfishSpawnRule is the part of Silverfish.checkSilverfishSpawnRules
// after checkAnyLightMonsterSpawnRules: from a spawner always, naturally
// only with no player within 5 blocks (Paper: of players that affect
// spawning).
func silverfishSpawnRule(fromSpawner, playerWithin5 bool) bool { return fromSpawner || !playerWithin5 }
