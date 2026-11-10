package jraid

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

// TestFangLayout checks EvokerAttackSpellGoal: 13 fangs in two rings near the target, 16 in a line otherwise.
func TestFangLayout(t *testing.T) {
	near := FangLayout(0, 0, 2, 0, 4)
	if len(near) != 13 {
		t.Fatalf("%d fangs near, want 13", len(near))
	}
	for i, f := range near {
		r := math.Hypot(f.X, f.Z)
		want, delay := 1.5, 0
		if i >= 5 {
			want, delay = 2.5, 3
		}
		if math.Abs(r-want) > 1e-5 || f.Delay != delay {
			t.Errorf("fang %d at radius %v delay %d, want %v delay %d", i, r, f.Delay, want, delay)
		}
	}
	far := FangLayout(0, 0, 0, 10, 100)
	if len(far) != 16 {
		t.Fatalf("%d fangs far, want 16", len(far))
	}
	for i, f := range far {
		if math.Abs(f.X) > 1e-5 || math.Abs(f.Z-1.25*float64(i+1)) > 1e-5 || f.Delay != i {
			t.Errorf("fang %d at %v,%v delay %d", i, f.X, f.Z, f.Delay)
		}
	}
}

type ground struct {
	floor int
	slab  bool
}

func (g ground) TopFaceSturdy(pos cube.Pos) bool { return pos[1] == g.floor }
func (g ground) CollisionTop(pos cube.Pos) float64 {
	if g.slab && pos[1] == g.floor+1 {
		return 0.5
	}
	return 0
}

// TestFangY checks the ground search of createSpellEntity.
func TestFangY(t *testing.T) {
	if y, ok := FangY(ground{floor: 63}, 0, 0, 64, 65); !ok || y != 64 {
		t.Errorf("flat: %v %v, want 64", y, ok)
	}
	if y, ok := FangY(ground{floor: 63, slab: true}, 0, 0, 64, 65); !ok || y != 64.5 {
		t.Errorf("slab: %v %v, want 64.5", y, ok)
	}
	if _, ok := FangY(ground{floor: 50}, 0, 0, 64, 65); ok {
		t.Errorf("fang placed over a 14 block drop")
	}
}

// TestFangTiming checks EvokerFangs.tick: the spike event right after the warm-up, the bite 8 ticks later, gone 22
// ticks after the warm-up.
func TestFangTiming(t *testing.T) {
	f := NewFangState(3)
	spikeAt, biteAt, doneAt := -1, -1, -1
	for tick := 1; tick < 100 && doneAt < 0; tick++ {
		spike, bite, done := f.Tick()
		if spike {
			spikeAt = tick
		}
		if bite {
			biteAt = tick
		}
		if done {
			doneAt = tick
		}
	}
	if spikeAt != 4 || biteAt != 11 || doneAt != 26 {
		t.Errorf("spike %d bite %d done %d, want 4 11 26", spikeAt, biteAt, doneAt)
	}
}

// TestSpellTiming checks a fangs spell goal ticking every other tick: cast 20 game ticks after it starts, arms up
// for 40 ticks, startable again 100 ticks later.
func TestSpellTiming(t *testing.T) {
	var c Spellcaster
	g := UseSpell{Spell: SpellFangs}
	if !g.CanUse(&c, true, 0) {
		t.Fatalf("cannot start")
	}
	g.Start(&c, 0, func(n int) int { return (n + 1) / 2 })
	if c.Current != SpellFangs || c.ArmPose(false) != PoseSpellcasting {
		t.Fatalf("not casting")
	}
	cast := -1
	for tick := 1; tick <= 40; tick++ {
		c.ServerAIStep()
		if tick%2 == 0 && g.Tick() {
			cast = tick
		}
	}
	if cast != 20 {
		t.Errorf("cast on tick %d, want 20", cast)
	}
	if c.IsCasting() {
		t.Errorf("still casting after 40 ticks")
	}
	if g.CanUse(&c, true, 99) || !g.CanUse(&c, true, 100) {
		t.Errorf("interval not 100 ticks")
	}
}

// TestWitchPotions checks Witch.performRangedAttack's choice.
func TestWitchPotions(t *testing.T) {
	r := newRand(30)
	cases := []struct {
		t    WitchTarget
		dist float64
		want Potion
	}{
		{WitchTarget{Raider: true, Health: 4}, 5, PotionHealing},
		{WitchTarget{Raider: true, Health: 10}, 5, PotionRegeneration},
		{WitchTarget{Health: 20}, 9, PotionSlowness},
		{WitchTarget{Health: 20, Slowness: true}, 9, PotionPoison},
		{WitchTarget{Health: 7, Slowness: true}, 9, PotionHarming},
		{WitchTarget{Health: 20, Poison: true}, 5, PotionHarming},
	}
	for _, c := range cases {
		if got, _ := WitchThrow(r, c.t, c.dist); got != c.want {
			t.Errorf("%+v at %v: %v, want %v", c.t, c.dist, got, c.want)
		}
	}
	weak := 0
	for i := 0; i < 4000; i++ {
		if p, _ := WitchThrow(r, WitchTarget{Health: 6}, 2); p == PotionWeakness {
			weak++
		}
	}
	if weak < 850 || weak > 1150 {
		t.Errorf("weakness %d of 4000 within 3 blocks, want about 1000", weak)
	}
	drinks := map[Potion]int{}
	for i := 0; i < 10000; i++ {
		drinks[WitchDrink(r, WitchState{Health: 10, MaxHealth: 26})]++
	}
	if drinks[PotionHealing] < 400 || drinks[PotionHealing] > 600 || drinks[PotionWaterBreathing] != 0 {
		t.Errorf("hurt witch drinks %v, want healing about 5%%", drinks)
	}
}

// TestRavagerTimers checks the stun -> roar sequence and the attack timer.
func TestRavagerTimers(t *testing.T) {
	var rv RavagerState
	rv.StunnedTick = RavagerStunDuration
	roarAt, stunEndAt := -1, -1
	for tick := 1; tick <= 80; tick++ {
		roar, end := rv.Tick()
		if roar {
			roarAt = tick
		}
		if end {
			stunEndAt = tick
		}
	}
	if stunEndAt != 40 || roarAt != 50 {
		t.Errorf("stun ended %d roar %d, want 40 50", stunEndAt, roarAt)
	}
	rv.Attack()
	if !rv.Immobile() {
		t.Errorf("attacking ravager moves")
	}
	if x, y, z := StrongKnockback(1, 0); x != 4 || y != 0.2 || z != 0 {
		t.Errorf("knockback %v %v %v", x, y, z)
	}
}

type crossbowMob struct {
	using        bool
	useTicks     int
	moves, shots int
	charging     bool
}

func (m *crossbowMob) MoveTo(float64)             { m.moves++ }
func (m *crossbowMob) StopNavigation()            {}
func (m *crossbowMob) LookAtTarget()              {}
func (m *crossbowMob) StartUsingCrossbow()        { m.using, m.useTicks = true, 0 }
func (m *crossbowMob) IsUsingItem() bool          { return m.using }
func (m *crossbowMob) TicksUsingItem() int        { return m.useTicks }
func (m *crossbowMob) ChargeDuration() int        { return 25 }
func (m *crossbowMob) ReleaseUsingItem()          { m.using = false }
func (m *crossbowMob) SetChargingCrossbow(b bool) { m.charging = b }
func (m *crossbowMob) PerformRangedAttack()       { m.shots++ }

// TestCrossbowCycle checks RangedCrossbowAttackGoal: in range with sight, a shot takes the charge (25 ticks) plus
// 20 to 39 ticks of aiming plus the 5 ticks of sight needed first.
func TestCrossbowCycle(t *testing.T) {
	r := newRand(31)
	g := NewCrossbowGoal(8)
	m := &crossbowMob{}
	first := -1
	for tick := 1; tick <= 400; tick++ {
		if m.using {
			m.useTicks++
		}
		g.Tick(r, m, true, 25)
		if m.shots == 1 && first < 0 {
			first = tick
		}
	}
	if first < 5+25+20 || first > 5+25+40+2 {
		t.Errorf("first shot on tick %d", first)
	}
	if m.shots < 5 || m.shots > 8 {
		t.Errorf("%d shots in 20 seconds", m.shots)
	}
	far := NewCrossbowGoal(8)
	fm := &crossbowMob{}
	for tick := 0; tick < 100; tick++ {
		far.Tick(r, fm, true, 100)
	}
	if fm.shots != 0 || fm.moves == 0 || fm.using {
		t.Errorf("out of range: %d shots, %d moves", fm.shots, fm.moves)
	}
}

// TestBellResonance checks BellBlockEntity: villagers within 32 blocks hear the bell, it resonates 5 ticks after
// ringing when a raider is within 32 blocks, and raiders within 48 blocks glow for 3 seconds 40 ticks after resonating.
func TestBellResonance(t *testing.T) {
	l := &bellLevel{}
	near := &bellEntity{pos: mgl64.Vec3{10.5, 64, 0.5}, raider: true}
	mid := &bellEntity{pos: mgl64.Vec3{40.5, 64, 0.5}, raider: true}
	villager := &bellEntity{pos: mgl64.Vec3{20.5, 64, 0.5}}
	l.entities = []BellEntity{near, mid, villager}
	b := &Bell{Pos: cube.Pos{0, 64, 0}}
	l.time = 100
	b.OnHit(l, 2)
	if villager.heard != 100 || near.heard != 100 || mid.heard != 0 {
		t.Errorf("heard: villager %d near %d mid %d", villager.heard, near.heard, mid.heard)
	}
	resonateAt, glowAt := -1, -1
	for tick := 1; tick <= 80; tick++ {
		b.Tick(l)
		if l.resonated > 0 && resonateAt < 0 {
			resonateAt = tick
		}
		if near.glow > 0 && glowAt < 0 {
			glowAt = tick
		}
	}
	if resonateAt != 5 || glowAt != 45 {
		t.Errorf("resonated on tick %d, glowed on %d, want 5 and 45", resonateAt, glowAt)
	}
	if mid.glow != BellGlowTicks || villager.glow != 0 {
		t.Errorf("mid glow %d villager glow %d", mid.glow, villager.glow)
	}
	// No raider within 32 blocks: no resonance.
	l2 := &bellLevel{entities: []BellEntity{&bellEntity{pos: mgl64.Vec3{40.5, 64, 0.5}, raider: true}}}
	b2 := &Bell{Pos: cube.Pos{0, 64, 0}}
	b2.OnHit(l2, 2)
	for tick := 0; tick < 80; tick++ {
		b2.Tick(l2)
	}
	if l2.resonated != 0 {
		t.Errorf("resonated with the raider 40 blocks away")
	}
}

type bellLevel struct {
	time      int64
	entities  []BellEntity
	resonated int
}

func (l *bellLevel) GameTime() int64 { return l.time }
func (l *bellLevel) LivingEntitiesWithin(box cube.BBox) []BellEntity {
	var out []BellEntity
	for _, e := range l.entities {
		if box.Vec3Within(e.Position()) {
			out = append(out, e)
		}
	}
	return out
}
func (l *bellLevel) PlayResonate(cube.Pos) { l.resonated++ }

type bellEntity struct {
	pos    mgl64.Vec3
	raider bool
	heard  int64
	glow   int
}

func (e *bellEntity) Alive() bool          { return true }
func (e *bellEntity) Removed() bool        { return false }
func (e *bellEntity) Position() mgl64.Vec3 { return e.pos }
func (e *bellEntity) IsRaider() bool       { return e.raider }
func (e *bellEntity) HearBell(t int64)     { e.heard = t }
func (e *bellEntity) Glow(ticks int)       { e.glow = ticks }
