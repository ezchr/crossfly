package cmd

import "testing"

func TestCompactBlockStates(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`~ ~ ~ stone`, `~ ~ ~ stone`},
		{`~ ~ ~ unpowered_repeater ["minecraft:cardinal_direction" = "north", "repeater_delay"=2]`,
			`~ ~ ~ unpowered_repeater ["minecraft:cardinal_direction"="north","repeater_delay"=2]`},
		{`0 0 0 1 1 1 stone replace wool [ "color" : "red" ]`, `0 0 0 1 1 1 stone replace wool ["color":"red"]`},
		{`~ ~ ~ repeater[facing=north, delay=3]`, `~ ~ ~ repeater[facing=north, delay=3]`}, // no quotes: split, rejoined by the parameter
		{`@a[name="A B", r=5] hi`, `@a[name="A B", r=5] hi`},                               // not a block state
		{`"[x" = y]`, `"[x" = y]`},
	} {
		if got := compactBlockStates(c.in); got != c.want {
			t.Errorf("compactBlockStates(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
