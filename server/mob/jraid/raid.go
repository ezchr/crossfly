package jraid

import (
	"math"
	"math/rand/v2"
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// Raid constants (net.minecraft.world.entity.raid.Raid).
const (
	allowSpawningWithinVillageSeconds = 7
	sectionRadiusForNewVillageCenter  = 2
	raidTimeoutTicks                  = 48000
	numSpawnAttempts                  = 5
	postRaidTickLimit                 = 40
	defaultPreRaidTicks               = 300
	// MaxNoActionTime is Raid.MAX_NO_ACTION_TIME: raiders idle longer than this outside the village leave the raid.
	MaxNoActionTime = 2400
	// MaxCelebrationTicks is Raid.MAX_CELEBRATION_TICKS.
	MaxCelebrationTicks     = 600
	outsideRaidBoundsTimout = 30
	lowMobThreshold         = 2
	// HeroOfTheVillageDuration is Raid.HERO_OF_THE_VILLAGE_DURATION.
	HeroOfTheVillageDuration = 48000
	// ValidRaidRadiusSqr is Raid.VALID_RAID_RADIUS_SQR: ServerLevel.getRaidAt finds raids centred this close.
	ValidRaidRadiusSqr = 9216
	// RaidRemovalThresholdSqr is Raid.RAID_REMOVAL_THRESHOLD_SQR: raiders farther from the centre leave the raid.
	RaidRemovalThresholdSqr = 12544
	// VillageRadiusBuffer is Raid.VILLAGE_RADIUS_BUFFER.
	VillageRadiusBuffer = 16
)

// Status is Raid.RaidStatus.
type Status int

const (
	Ongoing Status = iota
	Victory
	Loss
	Stopped
)

func (s Status) String() string {
	switch s {
	case Ongoing:
		return "ongoing"
	case Victory:
		return "victory"
	case Loss:
		return "loss"
	case Stopped:
		return "stopped"
	}
	return ""
}

// BarText is the text of the raid's boss bar.
type BarText int

const (
	// BarRaid is event.minecraft.raid ("Raid").
	BarRaid BarText = iota
	// BarRaidersRemaining is event.minecraft.raid followed by " - " and event.minecraft.raid.raiders_remaining
	// ("Raiders Remaining: %s").
	BarRaidersRemaining
	// BarVictory is event.minecraft.raid.victory.full ("Raid - Victory").
	BarVictory
	// BarDefeat is event.minecraft.raid.defeat.full ("Raid - Defeat").
	BarDefeat
)

// Bar is the raid's boss bar (Raid.raidEvent, a red ServerBossEvent with 10 notches).
type Bar struct {
	Text      BarText
	Remaining int
	Progress  float32
	Visible   bool
}

// bossEvent is the ServerBossEvent of a raid: its state and the players that see it.
type bossEvent struct {
	id      uuid.UUID
	bar     Bar
	players []Player
	dirty   bool
}

func (b *bossEvent) setText(t BarText, remaining int) {
	if b.bar.Text != t || b.bar.Remaining != remaining {
		b.bar.Text, b.bar.Remaining, b.dirty = t, remaining, true
	}
}

func (b *bossEvent) setProgress(p float32) {
	if b.bar.Progress != p {
		b.bar.Progress, b.dirty = p, true
	}
}

func (b *bossEvent) setVisible(v bool) {
	if b.bar.Visible != v {
		b.bar.Visible, b.dirty = v, true
		for _, p := range b.players {
			if v {
				p.ShowRaidBar(b.id, b.bar)
			} else {
				p.HideRaidBar(b.id)
			}
		}
		b.dirty = false
	}
}

func (b *bossEvent) addPlayer(p Player) {
	b.players = append(b.players, p)
	if b.bar.Visible {
		p.ShowRaidBar(b.id, b.bar)
	}
}

func (b *bossEvent) removePlayer(p Player) {
	if i := slices.IndexFunc(b.players, func(o Player) bool { return o.UUID() == p.UUID() }); i >= 0 {
		b.players = slices.Delete(b.players, i, i+1)
		p.HideRaidBar(b.id)
	}
}

func (b *bossEvent) removeAll() {
	for _, p := range b.players {
		p.HideRaidBar(b.id)
	}
	b.players = nil
}

func (b *bossEvent) has(id uuid.UUID) bool {
	return slices.ContainsFunc(b.players, func(p Player) bool { return p.UUID() == id })
}

// flush sends a changed bar to its players (ServerBossEvent broadcasts each change).
func (b *bossEvent) flush() {
	if !b.dirty {
		return
	}
	b.dirty = false
	if !b.bar.Visible {
		return
	}
	for _, p := range b.players {
		p.ShowRaidBar(b.id, b.bar)
	}
}

// Raid is net.minecraft.world.entity.raid.Raid: a raid on a village.
type Raid struct {
	id int

	groupToLeader map[int]Raider
	groupRaiders  map[int][]Raider
	heroes        []uuid.UUID

	ticksActive     int64
	center          cube.Pos
	started         bool
	totalHealth     float32
	raidOmenLevel   int
	active          bool
	groupsSpawned   int
	postRaidTicks   int
	cooldownTicks   int
	numGroups       int
	status          Status
	celebrationTick int
	waveSpawnPos    *cube.Pos

	rand  *rand.Rand
	event bossEvent
}

// NewRaid is Raid(center, difficulty).
func NewRaid(center cube.Pos, d Difficulty, r *rand.Rand) *Raid {
	raid := &Raid{
		groupToLeader: map[int]Raider{},
		groupRaiders:  map[int][]Raider{},
		active:        true,
		cooldownTicks: defaultPreRaidTicks,
		center:        center,
		numGroups:     NumGroups(d),
		status:        Ongoing,
		rand:          r,
	}
	raid.event = bossEvent{id: uuidFrom(r), bar: Bar{Text: BarRaid, Visible: true}}
	return raid
}

func uuidFrom(r *rand.Rand) uuid.UUID {
	var u uuid.UUID
	for i := range u {
		u[i] = byte(r.Uint32())
	}
	u[6] = u[6]&0x0f | 0x40
	u[8] = u[8]&0x3f | 0x80
	return u
}

// ID is the raid's id in its Raids (Raids.getId), 0 before it is added.
func (r *Raid) ID() int { return r.id }

// Center is Raid.getCenter.
func (r *Raid) Center() cube.Pos { return r.center }

// IsOver is Raid.isOver: the raid was won or lost.
func (r *Raid) IsOver() bool { return r.status == Victory || r.status == Loss }

// IsBetweenWaves is Raid.isBetweenWaves.
func (r *Raid) IsBetweenWaves() bool {
	return r.HasFirstWaveSpawned() && r.TotalRaidersAlive() == 0 && r.cooldownTicks > 0
}

// HasFirstWaveSpawned is Raid.hasFirstWaveSpawned.
func (r *Raid) HasFirstWaveSpawned() bool { return r.groupsSpawned > 0 }

// Status is the raid's status.
func (r *Raid) Status() Status { return r.status }

// IsStopped, IsVictory, IsLoss and IsInProgress test the raid's status.
func (r *Raid) IsStopped() bool    { return r.status == Stopped }
func (r *Raid) IsVictory() bool    { return r.status == Victory }
func (r *Raid) IsLoss() bool       { return r.status == Loss }
func (r *Raid) IsInProgress() bool { return r.status == Ongoing }

// IsStarted is Raid.isStarted: its first wave spawned.
func (r *Raid) IsStarted() bool { return r.started }

// IsActive is Raid.isActive: its centre is loaded.
func (r *Raid) IsActive() bool { return r.active }

// GroupsSpawned is Raid.getGroupsSpawned: the waves spawned so far.
func (r *Raid) GroupsSpawned() int { return r.groupsSpawned }

// NumGroups is the raid's waves before its bonus wave.
func (r *Raid) NumGroups() int { return r.numGroups }

// RaidOmenLevel is Raid.getRaidOmenLevel.
func (r *Raid) RaidOmenLevel() int { return r.raidOmenLevel }

// SetRaidOmenLevel is Raid.setRaidOmenLevel.
func (r *Raid) SetRaidOmenLevel(v int) { r.raidOmenLevel = v }

// EnchantOdds is Raid.getEnchantOdds.
func (r *Raid) EnchantOdds() float32 { return EnchantOdds(r.raidOmenLevel) }

// TotalHealth is Raid.getTotalHealth.
func (r *Raid) TotalHealth() float32 { return r.totalHealth }

// Bar returns the raid's boss bar.
func (r *Raid) Bar() Bar { return r.event.bar }

// Heroes are the UUIDs of the heroes of the village: the players that killed a raider of the raid.
func (r *Raid) Heroes() []uuid.UUID { return slices.Clone(r.heroes) }

// AbsorbRaidOmen is Raid.absorbRaidOmen: the raid's omen level grows by the player's raid omen level.
func (r *Raid) AbsorbRaidOmen(p Player) bool {
	amp, ok := p.RaidOmen()
	if !ok {
		return false
	}
	r.raidOmenLevel = min(max(r.raidOmenLevel+amp+1, 0), MaxRaidOmenLevel)
	return true
}

// Stop is Raid.stop.
func (r *Raid) Stop() {
	r.active = false
	r.event.removeAll()
	r.status = Stopped
}

func (r *Raid) validPlayer(l Level, rs *Raids, p Player) bool {
	return p.Alive() && rs.RaidAt(p.BlockPos()) == r
}

// updatePlayers is Raid.updatePlayers: the players in the raid see its bar.
func (r *Raid) updatePlayers(l Level, rs *Raids) {
	var now []Player
	for _, p := range l.Players() {
		if r.validPlayer(l, rs, p) {
			now = append(now, p)
		}
	}
	for _, p := range now {
		if !r.event.has(p.UUID()) {
			r.event.addPlayer(p)
		}
	}
	for _, p := range slices.Clone(r.event.players) {
		if !slices.ContainsFunc(now, func(o Player) bool { return o.UUID() == p.UUID() }) {
			r.event.removePlayer(p)
		}
	}
}

// Tick is Raid.tick.
func (r *Raid) Tick(l Level, rs *Raids) {
	defer r.event.flush()
	if r.IsStopped() {
		return
	}
	if r.status == Ongoing {
		oldActive := r.active
		r.active = l.ChunkLoaded(r.center)
		if l.Difficulty() == Peaceful {
			r.Stop()
			return
		}
		if oldActive != r.active {
			r.event.setVisible(r.active)
		}
		if !r.active {
			return
		}
		if !IsVillage(l, r.center) {
			r.moveCenterToNearbyVillageSection(l)
		}
		if !IsVillage(l, r.center) {
			if r.groupsSpawned > 0 {
				r.status = Loss
			} else {
				r.Stop()
			}
		}
		r.ticksActive++
		if r.ticksActive >= raidTimeoutTicks {
			r.Stop()
			return
		}
		alive := r.TotalRaidersAlive()
		if alive == 0 && r.hasMoreWaves() {
			if r.cooldownTicks <= 0 {
				if r.cooldownTicks == 0 && r.groupsSpawned > 0 {
					r.cooldownTicks = defaultPreRaidTicks
					r.event.setText(BarRaid, 0)
					return
				}
			} else {
				hasCached := r.waveSpawnPos != nil
				shouldFind := !hasCached && r.cooldownTicks%5 == 0
				if hasCached && !l.EntityTicking(*r.waveSpawnPos) {
					shouldFind = true
				}
				if shouldFind {
					r.waveSpawnPos = r.findRandomSpawnPos(l, 8)
				}
				if r.cooldownTicks == defaultPreRaidTicks || r.cooldownTicks%20 == 0 {
					r.updatePlayers(l, rs)
				}
				r.cooldownTicks--
				r.event.setProgress(clampf(float32(defaultPreRaidTicks-r.cooldownTicks)/defaultPreRaidTicks, 0, 1))
			}
		}
		if r.ticksActive%20 == 0 {
			r.updatePlayers(l, rs)
			r.updateRaiders(l)
			if alive > 0 && alive <= lowMobThreshold {
				r.event.setText(BarRaidersRemaining, alive)
			} else {
				r.event.setText(BarRaid, 0)
			}
		}
		soundPlayed, attempt := false, 0
		for r.shouldSpawnGroup() {
			pos := r.waveSpawnPos
			if pos == nil {
				pos = r.findRandomSpawnPos(l, 20)
			}
			if pos != nil {
				r.started = true
				r.spawnGroup(l, *pos)
				if !soundPlayed {
					r.playSound(l, *pos)
					soundPlayed = true
				}
			} else {
				attempt++
			}
			if attempt > numSpawnAttempts {
				r.Stop()
				break
			}
		}
		if r.started && !r.hasMoreWaves() && alive == 0 {
			if r.postRaidTicks < postRaidTickLimit {
				r.postRaidTicks++
			} else {
				r.status = Victory
				for _, id := range r.heroes {
					if h, ok := l.Hero(id); ok && !h.Spectator() {
						h.AddHeroOfTheVillage(r.raidOmenLevel-1, HeroOfTheVillageDuration)
					}
				}
			}
		}
		rs.setDirty()
		return
	}
	if r.IsOver() {
		r.celebrationTick++
		if r.celebrationTick >= MaxCelebrationTicks {
			r.Stop()
			return
		}
		if r.celebrationTick%20 == 0 {
			r.updatePlayers(l, rs)
			r.event.setVisible(true)
			if r.IsVictory() {
				r.event.setProgress(0)
				r.event.setText(BarVictory, 0)
			} else {
				r.event.setText(BarDefeat, 0)
			}
		}
	}
}

// moveCenterToNearbyVillageSection is Raid.moveRaidCenterToNearbyVillageSection: the centre moves to the centre of
// the closest village sub chunk at most two sub chunks away.
func (r *Raid) moveCenterToNearbyVillageSection(l Level) {
	sx, sy, sz := r.center[0]>>4, r.center[1]>>4, r.center[2]>>4
	best, found := cube.Pos{}, false
	bestDist := math.MaxFloat64
	for x := sx - sectionRadiusForNewVillageCenter; x <= sx+sectionRadiusForNewVillageCenter; x++ {
		for y := sy - sectionRadiusForNewVillageCenter; y <= sy+sectionRadiusForNewVillageCenter; y++ {
			for z := sz - sectionRadiusForNewVillageCenter; z <= sz+sectionRadiusForNewVillageCenter; z++ {
				c := cube.Pos{x<<4 + 8, y<<4 + 8, z<<4 + 8}
				if !IsVillage(l, c) {
					continue
				}
				if d := distSqr(c, r.center); d < bestDist {
					best, bestDist, found = c, d, true
				}
			}
		}
	}
	if found {
		r.center = best
	}
}

// hasMoreWaves is Raid.hasMoreWaves.
func (r *Raid) hasMoreWaves() bool {
	if r.hasBonusWave() {
		return !r.hasSpawnedBonusWave()
	}
	return !r.isFinalWave()
}

func (r *Raid) isFinalWave() bool         { return r.groupsSpawned == r.numGroups }
func (r *Raid) hasBonusWave() bool        { return r.raidOmenLevel > 1 }
func (r *Raid) hasSpawnedBonusWave() bool { return r.groupsSpawned > r.numGroups }
func (r *Raid) shouldSpawnBonusGroup() bool {
	return r.isFinalWave() && r.TotalRaidersAlive() == 0 && r.hasBonusWave()
}

// updateRaiders is Raid.updateRaiders: raiders gone, in another dimension, far from the centre or idle outside the
// village leave the raid.
func (r *Raid) updateRaiders(l Level) {
	var remove []Raider
	for _, wave := range r.waves() {
		for _, raider := range r.groupRaiders[wave] {
			pos := raider.BlockPos()
			if raider.Removed() || !l.SameLevel(raider) || distSqr(r.center, pos) >= RaidRemovalThresholdSqr {
				remove = append(remove, raider)
				continue
			}
			if raider.TickCount() > 600 {
				if !l.EntityLoaded(raider.UUID()) {
					remove = append(remove, raider)
				}
				st := raider.RaiderState()
				if !IsVillage(l, pos) && raider.NoActionTime() > MaxNoActionTime {
					st.TicksOutsideRaid++
				}
				if st.TicksOutsideRaid >= outsideRaidBoundsTimout {
					remove = append(remove, raider)
				}
			}
		}
	}
	for _, raider := range remove {
		r.RemoveFromRaid(raider, true)
		if raider.RaiderState().PatrolLeader {
			r.RemoveLeader(raider.RaiderState().Wave)
		}
	}
}

// playSound is Raid.playSound: the raid horn, 13 blocks from each player towards the wave, for players within 64
// blocks (horizontally) of the wave or in the raid.
func (r *Raid) playSound(l Level, origin cube.Pos) {
	raidLoc := origin.Vec3Centre()
	for _, p := range l.Players() {
		pl := p.Position()
		dist := math.Sqrt((raidLoc[0]-pl[0])*(raidLoc[0]-pl[0]) + (raidLoc[2]-pl[2])*(raidLoc[2]-pl[2]))
		x3 := pl[0] + 13.0/dist*(raidLoc[0]-pl[0])
		z3 := pl[2] + 13.0/dist*(raidLoc[2]-pl[2])
		if dist <= 64 || r.event.has(p.UUID()) {
			p.PlayRaidHorn(mgl64.Vec3{x3, pl[1], z3})
		}
	}
}

// spawnGroup is Raid.spawnGroup.
func (r *Raid) spawnGroup(l Level, pos cube.Pos) {
	group := r.groupsSpawned + 1
	r.totalHealth = 0
	diff := l.DifficultyAt(pos)
	bonus := r.shouldSpawnBonusGroup()
	for _, m := range Composition(r.rand, diff.Base, group, r.numGroups, bonus) {
		raider := l.NewRaider(m.Kind)
		if raider == nil {
			continue
		}
		if m.Leader {
			raider.RaiderState().setPatrolLeader(true)
			r.SetLeader(group, raider)
		}
		r.JoinRaid(l, group, raider, &pos, false)
		if m.Rider != NoKind {
			if rider := l.NewRaider(m.Rider); rider != nil {
				r.JoinRaid(l, group, rider, &pos, false)
				rider.RideForRaid(raider, pos)
			}
		}
	}
	r.waveSpawnPos = nil
	r.groupsSpawned++
	r.UpdateBossbar()
}

// JoinRaid is Raid.joinRaid: the raider joins wave group. A new raider (exists false) is spawned at pos.
func (r *Raid) JoinRaid(l Level, group int, raider Raider, pos *cube.Pos, exists bool) {
	if !r.AddWaveMob(group, raider, true) {
		return
	}
	st := raider.RaiderState()
	st.raid = r
	st.Wave = group
	st.CanJoinRaid = true
	st.TicksOutsideRaid = 0
	if !exists && pos != nil {
		raider.SpawnForRaid(*pos, group)
	}
}

// UpdateBossbar is Raid.updateBossbar: the bar shows the health left of the wave.
func (r *Raid) UpdateBossbar() {
	p := float32(0)
	if r.totalHealth != 0 {
		p = r.HealthOfLivingRaiders() / r.totalHealth
	} else if h := r.HealthOfLivingRaiders(); h > 0 {
		p = 1
	}
	if p != p { // NaN: 0/0
		p = 0
	}
	r.event.setProgress(clampf(p, 0, 1))
}

// HealthOfLivingRaiders is Raid.getHealthOfLivingRaiders.
func (r *Raid) HealthOfLivingRaiders() float32 {
	h := float32(0)
	for _, rs := range r.groupRaiders {
		for _, raider := range rs {
			h += raider.Health()
		}
	}
	return h
}

func (r *Raid) shouldSpawnGroup() bool {
	return r.cooldownTicks == 0 && (r.groupsSpawned < r.numGroups || r.shouldSpawnBonusGroup()) && r.TotalRaidersAlive() == 0
}

// TotalRaidersAlive is Raid.getTotalRaidersAlive: the raiders still in the raid.
func (r *Raid) TotalRaidersAlive() int {
	n := 0
	for _, rs := range r.groupRaiders {
		n += len(rs)
	}
	return n
}

// Raiders is Raid.getRaiders.
func (r *Raid) Raiders() []Raider {
	var out []Raider
	for _, w := range r.waves() {
		out = append(out, r.groupRaiders[w]...)
	}
	return out
}

func (r *Raid) waves() []int {
	waves := make([]int, 0, len(r.groupRaiders))
	for w := range r.groupRaiders {
		waves = append(waves, w)
	}
	slices.Sort(waves)
	return waves
}

// RemoveFromRaid is Raid.removeFromRaid.
func (r *Raid) RemoveFromRaid(raider Raider, removeFromTotalHealth bool) {
	st := raider.RaiderState()
	rs := r.groupRaiders[st.Wave]
	i := slices.IndexFunc(rs, func(o Raider) bool { return o.UUID() == raider.UUID() })
	if i < 0 {
		return
	}
	r.groupRaiders[st.Wave] = slices.Delete(rs, i, i+1)
	if removeFromTotalHealth {
		r.totalHealth -= raider.Health()
	}
	st.raid = nil
	r.UpdateBossbar()
}

// AddWaveMob is Raid.addWaveMob: the raider is added to wave (a raider with the same UUID is replaced).
func (r *Raid) AddWaveMob(wave int, raider Raider, updateHealth bool) bool {
	rs := r.groupRaiders[wave]
	if i := slices.IndexFunc(rs, func(o Raider) bool { return o.UUID() == raider.UUID() }); i >= 0 {
		rs = slices.Delete(rs, i, i+1)
	}
	r.groupRaiders[wave] = append(rs, raider)
	if updateHealth {
		r.totalHealth += raider.Health()
	}
	r.UpdateBossbar()
	return true
}

// SetLeader is Raid.setLeader: the raider is the wave's captain and wears the ominous banner.
func (r *Raid) SetLeader(wave int, raider Raider) {
	r.groupToLeader[wave] = raider
	raider.SetCaptainBanner()
}

// Leader is Raid.getLeader.
func (r *Raid) Leader(wave int) (Raider, bool) {
	l, ok := r.groupToLeader[wave]
	return l, ok
}

// RemoveLeader is Raid.removeLeader.
func (r *Raid) RemoveLeader(wave int) { delete(r.groupToLeader, wave) }

// AddHeroOfTheVillage is Raid.addHeroOfTheVillage.
func (r *Raid) AddHeroOfTheVillage(id uuid.UUID) {
	if !slices.Contains(r.heroes, id) {
		r.heroes = append(r.heroes, id)
	}
}

// findRandomSpawnPos is Raid.findRandomSpawnPos: a surface position on a circle around the centre that shrinks as
// the pre-raid countdown runs out, outside the village until the last 7 seconds, with the area around loaded and
// room for a ravager (or snow).
func (r *Raid) findRandomSpawnPos(l Level, maxTries int) *cube.Pos {
	secondsRemaining := r.cooldownTicks / 20
	howFar := 0.22*float32(secondsRemaining) - 0.24
	startAngle := r.rand.Float32() * (math.Pi * 2)
	for i := 0; i < maxTries; i++ {
		angle := startAngle + math.Pi*float32(i)/8
		x := r.center[0] + floor32(cos32(angle)*32*howFar) + r.rand.IntN(3)*floor32(howFar)
		z := r.center[2] + floor32(sin32(angle)*32*howFar) + r.rand.IntN(3)*floor32(howFar)
		y := l.SurfaceY(x, z)
		if abs(y-r.center[1]) > 96 {
			continue
		}
		pos := cube.Pos{x, y, z}
		if IsVillage(l, pos) && secondsRemaining > allowSpawningWithinVillageSeconds {
			continue
		}
		const delta = 10
		if l.ChunksLoaded(x-delta, z-delta, x+delta, z+delta) && l.EntityTicking(pos) && l.RavagerSpawnPositionOK(pos) {
			return &pos
		}
	}
	return nil
}

func distSqr(a, b cube.Pos) float64 {
	dx, dy, dz := float64(a[0]-b[0]), float64(a[1]-b[1]), float64(a[2]-b[2])
	return dx*dx + dy*dy + dz*dz
}

func floor32(v float32) int {
	return int(math.Floor(float64(v)))
}

func cos32(v float32) float32 { return float32(math.Cos(float64(v))) }
func sin32(v float32) float32 { return float32(math.Sin(float64(v))) }

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
