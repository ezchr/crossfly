//go:build crossfly_full

package molang

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"strings"
)

// Env resolves query.* calls. Name is the lower-cased query name without the
// query./q. prefix. ok=false means the query is unknown to this Env; the
// expression then reads 0, which is what Molang does for unknown queries.
type Env interface {
	Query(name string, args []Value) (v Value, ok bool)
}

// RandSource is an optional Env extension that supplies the random source for
// math.random and friends. Without it the global math/rand/v2 source is used.
type RandSource interface {
	Rand() *rand.Rand
}

// Variables is an optional Env extension for variable./context. reads and
// writes. temp. variables live only for one evaluation.
type Variables interface {
	Variable(ns, name string) (Value, bool)
	SetVariable(ns, name string, v Value)
}

// EnvFunc adapts a function to Env.
type EnvFunc func(name string, args []Value) (Value, bool)

// Query ...
func (f EnvFunc) Query(name string, args []Value) (Value, bool) { return f(name, args) }

// Expr is a parsed Molang expression. It is immutable and safe for concurrent
// use.
type Expr struct {
	src     string
	root    node
	queries []string
}

// Parse parses src.
func Parse(src string) (*Expr, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks, qs: map[string]struct{}{}}
	root, err := p.program()
	if err != nil {
		return nil, fmt.Errorf("%w (in %q)", err, src)
	}
	e := &Expr{src: src, root: root}
	for q := range p.qs {
		e.queries = append(e.queries, q)
	}
	sort.Strings(e.queries)
	return e, nil
}

// MustParse is Parse that panics on error. Use it for constants only.
func MustParse(src string) *Expr {
	e, err := Parse(src)
	if err != nil {
		panic(err)
	}
	return e
}

// Source returns the original text.
func (e *Expr) Source() string { return e.src }

// Queries returns the query names the expression calls, sorted.
func (e *Expr) Queries() []string { return e.queries }

// Constant reports whether the expression has no queries, variables or
// random calls, so it evaluates to the same value every time.
func (e *Expr) Constant() bool { return constant(e.root) }

func constant(n node) bool {
	switch n := n.(type) {
	case numNode, strNode:
		return true
	case unaryNode:
		return constant(n.x)
	case binNode:
		return constant(n.l) && constant(n.r)
	case ternNode:
		return constant(n.cond) && constant(n.a) && (n.b == nil || constant(n.b))
	case callNode:
		if n.ns != "math" || strings.HasPrefix(n.name, "random") || strings.HasPrefix(n.name, "die_roll") {
			return false
		}
		for _, a := range n.args {
			if !constant(a) {
				return false
			}
		}
		return true
	}
	return false
}

// Eval evaluates the expression. env may be nil.
func (e *Expr) Eval(env Env) Value {
	ev := &evaluator{env: env}
	return ev.eval(e.root)
}

type evaluator struct {
	env  Env
	temp map[string]Value
	ret  bool
}

func (ev *evaluator) rnd() *rand.Rand {
	if rs, ok := ev.env.(RandSource); ok {
		if r := rs.Rand(); r != nil {
			return r
		}
	}
	return nil
}

func (ev *evaluator) float64() float64 {
	if r := ev.rnd(); r != nil {
		return r.Float64()
	}
	return rand.Float64()
}

func (ev *evaluator) intN(n int) int {
	if n <= 0 {
		return 0
	}
	if r := ev.rnd(); r != nil {
		return r.IntN(n)
	}
	return rand.IntN(n)
}

func (ev *evaluator) eval(n node) Value {
	switch n := n.(type) {
	case numNode:
		return Num(float64(n))
	case strNode:
		return Str(string(n))
	case thisNode:
		return Value{}
	case unaryNode:
		x := ev.eval(n.x)
		if n.op == "!" {
			return Bool(!x.Truthy())
		}
		return Num(-x.Float())
	case binNode:
		return ev.binary(n)
	case ternNode:
		if ev.eval(n.cond).Truthy() {
			return ev.eval(n.a)
		}
		if n.b == nil {
			return Value{}
		}
		return ev.eval(n.b)
	case assignNode:
		v := ev.eval(n.x)
		if n.ns == "temp" {
			if ev.temp == nil {
				ev.temp = map[string]Value{}
			}
			ev.temp[n.name] = v
		} else if vs, ok := ev.env.(Variables); ok {
			vs.SetVariable(n.ns, n.name, v)
		}
		return v
	case callNode:
		return ev.call(n)
	case *seqNode:
		var last Value
		for i, s := range n.stmts {
			last = ev.eval(s)
			if n.ret[i] {
				return last
			}
		}
		// A multi-statement expression without return evaluates to 0.
		if len(n.stmts) > 1 {
			return Value{}
		}
		return last
	}
	return Value{}
}

func (ev *evaluator) binary(n binNode) Value {
	switch n.op {
	case "&&":
		return Bool(ev.eval(n.l).Truthy() && ev.eval(n.r).Truthy())
	case "||":
		return Bool(ev.eval(n.l).Truthy() || ev.eval(n.r).Truthy())
	case "??":
		// The left side is "missing" when it is an unknown variable or query.
		if c, ok := n.l.(callNode); ok {
			if v, ok := ev.lookup(c); ok {
				return v
			}
			return ev.eval(n.r)
		}
		return ev.eval(n.l)
	}
	l, r := ev.eval(n.l), ev.eval(n.r)
	switch n.op {
	case "==":
		return Bool(l.Equal(r))
	case "!=":
		return Bool(!l.Equal(r))
	case "<":
		return Bool(l.Float() < r.Float())
	case "<=":
		return Bool(l.Float() <= r.Float())
	case ">":
		return Bool(l.Float() > r.Float())
	case ">=":
		return Bool(l.Float() >= r.Float())
	case "+":
		return Num(l.Float() + r.Float())
	case "-":
		return Num(l.Float() - r.Float())
	case "*":
		return Num(l.Float() * r.Float())
	case "/":
		d := r.Float()
		if d == 0 {
			return Value{}
		}
		return Num(l.Float() / d)
	}
	return Value{}
}

// lookup resolves a variable or query and reports whether it exists.
func (ev *evaluator) lookup(c callNode) (Value, bool) {
	switch c.ns {
	case "temp":
		v, ok := ev.temp[c.name]
		return v, ok
	case "variable", "context":
		if vs, ok := ev.env.(Variables); ok {
			return vs.Variable(c.ns, c.name)
		}
		return Value{}, false
	case "query":
		if ev.env == nil {
			return Value{}, false
		}
		args := make([]Value, len(c.args))
		for i, a := range c.args {
			args[i] = ev.eval(a)
		}
		return ev.env.Query(c.name, args)
	}
	return ev.call(c), true
}

func (ev *evaluator) call(c callNode) Value {
	if c.ns != "math" {
		v, _ := ev.lookup(c)
		return v
	}
	if c.name == "pi" {
		return Num(math.Pi)
	}
	args := make([]float64, len(c.args))
	for i, a := range c.args {
		args[i] = ev.eval(a).Float()
	}
	f := mathFuncs[c.name]
	if f.n >= 0 && len(args) < f.n {
		return Value{}
	}
	return Num(f.fn(ev, args))
}

type mathFunc struct {
	n  int // minimum argument count
	fn func(ev *evaluator, a []float64) float64
}

const deg = math.Pi / 180

var mathFuncs = map[string]mathFunc{
	"abs":   {1, func(_ *evaluator, a []float64) float64 { return math.Abs(a[0]) }},
	"acos":  {1, func(_ *evaluator, a []float64) float64 { return math.Acos(a[0]) / deg }},
	"asin":  {1, func(_ *evaluator, a []float64) float64 { return math.Asin(a[0]) / deg }},
	"atan":  {1, func(_ *evaluator, a []float64) float64 { return math.Atan(a[0]) / deg }},
	"atan2": {2, func(_ *evaluator, a []float64) float64 { return math.Atan2(a[0], a[1]) / deg }},
	"ceil":  {1, func(_ *evaluator, a []float64) float64 { return math.Ceil(a[0]) }},
	"clamp": {3, func(_ *evaluator, a []float64) float64 { return math.Min(math.Max(a[0], a[1]), a[2]) }},
	"cos":   {1, func(_ *evaluator, a []float64) float64 { return math.Cos(a[0] * deg) }},
	"sin":   {1, func(_ *evaluator, a []float64) float64 { return math.Sin(a[0] * deg) }},
	"exp":   {1, func(_ *evaluator, a []float64) float64 { return math.Exp(a[0]) }},
	"ln":    {1, func(_ *evaluator, a []float64) float64 { return math.Log(a[0]) }},
	"floor": {1, func(_ *evaluator, a []float64) float64 { return math.Floor(a[0]) }},
	"round": {1, func(_ *evaluator, a []float64) float64 { return math.Floor(a[0] + 0.5) }},
	"trunc": {1, func(_ *evaluator, a []float64) float64 { return math.Trunc(a[0]) }},
	"sqrt":  {1, func(_ *evaluator, a []float64) float64 { return math.Sqrt(a[0]) }},
	"pow":   {2, func(_ *evaluator, a []float64) float64 { return math.Pow(a[0], a[1]) }},
	"max":   {2, func(_ *evaluator, a []float64) float64 { return math.Max(a[0], a[1]) }},
	"min":   {2, func(_ *evaluator, a []float64) float64 { return math.Min(a[0], a[1]) }},
	"mod":   {2, func(_ *evaluator, a []float64) float64 { return math.Mod(a[0], a[1]) }},
	"sign": {1, func(_ *evaluator, a []float64) float64 {
		if a[0] < 0 {
			return -1
		}
		return 1
	}},
	"lerp": {3, func(_ *evaluator, a []float64) float64 { return a[0] + (a[1]-a[0])*a[2] }},
	"inverse_lerp": {3, func(_ *evaluator, a []float64) float64 {
		if a[1] == a[0] {
			return 0
		}
		return (a[2] - a[0]) / (a[1] - a[0])
	}},
	"lerprotate": {3, func(_ *evaluator, a []float64) float64 {
		d := wrapDeg(a[1] - a[0])
		return wrapDeg(a[0] + d*a[2])
	}},
	"min_angle":     {1, func(_ *evaluator, a []float64) float64 { return wrapDeg(a[0]) }},
	"hermite_blend": {1, func(_ *evaluator, a []float64) float64 { t := a[0]; return 3*t*t - 2*t*t*t }},
	"random": {2, func(ev *evaluator, a []float64) float64 {
		return a[0] + ev.float64()*(a[1]-a[0])
	}},
	"random_integer": {2, func(ev *evaluator, a []float64) float64 {
		lo, hi := int(math.Round(a[0])), int(math.Round(a[1]))
		if hi < lo {
			lo, hi = hi, lo
		}
		return float64(lo + ev.intN(hi-lo+1))
	}},
	"die_roll": {3, func(ev *evaluator, a []float64) float64 {
		s := 0.0
		for i := 0; i < int(a[0]); i++ {
			s += a[1] + ev.float64()*(a[2]-a[1])
		}
		return s
	}},
	"die_roll_integer": {3, func(ev *evaluator, a []float64) float64 {
		lo, hi := int(math.Round(a[1])), int(math.Round(a[2]))
		if hi < lo {
			lo, hi = hi, lo
		}
		s := 0
		for i := 0; i < int(a[0]); i++ {
			s += lo + ev.intN(hi-lo+1)
		}
		return float64(s)
	}},
}

func wrapDeg(d float64) float64 {
	d = math.Mod(d+180, 360)
	if d < 0 {
		d += 360
	}
	return d - 180
}
