package jpoi

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// fakeWorld is a set of PoI blocks for the test scanner.
type fakeWorld map[cube.Pos]*Type

func (f fakeWorld) scan(_ *world.Tx, c world.ChunkPos) map[cube.Pos]*Type {
	out := map[cube.Pos]*Type{}
	for p, t := range f {
		if int32(p[0]>>4) == c[0] && int32(p[2]>>4) == c[1] {
			out[p] = t
		}
	}
	return out
}

func TestTakeReleaseAndOccupancy(t *testing.T) {
	w := fakeWorld{{0, 64, 0}: Home, {3, 64, 0}: Home, {40, 64, 0}: Home, {5, 64, 5}: Librarian}
	m := NewTestManager(-64, 319, w.scan)
	got := m.FindAllClosestFirstWithType(nil, Is(Home), nil, cube.Pos{2, 64, 0}, 48, HasSpace)
	if len(got) != 3 || got[0].Pos != (cube.Pos{3, 64, 0}) || got[1].Pos != (cube.Pos{0, 64, 0}) || got[2].Pos != (cube.Pos{40, 64, 0}) {
		t.Fatalf("closest first: %v", got)
	}
	p, ok := m.Take(nil, Is(Home), func(_ *Type, p cube.Pos) bool { return p == cube.Pos{3, 64, 0} }, cube.Pos{2, 64, 0}, 48)
	if !ok || p != (cube.Pos{3, 64, 0}) {
		t.Fatal("take failed")
	}
	if _, ok := m.Take(nil, Is(Home), func(_ *Type, p cube.Pos) bool { return p == cube.Pos{3, 64, 0} }, cube.Pos{2, 64, 0}, 48); ok {
		t.Fatal("took a bed with no tickets left")
	}
	if n := m.CountInRange(nil, Is(Home), cube.Pos{2, 64, 0}, 48, HasSpace); n != 2 {
		t.Fatalf("free beds %d, want 2", n)
	}
	if n := m.CountInRange(nil, Is(Home), cube.Pos{2, 64, 0}, 48, IsOccupied); n != 1 {
		t.Fatalf("occupied beds %d, want 1", n)
	}
	if !m.Release(nil, cube.Pos{3, 64, 0}) || m.Release(nil, cube.Pos{3, 64, 0}) {
		t.Fatal("release should succeed once")
	}
	if _, ok := m.Find(nil, IsAcquirableJobSite, nil, cube.Pos{0, 64, 0}, 48, HasSpace); !ok {
		t.Fatal("lectern not found as a job site")
	}
}

func TestSectionsToVillage(t *testing.T) {
	w := fakeWorld{{8, 64, 8}: Home}
	m := NewTestManager(-64, 319, w.scan)
	if d := m.SectionsToVillage(nil, cube.Pos{8, 64, 8}); d != 7 {
		t.Fatalf("unclaimed bed made a village: %d", d)
	}
	m.EnsureLoadedAndValid(nil, cube.Pos{8, 64, 8}, 16)
	if _, ok := m.Take(nil, Is(Home), nil, cube.Pos{8, 64, 8}, 16); !ok {
		t.Fatal("take")
	}
	if d := m.SectionsToVillage(nil, cube.Pos{8, 64, 8}); d != 0 {
		t.Fatalf("distance in the centre %d", d)
	}
	if d := m.SectionsToVillage(nil, cube.Pos{8 + 16*3, 64 + 16*2, 8}); d != 3 {
		t.Fatalf("distance 3 sections away %d", d)
	}
	if !m.IsVillage(nil, cube.Pos{20, 64, 8}) || m.IsVillage(nil, cube.Pos{8 + 32, 64, 8}) {
		t.Fatal("isVillage wrong")
	}
	m.Release(nil, cube.Pos{8, 64, 8})
	if d := m.SectionsToVillage(nil, cube.Pos{8, 64, 8}); d != 7 {
		t.Fatalf("released bed still a village centre: %d", d)
	}
}

func TestRescanDropsBrokenBlocks(t *testing.T) {
	w := fakeWorld{{1, 64, 1}: Farmer}
	m := NewTestManager(-64, 319, w.scan)
	if !m.Exists(nil, cube.Pos{1, 64, 1}, Is(Farmer)) {
		t.Fatal("composter missing")
	}
	delete(w, cube.Pos{1, 64, 1})
	// Without a transaction the change log cannot be read: the periodic
	// rescan notices.
	m.SetTime(rescanTicks + 1)
	if m.Exists(nil, cube.Pos{1, 64, 1}, Is(Farmer)) {
		t.Fatal("composter still there after a rescan")
	}
}
