//go:build crossfly_full

package core

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/go-gl/mathgl/mgl64"
)

// ageable is minecraft:ageable: a baby grows up after duration seconds
// (default 1200), sooner when fed its feed_items (each feeding takes
// "growth" of the duration off, default 0.1), and fires grow_up when grown
// (BDS AgeableComponent/AgeableDefinition). pause_growth_items stop the
// growth and reset_growth_items start it over (golden dandelion).
type ageable struct {
	duration float64
	feed     []feedItem
	pause    []ItemDescriptor
	reset    []ItemDescriptor
	growUp   Trigger
	filter   mob.Filter

	age    int // ticks grown
	paused bool
	done   bool
}

type feedItem struct {
	desc   ItemDescriptor
	growth float64
	result string
}

func parseFeedItems(raw json.RawMessage) ([]feedItem, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, nil
	}
	if raw[0] != '[' {
		raw = append(append([]byte{'['}, raw...), ']')
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	var out []feedItem
	for _, e := range list {
		d, err := parseItemEntry(e)
		if err != nil {
			return nil, err
		}
		f := feedItem{desc: d, growth: 0.1}
		var obj struct {
			Growth     *float64 `json:"growth"`
			ResultItem string   `json:"result_item"`
		}
		if json.Unmarshal(e, &obj) == nil {
			if obj.Growth != nil {
				f.growth = *obj.Growth
			}
			f.result = obj.ResultItem
		}
		out = append(out, f)
	}
	return out, nil
}

func newAgeable(key string, raw json.RawMessage) (mob.Component, error) {
	j := struct {
		Duration *float64        `json:"duration"`
		Feed     json.RawMessage `json:"feed_items"`
		Pause    json.RawMessage `json:"pause_growth_items"`
		Reset    json.RawMessage `json:"reset_growth_items"`
		GrowUp   Trigger         `json:"grow_up"`
		Filters  json.RawMessage `json:"interact_filters"`
	}{}
	if err := decode(raw, &j); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	c := &ageable{duration: 1200, growUp: j.GrowUp}
	if j.Duration != nil {
		c.duration = *j.Duration
	}
	var err error
	if c.feed, err = parseFeedItems(j.Feed); err != nil {
		return nil, fmt.Errorf("%v: feed_items: %w", key, err)
	}
	if c.pause, err = ParseItemList(j.Pause); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	if c.reset, err = ParseItemList(j.Reset); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	if c.filter, err = mob.CompileFilter(j.Filters); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	if c.growUp.Event == "" {
		c.growUp = Trigger{Event: mob.EventGrowUp, Target: "self", Filter: mob.AlwaysTrue}
	}
	return c, nil
}

func (c *ageable) Attach(*mob.Mob) {}
func (c *ageable) Detach(*mob.Mob) {}

func (c *ageable) total() int { return ticksOf(c.duration) }

// Tick grows the baby and fires grow_up at the end.
func (c *ageable) Tick(m *mob.Mob) {
	if c.done || c.paused || c.duration < 0 {
		return
	}
	if c.age++; c.age >= c.total() {
		c.done = true
		c.growUp.Fire(m, nil)
	}
}

// Age returns the ticks grown and the ticks it takes to grow up.
func (c *ageable) Age() (grown, total int) { return c.age, c.total() }

// Interact feeds the baby.
func (c *ageable) Interact(m *mob.Mob, user item.User, held item.Stack, _ mgl64.Vec3) (item.Stack, bool) {
	if held.Empty() || c.done || m.Meta().Angry {
		return held, false
	}
	if !c.filter.Test(InteractContext(m, user, held)) {
		return held, false
	}
	if c.paused && MatchesAny(c.reset, held) {
		c.paused, c.age = false, 0
		return ConsumeOne(user, held), true
	}
	if !c.paused && MatchesAny(c.pause, held) {
		c.paused = true
		return ConsumeOne(user, held), true
	}
	for _, f := range c.feed {
		if !f.desc.Matches(held) {
			continue
		}
		if c.duration > 0 {
			c.age += int(float64(c.total()) * f.growth)
		}
		ShowAction(m, FeedAction{})
		left := ConsumeOne(user, held)
		if f.result != "" && !IsCreative(user) {
			if s, ok := ParseItemDescriptor(f.result).Stack(1); ok {
				left = ReplaceOne(m, user, held, s)
			}
		}
		if c.age >= c.total() && c.duration > 0 {
			c.done = true
			c.growUp.Fire(m, nil)
		}
		return left, true
	}
	return held, false
}

func (c *ageable) EncodeNBT() map[string]any {
	return map[string]any{"Age": int32(c.age), "Paused": boolByte(c.paused)}
}

func (c *ageable) DecodeNBT(_ *mob.Mob, d map[string]any) {
	if v, ok := d["Age"].(int32); ok {
		c.age = int(v)
	}
	c.paused = d["Paused"] == uint8(1)
}
