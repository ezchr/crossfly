//go:build crossfly_full

package event

import (
	"log/slog"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/mob/def/molang"
	"github.com/df-mc/dragonfly/server/mob/filter"
)

// State is the definition-side state of an entity that events change. The
// def package's *Instance implements it.
type State interface {
	// Event returns the named event of the entity's definition, or nil.
	Event(name string) *Response
	// AddGroup pushes a component group on the stack; false if the
	// definition has no such group.
	AddGroup(name string) bool
	// RemoveGroup pushes a removal of the group; false if there is no such group.
	RemoveGroup(name string) bool
	// SetProperty queues a property change. It fails for unknown properties,
	// wrong types and out-of-range numbers (BDS logs and ignores those).
	SetProperty(name string, v molang.Value) error
	// Commit applies queued property changes and publishes the component
	// diff of the group changes made since the last commit.
	Commit()
	// HandleAction is the fallback for actions the entity itself does not
	// handle (queue_command, play_sound, ...).
	HandleAction(a Action, ctx *filter.Context)
	// Query resolves Molang queries for set_property expressions.
	molang.Env
}

// Entity is an actor that runs events.
type Entity interface {
	filter.Actor
	// EventState returns the entity's definition state, or nil if the entity
	// cannot run events (removed, or not data driven).
	EventState() State
}

// ActionHandler is optional on an Entity: return true when the action was
// handled; false falls back to State.HandleAction.
type ActionHandler interface {
	HandleAction(a Action, ctx *filter.Context) bool
}

// BlockEventHandler is optional on an Entity: it handles triggers with target
// "block" (Block::executeEvent at the context block position).
type BlockEventHandler interface {
	BlockEvent(event string, ctx *filter.Context)
}

// NameTagger is optional on an Entity. BDS keys its event loop detection on
// (event name, name tag), so an unnamed entity cannot run an event that is
// already in the current chain, even on another entity.
type NameTagger interface {
	NameTag() string
}

// Modifier is what one executed leaf contributes (ActorDefinitionModifier).
type Modifier struct {
	Add, Remove []string
	Trigger     *Trigger
	Actions     []Action
}

// Logger receives loop-detection and property errors.
var Logger = slog.Default()

type chainKey struct{ event, name string }

// Chain is the event chain of one top-level Fire. It is shared by all events
// triggered from it, like the vector BDS passes through _executeEvent; keys
// are never removed, so one chain runs each (event, name tag) at most once.
type Chain struct{ keys []chainKey }

// Fire runs the named event on e (ActorDefinitionDescriptor::executeEvent).
// ctx.Self is set to e if nil. It returns false when the event does not exist
// or a loop was detected.
func Fire(e Entity, name string, ctx *filter.Context) bool {
	if ctx == nil {
		ctx = &filter.Context{}
	}
	if ctx.Self == nil {
		ctx = ctx.WithSelf(e)
	}
	return FireChain(e, name, ctx, &Chain{})
}

// FireChain is Fire with an existing chain.
func FireChain(e Entity, name string, ctx *filter.Context, chain *Chain) bool {
	st := e.EventState()
	if st == nil {
		return false
	}
	key := chainKey{event: name}
	if n, ok := e.(NameTagger); ok {
		key.name = n.NameTag()
	}
	for _, k := range chain.keys {
		if k == key {
			if Logger != nil {
				Logger.Warn("mob event: detected loop in event execution, aborting", "event", name)
			}
			return false
		}
	}
	chain.keys = append(chain.keys, key)

	r := st.Event(name)
	if r == nil {
		return false
	}
	var mods []Modifier
	if r.Filters.Eval(ctx) {
		Collect(r, ctx, &mods)
	}
	for _, m := range mods {
		for _, g := range m.Remove {
			st.RemoveGroup(g)
		}
		for _, g := range m.Add {
			st.AddGroup(g)
		}
		for _, a := range m.Actions {
			runAction(e, st, a, ctx)
		}
	}
	st.Commit()
	for _, m := range mods {
		if m.Trigger != nil && m.Trigger.Filters.Eval(ctx) {
			trigger(e, m.Trigger, ctx, chain)
		}
	}
	return true
}

// Collect is ActorDefinitionEvent::_evaluateGroups: it walks the response
// tree and appends the leaves to run. The children's filters of a sequence
// are all evaluated before any child runs, and nothing runs until the whole
// event is collected, so every filter of one event sees the state from
// before the event.
func Collect(r *Response, ctx *filter.Context, out *[]Modifier) {
	switch r.Kind {
	case Leaf:
		*out = append(*out, Modifier{Add: r.Add, Remove: r.Remove, Trigger: r.Trigger, Actions: r.Actions})
	case Sequence:
		pass := make([]*Response, 0, len(r.Children))
		for _, c := range r.Children {
			if c.Filters.Eval(ctx) {
				pass = append(pass, c)
			}
		}
		for _, c := range pass {
			Collect(c, ctx, out)
		}
	case Randomize:
		// Children whose filters fail are excluded from the draw.
		var pass []*Response
		total := float32(0)
		for _, c := range r.Children {
			if c.Filters.Eval(ctx) {
				pass = append(pass, c)
				total += float32(c.Weight)
			}
		}
		if len(pass) == 0 {
			return
		}
		x := randFloat(ctx) * total
		for _, c := range pass {
			w := float32(c.Weight)
			if x <= w {
				Collect(c, ctx, out)
				return
			}
			x -= w
		}
	case FirstValid:
		for _, c := range r.Children {
			if c.Filters.Eval(ctx) {
				Collect(c, ctx, out)
				return
			}
		}
	}
}

func randFloat(ctx *filter.Context) float32 {
	if ctx.Rand != nil {
		return ctx.Rand.Float32()
	}
	return rand.Float32()
}

func runAction(e Entity, st State, a Action, ctx *filter.Context) {
	if sp, ok := a.(SetProperty); ok {
		for _, v := range sp.Values {
			if err := st.SetProperty(v.Name, v.Value(st)); err != nil && Logger != nil {
				Logger.Warn("mob event: set_property failed", "property", v.Name, "err", err)
			}
		}
		return
	}
	if h, ok := e.(ActionHandler); ok && h.HandleAction(a, ctx) {
		return
	}
	st.HandleAction(a, ctx)
}

// trigger is ActorDefinitionDescriptor::_forceExecuteTrigger.
func trigger(e Entity, t *Trigger, ctx *filter.Context, chain *Chain) {
	switch t.Target {
	case filter.SubjectSelf:
		FireChain(e, t.Event, ctx, chain)
	case filter.SubjectOther:
		if o, ok := ctx.Other.(Entity); ok {
			FireChain(o, t.Event, ctx.WithSelf(o), chain)
		}
	case filter.SubjectTarget:
		var tg filter.Actor
		if rel, ok := e.(filter.Related); ok {
			tg = rel.Related(filter.RelationTarget)
		}
		if tg == nil {
			tg = ctx.Target
		}
		if o, ok := tg.(Entity); ok {
			FireChain(o, t.Event, ctx.WithSelf(o), chain)
		}
	case filter.SubjectBaby:
		// BDS builds a fresh parameter list: self = the baby, other = the parent.
		if b, ok := ctx.Baby.(Entity); ok {
			nctx := &filter.Context{Self: b, Other: e, World: ctx.World, Rand: ctx.Rand}
			FireChain(b, t.Event, nctx, chain)
		}
	case filter.SubjectBlock:
		if h, ok := e.(BlockEventHandler); ok && ctx.Block != nil {
			h.BlockEvent(t.Event, ctx)
		}
	}
}
