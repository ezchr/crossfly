//go:build crossfly_full

package mob

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// ControlFlags are the controls a goal needs exclusively (BDS Goal +8,
// "control_flags" in JSON). Two running goals of the same selector kind may
// not share a flag.
type ControlFlags uint8

const (
	FlagMove ControlFlags = 1 << iota
	FlagLook
	FlagJump
)

// String ...
func (f ControlFlags) String() string {
	var s []string
	if f&FlagMove != 0 {
		s = append(s, "move")
	}
	if f&FlagLook != 0 {
		s = append(s, "look")
	}
	if f&FlagJump != 0 {
		s = append(s, "jump")
	}
	return strings.Join(s, "|")
}

// Goal is one minecraft:behavior.* goal. Only CanUse and Flags are
// required; the rest of the BDS Goal vtable is optional and has the BDS
// defaults when not implemented:
//
//	canUse            +8   CanUse (required)
//	canContinueToUse  +16  GoalContinuer, default: CanUse(m)
//	canBeInterrupted  +24  GoalInterruptible, default: true
//	start             +32  GoalStarter, default: nothing
//	stop              +40  GoalStopper, default: nothing
//	tick              +48  GoalTicker, default: nothing
//	appendDebugInfo   +56  GoalDebugger, default: the goal key
//	isTargetGoal      +64  TargetGoal, default: false
//
// Flags is read every time it is needed, so a goal may change its flags at
// run time as some BDS goals do.
type Goal interface {
	CanUse(m *Mob) bool
	Flags() ControlFlags
}

type (
	GoalContinuer     interface{ CanContinueToUse(m *Mob) bool }
	GoalInterruptible interface{ CanBeInterrupted(m *Mob) bool }
	GoalStarter       interface{ Start(m *Mob) }
	GoalStopper       interface{ Stop(m *Mob) }
	GoalTicker        interface{ Tick(m *Mob) }
	GoalDebugger      interface{ AppendDebugInfo(b *strings.Builder) }
	TargetGoal        interface{ IsTargetGoal() bool }
)

// GoalSpec is what a goal factory gets: the component key and JSON, plus the
// two fields every BDS goal definition parses (BaseGoalDefinition).
type GoalSpec struct {
	Key string
	Raw json.RawMessage
	// Priority is "priority" (default 0; lower runs first).
	Priority int
	// ControlFlags is "control_flags", valid when HasControlFlags.
	ControlFlags    ControlFlags
	HasControlFlags bool
}

// FlagsOr returns the JSON control_flags if the JSON had them, def otherwise.
func (s GoalSpec) FlagsOr(def ControlFlags) ControlFlags {
	if s.HasControlFlags {
		return s.ControlFlags
	}
	return def
}

// Decode unmarshals the goal JSON into v (unknown fields ignored).
func (s GoalSpec) Decode(v any) error {
	if len(s.Raw) == 0 {
		return nil
	}
	return json.Unmarshal(s.Raw, v)
}

// GoalFactory builds a goal from its spec. It is called once per mob each
// time the goal's component becomes active, so the goal may keep state.
type GoalFactory func(spec GoalSpec) (Goal, error)

// ParseGoalSpec reads priority and control_flags from a goal's JSON.
func ParseGoalSpec(key string, raw json.RawMessage) (GoalSpec, error) {
	spec := GoalSpec{Key: key, Raw: raw}
	if len(raw) == 0 {
		return spec, nil
	}
	var base struct {
		Priority     *float64 `json:"priority"`
		ControlFlags []string `json:"control_flags"`
	}
	if err := json.Unmarshal(raw, &base); err != nil {
		return spec, fmt.Errorf("%v: %w", key, err)
	}
	if base.Priority != nil {
		spec.Priority = int(math.Trunc(*base.Priority))
	}
	if base.ControlFlags != nil {
		spec.HasControlFlags = true
		for _, f := range base.ControlFlags {
			switch f {
			case "move":
				spec.ControlFlags |= FlagMove
			case "look":
				spec.ControlFlags |= FlagLook
			case "jump":
				spec.ControlFlags |= FlagJump
			}
		}
	}
	return spec, nil
}

// GoalBase can be embedded by goals for Flags. Set Control in the factory,
// usually to spec.FlagsOr(<BDS default of the goal>).
type GoalBase struct {
	Control ControlFlags
}

// Flags ...
func (b *GoalBase) Flags() ControlFlags { return b.Control }
