//go:build crossfly_full

package village

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/mob"
	ccore "github.com/df-mc/dragonfly/server/mob/component/core"
	vc "github.com/df-mc/dragonfly/server/mob/component/village"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// celebrate is minecraft:behavior.celebrate (BDS RaiderCelebrationGoal):
// with no target, jump every jump_interval and play celebration_sound every
// sound_interval for duration seconds.
type celebrate struct {
	mob.GoalBase
	sound         string
	duration      int
	soundI, jumpI rangeF
	onEnd         ccore.Trigger
	elapsed       int
	soundT, jumpT int
}

func newCelebrate(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Sound    *string       `json:"celebration_sound"`
		Duration *float64      `json:"duration"`
		SoundI   *rangeF       `json:"sound_interval"`
		JumpI    *rangeF       `json:"jump_interval"`
		OnEnd    ccore.Trigger `json:"on_celebration_end_event"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	g := &celebrate{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagJump)}, sound: "celebrate", duration: 600,
		soundI: rangeF{Min: 2, Max: 7}, jumpI: rangeF{Min: 1, Max: 3.5}, onEnd: j.OnEnd}
	if j.Sound != nil {
		g.sound = *j.Sound
	}
	if j.Duration != nil {
		g.duration = int(*j.Duration * 20)
	}
	if j.SoundI != nil {
		g.soundI = *j.SoundI
	}
	if j.JumpI != nil {
		g.jumpI = *j.JumpI
	}
	return g, nil
}

func (g *celebrate) CanUse(m *mob.Mob) bool {
	_, has := m.Target()
	return !has
}

func (g *celebrate) CanContinueToUse(*mob.Mob) bool { return g.elapsed < g.duration }

func (g *celebrate) Start(m *mob.Mob) {
	g.elapsed = 0
	g.soundT, g.jumpT = int(randIn(m, g.soundI)*20), int(randIn(m, g.jumpI)*20)
	vc.SetCelebrating(m, true, false)
}

func (g *celebrate) Tick(m *mob.Mob) {
	g.elapsed++
	if g.soundT--; g.soundT <= 0 {
		m.PlaySound(g.sound)
		g.soundT = int(randIn(m, g.soundI) * 20)
	}
	if g.jumpT--; g.jumpT <= 0 {
		if !m.Riding() {
			m.JumpControl().Jump()
		}
		g.jumpT = int(randIn(m, g.jumpI) * 20)
	}
}

func (g *celebrate) Stop(m *mob.Mob) {
	vc.SetCelebrating(m, false, false)
	g.onEnd.Fire(m, nil)
}

// moveToVillage is minecraft:behavior.move_to_village (BDS
// MoveToVillageGoal): outside a village (its own, or one within
// search_range), walk to a random spot in it in steps of 3/4 the follow
// range. stroll_towards_village is the variant that only goes search_range
// towards it, with start_chance.
type moveToVillage struct {
	mob.GoalBase
	speed    float64
	radiusSq float64
	cooldown int
	rng      int
	stroll   bool
	chance   float64
	counter  int
	next     int
	target   mgl64.Vec3
	reached  bool
	village  *vc.Village
}

func newMoveToVillage(stroll bool) mob.GoalFactory {
	return func(spec mob.GoalSpec) (mob.Goal, error) {
		j := struct {
			Speed    *float64 `json:"speed_multiplier"`
			Radius   *float64 `json:"goal_radius"`
			Cooldown float64  `json:"cooldown_time"`
			Range    int      `json:"search_range"`
			Chance   *float64 `json:"start_chance"`
		}{}
		if err := decode(spec, &j); err != nil {
			return nil, err
		}
		g := &moveToVillage{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}, speed: 1, radiusSq: 0.25, cooldown: int(j.Cooldown * 20),
			rng: j.Range, stroll: stroll, chance: 0.1}
		if j.Speed != nil {
			g.speed = *j.Speed
		}
		if j.Radius != nil {
			g.radiusSq = *j.Radius * *j.Radius
		}
		if j.Chance != nil {
			g.chance = *j.Chance
		}
		return g, nil
	}
}

func (g *moveToVillage) findVillage(m *mob.Mob) *vc.Village {
	if v, ok := vc.VillageOf(m); ok {
		return v
	}
	tol := 64.0
	if g.rng > 0 {
		tol = float64(g.rng)
	}
	return vc.ManagerOf(m.Tx()).ClosestVillage(m.Position(), tol)
}

func (g *moveToVillage) CanUse(m *mob.Mob) bool {
	if g.counter > 0 {
		g.counter--
		return false
	}
	if !hasNav(m) {
		return false
	}
	if g.next--; g.next > 0 {
		return false
	}
	g.next = 50
	if g.stroll && m.Rand().Float64() > g.chance {
		return false
	}
	v := g.findVillage(m)
	if v == nil || v.Within(m.Position(), 0) {
		return false
	}
	g.village = v
	return true
}

func (g *moveToVillage) randomInVillage(m *mob.Mob) mgl64.Vec3 {
	b := g.village.Bounds()
	mn, mx := b.Min(), b.Max()
	x := mn[0] + m.Rand().Float64()*(mx[0]-mn[0])
	z := mn[2] + m.Rand().Float64()*(mx[2]-mn[2])
	y := mn[1] + m.Rand().Float64()*(mx[1]-mn[1])
	return topSolid(m.Tx(), x, y, z)
}

func (g *moveToVillage) Start(m *mob.Mob) {
	g.reached = false
	g.target = g.randomInVillage(m)
	if g.stroll {
		dir := g.target.Sub(m.Position())
		dir[1] = 0
		if dir.Len() > 0 {
			p := m.Position().Add(dir.Normalize().Mul(float64(g.rng)))
			g.target = topSolid(m.Tx(), p[0], p[1], p[2])
		}
	}
	g.step(m)
}

// step is MoveToVillageGoal::_moveToBlock.
func (g *moveToVillage) step(m *mob.Mob) {
	pos := m.Position()
	d := g.target.Sub(pos)
	reach := m.Attr(mob.AttrFollowRange) * 0.75
	wp := g.target
	if d.Len() > reach {
		p := pos.Add(d.Normalize().Mul(reach))
		wp = topSolid(m.Tx(), p[0], p[1], p[2])
	}
	m.Navigation().MoveTo(m, wp, g.speed)
}

func (g *moveToVillage) Tick(m *mob.Mob) {
	n := m.Navigation()
	if n.Done() || (n.Path() != nil && n.Path().Len()-n.Path().Index() <= 2) {
		g.step(m)
	}
	if g.village != nil && g.village.Within(m.Position(), 0) {
		g.reached = true
	}
}

func (g *moveToVillage) CanContinueToUse(m *mob.Mob) bool {
	if g.village.Removed() {
		return false
	}
	if g.stroll {
		return distSq(m.Position(), g.target) >= 2*g.radiusSq+1
	}
	return distSq(m.Position(), g.target) >= 2*g.radiusSq+1 || (g.reached && !g.village.Within(m.Position(), 0))
}

func (g *moveToVillage) Stop(m *mob.Mob) {
	m.Navigation().Stop(m)
	g.counter = g.cooldown
}

// followTargetCaptain is minecraft:behavior.follow_target_captain (BDS
// FollowTargetCaptainGoal): patrol followers find an illager captain within
// (16, 12, 16) and keep follow_distance..within_radius from it.
type followTargetCaptain struct {
	mob.GoalBase
	speed              float64
	withinSq, followSq float64
	captain            *world.EntityHandle
	attempts           int
}

func newFollowTargetCaptain(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Speed  *float64 `json:"speed_multiplier"`
		Within float64  `json:"within_radius"`
		Follow float64  `json:"follow_distance"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	g := &followTargetCaptain{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}, speed: 1, withinSq: j.Within * j.Within, followSq: j.Follow * j.Follow}
	if j.Speed != nil {
		g.speed = *j.Speed
	}
	return g, nil
}

func isCaptain(m *mob.Mob) bool { return m.HasComponent("minecraft:is_illager_captain") }

func (g *followTargetCaptain) captainOf(m *mob.Mob) (*mob.Mob, bool) {
	if g.captain != nil {
		if c, ok := mob.OfHandle(m.Tx(), g.captain); ok && !c.Dead() {
			return c, true
		}
		g.captain = nil
	}
	if g.attempts >= 5 {
		return nil, false
	}
	g.attempts++
	var found *mob.Mob
	for _, e := range nearby(m, 16, 12) {
		if o, ok := mob.Of(e); ok && o.HasFamily("illager") && isCaptain(o) && !o.Dead() {
			found = o
		}
	}
	if found != nil {
		g.captain = found.H()
	}
	return found, found != nil
}

func (g *followTargetCaptain) CanUse(m *mob.Mob) bool {
	if !hasNav(m) || isCaptain(m) {
		return false
	}
	c, ok := g.captainOf(m)
	if !ok {
		return false
	}
	d := distSq(c.Position(), m.Position())
	return g.followSq <= d && d <= g.withinSq
}

func (g *followTargetCaptain) CanContinueToUse(m *mob.Mob) bool {
	c, ok := g.captainOf(m)
	if !ok {
		return false
	}
	d := distSq(c.Position(), m.Position())
	return g.followSq < d && d < g.withinSq
}

func (g *followTargetCaptain) move(m *mob.Mob) {
	c, ok := g.captainOf(m)
	if !ok {
		return
	}
	pos := m.Position()
	d := c.Position().Sub(pos)
	reach := m.Attr(mob.AttrFollowRange) * 0.75
	wp := c.Position()
	if d.Len() > reach {
		p := pos.Add(d.Normalize().Mul(reach))
		wp = topSolid(m.Tx(), p[0], p[1], p[2])
	}
	m.Navigation().MoveTo(m, wp, g.speed)
}

func (g *followTargetCaptain) Start(m *mob.Mob) { g.move(m) }

func (g *followTargetCaptain) Tick(m *mob.Mob) {
	if m.Navigation().Done() {
		g.move(m)
	}
}

func (g *followTargetCaptain) Stop(m *mob.Mob) { m.Navigation().Stop(m) }

// defendVillageTarget is minecraft:behavior.defend_village_target (BDS
// DefendVillageTargetGoal, a target goal): with attack_chance, target the
// village's closest aggressor (a player only with bad standing) that passes
// entity_types.
type defendVillageTarget struct {
	mob.GoalBase
	chance    float64
	mustReach bool
	types     filterList
	target    *world.EntityHandle
	unseen    int
}

func newDefendVillageTarget(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Chance    *float64   `json:"attack_chance"`
		MustReach bool       `json:"must_reach"`
		Types     filterList `json:"entity_types"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	g := &defendVillageTarget{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}, chance: 0.05, mustReach: j.MustReach, types: j.Types}
	if j.Chance != nil {
		g.chance = *j.Chance
	}
	return g, nil
}

func (*defendVillageTarget) IsTargetGoal() bool { return true }

func (g *defendVillageTarget) CanUse(m *mob.Mob) bool {
	v, ok := vc.VillageOf(m)
	if !ok {
		return false
	}
	a, ok := v.ClosestAggressor(m.Tx(), m.Position())
	if !ok || m.Rand().Float64() >= g.chance {
		return false
	}
	if _, isP := a.(*player.Player); isP {
		if a, ok = v.ClosestBadStandingPlayer(m.Tx(), m.Position()); !ok {
			return false
		}
	}
	if !g.types.any(m, a) || !g.canAttack(m, a) {
		return false
	}
	g.target = a.H()
	return true
}

func (g *defendVillageTarget) canAttack(m *mob.Mob, e world.Entity) bool {
	if !alive(m.Tx(), e) || e.H() == m.H() {
		return false
	}
	if p, ok := e.(*player.Player); ok && (!p.GameMode().Visible() || p.GameMode().CreativeInventory()) {
		return false
	}
	fr := m.Attr(mob.AttrFollowRange)
	if distSq(e.Position(), m.Position()) > fr*fr {
		return false
	}
	if g.mustReach && m.Navigation().CreatePathToEntity(m, e) == nil {
		return false
	}
	return true
}

func (g *defendVillageTarget) Start(m *mob.Mob) {
	if t, ok := g.target.Entity(m.Tx()); ok {
		m.SetTarget(t)
	}
	g.unseen = 0
}

func (g *defendVillageTarget) CanContinueToUse(m *mob.Mob) bool {
	t, ok := m.Target()
	if !ok || !alive(m.Tx(), t) {
		return false
	}
	fr := m.Attr(mob.AttrFollowRange)
	return distSq(t.Position(), m.Position()) <= fr*fr
}

func (g *defendVillageTarget) Stop(m *mob.Mob) {
	if t, ok := m.Target(); ok && g.target != nil && t.H() == g.target {
		m.ResetTarget()
	}
	g.target = nil
}

// moveThroughVillage is minecraft:behavior.move_through_village. In BDS
// 1.21.3 its canUse is a folded "return false" stub, so iron golems never
// run it; this does the same.
type moveThroughVillage struct{ mob.GoalBase }

func newMoveThroughVillage(spec mob.GoalSpec) (mob.Goal, error) {
	return &moveThroughVillage{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}}, nil
}

func (*moveThroughVillage) CanUse(*mob.Mob) bool { return false }

var _ = block.Air{}
