package jraid

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// TestPatrolSpawn checks PatrolSpawner.tick with Paper's default config: the first attempt is on the first tick,
// then every 12000 to 13199 ticks; a patrol of ceil(regional difficulty) + 1 pillagers, the first one the leader,
// spawns 24 to 47 blocks from the player on both axes.
func TestPatrolSpawn(t *testing.T) {
	l := newFakeLevel(Normal)
	l.days = 5
	l.effective = 2.3
	p := newFakePlayer(cube.Pos{1000, 64, 1000})
	l.players = append(l.players, p)
	s := NewPatrolSpawner(DefaultPatrolConfig)
	r := newRand(20)

	attempts, spawned := 0, 0
	last := -1
	for tick := 0; tick < 13200*60; tick++ {
		before := s.nextTick
		s.Tick(l, r, true)
		if s.nextTick > before {
			attempts++
			if last >= 0 {
				if gap := tick - last; gap < 12000 || gap > 13200 {
					t.Fatalf("attempt gap %d ticks", gap)
				}
			} else if tick != 0 {
				t.Fatalf("first attempt on tick %d, want 0", tick)
			}
			last = tick
		}
		if len(l.patrol) > 0 {
			spawned++
			if len(l.patrol) != 4 {
				t.Fatalf("patrol of %d, want ceil(2.3)+1 = 4", len(l.patrol))
			}
			if !l.leaders[0] || l.leaders[1] || l.leaders[2] || l.leaders[3] {
				t.Fatalf("leaders %v, want only the first", l.leaders)
			}
			first := l.patrol[0]
			dx, dz := abs(first[0]-p.pos[0]), abs(first[2]-p.pos[2])
			if dx < 24 || dx > 47 || dz < 24 || dz > 47 {
				t.Fatalf("patrol at %v, %d/%d blocks from the player", first, dx, dz)
			}
			l.patrol, l.leaders = nil, nil
		}
	}
	// 20% of the attempts spawn a patrol.
	if attempts < 58 || attempts > 67 {
		t.Errorf("%d attempts", attempts)
	}
	if spawned == 0 || spawned > attempts/2 {
		t.Errorf("%d patrols out of %d attempts", spawned, attempts)
	}
}

// TestPatrolRules checks the conditions under which no patrol spawns: before day 5, at night, near a village (two
// sub chunks), without the spawn_patrols game rule, with monsters off, for spectators, where the biome forbids it
// and with Paper's patrols disabled.
func TestPatrolRules(t *testing.T) {
	cases := map[string]func(l *fakeLevel, p *fakePlayer, c *PatrolConfig) bool{
		"day 4": func(l *fakeLevel, p *fakePlayer, c *PatrolConfig) bool { l.days = 4; return true },
		"night": func(l *fakeLevel, p *fakePlayer, c *PatrolConfig) bool { l.bright = false; return true },
		"near village": func(l *fakeLevel, p *fakePlayer, c *PatrolConfig) bool {
			l.pois = []cube.Pos{{1030, 64, 1000}}
			return true
		},
		"game rule":   func(l *fakeLevel, p *fakePlayer, c *PatrolConfig) bool { l.patrols = false; return true },
		"no monsters": func(l *fakeLevel, p *fakePlayer, c *PatrolConfig) bool { return false },
		"spectator":   func(l *fakeLevel, p *fakePlayer, c *PatrolConfig) bool { p.spectator = true; return true },
		"biome": func(l *fakeLevel, p *fakePlayer, c *PatrolConfig) bool {
			l.noPatrolsAt = func(cube.Pos) bool { return true }
			return true
		},
		"disabled": func(l *fakeLevel, p *fakePlayer, c *PatrolConfig) bool { c.Disable = true; return true },
		"chance 0": func(l *fakeLevel, p *fakePlayer, c *PatrolConfig) bool { c.SpawnChance = 0; return true },
	}
	for name, f := range cases {
		l := newFakeLevel(Normal)
		l.days = 5
		p := newFakePlayer(cube.Pos{1000, 64, 1000})
		l.players = append(l.players, p)
		c := DefaultPatrolConfig
		c.SpawnChance = 1
		monsters := f(l, p, &c)
		s := NewPatrolSpawner(c)
		r := newRand(21)
		for tick := 0; tick < 13200*5; tick++ {
			s.Tick(l, r, monsters)
		}
		if len(l.patrol) != 0 {
			t.Errorf("%s: a patrol spawned", name)
		}
	}
	// Control: with a chance of 1 every attempt spawns.
	l := newFakeLevel(Normal)
	l.days = 5
	l.players = append(l.players, newFakePlayer(cube.Pos{1000, 64, 1000}))
	c := DefaultPatrolConfig
	c.SpawnChance = 1
	s := NewPatrolSpawner(c)
	s.Tick(l, newRand(22), true)
	if len(l.patrol) == 0 {
		t.Errorf("control: no patrol")
	}
}

// TestPatrolFinalizeSpawn checks PatrollingMonster.finalizeSpawn: patrol spawns patrol, raid and structure spawns
// never lead, others lead 6% of the time and leaders wear the banner.
func TestPatrolFinalizeSpawn(t *testing.T) {
	r := newRand(23)
	leaders := 0
	for i := 0; i < 10000; i++ {
		var s State
		banner := false
		s.FinalizeSpawn(r, ReasonNatural, true, false, func() { banner = true })
		if s.PatrolLeader != banner {
			t.Fatalf("leader %v, banner %v", s.PatrolLeader, banner)
		}
		if s.PatrolLeader {
			leaders++
		}
		if s.Patrolling || !s.CanJoinRaid {
			t.Fatalf("natural pillager patrols or cannot join raids")
		}
	}
	if leaders < 500 || leaders > 700 {
		t.Errorf("%d leaders of 10000, want about 600", leaders)
	}
	for _, reason := range []SpawnReason{ReasonPatrol, ReasonEvent, ReasonStructure} {
		for i := 0; i < 1000; i++ {
			var s State
			s.FinalizeSpawn(r, reason, true, false, func() {})
			if s.PatrolLeader {
				t.Fatalf("reason %d made a leader", reason)
			}
			if s.Patrolling != (reason == ReasonPatrol) {
				t.Fatalf("reason %d: patrolling %v", reason, s.Patrolling)
			}
		}
	}
	var s State
	s.FinalizeSpawn(r, ReasonSpawnEgg, false, false, func() {})
	if s.PatrolLeader {
		t.Errorf("a ravager or witch leads")
	}
	if !s.RemoveWhenFarAway(1) || s.RemoveWhenFarAway(0) != true {
		t.Errorf("a monster outside patrols and raids does not despawn")
	}
	s.Patrolling = true
	if s.RemoveWhenFarAway(16384) || !s.RemoveWhenFarAway(16385) {
		t.Errorf("patrolling monsters despawn only beyond 128 blocks")
	}
}
