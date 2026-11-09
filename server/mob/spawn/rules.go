//go:build crossfly_full

package spawn

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/mob/data"
)

// Rule is one entry of a spawn rules file's "conditions" array (BDS
// MobSpawnerData: an identifier plus one MobSpawnRules). A file with three
// conditions gives three Rules for the same identifier; each is added to the
// biomes its biome filter accepts.
type Rule struct {
	// Identifier is the entity to spawn, e.g. "minecraft:zombie".
	Identifier string
	// SpawnEvent is fired instead of minecraft:entity_spawned
	// (minecraft:spawn_event).
	SpawnEvent string
	// Pool is the population_control of the file ("animal", "monster", ...).
	Pool string

	// Weight is minecraft:weight.default (100 when absent); Rarity, when > 0,
	// makes a herd spawn only one time in Rarity.
	Weight, Rarity int

	Surface, Underground, Underwater, Lava bool

	// MinBrightness..MaxBrightness is the brightness_filter range;
	// AdjustForWeather darkens the sky like a thunderstorm when it thunders.
	MinBrightness, MaxBrightness int
	AdjustForWeather             bool
	// MinMoon/MaxMoon is the moon brightness range (BDS default 0..1).
	MinMoon, MaxMoon float32

	// MinY..MaxY is the height_filter range (inclusive) of the spawn block.
	MinY, MaxY int
	// MinDifficulty..MaxDifficulty are world difficulty ids (0 peaceful .. 3 hard).
	MinDifficulty, MaxDifficulty int
	// MinDistance/MaxDistance is the distance_filter: the nearest player must
	// be at least MinDistance and at most MaxDistance blocks away (24..128
	// by default).
	MinDistance, MaxDistance int

	// DensitySurface/DensityUnderground are density_limit (-1: none): the
	// maximum of this identifier within the 9x9 chunks around the attempt.
	DensitySurface, DensityUnderground int

	// OnBlocks / PreventedBlocks are spawns_on_block_filter and
	// spawns_on_block_prevented_filter (block names; empty = any).
	OnBlocks, PreventedBlocks []string
	// AboveBlocks/AboveDistance are spawns_above_block_filter.
	AboveBlocks   []string
	AboveDistance int

	Herds        []Herd
	Permutations []Permutation

	// MinWorldAge..MaxWorldAge is world_age_filter, in seconds of world age.
	MinWorldAge, MaxWorldAge uint64
	// Delay* are delay_filter: after a spawn of DelayID the next one is
	// delayed by DelayMin..DelayMax seconds, and then only happens with a
	// DelayChance percent chance.
	DelayID              string
	DelayMin, DelayMax   int
	DelayChance          int
	MobEvent             string
	PlayerInVillage      int
	VillageBorder        int
	Persistent           bool
	Experimental         bool
	DisallowInBubble     bool
	hardcoded            func(c *conditions, e *env) bool
	biome                biomeFilter
	file                 string
	permuteWeightTotal   int
	conditionIndexInFile int
}

// Herd is one minecraft:herd entry (BDS MobSpawnHerdInfo).
type Herd struct {
	MinSize, MaxSize int
	// InitialEvent is fired on the first InitialEventCount mobs of the herd,
	// Event on every mob after the first EventSkipCount.
	InitialEvent      string
	InitialEventCount int
	Event             string
	EventSkipCount    int
}

// Permutation is one minecraft:permute_type entry. EntityType may carry a
// spawn event in angle brackets ("minecraft:pillager<minecraft:spawn_as_patrol_follower>");
// an empty EntityType keeps the identifier.
type Permutation struct {
	Weight          int
	GuaranteedCount int
	EntityType      string
}

// defaultHerd is used when a condition has no minecraft:herd.
var defaultHerd = Herd{MinSize: 1, MaxSize: 1}

const (
	minDefaultSpawnDistance = 24  // MobSpawnRules::MIN_DEFAULT_SPAWN_DISTANCE
	maxDefaultSpawnDistance = 128 // MobSpawnRules::MAX_DEFAULT_SPAWN_DISTANCE (also the cap)
	// lowEndMaxSpawnDistance is MobSpawnRules::LOW_END_MAX_SPAWN_DISTANCE:
	// with a simulation distance of 4 mobs spawn at most this far away.
	lowEndMaxSpawnDistance = 44
	maxWorldAge            = math.MaxUint64
)

func newRule() *Rule {
	return &Rule{
		Weight: 100, MaxBrightness: 15, MinMoon: 0, MaxMoon: 1,
		MinY: math.MinInt32, MaxY: math.MaxInt32,
		MinDifficulty: 0, MaxDifficulty: 3,
		MinDistance: minDefaultSpawnDistance, MaxDistance: maxDefaultSpawnDistance,
		DensitySurface: -1, DensityUnderground: -1,
		MaxWorldAge: maxWorldAge, VillageBorder: 10,
		biome: biomeAlways,
	}
}

// ParseRules parses one spawn rules file (format 1.8.0+) into its Rules.
// A file without conditions (blaze, cat, pillager, wither_skeleton:
// structure/village spawns) gives none.
func ParseRules(raw []byte) ([]*Rule, error) {
	var f struct {
		SpawnRules struct {
			Description struct {
				Identifier        string `json:"identifier"`
				PopulationControl string `json:"population_control"`
			} `json:"description"`
			Conditions []map[string]json.RawMessage `json:"conditions"`
		} `json:"minecraft:spawn_rules"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	desc := f.SpawnRules.Description
	if desc.Identifier == "" {
		return nil, fmt.Errorf("spawn rules without identifier")
	}
	out := make([]*Rule, 0, len(f.SpawnRules.Conditions))
	for i, c := range f.SpawnRules.Conditions {
		r, err := parseCondition(desc.Identifier, desc.PopulationControl, c)
		if err != nil {
			return nil, fmt.Errorf("%v condition %d: %w", desc.Identifier, i, err)
		}
		r.conditionIndexInFile = i
		out = append(out, r)
	}
	return out, nil
}

// parseCondition mirrors ActorSpawnRuleBase::loadSpawnCondition.
func parseCondition(id, pool string, c map[string]json.RawMessage) (*Rule, error) {
	r := newRule()
	r.Identifier, r.Pool = id, pool
	obj := func(key string) (map[string]json.RawMessage, bool) {
		raw, ok := c[key]
		if !ok {
			return nil, false
		}
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil {
			return nil, false // BDS: "need to be put inside a json object - using default values"
		}
		return m, true
	}
	if m, ok := obj("minecraft:weight"); ok {
		r.Weight = max(0, intOr(m["default"], 100))
		if _, ok := m["rarity"]; ok {
			r.Rarity = intOr(m["rarity"], 5)
		}
	}
	if _, ok := c["minecraft:density_limit"]; ok {
		r.DensitySurface, r.DensityUnderground = -1, -1
		if m, ok := obj("minecraft:density_limit"); ok {
			r.DensitySurface = intOr(m["surface"], -1)
			r.DensityUnderground = intOr(m["underground"], -1)
		}
	}
	r.OnBlocks = blockList(c["minecraft:spawns_on_block_filter"])
	r.PreventedBlocks = blockList(c["minecraft:spawns_on_block_prevented_filter"])
	if m, ok := obj("minecraft:spawns_above_block_filter"); ok {
		r.AboveBlocks = blockList(m["blocks"])
		r.AboveDistance = intOr(m["distance"], 0)
	}
	if raw, ok := c["minecraft:herd"]; ok {
		for _, h := range objects(raw) {
			r.Herds = append(r.Herds, Herd{
				MinSize: intOr(h["min_size"], 1), MaxSize: intOr(h["max_size"], 1),
				InitialEvent: str(h["initial_event"]), InitialEventCount: intOr(h["initial_event_count"], 0),
				Event: str(h["event"]), EventSkipCount: intOr(h["event_skip_count"], 0),
			})
		}
	}
	if raw, ok := c["minecraft:permute_type"]; ok {
		for _, p := range objects(raw) {
			perm := Permutation{Weight: intOr(p["weight"], 0), GuaranteedCount: intOr(p["guaranteed_count"], 0), EntityType: str(p["entity_type"])}
			r.Permutations = append(r.Permutations, perm)
			r.permuteWeightTotal += perm.Weight
		}
	}
	if _, ok := c["minecraft:brightness_filter"]; ok {
		r.MinBrightness, r.MaxBrightness, r.AdjustForWeather = 0, 15, false
		if m, ok := obj("minecraft:brightness_filter"); ok {
			r.MinBrightness = clamp(intOr(m["min"], 0), 0, 15)
			r.MaxBrightness = clamp(intOr(m["max"], 15), 0, 15)
			r.AdjustForWeather = boolOr(m["adjust_for_weather"], false)
		}
	}
	if m, ok := obj("minecraft:height_filter"); ok {
		r.MinY = intOr(m["min"], minDefaultSpawnDistance)
		r.MaxY = intOr(m["max"], maxDefaultSpawnDistance)
		if r.MinY > r.MaxY {
			r.MinY, r.MaxY = r.MaxY, r.MinY
		}
	}
	_, r.Surface = c["minecraft:spawns_on_surface"]
	_, r.Underground = c["minecraft:spawns_underground"]
	_, r.Underwater = c["minecraft:spawns_underwater"]
	_, r.DisallowInBubble = c["minecraft:disallow_spawns_in_bubble"]
	_, r.Lava = c["minecraft:spawns_lava"]
	if raw, ok := c["minecraft:biome_filter"]; ok {
		f, err := compileBiomeFilter(raw)
		if err != nil {
			return nil, fmt.Errorf("biome_filter: %w", err)
		}
		r.biome = f
	}
	if m, ok := obj("minecraft:difficulty_filter"); ok {
		r.MinDifficulty = difficultyOr(str(m["min"]), 0)
		r.MaxDifficulty = difficultyOr(str(m["max"]), 3)
	}
	if _, ok := c["minecraft:distance_filter"]; ok {
		lo, hi := minDefaultSpawnDistance, maxDefaultSpawnDistance
		if m, ok := obj("minecraft:distance_filter"); ok {
			lo, hi = intOr(m["min"], minDefaultSpawnDistance), intOr(m["max"], maxDefaultSpawnDistance)
			if lo > hi {
				lo, hi = minDefaultSpawnDistance, maxDefaultSpawnDistance
			}
		}
		// MobSpawnRules::setSpawnDistances: min >= 0, max capped at 128.
		r.MinDistance, r.MaxDistance = max(lo, 0), min(hi, maxDefaultSpawnDistance)
	}
	_, r.Experimental = c["minecraft:is_experimental"]
	if m, ok := obj("minecraft:world_age_filter"); ok {
		r.MinWorldAge = uint64(max(0, int64Or(m["min"], 0)))
		if _, ok := m["max"]; ok {
			r.MaxWorldAge = uint64(max(0, int64Or(m["max"], 0)))
		}
		if r.MinWorldAge > r.MaxWorldAge {
			r.MinWorldAge, r.MaxWorldAge = 0, maxWorldAge
		}
	}
	if m, ok := obj("minecraft:delay_filter"); ok {
		r.DelayMin, r.DelayMax = intOr(m["min"], 0), intOr(m["max"], 0)
		if r.DelayMin > r.DelayMax {
			r.DelayMin = r.DelayMax
		}
		r.DelayID = str(m["identifier"])
		r.DelayChance = intOr(m["spawn_chance"], 100)
	}
	if m, ok := obj("minecraft:mob_event_filter"); ok {
		r.MobEvent = str(m["event"])
	}
	if m, ok := obj("minecraft:spawn_event"); ok {
		r.SpawnEvent = str(m["event"])
	}
	_, r.Persistent = c["minecraft:is_persistent"]
	if m, ok := obj("minecraft:player_in_village_filter"); ok {
		r.PlayerInVillage = intOr(m["distance"], 0)
		r.VillageBorder = intOr(m["village_border_tolerance"], 10)
	}
	if id == "minecraft:slime" {
		// unity::_applyHardcodedSpawnRules: only the slime has a callback.
		r.hardcoded = slimeSpawnRules
	}
	if id == "minecraft:phantom" {
		// Phantoms only come to players that have not slept for days
		// (minecraft:insomnia on the player), which Dragonfly does not
		// track, and they need fly navigation (Phase 2). Never spawn them.
		r.hardcoded = func(*conditions, *env) bool { return false }
	}
	return r, nil
}

// selectHerd is MobSpawnRules::selectRandomHerd.
func (r *Rule) selectHerd(rnd randSource) Herd {
	if len(r.Herds) == 0 {
		return defaultHerd
	}
	return r.Herds[rnd.IntN(len(r.Herds))]
}

// --- registry ---

var (
	rulesMu     sync.Mutex
	extraRules  [][]byte
	rulesOnce   sync.Once
	ruleSet     *ruleIndex
	rulesFrozen bool
)

type ruleIndex struct {
	all   []*Rule
	pools map[string]string // identifier -> population_control
}

// RegisterRules adds a custom spawn rules file (a behaviour pack's
// spawn_rules/*.json) on top of the vanilla ones. A file for an identifier
// that already has rules replaces them, like a later pack in the stack.
// Call it before the first world ticks with spawning enabled (an init or
// server start-up); it panics afterwards.
func RegisterRules(raw []byte) error {
	if _, err := ParseRules(raw); err != nil {
		return err
	}
	rulesMu.Lock()
	defer rulesMu.Unlock()
	if rulesFrozen {
		panic("spawn: RegisterRules after rules were loaded")
	}
	extraRules = append(extraRules, bytes.Clone(raw))
	return nil
}

// Rules returns every loaded spawn rule (vanilla plus RegisterRules), in
// file order.
func Rules() []*Rule { return rules().all }

// PopulationControl returns the population_control pool of an identifier
// ("animal", "monster", ...), or "" when it has no spawn rules file.
func PopulationControl(id string) string { return rules().pools[id] }

func rules() *ruleIndex {
	rulesOnce.Do(func() {
		rulesMu.Lock()
		rulesFrozen = true
		extra := extraRules
		rulesMu.Unlock()
		byID := map[string][]*Rule{}
		pools := map[string]string{}
		var order []string
		add := func(raw []byte, name string) {
			rs, err := ParseRules(raw)
			if err != nil {
				panic(fmt.Sprintf("spawn: %v: %v", name, err))
			}
			id, pool := identifierOf(raw)
			if _, ok := byID[id]; !ok {
				order = append(order, id)
			}
			for _, r := range rs {
				r.file = name
			}
			byID[id], pools[id] = rs, pool
		}
		for _, f := range data.SpawnRules() {
			add(f.Data, f.Name)
		}
		for i, raw := range extra {
			add(raw, fmt.Sprintf("custom #%d", i))
		}
		idx := &ruleIndex{pools: pools}
		for _, id := range order {
			idx.all = append(idx.all, byID[id]...)
		}
		ruleSet = idx
	})
	return ruleSet
}

func identifierOf(raw []byte) (string, string) {
	var f struct {
		SpawnRules struct {
			Description struct {
				Identifier        string `json:"identifier"`
				PopulationControl string `json:"population_control"`
			} `json:"description"`
		} `json:"minecraft:spawn_rules"`
	}
	_ = json.Unmarshal(raw, &f)
	return f.SpawnRules.Description.Identifier, f.SpawnRules.Description.PopulationControl
}

// --- JSON helpers ---

func intOr(raw json.RawMessage, def int) int {
	var f float64
	if raw == nil || json.Unmarshal(raw, &f) != nil {
		return def
	}
	return int(f)
}

func int64Or(raw json.RawMessage, def int64) int64 {
	var f float64
	if raw == nil || json.Unmarshal(raw, &f) != nil {
		return def
	}
	return int64(f)
}

func boolOr(raw json.RawMessage, def bool) bool {
	var b bool
	if raw == nil || json.Unmarshal(raw, &b) != nil {
		return def
	}
	return b
}

func str(raw json.RawMessage) string {
	var s string
	if raw == nil || json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// objects reads an object or an array of objects.
func objects(raw json.RawMessage) []map[string]json.RawMessage {
	var one map[string]json.RawMessage
	if json.Unmarshal(raw, &one) == nil {
		return []map[string]json.RawMessage{one}
	}
	var many []map[string]json.RawMessage
	_ = json.Unmarshal(raw, &many)
	return many
}

// blockList reads a block descriptor list: a name, an array of names, or
// objects with "name" (BlockListSerializer). Names are normalised.
func blockList(raw json.RawMessage) []string {
	if raw == nil {
		return nil
	}
	var out []string
	add := func(s string) {
		if s != "" {
			out = append(out, normaliseBlockName(s))
		}
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		add(s)
		return out
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) != nil {
		list = []json.RawMessage{raw}
	}
	for _, e := range list {
		if json.Unmarshal(e, &s) == nil {
			add(s)
			continue
		}
		var o struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(e, &o) == nil {
			add(o.Name)
		}
	}
	return out
}

// blockAliases are the old names some vanilla spawn rules still use, mapped
// to the current Bedrock names Dragonfly encodes (BlockDescriptor resolves
// them through the block alias table in BDS).
var blockAliases = map[string]string{
	"minecraft:grass": "minecraft:grass_block",
}

func normaliseBlockName(s string) string {
	if !strings.Contains(s, ":") {
		s = "minecraft:" + s
	}
	if a, ok := blockAliases[s]; ok {
		return a
	}
	return s
}

func difficultyOr(s string, def int) int {
	switch strings.ToLower(s) {
	case "peaceful":
		return 0
	case "easy":
		return 1
	case "normal":
		return 2
	case "hard":
		return 3
	}
	return def
}

func clamp(v, lo, hi int) int { return min(max(v, lo), hi) }
