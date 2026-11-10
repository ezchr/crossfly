package javasession

import "testing"

// TestDragonPartIDs: the client numbers the dragon's eight part entities right after the
// dragon, so the next entity gets the id after them.
func TestDragonPartIDs(t *testing.T) {
	s := mobTestSession()
	dragon := s.addEntityID(newFakeMob("minecraft:ender_dragon"))
	cow := s.addEntityID(newFakeMob("minecraft:cow"))
	if cow != dragon+dragonParts+1 && cow != dragon+dragonParts+2 { // +2 when the self id was skipped
		t.Errorf("the entity after the dragon (%d) got %d, want %d", dragon, cow, dragon+dragonParts+1)
	}
}
