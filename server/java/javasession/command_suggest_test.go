package javasession

import (
	"bytes"
	"fmt"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/java/protocol/wire"
	"github.com/df-mc/dragonfly/server/world"
)

// bigEnum has more options than fit as literals, like the item names of /give.
type bigEnum string

func (bigEnum) Type() string { return "JTestItem" }
func (bigEnum) Options(cmd.Source) []string {
	opts := []string{"diamond", "minecraft:diamond", "diamond_sword", "minecraft:diamond_sword", "stone"}
	for i := range maxEnumLiterals {
		opts = append(opts, fmt.Sprintf("filler_%03d", i))
	}
	return opts
}

type testGive struct {
	Targets []cmd.Target      `cmd:"player"`
	Item    bigEnum           `cmd:"item"`
	Amount  cmd.Optional[int] `cmd:"amount"`
}

func (testGive) Run(cmd.Source, *cmd.Output, *world.Tx) {}

type testKill struct {
	Targets cmd.Optional[[]cmd.Target] `cmd:"target"`
}

func (testKill) Run(cmd.Source, *cmd.Output, *world.Tx) {}

// TestCommandSuggestions checks that player parameters are players-only entity arguments (the
// client then suggests player names, not the UUID of the entity it looks at), and that an enum too
// big for literals is an ask_server argument the server completes from its options.
func TestCommandSuggestions(t *testing.T) {
	cmd.Register(cmd.New("jgive", "", []string{"jg"}, testGive{}))
	cmd.Register(cmd.New("jkill", "", nil, testKill{}))
	tr := buildCommandTree(nil)
	var give, kill int32 = -1, -1
	for i, n := range tr.nodes {
		switch n.name {
		case "jgive":
			give = int32(i)
		case "jkill":
			kill = int32(i)
		}
	}
	if give < 0 || kill < 0 {
		t.Fatal("test commands missing from the tree")
	}
	player := tr.nodes[tr.nodes[give].children[0]]
	if player.parser != parserEntity || player.prop != entityPlayersOnly {
		t.Errorf("/jgive player node %+v: want a players-only entity argument", player)
	}
	if target := tr.nodes[tr.nodes[kill].children[0]]; target.parser != parserEntity || target.prop != 0 {
		t.Errorf("/jkill target node %+v: want an entity argument for any entity", target)
	}
	item := tr.nodes[player.children[0]]
	if item.suggest == nil || item.parser != parserResLoc {
		t.Errorf("/jgive item node %+v: want a resource location the server suggests for", item)
	}
	var w wire.Writer
	tr.encode(&w, nil)
	if !bytes.Contains(w.B, []byte(askServer)) {
		t.Error("ask_server missing from the encoded tree")
	}

	for _, c := range []struct {
		text  string
		start int
		want  []string
	}{
		{"/jgive TestPlayer dia", 18, []string{"diamond", "diamond_sword"}},
		{"/jgive TestPlayer minecraft:dia", 18, []string{"minecraft:diamond", "minecraft:diamond_sword"}},
		{"/jg @a DIAMOND_", 7, []string{"diamond_sword"}}, // alias, case-insensitive
		{"jgive Ezchr st", 12, []string{"stone"}},
		{"/jgive Ezchr diamond 2", 21, nil}, // an int: nothing to suggest
		{"/jkill x", 7, nil},
		{"/nosuchcommand a b", 17, nil},
	} {
		start, got := tr.suggestions(nil, c.text)
		if start != c.start || !slices.Equal(got, c.want) {
			t.Errorf("suggestions(%q) = %d %q, want %d %q", c.text, start, got, c.start, c.want)
		}
	}
}
