//go:build crossfly_full

package event

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/df-mc/dragonfly/server/mob/def/molang"
	"github.com/df-mc/dragonfly/server/mob/filter"
)

// Action is an event response other than add/remove/trigger (BDS's
// ActorEventResponseCollection entries).
type Action interface {
	// Key is the JSON key, e.g. "set_property".
	Key() string
}

// SetProperty changes description.properties values. The executor applies it
// through State.SetProperty.
type SetProperty struct{ Values []PropertyValue }

// QueueCommand runs commands on the target. The default handler (def.Instance)
// stores them in a log.
type QueueCommand struct {
	Commands []string
	Target   filter.Subject
}

// PlaySound plays a level sound event on the target.
type PlaySound struct {
	Sound  string
	Target filter.Subject
}

// EmitVibration emits a game event (vibration) at the entity.
type EmitVibration struct{ Vibration string }

// EmitParticle spawns a particle at the entity.
type EmitParticle struct {
	Particle string
	Target   filter.Subject
}

// ResetTarget clears the entity's target.
type ResetTarget struct{}

// StopMovement stops the entity.
type StopMovement struct{ Vertical, Horizontal bool }

// SetHomePosition sets the home (minecraft:home) to the current position.
type SetHomePosition struct{}

// DropItem drops the item in a slot ("slot.weapon.mainhand").
type DropItem struct{ Slot string }

// ExecuteEventOnHomeBlock runs a block event on the home block.
type ExecuteEventOnHomeBlock struct{ Event string }

// Unknown keeps a response key the parser does not know.
type Unknown struct {
	Name string
	Raw  json.RawMessage
}

func (SetProperty) Key() string             { return "set_property" }
func (QueueCommand) Key() string            { return "queue_command" }
func (PlaySound) Key() string               { return "play_sound" }
func (EmitVibration) Key() string           { return "emit_vibration" }
func (EmitParticle) Key() string            { return "emit_particle" }
func (ResetTarget) Key() string             { return "reset_target" }
func (StopMovement) Key() string            { return "stop_movement" }
func (SetHomePosition) Key() string         { return "set_home_position" }
func (DropItem) Key() string                { return "drop_item" }
func (ExecuteEventOnHomeBlock) Key() string { return "execute_event_on_home_block" }
func (u Unknown) Key() string               { return u.Name }

// actionOrder fixes the execution order of the actions of one leaf.
func actionOrder(k string) int {
	switch k {
	case "set_property":
		return 0
	case "queue_command":
		return 1
	case "play_sound":
		return 2
	case "emit_particle":
		return 3
	case "emit_vibration":
		return 4
	case "reset_target":
		return 5
	case "stop_movement":
		return 6
	case "set_home_position":
		return 7
	case "drop_item":
		return 8
	case "execute_event_on_home_block":
		return 9
	}
	return 100
}

func obj(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

func target(m map[string]any) (filter.Subject, error) {
	if t, ok := m["target"]; ok {
		return filter.ParseSubject(t)
	}
	return filter.SubjectSelf, nil
}

func parseAction(k string, v any) (Action, error) {
	m := obj(v)
	switch k {
	case "set_property":
		if m == nil {
			return nil, fmt.Errorf("event: set_property must be an object")
		}
		names := make([]string, 0, len(m))
		for n := range m {
			names = append(names, n)
		}
		sort.Strings(names)
		sp := SetProperty{}
		for _, n := range names {
			pv, err := parsePropertyValue(n, m[n])
			if err != nil {
				return nil, err
			}
			sp.Values = append(sp.Values, pv)
		}
		return sp, nil
	case "queue_command":
		qc := QueueCommand{}
		switch c := m["command"].(type) {
		case string:
			qc.Commands = []string{c}
		case []any:
			for _, e := range c {
				if s, ok := e.(string); ok {
					qc.Commands = append(qc.Commands, s)
				}
			}
		}
		t, err := target(m)
		qc.Target = t
		return qc, err
	case "play_sound":
		t, err := target(m)
		return PlaySound{Sound: str(m, "sound"), Target: t}, err
	case "emit_vibration":
		return EmitVibration{Vibration: str(m, "vibration")}, nil
	case "emit_particle":
		t, err := target(m)
		return EmitParticle{Particle: str(m, "particle"), Target: t}, err
	case "reset_target":
		return ResetTarget{}, nil
	case "stop_movement":
		sm := StopMovement{Vertical: true, Horizontal: true}
		if b, ok := m["stop_vertical_movement"].(bool); ok {
			sm.Vertical = b
		}
		if b, ok := m["stop_horizontal_movement"].(bool); ok {
			sm.Horizontal = b
		}
		return sm, nil
	case "set_home_position":
		return SetHomePosition{}, nil
	case "drop_item":
		return DropItem{Slot: str(m, "slot")}, nil
	case "execute_event_on_home_block":
		return ExecuteEventOnHomeBlock{Event: str(m, "event")}, nil
	}
	raw, _ := json.Marshal(v)
	return Unknown{Name: k, Raw: raw}, nil
}

// ParsePropertyValue parses one set_property value: a bool, a number, a
// Molang string, or a plain string (an enum value such as "warm"). Strings
// that fail to parse as Molang are taken literally, which is how vanilla
// writes enum values.
func ParsePropertyValue(name string, v any) (PropertyValue, error) {
	return parsePropertyValue(name, v)
}

func parsePropertyValue(name string, v any) (PropertyValue, error) {
	pv := PropertyValue{Name: name}
	switch v := v.(type) {
	case bool:
		pv.Literal = molang.Bool(v)
	case float64:
		pv.Literal = molang.Num(v)
	case string:
		e, err := molang.Parse(v)
		if err != nil {
			pv.Literal = molang.Str(v)
			break
		}
		if e.Constant() {
			pv.Literal = e.Eval(nil)
			break
		}
		pv.Expr = e
	default:
		return pv, fmt.Errorf("event: set_property %s: unsupported value %v", name, v)
	}
	return pv, nil
}
