//go:build crossfly_full

package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/df-mc/dragonfly/server/mob"
)

// Expr is a compiled Molang expression of the small subset the passive
// components use (experience_reward, eat_block success_chance,
// ambient_sound_interval conditions): numbers, query.*, math.* functions,
// ! && || ?: comparisons and + - * /. Unknown queries are 0, like an unset
// variable in BDS.
type Expr interface {
	Eval(m *mob.Mob) float64
}

// Const is a constant expression.
type Const float64

// Eval ...
func (c Const) Eval(*mob.Mob) float64 { return float64(c) }

// ParseExpr reads a JSON number, bool or Molang string.
func ParseExpr(raw json.RawMessage) (Expr, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return Const(0), nil
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return Const(f), nil
	}
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		if b {
			return Const(1), nil
		}
		return Const(0), nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("molang: %s is not a number or string", raw)
	}
	return CompileMolang(s)
}

// CompileMolang compiles a Molang string.
func CompileMolang(s string) (Expr, error) {
	src := strings.TrimSpace(s)
	if strings.HasPrefix(strings.ToLower(src), "return ") {
		src = src[len("return "):]
	}
	p := &molangParser{src: src}
	p.next()
	e, err := p.ternary()
	if err != nil {
		return nil, fmt.Errorf("molang %q: %w", s, err)
	}
	if p.tok.kind == tokSemicolon {
		p.next()
	}
	if p.tok.kind != tokEOF {
		return nil, fmt.Errorf("molang %q: unexpected %q", s, p.tok.text)
	}
	return e, nil
}

type tokKind int

const (
	tokEOF tokKind = iota
	tokNum
	tokIdent
	tokOp
	tokLParen
	tokRParen
	tokComma
	tokSemicolon
	tokReturn
)

type token struct {
	kind tokKind
	text string
	num  float64
}

type molangParser struct {
	src string
	pos int
	tok token
}

func (p *molangParser) next() {
	for p.pos < len(p.src) && unicode.IsSpace(rune(p.src[p.pos])) {
		p.pos++
	}
	if p.pos >= len(p.src) {
		p.tok = token{kind: tokEOF}
		return
	}
	c := p.src[p.pos]
	switch {
	case c >= '0' && c <= '9' || c == '.' && p.pos+1 < len(p.src) && p.src[p.pos+1] >= '0' && p.src[p.pos+1] <= '9':
		start := p.pos
		for p.pos < len(p.src) && (p.src[p.pos] >= '0' && p.src[p.pos] <= '9' || p.src[p.pos] == '.') {
			p.pos++
		}
		if p.pos < len(p.src) && (p.src[p.pos] == 'f' || p.src[p.pos] == 'F') {
			p.pos++
		}
		f, _ := strconv.ParseFloat(strings.TrimRight(p.src[start:p.pos], "fF"), 64)
		p.tok = token{kind: tokNum, num: f, text: p.src[start:p.pos]}
	case unicode.IsLetter(rune(c)) || c == '_':
		start := p.pos
		for p.pos < len(p.src) && (unicode.IsLetter(rune(p.src[p.pos])) || unicode.IsDigit(rune(p.src[p.pos])) || p.src[p.pos] == '_' || p.src[p.pos] == '.') {
			p.pos++
		}
		text := strings.ToLower(p.src[start:p.pos])
		switch text {
		case "return":
			p.tok = token{kind: tokReturn, text: text}
		case "true":
			p.tok = token{kind: tokNum, num: 1, text: text}
		case "false":
			p.tok = token{kind: tokNum, num: 0, text: text}
		default:
			p.tok = token{kind: tokIdent, text: text}
		}
	case c == '(':
		p.pos++
		p.tok = token{kind: tokLParen, text: "("}
	case c == ')':
		p.pos++
		p.tok = token{kind: tokRParen, text: ")"}
	case c == ',':
		p.pos++
		p.tok = token{kind: tokComma, text: ","}
	case c == ';':
		p.pos++
		p.tok = token{kind: tokSemicolon, text: ";"}
	default:
		for _, op := range []string{"&&", "||", "==", "!=", "<=", ">=", "??", "!", "<", ">", "+", "-", "*", "/", "?", ":"} {
			if strings.HasPrefix(p.src[p.pos:], op) {
				p.pos += len(op)
				p.tok = token{kind: tokOp, text: op}
				return
			}
		}
		p.tok = token{kind: tokOp, text: string(c)}
		p.pos++
	}
}

func (p *molangParser) isOp(s string) bool { return p.tok.kind == tokOp && p.tok.text == s }

func (p *molangParser) ternary() (Expr, error) {
	cond, err := p.binary(0)
	if err != nil {
		return nil, err
	}
	if !p.isOp("?") {
		return cond, nil
	}
	p.next()
	a, err := p.ternary()
	if err != nil {
		return nil, err
	}
	if !p.isOp(":") {
		// Binary conditional "a ? b": b when a, else 0.
		return ternaryExpr{cond, a, Const(0)}, nil
	}
	p.next()
	b, err := p.ternary()
	if err != nil {
		return nil, err
	}
	return ternaryExpr{cond, a, b}, nil
}

var precedence = map[string]int{"??": 1, "||": 2, "&&": 3, "==": 4, "!=": 4, "<": 5, ">": 5, "<=": 5, ">=": 5, "+": 6, "-": 6, "*": 7, "/": 7}

func (p *molangParser) binary(min int) (Expr, error) {
	left, err := p.unary()
	if err != nil {
		return nil, err
	}
	for p.tok.kind == tokOp {
		prec, ok := precedence[p.tok.text]
		if !ok || prec <= min {
			break
		}
		op := p.tok.text
		p.next()
		right, err := p.binary(prec)
		if err != nil {
			return nil, err
		}
		left = binaryExpr{op, left, right}
	}
	return left, nil
}

func (p *molangParser) unary() (Expr, error) {
	if p.isOp("!") || p.isOp("-") {
		op := p.tok.text
		p.next()
		e, err := p.unary()
		if err != nil {
			return nil, err
		}
		return unaryExpr{op, e}, nil
	}
	return p.primary()
}

func (p *molangParser) primary() (Expr, error) {
	switch p.tok.kind {
	case tokNum:
		v := p.tok.num
		p.next()
		return Const(v), nil
	case tokLParen:
		p.next()
		e, err := p.ternary()
		if err != nil {
			return nil, err
		}
		if p.tok.kind != tokRParen {
			return nil, fmt.Errorf("missing )")
		}
		p.next()
		return e, nil
	case tokIdent:
		name := p.tok.text
		p.next()
		var args []Expr
		if p.tok.kind == tokLParen {
			p.next()
			for p.tok.kind != tokRParen {
				if p.tok.kind == tokIdent && (p.tok.text == "this" || p.tok.text == "self") {
					p.next()
				} else {
					a, err := p.ternary()
					if err != nil {
						return nil, err
					}
					args = append(args, a)
				}
				if p.tok.kind == tokComma {
					p.next()
				} else if p.tok.kind != tokRParen {
					return nil, fmt.Errorf("bad arguments to %v", name)
				}
			}
			p.next()
		}
		return compileName(name, args)
	}
	return nil, fmt.Errorf("unexpected %q", p.tok.text)
}

func compileName(name string, args []Expr) (Expr, error) {
	if strings.HasPrefix(name, "q.") {
		name = "query." + name[2:]
	}
	if strings.HasPrefix(name, "math.") {
		return mathExpr{fn: name[5:], args: args}, nil
	}
	if strings.HasPrefix(name, "query.") {
		return queryExpr(name[6:]), nil
	}
	// variable.*, temp.*, context.*: not modelled.
	return Const(0), nil
}

type ternaryExpr struct{ cond, a, b Expr }

func (t ternaryExpr) Eval(m *mob.Mob) float64 {
	if t.cond.Eval(m) != 0 {
		return t.a.Eval(m)
	}
	return t.b.Eval(m)
}

type unaryExpr struct {
	op string
	e  Expr
}

func (u unaryExpr) Eval(m *mob.Mob) float64 {
	v := u.e.Eval(m)
	if u.op == "-" {
		return -v
	}
	return b2f(v == 0)
}

type binaryExpr struct {
	op   string
	a, b Expr
}

func (e binaryExpr) Eval(m *mob.Mob) float64 {
	a := e.a.Eval(m)
	switch e.op {
	case "&&":
		return b2f(a != 0 && e.b.Eval(m) != 0)
	case "||":
		return b2f(a != 0 || e.b.Eval(m) != 0)
	case "??":
		return a
	}
	b := e.b.Eval(m)
	switch e.op {
	case "==":
		return b2f(a == b)
	case "!=":
		return b2f(a != b)
	case "<":
		return b2f(a < b)
	case ">":
		return b2f(a > b)
	case "<=":
		return b2f(a <= b)
	case ">=":
		return b2f(a >= b)
	case "+":
		return a + b
	case "-":
		return a - b
	case "*":
		return a * b
	case "/":
		if b == 0 {
			return 0
		}
		return a / b
	}
	return 0
}

type mathExpr struct {
	fn   string
	args []Expr
}

func (e mathExpr) arg(m *mob.Mob, i int) float64 {
	if i < len(e.args) {
		return e.args[i].Eval(m)
	}
	return 0
}

func (e mathExpr) Eval(m *mob.Mob) float64 {
	r := m.Rand()
	switch e.fn {
	case "random":
		lo, hi := e.arg(m, 0), e.arg(m, 1)
		return lo + r.Float64()*(hi-lo)
	case "random_integer":
		lo, hi := int(e.arg(m, 0)), int(e.arg(m, 1))
		if hi < lo {
			lo, hi = hi, lo
		}
		return float64(lo + r.IntN(hi-lo+1))
	case "die_roll":
		n, lo, hi := int(e.arg(m, 0)), e.arg(m, 1), e.arg(m, 2)
		sum := 0.0
		for i := 0; i < n; i++ {
			sum += lo + r.Float64()*(hi-lo)
		}
		return sum
	case "die_roll_integer":
		n, lo, hi := int(e.arg(m, 0)), int(e.arg(m, 1)), int(e.arg(m, 2))
		if hi < lo {
			lo, hi = hi, lo
		}
		sum := 0
		for i := 0; i < n; i++ {
			sum += lo + r.IntN(hi-lo+1)
		}
		return float64(sum)
	case "min":
		return math.Min(e.arg(m, 0), e.arg(m, 1))
	case "max":
		return math.Max(e.arg(m, 0), e.arg(m, 1))
	case "abs":
		return math.Abs(e.arg(m, 0))
	case "floor":
		return math.Floor(e.arg(m, 0))
	case "ceil":
		return math.Ceil(e.arg(m, 0))
	case "round":
		return math.Round(e.arg(m, 0))
	case "trunc":
		return math.Trunc(e.arg(m, 0))
	case "clamp":
		return math.Max(e.arg(m, 1), math.Min(e.arg(m, 2), e.arg(m, 0)))
	case "sqrt":
		return math.Sqrt(e.arg(m, 0))
	case "mod":
		if b := e.arg(m, 1); b != 0 {
			return math.Mod(e.arg(m, 0), b)
		}
	case "pi":
		return math.Pi
	}
	return 0
}

// queryExpr is a no-argument query on the mob.
type queryExpr string

func (q queryExpr) Eval(m *mob.Mob) float64 {
	if m == nil {
		return 0
	}
	switch string(q) {
	case "is_baby":
		return b2f(IsBaby(m))
	case "last_hit_by_player":
		return b2f(LastHitByPlayer(m))
	case "equipment_count":
		// BDS Actor::getEquipmentCount: the worn armour pieces (getAllArmor,
		// body armour included), not the hands.
		n := 0
		for _, s := range m.Armour().Slots() {
			if !s.Empty() {
				n++
			}
		}
		if !m.BodyArmour().Empty() {
			n++
		}
		return float64(n)
	case "variant":
		return float64(m.Meta().Variant)
	case "mark_variant":
		return float64(m.Meta().MarkVariant)
	case "skin_id":
		return float64(m.Meta().SkinID)
	case "color":
		return float64(m.Meta().Color)
	case "is_sheared":
		return b2f(m.Meta().Sheared)
	case "is_saddled":
		return b2f(m.Meta().Saddled)
	case "is_tamed":
		return b2f(m.Meta().Tamed)
	case "is_on_ground":
		return b2f(m.OnGround())
	case "is_in_water":
		return b2f(m.Physics().InWater)
	case "head_is_in_water", "is_underwater":
		return b2f(m.Physics().Underwater)
	case "is_in_lava":
		return b2f(m.Physics().InLava)
	case "is_on_fire":
		return b2f(m.OnFireDuration() > 0)
	case "is_using_item":
		return b2f(m.Meta().UsingItem)
	case "health":
		return m.Health()
	case "max_health":
		return m.MaxHealth()
	case "is_angry":
		return b2f(m.Meta().Angry)
	case "has_target":
		_, ok := m.Target()
		return b2f(ok)
	case "anger_level":
		// minecraft:anger_level (package mob/component/boss).
		if a, ok := mob.ComponentOf[interface{ AngerLevel() int }](m); ok {
			return float64(a.AngerLevel())
		}
		return 0
	}
	// player_level, anger_level, ...: not known for mobs.
	return 0
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// IsBaby reports whether m is a baby (minecraft:is_baby active).
func IsBaby(m *mob.Mob) bool { return m.Meta().Baby || m.HasComponent("minecraft:is_baby") }
