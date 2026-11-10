//go:build crossfly_full

package hostile

import (
	"testing"

	"github.com/google/uuid"
)

func suspect(n byte, player bool) Suspect {
	var id uuid.UUID
	id[15] = n
	return Suspect{ID: id, Player: player}
}

// TestAngerLevels: AngerLevel.byAnger thresholds.
func TestAngerLevels(t *testing.T) {
	for anger, want := range map[int]AngerLevel{0: AngerCalm, 39: AngerCalm, 40: AngerAgitated, 79: AngerAgitated, 80: AngerAngry, 150: AngerAngry} {
		if got := angerLevelOf(anger); got != want {
			t.Errorf("anger %d: level %d, want %d", anger, got, want)
		}
	}
}

// TestAngerSorting: angry suspects first, then players, then the angriest;
// the highest anger is tracked; anger is capped at 150.
func TestAngerSorting(t *testing.T) {
	a := NewAngerManagement(newRand(), nil)
	zombie := suspect(1, false)
	player := suspect(2, true)
	husk := suspect(3, false)
	a.IncreaseAnger(zombie, 90) // angry mob
	a.IncreaseAnger(player, 60) // agitated player
	a.IncreaseAnger(husk, 70)   // agitated mob
	order := []Suspect{zombie, player, husk}
	for i, s := range a.suspects {
		if s != order[i] {
			t.Fatalf("order %v, want zombie, player, husk", a.suspects)
		}
	}
	if a.ActiveAnger(nil) != 90 {
		t.Errorf("highest anger %d", a.ActiveAnger(nil))
	}
	if got := a.IncreaseAnger(player, 200); got != 150 {
		t.Errorf("capped anger %d", got)
	}
	if a.suspects[0] != player {
		t.Errorf("an angry player comes before an angry mob: %v", a.suspects)
	}
	top, ok := a.TopSuspect(func(s Suspect) bool { return s != player })
	if !ok || top != zombie {
		t.Errorf("top suspect without the player: %v", top)
	}
	a.ClearAnger(player.ID)
	if a.AngerAt(player.ID) != 0 || len(a.suspects) != 2 {
		t.Errorf("clear anger left %v", a.suspects)
	}
}

// TestAngerTick: anger decays one a tick; invalid or removed suspects go,
// unloaded ones keep their anger by UUID and get it back once loaded.
func TestAngerTick(t *testing.T) {
	a := NewAngerManagement(newRand(), nil)
	p := suspect(1, true)
	gone := suspect(2, false)
	a.IncreaseAnger(p, 50)
	a.IncreaseAnger(gone, 50)
	loaded := map[uuid.UUID]bool{p.ID: true}
	removal := map[uuid.UUID]SuspectRemoval{}
	lookup := func(id uuid.UUID) (Suspect, bool) {
		if loaded[id] {
			return Suspect{ID: id, Player: true}, true
		}
		return Suspect{}, false
	}
	status := func(s Suspect) (bool, SuspectRemoval) { return true, removal[s.ID] }
	a.Tick(lookup, status)
	if a.AngerAt(p.ID) != 49 {
		t.Errorf("anger after a tick %d", a.AngerAt(p.ID))
	}
	removal[gone.ID] = SuspectGone
	removal[p.ID] = SuspectUnloaded
	loaded[p.ID] = false
	a.Tick(lookup, status)
	if len(a.suspects) != 0 {
		t.Fatalf("suspects left %v", a.suspects)
	}
	if a.saved[p.ID] != 48 || len(a.saved) != 1 {
		t.Errorf("saved %v, want the player at 48", a.saved)
	}
	// Back: converted on the next conversion tick (every 2 ticks).
	loaded[p.ID] = true
	removal[p.ID] = SuspectPresent
	for i := 0; i < 2; i++ {
		a.Tick(lookup, status)
	}
	if v := a.AngerAt(p.ID); v < 44 || v > 46 || len(a.saved) != 0 {
		t.Errorf("anger back %d, saved %v", v, a.saved)
	}
	// Invalid targets lose their anger.
	a.Tick(lookup, func(Suspect) (bool, SuspectRemoval) { return false, SuspectPresent })
	if len(a.suspects) != 0 || a.ActiveAnger(nil) != 0 {
		t.Errorf("invalid suspect kept: %v", a.suspects)
	}
}

// TestAngerSaved: a new suspect gets back its saved anger, the saved list
// lists loaded suspects first.
func TestAngerSaved(t *testing.T) {
	p := suspect(7, true)
	q := suspect(8, false)
	a := NewAngerManagement(newRand(), map[uuid.UUID]int{p.ID: 30, q.ID: 10})
	if got := a.IncreaseAnger(p, 35); got != 65 {
		t.Errorf("anger with saved %d, want 65", got)
	}
	s := a.Saved()
	if len(s) != 2 || s[0] != (SavedAnger{p.ID, 65}) || s[1] != (SavedAnger{q.ID, 10}) {
		t.Errorf("saved %v", s)
	}
}

// TestWakeOffsets: SilverfishWakeUpFriendsGoal visits 0, 1, -1, 2, -2, ...
func TestWakeOffsets(t *testing.T) {
	got := wakeOffsets(2)
	want := []int{0, 1, -1, 2, -2}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%v, want %v", got, want)
		}
	}
	if n := len(wakeOffsets(10)); n != 21 {
		t.Errorf("%d offsets for 10", n)
	}
}
