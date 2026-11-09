//go:build crossfly_full

package molang

import (
	"math/rand/v2"
	"testing"
)

type env struct {
	q    map[string]Value
	vars map[string]Value
	r    *rand.Rand
}

func (e *env) Query(name string, args []Value) (Value, bool) {
	if name == "property" && len(args) == 1 {
		v, ok := e.q["property:"+args[0].Str]
		return v, ok
	}
	if name == "has_any_family" {
		for _, a := range args {
			if a.Str == "zombie" {
				return Num(1), true
			}
		}
		return Num(0), true
	}
	v, ok := e.q[name]
	return v, ok
}
func (e *env) Rand() *rand.Rand { return e.r }
func (e *env) Variable(ns, name string) (Value, bool) {
	v, ok := e.vars[ns+"."+name]
	return v, ok
}
func (e *env) SetVariable(ns, name string, v Value) { e.vars[ns+"."+name] = v }

func TestEval(t *testing.T) {
	e := &env{q: map[string]Value{
		"is_baby": Num(0), "last_hit_by_player": Num(1), "equipment_count": Num(2),
		"property:minecraft:creaking_swaying_ticks": Num(5), "variant": Num(3),
		"anger_level": Num(40),
	}, vars: map[string]Value{}, r: rand.New(rand.NewPCG(1, 2))}
	cases := []struct {
		src  string
		want float64
	}{
		{"1 + 2 * 3", 7},
		{"(1 + 2) * 3", 9},
		{"10 / 4", 2.5},
		{"-2 + 5", 3},
		{"!0", 1},
		{"1 > 2 || 3 >= 3", 1},
		{"1 < 2 && 2 <= 1", 0},
		{"1 == 1.0", 1},
		{"'a' == 'a'", 1},
		{"'a' != 'b'", 1},
		{"query.last_hit_by_player ? 5 : 0", 5},
		{"query.last_hit_by_player ? 5: 0", 5},
		{"q.is_baby ? 0.02 : 0.001", 0.001},
		{"query.last_hit_by_player ? (query.is_baby ? 12 : 5) + 1 : 0", 6},
		{"!query.is_baby && query.last_hit_by_player ? 7 : 0", 7},
		{"query.last_hit_by_player ? query.variant : 0", 3},
		{"math.clamp(query.property('minecraft:creaking_swaying_ticks') + 1, 0, 6)", 6},
		{"math.clamp(query.property('minecraft:creaking_swaying_ticks') + 3, 0, 6)", 6},
		{"Math.Min(4 * 7, 100)", 28},
		{"math.max(1, 2)", 2},
		{"math.abs(-3)", 3},
		{"math.floor(2.7) + math.ceil(2.1) + math.round(2.5) + math.trunc(-2.5)", 2 + 3 + 3 - 2},
		{"math.mod(7, 3)", 1},
		{"math.pow(2, 10)", 1024},
		{"math.sqrt(16)", 4},
		{"math.lerp(0, 10, 0.5)", 5},
		{"query.anger_level(this) >= 40", 1},
		{"2.0 - math.clamp(query.anger_level / 80 * 1.5, 0, 1.5)", 1.25},
		{"query.has_any_family('blaze', 'zombie') ? -90 : 0", -90},
		{"query.unknown_query", 0},
		{"v.x = 3; t.y = v.x * 2; return t.y + 1;", 7},
		{"v.missing ?? 4", 4},
		{"v.x ?? 4", 3},
		{"1 ? 2", 2},
		{"0 ? 2", 0},
		{"true", 1},
		{"false", 0},
		{"1.5f", 1.5},
	}
	for _, c := range cases {
		x, err := Parse(c.src)
		if err != nil {
			t.Fatalf("%q: %v", c.src, err)
		}
		if got := x.Eval(e).Float(); got != c.want {
			t.Errorf("%q = %v, want %v", c.src, got, c.want)
		}
	}
}

func TestRandom(t *testing.T) {
	e := &env{r: rand.New(rand.NewPCG(7, 7))}
	ri := MustParse("math.random_integer(1, 3)")
	rf := MustParse("Math.Random(1,3)")
	dr := MustParse("Math.die_roll(2,1,3)")
	seen := map[float64]bool{}
	for i := 0; i < 1000; i++ {
		v := ri.Eval(e).Float()
		if v < 1 || v > 3 || v != float64(int(v)) {
			t.Fatalf("random_integer gave %v", v)
		}
		seen[v] = true
		if f := rf.Eval(e).Float(); f < 1 || f >= 3 {
			t.Fatalf("random gave %v", f)
		}
		if d := dr.Eval(e).Float(); d < 2 || d > 6 {
			t.Fatalf("die_roll gave %v", d)
		}
	}
	if len(seen) != 3 {
		t.Fatalf("random_integer covered %v", seen)
	}
	if ri.Constant() || !MustParse("1 + math.abs(-2)").Constant() {
		t.Fatal("Constant wrong")
	}
}

func TestParseErrors(t *testing.T) {
	for _, src := range []string{"warm", "temperate", "1 +", "(1", "math.nope(1)", "'abc"} {
		if _, err := Parse(src); err == nil {
			t.Errorf("%q: expected an error", src)
		}
	}
	x := MustParse("query.had_component_group('has_nectar') && q.is_baby")
	if q := x.Queries(); len(q) != 2 || q[0] != "had_component_group" || q[1] != "is_baby" {
		t.Fatalf("queries %v", q)
	}
}
