//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world/sound"
	"testing"
)

func TestJukeboxComparatorReadings(t *testing.T) {
	expected := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 14, 13, 15, 14, 12, 11, 13, 10, 9}
	for i, disc := range sound.MusicDiscs() {
		got, ok := redstoneAnalogOutput(cube.Pos{}, Jukebox{Item: item.NewStack(item.MusicDisc{DiscType: disc}, 1)}, nil)
		if !ok || got != expected[i] {
			t.Errorf("disc %s: %d, want %d", disc.String(), got, expected[i])
		}
	}
	got, ok := redstoneAnalogOutput(cube.Pos{}, Jukebox{}, nil)
	if !ok || got != 0 {
		t.Fatal("empty jukebox must read zero")
	}
}
func TestLecternComparatorReadings(t *testing.T) {
	for _, c := range []struct{ pages, page, power int }{{0, 0, 15}, {1, 0, 15}, {2, 0, 1}, {2, 1, 15}, {3, 1, 8}, {4, 1, 5}, {4, 2, 10}, {15, 7, 8}, {100, 98, 14}, {100, 99, 15}} {
		for _, written := range []bool{false, true} {
			var book item.Stack
			if written {
				book = item.NewStack(item.WrittenBook{Pages: make([]string, c.pages)}, 1)
			} else {
				book = item.NewStack(item.BookAndQuill{Pages: make([]string, c.pages)}, 1)
			}
			got, ok := redstoneAnalogOutput(cube.Pos{}, Lectern{Book: book, Page: c.page}, nil)
			if !ok || got != c.power {
				t.Errorf("pages=%d page=%d written=%v: %d want%d", c.pages, c.page, written, got, c.power)
			}
		}
	}
	got, ok := redstoneAnalogOutput(cube.Pos{}, Lectern{}, nil)
	if !ok || got != 0 {
		t.Fatal("empty lectern must read zero")
	}
}
