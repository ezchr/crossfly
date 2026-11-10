//go:build crossfly_full

package hostile

import (
	"math/rand/v2"
	"slices"

	"github.com/google/uuid"
)

// AngerLevel is the warden's AngerLevel.
type AngerLevel uint8

const (
	AngerCalm AngerLevel = iota
	AngerAgitated
	AngerAngry
)

// Anger thresholds and limits (AngerLevel, AngerManagement).
const (
	angerAgitatedMin = 40
	angerAngryMin    = 80
	maxAnger         = 150
	angerConversion  = 2
)

// angerLevelOf is AngerLevel.byAnger.
func angerLevelOf(anger int) AngerLevel {
	switch {
	case anger >= angerAngryMin:
		return AngerAngry
	case anger >= angerAgitatedMin:
		return AngerAgitated
	}
	return AngerCalm
}

// Angry is AngerLevel.isAngry.
func (l AngerLevel) Angry() bool { return l == AngerAngry }

// SuspectRemoval is why a suspect stopped being valid (Entity.RemovalReason
// as AngerManagement.tick reads it).
type SuspectRemoval uint8

const (
	// SuspectPresent: the entity is still in the world.
	SuspectPresent SuspectRemoval = iota
	// SuspectGone: killed or discarded: its anger is forgotten.
	SuspectGone
	// SuspectUnloaded: changed dimension or unloaded (with its chunk or its
	// player): its anger is kept by UUID and picked up again when it is back.
	SuspectUnloaded
)

// Suspect is an entity the warden may be angry at.
type Suspect struct {
	ID     uuid.UUID
	Player bool
}

// AngerManagement is the warden's AngerManagement: anger per suspect (0 to
// 150), decaying by one a tick, with the suspects sorted angry first, players
// first, angriest first.
type AngerManagement struct {
	conversionDelay int
	highestAnger    int
	suspects        []Suspect
	anger           map[uuid.UUID]int
	// saved is angerByUuid: anger of suspects not loaded (yet).
	saved      map[uuid.UUID]int
	savedOrder []uuid.UUID
}

// NewAngerManagement is the AngerManagement constructor with saved anger
// (from the "suspects" list of the warden's NBT).
func NewAngerManagement(r *rand.Rand, saved map[uuid.UUID]int) *AngerManagement {
	a := &AngerManagement{
		conversionDelay: randomBetweenInclusive(r, 0, angerConversion),
		anger:           map[uuid.UUID]int{},
		saved:           map[uuid.UUID]int{},
	}
	for id, v := range saved {
		a.saved[id] = v
		a.savedOrder = append(a.savedOrder, id)
	}
	slices.SortFunc(a.savedOrder, func(x, y uuid.UUID) int { return slices.Compare(x[:], y[:]) })
	return a
}

// Tick is AngerManagement.tick: every 2 ticks saved anger of suspects that
// are loaded again moves back; all anger drops by one; suspects that are no
// longer valid targets (or are gone) are dropped, keeping the anger of those
// only unloaded. lookup reports whether a suspect is loaded (and so may be
// returned), valid (a valid target) and why it was removed.
func (a *AngerManagement) Tick(lookup func(id uuid.UUID) (s Suspect, loaded bool), status func(s Suspect) (valid bool, removal SuspectRemoval)) {
	a.conversionDelay--
	if a.conversionDelay <= 0 {
		a.convertFromUUIDs(lookup)
		a.conversionDelay = angerConversion
	}
	for _, id := range slices.Clone(a.savedOrder) {
		if v := a.saved[id]; v <= 1 {
			a.removeSaved(id)
		} else {
			a.saved[id] = v - 1
		}
	}
	kept := a.suspects[:0]
	for _, s := range a.suspects {
		v := a.anger[s.ID]
		valid, removal := status(s)
		if v > 1 && valid && removal == SuspectPresent {
			a.anger[s.ID] = v - 1
			kept = append(kept, s)
			continue
		}
		delete(a.anger, s.ID)
		if v > 1 && removal == SuspectUnloaded {
			a.putSaved(s.ID, v-1)
		}
	}
	a.suspects = kept
	a.sortAndUpdateHighestAnger()
}

func (a *AngerManagement) putSaved(id uuid.UUID, v int) {
	if _, ok := a.saved[id]; !ok {
		a.savedOrder = append(a.savedOrder, id)
	}
	a.saved[id] = v
}

func (a *AngerManagement) removeSaved(id uuid.UUID) int {
	v, ok := a.saved[id]
	if !ok {
		return 0
	}
	delete(a.saved, id)
	a.savedOrder = slices.DeleteFunc(a.savedOrder, func(x uuid.UUID) bool { return x == id })
	return v
}

// convertFromUUIDs is AngerManagement.convertFromUuids.
func (a *AngerManagement) convertFromUUIDs(lookup func(id uuid.UUID) (Suspect, bool)) {
	for _, id := range slices.Clone(a.savedOrder) {
		s, ok := lookup(id)
		if !ok {
			continue
		}
		v := a.removeSaved(id)
		if _, known := a.anger[id]; !known {
			a.suspects = append(a.suspects, s)
		}
		a.anger[id] = v
	}
}

// sortAndUpdateHighestAnger is AngerManagement.sortAndUpdateHighestAnger.
// Java finds the highest anger inside the sort's comparator, which compares
// every suspect at least once when there are two or more: the highest anger
// of all suspects. With one suspect it is that one's anger.
func (a *AngerManagement) sortAndUpdateHighestAnger() {
	a.highestAnger = 0
	slices.SortStableFunc(a.suspects, func(x, y Suspect) int {
		ax, ay := a.anger[x.ID], a.anger[y.ID]
		angryX, angryY := angerLevelOf(ax).Angry(), angerLevelOf(ay).Angry()
		if angryX != angryY {
			if angryX {
				return -1
			}
			return 1
		}
		if x.Player != y.Player {
			if x.Player {
				return -1
			}
			return 1
		}
		switch {
		case ax > ay:
			return -1
		case ax < ay:
			return 1
		}
		return 0
	})
	for _, s := range a.suspects {
		a.highestAnger = max(a.highestAnger, a.anger[s.ID])
	}
}

// IncreaseAnger is AngerManagement.increaseAnger: anger at s goes up by
// increment, at most 150; a new suspect also gets back anger saved for it
// (which may take it past 150, as in Java). It returns the anger at s.
func (a *AngerManagement) IncreaseAnger(s Suspect, increment int) int {
	cur, known := a.anger[s.ID]
	cur = min(maxAnger, cur+increment)
	if !known {
		cur += a.removeSaved(s.ID)
		a.suspects = append(a.suspects, s)
	}
	a.anger[s.ID] = cur
	a.sortAndUpdateHighestAnger()
	return cur
}

// ClearAnger is AngerManagement.clearAnger.
func (a *AngerManagement) ClearAnger(id uuid.UUID) {
	delete(a.anger, id)
	a.suspects = slices.DeleteFunc(a.suspects, func(s Suspect) bool { return s.ID == id })
	a.sortAndUpdateHighestAnger()
}

// TopSuspect is AngerManagement.getTopSuspect: the first suspect passing
// filter (Warden.canTargetEntity), in sort order.
func (a *AngerManagement) TopSuspect(filter func(Suspect) bool) (Suspect, bool) {
	for _, s := range a.suspects {
		if filter(s) {
			return s, true
		}
	}
	return Suspect{}, false
}

// ActiveAnger is AngerManagement.getActiveAnger: the anger at the current
// target, or the highest anger without one.
func (a *AngerManagement) ActiveAnger(target *uuid.UUID) int {
	if target == nil {
		return a.highestAnger
	}
	return a.anger[*target]
}

// AngerAt returns the anger at a suspect.
func (a *AngerManagement) AngerAt(id uuid.UUID) int { return a.anger[id] }

// Saved is the "suspects" list of the warden's NBT (createUuidAngerPairs):
// the loaded suspects, then the saved ones.
func (a *AngerManagement) Saved() []SavedAnger {
	out := make([]SavedAnger, 0, len(a.suspects)+len(a.saved))
	for _, s := range a.suspects {
		out = append(out, SavedAnger{ID: s.ID, Anger: a.anger[s.ID]})
	}
	for _, id := range a.savedOrder {
		out = append(out, SavedAnger{ID: id, Anger: a.saved[id]})
	}
	return out
}

// SavedAnger is one entry of the saved suspects.
type SavedAnger struct {
	ID    uuid.UUID
	Anger int
}
