//go:build crossfly_full

package spawn

import "testing"

func TestDarkEnoughToSpawn(t *testing.T) {
	ten := func() int { return 10 }
	two := func() int { return 2 }
	for _, c := range []struct {
		name               string
		raw, block         int
		ultraWarm, thunder bool
		thunderRaw         func() int
		want               bool
	}{
		{"dark", 0, 0, false, false, ten, true},
		{"raw 7", 7, 0, false, false, ten, true},
		{"raw 8", 8, 0, false, false, ten, false},
		{"any block light", 1, 1, false, false, ten, false},
		{"torch far away", 3, 3, false, false, ten, false},
		{"thunder darkens the sky", 12, 0, false, true, two, true},
		{"thunder, still bright", 3, 0, false, true, ten, false},
		{"nether ignores block light", 7, 7, true, false, ten, true},
		{"nether too bright", 8, 8, true, false, ten, false},
	} {
		if got := darkEnough(c.raw, c.block, c.ultraWarm, c.thunder, c.thunderRaw); got != c.want {
			t.Errorf("%s: darkEnough = %v, want %v", c.name, got, c.want)
		}
	}
	for _, id := range []string{"minecraft:zombie", "minecraft:drowned", "minecraft:skeleton", "minecraft:creeper", "minecraft:spider", "minecraft:enderman", "minecraft:witch", "minecraft:wither_skeleton", "minecraft:bogged"} {
		if !monsterSpawnTypes[id] {
			t.Errorf("%s does not take the monster darkness check", id)
		}
	}
	for _, id := range []string{"minecraft:slime", "minecraft:magma_cube", "minecraft:guardian", "minecraft:zombie_pigman", "minecraft:cow", "minecraft:phantom"} {
		if monsterSpawnTypes[id] {
			t.Errorf("%s takes the monster darkness check", id)
		}
	}
}
