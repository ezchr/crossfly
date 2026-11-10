package jraid

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// startRaid gives the player in the village bad omen of the amplifier and ticks the omens until the raid starts,
// as the Java mob mode's effect hook does: bad omen turns into raid omen, which triggers the raid on its last tick.
func startRaid(t *testing.T, l *fakeLevel, rs *Raids, p *fakePlayer, amplifier int) *Raid {
	t.Helper()
	if rs.BadOmenTick(l, p, amplifier) {
		t.Fatalf("bad omen stayed in the village")
	}
	if amp, ok := p.RaidOmen(); !ok || amp != amplifier {
		t.Fatalf("raid omen %d %v, want %d", amp, ok, amplifier)
	}
	for remaining := RaidOmenTicks; remaining > 1; remaining-- {
		if !rs.RaidOmenTick(l, p, remaining) {
			t.Fatalf("raid omen ended at %d ticks left", remaining)
		}
	}
	if rs.RaidOmenTick(l, p, 1) {
		t.Fatalf("raid omen stayed on its last tick")
	}
	p.RemoveRaidOmen()
	raid := rs.RaidAt(p.pos)
	if raid == nil {
		t.Fatalf("no raid after raid omen")
	}
	return raid
}

func tickN(l *fakeLevel, rs *Raids, n int) {
	for i := 0; i < n; i++ {
		l.time++
		rs.Tick(l)
	}
}

// TestOmenToRaid checks bad omen -> raid omen -> raid: the raid is centred on the average of the occupied points of
// interest within 64 blocks, its omen level is the amplifier + 1, its id is 2 (Raids.getUniqueId pre-increments
// next_id) and players in it see its bar.
func TestOmenToRaid(t *testing.T) {
	l := newFakeLevel(Normal)
	l.pois = village()
	rs := NewRaids(newRand(3))
	p := newFakePlayer(cube.Pos{2, 64, 2})
	l.players = append(l.players, p)

	raid := startRaid(t, l, rs, p, 1)
	if raid.ID() != 2 {
		t.Errorf("raid id %d, want 2", raid.ID())
	}
	if raid.RaidOmenLevel() != 2 {
		t.Errorf("omen level %d, want 2", raid.RaidOmenLevel())
	}
	// Average of the pois: (8/4, 257/4, 6/4) floored.
	if want := (cube.Pos{2, 64, 1}); raid.Center() != want {
		t.Errorf("centre %v, want %v", raid.Center(), want)
	}
	if raid.IsStarted() || raid.GroupsSpawned() != 0 {
		t.Fatalf("raid started before its countdown")
	}
	tickN(l, rs, 1)
	if _, ok := p.bars[raid.event.id]; !ok {
		t.Errorf("player in the raid does not see its bar")
	}
}

// TestOmenOutsideVillage checks that bad omen stays outside villages, on peaceful, for spectators and in a village
// whose raid has the highest omen level.
func TestOmenOutsideVillage(t *testing.T) {
	l := newFakeLevel(Normal)
	l.pois = village()
	rs := NewRaids(newRand(4))
	far := newFakePlayer(cube.Pos{200, 64, 200})
	if !rs.BadOmenTick(l, far, 0) {
		t.Errorf("bad omen consumed outside a village")
	}
	spec := newFakePlayer(cube.Pos{0, 64, 0})
	spec.spectator = true
	if !rs.BadOmenTick(l, spec, 0) {
		t.Errorf("bad omen consumed by a spectator")
	}
	l.difficulty = Peaceful
	in := newFakePlayer(cube.Pos{0, 64, 0})
	if !rs.BadOmenTick(l, in, 0) {
		t.Errorf("bad omen consumed on peaceful")
	}
	l.difficulty = Normal
	l.players = append(l.players, in)
	raid := startRaid(t, l, rs, in, 4)
	if raid.RaidOmenLevel() != 5 {
		t.Fatalf("omen level %d, want 5", raid.RaidOmenLevel())
	}
	if !rs.BadOmenTick(l, in, 0) {
		t.Errorf("bad omen consumed in a raid at the highest omen level")
	}
}

// TestRaidExtends checks that more raid omen raises a running raid's omen level, capped at 5.
func TestRaidExtends(t *testing.T) {
	l := newFakeLevel(Normal)
	l.pois = village()
	rs := NewRaids(newRand(5))
	p := newFakePlayer(cube.Pos{0, 64, 0})
	l.players = append(l.players, p)
	raid := startRaid(t, l, rs, p, 0)
	if again := startRaid(t, l, rs, p, 2); again != raid {
		t.Fatalf("a second raid started in the same village")
	}
	if raid.RaidOmenLevel() != 4 {
		t.Errorf("omen level %d, want 4", raid.RaidOmenLevel())
	}
	startRaid(t, l, rs, p, 3)
	if raid.RaidOmenLevel() != 5 {
		t.Errorf("omen level %d, want 5 (capped)", raid.RaidOmenLevel())
	}
	if len(rs.All()) != 1 {
		t.Errorf("%d raids, want 1", len(rs.All()))
	}
}

// runRaid plays a raid to its end: each wave spawns after its 300 tick countdown and is killed by the player.
// It returns the wave sizes.
func runRaid(t *testing.T, l *fakeLevel, rs *Raids, raid *Raid, p *fakePlayer) []int {
	t.Helper()
	var waves []int
	for i := 0; i < 20 && raid.IsInProgress(); i++ {
		before := raid.GroupsSpawned()
		ticks := 0
		for raid.GroupsSpawned() == before && raid.IsInProgress() && ticks < 400 {
			tickN(l, rs, 1)
			ticks++
		}
		if raid.GroupsSpawned() == before {
			break
		}
		if ticks < 300 || ticks > 302 {
			t.Errorf("wave %d spawned after %d ticks, want 300", raid.GroupsSpawned(), ticks)
		}
		waves = append(waves, raid.TotalRaidersAlive())
		id := p.id
		l.killAll(&id)
		tickN(l, rs, 1)
	}
	tickN(l, rs, postRaidTickLimit+1)
	return waves
}

// TestRaidWavesAndVictory checks a whole raid per difficulty: the number of waves (3/5/7, plus the bonus wave with
// an omen level above 1), the captain with the ominous banner, the horn, then victory after 40 ticks and the hero
// of the village effect (amplifier omen level - 1, 40 minutes) for the player who killed raiders.
func TestRaidWavesAndVictory(t *testing.T) {
	for _, c := range []struct {
		d     Difficulty
		amp   int
		waves int
	}{
		{Easy, 0, 3}, {Normal, 0, 5}, {Hard, 0, 7},
		{Easy, 1, 4}, {Normal, 2, 6}, {Hard, 4, 8},
	} {
		l := newFakeLevel(c.d)
		l.pois = village()
		rs := NewRaids(newRand(uint64(6 + c.amp)))
		p := newFakePlayer(cube.Pos{0, 64, 0})
		l.players = append(l.players, p)
		raid := startRaid(t, l, rs, p, c.amp)
		waves := runRaid(t, l, rs, raid, p)
		if len(waves) != c.waves {
			t.Errorf("%v omen %d: %d waves, want %d", c.d, c.amp+1, len(waves), c.waves)
		}
		if !raid.IsVictory() {
			t.Errorf("%v: status %v, want victory", c.d, raid.Status())
		}
		if p.hero != c.amp || p.heroTicks != HeroOfTheVillageDuration {
			t.Errorf("%v: hero %d for %d ticks, want %d for %d", c.d, p.hero, p.heroTicks, c.amp, HeroOfTheVillageDuration)
		}
		if p.horns != len(waves) {
			t.Errorf("%v: %d horns, want one per wave (%d)", c.d, p.horns, len(waves))
		}
		captains := 0
		for _, r := range l.raiders {
			if r.banner {
				captains++
				if !r.st.PatrolLeader || !r.kind.CanBeLeader() {
					t.Errorf("%v: captain %v without leader flag", c.d, r.kind)
				}
			}
		}
		if captains != len(waves) {
			t.Errorf("%v: %d captains, want one per wave", c.d, captains)
		}
		// Victory bar, then the raid stops after 600 celebration ticks.
		tickN(l, rs, 20)
		if b := p.bars[raid.event.id]; b.Text != BarVictory {
			t.Errorf("%v: bar %v, want victory", c.d, b.Text)
		}
		tickN(l, rs, MaxCelebrationTicks)
		if !raid.IsStopped() || len(rs.All()) != 0 {
			t.Errorf("%v: raid not stopped and removed after celebrating", c.d)
		}
		if len(p.bars) != 0 {
			t.Errorf("%v: bar still shown after the raid", c.d)
		}
	}
}

// TestRaidLoss checks that a raid whose village is gone after its first wave is lost (the raiders celebrate,
// Raider.RaiderCelebration) and one whose village is gone before stops.
func TestRaidLoss(t *testing.T) {
	l := newFakeLevel(Normal)
	l.pois = village()
	rs := NewRaids(newRand(9))
	p := newFakePlayer(cube.Pos{0, 64, 0})
	l.players = append(l.players, p)
	raid := startRaid(t, l, rs, p, 0)
	tickN(l, rs, 301)
	if raid.GroupsSpawned() != 1 {
		t.Fatalf("first wave not spawned")
	}
	l.pois = nil
	tickN(l, rs, 1)
	if !raid.IsLoss() {
		t.Fatalf("status %v, want loss", raid.Status())
	}
	for _, r := range l.alive() {
		if r.st.Raid() != raid || !r.st.Raid().IsLoss() {
			t.Fatalf("raider not in the lost raid")
		}
	}
	tickN(l, rs, 20)
	if b := p.bars[raid.event.id]; b.Text != BarDefeat {
		t.Errorf("bar %v, want defeat", b.Text)
	}

	l2 := newFakeLevel(Normal)
	l2.pois = village()
	rs2 := NewRaids(newRand(10))
	p2 := newFakePlayer(cube.Pos{0, 64, 0})
	l2.players = append(l2.players, p2)
	raid2 := startRaid(t, l2, rs2, p2, 0)
	l2.pois = nil
	tickN(l2, rs2, 1)
	if !raid2.IsStopped() {
		t.Errorf("raid without a village before its first wave: %v, want stopped", raid2.Status())
	}
}

// TestRaidStops checks the stop reasons: peaceful, the raids game rule and the 48000 tick timeout.
func TestRaidStops(t *testing.T) {
	setup := func(seed uint64) (*fakeLevel, *Raids, *Raid) {
		l := newFakeLevel(Normal)
		l.pois = village()
		rs := NewRaids(newRand(seed))
		p := newFakePlayer(cube.Pos{0, 64, 0})
		l.players = append(l.players, p)
		return l, rs, startRaid(t, l, rs, p, 0)
	}
	l, rs, raid := setup(11)
	l.difficulty = Peaceful
	tickN(l, rs, 1)
	if !raid.IsStopped() {
		t.Errorf("peaceful: %v, want stopped", raid.Status())
	}
	l, rs, raid = setup(12)
	l.raids = false
	tickN(l, rs, 1)
	if !raid.IsStopped() || len(rs.All()) != 0 {
		t.Errorf("raids off: %v, want stopped and removed", raid.Status())
	}
	l, rs, raid = setup(13)
	tickN(l, rs, 301)
	// Raiders that never die: the raid times out.
	tickN(l, rs, raidTimeoutTicks)
	if !raid.IsStopped() {
		t.Errorf("timeout: %v, want stopped", raid.Status())
	}
}

// TestRaidUnspawnable checks that a raid with no place to spawn its wave stops (more than 5 failed attempts).
func TestRaidUnspawnable(t *testing.T) {
	l := newFakeLevel(Normal)
	l.pois = village()
	l.spawnFails = true
	rs := NewRaids(newRand(14))
	p := newFakePlayer(cube.Pos{0, 64, 0})
	l.players = append(l.players, p)
	raid := startRaid(t, l, rs, p, 0)
	tickN(l, rs, 300)
	if !raid.IsStopped() {
		t.Errorf("%v, want stopped", raid.Status())
	}
}

// TestRaiderJoinsNearbyRaid checks Raider.aiStep: a raider that can join raids joins the raid it is in on a
// multiple of 20 game ticks, into the current wave; one idle too long does not.
func TestRaiderJoinsNearbyRaid(t *testing.T) {
	l := newFakeLevel(Normal)
	l.pois = village()
	rs := NewRaids(newRand(15))
	p := newFakePlayer(cube.Pos{0, 64, 0})
	l.players = append(l.players, p)
	raid := startRaid(t, l, rs, p, 0)
	tickN(l, rs, 301)

	r := &fakeRaider{kind: Pillager, id: [16]byte{1}, health: 24, alive: true, pos: cube.Pos{5, 64, 5}}
	r.st.CanJoinRaid = true
	l.time = 39
	AIStep(l, rs, r, false)
	if r.st.Raid() != nil {
		t.Fatalf("joined off a 20 tick boundary")
	}
	l.time = 40
	AIStep(l, rs, r, false)
	if r.st.Raid() != raid || r.st.Wave != 1 {
		t.Fatalf("raid %v wave %d, want the raid's wave 1", r.st.Raid(), r.st.Wave)
	}
	idle := &fakeRaider{kind: Pillager, id: [16]byte{2}, health: 24, alive: true, pos: cube.Pos{5, 64, 5}, idle: MaxNoActionTime + 1}
	idle.st.CanJoinRaid = true
	AIStep(l, rs, idle, false)
	if idle.st.Raid() != nil {
		t.Errorf("idle raider joined")
	}
	witch := &fakeRaider{kind: Witch, id: [16]byte{3}, health: 26, alive: true, pos: cube.Pos{5, 64, 5}}
	witch.st.FinalizeSpawn(newRand(1), ReasonNatural, false, true, func() {})
	AIStep(l, rs, witch, false)
	if witch.st.Raid() != nil {
		t.Errorf("a swamp hut witch joined the raid")
	}
}

// TestCaptainDeath checks Raider.die: the wave loses its leader, a player killer becomes a hero and the raider
// leaves the raid; another raider of the wave may then pick up the banner.
func TestCaptainDeath(t *testing.T) {
	l := newFakeLevel(Normal)
	l.pois = village()
	rs := NewRaids(newRand(16))
	p := newFakePlayer(cube.Pos{0, 64, 0})
	l.players = append(l.players, p)
	raid := startRaid(t, l, rs, p, 0)
	tickN(l, rs, 301)
	leader, ok := raid.Leader(1)
	if !ok {
		t.Fatalf("wave 1 has no captain")
	}
	lr := leader.(*fakeRaider)
	lr.alive, lr.removed = false, true
	id := p.id
	Die(lr, &id)
	if _, ok := raid.Leader(1); ok {
		t.Errorf("dead captain still leads")
	}
	if len(raid.Heroes()) != 1 || raid.Heroes()[0] != p.id {
		t.Errorf("heroes %v, want the player", raid.Heroes())
	}
	var other *fakeRaider
	for _, r := range l.alive() {
		if r.kind.CanBeLeader() {
			other = r
			break
		}
	}
	if other == nil {
		t.Fatalf("no raider left to pick up the banner")
	}
	if !CanPickUpBanner(l, other, false) {
		t.Fatalf("raider may not pick up the banner")
	}
	if !PickUpBanner(other) || !other.banner || !other.st.PatrolLeader {
		t.Fatalf("banner not picked up")
	}
	if got, _ := raid.Leader(1); got != Raider(other) {
		t.Errorf("new captain not the wave's leader")
	}
	l.griefing = false
	if CanPickUpBanner(l, other, false) {
		t.Errorf("banner pick up without mob griefing")
	}
}
