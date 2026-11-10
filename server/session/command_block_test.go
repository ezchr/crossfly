package session

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

type testBlockArg string

func (testBlockArg) Type() string                             { return "TestBlock" }
func (testBlockArg) Options(cmd.Source) []string              { return []string{"stone", "unpowered_repeater"} }
func (testBlockArg) Parse(l *cmd.Line, v reflect.Value) error { return nil }
func (testBlockArg) BlockPredicate() bool                     { return false }

type testFillMode string

func (testFillMode) Type() string                { return "TestFillMode" }
func (testFillMode) Options(cmd.Source) []string { return []string{"replace"} }

type testFill struct {
	From   mgl64.Vec3                 `cmd:"from"`
	To     mgl64.Vec3                 `cmd:"to"`
	Block  testBlockArg               `cmd:"block"`
	Mode   cmd.Optional[testFillMode] `cmd:"mode"`
	Filter cmd.Optional[testBlockArg] `cmd:"filter"`
}

func (testFill) Run(cmd.Source, *cmd.Output, *world.Tx) {}

// TestBlockStatesParameter checks that a block parameter is followed by an optional block states
// argument, as BDS sends /fill's, so that the client sends unpowered_repeater ["repeater_delay"=2].
func TestBlockStatesParameter(t *testing.T) {
	pk, _ := BuildAvailableCommands(map[string]cmd.Command{"tfill": cmd.New("tfill", "", nil, testFill{})}, nil, nil)
	if len(pk.Commands) != 1 || len(pk.Commands[0].Overloads) != 1 {
		t.Fatalf("commands %+v", pk.Commands)
	}
	var names []string
	for _, p := range pk.Commands[0].Overloads[0].Parameters {
		names = append(names, p.Name)
		if p.Name == "blockStates" || p.Name == "filterStates" {
			if p.Type != protocol.CommandArgValid|protocol.CommandArgTypeBlockStates || !p.Optional {
				t.Errorf("%s: type %#x optional %v, want optional block states", p.Name, p.Type, p.Optional)
			}
		}
	}
	want := []string{"from", "to", "block", "blockStates", "mode", "filter", "filterStates"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("parameters %v, want %v", names, want)
	}
}
