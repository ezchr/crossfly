package javasession

import (
	"encoding/hex"
	"testing"

	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// setFlags sets the extra actor flags of b to exactly flags.
func (b *bossFake) setFlags(flags ...int) *bossFake {
	b.flags = [3]uint64{}
	for _, f := range flags {
		b.flags[f/64] |= 1 << (f % 64)
	}
	return b
}

// viewData runs viewMobData for e as entity 7 and returns the set_entity_data body sent ("" for
// none) and the entity events sent.
func viewData(t *testing.T, s *Session, e world.Entity) (string, []string) {
	t.Helper()
	s.viewMobData(e, 7)
	var data string
	var events []string
	for _, p := range s.take() {
		switch p.id {
		case v777.ClientboundPlaySetEntityData:
			data = hex.EncodeToString(p.w.B)
		case v777.ClientboundPlayEntityEvent:
			events = append(events, hex.EncodeToString(p.w.B))
		default:
			t.Errorf("unexpected packet %v", p.id)
		}
	}
	return data, events
}

// TestSnifferStates: the sniffer goal flags become Sniffer.State (18, SNIFFER_STATE 35).
func TestSnifferStates(t *testing.T) {
	s := mobTestSession()
	snifferData := func(state int32) string { return mob(10, 0).z(16, false).raw(18, 35, state).end() }
	tests := []struct {
		name  string
		flags []int
		want  int32
	}{
		{"idle", nil, snifferIdling},
		{"long sniff", []int{protocol.EntityDataFlagTimerFlag1}, snifferSniffing},
		{"walking to the dig", []int{protocol.EntityDataFlagSearching}, snifferSearching},
		{"digging", []int{protocol.EntityDataFlagDigging}, snifferDigging},
		{"digging, seed found", []int{protocol.EntityDataFlagDigging, protocol.EntityDataFlagTimerFlag3}, snifferDigging},
		{"standing up, happy", []int{protocol.EntityDataFlagTimerFlag2, protocol.EntityDataFlagTimerFlag3}, snifferRising},
		{"happy", []int{protocol.EntityDataFlagTimerFlag3}, snifferHappy},
	}
	for _, tc := range tests {
		sn := (&bossFake{fakeMob: newFakeMob("minecraft:sniffer")}).setFlags(tc.flags...)
		s.mobData = nil
		if got, _ := viewData(t, s, sn); got != snifferData(tc.want) {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, got, snifferData(tc.want))
		}
	}
	// A sniffer without goal flags (not a definition mob) keeps the client default.
	s.mobData = nil
	if got, _ := viewData(t, s, newFakeMob("minecraft:sniffer")); got != mob(10, 0).z(16, false).end() {
		t.Errorf("plain sniffer: %s", got)
	}
	// A digging sniffer has no Java pose (DIGGING is the warden's burrowing).
	if p, ok := mobFlagPose((&bossFake{fakeMob: newFakeMob("minecraft:sniffer")}).setFlags(protocol.EntityDataFlagDigging)); ok {
		t.Errorf("digging sniffer pose %d", p)
	}
}

// TestArmadilloStates walks an armadillo through a roll-up, peeks, relaxing, a new threat while
// unrolling and the unroll: ArmadilloState (18, ARMADILLO_STATE 36) and the peek entity event 64.
func TestArmadilloStates(t *testing.T) {
	s := mobTestSession()
	a := &bossFake{fakeMob: newFakeMob("minecraft:armadillo")}
	s.entityIDs[a.H()] = 7
	data := func(state int32) string { return mob(10, 0).z(16, false).raw(18, 36, state).end() }
	peek := "00000007" + "40"
	steps := []struct {
		name  string
		state string
		flags []int
		data  string // "" when nothing changes for the client
		peek  bool
	}{
		{"walking", "unrolled", nil, data(armadilloIdle), false},
		{"rolls up", "rolled_up", nil, data(armadilloRolling), false},
		{"still rolling", "rolled_up", nil, "", false},
		{"rolled (timer_flag_1)", "rolled_up", []int{protocol.EntityDataFlagTimerFlag1}, data(armadilloScared), false},
		{"peeks", "rolled_up_peeking", []int{protocol.EntityDataFlagTimerFlag1}, "", true},
		{"peeking, timer off", "rolled_up_peeking", nil, "", false},
		{"back in the ball", "rolled_up", nil, "", false},
		{"rolled again", "rolled_up", []int{protocol.EntityDataFlagTimerFlag1}, "", false},
		{"peeks again", "rolled_up_peeking", nil, "", true},
		{"no threat", "rolled_up_relaxing", nil, "", false},
		{"unrolls", "rolled_up_unrolling", nil, data(armadilloUnrolling), false},
		{"threat while unrolling", "rolled_up", nil, data(armadilloScared), false},
		{"relaxing", "rolled_up_relaxing", nil, "", false},
		{"unrolls again", "rolled_up_unrolling", nil, data(armadilloUnrolling), false},
		{"unrolled", "unrolled", nil, data(armadilloIdle), false},
	}
	for _, st := range steps {
		a.prop("minecraft:armadillo_state", st.state)
		a.setFlags(st.flags...)
		got, events := viewData(t, s, a)
		if got != st.data {
			t.Errorf("%s: data\n got %s\nwant %s", st.name, got, st.data)
		}
		if st.peek && (len(events) != 1 || events[0] != peek) || !st.peek && len(events) != 0 {
			t.Errorf("%s: events %v, peek %v", st.name, events, st.peek)
		}
	}

	// A client that first sees a rolled armadillo gets the closed ball, not the roll-up.
	s2 := mobTestSession()
	s2.entityIDs[a.H()] = 7
	a.prop("minecraft:armadillo_state", "rolled_up")
	a.setFlags()
	if got, _ := viewData(t, s2, a); got != data(armadilloScared) {
		t.Errorf("first view of a rolled armadillo:\n got %s\nwant %s", got, data(armadilloScared))
	}
	// Without the property (not a definition mob) the client keeps its default.
	s2.mobData = nil
	if got, _ := viewData(t, s2, newFakeMob("minecraft:armadillo")); got != mob(10, 0).z(16, false).end() {
		t.Errorf("plain armadillo: %s", got)
	}
}

// TestCreakingStates: minecraft:creaking_state becomes CAN_MOVE (16), IS_ACTIVE (17) and
// IS_TEARING_DOWN (18).
func TestCreakingStates(t *testing.T) {
	s := mobTestSession()
	tests := []struct {
		state                 string
		move, active, tearing bool
	}{
		{"neutral", true, false, false},
		{"hostile_observed", false, true, false},
		{"hostile_unobserved", true, true, false},
		{"twitching", false, false, true},
		{"crumbling", false, false, true},
	}
	for _, tc := range tests {
		c := newFakeMob("minecraft:creaking").prop("minecraft:creaking_state", tc.state)
		s.mobData = nil
		want := mob(10, 0).z(16, tc.move).z(17, tc.active).z(18, tc.tearing).end()
		if got, _ := viewData(t, s, c); got != want {
			t.Errorf("%s:\n got %s\nwant %s", tc.state, got, want)
		}
	}
}

// TestHappyGhastStaysStill: the Bedrock collidable flag (adult_immobile) is HappyGhast.STAYS_STILL.
func TestHappyGhastStaysStill(t *testing.T) {
	s := mobTestSession()
	for _, still := range []bool{false, true} {
		g := &bossFake{fakeMob: newFakeMob("minecraft:happy_ghast")}
		if still {
			g.setFlags(protocol.EntityDataFlagCollidable)
		}
		s.mobData = nil
		want := mob(10, 0).z(16, false).z(19, still).end()
		if got, _ := viewData(t, s, g); got != want {
			t.Errorf("still %v:\n got %s\nwant %s", still, got, want)
		}
	}
}
