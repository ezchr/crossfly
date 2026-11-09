//go:build crossfly_full

package village

import (
	"encoding/json"
	"fmt"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// Dweller is minecraft:dweller (BDS DwellerComponent + DwellerSystem): it
// asks the manager to look for POIs around the mob every
// update_interval_base + rand(update_interval_variant) ticks, joins the
// closest suitable village (migrating when it left its village's bounds by
// more than dwelling_bounds_tolerance) and keeps its record fresh.
type Dweller struct {
	role        Role
	typ         string
	base, vary  int
	canFindPOI  bool
	canMigrate  bool
	reward      int
	tolerance   float64
	defPref     string
	counter     int
	village     *Village
	hasJoined   bool
	rewardDue   bool
	preferred   string
	initialised bool
}

func newDweller(_ string, raw json.RawMessage) (mob.Component, error) {
	j := struct {
		Type      string   `json:"dwelling_type"`
		Role      string   `json:"dweller_role"`
		Base      int      `json:"update_interval_base"`
		Variant   int      `json:"update_interval_variant"`
		FindPOI   bool     `json:"can_find_poi"`
		Reward    int      `json:"first_founding_reward"`
		Migrate   bool     `json:"can_migrate"`
		Tolerance *float64 `json:"dwelling_bounds_tolerance"`
		Pref      string   `json:"preferred_profession"`
	}{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &j); err != nil {
			return nil, fmt.Errorf("dweller: %w", err)
		}
	}
	d := &Dweller{role: parseRole(j.Role), typ: j.Type, base: j.Base, vary: j.Variant, canFindPOI: j.FindPOI,
		canMigrate: j.Migrate, reward: j.Reward, tolerance: 10, defPref: j.Pref, rewardDue: true}
	if j.Tolerance != nil {
		d.tolerance = *j.Tolerance
	}
	return d, nil
}

// DwellerOf returns m's dweller component.
func DwellerOf(m *mob.Mob) (*Dweller, bool) { return mob.ComponentOf[*Dweller](m) }

// VillageOf returns the village m dwells in, if any.
func VillageOf(m *mob.Mob) (*Village, bool) {
	d, ok := DwellerOf(m)
	if !ok || d.village.Removed() {
		return nil, false
	}
	return d.village, true
}

// Role is the dweller's role.
func (d *Dweller) Role() Role { return d.role }

// Village returns the dweller's village (nil if none).
func (d *Dweller) Village() *Village {
	if d.village.Removed() {
		return nil
	}
	return d.village
}

// Preferred is the dweller's preferred profession.
func (d *Dweller) Preferred() string { return d.preferred }

// SetVillage joins v directly (raids, iron golems spawned by a village).
func (d *Dweller) SetVillage(m *mob.Mob, v *Village) {
	if old := d.Village(); old != nil && old != v {
		old.removeActor(m.H())
	}
	d.village, d.hasJoined = v, true
	if v != nil {
		v.addActor(m.Tx(), d.role, m.H(), m.Position())
	}
}

func (d *Dweller) Attach(m *mob.Mob) {
	if m.HasComponent("minecraft:is_baby") || d.defPref == "" {
		d.preferred = ""
	} else if d.preferred == "" {
		d.preferred = d.defPref
	}
	// A component group swap rebuilds the component: keep the village.
	if v, ok := m.Data(dwellerVillageKey{}); ok {
		d.village, _ = v.(*Village)
		d.hasJoined = d.village != nil
	}
}

func (d *Dweller) Detach(m *mob.Mob) {
	if v := d.Village(); v != nil {
		m.SetData(dwellerVillageKey{}, v)
	}
}

type dwellerVillageKey struct{}

// trySetPreferredProfession is DwellerComponent::trySetPreferredProfession.
func (d *Dweller) trySetPreferredProfession(m *mob.Mob, name string) bool {
	if m.HasComponent("minecraft:is_baby") || TradeLocked(m) {
		return false
	}
	d.preferred = name
	return true
}

// HasPreferredProfession is DwellerComponent::hasPreferredProfession: an
// adult locked in to its trades with a profession.
func (d *Dweller) HasPreferredProfession(m *mob.Mob) bool {
	return !m.HasComponent("minecraft:is_baby") && TradeLocked(m) && d.preferred != ""
}

// Tick is DwellerSystem::_tickDwellerComponent.
func (d *Dweller) Tick(m *mob.Mob) {
	if m.Dead() {
		return
	}
	mgr := ManagerOf(m.Tx())
	if v := d.Village(); v != nil {
		v.lastActive = mgr.now
	}
	if d.counter > 0 {
		d.counter--
		return
	}
	if d.canFindPOI {
		mgr.submitQuery(m.H())
	}
	d.counter = d.base
	if d.vary > 0 {
		d.counter += m.Rand().IntN(d.vary)
	}
	v := d.Village()
	if (d.canMigrate || !d.hasJoined) && (v == nil || !v.Within(m.Position(), d.tolerance)) {
		if nv := mgr.closestSuitable(m.Position(), maxPOITolerance); nv != nil && nv != v {
			if v != nil {
				v.removeActor(m.H())
			}
			nv.addActor(m.Tx(), d.role, m.H(), m.Position())
			d.village, d.hasJoined = nv, true
			if d.role == RoleInhabitant && d.rewardDue {
				nv.RewardAllPlayers(m.Tx(), d.reward)
				d.rewardDue = false
			}
		}
	} else if v != nil {
		if dd, ok := v.dwellers[d.role][m.H()]; ok {
			dd.lastSeen, dd.pos = mgr.now, m.Position()
		}
	}
	if v := d.Village(); v != nil && !v.HasDweller(d.role, m.H()) {
		d.village = nil
	}
	if d.counter > 0 {
		d.counter--
	}
}

// OnHurt is DwellerComponent::setLastHurtByMob: inhabitants and defenders
// remember aggressors; players lose standing for hitting villagers.
func (d *Dweller) OnHurt(m *mob.Mob, _ float64, _ world.DamageSource) {
	v := d.Village()
	if v == nil {
		return
	}
	attacker, _, ok := m.LastHurtBy()
	if !ok || attacker == nil {
		return
	}
	switch d.role {
	case RoleInhabitant:
		v.AddAggressor(attacker)
		if _, ok := attacker.(*player.Player); ok {
			pen := StandingSmallPenalty
			if m.HasComponent("minecraft:is_baby") {
				pen = StandingLargePenalty
			}
			v.ModifyStanding(attacker, pen)
		}
	case RoleDefender:
		v.AddAggressor(attacker)
	}
}

// OnDeath is DwellerComponent::onDeath.
func (d *Dweller) OnDeath(m *mob.Mob, _ world.DamageSource) {
	v := d.Village()
	if v == nil {
		return
	}
	killer, _, _ := m.LastHurtBy()
	_, byPlayer := killer.(*player.Player)
	switch d.role {
	case RoleInhabitant:
		if byPlayer {
			v.ModifyStanding(killer, StandingMediumPenalty)
		} else if km, ok := mob.Of(killer); ok && km.HasFamily("monster") {
			v.ResetNoBreedTimer()
		}
	case RoleDefender:
		if byPlayer {
			v.ModifyStanding(killer, StandingGolemDeath)
		}
	case RoleHostile:
		if byPlayer && v.raid != nil {
			v.raid.addHero(killer)
		}
	}
	v.removeActor(m.H())
	d.village = nil
}

// EncodeNBT ...
func (d *Dweller) EncodeNBT() map[string]any {
	return map[string]any{"PreferredProfession": d.preferred, "RewardPlayersOnFirstFounding": boolByte(d.rewardDue), "HasJoinedOnce": boolByte(d.hasJoined)}
}

// DecodeNBT ...
func (d *Dweller) DecodeNBT(_ *mob.Mob, data map[string]any) {
	if s, ok := data["PreferredProfession"].(string); ok {
		d.preferred = s
	}
	if b, ok := data["RewardPlayersOnFirstFounding"].(uint8); ok {
		d.rewardDue = b != 0
	}
}

func boolByte(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}

// scheduler is minecraft:scheduler (BDS SchedulerSystem): every tick the
// first scheduled event whose filters pass is found; its event fires once
// when that entry changes (edge triggered). min/max_delay_secs are parsed
// but unused, as in BDS 1.21.3.
type scheduler struct {
	events []core.Trigger
	cur    int
}

func newScheduler(_ string, raw json.RawMessage) (mob.Component, error) {
	var j struct {
		Events []core.Trigger `json:"scheduled_events"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &j); err != nil {
			return nil, fmt.Errorf("scheduler: %w", err)
		}
	}
	return &scheduler{events: j.Events, cur: -1}, nil
}

func (s *scheduler) Attach(*mob.Mob) {}
func (s *scheduler) Detach(*mob.Mob) {}

func (s *scheduler) Tick(m *mob.Mob) {
	ctx := m.Ctx()
	if s.cur >= 0 && s.cur < len(s.events) && s.events[s.cur].Passes(ctx) {
		return
	}
	s.cur = -1
	for i, e := range s.events {
		if e.Passes(ctx) {
			s.cur = i
			e.Fire(m, ctx)
			return
		}
	}
}
