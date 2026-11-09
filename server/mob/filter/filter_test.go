//go:build crossfly_full

package filter

import (
	"encoding/json"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-gl/mathgl/mgl64"
)

type fakeActor struct {
	pos     mgl64.Vec3
	fam     []string
	comps   map[string]bool
	flags   map[Flag]bool
	nums    map[Number]float64
	rel     map[Relation]Actor
	equip   map[Domain][]Item
	props   map[string]any
	effects []string
	colour  string
}

func (a *fakeActor) Position() mgl64.Vec3 { return a.pos }
func (a *fakeActor) HasFamily(f string) bool {
	for _, x := range a.fam {
		if x == f {
			return true
		}
	}
	return false
}
func (a *fakeActor) HasComponent(n string) bool { return a.comps[n] }
func (a *fakeActor) Flag(f Flag) bool           { return a.flags[f] }
func (a *fakeActor) Number(n Number) (float64, bool) {
	v, ok := a.nums[n]
	return v, ok
}
func (a *fakeActor) Related(r Relation) Actor  { return a.rel[r] }
func (a *fakeActor) Equipment(d Domain) []Item { return a.equip[d] }
func (a *fakeActor) PropertyValue(n string) (any, bool) {
	v, ok := a.props[n]
	return v, ok
}
func (a *fakeActor) HasEffect(n string) bool {
	for _, e := range a.effects {
		if n == "" || e == n {
			return true
		}
	}
	return false
}
func (a *fakeActor) Colour() string { return a.colour }

type fakeWorld struct {
	NopWorld
	time       int64
	day        bool
	difficulty int
	bright     float64
	tags       map[string]bool
	underg     bool
	weather    Weather
	block      string
}

func (w *fakeWorld) Time() int64                                      { return w.time }
func (w *fakeWorld) Daytime() bool                                    { return w.day }
func (w *fakeWorld) Difficulty() int                                  { return w.difficulty }
func (w *fakeWorld) Brightness(mgl64.Vec3) float64                    { return w.bright }
func (w *fakeWorld) BiomeHasTag(_ mgl64.Vec3, tag string) bool        { return w.tags[tag] }
func (w *fakeWorld) Underground(mgl64.Vec3) bool                      { return w.underg }
func (w *fakeWorld) WeatherAt(mgl64.Vec3) Weather                     { return w.weather }
func (w *fakeWorld) BlockName(mgl64.Vec3) string                      { return w.block }
func (w *fakeWorld) GameRule(name string) bool                        { return name == "domobspawning" }
func (w *fakeWorld) NearestPlayerDistance(mgl64.Vec3) (float64, bool) { return 10, true }

type fakeBlock struct {
	name string
	wl   bool
}

func (b fakeBlock) Name() string         { return b.name }
func (b fakeBlock) Waterlogged() bool    { return b.wl }
func (b fakeBlock) Position() mgl64.Vec3 { return mgl64.Vec3{5, 64, 5} }

func newMob() *fakeActor {
	return &fakeActor{
		fam:   []string{"zombie", "undead", "monster", "mob"},
		comps: map[string]bool{"minecraft:is_baby": false, "minecraft:explode": false},
		flags: map[Flag]bool{}, nums: map[Number]float64{NumberHealth: 20, NumberMaxHealth: 20},
		rel: map[Relation]Actor{}, equip: map[Domain][]Item{},
		props: map[string]any{"minecraft:climate_variant": "warm", "minecraft:swaying": 3, "minecraft:flag": true, "minecraft:f": 0.5},
	}
}

func newPlayer() *fakeActor {
	p := newMob()
	p.fam = []string{"player"}
	p.equip[DomainHand] = []Item{{Name: "minecraft:shears"}}
	p.equip[DomainAny] = []Item{{Name: "minecraft:shears"}, {Name: "minecraft:diamond_helmet"}}
	p.equip[DomainHead] = []Item{{Name: "minecraft:diamond_helmet", Damage: 3}}
	return p
}

func ctxFor(self, other Actor, w *fakeWorld) *Context {
	return &Context{Self: self, Other: other, World: w, Rand: rand.New(rand.NewPCG(1, 1))}
}

func TestOperators(t *testing.T) {
	for _, c := range []struct {
		op   Operator
		a, b bool
		want bool
	}{
		{OpEqual, true, true, true}, {OpEqual, true, false, false},
		{OpNotEqual, true, false, true}, {OpGreater, true, false, true}, {OpGreater, true, true, false},
		{OpLess, false, true, true}, {OpGreaterEqual, false, true, false}, {OpGreaterEqual, true, true, true},
		{OpLessEqual, true, false, false}, {OpLessEqual, false, false, true},
	} {
		if got := cmpBool(c.op, c.a, c.b); got != c.want {
			t.Errorf("bool %v %v %v = %v", c.a, c.op, c.b, got)
		}
	}
	if !cmpInt(OpGreaterEqual, 3, 3) || cmpInt(OpLess, 3, 3) || !cmpFloat(OpLess, 0.48, 0.49) {
		t.Fatal("numeric operators")
	}
	for s, want := range map[string]Operator{"==": OpEqual, "equals": OpEqual, "not": OpNotEqual, "<>": OpNotEqual, ">=": OpGreaterEqual} {
		if got, _ := ParseOperator(s); got != want {
			t.Errorf("operator %q = %v", s, got)
		}
	}
	if got, _ := ParseOperator(float64(1)); got != OpNotEqual {
		t.Fatal("numeric operator")
	}
	if got, _ := ParseSubject(float64(1)); got != SubjectOther {
		t.Fatal("numeric subject")
	}
	if got, _ := ParseSubject(float64(5)); got != SubjectSelf {
		t.Fatal("subject 5 maps to self in BDS")
	}
	if _, err := ParseSubject("nobody"); err == nil {
		t.Fatal("bad subject accepted")
	}
}

func TestTests(t *testing.T) {
	type tc struct {
		name   string
		filter string
		setup  func(self, other *fakeActor, w *fakeWorld, ctx *Context)
		want   bool
	}
	cases := []tc{
		// is_family
		{"family other player", `{"test":"is_family","subject":"other","value":"player"}`, nil, true},
		{"family other not player", `{"test":"is_family","subject":"other","operator":"!=","value":"player"}`, nil, false},
		{"family numeric form", `{"test":"is_family","subject":1,"operator":0,"value":"player"}`, nil, true},
		{"family self", `{"test":"is_family","value":"undead"}`, nil, true},
		{"family missing subject", `{"test":"is_family","subject":"target","operator":"!=","value":"player"}`, nil, false},
		// has_component
		{"component absent", `{"test":"has_component","operator":"!=","value":"minecraft:is_baby"}`, nil, true},
		{"component present", `{"test":"has_component","value":"minecraft:is_baby"}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { s.comps["minecraft:is_baby"] = true }, true},
		{"component numeric op", `{"test":"has_component","subject":1,"operator":1,"value":"minecraft:is_baby"}`, nil, true},
		// has_equipment
		{"equipment hand shears", `{"test":"has_equipment","subject":"other","domain":"hand","value":"shears"}`, nil, true},
		{"equipment hand ns", `{"test":"has_equipment","subject":"other","domain":"hand","value":"minecraft:shears"}`, nil, true},
		{"equipment wrong item", `{"test":"has_equipment","subject":"other","domain":"hand","value":"flint_and_steel"}`, nil, false},
		{"equipment not", `{"test":"has_equipment","subject":"other","domain":"hand","operator":"not","value":"flint_and_steel"}`, nil, true},
		{"equipment head", `{"test":"has_equipment","subject":"other","domain":"head","value":"diamond_helmet"}`, nil, true},
		{"equipment aux", `{"test":"has_equipment","subject":"other","domain":"hand","value":"shears:0"}`, nil, true},
		{"damaged equipment", `{"test":"has_damaged_equipment","subject":"other","domain":"head","value":"diamond_helmet"}`, nil, true},
		{"all slots empty hand (self)", `{"test":"all_slots_empty","value":"hand"}`, nil, true},
		{"all slots empty hand not (other)", `{"test":"all_slots_empty","subject":"other","operator":"not","value":"hand"}`, nil, true},
		// water/lava/environment flags
		{"in_water default true", `{"test":"in_water"}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { s.flags[FlagInWater] = true }, true},
		{"in_water false", `{"test":"in_water","value":false}`, nil, true},
		{"in_lava == true", `{"test":"in_lava","subject":"self","operator":"==","value":true}`, nil, false},
		{"is_underwater", `{"test":"is_underwater"}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { s.flags[FlagUnderwater] = true }, true},
		{"on_fire", `{"test":"on_fire"}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { s.flags[FlagOnFire] = true }, true},
		{"on_ground", `{"test":"on_ground","value":false}`, nil, true},
		{"is_sneaking other", `{"test":"is_sneaking","subject":"other"}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { o.flags[FlagSneaking] = true }, true},
		{"is_baby", `{"test":"is_baby","value":false}`, nil, true},
		{"is_riding", `{"test":"is_riding","subject":"self","operator":"equals","value":true}`, nil, false},
		// world
		{"is_daytime", `{"test":"is_daytime","value":false}`, nil, true},
		{"is_daytime numeric", `{"test":"is_daytime","subject":0,"operator":0,"value":true}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { w.day = true }, true},
		{"brightness >", `{"test":"is_brightness","operator":">","value":0.49}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { w.bright = 0.5 }, true},
		{"brightness < (spider neutral in light)", `{"test":"is_brightness","operator":"<","value":0.49}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { w.bright = 0.49 }, false},
		{"biome tag", `{"test":"has_biome_tag","value":"spawns_warm_variant_farm_animals"}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { w.tags["spawns_warm_variant_farm_animals"] = true }, true},
		{"biome tag not", `{"test":"has_biome_tag","operator":"not","value":"nether"}`, nil, true},
		{"difficulty hard", `{"test":"is_difficulty","value":"hard"}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { w.difficulty = 3 }, true},
		{"difficulty != hard", `{"test":"is_difficulty","operator":"!=","value":"hard"}`, nil, true},
		{"difficulty numeric != peaceful", `{"test":"is_difficulty","subject":0,"operator":1,"value":0}`, nil, true},
		{"hourly clock >=", `{"test":"hourly_clock_time","operator":">=","value":12000}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { w.time = 24000*3 + 13000 }, true},
		{"hourly clock <", `{"test":"hourly_clock_time","operator":"<","value":12000}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { w.time = 13000 }, false},
		{"underground", `{"test":"is_underground","value":true}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { w.underg = true }, true},
		{"weather thunder", `{"test":"weather_at_position","value":"thunderstorm"}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { w.weather.Thunder = true }, true},
		{"in_block powder snow", `{"test":"in_block","value":"minecraft:powder_snow"}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { w.block = "minecraft:powder_snow" }, true},
		{"in_block not", `{"test":"in_block","operator":"not","value":"minecraft:powder_snow"}`, nil, true},
		{"game rule", `{"test":"is_game_rule","domain":"domobspawning","value":false}`, nil, false},
		{"distance to player", `{"test":"distance_to_nearest_player","operator":">","value":8}`, nil, true},
		// properties
		{"enum property", `{"test":"enum_property","domain":"minecraft:climate_variant","value":"warm"}`, nil, true},
		{"enum property not", `{"test":"enum_property","domain":"minecraft:climate_variant","operator":"not","value":"warm"}`, nil, false},
		{"enum property missing", `{"test":"enum_property","domain":"minecraft:nope","operator":"!=","value":"warm"}`, nil, true},
		{"int property", `{"test":"int_property","domain":"minecraft:swaying","operator":">","value":2}`, nil, true},
		{"float property", `{"test":"float_property","domain":"minecraft:f","operator":"<","value":0.6}`, nil, true},
		{"bool property default true", `{"test":"bool_property","domain":"minecraft:flag"}`, nil, true},
		{"bool property != no value", `{"test":"bool_property","domain":"minecraft:flag","operator":"!="}`, nil, false},
		{"has_property", `{"test":"has_property","value":"minecraft:flag"}`, nil, true},
		// numbers
		{"rider_count equals 0", `{"test":"rider_count","subject":"self","operator":"equals","value":0}`, nil, true},
		{"rider_count > 0", `{"test":"rider_count","operator":">","value":0}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { s.nums[NumberRiderCount] = 1 }, true},
		{"actor_health >", `{"test":"actor_health","subject":"self","operator":">","value":8}`, nil, true},
		{"actor_health target", `{"test":"actor_health","subject":"target","value":20}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { c.Target = o }, true},
		{"missing health", `{"test":"is_missing_health"}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { s.nums[NumberHealth] = 3 }, true},
		{"is_variant", `{"test":"is_variant","subject":"self","operator":"==","value":3}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { s.nums[NumberVariant] = 3 }, true},
		{"y_rotation wraps", `{"test":"y_rotation","operator":">=","value":135}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { s.nums[NumberYRotation] = 500 }, true},
		{"random_chance 1 always", `{"test":"random_chance","value":1}`, nil, true},
		// relations
		{"has_target false", `{"test":"has_target","value":false}`, nil, true},
		{"has_target", `{"test":"has_target"}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { s.rel[RelationTarget] = o }, true},
		{"is_target", `{"test":"is_target","subject":"other"}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { s.rel[RelationTarget] = o }, true},
		{"is_owner", `{"test":"is_owner","subject":"other"}`, nil, false},
		{"target_distance", `{"test":"target_distance","operator":"<=","value":3}`, func(s, o *fakeActor, w *fakeWorld, c *Context) {
			o.pos = mgl64.Vec3{2, 0, 0}
			s.rel[RelationTarget] = o
		}, true},
		{"vehicle family", `{"test":"is_vehicle_family","value":"zombiehorse"}`, nil, false},
		{"vehicle family not", `{"test":"is_vehicle_family","operator":"not","value":"zombiehorse"}`, nil, true},
		// misc
		{"mob effect", `{"test":"has_mob_effect","subject":"self","value":"weakness"}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { s.effects = []string{"weakness"} }, true},
		{"is_color", `{"test":"is_color","subject":"other","value":"silver"}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { o.colour = "light_gray" }, true},
		{"has_damage fatal", `{"test":"has_damage","value":"fatal"}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { c.Damage = &Damage{Cause: "fall", Fatal: true} }, true},
		{"has_damage cause", `{"test":"has_damage","value":"fall"}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { c.Damage = &Damage{Cause: "fall"} }, true},
		{"is_block (block subject)", `{"test":"is_block","subject":"block","value":"minecraft:bee_nest"}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { c.Block = fakeBlock{name: "minecraft:bee_nest"} }, true},
		{"is_block without block", `{"test":"is_block","subject":"block","value":"minecraft:bee_nest"}`, nil, false},
		{"is_waterlogged", `{"test":"is_waterlogged","subject":"block","operator":"==","value":false}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { c.Block = fakeBlock{name: "x"} }, true},
		// groups
		{"all_of", `{"all_of":[{"test":"is_family","subject":"other","value":"player"},{"test":"has_component","operator":"!=","value":"minecraft:explode"}]}`, nil, true},
		{"any_of empty-ish", `{"any_of":[{"test":"is_family","subject":"other","value":"cow"},{"test":"is_family","subject":"other","value":"pig"}]}`, nil, false},
		{"none_of", `{"none_of":[{"test":"is_family","subject":"other","value":"cow"}]}`, nil, true},
		{"array is all_of", `[{"test":"rider_count","subject":"self","operator":"==","value":0},{"test":"enum_property","subject":"self","domain":"minecraft:climate_variant","value":"warm"}]`, nil, true},
		{"AND/OR", `{"AND":[{"OR":[{"test":"is_family","subject":1,"operator":0,"value":"ocelot"},{"test":"is_family","subject":1,"operator":0,"value":"player"}]}]}`, nil, true},
		{"legacy other_with_families", `{"other_with_families":"player"}`, nil, true},
		{"legacy other_with_families array", `{"other_with_families":["cow","player"]}`, nil, true},
		{"legacy other_without_families", `{"other_without_families":["cow","player"]}`, nil, false},
		{"legacy day", `{"day":true}`, func(s, o *fakeActor, w *fakeWorld, c *Context) { w.day = true }, true},
		// none_of inside none_of flattens in BDS: NOT(NOT x) == NOT x
		{"none_of none_of flattens", `{"none_of":[{"none_of":[{"test":"is_family","subject":"other","value":"player"}]}]}`, nil, false},
		{"unknown test is false", `{"test":"no_such_test_xyz"}`, nil, false},
		{"unknown test in none_of passes", `{"none_of":[{"test":"no_such_test_xyz"}]}`, nil, true},
	}
	for _, c := range cases {
		self, other := newMob(), newPlayer()
		w := &fakeWorld{difficulty: 2, tags: map[string]bool{}, block: "minecraft:air"}
		ctx := ctxFor(self, other, w)
		if c.setup != nil {
			c.setup(self, other, w, ctx)
		}
		f, err := Parse(json.RawMessage(c.filter))
		if err != nil {
			t.Errorf("%s: parse: %v", c.name, err)
			continue
		}
		if got := f.Eval(ctx); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestRandomChance(t *testing.T) {
	f := MustParse(`{"test":"random_chance","value":4}`)
	ctx := ctxFor(newMob(), nil, &fakeWorld{})
	n := 0
	for i := 0; i < 40000; i++ {
		if f.Eval(ctx) {
			n++
		}
	}
	if n < 9000 || n > 11000 {
		t.Fatalf("random_chance 1/4 passed %d of 40000", n)
	}
}

// TestVanillaFilters parses every filter in the 129 vanilla entity files and
// checks that no test is unknown.
func TestVanillaFilters(t *testing.T) {
	files, _ := filepath.Glob("../def/testdata/entities/*.json")
	if len(files) != 129 {
		t.Fatalf("found %d entity files", len(files))
	}
	used := map[string]int{}
	n := 0
	for _, fn := range files {
		data, err := os.ReadFile(fn)
		if err != nil {
			t.Fatal(err)
		}
		var v any
		if err := json.Unmarshal(data, &v); err != nil {
			t.Fatal(err)
		}
		walkFilters(v, func(path string, fv any) {
			n++
			f, err := ParseValue(fv)
			if err != nil {
				t.Errorf("%s %s: %v", filepath.Base(fn), path, err)
				return
			}
			f.Walk(func(l *Leaf) {
				used[l.Name]++
				if l.Def == nil {
					t.Errorf("%s %s: unknown test %q", filepath.Base(fn), path, l.Name)
				}
			})
		})
	}
	t.Logf("%d filters, %d distinct tests used; %d tests registered", n, len(used), len(Registered()))
	if len(used) < 80 {
		t.Fatalf("only %d distinct tests found, the walker misses filters", len(used))
	}
}

func isFilterKey(k string) bool {
	return k == "filters" || k == "filter" || strings.HasSuffix(k, "_filters") || strings.HasSuffix(k, "_filter")
}

func walkFilters(v any, fn func(path string, f any)) {
	var walk func(p string, v any)
	walk = func(p string, v any) {
		switch v := v.(type) {
		case map[string]any:
			for k, x := range v {
				switch x.(type) {
				case map[string]any, []any:
					if isFilterKey(k) {
						fn(p+"/"+k, x)
						continue
					}
				}
				walk(p+"/"+k, x)
			}
		case []any:
			for _, x := range v {
				walk(p, x)
			}
		}
	}
	walk("", v)
}
