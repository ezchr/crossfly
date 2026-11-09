//go:build crossfly_full

package combat

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
)

// lastHurtByMemory is how long the last attacker is remembered for
// hurt_by_target (BDS/Java Mob::setLastHurtByMob keeps it for 100 ticks).
const lastHurtByMemory = 100

// hurtByTarget is minecraft:behavior.hurt_by_target (BDS HurtByTargetGoal):
// targets the entity that last hurt the mob.
type hurtByTarget struct {
	targetBase
	alertSameType bool
}

func newHurtByTarget(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		EntityTypes   json.RawMessage `json:"entity_types"`
		AlertSameType bool            `json:"alert_same_type"`
		HurtOwner     bool            `json:"hurt_owner"`
	}
	if err := spec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	d, err := parseDescriptors(v.EntityTypes, 16)
	if err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	return &hurtByTarget{
		targetBase: targetBase{
			GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}, key: spec.Key, descriptors: d,
			attackOwner: v.HurtOwner,
		},
		alertSameType: v.AlertSameType,
	}, nil
}

// lastHurtBy returns the attacker if it hurt the mob recently.
func lastHurtBy(m *mob.Mob) (world.Entity, bool) {
	e, tick, ok := m.LastHurtBy()
	if !ok || m.Ticks()-tick > lastHurtByMemory {
		return nil, false
	}
	return e, true
}

// CanUse ...
func (g *hurtByTarget) CanUse(m *mob.Mob) bool {
	e, ok := lastHurtBy(m)
	return ok && g.canAttackAndReach(m, e, false, nil)
}

// Start sets the attacker as target and, with alert_same_type, alerts the
// mobs of the same type around that have no target.
func (g *hurtByTarget) Start(m *mob.Mob) {
	g.targetBase.Start(m)
	e, ok := lastHurtBy(m)
	if !ok {
		return
	}
	m.SetTarget(e)
	if !g.alertSameType {
		return
	}
	r := m.Attr(mob.AttrFollowRange)
	p := m.Pos()
	box := cube.Box(p[0]-r, p[1]-10, p[2]-r, p[0]+r, p[1]+10, p[2]+r)
	for o := range m.Tx().EntitiesWithin(box) {
		om, ok := mob.Of(o)
		if !ok || om.H() == m.H() || om.H() == e.H() || om.Identifier() != m.Identifier() || om.Dead() {
			continue
		}
		if _, has := om.Target(); has {
			continue
		}
		if om.Owner() != nil && om.Owner() == e.H() {
			continue
		}
		om.SetTarget(e)
	}
}

// AppendDebugInfo ...
func (g *hurtByTarget) AppendDebugInfo(b *strings.Builder) { b.WriteString("HurtByTarget") }
