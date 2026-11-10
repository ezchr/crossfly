package jraid

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// fakeLevel is a flat level (surface y 64) where every chunk is loaded, with a village of occupied points of
// interest.
type fakeLevel struct {
	difficulty  Difficulty
	time        int64
	raids       bool
	griefing    bool
	pois        []cube.Pos
	players     []*fakePlayer
	raiders     []*fakeRaider
	effective   float32
	spawnFails  bool
	patrols     bool
	bright      bool
	days        int64
	noPatrolsAt func(cube.Pos) bool
	patrol      []cube.Pos
	leaders     []bool
}

func newFakeLevel(d Difficulty) *fakeLevel {
	return &fakeLevel{difficulty: d, raids: true, griefing: true, effective: float32(d), patrols: true, bright: true}
}

func (l *fakeLevel) Difficulty() Difficulty { return l.difficulty }
func (l *fakeLevel) DifficultyAt(cube.Pos) DifficultyInstance {
	return DifficultyInstance{Base: l.difficulty, Effective: l.effective}
}
func (l *fakeLevel) GameTime() int64                { return l.time }
func (l *fakeLevel) RaidsEnabled() bool             { return l.raids }
func (l *fakeLevel) MobGriefing() bool              { return l.griefing }
func (l *fakeLevel) CanStartRaid(pos cube.Pos) bool { return true }
func (l *fakeLevel) SectionsToVillage(pos cube.Pos) int {
	best := 7
	for _, p := range l.pois {
		d := max(abs(p[0]>>4-pos[0]>>4), abs(p[1]>>4-pos[1]>>4), abs(p[2]>>4-pos[2]>>4))
		best = min(best, d)
	}
	return best
}
func (l *fakeLevel) OccupiedVillagePOI(pos cube.Pos, radius int) []cube.Pos {
	var out []cube.Pos
	for _, p := range l.pois {
		if distSqr(p, pos) <= float64(radius*radius) {
			out = append(out, p)
		}
	}
	return out
}
func (l *fakeLevel) ChunkLoaded(cube.Pos) bool            { return true }
func (l *fakeLevel) ChunksLoaded(_, _, _, _ int) bool     { return true }
func (l *fakeLevel) EntityTicking(cube.Pos) bool          { return true }
func (l *fakeLevel) SurfaceY(_, _ int) int                { return 64 }
func (l *fakeLevel) RavagerSpawnPositionOK(cube.Pos) bool { return !l.spawnFails }
func (l *fakeLevel) NewRaider(k Kind) Raider {
	r := &fakeRaider{kind: k, id: uuid.New(), health: maxHealth(k), alive: true}
	l.raiders = append(l.raiders, r)
	return r
}
func (l *fakeLevel) Players() []Player {
	out := make([]Player, len(l.players))
	for i, p := range l.players {
		out[i] = p
	}
	return out
}
func (l *fakeLevel) Hero(id uuid.UUID) (Hero, bool) {
	for _, p := range l.players {
		if p.id == id {
			return p, true
		}
	}
	return nil, false
}
func (l *fakeLevel) EntityLoaded(id uuid.UUID) bool {
	for _, r := range l.raiders {
		if r.id == id {
			return !r.removed
		}
	}
	return false
}
func (l *fakeLevel) SameLevel(Raider) bool { return true }

// PatrolLevel.
func (l *fakeLevel) SpawnPatrols() bool                   { return l.patrols }
func (l *fakeLevel) BrightOutside() bool                  { return l.bright }
func (l *fakeLevel) OverworldDays() int64                 { return l.days }
func (l *fakeLevel) PlayTime(Player) int64                { return 0 }
func (l *fakeLevel) MotionBlockingNoLeavesY(_, _ int) int { return 64 }
func (l *fakeLevel) CanPillagerPatrolSpawn(pos cube.Pos) bool {
	return l.noPatrolsAt == nil || !l.noPatrolsAt(pos)
}
func (l *fakeLevel) PillagerSpawnPositionOK(cube.Pos) bool { return true }
func (l *fakeLevel) SpawnPatrolMember(pos cube.Pos, leader bool) bool {
	l.patrol = append(l.patrol, pos)
	l.leaders = append(l.leaders, leader)
	return true
}

func maxHealth(k Kind) float32 {
	switch k {
	case Witch:
		return 26
	case Ravager:
		return 100
	}
	return 24
}

// alive returns the raiders of the level still alive.
func (l *fakeLevel) alive() []*fakeRaider {
	var out []*fakeRaider
	for _, r := range l.raiders {
		if r.alive {
			out = append(out, r)
		}
	}
	return out
}

// killAll kills every living raider, by the player killer (nil for none).
func (l *fakeLevel) killAll(killer *uuid.UUID) {
	for _, r := range l.alive() {
		r.alive, r.removed = false, true
		Die(r, killer)
	}
}

type fakeRaider struct {
	st        State
	kind      Kind
	id        uuid.UUID
	health    float32
	alive     bool
	removed   bool
	pos       cube.Pos
	ticks     int
	idle      int
	banner    bool
	spawned   bool
	wave      int
	vehicle   *fakeRaider
	celebrate bool
}

func (r *fakeRaider) RaiderState() *State { return &r.st }
func (r *fakeRaider) UUID() uuid.UUID     { return r.id }
func (r *fakeRaider) CanBeLeader() bool   { return r.kind.CanBeLeader() }
func (r *fakeRaider) Health() float32     { return r.health }
func (r *fakeRaider) Alive() bool         { return r.alive }
func (r *fakeRaider) Removed() bool       { return r.removed }
func (r *fakeRaider) BlockPos() cube.Pos  { return r.pos }
func (r *fakeRaider) TickCount() int      { return r.ticks }
func (r *fakeRaider) NoActionTime() int   { return r.idle }
func (r *fakeRaider) Celebrate(on bool)   { r.celebrate = on }
func (r *fakeRaider) SetCaptainBanner()   { r.banner = true }
func (r *fakeRaider) SpawnForRaid(pos cube.Pos, wave int) {
	r.pos, r.spawned, r.wave = pos.Add(cube.Pos{0, 1, 0}), true, wave
}
func (r *fakeRaider) RideForRaid(vehicle Raider, pos cube.Pos) {
	r.vehicle, r.pos = vehicle.(*fakeRaider), pos
}

type fakePlayer struct {
	id        uuid.UUID
	pos       cube.Pos
	spectator bool
	bars      map[uuid.UUID]Bar
	horns     int
	raidOmen  int
	hasRaid   bool
	badOmen   bool
	hero      int
	heroTicks int
}

func newFakePlayer(pos cube.Pos) *fakePlayer {
	return &fakePlayer{id: uuid.New(), pos: pos, bars: map[uuid.UUID]Bar{}, hero: -1}
}

func (p *fakePlayer) UUID() uuid.UUID                 { return p.id }
func (p *fakePlayer) Alive() bool                     { return true }
func (p *fakePlayer) Spectator() bool                 { return p.spectator }
func (p *fakePlayer) BlockPos() cube.Pos              { return p.pos }
func (p *fakePlayer) Position() mgl64.Vec3            { return p.pos.Vec3Middle() }
func (p *fakePlayer) ShowRaidBar(id uuid.UUID, b Bar) { p.bars[id] = b }
func (p *fakePlayer) HideRaidBar(id uuid.UUID)        { delete(p.bars, id) }
func (p *fakePlayer) PlayRaidHorn(mgl64.Vec3)         { p.horns++ }
func (p *fakePlayer) RaidOmen() (int, bool)           { return p.raidOmen, p.hasRaid }
func (p *fakePlayer) AddRaidOmen(amp, _ int)          { p.raidOmen, p.hasRaid = amp, true }
func (p *fakePlayer) RemoveBadOmen()                  { p.badOmen = false }
func (p *fakePlayer) RemoveRaidOmen()                 { p.hasRaid = false }
func (p *fakePlayer) AddHeroOfTheVillage(amp, ticks int) {
	p.hero, p.heroTicks = amp, ticks
}

func newRand(seed uint64) *rand.Rand { return rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)) }

// village is a small village: beds and job sites around (0, 64, 0).
func village() []cube.Pos {
	return []cube.Pos{{0, 64, 0}, {10, 64, 4}, {-8, 65, 12}, {6, 64, -10}}
}
