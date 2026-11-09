//go:build crossfly_full

package boss

import (
	"encoding/json"
	"fmt"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
)

// eventTrigger is a trigger component the hard-coded boss code fires
// (minecraft:on_start_landing, minecraft:on_start_takeoff: BDS
// OnStartLandingDefinition / OnStartTakeoffDefinition, fired by the dragon
// goals). {"event", "target", "filters"}.
type eventTrigger struct {
	t sense.Trigger
}

func newEventTrigger(key string, raw json.RawMessage) (mob.Component, error) {
	t, err := sense.ParseTrigger(raw)
	if err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	return &eventTrigger{t: t}, nil
}

// Attach ...
func (*eventTrigger) Attach(*mob.Mob) {}

// Detach ...
func (*eventTrigger) Detach(*mob.Mob) {}

// Trigger keys.
const (
	OnStartLanding = "minecraft:on_start_landing"
	OnStartTakeoff = "minecraft:on_start_takeoff"
	OnDeath        = "minecraft:on_death"
)

// FireTrigger runs the trigger component key of m (on_start_landing,
// on_start_takeoff, or on_death, whose JSON is read here as well because the
// dragon never dies the usual way). It reports whether an event fired.
func FireTrigger(m *mob.Mob, key string) bool {
	if c, ok := m.Component(key); ok {
		if et, ok := c.(*eventTrigger); ok {
			return et.t.Run(m, nil)
		}
	}
	raw, ok := m.ComponentJSON(key)
	if !ok {
		return false
	}
	t, err := sense.ParseTrigger(raw)
	if err != nil {
		return false
	}
	return t.Run(m, nil)
}
