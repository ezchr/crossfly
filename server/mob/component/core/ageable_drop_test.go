//go:build crossfly_full

package core_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	. "github.com/df-mc/dragonfly/server/mob/component/core/coretest"
	"github.com/df-mc/dragonfly/server/world"
)

// turtle is the vanilla turtle's baby ageable (the component JSON copied
// verbatim but for a duration of 1 second instead of 1200).
var turtle = &Def{ID: "test:turtle", Category: "creature",
	Base: []mob.RawComponent{
		C("minecraft:health", `{"value":30,"max":30}`),
	},
	Groups: map[string][]mob.RawComponent{
		"baby": {
			C("minecraft:is_baby", `{}`),
			C("minecraft:ageable", `{"duration":1,"feed_items":["seagrass"],"drop_items":["turtle_shell_piece"],"pause_growth_items":["golden_dandelion"],"reset_growth_items":["golden_dandelion"],"grow_up":{"event":"minecraft:ageable_grow_up","target":"self"}}`),
		},
		"adult": {},
	},
	Events: map[string]Event{
		mob.EventBorn:   {Add: []string{"baby"}},
		mob.EventGrowUp: {Remove: []string{"baby"}, Add: []string{"adult"}},
	},
}

func init() { Register(turtle) }

// TestTurtleShellPieceIsScute: the turtle JSON's old name of the scute
// resolves to the turtle scute item.
func TestTurtleShellPieceIsScute(t *testing.T) {
	s, ok := core.ParseItemDescriptor("turtle_shell_piece").Stack(1)
	if !ok {
		t.Fatal("turtle_shell_piece resolves to no item")
	}
	if _, ok := s.Item().(item.Scute); !ok {
		t.Fatalf("turtle_shell_piece is %v, want the turtle scute", s.Item())
	}
	if !core.ParseItemDescriptor("minecraft:turtle_shell_piece").Matches(item.NewStack(item.Scute{}, 1)) {
		t.Error("turtle_shell_piece does not match a turtle scute")
	}
}

// scutes counts the turtle scutes lying in the world.
func scutes(tx *world.Tx) int {
	n := 0
	for _, s := range Items(tx) {
		if _, ok := s.Item().(item.Scute); ok {
			n += s.Count()
		}
	}
	return n
}

// TestAgeableDropsOnGrowUp: a baby turtle drops one scute when it grows up
// (drop_items), by time and by feeding, and none before.
func TestAgeableDropsOnGrowUp(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx)
		baby, _ := Spawn(t, tx, "test:turtle", at, mob.EventBorn)
		TickAll(tx, 10, baby)
		if n := scutes(tx); n != 0 || !InstOf(baby).HasGroup("baby") {
			Fatalf(t, "%v scutes before growing up", n)
		}
		TickAll(tx, 15, baby)
		if InstOf(baby).HasGroup("baby") {
			Fatal(t, "baby turtle did not grow up")
		}
		if n := scutes(tx); n != 1 {
			Fatalf(t, "%v scutes after growing up, want 1", n)
		}
		TickAll(tx, 30, baby)
		if n := scutes(tx); n != 1 {
			Fatalf(t, "%v scutes later on, want still 1", n)
		}
	})
}
