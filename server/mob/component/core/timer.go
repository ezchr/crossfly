//go:build crossfly_full

package core

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/df-mc/dragonfly/server/mob"
)

// timer is minecraft:timer: fires time_down_event after a time, once or in
// a loop (BDS TimerComponent/TimerDefinition).
//
//	time                 seconds, a number or [min, max] (default 0)
//	looping              restart after firing (default true)
//	randomInterval       roll the time between min and max (default true)
//	random_time_choices  [{value, weight}] weighted choice of the time
//	time_down_event      the trigger
type timer struct {
	lo, hi  float64
	looping bool
	random  bool
	choices []timeChoice
	event   Trigger
	left    int // ticks left
	done    bool
}

type timeChoice struct {
	Value  float64 `json:"value"`
	Weight int     `json:"weight"`
}

func newTimer(key string, raw json.RawMessage) (mob.Component, error) {
	j := struct {
		Time           json.RawMessage `json:"time"`
		Looping        *bool           `json:"looping"`
		RandomInterval *bool           `json:"randomInterval"`
		Choices        []timeChoice    `json:"random_time_choices"`
		Event          Trigger         `json:"time_down_event"`
	}{}
	if err := decode(raw, &j); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	t := &timer{looping: true, random: true, choices: j.Choices, event: j.Event}
	if j.Looping != nil {
		t.looping = *j.Looping
	}
	if j.RandomInterval != nil {
		t.random = *j.RandomInterval
	}
	if tm := bytes.TrimSpace(j.Time); len(tm) > 0 {
		var r rangeValue
		if err := json.Unmarshal(tm, &r); err != nil {
			return nil, fmt.Errorf("%v: time: %w", key, err)
		}
		t.lo, t.hi = r.Min, r.Max
	}
	return t, nil
}

func (t *timer) duration(m *mob.Mob) int {
	if len(t.choices) > 0 {
		total := 0
		for _, c := range t.choices {
			total += max(c.Weight, 0)
		}
		if total > 0 {
			n := m.Rand().IntN(total)
			for _, c := range t.choices {
				if n -= max(c.Weight, 0); n < 0 {
					return ticksOf(c.Value)
				}
			}
		}
	}
	if t.random && t.hi > t.lo {
		return ticksOf(t.lo + m.Rand().Float64()*(t.hi-t.lo))
	}
	return ticksOf(t.lo)
}

func (t *timer) Attach(m *mob.Mob) { t.left, t.done = t.duration(m), false }
func (t *timer) Detach(*mob.Mob)   {}

// Tick counts down and fires the event.
func (t *timer) Tick(m *mob.Mob) {
	if t.done {
		return
	}
	if t.left > 0 {
		t.left--
		return
	}
	if t.looping {
		t.left = t.duration(m)
	} else {
		t.done = true
	}
	t.event.Fire(m, nil)
}

func (t *timer) EncodeNBT() map[string]any {
	return map[string]any{"TimeLeft": int32(t.left), "Done": boolByte(t.done)}
}

func (t *timer) DecodeNBT(_ *mob.Mob, d map[string]any) {
	if v, ok := d["TimeLeft"].(int32); ok {
		t.left = int(v)
	}
	t.done = d["Done"] == uint8(1)
}

func boolByte(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}
