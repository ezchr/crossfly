//go:build crossfly_full

package village

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/bossbar"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Raids (BDS Raid, Village::triggerRaid/_createRaid/tickRaid,
// RaidTriggerSystem, RaidOmenMobEffect, RaidBossSystem).

// RaidState is Raid's state.
type RaidState int

const (
	RaidPre       RaidState = iota // 300-tick warm-up
	RaidFindSpawn                  // looking for a spawn point
	RaidSpawn                      // spawning the wave
	RaidInPlay                     // the wave is alive
	RaidVictory
	RaidFinished
	RaidDone
)

// Raid constants (Village statics, read from the BDS 1.21.3 binary).
const (
	raidPrepTicks        = 300
	raidGroupDelay       = 40
	raidHelpDelay        = 600
	raidFinishedDelay    = 600
	raidMaxSpawnFailures = 5
	raidTimeout          = 48000
	raidBoundsPadding    = 32
	heroDuration         = 48000
	raidOmenDuration     = 30 * time.Second
	recruitRadius        = 16
)

// raidGroups are definitions/spawn_groups/raid_group_N.json of BDS 1.21.3:
// identifier, count and spawn event per wave.
var raidGroups = [][]raidEntry{
	{{"minecraft:pillager", 4, ""}},
	{{"minecraft:pillager", 3, ""}, {"minecraft:vindicator", 2, ""}},
	{{"minecraft:pillager", 3, ""}, {"minecraft:ravager", 1, ""}},
	{{"minecraft:witch", 3, ""}, {"minecraft:pillager", 3, ""}},
	{{"minecraft:vindicator", 4, ""}, {"minecraft:evocation_illager", 1, ""}, {"minecraft:ravager", 1, ""}, {"minecraft:ravager", 1, "minecraft:spawn_for_raid_with_pillager_rider"}},
	{{"minecraft:pillager", 5, ""}, {"minecraft:vindicator", 2, ""}, {"minecraft:evocation_illager", 1, ""}},
	{{"minecraft:vindicator", 6, ""}, {"minecraft:witch", 1, ""}, {"minecraft:evocation_illager", 2, ""}, {"minecraft:ravager", 1, "minecraft:spawn_for_raid_with_pillager_rider"}, {"minecraft:ravager", 1, "minecraft:spawn_for_raid_with_evoker_rider"}},
}

type raidEntry struct {
	id    string
	count int
	event string
}

// Raid is a village raid.
type Raid struct {
	v         *Village
	State     RaidState
	Status    int // 1 victory, 2 defeat
	Group     int
	NumGroups int
	ticks     int
	spawnPos  mgl64.Vec3
	raiders   []*world.EntityHandle
	numRaid   int
	maxHealth float64
	fails     int
	groupWait int
	helpWait  int
	inPlay    int
	heroes    []*world.EntityHandle
	attempt   int
}

// TriggerRaid is Village::triggerRaid: a raid with 3/5/7 waves by
// difficulty, unless there is one or the world is peaceful.
func (v *Village) TriggerRaid(tx *world.Tx) bool {
	if v.raid != nil {
		return false
	}
	n := 0
	switch tx.World().Difficulty() {
	case world.DifficultyEasy:
		n = 3
	case world.DifficultyNormal:
		n = 5
	case world.DifficultyHard:
		n = 7
	default:
		return false
	}
	v.raid = &Raid{v: v, NumGroups: n, groupWait: raidGroupDelay, helpWait: raidHelpDelay}
	return true
}

// Raiders returns the live raiders.
func (r *Raid) Raiders() []*world.EntityHandle { return slices.Clone(r.raiders) }

// Bounds is the raid area: the village bounds grown by 32.
func (r *Raid) Bounds() cube.BBox { return r.v.bounds.Grow(raidBoundsPadding) }

func (r *Raid) setState(s RaidState) {
	r.State, r.ticks, r.groupWait, r.helpWait = s, 0, raidGroupDelay, raidHelpDelay
}

func (r *Raid) addHero(p world.Entity) {
	if !slices.Contains(r.heroes, p.H()) {
		r.heroes = append(r.heroes, p.H())
	}
}

func (r *Raid) removeRaider(h *world.EntityHandle) {
	if i := slices.Index(r.raiders, h); i >= 0 {
		r.raiders = slices.Delete(r.raiders, i, i+1)
	}
}

// addRaider is Raid::addRaider.
func (r *Raid) addRaider(m *mob.Mob) {
	if slices.Contains(r.raiders, m.H()) {
		return
	}
	r.raiders = append(r.raiders, m.H())
	r.numRaid++
	r.maxHealth += m.MaxHealth()
}

// tickRaid is Village::tickRaid + Raid::tick.
func (v *Village) tickRaid(tx *world.Tx) {
	r := v.raid
	if r.State == RaidDone {
		r.end(tx)
		return
	}
	if r.State == RaidInPlay && r.inPlay > raidTimeout {
		for _, h := range r.raiders {
			if m, ok := mob.OfHandle(tx, h); ok {
				m.Fire("minecraft:raid_expired", nil)
			}
		}
		r.end(tx)
		return
	}
	if tx.World().Difficulty() == world.DifficultyPeaceful {
		for _, h := range r.raiders {
			if e, ok := h.Entity(tx); ok {
				_ = e.Close()
			}
		}
		r.end(tx)
		return
	}
	r.tick(tx)
	r.updateBars(tx)
}

func (r *Raid) tick(tx *world.Tx) {
	prev := r.ticks
	r.ticks++
	switch r.State {
	case RaidPre:
		if r.ticks >= raidPrepTicks {
			r.setState(RaidFindSpawn)
			r.attempt = 0
		}
	case RaidFindSpawn:
		pos, ok, abort := r.findSpawn(tx, r.attempt)
		r.attempt++
		if abort {
			r.setState(RaidDone)
		} else if ok {
			r.spawnPos = pos
			r.setState(RaidSpawn)
		}
	case RaidSpawn:
		if prev == 0 {
			return // _spawnRaidGroup returns false the first tick
		}
		before := len(r.raiders)
		r.spawnGroup(tx)
		if len(r.raiders) > before {
			r.numRaid = len(r.raiders)
			r.maxHealth = 0
			for _, h := range r.raiders {
				if m, ok := mob.OfHandle(tx, h); ok {
					r.maxHealth += m.Health()
				}
			}
			r.fails = 0
			r.horn(tx, r.spawnPos)
			r.setState(RaidInPlay)
			r.inPlay = 0
		} else {
			r.fails++
			if r.fails > raidMaxSpawnFailures {
				r.setState(RaidDone)
			} else {
				r.setState(RaidFindSpawn)
				r.attempt = 0
			}
		}
	case RaidInPlay:
		r.inPlay++
		r.tickInPlay(tx)
	case RaidVictory:
		for _, h := range r.heroes {
			if e, ok := h.Entity(tx); ok {
				if p, ok := e.(*player.Player); ok {
					p.AddEffect(effect.New(HeroOfTheVillage, 1, heroDuration*time.Second/20))
				}
			}
		}
		r.heroes = nil
		r.Status = 1
		r.setState(RaidFinished)
	case RaidFinished:
		if r.ticks >= raidFinishedDelay {
			r.setState(RaidDone)
		}
	}
}

// tickInPlay is Raid::_tickGroupInPlay.
func (r *Raid) tickInPlay(tx *world.Tx) {
	r.raiders = slices.DeleteFunc(r.raiders, func(h *world.EntityHandle) bool {
		if r.v.HasDweller(RoleHostile, h) {
			if e, ok := h.Entity(tx); ok && !dead(e) {
				return false
			}
		}
		if m, ok := mob.OfHandle(tx, h); ok && !m.Dead() {
			m.Fire("minecraft:raid_expired", nil)
		}
		r.helpWait = raidHelpDelay
		return true
	})
	if len(r.raiders) > 0 {
		r.recruit(tx)
		if r.helpWait--; r.helpWait <= 0 {
			r.helpWait = raidHelpDelay
			h := r.raiders[int(r.v.m.now)%len(r.raiders)]
			if e, ok := h.Entity(tx); ok {
				r.horn(tx, e.Position())
			}
		}
	} else {
		if r.groupWait--; r.groupWait < 0 {
			r.Group++
			if r.Group >= r.NumGroups {
				r.setState(RaidVictory)
				r.victory(tx)
			} else {
				r.setState(RaidPre)
			}
		}
	}
	if r.State == RaidInPlay && (r.v.BedCount() == 0 || r.v.Villagers() == 0) {
		r.Status = 2
		r.setState(RaidFinished)
		for _, h := range r.raiders {
			if m, ok := mob.OfHandle(tx, h); ok {
				m.Fire("minecraft:start_celebrating", nil)
			}
		}
	}
}

// victory is the onVictory hook: villagers celebrate.
func (r *Raid) victory(tx *world.Tx) {
	for _, h := range r.v.Dwellers(RoleInhabitant) {
		if m, ok := mob.OfHandle(tx, h); ok {
			m.Fire("minecraft:start_celebrating", nil)
		}
	}
}

// recruit is RaidRecruitSystem: raiders recruit can_join_raid mobs within
// 16 blocks (minecraft:spawn_for_raid), which then join the raid.
func (r *Raid) recruit(tx *world.Tx) {
	if r.v.m.now%20 != 0 {
		return
	}
	for _, h := range slices.Clone(r.raiders) {
		e, ok := h.Entity(tx)
		if !ok {
			continue
		}
		p := e.Position()
		box := cube.Box(p[0]-recruitRadius, p[1]-recruitRadius, p[2]-recruitRadius, p[0]+recruitRadius, p[1]+recruitRadius, p[2]+recruitRadius)
		for o := range tx.EntitiesWithin(box) {
			m, ok := mob.Of(o)
			if !ok || m.Dead() || !m.HasComponent("minecraft:can_join_raid") || slices.Contains(r.raiders, m.H()) {
				continue
			}
			m.Fire("minecraft:spawn_for_raid", nil)
			r.join(m)
		}
	}
}

// join makes m a raider of r's village (_readyRaidGroup).
func (r *Raid) join(m *mob.Mob) {
	if d, ok := DwellerOf(m); ok && d.role == RoleHostile {
		d.SetVillage(m, r.v)
	} else {
		r.v.addActor(m.Tx(), RoleHostile, m.H(), m.Position())
	}
	r.addRaider(m)
}

// findSpawn is the findSpawn hook of Village::_createRaid: attempts 0..5
// with different searches, 6+ aborts.
func (r *Raid) findSpawn(tx *world.Tx, attempt int) (mgl64.Vec3, bool, bool) {
	switch attempt {
	case 0, 2, 3, 4, 5:
		dist := []float64{16, 0, 0, 1, 8, 8}[attempt]
		p, ok := r.perimeterSpawn(tx, dist)
		return p, ok, false
	case 1:
		p, ok := r.playerCentricSpawn(tx)
		return p, ok, false
	}
	return mgl64.Vec3{}, false, true
}

// perimeterSpawn is Village::_findSpawnPointForRaid: a point on the bounds'
// perimeter pushed dist outwards, on the ground (10 tries).
func (r *Raid) perimeterSpawn(tx *world.Tx, dist float64) (mgl64.Vec3, bool) {
	mn, mx := r.v.bounds.Min(), r.v.bounds.Max()
	w, d := mx[0]-mn[0], mx[2]-mn[2]
	per := 2 * (w + d)
	c := r.v.Center()
	for i := range 10 {
		t := math.Mod(float64(r.v.m.now*7919+int64(i)*104729), per)
		var p mgl64.Vec3
		switch {
		case t < w:
			p = mgl64.Vec3{mn[0] + t, c[1], mn[2]}
		case t < w+d:
			p = mgl64.Vec3{mx[0], c[1], mn[2] + t - w}
		case t < 2*w+d:
			p = mgl64.Vec3{mx[0] - (t - w - d), c[1], mx[2]}
		default:
			p = mgl64.Vec3{mn[0], c[1], mx[2] - (t - 2*w - d)}
		}
		dir := mgl64.Vec3{p[0] - c[0], 0, p[2] - c[2]}
		if dir.Len() > 0 {
			p = p.Add(dir.Normalize().Mul(dist))
		}
		x, z := int(math.Floor(p[0])), int(math.Floor(p[2]))
		y := TopSolid(tx, x, int(c[1])+8, z)
		if s, ok := standingSpot(tx, cube.Pos{x, y, z}, 8); ok {
			return s.Vec3Middle(), true
		}
	}
	return mgl64.Vec3{}, false
}

// playerCentricSpawn is _findPlayerCentricSpawnPointForRaid.
func (r *Raid) playerCentricSpawn(tx *world.Tx) (mgl64.Vec3, bool) {
	b := r.Bounds()
	var players []world.Entity
	for p := range tx.Players() {
		if b.Vec3Within(p.Position()) {
			players = append(players, p)
		}
	}
	if len(players) == 0 {
		return mgl64.Vec3{}, false
	}
	c := r.v.Center()
	for i := range 10 {
		pp := players[(int(r.v.m.now)+i)%len(players)].Position()
		rad := 32 + float64((r.v.m.now*31+int64(i)*17)%16)
		ang := float64((r.v.m.now*13+int64(i)*97)%360) * math.Pi / 180
		off := mgl64.Vec3{math.Cos(ang) * rad, 0, math.Sin(ang) * rad}
		if off.Dot(mgl64.Vec3{c[0] - pp[0], 0, c[2] - pp[2]}) > 0 {
			off = off.Mul(-1)
		}
		p := pp.Add(off)
		x, z := int(math.Floor(p[0])), int(math.Floor(p[2]))
		y := TopSolid(tx, x, int(pp[1])+8, z)
		if s, ok := standingSpot(tx, cube.Pos{x, y, z}, 8); ok {
			return s.Vec3Middle(), true
		}
	}
	return mgl64.Vec3{}, false
}

// spawnGroup is _spawnRaidGroup + _readyRaidGroup.
func (r *Raid) spawnGroup(tx *world.Tx) {
	if r.Group >= len(raidGroups) {
		return
	}
	for _, e := range raidGroups[r.Group] {
		ev := e.event
		if ev == "" {
			ev = "minecraft:spawn_for_raid"
		}
		for i := range e.count {
			off := mgl64.Vec3{float64(i%3) - 1, 0, float64(i/3%3) - 1}
			m, err := mob.Spawn(tx, e.id, r.spawnPos.Add(off), mob.SpawnOptions{Event: ev})
			if err != nil {
				continue
			}
			r.join(m)
			for _, p := range m.Passengers() {
				if pm, ok := mob.Of(p); ok {
					r.join(pm)
				}
			}
		}
	}
}

// horn plays raid.horn to the players in the raid area.
func (r *Raid) horn(tx *world.Tx, at mgl64.Vec3) {
	tx.PlaySound(at, mob.Sound{Event: "raid.horn"})
}

// Fraction is Raid::getBossBarFilledFraction.
func (r *Raid) Fraction(tx *world.Tx) float64 {
	switch r.State {
	case RaidPre:
		return math.Min(1, float64(r.ticks)/raidPrepTicks)
	case RaidFindSpawn, RaidSpawn:
		return 1
	}
	if r.maxHealth <= 0 {
		if r.numRaid == 0 {
			return 0
		}
		return float64(len(r.raiders)) / float64(r.numRaid)
	}
	sum := 0.0
	for _, h := range r.raiders {
		if m, ok := mob.OfHandle(tx, h); ok {
			sum += m.Health()
		}
	}
	return math.Max(0, math.Min(1, sum/r.maxHealth))
}

// Title is RaidBossComponent::getName.
func (r *Raid) Title() string {
	t := "Raid"
	switch {
	case r.State == RaidFinished && r.Status == 1:
		t += " - Victory"
	case r.State == RaidFinished && r.Status == 2:
		t += " - Defeat"
	case r.State == RaidInPlay && len(r.raiders) <= 2:
		t += fmt.Sprintf(" - Raiders Remaining: %d", len(r.raiders))
	}
	return t
}

// updateBars shows the raid bar to players inside the raid area
// (RaidBossSystem) once a second, and hides it when they leave.
func (r *Raid) updateBars(tx *world.Tx) {
	mgr := r.v.m
	if mgr.now%20 != 0 {
		return
	}
	b := r.Bounds()
	bar := bossbar.New(r.Title()).WithHealthPercentage(r.Fraction(tx)).WithColour(bossbar.Red())
	for e := range tx.Players() {
		p, ok := e.(*player.Player)
		if !ok {
			continue
		}
		in := b.Vec3Within(p.Position())
		cur, had := mgr.bars[p.H()]
		switch {
		case in:
			p.SendBossBar(bar)
			mgr.bars[p.H()] = r
		case had && cur == r:
			p.RemoveBossBar()
			delete(mgr.bars, p.H())
		}
	}
}

// end deletes the raid and its bars.
func (r *Raid) end(tx *world.Tx) {
	mgr := r.v.m
	for h, cur := range mgr.bars {
		if cur != r {
			continue
		}
		if e, ok := h.Entity(tx); ok {
			if p, ok := e.(*player.Player); ok {
				p.RemoveBossBar()
			}
		}
		delete(mgr.bars, h)
	}
	r.State = RaidDone
	if r.v.raid == r {
		r.v.raid = nil
	}
}

// omenState is a player's raid omen: the village position it was gained at
// (GainedRaidOmenAtPositionComponent) and when it runs out.
type omenState struct {
	pos    mgl64.Vec3
	expiry int64
	level  int
}

// tickPlayers turns bad omen into raid omen in villages (player.json's
// environment_sensor) and triggers the raid when raid omen runs out
// (RaidOmenMobEffect::onEffectExpired -> RaidTriggerSystem). Insomnia is
// the player's own (InsomniaTicks).
func (m *Manager) tickPlayers(tx *world.Tx) {
	seen := map[*world.EntityHandle]bool{}
	for e := range tx.Players() {
		p, ok := e.(*player.Player)
		if !ok {
			continue
		}
		h := p.H()
		seen[h] = true
		if st, ok := m.omens[h]; ok {
			if _, has := hasEffect(p, RaidOmen); !has || m.now >= st.expiry {
				delete(m.omens, h)
				if m.now+1 >= st.expiry {
					if v := m.ClosestVillage(st.pos, maxPOITolerance); v != nil && p.GameMode().Visible() {
						v.TriggerRaid(tx)
					}
					p.RemoveEffect(RaidOmen)
				}
			}
			continue
		}
		if m.now%20 != 0 {
			continue
		}
		bad, ok := hasEffect(p, BadOmen)
		if !ok {
			continue
		}
		if v := m.VillageAt(p.Position()); v != nil && v.raid == nil {
			p.RemoveEffect(BadOmen)
			p.AddEffect(effect.New(RaidOmen, bad.Level(), raidOmenDuration))
			m.omens[h] = &omenState{pos: p.Position(), expiry: m.now + int64(raidOmenDuration/(time.Second/20)), level: bad.Level()}
		}
	}
	for h := range m.omens {
		if !seen[h] {
			delete(m.omens, h)
		}
	}
}

// traderScheduler is WanderingTraderScheduler: once a day (24000 ticks) a
// chance (25% + 25% per failed day, at most 75%; Java's values, the BDS
// table is not in the dump) to spawn a wandering trader with two trader
// llamas near the bell of the village closest to a random player.
type traderScheduler struct {
	next     int64
	days     int
	spawning bool
	existing *world.EntityHandle
}

var traderChance = [4]float64{0.25, 0.5, 0.75, 0.75}

// manages is isWanderingTraderCurrentlyManaged.
func (t *traderScheduler) manages(h *world.EntityHandle) bool {
	return t.existing == nil || t.existing == h || t.existing.Closed()
}

func (t *traderScheduler) tick(tx *world.Tx, m *Manager) {
	if !TraderSpawning || tx.World().Dimension() != world.Overworld {
		return
	}
	if !t.spawning && m.now >= t.next {
		t.next = m.now + 24000
		if float64(m.rand(1000))/1000 >= traderChance[min(t.days, 3)] {
			t.days++
		} else {
			t.days, t.spawning = 0, true
		}
	}
	if !t.spawning {
		return
	}
	var players []world.Entity
	for p := range tx.Players() {
		players = append(players, p)
	}
	if len(players) == 0 {
		return
	}
	p := players[m.rand(len(players))]
	if t.existing != nil && !t.existing.Closed() {
		if e, ok := t.existing.Entity(tx); ok {
			for o := range tx.Players() {
				if o.Position().Sub(e.Position()).Len() < 48 {
					return
				}
			}
		}
	}
	var origin mgl64.Vec3
	if v := m.ClosestVillage(p.Position(), 24); v != nil {
		if bell, ok := v.ClosestPOI(POIMeeting, p.Position()); ok {
			origin = bell.Centre()
		} else {
			origin = v.Center()
		}
	} else {
		return
	}
	if mm, ok := m.trySpawnMob(tx, "minecraft:wandering_trader", "minecraft:scheduled", origin, 10, 2, 8); ok {
		if t.existing != nil {
			if e, ok := t.existing.Entity(tx); ok {
				_ = e.Close()
			}
		}
		t.existing, t.spawning = mm.H(), false
		// Its two trader llamas come from its own minecraft:spawn_entity
		// (should_leash), as in BDS: the scheduler spawns only the trader.
	}
}

// TraderSpawning turns the wandering trader scheduler on (default on, like
// the doTraderSpawning game rule).
var TraderSpawning = true

// celebrate helpers shared with the goals.

// SetCelebrating turns the celebrating actor flag (93, or 95 for the
// special dance) on or off.
func SetCelebrating(m *mob.Mob, v, special bool) {
	if special {
		m.SetActorFlag(FlagCelebratingSpecial, v)
	} else {
		m.SetActorFlag(FlagCelebrating, v)
	}
}

// SetGroups changes the number of waves.
func (r *Raid) SetGroups(n int) { r.NumGroups = n }
