//go:build crossfly_full

// Package debugcmd is /mobdebug for servers running definition-driven mobs:
// it shows the looked-at mob's goals (priority, kind, flags, running, their
// appendDebugInfo text), its current path as particles, its active
// component groups and components, and which of its components have no
// implementation yet. Register it from the server:
//
//	cmd.Register(debugcmd.Command(func(src cmd.Source) bool { return isAdmin(src) }))
//
// Describe is the same report as data, also used by the mobtest kit to
// explain failing scenario tests.
package debugcmd

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/go-gl/mathgl/mgl64"
)

// GoalInfo is one goal of a mob's selector.
type GoalInfo struct {
	Key      string
	Priority int
	Target   bool
	Running  bool
	Flags    mob.ControlFlags
	// Debug is the goal's AppendDebugInfo text (empty without one).
	Debug string
}

// Info is a snapshot of one mob for debugging.
type Info struct {
	Identifier string
	Pos        mgl64.Vec3
	Yaw        float64
	HeadYaw    float64
	BodyYaw    float64
	Health     float64
	MaxHealth  float64
	Dead       bool
	Ticks      int64
	Target     string
	Physics    mob.Physics
	Speed      float64

	Goals []GoalInfo
	// SelectorDebug is GoalSelector.DebugInfo (BDS buildDebugInfo).
	SelectorDebug string

	// Groups are the added component groups, when the linked definition
	// package exposes them (GroupsKnown).
	Groups      []string
	GroupsKnown bool
	// Components are the active component keys in definition order.
	Components []string
	// Unsupported are the active keys without a registered factory.
	Unsupported []string
	// Properties are the actor properties and their values.
	Properties []mob.PropertyValue

	// Path is the navigation path being followed, if any.
	Path         []cube.Pos
	PathIndex    int
	PathComplete bool
	Wanted       mgl64.Vec3
	HasWanted    bool

	Meta mob.Meta
}

// The definition package (A1) may expose the added groups under one of these
// names on its Instance; Describe falls back to the instance NBT.
type (
	groupsLister       interface{ Groups() []string }
	activeGroupsLister interface{ ActiveGroups() []string }
	compGroupsLister   interface{ ComponentGroups() []string }
)

// Describe takes a snapshot of m. It must run in m's transaction.
func Describe(m *mob.Mob) Info {
	i := Info{
		Identifier: m.Identifier(), Pos: m.Pos(), Yaw: m.Yaw(), HeadYaw: m.HeadYaw(), BodyYaw: m.BodyYaw(),
		Health: m.Health(), MaxHealth: m.MaxHealth(), Dead: m.Dead(), Ticks: m.Ticks(),
		Physics: *m.Physics(), Speed: m.Speed(), Meta: m.Meta(),
		Components: m.Components(), Properties: m.Properties(),
	}
	if t, ok := m.Target(); ok {
		i.Target = fmt.Sprintf("%v at %.1f blocks", entityName(t.H().Type().EncodeEntity()), t.Position().Sub(m.Pos()).Len())
	}
	sel := m.Goals()
	for _, key := range sel.Keys() {
		g, _ := sel.Goal(key)
		gi := GoalInfo{Key: key, Running: sel.Running(key)}
		if raw, ok := m.ComponentJSON(key); ok {
			if spec, err := mob.ParseGoalSpec(key, raw); err == nil {
				gi.Priority = spec.Priority
			}
		}
		if g != nil {
			gi.Flags = g.Flags()
			if t, ok := g.(mob.TargetGoal); ok {
				gi.Target = t.IsTargetGoal()
			}
			if d, ok := g.(mob.GoalDebugger); ok {
				var b strings.Builder
				d.AppendDebugInfo(&b)
				gi.Debug = strings.TrimSpace(b.String())
			}
		}
		i.Goals = append(i.Goals, gi)
	}
	sort.SliceStable(i.Goals, func(a, b int) bool { return i.Goals[a].Priority < i.Goals[b].Priority })
	i.SelectorDebug = sel.DebugInfo()
	for _, k := range i.Components {
		if !mob.Supported(k) {
			i.Unsupported = append(i.Unsupported, k)
		}
	}
	i.Groups, i.GroupsKnown = groupsOf(m.Instance())
	if p := m.Navigation().Path(); p != nil {
		i.PathIndex, i.PathComplete = p.Index(), p.Complete()
		for n := range p.Len() {
			i.Path = append(i.Path, p.Node(n))
		}
	}
	if mc := m.MoveControl(); mc.HasWanted() {
		i.Wanted, i.HasWanted = mc.WantedPosition(), true
	}
	return i
}

func groupsOf(inst mob.Instance) ([]string, bool) {
	switch g := inst.(type) {
	case nil:
		return nil, false
	case groupsLister:
		return g.Groups(), true
	case activeGroupsLister:
		return g.ActiveGroups(), true
	case compGroupsLister:
		return g.ComponentGroups(), true
	}
	// Fall back to the saved state: any list of strings under a key naming
	// groups.
	for k, v := range inst.EncodeNBT() {
		if !strings.Contains(strings.ToLower(k), "group") {
			continue
		}
		switch v := v.(type) {
		case []string:
			return slices.Clone(v), true
		case []any:
			var out []string
			for _, e := range v {
				if s, ok := e.(string); ok {
					out = append(out, s)
				}
			}
			return out, true
		}
	}
	return nil, false
}

func entityName(id string) string { return strings.TrimPrefix(id, "minecraft:") }

// String is a multi-line report of everything in i.
func (i Info) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%v at (%.2f, %.2f, %.2f) yaw %.0f head %.0f body %.0f, health %.1f/%.1f, tick %d",
		entityName(i.Identifier), i.Pos[0], i.Pos[1], i.Pos[2], i.Yaw, i.HeadYaw, i.BodyYaw, i.Health, i.MaxHealth, i.Ticks)
	if i.Dead {
		b.WriteString(" (dead)")
	}
	fmt.Fprintf(&b, "\nphysics: ground=%v hcoll=%v water=%v lava=%v fall=%.2f speed=%.3f",
		i.Physics.OnGround, i.Physics.HorizontalCollision, i.Physics.InWater, i.Physics.InLava, i.Physics.FallDistance, i.Speed)
	if i.Target != "" {
		fmt.Fprintf(&b, "\ntarget: %v", i.Target)
	}
	b.WriteString("\n")
	b.WriteString(i.GoalsString())
	b.WriteString(i.PathString())
	b.WriteString(i.GroupsString())
	return b.String()
}

// GoalsString lists the goals, most important first; running ones marked *.
func (i Info) GoalsString() string {
	var b strings.Builder
	fmt.Fprintf(&b, "goals (%d):\n", len(i.Goals))
	for _, g := range i.Goals {
		mark, kind := " ", ""
		if g.Running {
			mark = "*"
		}
		if g.Target {
			kind = " [target]"
		}
		fmt.Fprintf(&b, " %s %2d %v%v {%v}", mark, g.Priority, strings.TrimPrefix(g.Key, "minecraft:behavior."), kind, g.Flags)
		if g.Debug != "" && g.Debug != g.Key {
			fmt.Fprintf(&b, ": %v", strings.ReplaceAll(g.Debug, "\n", " "))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// PathString describes the path being followed.
func (i Info) PathString() string {
	if len(i.Path) == 0 {
		if i.HasWanted {
			return fmt.Sprintf("path: none, wanted (%.1f, %.1f, %.1f)\n", i.Wanted[0], i.Wanted[1], i.Wanted[2])
		}
		return "path: none\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "path: %d nodes, at %d, complete=%v:", len(i.Path), i.PathIndex, i.PathComplete)
	for n, p := range i.Path {
		if n == i.PathIndex {
			b.WriteString(" >")
		} else {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%d,%d,%d", p[0], p[1], p[2])
	}
	b.WriteByte('\n')
	return b.String()
}

// GroupsString lists the component groups, components (unsupported marked
// !) and properties.
func (i Info) GroupsString() string {
	var b strings.Builder
	if i.GroupsKnown {
		fmt.Fprintf(&b, "groups: %v\n", strings.Join(i.Groups, ", "))
	} else {
		b.WriteString("groups: (not exposed by the definition package)\n")
	}
	fmt.Fprintf(&b, "components (%d, %d unsupported):", len(i.Components), len(i.Unsupported))
	for _, c := range i.Components {
		b.WriteByte(' ')
		if slices.Contains(i.Unsupported, c) {
			b.WriteByte('!')
		}
		b.WriteString(strings.TrimPrefix(c, "minecraft:"))
	}
	b.WriteByte('\n')
	if len(i.Properties) > 0 {
		b.WriteString("properties:")
		for _, p := range i.Properties {
			fmt.Fprintf(&b, " %v=%v", p.Decl.Name, p.Value)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// UnsupportedReport is the server-wide list of component keys live mobs
// had without a factory (most used first) and the factory errors.
func UnsupportedReport(limit int) string {
	seen := mob.UnsupportedSeen()
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool {
		if seen[keys[a]] != seen[keys[b]] {
			return seen[keys[a]] > seen[keys[b]]
		}
		return keys[a] < keys[b]
	})
	var b strings.Builder
	fmt.Fprintf(&b, "unsupported components seen: %d\n", len(keys))
	for n, k := range keys {
		if limit > 0 && n >= limit {
			fmt.Fprintf(&b, " ... %d more\n", len(keys)-n)
			break
		}
		fmt.Fprintf(&b, " %5d %v\n", seen[k], k)
	}
	errs := mob.FactoryErrors()
	if len(errs) > 0 {
		ek := make([]string, 0, len(errs))
		for k := range errs {
			ek = append(ek, k)
		}
		sort.Strings(ek)
		fmt.Fprintf(&b, "factory errors: %d\n", len(errs))
		for n, k := range ek {
			if limit > 0 && n >= limit {
				fmt.Fprintf(&b, " ... %d more\n", len(ek)-n)
				break
			}
			fmt.Fprintf(&b, " %v: %v\n", k, errs[k])
		}
	}
	return b.String()
}
