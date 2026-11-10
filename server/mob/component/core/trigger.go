//go:build crossfly_full

package core

import (
	"bytes"
	"encoding/json"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
)

// Trigger is an event reference in component JSON: {"event": "...",
// "target": "self"|"other"|...} or a bare event name (BDS
// ActorDefinitionTrigger). Filters are evaluated with the subject named by
// target.
type Trigger struct {
	Event  string
	Target string
	Filter mob.Filter
}

func (t *Trigger) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	var s string
	if json.Unmarshal(b, &s) == nil {
		t.Event, t.Target, t.Filter = s, "self", mob.AlwaysTrue
		return nil
	}
	var j struct {
		Event   string          `json:"event"`
		Target  string          `json:"target"`
		Filters json.RawMessage `json:"filters"`
	}
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	f, err := mob.CompileFilter(j.Filters)
	if err != nil {
		return err
	}
	t.Event, t.Target, t.Filter = j.Event, j.Target, f
	if t.Target == "" {
		t.Target = "self"
	}
	return nil
}

// Passes reports whether the trigger's filters pass for ctx.
func (t Trigger) Passes(ctx *mob.FilterContext) bool {
	return t.Filter == nil || t.Filter.Test(ctx)
}

// Fire runs the trigger: when its filters pass, the event fires on the mob
// target names (self, or another mob in ctx: other, target, parent, baby,
// player, damager). It reports whether the filters passed.
func (t Trigger) Fire(m *mob.Mob, ctx *mob.FilterContext) bool {
	if ctx == nil {
		ctx = m.Ctx()
	}
	if !t.Passes(ctx) {
		return false
	}
	if t.Event == "" {
		return true
	}
	var target world.Entity = m
	switch t.Target {
	case "block":
		// Block::executeEvent: the event goes to the block (a bee entering its hive).
		if ctx.HasBlock {
			block.ExecuteMobEvent(m.Tx(), ctx.BlockPos, t.Event, m)
		}
		return true
	case "other":
		target = ctx.Other
	case "target":
		target = ctx.Target
	case "parent":
		target = ctx.Parent
	case "baby":
		target = ctx.Baby
	case "player":
		target = ctx.Player
	case "damager":
		target = ctx.Damager
	}
	if target == nil {
		return true
	}
	if tm, ok := mob.Of(target); ok {
		c := *ctx
		if tm != m {
			c.Other = m
		}
		tm.Fire(t.Event, &c)
	}
	return true
}
