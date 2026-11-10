package jraid

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/google/uuid"
)

// State holds the fields of a raider (Raider and PatrollingMonster) that raids and patrols use. Each raider mob
// owns one.
type State struct {
	raid *Raid
	// Wave is Raider.wave: the wave the raider joined.
	Wave int
	// CanJoinRaid is Raider.canJoinRaid.
	CanJoinRaid bool
	// TicksOutsideRaid is Raider.ticksOutsideRaid.
	TicksOutsideRaid int
	// PatrolTarget is PatrollingMonster.patrolTarget.
	PatrolTarget *cube.Pos
	// PatrolLeader is PatrollingMonster.patrolLeader.
	PatrolLeader bool
	// Patrolling is PatrollingMonster.patrolling.
	Patrolling bool
	// Celebrating is Raider.IS_CELEBRATING.
	Celebrating bool
	// pendingRaidID is the RaidId read from NBT, resolved once the raider is in a level.
	pendingRaidID int
}

// Raid is Raider.getCurrentRaid.
func (s *State) Raid() *Raid { return s.raid }

// SetRaid is Raider.setCurrentRaid.
func (s *State) SetRaid(r *Raid) { s.raid = r }

// HasActiveRaid is Raider.hasActiveRaid.
func (s *State) HasActiveRaid() bool { return s.raid != nil && s.raid.IsActive() }

// HasRaid is Raider.hasRaid: the raider is in a raid or in the area of one.
func (s *State) HasRaid(rs *Raids, pos cube.Pos) bool { return s.raid != nil || rs.RaidAt(pos) != nil }

// CanJoinPatrol is Raider.canJoinPatrol (PatrollingMonster.canJoinPatrol for other patrolling monsters).
func (s *State) CanJoinPatrol() bool { return !s.HasActiveRaid() }

// setPatrolLeader is PatrollingMonster.setPatrolLeader.
func (s *State) setPatrolLeader(v bool) {
	s.PatrolLeader = v
	s.Patrolling = true
}

// SetPatrolLeader is PatrollingMonster.setPatrolLeader.
func (s *State) SetPatrolLeader(v bool) { s.setPatrolLeader(v) }

// SetPatrolTarget is PatrollingMonster.setPatrolTarget.
func (s *State) SetPatrolTarget(p cube.Pos) {
	s.PatrolTarget = &p
	s.Patrolling = true
}

// FindPatrolTarget is PatrollingMonster.findPatrolTarget: a target up to 500 blocks away on both axes.
func (s *State) FindPatrolTarget(r *rand.Rand, pos cube.Pos) {
	t := pos.Add(cube.Pos{-500 + r.IntN(1000), 0, -500 + r.IntN(1000)})
	s.PatrolTarget = &t
	s.Patrolling = true
}

// IsCaptain is Raider.isCaptain: the raider leads its patrol or wave and wears the ominous banner.
func (s *State) IsCaptain(wearsOminousBanner bool) bool { return wearsOminousBanner && s.PatrolLeader }

// RemoveWhenFarAway is Raider.removeWhenFarAway with PatrollingMonster.removeWhenFarAway: raiders of a raid never
// despawn, patrolling ones only beyond 128 blocks.
func (s *State) RemoveWhenFarAway(distSqr float64) bool {
	if s.raid != nil {
		return false
	}
	return !s.Patrolling || distSqr > 16384
}

// RequiresCustomPersistence is Raider.requiresCustomPersistence.
func (s *State) RequiresCustomPersistence() bool { return s.raid != nil }

// SpawnReason is the EntitySpawnReason a raider is finalized with, as far as raiders tell them apart.
type SpawnReason int

const (
	ReasonOther SpawnReason = iota
	ReasonNatural
	ReasonPatrol
	ReasonEvent
	ReasonStructure
	ReasonSpawner
	ReasonCommand
	ReasonSpawnEgg
)

// FinalizeSpawn is PatrollingMonster.finalizeSpawn with Raider.finalizeSpawn: a raider becomes a patrol leader with
// a chance of 6% unless it spawned in a patrol, raid or structure; a leader wears the ominous banner
// (setCaptainBanner); a patrol spawn patrols. Witches spawned naturally (in swamp huts) cannot join raids.
func (s *State) FinalizeSpawn(r *rand.Rand, reason SpawnReason, canBeLeader, witch bool, setCaptainBanner func()) {
	s.CanJoinRaid = !witch || reason != ReasonNatural
	if reason != ReasonPatrol && reason != ReasonEvent && reason != ReasonStructure && r.Float32() < 0.06 && canBeLeader {
		s.PatrolLeader = true
	}
	if s.PatrolLeader {
		setCaptainBanner()
	}
	if reason == ReasonPatrol {
		s.Patrolling = true
	}
}

// CanJoinRaid is Raids.canJoinRaid.
func CanJoinRaid(r Raider) bool {
	return r.Alive() && r.RaiderState().CanJoinRaid && r.NoActionTime() <= MaxNoActionTime
}

// AIStep is the part of Raider.aiStep before Mob.aiStep: a raider without a raid joins the raid it is in every
// second; one in a raid resets its idle time while it targets a player or iron golem (the return value).
func AIStep(l Level, rs *Raids, r Raider, targetsPlayerOrGolem bool) (resetNoActionTime bool) {
	if !r.Alive() {
		return false
	}
	st := r.RaiderState()
	if !st.CanJoinRaid {
		return false
	}
	if st.raid == nil {
		if l.GameTime()%20 == 0 {
			if nearby := rs.RaidAt(r.BlockPos()); nearby != nil && CanJoinRaid(r) {
				nearby.JoinRaid(l, nearby.GroupsSpawned(), r, nil, true)
			}
		}
		return false
	}
	return targetsPlayerOrGolem
}

// NoActionTimeStep is Raider.updateNoActionTime: raiders idle twice as fast.
const NoActionTimeStep = 2

// Die is the part of Raider.die before LivingEntity.die: a captain leaves its wave without one, a player killer
// becomes a hero of the village and the raider leaves the raid.
func Die(r Raider, killerPlayer *uuid.UUID) {
	st := r.RaiderState()
	raid := st.raid
	if raid == nil {
		return
	}
	if st.PatrolLeader {
		raid.RemoveLeader(st.Wave)
	}
	if killerPlayer != nil {
		raid.AddHeroOfTheVillage(*killerPlayer)
	}
	raid.RemoveFromRaid(r, false)
}

// Hurt is Raider.hurtServer: the raid bar follows the raider's health.
func Hurt(r Raider) {
	if st := r.RaiderState(); st.HasActiveRaid() {
		st.raid.UpdateBossbar()
	}
}

// CanPickUpBanner is the inverse of Raider.ObtainRaidLeaderBannerGoal.cannotPickUpBanner: mob griefing is on, the
// raider is in an active raid that is not over, it can lead, it does not wear the banner yet and its wave has no
// living leader.
func CanPickUpBanner(l Level, r Raider, wearsOminousBanner bool) bool {
	if !l.MobGriefing() {
		return false
	}
	st := r.RaiderState()
	if !st.HasActiveRaid() || st.raid.IsOver() || !r.CanBeLeader() || wearsOminousBanner {
		return false
	}
	leader, ok := st.raid.Leader(st.Wave)
	return !ok || !leader.Alive()
}

// PickUpBanner is the banner part of Raider.pickUpItem: a raider of an active raid whose wave has no leader picks
// up an ominous banner and becomes the leader. It reports whether the raider takes the banner.
func PickUpBanner(r Raider) bool {
	st := r.RaiderState()
	if !st.HasActiveRaid() {
		return false
	}
	if _, ok := st.raid.Leader(st.Wave); ok {
		return false
	}
	st.raid.groupToLeader[st.Wave] = r
	r.SetCaptainBanner()
	st.setPatrolLeader(true)
	return true
}

// SaveData is Raider.addAdditionalSaveData with PatrollingMonster's fields.
func (s *State) SaveData(m map[string]any) {
	m["Wave"] = int32(s.Wave)
	m["CanJoinRaid"] = boolByte(s.CanJoinRaid)
	if s.raid != nil && s.raid.id > 0 {
		m["RaidId"] = int32(s.raid.id)
	}
	if s.PatrolTarget != nil {
		m["patrol_target"] = []int32{int32(s.PatrolTarget[0]), int32(s.PatrolTarget[1]), int32(s.PatrolTarget[2])}
	}
	m["PatrolLeader"] = boolByte(s.PatrolLeader)
	m["Patrolling"] = boolByte(s.Patrolling)
}

// LoadData is Raider.readAdditionalSaveData with PatrollingMonster's fields. The raid of RaidId is joined by
// Rejoin once the raider is in its level.
func (s *State) LoadData(m map[string]any) {
	s.Wave = int(intOf(m["Wave"]))
	s.CanJoinRaid = intOf(m["CanJoinRaid"]) != 0
	s.pendingRaidID = int(intOf(m["RaidId"]))
	if t, ok := m["patrol_target"].([]int32); ok && len(t) == 3 {
		s.PatrolTarget = &cube.Pos{int(t[0]), int(t[1]), int(t[2])}
	}
	s.PatrolLeader = intOf(m["PatrolLeader"]) != 0
	s.Patrolling = intOf(m["Patrolling"]) != 0
}

// Rejoin is the raid part of Raider.readAdditionalSaveData: a loaded raider joins the raid it was in, as the
// leader if it led its patrol.
func Rejoin(rs *Raids, r Raider) {
	st := r.RaiderState()
	if st.pendingRaidID == 0 {
		return
	}
	id := st.pendingRaidID
	st.pendingRaidID = 0
	raid := rs.Get(id)
	st.raid = raid
	if raid == nil {
		return
	}
	raid.AddWaveMob(st.Wave, r, false)
	if st.PatrolLeader {
		raid.SetLeader(st.Wave, r)
	}
}

func boolByte(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}

func intOf(v any) int64 {
	switch v := v.(type) {
	case uint8:
		return int64(v)
	case int8:
		return int64(v)
	case int16:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}
