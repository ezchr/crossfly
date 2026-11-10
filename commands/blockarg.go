//go:build crossfly_full

package commands

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/java/javamap"
	"github.com/df-mc/dragonfly/server/world"
)

// Blocks with states in commands, as /setblock and /fill take them on both editions:
//
//	stone                                          a plain name: the block's default state
//	minecraft:repeater[facing=north,delay=3]       Java Edition: Java names, mapped to the Bedrock state
//	unpowered_repeater ["repeater_delay"=2]        Bedrock Edition: Bedrock names, the states may also be
//	unpowered_repeater [repeater_delay=2]          attached to the name and unquoted
//
// States left out take the block's default. As a /fill replace filter, a block without states matches every
// state of the block, one with states only the states named.

// BlockArg is a command parameter that takes a block with or without states (see ParseBlock). Bedrock clients get
// the block names to choose from and an optional block states argument after it; Java clients complete it with
// their own minecraft:block_state argument.
type BlockArg string

// Type ...
func (BlockArg) Type() string { return "Block" }

// Options ...
func (BlockArg) Options(cmd.Source) []string { return BlockNames() }

// Parse ...
func (BlockArg) Parse(line *cmd.Line, v reflect.Value) error {
	s, err := ScanBlock(line)
	if err == nil {
		v.SetString(s)
	}
	return err
}

// BlockPredicate ...
func (BlockArg) BlockPredicate() bool { return false }

// BlockFilterArg is BlockArg as a filter, such as /fill's replace filter: Java clients complete it as a
// minecraft:block_predicate.
type BlockFilterArg string

// Type ...
func (BlockFilterArg) Type() string { return "Block" }

// Options ...
func (BlockFilterArg) Options(cmd.Source) []string { return BlockNames() }

// Parse ...
func (BlockFilterArg) Parse(line *cmd.Line, v reflect.Value) error {
	s, err := ScanBlock(line)
	if err == nil {
		v.SetString(s)
	}
	return err
}

// BlockPredicate ...
func (BlockFilterArg) BlockPredicate() bool { return true }

// ScanBlock reads a block argument from the command line: the block name and the states attached to it or in
// the words that follow ("unpowered_repeater", `["repeater_delay"=2]`). It returns them as one string for
// ParseBlock. It consumes all words but the last, which a cmd.Parameter's Parse leaves to the command parser.
func ScanBlock(line *cmd.Line) (string, error) {
	rest, _ := line.NextN(line.Len())
	if len(rest) == 0 {
		return "", line.UsageError()
	}
	text, n := rest[0], 1
	if !strings.Contains(text, "[") && len(rest) > 1 && strings.HasPrefix(rest[1], "[") {
		text, n = text+" "+rest[1], 2
	}
	for bracketDepth(text) > 0 && n < len(rest) {
		text, n = text+" "+rest[n], n+1
	}
	if bracketDepth(text) > 0 {
		return "", errors.New("Expected closing ] for block state properties")
	}
	line.RemoveN(n - 1)
	return text, nil
}

// bracketDepth returns how many [ are still open at the end of s, outside double quotes.
func bracketDepth(s string) int {
	depth, quoted := 0, false
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
		case quoted:
		case r == '[':
			depth++
		case r == ']':
			depth--
		}
	}
	return depth
}

// BlockNames returns the block names Bedrock clients may choose from, bare and namespaced: those of the block
// items and of every registered block state.
func BlockNames() []string { return blockNames() }

var blockNames = sync.OnceValue(func() []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		short := strings.TrimPrefix(name, "minecraft:")
		if !seen[short] {
			seen[short] = true
			out = append(out, short, name)
		}
	}
	add("minecraft:air")
	for _, it := range world.Items() {
		if _, ok := it.(world.Block); ok {
			name, _ := it.EncodeItem()
			add(name)
		}
	}
	for name := range bedrockBlocks() {
		add(name)
	}
	slices.Sort(out)
	return out
})

// BlockState is a block typed in a command: the block to place and what it matches as a filter.
type BlockState struct {
	block       world.Block
	waterlogged bool

	// name is the Bedrock name. With Bedrock states typed, states holds them and a block matches only these.
	name   string
	states map[string]any
	// javaName is the Java name. With Java properties typed, javaProps holds them and a block matches by its
	// Java state. Without either, a block matches by its Bedrock or its Java name.
	javaName  string
	javaProps map[string]string
}

// Block returns the block to place, never nil (air is block.Air).
func (s BlockState) Block() world.Block { return s.block }

// Liquid returns the liquid to put with the block: water for a Java state with waterlogged=true (Bedrock keeps
// the water in a second layer), else nil.
func (s BlockState) Liquid() world.Liquid {
	if s.waterlogged {
		return block.Water{Still: true, Depth: 8}
	}
	return nil
}

// Matches reports whether b, a block in the world, is the block typed, as a filter: any state of it when no
// states were typed, else the states typed.
func (s BlockState) Matches(b world.Block) bool {
	name, props := "minecraft:air", map[string]any(nil)
	if b != nil {
		name, props = b.EncodeBlock()
	}
	switch {
	case s.states != nil:
		if name != s.name {
			return false
		}
		for k, v := range s.states {
			if props[k] != v {
				return false
			}
		}
		return true
	case s.javaProps != nil:
		jname, jprops, ok := javaState(b)
		if !ok || jname != s.javaName {
			return false
		}
		for k, v := range s.javaProps {
			if jprops[k] != v {
				return false
			}
		}
		return true
	}
	if name == s.name {
		return true
	}
	jname, _, ok := javaState(b)
	return ok && s.javaName != "" && jname == s.javaName
}

// javaState returns the Java block name and properties b shows as.
func javaState(b world.Block) (string, map[string]string, bool) {
	table := javamap.DefaultBlockStates()
	rid := world.BlockRuntimeID(b)
	if int(rid) >= len(table) {
		return "", nil, false
	}
	return javamap.JavaStateProperties(table[rid])
}

// ParseBlock parses a block as ScanBlock returns it: a name, with or without states in Java's or Bedrock's
// syntax. The errors name the unknown block, state or value.
func ParseBlock(text string) (BlockState, error) {
	name, props, bracketed, err := splitBlock(text)
	if err != nil {
		return BlockState{}, err
	}
	if !bracketed {
		return plainBlock(name)
	}
	bedrockSyntax := false
	for _, p := range props {
		bedrockSyntax = bedrockSyntax || p.quoted || p.colon
	}
	if jb, ok := javamap.JavaBlockByName(name); ok && !bedrockSyntax {
		s, jerr := javaBlock(jb, props)
		if jerr == nil {
			return s, nil
		}
		if s, err := bedrockBlock(name, props); err == nil {
			return s, nil
		}
		return BlockState{}, jerr
	}
	return bedrockBlock(name, props)
}

// blockProp is one state typed in brackets.
type blockProp struct {
	key, value string
	quoted     bool // key or value in double quotes: Bedrock's syntax
	colon      bool // "key":value, Bedrock's older syntax
}

// splitBlock splits a block argument into its name and the states in brackets, if any.
func splitBlock(text string) (name string, props []blockProp, bracketed bool, err error) {
	text = strings.TrimSpace(text)
	i := strings.IndexByte(text, '[')
	if i < 0 {
		if strings.ContainsAny(text, "]{ ") {
			return "", nil, false, fmt.Errorf("Unknown block: %s", text)
		}
		return strings.ToLower(text), nil, false, nil
	}
	name = strings.ToLower(strings.TrimSpace(text[:i]))
	if name == "" {
		return "", nil, false, errors.New("Expected a block name before [")
	}
	r := []rune(text[i+1:])
	pos := 0
	skip := func() {
		for pos < len(r) && (r[pos] == ' ' || r[pos] == '\t') {
			pos++
		}
	}
	// word reads a quoted string or a bare word ending at one of stop or a space.
	word := func(stop string) (string, bool, error) {
		if pos < len(r) && r[pos] == '"' {
			end := pos + 1
			for end < len(r) && r[end] != '"' {
				end++
			}
			if end == len(r) {
				return "", true, errors.New("Unclosed quote in block states")
			}
			w := string(r[pos+1 : end])
			pos = end + 1
			return w, true, nil
		}
		start := pos
		for pos < len(r) && r[pos] != ' ' && !strings.ContainsRune(stop, r[pos]) {
			pos++
		}
		return string(r[start:pos]), false, nil
	}
	seen := map[string]bool{}
	skip()
	if pos < len(r) && r[pos] == ']' {
		pos++
	} else {
		for {
			skip()
			var p blockProp
			key, kq, err := word("=,]")
			if err != nil {
				return "", nil, false, err
			}
			if key == "" {
				return "", nil, false, errors.New("Expected a block state name in [...]")
			}
			skip()
			if pos == len(r) || (r[pos] != '=' && !(kq && r[pos] == ':')) {
				return "", nil, false, fmt.Errorf("Expected = after block state %s", key)
			}
			p.colon = r[pos] == ':'
			pos++
			skip()
			value, vq, err := word(",]")
			if err != nil {
				return "", nil, false, err
			}
			if value == "" {
				return "", nil, false, fmt.Errorf("Expected a value for block state %s", key)
			}
			p.key, p.value, p.quoted = key, value, kq || vq
			if seen[key] {
				return "", nil, false, fmt.Errorf("Property '%s' can only be set once for block %s", key, name)
			}
			seen[key] = true
			props = append(props, p)
			skip()
			if pos == len(r) {
				return "", nil, false, errors.New("Expected closing ] for block state properties")
			}
			if r[pos] == ']' {
				pos++
				break
			}
			if r[pos] != ',' {
				return "", nil, false, fmt.Errorf("Expected , or ] after block state %s", key)
			}
			pos++
		}
	}
	if rest := strings.TrimSpace(string(r[pos:])); rest != "" {
		if strings.HasPrefix(rest, "{") {
			return "", nil, false, errors.New("Block entity data ({...}) is not supported")
		}
		return "", nil, false, fmt.Errorf("Unexpected text after the block states: %s", rest)
	}
	return name, props, true, nil
}

// plainBlock resolves a block name without states: the block of the item of that name (as before states were
// supported), else the Bedrock block, else the Java block, in its default state.
func plainBlock(name string) (BlockState, error) {
	full := name
	if !strings.Contains(full, ":") {
		full = "minecraft:" + full
	}
	var b world.Block
	if full == "minecraft:air" {
		b = air()
	} else if it, ok := world.ItemByName(full, 0); ok {
		b, _ = it.(world.Block)
	}
	if b == nil {
		if bb, ok := bedrockBlocks()[full]; ok {
			b, _ = world.BlockByName(full, bb.def)
		}
	}
	if b == nil {
		if jb, ok := javamap.JavaBlockByName(full); ok {
			if rid, ok := javamap.BedrockRuntimeID(jb.Default, nil); ok {
				b, _ = world.BlockByRuntimeID(rid)
			}
		}
	}
	if b == nil {
		return BlockState{}, fmt.Errorf("Unknown block: %s", name)
	}
	s := BlockState{block: b}
	s.name, _ = b.EncodeBlock()
	s.javaName, _, _ = javaState(b)
	return s, nil
}

func air() world.Block {
	b, _ := world.BlockByName("minecraft:air", map[string]any{})
	return b
}

// javaBlock resolves Java properties typed for the Java block jb.
func javaBlock(jb *javamap.JavaBlock, props []blockProp) (BlockState, error) {
	m := make(map[string]string, len(props))
	named := make([]string, 0, len(props))
	for _, p := range props {
		m[p.key] = p.value
		named = append(named, p.key)
	}
	id, err := jb.State(m)
	if err != nil {
		return BlockState{}, err
	}
	if jb.Name == "minecraft:air" {
		return BlockState{block: air(), name: "minecraft:air", javaName: jb.Name, javaProps: m}, nil
	}
	rid, ok := javamap.BedrockRuntimeID(id, named)
	if !ok {
		return BlockState{}, fmt.Errorf("Block %s has no Bedrock Edition counterpart here", jb.Name)
	}
	b, ok := world.BlockByRuntimeID(rid)
	if !ok {
		return BlockState{}, fmt.Errorf("Block %s has no Bedrock Edition counterpart here", jb.Name)
	}
	s := BlockState{block: b, waterlogged: m["waterlogged"] == "true", javaName: jb.Name, javaProps: m}
	s.name, _ = b.EncodeBlock()
	return s, nil
}

// bedrockStates holds what a Bedrock block's states can be.
type bedrockStates struct {
	def    map[string]any   // the default state: the first in the palette
	values map[string][]any // every value of each state, in palette order
}

// bedrockBlocks maps every Bedrock block name in world.DefaultBlockRegistry to its states.
var bedrockBlocks = sync.OnceValue(func() map[string]*bedrockStates {
	reg := world.DefaultBlockRegistry
	reg.Finalize()
	m := map[string]*bedrockStates{}
	for rid := range uint32(reg.BlockCount()) {
		name, props, ok := reg.RuntimeIDToState(rid)
		if !ok {
			continue
		}
		bs := m[name]
		if bs == nil {
			def, _ := reg.DefaultProperties(name)
			bs = &bedrockStates{def: def, values: map[string][]any{}}
			m[name] = bs
		}
		for k, v := range props {
			if !slices.Contains(bs.values[k], v) {
				bs.values[k] = append(bs.values[k], v)
			}
		}
	}
	return m
})

// bedrockBlock resolves Bedrock states typed for the Bedrock block name, or for the block of the item or Java block
// of that name.
func bedrockBlock(name string, props []blockProp) (BlockState, error) {
	full := name
	if !strings.Contains(full, ":") {
		full = "minecraft:" + full
	}
	bs := bedrockBlocks()[full]
	if bs == nil {
		// The block of the item of that name, else of the Java block of that name (repeater: unpowered_repeater).
		var b world.Block
		if it, ok := world.ItemByName(full, 0); ok {
			b, _ = it.(world.Block)
		}
		if jb, ok := javamap.JavaBlockByName(full); ok && b == nil {
			if rid, ok := javamap.BedrockRuntimeID(jb.Default, nil); ok {
				b, _ = world.BlockByRuntimeID(rid)
			}
		}
		if b != nil {
			full, _ = b.EncodeBlock()
			bs = bedrockBlocks()[full]
		}
	}
	if bs == nil {
		return BlockState{}, fmt.Errorf("Unknown block: %s", name)
	}
	want := make(map[string]any, len(bs.def))
	for k, v := range bs.def {
		want[k] = v
	}
	typed := make(map[string]any, len(props))
	for _, p := range props {
		key := p.key
		values, ok := bs.values[key]
		if !ok {
			if v, ok2 := bs.values["minecraft:"+key]; ok2 {
				key, values, ok = "minecraft:"+key, v, true
			}
		}
		if !ok {
			return BlockState{}, fmt.Errorf("Block %s has no block state %q (states: %s)", full, p.key, stateNames(bs))
		}
		v, ok := bedrockValue(p.value, values)
		if !ok {
			return BlockState{}, fmt.Errorf("Block state %q of %s does not accept %s (values: %s)", key, full, p.value, valueList(values))
		}
		want[key], typed[key] = v, v
	}
	b, ok := world.BlockByName(full, want)
	if !ok {
		return BlockState{}, fmt.Errorf("Block %s has no state with %v", full, typed)
	}
	s := BlockState{block: b, name: full, states: typed}
	s.javaName, _, _ = javaState(b)
	return s, nil
}

// bedrockValue converts a typed value to the type of a Bedrock state's values (string, int32, or uint8 or bool
// for a boolean) and reports whether it is one of them.
func bedrockValue(raw string, values []any) (any, bool) {
	if len(values) == 0 {
		return nil, false
	}
	var v any
	switch values[0].(type) {
	case string:
		v = raw
	case int32:
		n, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			return nil, false
		}
		v = int32(n)
	case uint8, bool:
		var on bool
		switch strings.ToLower(raw) {
		case "true", "1":
			on = true
		case "false", "0":
		default:
			return nil, false
		}
		// The palette keeps booleans as bytes, Dragonfly's blocks encode them as bools.
		if _, ok := values[0].(bool); ok {
			v = on
		} else if on {
			v = uint8(1)
		} else {
			v = uint8(0)
		}
	default:
		return nil, false
	}
	return v, slices.Contains(values, v)
}

func stateNames(bs *bedrockStates) string {
	names := make([]string, 0, len(bs.values))
	for k := range bs.values {
		names = append(names, strconv.Quote(k))
	}
	slices.Sort(names)
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

func valueList(values []any) string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		switch v := v.(type) {
		case string:
			out = append(out, strconv.Quote(v))
		case uint8:
			out = append(out, strconv.FormatBool(v != 0))
		case bool:
			out = append(out, strconv.FormatBool(v))
		default:
			out = append(out, fmt.Sprint(v))
		}
	}
	return strings.Join(out, ", ")
}
