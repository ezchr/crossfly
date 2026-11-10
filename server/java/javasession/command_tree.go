package javasession

import (
	"hash/maphash"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/cmd"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/java/protocol/version"
	"github.com/df-mc/dragonfly/server/java/protocol/wire"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The commands tree (ClientboundPlayCommands): Dragonfly's commands as a Brigadier graph, so the
// Java client suggests and highlights them and sends them as chat_command. Each command is a
// literal under the root; each overload is a path of parameter nodes from it. Aliases are
// literals that redirect to the command. Parameter types map to Brigadier parsers:
//
//	int types -> brigadier:integer      float types -> brigadier:double
//	string    -> brigadier:string word  cmd.Varargs -> brigadier:string greedy
//	bool      -> brigadier:bool         mgl64.Vec3  -> minecraft:vec3
//	cmd.Target, []cmd.Target -> minecraft:entity (single / multiple; players only when the
//	                            parameter is named for players, so the client suggests the
//	                            online players' names and not the UUID of what it looks at)
//	cmd.SubCommand -> literal           cmd.Enum    -> one literal per option; with more than
//	                                                   maxEnumLiterals (items, blocks) an argument
//	                                                   the server suggests for (ask_server):
//	                                                   minecraft:resource_location, or string word
//	cmd.BlockParameter -> minecraft:block_state, or minecraft:block_predicate for a filter: the
//	                      client completes the block and its states itself
//	other cmd.Parameter -> string word (greedy when last)
//
// The tree is rebuilt every commandRefresh and resent only when it changed (permissions and enum
// options can change, as the Bedrock session does every 5 seconds).

const (
	maxEnumLiterals = 128
	commandRefresh  = 5 * time.Second
)

// Brigadier argument parser ids (minecraft:command_argument_type, 26.3).
const (
	parserBool    = 0
	parserDouble  = 2
	parserInteger = 3
	parserString  = 5
	parserEntity  = 6
	parserVec3    = 10
	parserBlock   = 12 // minecraft:block_state
	parserBlockP  = 13 // minecraft:block_predicate
	parserResLoc  = 36 // minecraft:resource_location

	stringWord   = 0
	stringGreedy = 2
)

const (
	nodeRoot     = 0
	nodeLiteral  = 1
	nodeArgument = 2

	flagExecutable  = 0x04
	flagRedirect    = 0x08
	flagSuggestions = 0x10

	entitySingle      = 0x01
	entityPlayersOnly = 0x02

	// askServer is the suggestions type of arguments the server suggests values for: the client
	// sends command_suggestion as they are typed.
	askServer = "minecraft:ask_server"
)

type cmdNode struct {
	kind     byte
	exec     bool
	name     string
	parser   int32
	prop     int32 // string mode / entity flags; -1 for none
	children []int32
	redirect int32 // -1 for none
	// suggest is the enum whose options the server suggests for this argument (ask_server).
	suggest cmd.Enum
}

type cmdTree struct {
	nodes []cmdNode
}

func (t *cmdTree) add(n cmdNode) int32 {
	t.nodes = append(t.nodes, n)
	return int32(len(t.nodes) - 1)
}

// child returns parent's child named name, creating it from n if there is none. Brigadier keys
// children by name, so a second node with the same name would replace the first on the client.
func (t *cmdTree) child(parent int32, n cmdNode) int32 {
	for _, c := range t.nodes[parent].children {
		if t.nodes[c].name == n.name {
			return c
		}
	}
	i := t.add(n)
	t.nodes[parent].children = append(t.nodes[parent].children, i)
	return i
}

// link makes the existing node i a child of parent unless parent has a child of that name.
func (t *cmdTree) link(parent, i int32) int32 {
	for _, c := range t.nodes[parent].children {
		if t.nodes[c].name == t.nodes[i].name {
			return c
		}
	}
	t.nodes[parent].children = append(t.nodes[parent].children, i)
	return i
}

// buildCommandTree builds the tree of the commands src may run. Call it in src's transaction.
func buildCommandTree(src cmd.Source) *cmdTree {
	t := &cmdTree{}
	t.add(cmdNode{kind: nodeRoot, redirect: -1, prop: -1})
	all := cmd.Commands()
	names := make([]string, 0, len(all))
	for alias, c := range all {
		if c.Name() == alias {
			names = append(names, alias)
		}
	}
	slices.Sort(names)
	for _, name := range names {
		c := all[name]
		if !validLiteral(name) || len(c.Runnables(src)) == 0 {
			continue
		}
		root := t.child(0, cmdNode{kind: nodeLiteral, name: name, redirect: -1, prop: -1})
		for _, params := range c.Params(src) {
			t.addOverload(root, params, src)
		}
		for _, alias := range c.Aliases() {
			if alias == name || !validLiteral(alias) {
				continue
			}
			if _, taken := all[alias]; taken && all[alias].Name() == alias {
				continue // a command of its own
			}
			t.child(0, cmdNode{kind: nodeLiteral, name: alias, redirect: root, prop: -1, exec: t.nodes[root].exec})
		}
	}
	return t
}

func validLiteral(s string) bool {
	return s != "" && !strings.ContainsAny(s, " \t\n")
}

// addOverload adds the path of one runnable's parameters under the command node.
func (t *cmdTree) addOverload(root int32, params []cmd.ParamInfo, src cmd.Source) {
	frontier := []int32{root}
	if allOptional(params) {
		t.nodes[root].exec = true
	}
	for i, p := range params {
		last := i == len(params)-1
		var next []int32
		if lits := literalsOf(p, src); lits != nil {
			for _, f := range frontier {
				for _, l := range lits {
					next = append(next, t.child(f, cmdNode{kind: nodeLiteral, name: l, redirect: -1, prop: -1}))
				}
			}
		} else {
			n := argumentOf(p, last, src)
			shared := int32(-1)
			for _, f := range frontier {
				if shared < 0 {
					shared = t.child(f, n)
					next = append(next, shared)
				} else {
					next = append(next, t.link(f, shared))
				}
			}
		}
		slices.Sort(next)
		frontier = slices.Compact(next)
		if allOptional(params[i+1:]) {
			for _, f := range frontier {
				t.nodes[f].exec = true
			}
		}
	}
}

func allOptional(params []cmd.ParamInfo) bool {
	for _, p := range params {
		if !p.Optional {
			return false
		}
	}
	return true
}

// literalsOf returns the literal words a parameter takes, or nil if it is an argument.
func literalsOf(p cmd.ParamInfo, src cmd.Source) []string {
	if _, ok := p.Value.(cmd.BlockParameter); ok {
		return nil
	}
	switch v := p.Value.(type) {
	case cmd.SubCommand:
		if validLiteral(p.Name) {
			return []string{p.Name}
		}
		return nil
	case bool:
		return nil
	case cmd.Enum:
		opts := v.Options(src)
		if len(opts) == 0 || len(opts) > maxEnumLiterals {
			return nil
		}
		lits := make([]string, 0, len(opts))
		for _, o := range opts {
			if validLiteral(o) {
				lits = append(lits, o)
			}
		}
		if len(lits) == 0 {
			return nil
		}
		return lits
	}
	return nil
}

var (
	targetType  = reflect.TypeFor[cmd.Target]()
	targetsType = reflect.TypeFor[[]cmd.Target]()
)

// playersOnly reports whether a target parameter only takes players, judged by its name as
// Dragonfly's commands name them ("player", /kick's "name"); such as /give, /gamemode and /clear.
func playersOnly(p cmd.ParamInfo) bool {
	name := strings.ToLower(p.Name)
	return strings.Contains(name, "player") || name == "name"
}

// resourceLocation reports whether s is a valid minecraft:resource_location, which the client
// checks as it is typed.
func resourceLocation(s string) bool {
	ns, path, found := strings.Cut(s, ":")
	if !found {
		ns, path = "minecraft", s
	}
	if ns == "" || path == "" {
		return false
	}
	for _, r := range ns {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	for _, r := range path {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' || r == '/') {
			return false
		}
	}
	return true
}

// argumentOf returns the argument node for a parameter.
func argumentOf(p cmd.ParamInfo, last bool, src cmd.Source) cmdNode {
	n := cmdNode{kind: nodeArgument, name: p.Name, redirect: -1, prop: -1}
	if n.name == "" {
		n.name = "value"
	}
	if b, ok := p.Value.(cmd.BlockParameter); ok {
		n.parser = parserBlock
		if b.BlockPredicate() {
			n.parser = parserBlockP
		}
		return n
	}
	switch v := p.Value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		n.parser = parserInteger
		n.prop = 0 // no min/max
		return n
	case float32, float64:
		n.parser = parserDouble
		n.prop = 0
		return n
	case bool:
		n.parser = parserBool
		return n
	case cmd.Varargs, cmd.JSON:
		n.parser, n.prop = parserString, stringGreedy
		return n
	case mgl64.Vec3:
		n.parser = parserVec3
		return n
	case []cmd.Target:
		n.parser, n.prop = parserEntity, 0
		if playersOnly(p) {
			n.prop |= entityPlayersOnly
		}
		return n
	case cmd.Enum:
		// Too many options for literals (literalsOf): the client asks the server as it is typed.
		n.parser, n.prop, n.suggest = parserResLoc, -1, v
		for _, o := range v.Options(src) {
			if !resourceLocation(o) {
				n.parser, n.prop = parserString, stringWord
				break
			}
		}
		return n
	}
	if t := reflect.TypeOf(p.Value); t != nil {
		if t == targetsType {
			n.parser, n.prop = parserEntity, 0
			if playersOnly(p) {
				n.prop |= entityPlayersOnly
			}
			return n
		}
		if t == targetType || t.Implements(targetType) {
			n.parser, n.prop = parserEntity, entitySingle
			if m, ok := p.Value.(interface{ MultipleTargets() bool }); ok && m.MultipleTargets() {
				n.prop = 0 // a cmd.Parameter that may select several players, such as /tp's target
			}
			if playersOnly(p) {
				n.prop |= entityPlayersOnly
			}
			return n
		}
	}
	n.parser, n.prop = parserString, stringWord
	if _, ok := p.Value.(cmd.Parameter); ok && last {
		n.prop = stringGreedy
	}
	return n
}

// encode writes the commands packet body. args maps the parser ids (minecraft:command_argument_type,
// 26.3) to the client version's (nil: unchanged).
func (t *cmdTree) encode(w *wire.Writer, args []int32) {
	w.VarInt(int32(len(t.nodes)))
	for _, n := range t.nodes {
		flags := n.kind
		if n.exec {
			flags |= flagExecutable
		}
		if n.redirect >= 0 {
			flags |= flagRedirect
		}
		if n.suggest != nil {
			flags |= flagSuggestions
		}
		w.Byte(flags)
		w.VarInt(int32(len(n.children)))
		for _, c := range n.children {
			w.VarInt(c)
		}
		if n.redirect >= 0 {
			w.VarInt(n.redirect)
		}
		if n.kind == nodeRoot {
			continue
		}
		w.String(n.name)
		if n.kind != nodeArgument {
			continue
		}
		w.VarInt(version.Map(args, n.parser))
		switch n.parser {
		case parserInteger, parserDouble:
			w.Byte(0) // no min, no max
		case parserString:
			w.VarInt(n.prop)
		case parserEntity:
			w.Byte(byte(n.prop))
		}
		if n.suggest != nil {
			w.String(askServer)
		}
	}
	w.VarInt(0) // root
}

var cmdSeed = maphash.MakeSeed()

// sendCommands sends the commands tree if it differs from the last one sent. Call it in c's
// transaction.
func (s *Session) sendCommands(c session.Controllable) {
	t := buildCommandTree(c)
	w := s.packet()
	var args []int32
	if l := s.legacy(); l != nil {
		args = l.argumentType
	}
	t.encode(w, args)
	h := maphash.Bytes(cmdSeed, w.B)
	ts := s.txt()
	ts.mu.Lock()
	same := ts.cmdHash == h
	ts.cmdHash = h
	ts.cmdTree = t // command_suggestion requests are answered from it
	ts.mu.Unlock()
	if same {
		s.writers.Put(w)
		return
	}
	s.queue(v777.ClientboundPlayCommands, w)
}

// SpawnText starts the text side once the player is in the world: it subscribes the player to
// the global chat (as the Bedrock session does in Spawn), sends the commands tree and refreshes it
// every few seconds. Call it at the end of Spawn, in the spawn transaction. The player is
// unsubscribed from the chat when the connection closes.
func (s *Session) SpawnText(c session.Controllable) {
	chat.Global.Subscribe(c)
	s.sendCommands(c)
	go func() {
		t := time.NewTicker(commandRefresh)
		defer t.Stop()
		for {
			select {
			case <-s.closed:
				chat.Global.Unsubscribe(c)
				return
			case <-t.C:
				err := s.withPlayer(func(_ *world.Tx, c session.Controllable) { s.sendCommands(c) })
				if err != nil {
					<-s.closed
					chat.Global.Unsubscribe(c)
					return
				}
			}
		}
	}()
}

// Suggestions for the arguments the server suggests values for (ask_server).

// children returns the children of node i, following a redirect (an alias) to its command.
func (t *cmdTree) children(i int32) []int32 {
	if r := t.nodes[i].redirect; r >= 0 {
		return t.nodes[r].children
	}
	return t.nodes[i].children
}

// words is how many space-separated words an argument takes; 0 for the rest of the line.
func (n cmdNode) words() int {
	switch {
	case n.parser == parserVec3:
		return 3
	case n.parser == parserString && n.prop == stringGreedy:
		return 0
	}
	return 1
}

// suggestions returns the suggestions for the last word of a command line typed so far (with or
// without its leading slash) and where that word starts in text, in UTF-16 units as Brigadier
// counts. The line is followed through the tree word by word; the last word is completed from
// the options of every ask_server argument it may be. Call it in src's transaction.
func (t *cmdTree) suggestions(src cmd.Source, text string) (start int, matches []string) {
	input := strings.TrimPrefix(text, "/")
	words := strings.Split(input, " ")
	lastWord := words[len(words)-1]
	start = utf16Len(text) - utf16Len(lastWord)

	type state struct {
		node int32
		left int // words of the node's argument still to come
	}
	states := []state{{node: 0}}
	for _, word := range words[:len(words)-1] {
		var next []state
		for _, st := range states {
			if st.left > 0 {
				next = append(next, state{st.node, st.left - 1})
				continue
			}
			for _, c := range t.children(st.node) {
				n := t.nodes[c]
				switch {
				case n.kind == nodeLiteral && n.name == word:
					next = append(next, state{node: c})
				case n.kind == nodeArgument && n.words() > 0:
					next = append(next, state{c, n.words() - 1})
				}
			}
		}
		slices.SortFunc(next, func(a, b state) int {
			if a.node != b.node {
				return int(a.node - b.node)
			}
			return a.left - b.left
		})
		states = slices.Compact(next)
		if len(states) == 0 {
			return start, nil
		}
	}
	seen := map[string]bool{}
	prefix := strings.ToLower(lastWord)
	for _, st := range states {
		if st.left > 0 {
			continue
		}
		for _, c := range t.children(st.node) {
			e := t.nodes[c].suggest
			if e == nil {
				continue
			}
			opts := e.Options(src)
			all := make(map[string]bool, len(opts))
			for _, o := range opts {
				all[o] = true
			}
			for _, o := range opts {
				if seen[o] || !strings.HasPrefix(strings.ToLower(o), prefix) {
					continue
				}
				if rest, ok := strings.CutPrefix(o, "minecraft:"); ok && all[rest] && !strings.HasPrefix(prefix, "minecraft:") {
					continue // stone, and not minecraft:stone too, unless that is being typed
				}
				seen[o] = true
				matches = append(matches, o)
			}
		}
	}
	slices.Sort(matches)
	return start, matches
}

// utf16Len is the length of s in UTF-16 code units, as Brigadier measures text.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// maxSuggestions caps one answer; Brigadier shows ten at a time and narrows them as the player types.
const maxSuggestions = 2000

// answerSuggestions answers a command_suggestion request. Call it in c's transaction.
func (s *Session) answerSuggestions(c session.Controllable, id int32, text string) {
	ts := s.txt()
	ts.mu.Lock()
	t := ts.cmdTree
	ts.mu.Unlock()
	if t == nil {
		return
	}
	start, matches := t.suggestions(c, text)
	if len(matches) > maxSuggestions {
		matches = matches[:maxSuggestions]
	}
	w := s.packet()
	w.VarInt(id)
	w.VarInt(int32(start))
	w.VarInt(int32(utf16Len(text) - start))
	w.VarInt(int32(len(matches)))
	for _, m := range matches {
		w.String(m)
		w.Bool(false) // no tooltip
	}
	s.queue(v777.ClientboundPlayCommandSuggestions, w)
}
