//go:build crossfly_full

package jraider

import (
	"math/rand/v2"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/village"
	"github.com/df-mc/dragonfly/server/mob/javaspawn"
	"github.com/df-mc/dragonfly/server/mob/jpoi"
	"github.com/df-mc/dragonfly/server/mob/jraid"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/bossbar"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// worldState is ServerLevel's raid side: the level's Raids (raids.dat), its
// PatrolSpawner and the bells that are ringing.
type worldState struct {
	raids  *jraid.Raids
	patrol *jraid.PatrolSpawner
	rand   *rand.Rand
	bells  map[cube.Pos]*jraid.Bell
	// omen are the players that had raid omen at the last tick.
	omen map[uuid.UUID]bool
	// refs are the raiders of the level by UUID.
	refs map[uuid.UUID]*Raider
	// tx is the transaction of the world while this package runs in it.
	tx *world.Tx
}

var states sync.Map // *world.World -> *worldState

func stateOf(w *world.World) *worldState {
	if v, ok := states.Load(w); ok {
		return v.(*worldState)
	}
	seed := uint64(time.Now().UnixNano())
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	st := &worldState{raids: jraid.NewRaids(r), patrol: jraid.NewPatrolSpawner(jraid.DefaultPatrolConfig), rand: r,
		bells: map[cube.Pos]*jraid.Bell{}, omen: map[uuid.UUID]bool{}, refs: map[uuid.UUID]*Raider{}}
	v, _ := states.LoadOrStore(w, st)
	return v.(*worldState)
}

// Raids returns the raids of the world (ServerLevel.getRaids).
func Raids(w *world.World) *jraid.Raids { return stateOf(w).raids }

// levelOf binds the state of tx's world to tx and returns its Level.
func levelOf(tx *world.Tx) level {
	st := stateOf(tx.World())
	st.tx = tx
	return level{tx: tx, st: st}
}

// level is jraid.Level (and PatrolLevel, BellLevel) on a world transaction.
type level struct {
	tx *world.Tx
	st *worldState
}

func (l level) Difficulty() jraid.Difficulty {
	switch l.tx.World().Difficulty() {
	case world.DifficultyPeaceful:
		return jraid.Peaceful
	case world.DifficultyEasy:
		return jraid.Easy
	case world.DifficultyHard:
		return jraid.Hard
	}
	return jraid.Normal
}

// moonBrightness is DimensionType.MOON_BRIGHTNESS_PER_PHASE.
var moonBrightness = [8]float32{1, 0.75, 0.5, 0.25, 0, 0.25, 0.5, 0.75}

// DifficultyAt is ServerLevel.getCurrentDifficultyAt. Dragonfly keeps no chunk inhabited time, so that part is 0.
func (l level) DifficultyAt(cube.Pos) jraid.DifficultyInstance {
	t := int64(l.tx.World().Time())
	return jraid.NewDifficultyInstance(l.Difficulty(), t, 0, moonBrightness[(t/24000)%8])
}

func (l level) GameTime() int64 { return l.tx.CurrentTick() }

// RaidsEnabled and MobGriefing: Dragonfly has no game rules, both are on.
func (l level) RaidsEnabled() bool { return true }
func (l level) MobGriefing() bool  { return true }

// CanStartRaid is the can_start_raid environment attribute: false in the nether.
func (l level) CanStartRaid(cube.Pos) bool { return l.tx.World().Dimension() != world.Nether }

func (l level) SectionsToVillage(pos cube.Pos) int {
	return jpoi.For(l.tx).SectionsToVillage(l.tx, pos)
}

func (l level) OccupiedVillagePOI(pos cube.Pos, radius int) []cube.Pos {
	recs := jpoi.For(l.tx).InRange(l.tx, jpoi.IsVillage, pos, radius, jpoi.IsOccupied)
	out := make([]cube.Pos, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.Pos)
	}
	return out
}

func (l level) ChunkLoaded(pos cube.Pos) bool {
	_, ok := l.tx.BlockLoaded(pos)
	return ok
}

func (l level) ChunksLoaded(minX, minZ, maxX, maxZ int) bool {
	for x := minX >> 4; x <= maxX>>4; x++ {
		for z := minZ >> 4; z <= maxZ>>4; z++ {
			if !l.ChunkLoaded(cube.Pos{x << 4, 64, z << 4}) {
				return false
			}
		}
	}
	return true
}

func (l level) EntityTicking(pos cube.Pos) bool { return l.ChunkLoaded(pos) }

func (l level) SurfaceY(x, z int) int { return l.tx.HighestBlock(x, z) + 1 }

func (l level) RavagerSpawnPositionOK(pos cube.Pos) bool {
	if javaspawn.IsSpawnPositionOk(l.tx, pos, "minecraft:ravager") {
		return true
	}
	below, _ := l.tx.Block(pos.Side(cube.FaceDown)).EncodeBlock()
	at, _ := l.tx.Block(pos).EncodeBlock()
	return below == "minecraft:snow_layer" && at == "minecraft:air"
}

func (l level) NewRaider(k jraid.Kind) jraid.Raider {
	if _, ok := bedrockID[k]; !ok {
		return nil
	}
	return newPendingRaider(k, l)
}

func (l level) Players() []jraid.Player {
	var out []jraid.Player
	for e := range l.tx.Players() {
		if p, ok := e.(*player.Player); ok {
			out = append(out, playerRef{h: p.H(), st: l.st})
		}
	}
	return out
}

func (l level) Hero(id uuid.UUID) (jraid.Hero, bool) {
	for e := range l.tx.Players() {
		if e.H().UUID() == id {
			return playerRef{h: e.H(), st: l.st}, true
		}
	}
	return nil, false
}

func (l level) EntityLoaded(id uuid.UUID) bool {
	r, ok := l.st.refs[id]
	if !ok || r.h == nil {
		return false
	}
	_, ok = r.h.Entity(l.tx)
	return ok
}

func (l level) SameLevel(r jraid.Raider) bool {
	rr, ok := r.(*Raider)
	return ok && rr.w == l.tx.World()
}

// PatrolLevel.

// SpawnPatrols: the spawn_patrols game rule (on).
func (l level) SpawnPatrols() bool { return true }

// BrightOutside is Level.isBrightOutside: daytime (the rain darkening is not read).
func (l level) BrightOutside() bool {
	t := l.tx.World().Time() % 24000
	return t < 12000
}

func (l level) OverworldDays() int64 { return int64(l.tx.World().Time() / 24000) }

// PlayTime: Dragonfly keeps no play time statistic; the world time stands in (Paper's per-player start is off by
// default).
func (l level) PlayTime(jraid.Player) int64 { return int64(l.tx.World().Time()) }

// CanPillagerPatrolSpawn is the can_pillager_patrol_spawn attribute: off in mushroom fields.
func (l level) CanPillagerPatrolSpawn(pos cube.Pos) bool {
	return javaspawn.BiomeName(l.tx.Biome(pos)) != "mushroom_fields"
}

func (l level) MotionBlockingNoLeavesY(x, z int) int { return motionBlockingNoLeaves(l.tx, x, z) }

func (l level) PillagerSpawnPositionOK(pos cube.Pos) bool {
	if l.tx.Light(pos) > 8 {
		return false
	}
	return javaspawn.IsValidEmptySpawnBlock(l.tx, pos, "minecraft:pillager") &&
		javaspawn.IsValidSpawn(l.tx, pos.Side(cube.FaceDown), "minecraft:pillager")
}

func (l level) SpawnPatrolMember(pos cube.Pos, leader bool) bool {
	return spawnPatrolMember(l, pos, leader)
}

// BellLevel.

func (l level) LivingEntitiesWithin(box cube.BBox) []jraid.BellEntity {
	var out []jraid.BellEntity
	for e := range l.tx.EntitiesWithin(box) {
		if _, ok := e.(entity.Living); ok {
			out = append(out, bellEntity{h: e.H(), st: l.st})
		}
	}
	return out
}

func (l level) PlayResonate(pos cube.Pos) {
	l.tx.PlaySound(pos.Vec3Centre(), mob.Sound{Event: "block.bell.hit"})
}

// motionBlockingNoLeaves is the MOTION_BLOCKING_NO_LEAVES heightmap: the y above the highest block that blocks
// motion or holds a fluid, leaves not counted.
func motionBlockingNoLeaves(tx *world.Tx, x, z int) int {
	y := tx.HighestBlock(x, z)
	min := tx.Range()[0]
	for ; y >= min; y-- {
		pos := cube.Pos{x, y, z}
		b := tx.Block(pos)
		name, _ := b.EncodeBlock()
		if len(name) > 7 && (name[len(name)-7:] == "_leaves" || name == "minecraft:air") {
			continue
		}
		if _, ok := tx.Liquid(pos); ok {
			return y + 1
		}
		if len(b.Model().BBox(pos, tx)) > 0 {
			return y + 1
		}
	}
	return min
}

// playerRef is a player as raids see it, opened in the world's current transaction on each call.
type playerRef struct {
	h  *world.EntityHandle
	st *worldState
}

func (p playerRef) p() (*player.Player, bool) {
	if p.st.tx == nil {
		return nil, false
	}
	e, ok := p.h.Entity(p.st.tx)
	if !ok {
		return nil, false
	}
	pl, ok := e.(*player.Player)
	return pl, ok
}

func (p playerRef) UUID() uuid.UUID { return p.h.UUID() }
func (p playerRef) Alive() bool {
	pl, ok := p.p()
	return ok && !pl.Dead()
}
func (p playerRef) Spectator() bool {
	pl, ok := p.p()
	return ok && !pl.GameMode().Visible()
}
func (p playerRef) BlockPos() cube.Pos { return cube.PosFromVec3(p.Position()) }
func (p playerRef) Position() mgl64.Vec3 {
	if pl, ok := p.p(); ok {
		return pl.Position()
	}
	return mgl64.Vec3{}
}

// barTitle is the text of the raid bar (event.minecraft.raid*).
func barTitle(b jraid.Bar) string {
	switch b.Text {
	case jraid.BarRaidersRemaining:
		return "Raid - Raiders Remaining: " + itoa(b.Remaining)
	case jraid.BarVictory:
		return "Raid - Victory"
	case jraid.BarDefeat:
		return "Raid - Defeat"
	}
	return "Raid"
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func (p playerRef) ShowRaidBar(_ uuid.UUID, b jraid.Bar) {
	pl, ok := p.p()
	if !ok {
		return
	}
	if !b.Visible {
		pl.RemoveBossBar()
		return
	}
	pl.SendBossBar(bossbar.New(barTitle(b)).WithHealthPercentage(float64(b.Progress)).WithColour(bossbar.Red()))
}

func (p playerRef) HideRaidBar(uuid.UUID) {
	if pl, ok := p.p(); ok {
		pl.RemoveBossBar()
	}
}

func (p playerRef) PlayRaidHorn(mgl64.Vec3) {
	if pl, ok := p.p(); ok {
		pl.PlaySound(mob.Sound{Event: "raid.horn"})
	}
}

func (p playerRef) RaidOmen() (int, bool) {
	pl, ok := p.p()
	if !ok {
		return 0, false
	}
	e, ok := omen(pl, village.RaidOmen)
	if !ok {
		return 0, false
	}
	return e.Level() - 1, true
}

func ticks(n int) time.Duration { return time.Duration(n) * time.Second / 20 }

func (p playerRef) AddRaidOmen(amplifier, n int) {
	if pl, ok := p.p(); ok {
		// The omens of package village share one Go type, which Dragonfly keys effects by: raid omen replaces
		// bad omen (BadOmenMobEffect removes it right after anyway).
		pl.RemoveEffect(village.BadOmen)
		pl.AddEffect(effect.New(village.RaidOmen, amplifier+1, ticks(n)))
	}
}

func (p playerRef) RemoveBadOmen() {
	if pl, ok := p.p(); ok && hasOmen(pl, village.BadOmen) {
		pl.RemoveEffect(village.BadOmen)
	}
}

func (p playerRef) RemoveRaidOmen() {
	if pl, ok := p.p(); ok && hasOmen(pl, village.RaidOmen) {
		pl.RemoveEffect(village.RaidOmen)
	}
}

func (p playerRef) AddHeroOfTheVillage(amplifier, n int) {
	if pl, ok := p.p(); ok {
		pl.AddEffect(effect.New(village.HeroOfTheVillage, amplifier+1, ticks(n)).WithoutParticles())
	}
}

// hasOmen reports whether the player has exactly the omen t (the omens share their Go type, so Player.Effect
// finds any of them).
func hasOmen(p *player.Player, t effect.Type) bool {
	_, ok := omen(p, t)
	return ok
}

// omen returns the player's omen of type t.
func omen(p *player.Player, t effect.Type) (effect.Effect, bool) {
	e, ok := p.Effect(t)
	if !ok || e.Type() != t {
		return effect.Effect{}, false
	}
	return e, true
}

// bellEntity is a living entity near a ringing bell.
type bellEntity struct {
	h  *world.EntityHandle
	st *worldState
}

func (b bellEntity) e() (world.Entity, bool) {
	if b.st.tx == nil {
		return nil, false
	}
	return b.h.Entity(b.st.tx)
}

func (b bellEntity) Alive() bool {
	e, ok := b.e()
	if !ok {
		return false
	}
	l, ok := e.(entity.Living)
	return ok && !l.Dead()
}
func (b bellEntity) Removed() bool { _, ok := b.e(); return !ok }
func (b bellEntity) Position() mgl64.Vec3 {
	if e, ok := b.e(); ok {
		return e.Position()
	}
	return mgl64.Vec3{}
}
func (b bellEntity) IsRaider() bool {
	e, ok := b.e()
	return ok && isRaiderEntity(e)
}

// HearBell sets HEARD_BELL_TIME of villagers (their brain reacts by hiding during a raid).
func (b bellEntity) HearBell(gameTime int64) {
	if e, ok := b.e(); ok {
		hearBell(e, gameTime)
	}
}

// Glow is the glowing effect a resonating bell gives raiders. Neither client shows glowing for mobs yet (no
// Bedrock flag, the Java viewer's glowing bit needs a javasession change), so only the timer is kept.
func (b bellEntity) Glow(n int) {
	if e, ok := b.e(); ok {
		if r, ok := raiderOf(e); ok {
			r.glowTicks = n
		}
	}
}
