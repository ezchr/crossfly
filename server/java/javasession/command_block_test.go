package javasession

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// testBlock is a block parameter, as /setblock's (testBlockFilter as /fill's replace filter).
type testBlock string

func (testBlock) Type() string                             { return "JTestBlock" }
func (testBlock) Options(cmd.Source) []string              { return []string{"stone", "minecraft:stone"} }
func (testBlock) Parse(l *cmd.Line, v reflect.Value) error { return nil }
func (testBlock) BlockPredicate() bool                     { return false }

type testBlockFilter string

func (testBlockFilter) Type() string                             { return "JTestBlock" }
func (testBlockFilter) Options(cmd.Source) []string              { return []string{"stone", "minecraft:stone"} }
func (testBlockFilter) Parse(l *cmd.Line, v reflect.Value) error { return nil }
func (testBlockFilter) BlockPredicate() bool                     { return true }

type testSetblock struct {
	Pos    mgl64.Vec3                    `cmd:"position"`
	Block  testBlock                     `cmd:"block"`
	Filter cmd.Optional[testBlockFilter] `cmd:"filter"`
}

func (testSetblock) Run(cmd.Source, *cmd.Output, *world.Tx) {}

// TestBlockStateArguments checks that block parameters are minecraft:block_state arguments (and
// filters minecraft:block_predicate), which the client completes with the states of the block, not
// literals or a list the server suggests.
func TestBlockStateArguments(t *testing.T) {
	cmd.Register(cmd.New("jsetblock", "", nil, testSetblock{}))
	tr := buildCommandTree(nil)
	var root int32 = -1
	for i, n := range tr.nodes {
		if n.name == "jsetblock" {
			root = int32(i)
		}
	}
	if root < 0 {
		t.Fatal("test command missing from the tree")
	}
	pos := tr.nodes[tr.nodes[root].children[0]]
	block := tr.nodes[pos.children[0]]
	if block.kind != nodeArgument || block.parser != parserBlock || block.suggest != nil {
		t.Errorf("block node %+v: want a minecraft:block_state argument", block)
	}
	if len(block.children) != 1 {
		t.Fatalf("block node children %v: want the filter", block.children)
	}
	if filter := tr.nodes[block.children[0]]; filter.parser != parserBlockP {
		t.Errorf("filter node %+v: want a minecraft:block_predicate argument", filter)
	}
}
