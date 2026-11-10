//go:build crossfly_full

package commands

import (
	"fmt"
	"os"
	"strings"
	"testing"

	_ "github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestMain(m *testing.M) {
	// the server does this at startup (world.Config.New); registry lookups panic before it
	world.DefaultBlockRegistry.Finalize()
	os.Exit(m.Run())
}

// blockCase is a block typed in a command and the Bedrock state it must give. java holds the Java properties the
// state must show as to Java players.
type blockCase struct {
	in     string
	name   string
	states map[string]any
	java   map[string]string
}

var stateCases = []blockCase{
	// Java Edition's syntax.
	{in: "minecraft:repeater[facing=north,delay=3]", name: "minecraft:unpowered_repeater",
		states: map[string]any{"repeater_delay": int32(2), "minecraft:cardinal_direction": "north"},
		java:   map[string]string{"facing": "north", "delay": "3"}},
	{in: "repeater[delay=4,facing=east]", name: "minecraft:unpowered_repeater",
		states: map[string]any{"repeater_delay": int32(3), "minecraft:cardinal_direction": "east"},
		java:   map[string]string{"facing": "east", "delay": "4"}},
	{in: "piston[facing=up]", name: "minecraft:piston", states: map[string]any{"facing_direction": int32(1)},
		java: map[string]string{"facing": "up", "extended": "false"}},
	{in: "rail[shape=north_east]", name: "minecraft:rail", states: map[string]any{"rail_direction": int32(9)},
		java: map[string]string{"shape": "north_east"}},
	{in: "rail[shape=ascending_west]", name: "minecraft:rail", states: map[string]any{"rail_direction": int32(3)},
		java: map[string]string{"shape": "ascending_west"}},
	{in: "powered_rail[shape=ascending_east]", name: "minecraft:golden_rail",
		states: map[string]any{"rail_direction": int32(2), "rail_data_bit": uint8(0)},
		java:   map[string]string{"shape": "ascending_east", "powered": "false"}},
	{in: "oak_stairs[facing=east,half=top]", name: "minecraft:oak_stairs",
		states: map[string]any{"weirdo_direction": int32(0), "upside_down_bit": true},
		java:   map[string]string{"facing": "east", "half": "top"}},
	{in: "oak_slab[type=top]", name: "minecraft:oak_slab", states: map[string]any{"minecraft:vertical_half": "top"},
		java: map[string]string{"type": "top"}},
	{in: "oak_slab[type=double]", name: "minecraft:oak_double_slab", java: map[string]string{"type": "double"}},
	{in: "oak_door[half=upper,hinge=right]", name: "minecraft:wooden_door",
		states: map[string]any{"upper_block_bit": true, "door_hinge_bit": true},
		java:   map[string]string{"half": "upper", "hinge": "right", "facing": "north"}},
	{in: "observer[facing=down]", name: "minecraft:observer", states: map[string]any{"minecraft:facing_direction": "down"},
		java: map[string]string{"facing": "down"}},
	{in: "comparator[mode=subtract,facing=west]", name: "minecraft:unpowered_comparator",
		states: map[string]any{"output_subtract_bit": uint8(1)}, java: map[string]string{"mode": "subtract", "facing": "west"}},
	{in: "oak_log[axis=x]", name: "minecraft:oak_log", states: map[string]any{"pillar_axis": "x"}},
	{in: "oak_stairs[facing=south,shape=outer_left]", name: "minecraft:oak_stairs",
		java: map[string]string{"facing": "south", "shape": "outer_left"}},
	// Java properties Bedrock does not keep (a fence's connections): the closest state, the typed ones first.
	{in: "oak_fence[north=true]", name: "minecraft:oak_fence"},

	// Bedrock Edition's syntax.
	{in: `unpowered_repeater ["minecraft:cardinal_direction"="north","repeater_delay"=2]`, name: "minecraft:unpowered_repeater",
		states: map[string]any{"repeater_delay": int32(2), "minecraft:cardinal_direction": "north"},
		java:   map[string]string{"facing": "north", "delay": "3"}},
	{in: `unpowered_repeater ["minecraft:cardinal_direction" = "west", "repeater_delay" = 1]`, name: "minecraft:unpowered_repeater",
		states: map[string]any{"repeater_delay": int32(1), "minecraft:cardinal_direction": "west"}},
	{in: `unpowered_repeater [repeater_delay=3,cardinal_direction=south]`, name: "minecraft:unpowered_repeater",
		states: map[string]any{"repeater_delay": int32(3), "minecraft:cardinal_direction": "south"}},
	{in: `unpowered_repeater["repeater_delay":1]`, name: "minecraft:unpowered_repeater",
		states: map[string]any{"repeater_delay": int32(1)}},
	{in: `piston ["facing_direction"=1]`, name: "minecraft:piston", states: map[string]any{"facing_direction": int32(1)},
		java: map[string]string{"facing": "up"}},
	{in: `rail ["rail_direction"=8]`, name: "minecraft:rail", states: map[string]any{"rail_direction": int32(8)}},
	{in: `golden_rail ["rail_direction"=4,"rail_data_bit"=true]`, name: "minecraft:golden_rail",
		states: map[string]any{"rail_direction": int32(4), "rail_data_bit": uint8(1)}},
	{in: `oak_stairs ["weirdo_direction"=0,"upside_down_bit"=true]`, name: "minecraft:oak_stairs",
		states: map[string]any{"weirdo_direction": int32(0), "upside_down_bit": true},
		java:   map[string]string{"facing": "east", "half": "top"}},
	{in: `oak_slab ["minecraft:vertical_half"="top"]`, name: "minecraft:oak_slab",
		states: map[string]any{"minecraft:vertical_half": "top"}, java: map[string]string{"type": "top"}},
	{in: `wooden_door ["upper_block_bit"=true,"door_hinge_bit"=true]`, name: "minecraft:wooden_door",
		states: map[string]any{"upper_block_bit": true, "door_hinge_bit": true},
		java:   map[string]string{"half": "upper", "hinge": "right"}},
	{in: `observer ["minecraft:facing_direction"="up"]`, name: "minecraft:observer",
		states: map[string]any{"minecraft:facing_direction": "up"}, java: map[string]string{"facing": "up"}},
	// A Bedrock item name, or a Java name, stands for its block.
	{in: `repeater ["repeater_delay"=3]`, name: "minecraft:unpowered_repeater", states: map[string]any{"repeater_delay": int32(3)}},

	// Plain names, as before.
	{in: "stone", name: "minecraft:stone"},
	{in: "minecraft:oak_planks", name: "minecraft:oak_planks"},
	{in: "air", name: "minecraft:air"},
	{in: "unpowered_repeater", name: "minecraft:unpowered_repeater"},
	{in: "stone[]", name: "minecraft:stone"},
}

func TestParseBlockStates(t *testing.T) {
	for _, c := range stateCases {
		s, err := ParseBlock(c.in)
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		name, props := s.Block().EncodeBlock()
		if name != c.name {
			t.Errorf("%s: block %s, want %s", c.in, name, c.name)
		}
		for k, want := range c.states {
			got := props[k]
			if v, ok := got.(uint8); ok {
				if _, ok := want.(bool); ok {
					got = v != 0 // a palette state: a byte where Dragonfly's blocks use a bool
				}
			}
			if got != want {
				t.Errorf("%s: state %s = %v (%T), want %v; states %v", c.in, k, got, got, want, props)
			}
		}
		if c.java != nil {
			_, jp, _ := javaState(s.Block())
			for k, want := range c.java {
				if jp[k] != want {
					t.Errorf("%s: Java %s=%s, want %s; Java state %v", c.in, k, jp[k], want, jp)
				}
			}
		}
	}
}

func TestParseBlockWaterlogged(t *testing.T) {
	s, err := ParseBlock("oak_stairs[waterlogged=true,facing=west]")
	if err != nil {
		t.Fatal(err)
	}
	if s.Liquid() == nil {
		t.Errorf("waterlogged=true: no water")
	}
	s, _ = ParseBlock("oak_stairs[waterlogged=false]")
	if s.Liquid() != nil {
		t.Errorf("waterlogged=false: water")
	}
}

func TestParseBlockErrors(t *testing.T) {
	for in, want := range map[string]string{
		"nosuchblock":                             "Unknown block: nosuchblock",
		"nosuchblock[facing=up]":                  "Unknown block: nosuchblock",
		"repeater[colour=red]":                    "Block minecraft:repeater does not have property 'colour'",
		"repeater[delay=9]":                       "Block minecraft:repeater does not accept '9' for delay property",
		"repeater[delay=2,delay=3]":               "Property 'delay' can only be set once",
		"repeater[delay=2":                        "Expected closing ]",
		"repeater[delay]":                         "Expected = after block state delay",
		`unpowered_repeater ["colour"="red"]`:     `Block minecraft:unpowered_repeater has no block state "colour"`,
		`unpowered_repeater ["repeater_delay"=7]`: `Block state "repeater_delay" of minecraft:unpowered_repeater does not accept 7`,
		`piston ["facing_direction"="up"]`:        `Block state "facing_direction" of minecraft:piston does not accept up`,
		`chest[facing=north]{Items:[]}`:           "Block entity data",
	} {
		_, err := ParseBlock(in)
		if err == nil {
			t.Errorf("%s: no error, want %q", in, want)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error %q, want %q", in, err, want)
		}
	}
}

func TestBlockFilter(t *testing.T) {
	block := func(in string) world.Block {
		t.Helper()
		s, err := ParseBlock(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		return s.Block()
	}
	topEast := block("oak_stairs[facing=east,half=top]")
	bottomNorth := block("oak_stairs[facing=north,half=bottom]")
	stone := block("stone")
	repeater := block("repeater[delay=3]")
	powered := block(`powered_repeater ["repeater_delay"=1]`)
	airB := block("air")

	for _, c := range []struct {
		filter string
		in     []world.Block
		out    []world.Block
	}{
		// Without states: every state of the block.
		{"oak_stairs", []world.Block{topEast, bottomNorth}, []world.Block{stone, repeater}},
		{"minecraft:oak_stairs", []world.Block{topEast, bottomNorth}, []world.Block{stone}},
		{"repeater", []world.Block{repeater, powered}, []world.Block{stone}},
		{"stone", []world.Block{stone}, []world.Block{topEast, airB}},
		{"air", []world.Block{airB, nil}, []world.Block{stone}},
		// With Java states: those properties only.
		{"oak_stairs[half=top]", []world.Block{topEast}, []world.Block{bottomNorth, stone}},
		{"oak_stairs[facing=north]", []world.Block{bottomNorth}, []world.Block{topEast}},
		{"repeater[delay=3]", []world.Block{repeater}, []world.Block{powered}},
		{"repeater[powered=true]", []world.Block{powered}, []world.Block{repeater}},
		// With Bedrock states: those states only.
		{`oak_stairs ["upside_down_bit"=true]`, []world.Block{topEast}, []world.Block{bottomNorth, stone}},
		{`unpowered_repeater ["repeater_delay"=2]`, []world.Block{repeater}, []world.Block{powered}},
		{`powered_repeater ["repeater_delay"=1]`, []world.Block{powered}, []world.Block{repeater}},
	} {
		f, err := ParseBlock(c.filter)
		if err != nil {
			t.Errorf("%s: %v", c.filter, err)
			continue
		}
		for _, b := range c.in {
			if !f.Matches(b) {
				t.Errorf("filter %s does not match %v", c.filter, describe(b))
			}
		}
		for _, b := range c.out {
			if f.Matches(b) {
				t.Errorf("filter %s matches %v", c.filter, describe(b))
			}
		}
	}
}

func describe(b world.Block) string {
	if b == nil {
		return "nil"
	}
	name, props := b.EncodeBlock()
	return fmt.Sprintf("%s %v", name, props)
}

// --- the command line: block arguments with states in one or several words

type fakeSource struct{}

func (fakeSource) Position() mgl64.Vec3          { return mgl64.Vec3{} }
func (fakeSource) SendCommandOutput(*cmd.Output) {}

type scanned struct {
	Block  BlockArg                     `cmd:"block"`
	Mode   cmd.Optional[fillMode]       `cmd:"mode"`
	Filter cmd.Optional[BlockFilterArg] `cmd:"filter"`
}

var lastScan *scanned

func (s scanned) Run(cmd.Source, *cmd.Output, *world.Tx) { lastScan = &s }

func TestScanBlock(t *testing.T) {
	c := cmd.New("scantest", "", nil, scanned{})
	for _, tc := range []struct{ args, block, filter string }{
		{"stone", "stone", ""},
		{"minecraft:repeater[facing=north,delay=3]", "minecraft:repeater[facing=north,delay=3]", ""},
		{"repeater[facing=north, delay=3] replace stone", "repeater[facing=north, delay=3]", "stone"},
		{`unpowered_repeater ["minecraft:cardinal_direction"="north","repeater_delay"=2]`,
			`unpowered_repeater ["minecraft:cardinal_direction"="north","repeater_delay"=2]`, ""},
		{`unpowered_repeater ["minecraft:cardinal_direction" = "north", "repeater_delay" = 2] replace oak_stairs ["upside_down_bit"=true]`,
			`unpowered_repeater ["minecraft:cardinal_direction"="north","repeater_delay"=2]`, `oak_stairs ["upside_down_bit"=true]`},
		{"stone replace oak_stairs[half=top, facing=east]", "stone", "oak_stairs[half=top, facing=east]"},
		{"stone replace", "stone", ""},
	} {
		lastScan = nil
		c.Execute(tc.args, fakeSource{}, nil)
		if lastScan == nil {
			t.Errorf("%s: not run", tc.args)
			continue
		}
		if string(lastScan.Block) != tc.block {
			t.Errorf("%s: block %q, want %q", tc.args, lastScan.Block, tc.block)
		}
		f, _ := lastScan.Filter.Load()
		if string(f) != tc.filter {
			t.Errorf("%s: filter %q, want %q", tc.args, f, tc.filter)
		}
		if _, err := ParseBlock(string(lastScan.Block)); err != nil {
			t.Errorf("%s: %v", tc.args, err)
		}
		if f != "" {
			if _, err := ParseBlock(string(f)); err != nil {
				t.Errorf("%s: filter: %v", tc.args, err)
			}
		}
	}
}
