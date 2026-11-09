//go:build crossfly_full

package social

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/boss"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// AngerAt is implemented by a minecraft:anger_level component: sniffing
// raises the warden's anger at what it smelled (BDS
// AngerLevelComponent::increaseAngerAt).
type AngerAt interface {
	IncreaseAngerAt(m *mob.Mob, e world.Entity, amount int)
}

// sniffAnger is the anger the warden keeps per entity when it has no
// anger_level component (mob.Data).
type sniffAngerKey struct{}

// Sniff anger defaults (anger_level default_annoyingness 35, angry_threshold
// 80).
const (
	sniffAnnoyingness = 35
	sniffAngryAt      = 80
)

// SniffAnger returns the anger m built up at e through sniffing alone.
func SniffAnger(m *mob.Mob, e world.Entity) int {
	v, ok := m.Data(sniffAngerKey{})
	if !ok {
		return 0
	}
	return v.(map[*world.EntityHandle]int)[e.H()]
}

// sniff is minecraft:behavior.sniff (BDS SniffGoal; Java Sniffing): when the
// cooldown has run out and a nuisance (a living non-warden, players first)
// is within sniffing_radius, the warden stops and sniffs for duration with
// the sniffing actor flag and the "sniff" sound. When it ends, the nearest
// nuisance within suspicion_radius_horizontal/vertical makes it angrier
// (35, as Java's Sniffing.stop): through an anger_level component when the
// mob has one, otherwise counted here, becoming the target at 80.
//
//	duration                      1 s
//	sniffing_radius               5
//	suspicion_radius_horizontal   3
//	suspicion_radius_vertical     3
//	cooldown_range                {5, 10} s (Java SNIFF_COOLDOWN)
//
// Control flags move|look (BDS SetRotation(3)).
type sniff struct {
	mob.GoalBase
	duration     int
	radius       float64
	suspH, suspV float64
	cooldown     kit.Range

	next, ends int64
}

func newSniff(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Duration *float64        `json:"duration"`
		Radius   *float64        `json:"sniffing_radius"`
		SuspH    *float64        `json:"suspicion_radius_horizontal"`
		SuspV    *float64        `json:"suspicion_radius_vertical"`
		Cooldown json.RawMessage `json:"cooldown_range"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	g := &sniff{
		GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)},
		duration: kit.Ticks(kit.Or(v.Duration, 1)), radius: kit.Or(v.Radius, 5),
		suspH: kit.Or(v.SuspH, 3), suspV: kit.Or(v.SuspV, 3),
	}
	var err error
	if g.cooldown, err = kit.ParseRange(v.Cooldown, kit.Range{Min: 5, Max: 10}); err != nil {
		return nil, err
	}
	return g, nil
}

// nuisance is AngerLevelComponent::canBeNuisance with the warden's
// nuisance_filter: living, not a warden, not inanimate, not a creative
// player.
func nuisance(m *mob.Mob, e world.Entity) bool {
	if !kit.Alive(e) || kit.Creative(e) {
		return false
	}
	if om, ok := mob.Of(e); ok {
		return !om.HasFamily("warden") && !om.HasFamily("inanimate")
	}
	return kit.VisiblePlayer(e)
}

// nearest is SniffGoal::_fetchNearestSniffableActor: players first.
func (g *sniff) nearest(m *mob.Mob, h, v float64, sphere bool) (world.Entity, bool) {
	var first world.Entity
	for _, e := range kit.Around(m, h, v) {
		if !nuisance(m, e) {
			continue
		}
		if sphere && kit.DistSq(e.Position(), m.Position()) > h*h {
			continue
		}
		if kit.IsPlayer(e) {
			return e, true
		}
		if first == nil {
			first = e
		}
	}
	return first, first != nil
}

// CanUse ...
func (g *sniff) CanUse(m *mob.Mob) bool {
	if m.Ticks() < g.next {
		return false
	}
	if _, ok := m.Target(); ok {
		return false
	}
	_, ok := g.nearest(m, g.radius, g.radius, true)
	return ok
}

// CanContinueToUse ...
func (g *sniff) CanContinueToUse(m *mob.Mob) bool { return m.Ticks() < g.ends }

// Start ...
func (g *sniff) Start(m *mob.Mob) {
	g.ends = m.Ticks() + int64(g.duration)
	m.StopMovement()
	kit.Flag(m, protocol.EntityDataFlagSniffing, true)
	kit.Sound(m, "sniff")
}

// Stop ...
func (g *sniff) Stop(m *mob.Mob) {
	kit.Flag(m, protocol.EntityDataFlagSniffing, false)
	g.next = m.Ticks() + int64(kit.RollTicks(m, g.cooldown))
	e, ok := g.nearest(m, g.suspH, g.suspV, false)
	if !ok || math.Abs(e.Position()[1]-m.Position()[1]) > g.suspV {
		return
	}
	// SniffGoal::stop always makes the position of the nuisance the
	// suspicious one (minecraft:suspect_tracking, component/boss), which
	// investigate_suspicious_location walks to.
	if st, ok := boss.SuspectOf(m); ok {
		st.SetSuspiciousPos(m, cube.PosFromVec3(e.Position()))
	}
	if a, ok := mob.ComponentOf[AngerAt](m); ok {
		a.IncreaseAngerAt(m, e, sniffAnnoyingness)
		return
	}
	v, _ := m.Data(sniffAngerKey{})
	anger, _ := v.(map[*world.EntityHandle]int)
	if anger == nil {
		anger = map[*world.EntityHandle]int{}
		m.SetData(sniffAngerKey{}, anger)
	}
	anger[e.H()] += sniffAnnoyingness
	if anger[e.H()] >= sniffAngryAt {
		if _, has := m.Target(); !has {
			m.SetTarget(e)
		}
	}
}

// AppendDebugInfo ...
func (g *sniff) AppendDebugInfo(b *strings.Builder) { b.WriteString("Sniff") }
