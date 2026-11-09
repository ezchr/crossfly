//go:build crossfly_full

package nav

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/world"
)

// Kind is the navigation component variant. In BDS each one is a PathNavigation subclass that
// mainly decides whether the mob is a walker, swimmer or flier.
type Kind uint8

const (
	// KindWalk is minecraft:navigation.walk (PathNavigation).
	KindWalk Kind = iota
	// KindClimb is minecraft:navigation.climb (WallClimberPathNavigation, spiders).
	KindClimb
	// KindGeneric is minecraft:navigation.generic (GenericPathNavigation): walker = can_walk,
	// swimmer = can_swim.
	KindGeneric
	// KindFloat is minecraft:navigation.float (FloatNavigation, ghasts). Pathing is the walker's;
	// BDS only overrides travel(), which belongs to the physics package.
	KindFloat
	// KindFly is minecraft:navigation.fly (FlyingPathNavigation).
	KindFly
	// KindHover is minecraft:navigation.hover (HoverPathNavigation).
	KindHover
	// KindSwim is minecraft:navigation.swim (WaterBoundPathNavigation).
	KindSwim
)

// KindFromComponent maps a component name such as "minecraft:navigation.walk" to its Kind.
func KindFromComponent(name string) (Kind, bool) {
	switch strings.TrimPrefix(name, "minecraft:") {
	case "navigation.walk":
		return KindWalk, true
	case "navigation.climb":
		return KindClimb, true
	case "navigation.generic":
		return KindGeneric, true
	case "navigation.float":
		return KindFloat, true
	case "navigation.fly":
		return KindFly, true
	case "navigation.hover":
		return KindHover, true
	case "navigation.swim":
		return KindSwim, true
	}
	return 0, false
}

func (k Kind) String() string {
	return [...]string{"walk", "climb", "generic", "float", "fly", "hover", "swim"}[k]
}

// Options are the minecraft:navigation.* JSON fields (NavigationDescription::deserializeData).
type Options struct {
	IsAmphibious        bool
	AvoidSun            bool
	CanPassDoors        bool
	CanOpenDoors        bool
	CanOpenIronDoors    bool
	CanBreakDoors       bool
	AvoidWater          bool
	AvoidDamageBlocks   bool
	CanFloat            bool // can_path_over_water, defaulting to can_float
	CanSink             bool
	CanPathOverLava     bool
	CanWalkInLava       bool
	AvoidPortals        bool
	CanWalk             bool
	CanSwim             bool
	CanBreach           bool
	CanJump             bool
	UsingDoorAnnotation bool
	CanPathFromAir      bool
	BlocksToAvoid       []BlockMatcher
}

// DefaultOptions returns the defaults BDS uses for missing keys.
func DefaultOptions() Options {
	return Options{CanPassDoors: true, CanSink: true, CanWalk: true, CanJump: true}
}

// ParseOptions parses the body of a minecraft:navigation.* component. A nil or empty message gives
// DefaultOptions.
func ParseOptions(raw json.RawMessage) (Options, error) {
	o := DefaultOptions()
	if len(raw) == 0 {
		return o, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return o, fmt.Errorf("navigation: %w", err)
	}
	b := func(key string, dst *bool) error {
		v, ok := m[key]
		if !ok {
			return nil
		}
		if err := json.Unmarshal(v, dst); err != nil {
			return fmt.Errorf("navigation: %s: %w", key, err)
		}
		return nil
	}
	fields := []struct {
		key string
		dst *bool
	}{
		{"can_float", &o.CanFloat}, {"can_path_over_water", &o.CanFloat}, {"can_path_over_lava", &o.CanPathOverLava},
		{"can_walk_in_lava", &o.CanWalkInLava}, {"can_sink", &o.CanSink}, {"avoid_sun", &o.AvoidSun},
		{"can_pass_doors", &o.CanPassDoors}, {"can_open_doors", &o.CanOpenDoors}, {"can_open_iron_doors", &o.CanOpenIronDoors},
		{"can_break_doors", &o.CanBreakDoors}, {"avoid_water", &o.AvoidWater}, {"avoid_damage_blocks", &o.AvoidDamageBlocks},
		{"avoid_portals", &o.AvoidPortals}, {"is_amphibious", &o.IsAmphibious}, {"can_walk", &o.CanWalk},
		{"can_swim", &o.CanSwim}, {"can_breach", &o.CanBreach}, {"can_jump", &o.CanJump},
		{"using_door_annotation", &o.UsingDoorAnnotation}, {"can_path_from_air", &o.CanPathFromAir},
	}
	for _, f := range fields {
		if err := b(f.key, f.dst); err != nil {
			return o, err
		}
	}
	if v, ok := m["blocks_to_avoid"]; ok {
		list, err := parseBlockList(v)
		if err != nil {
			return o, fmt.Errorf("navigation: blocks_to_avoid: %w", err)
		}
		o.BlocksToAvoid = list
	}
	return o, nil
}

// PreferredPath is minecraft:preferred_path (PreferredPathDescription). BDS uses a default instance
// (PathFinder::DEFAULT_DESC) when the mob has none.
type PreferredPath struct {
	DefaultBlockCost float64
	JumpCost         float64
	MaxFallBlocks    int
	Blocks           []PreferredBlocks
}

// PreferredBlocks is one preferred_path_blocks entry: the cost of walking on any of Blocks.
type PreferredBlocks struct {
	Cost   float64
	Blocks []BlockMatcher
}

// DefaultPreferredPath is PathFinder::DEFAULT_DESC.
func DefaultPreferredPath() PreferredPath { return PreferredPath{MaxFallBlocks: 3} }

// ParsePreferredPath parses a minecraft:preferred_path component.
func ParsePreferredPath(raw json.RawMessage) (PreferredPath, error) {
	p := DefaultPreferredPath()
	var v struct {
		DefaultBlockCost *float64 `json:"default_block_cost"`
		JumpCost         *float64 `json:"jump_cost"`
		MaxFallBlocks    *int     `json:"max_fall_blocks"`
		Blocks           []struct {
			Cost   float64         `json:"cost"`
			Blocks json.RawMessage `json:"blocks"`
		} `json:"preferred_path_blocks"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return p, fmt.Errorf("preferred_path: %w", err)
	}
	if v.DefaultBlockCost != nil {
		p.DefaultBlockCost = *v.DefaultBlockCost
	}
	if v.JumpCost != nil {
		p.JumpCost = *v.JumpCost
	}
	if v.MaxFallBlocks != nil {
		p.MaxFallBlocks = *v.MaxFallBlocks
	}
	for _, e := range v.Blocks {
		list, err := parseBlockList(e.Blocks)
		if err != nil {
			return p, fmt.Errorf("preferred_path: %w", err)
		}
		p.Blocks = append(p.Blocks, PreferredBlocks{Cost: e.Cost, Blocks: list})
	}
	return p, nil
}

// BlockMatcher is a block descriptor from the JSON: a name, optionally with states that must
// match. Tag descriptors are kept but never match (Dragonfly has no block tags).
type BlockMatcher struct {
	Name   string
	States map[string]any
	Tags   string
}

// Matches reports whether b is the described block.
func (m BlockMatcher) Matches(b world.Block) bool {
	if m.Name == "" {
		return false
	}
	name, props := b.EncodeBlock()
	if name != m.Name {
		return false
	}
	for k, want := range m.States {
		got, ok := props[k]
		if !ok || fmt.Sprint(got) != fmt.Sprint(want) {
			return false
		}
	}
	return true
}

func parseBlockList(raw json.RawMessage) ([]BlockMatcher, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		// A single descriptor instead of a list.
		items = []json.RawMessage{raw}
	}
	out := make([]BlockMatcher, 0, len(items))
	for _, it := range items {
		var s string
		if json.Unmarshal(it, &s) == nil {
			out = append(out, BlockMatcher{Name: normaliseName(s)})
			continue
		}
		var obj struct {
			Name   string         `json:"name"`
			States map[string]any `json:"states"`
			Tags   string         `json:"tags"`
		}
		if err := json.Unmarshal(it, &obj); err != nil {
			return nil, err
		}
		m := BlockMatcher{States: obj.States, Tags: obj.Tags}
		if obj.Name != "" {
			m.Name = normaliseName(obj.Name)
		}
		out = append(out, m)
	}
	return out, nil
}

func normaliseName(s string) string {
	if !strings.Contains(s, ":") {
		return "minecraft:" + s
	}
	return s
}
