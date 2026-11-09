//go:build crossfly_full

package molang

import (
	"fmt"
	"strconv"
	"strings"
)

type tokKind uint8

const (
	tokEOF tokKind = iota
	tokNum
	tokStr
	tokIdent // dotted path, lower-cased
	tokOp
)

type token struct {
	kind tokKind
	text string
	num  float64
	pos  int
}

func lex(src string) ([]token, error) {
	var toks []token
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c >= '0' && c <= '9' || c == '.' && i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9':
			j := i
			for j < len(src) && (src[j] >= '0' && src[j] <= '9' || src[j] == '.') {
				j++
			}
			f, err := strconv.ParseFloat(src[i:j], 64)
			if err != nil {
				return nil, fmt.Errorf("molang: bad number %q at %d", src[i:j], i)
			}
			// Molang allows a trailing f (1.0f).
			if j < len(src) && (src[j] == 'f' || src[j] == 'F') {
				j++
			}
			toks = append(toks, token{kind: tokNum, num: f, pos: i})
			i = j
		case c == '\'':
			j := strings.IndexByte(src[i+1:], '\'')
			if j < 0 {
				return nil, fmt.Errorf("molang: unterminated string at %d", i)
			}
			toks = append(toks, token{kind: tokStr, text: src[i+1 : i+1+j], pos: i})
			i += j + 2
		case isIdentStart(c):
			j := i
			for j < len(src) && (isIdentStart(src[j]) || src[j] >= '0' && src[j] <= '9' || src[j] == '.') {
				j++
			}
			toks = append(toks, token{kind: tokIdent, text: strings.ToLower(src[i:j]), pos: i})
			i = j
		default:
			two := ""
			if i+1 < len(src) {
				two = src[i : i+2]
			}
			switch two {
			case "&&", "||", "==", "!=", "<=", ">=", "??", "->":
				toks = append(toks, token{kind: tokOp, text: two, pos: i})
				i += 2
				continue
			}
			if strings.IndexByte("()+-*/<>!?:,;=", c) < 0 {
				return nil, fmt.Errorf("molang: unexpected %q at %d", c, i)
			}
			toks = append(toks, token{kind: tokOp, text: string(c), pos: i})
			i++
		}
	}
	return append(toks, token{kind: tokEOF, pos: len(src)}), nil
}

func isIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// node kinds of the expression tree.
type node interface{}

type (
	numNode   float64
	strNode   string
	thisNode  struct{}
	unaryNode struct {
		op string
		x  node
	}
	binNode struct {
		op   string
		l, r node
	}
	ternNode struct{ cond, a, b node } // b == nil for the binary conditional
	callNode struct {
		ns   string // query, math, variable, temp, context
		name string
		args []node
	}
	assignNode struct {
		ns, name string
		x        node
	}
	seqNode struct {
		stmts []node
		ret   []bool
	}
)

type parser struct {
	toks []token
	i    int
	qs   map[string]struct{}
}

func (p *parser) peek() token { return p.toks[p.i] }
func (p *parser) next() token { t := p.toks[p.i]; p.i++; return t }
func (p *parser) isOp(s string) bool {
	t := p.peek()
	return t.kind == tokOp && t.text == s
}
func (p *parser) expect(s string) error {
	if !p.isOp(s) {
		return fmt.Errorf("molang: expected %q at %d", s, p.peek().pos)
	}
	p.i++
	return nil
}

func (p *parser) program() (node, error) {
	seq := &seqNode{}
	for {
		if p.peek().kind == tokEOF {
			break
		}
		ret := false
		if t := p.peek(); t.kind == tokIdent && t.text == "return" {
			p.i++
			ret = true
		}
		n, err := p.assign()
		if err != nil {
			return nil, err
		}
		seq.stmts = append(seq.stmts, n)
		seq.ret = append(seq.ret, ret)
		if p.isOp(";") {
			p.i++
			continue
		}
		if p.peek().kind != tokEOF {
			return nil, fmt.Errorf("molang: unexpected token at %d", p.peek().pos)
		}
	}
	if len(seq.stmts) == 1 && !seq.ret[0] {
		return seq.stmts[0], nil
	}
	return seq, nil
}

func (p *parser) assign() (node, error) {
	start := p.i
	if t := p.peek(); t.kind == tokIdent {
		ns, name := splitPath(t.text)
		if (ns == "variable" || ns == "temp" || ns == "context") && p.toks[p.i+1].kind == tokOp && p.toks[p.i+1].text == "=" {
			p.i += 2
			x, err := p.assign()
			if err != nil {
				return nil, err
			}
			return assignNode{ns: ns, name: name, x: x}, nil
		}
	}
	p.i = start
	return p.ternary()
}

func (p *parser) ternary() (node, error) {
	c, err := p.coalesce()
	if err != nil {
		return nil, err
	}
	if !p.isOp("?") {
		return c, nil
	}
	p.i++
	a, err := p.ternary()
	if err != nil {
		return nil, err
	}
	if !p.isOp(":") {
		return ternNode{cond: c, a: a}, nil
	}
	p.i++
	b, err := p.ternary()
	if err != nil {
		return nil, err
	}
	return ternNode{cond: c, a: a, b: b}, nil
}

func (p *parser) binary(next func() (node, error), ops ...string) (node, error) {
	l, err := next()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		matched := ""
		if t.kind == tokOp {
			for _, o := range ops {
				if t.text == o {
					matched = o
				}
			}
		}
		if matched == "" {
			return l, nil
		}
		p.i++
		r, err := next()
		if err != nil {
			return nil, err
		}
		l = binNode{op: matched, l: l, r: r}
	}
}

func (p *parser) coalesce() (node, error) { return p.binary(p.or, "??") }
func (p *parser) or() (node, error)       { return p.binary(p.and, "||") }
func (p *parser) and() (node, error)      { return p.binary(p.eq, "&&") }
func (p *parser) eq() (node, error)       { return p.binary(p.cmp, "==", "!=") }
func (p *parser) cmp() (node, error)      { return p.binary(p.add, "<", "<=", ">", ">=") }
func (p *parser) add() (node, error)      { return p.binary(p.mul, "+", "-") }
func (p *parser) mul() (node, error)      { return p.binary(p.unary, "*", "/") }

func (p *parser) unary() (node, error) {
	if p.isOp("!") || p.isOp("-") {
		op := p.next().text
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		return unaryNode{op: op, x: x}, nil
	}
	return p.primary()
}

func (p *parser) primary() (node, error) {
	t := p.next()
	switch t.kind {
	case tokNum:
		return numNode(t.num), nil
	case tokStr:
		return strNode(t.text), nil
	case tokOp:
		if t.text == "(" {
			n, err := p.assign()
			if err != nil {
				return nil, err
			}
			return n, p.expect(")")
		}
	case tokIdent:
		switch t.text {
		case "true":
			return numNode(1), nil
		case "false":
			return numNode(0), nil
		case "this":
			return thisNode{}, nil
		}
		ns, name := splitPath(t.text)
		if ns == "" {
			return nil, fmt.Errorf("molang: unknown identifier %q at %d", t.text, t.pos)
		}
		c := callNode{ns: ns, name: name}
		if p.isOp("(") {
			p.i++
			for !p.isOp(")") {
				a, err := p.assign()
				if err != nil {
					return nil, err
				}
				c.args = append(c.args, a)
				if p.isOp(",") {
					p.i++
				} else if !p.isOp(")") {
					return nil, fmt.Errorf("molang: expected , or ) at %d", p.peek().pos)
				}
			}
			p.i++
		}
		if ns == "query" {
			p.qs[name] = struct{}{}
		} else if ns == "math" {
			if _, ok := mathFuncs[name]; !ok && name != "pi" {
				return nil, fmt.Errorf("molang: unknown function math.%s", name)
			}
		}
		return c, nil
	}
	return nil, fmt.Errorf("molang: unexpected token at %d", t.pos)
}

// splitPath turns "q.is_baby" into ("query", "is_baby").
func splitPath(s string) (ns, name string) {
	dot := strings.IndexByte(s, '.')
	if dot < 0 {
		return "", s
	}
	head, rest := s[:dot], s[dot+1:]
	switch head {
	case "q", "query":
		return "query", rest
	case "math":
		return "math", rest
	case "v", "variable":
		return "variable", rest
	case "t", "temp":
		return "temp", rest
	case "c", "context":
		return "context", rest
	}
	return "", s
}
