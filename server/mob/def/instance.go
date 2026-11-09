//go:build crossfly_full

package def

import (
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/df-mc/dragonfly/server/mob/def/molang"
	"github.com/df-mc/dragonfly/server/mob/event"
	"github.com/df-mc/dragonfly/server/mob/filter"
)

// Instance is the definition state of one entity: BDS's
// ActorDefinitionDiffList (group stack + pending property changes) plus the
// property values. It is not safe for concurrent use; an entity's world
// goroutine owns it.
//
// The stack mirrors BDS exactly. Entry 0 is the description entry
// ("+minecraft:cow" in NBT), entry 1 the base "components" block ("+"), then
// one entry per add ("+group") or remove ("-group") in the order they
// happened. The active component set is the fold over the stack: an add
// entry overrides components by name, a remove entry deletes every component
// name its group declares, wherever that component came from (base
// components included). After each change the stack is compacted like
// ActorDefinitionDiffList::_updateStack, which never changes the result.
type Instance struct {
	def   *Definition
	stack []stackEntry
	next  uint64

	active    []activeComp // fold of the stack, in component order
	activeIdx map[string]int
	dirty     bool
	// gen counts the times the active set was computed again.
	gen       uint64
	committed map[string]activeComp

	props   []any
	pending []pendingProp

	// OnChange receives the component diff on Commit when it is not empty.
	OnChange func(Diff)
	// OnPropertyChange is called for every property value that changes
	// (client sync).
	OnPropertyChange func(p *PropertyDef, v any)
	// Fallback answers Molang queries the instance does not know
	// (query.is_baby, query.variant, ...). Usually the mob.
	Fallback molang.Env

	// Commands is the log of queue_command actions handled by the default
	// HandleAction (newest last, at most 64).
	Commands []string
}

type stackEntry struct {
	add    bool
	name   string // "" for the base components, the identifier for the description entry
	serial uint64
	comps  *compGroup // nil for the description entry
}

type activeComp struct {
	entry  *compEntry
	serial uint64
}

type pendingProp struct {
	p *PropertyDef
	v any
}

// Diff is the change of the active component set between two commits.
type Diff struct {
	// Added components were not active before.
	Added []Component
	// Removed components are no longer active (the old component).
	Removed []Component
	// Reinit components are still active but now come from a newer stack
	// entry (a group was added again or a group overrode them), so their
	// instance must be rebuilt with the new JSON, like BDS re-initialises
	// components of added groups.
	Reinit []Component
}

// Empty reports whether nothing changed.
func (d Diff) Empty() bool { return len(d.Added) == 0 && len(d.Removed) == 0 && len(d.Reinit) == 0 }

// NewInstance returns the state of a new entity: no groups, property
// defaults. Nothing is committed yet; the first Commit reports every base
// component as added. Run the spawn event (or Load saved data) next.
func (d *Definition) NewInstance() *Instance {
	i := &Instance{def: d, committed: map[string]activeComp{}}
	i.stack = []stackEntry{
		{add: true, name: d.ID, serial: i.serial()},
		{add: true, name: "", serial: i.serial(), comps: d.base},
	}
	i.dirty = true
	i.props = make([]any, len(d.Properties))
	for _, p := range d.Properties {
		i.props[p.Index] = p.Default
	}
	i.InitProperties()
	return i
}

func (i *Instance) serial() uint64 { i.next++; return i.next }

// Definition returns the definition.
func (i *Instance) Definition() *Definition { return i.def }

// Event returns the named event of the definition (event.State).
func (i *Instance) Event(name string) *event.Response { return i.def.Event(name) }

var unknownGroups sync.Map

func noteUnknownGroup(id, g string) {
	if _, seen := unknownGroups.LoadOrStore(id+"\x00"+g, true); !seen {
		slog.Default().Debug("mob def: event references a component group the definition does not have", "entity", id, "group", g)
	}
}

// AddGroup pushes "+group" (ActorDefinitionDiffList::addDefinition). Groups
// the definition does not declare are ignored, as in BDS (zombie's
// "minecraft:on_foot" is such a case).
func (i *Instance) AddGroup(name string) bool { return i.push(true, name) }

// RemoveGroup pushes "-group" (removeDefinition). Removing a group that was
// never added still removes the components of that name from the base
// components, as in BDS.
func (i *Instance) RemoveGroup(name string) bool { return i.push(false, name) }

func (i *Instance) push(add bool, name string) bool {
	comps, ok := i.def.groups[name]
	if !ok {
		noteUnknownGroup(i.def.ID, name)
		return false
	}
	i.stack = append(i.stack, stackEntry{add: add, name: name, serial: i.serial(), comps: comps})
	i.compact()
	i.dirty = true
	return true
}

// compact is ActorDefinitionDiffList::_updateStack. Entries 0 and 1 are never
// touched.
func (i *Instance) compact() {
	// 1. Drop entries whose every component is named again by a later entry.
	for j := len(i.stack) - 2; j >= 2; j-- {
		covered := true
		for _, c := range groupOrder(i.stack[j].comps) {
			found := false
			for k := j + 1; k < len(i.stack); k++ {
				if i.stack[k].comps.has(c.name) {
					found = true
					break
				}
			}
			if !found {
				covered = false
				break
			}
		}
		if covered {
			i.stack = append(i.stack[:j], i.stack[j+1:]...)
		}
	}
	// 2. Drop remove entries that do not overlap any entry below them
	// (down to the base components).
	for j := len(i.stack) - 1; j >= 3; j-- {
		e := i.stack[j]
		if e.add {
			continue
		}
		overlap := false
		for k := j - 1; k >= 1 && !overlap; k-- {
			for _, c := range groupOrder(e.comps) {
				if i.stack[k].comps.has(c.name) {
					overlap = true
					break
				}
			}
		}
		if !overlap {
			i.stack = append(i.stack[:j], i.stack[j+1:]...)
		}
	}
}

func groupOrder(g *compGroup) []*compEntry {
	if g == nil {
		return nil
	}
	return g.order
}

// fold computes the active set: add entries override by name in place (a new
// name is appended), remove entries delete their names. The order is the
// order names first became active, which is stable while the set does not
// change.
func (i *Instance) fold() []activeComp {
	if !i.dirty && i.active != nil {
		return i.active
	}
	act := make([]activeComp, 0, 48)
	idx := make(map[string]int, 48)
	for _, e := range i.stack {
		if e.comps == nil {
			continue
		}
		if e.add {
			for _, c := range e.comps.order {
				if k, ok := idx[c.name]; ok {
					act[k] = activeComp{entry: c, serial: e.serial}
					continue
				}
				idx[c.name] = len(act)
				act = append(act, activeComp{entry: c, serial: e.serial})
			}
			continue
		}
		removed := false
		for _, c := range e.comps.order {
			if k, ok := idx[c.name]; ok {
				act[k].entry = nil
				delete(idx, c.name)
				removed = true
			}
		}
		if removed {
			kept := act[:0]
			for _, a := range act {
				if a.entry != nil {
					kept = append(kept, a)
				}
			}
			act = kept
			for k, a := range act {
				idx[a.entry.name] = k
			}
		}
	}
	i.active, i.activeIdx, i.dirty = act, idx, false
	i.gen++
	return act
}

// Generation changes whenever the active component set may have changed
// (mob.InstanceGeneration).
func (i *Instance) Generation() uint64 {
	i.fold()
	return i.gen
}

// HasComponent reports whether the component is active now (uncommitted
// group changes included).
func (i *Instance) HasComponent(name string) bool {
	i.fold()
	_, ok := i.activeIdx[name]
	return ok
}

// Component returns the active component.
func (i *Instance) Component(name string) (Component, bool) {
	act := i.fold()
	k, ok := i.activeIdx[name]
	if !ok {
		return Component{}, false
	}
	return Component{entry: act[k].entry}, true
}

// Components returns the active components in component order (see fold).
func (i *Instance) Components() []Component {
	act := i.fold()
	out := make([]Component, len(act))
	for k, a := range act {
		out[k] = Component{entry: a.entry}
	}
	return out
}

// HasGroup is query.had_component_group: the group's latest stack entry is
// an add. A group whose components were all overridden later may have been
// compacted away and then reads false, as in BDS.
func (i *Instance) HasGroup(name string) bool {
	for j := len(i.stack) - 1; j >= 2; j-- {
		if i.stack[j].name == name {
			return i.stack[j].add
		}
	}
	return false
}

// StackEntry is one entry of the group stack as stored in NBT.
type StackEntry struct {
	Add bool
	// Group is the component group, "" for the base components and the
	// identifier for the description entry.
	Group string
}

// String is the NBT form: "+minecraft:cow_adult", "-minecraft:cow_baby", "+".
func (e StackEntry) String() string {
	if e.Add {
		return "+" + e.Group
	}
	return "-" + e.Group
}

// Stack returns the current stack.
func (i *Instance) Stack() []StackEntry {
	out := make([]StackEntry, len(i.stack))
	for j, e := range i.stack {
		out[j] = StackEntry{Add: e.add, Group: e.name}
	}
	return out
}

// ActiveGroups returns the groups with an add entry on the stack, bottom to top.
func (i *Instance) ActiveGroups() []string {
	var out []string
	for _, e := range i.stack[2:] {
		if e.add {
			out = append(out, e.name)
		}
	}
	return out
}

// Commit applies queued property changes, then computes the component diff
// since the last commit and passes it to OnChange (event.State).
func (i *Instance) Commit() { i.CommitDiff() }

// CommitDiff is Commit returning the diff.
func (i *Instance) CommitDiff() Diff {
	pend := i.pending
	i.pending = nil
	for _, p := range pend {
		i.setNow(p.p, p.v)
	}
	act := i.fold()
	var d Diff
	for _, a := range act {
		old, ok := i.committed[a.entry.name]
		switch {
		case !ok:
			d.Added = append(d.Added, Component{entry: a.entry})
		case old.serial != a.serial:
			d.Reinit = append(d.Reinit, Component{entry: a.entry})
		}
	}
	for name, old := range i.committed {
		if _, ok := i.activeIdx[name]; !ok {
			d.Removed = append(d.Removed, Component{entry: old.entry})
		}
	}
	byName := func(s []Component) {
		sort.Slice(s, func(x, y int) bool { return s[x].Name() < s[y].Name() })
	}
	byName(d.Removed)
	i.committed = make(map[string]activeComp, len(act))
	for _, a := range act {
		i.committed[a.entry.name] = a
	}
	if !d.Empty() && i.OnChange != nil {
		i.OnChange(d)
	}
	return d
}

// InitProperties evaluates Molang property defaults (e.g. bee's
// "query.had_component_group('has_nectar')"). NewInstance calls it; call it
// again after the initial groups are set if a default depends on them.
func (i *Instance) InitProperties() {
	for _, p := range i.def.Properties {
		if p.DefaultExpr == nil {
			continue
		}
		v, err := p.Convert(p.DefaultExpr.Eval(i))
		if err != nil {
			// "Default expression '%s' did not return a ... result": keep the zero value.
			continue
		}
		i.setNow(p, v)
	}
}

// PropertyValue returns a property value: int, float64, bool or string
// (filter.PropertyHolder). Queued changes are not visible until Commit.
func (i *Instance) PropertyValue(name string) (any, bool) {
	p := i.def.Property(name)
	if p == nil {
		return nil, false
	}
	return i.props[p.Index], true
}

// Properties returns all property values by name.
func (i *Instance) Properties() map[string]any {
	out := make(map[string]any, len(i.props))
	for _, p := range i.def.Properties {
		out[p.Name] = i.props[p.Index]
	}
	return out
}

// SetProperty queues a property change, applied on the next Commit
// (ActorPropertyUtils::tryQueue*). event.State.
func (i *Instance) SetProperty(name string, v molang.Value) error {
	p := i.def.Property(name)
	if p == nil {
		return fmt.Errorf("actor %s does not have property %s", i.def.ID, name)
	}
	gv, err := p.Convert(v)
	if err != nil {
		return err
	}
	i.pending = append(i.pending, pendingProp{p: p, v: gv})
	return nil
}

// SetPropertyNow sets a property immediately. v is an int, float64, bool,
// string (enum) or molang.Value; other numeric types are converted.
func (i *Instance) SetPropertyNow(name string, v any) error {
	p := i.def.Property(name)
	if p == nil {
		return fmt.Errorf("actor %s does not have property %s", i.def.ID, name)
	}
	v = normaliseValue(p, v)
	if mv, ok := v.(molang.Value); ok {
		gv, err := p.Convert(mv)
		if err != nil {
			return err
		}
		v = gv
	}
	if !p.Valid(v) {
		return fmt.Errorf("property %s: invalid value %v", name, v)
	}
	i.setNow(p, v)
	return nil
}

func (i *Instance) setNow(p *PropertyDef, v any) {
	if i.props[p.Index] == v {
		return
	}
	i.props[p.Index] = v
	if i.OnPropertyChange != nil {
		i.OnPropertyChange(p, v)
	}
}

// Query implements molang.Env for property and group queries and forwards
// the rest to Fallback.
func (i *Instance) Query(name string, args []molang.Value) (molang.Value, bool) {
	switch name {
	case "property", "actor_property":
		if len(args) == 1 {
			if v, ok := i.PropertyValue(args[0].Str); ok {
				return propValue(v), true
			}
		}
		return molang.Value{}, true
	case "has_property", "has_actor_property":
		if len(args) == 1 {
			_, ok := i.PropertyValue(args[0].Str)
			return molang.Bool(ok), true
		}
		return molang.Value{}, true
	case "had_component_group":
		if len(args) == 1 {
			return molang.Bool(i.HasGroup(args[0].Str)), true
		}
		return molang.Value{}, true
	}
	if i.Fallback != nil {
		return i.Fallback.Query(name, args)
	}
	return molang.Value{}, false
}

func propValue(v any) molang.Value {
	switch v := v.(type) {
	case int:
		return molang.Num(float64(v))
	case float64:
		return molang.Num(v)
	case bool:
		return molang.Bool(v)
	case string:
		return molang.Str(v)
	}
	return molang.Value{}
}

// HandleAction is the default for actions the mob does not handle:
// queue_command is logged in Commands, the rest are no-ops (event.State).
func (i *Instance) HandleAction(a event.Action, _ *filter.Context) {
	if qc, ok := a.(event.QueueCommand); ok {
		i.Commands = append(i.Commands, qc.Commands...)
		if n := len(i.Commands); n > 64 {
			i.Commands = append([]string(nil), i.Commands[n-64:]...)
		}
		slog.Default().Debug("mob def: queue_command", "entity", i.def.ID, "commands", qc.Commands)
	}
}
