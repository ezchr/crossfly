//go:build crossfly_full

package locomotion

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Flocking is minecraft:flocking (BDS FlockingComponent, FlockingSystem): fish
// and dolphins join flocks of their own type (same variants with
// match_variants) within influence_radius; a leader keeps the member list,
// merges nearby flocks and works out every member's steering vector from
// separation, cohesion (towards the centre of mass or the leader), the
// leader's path (goal), alignment with the leader's velocity, block
// avoidance and, in water, a pull down when near the surface. Loners
// (loner_chance) never flock. The steering is followed by the hard-coded
// minecraft:follow_flock goal BDS gives cod (speed 0.09), salmon (0.12),
// tropical fish (0.05) and dolphins (0.1).
type Flocking struct {
	cfg flockConfig

	members                     []*world.EntityHandle
	centre, vel, heading, steer mgl64.Vec3
	leader, inFlock, canFlock   bool
	hasGoal, goalHeld, tooFar   bool
	limit                       int
	prevPos                     mgl64.Vec3
	hasPrev                     bool
}

type flockConfig struct {
	InWater          bool    `json:"in_water"`
	MatchVariants    bool    `json:"match_variants"`
	UseCentreOfMass  bool    `json:"use_center_of_mass"`
	LowLimit         int     `json:"low_flock_limit"`
	HighLimit        int     `json:"high_flock_limit"`
	GoalWeight       float64 `json:"goal_weight"`
	LonerChance      float64 `json:"loner_chance"`
	InfluenceRadius  float64 `json:"influence_radius"`
	BreachInfluence  float64 `json:"breach_influence"`
	SeparationWeight float64 `json:"separation_weight"`
	SeparationThresh float64 `json:"separation_threshold"`
	CohesionWeight   float64 `json:"cohesion_weight"`
	CohesionThresh   float64 `json:"cohesion_threshold"`
	InnerCohesion    float64 `json:"innner_cohesion_threshold"`
	MinHeight        float64 `json:"min_height"`
	MaxHeight        float64 `json:"max_height"`
	BlockDistance    float64 `json:"block_distance"`
	BlockWeight      float64 `json:"block_weight"`
}

func newFlocking(_ string, raw json.RawMessage) (mob.Component, error) {
	cfg := flockConfig{SeparationWeight: 1, SeparationThresh: 2, CohesionWeight: 1, CohesionThresh: 1}
	if err := decode(raw, &cfg); err != nil {
		return nil, err
	}
	cfg.LonerChance = mgl64.Clamp(cfg.LonerChance, 0, 1)
	return &Flocking{cfg: cfg}, nil
}

// followFlockSpeeds are the follow_flock goals of Cod/Salmon/TropicalFish/
// Dolphin::createAIGoals (speed, priority).
var followFlockSpeeds = map[string][2]float64{
	"minecraft:cod":          {0.09, 2},
	"minecraft:salmon":       {0.12, 2},
	"minecraft:tropicalfish": {0.05, 2},
	"minecraft:dolphin":      {0.1, 3},
}

const followFlockKey = "minecraft:follow_flock"

func (f *Flocking) Attach(m *mob.Mob) {
	r := m.Rand()
	f.canFlock = r.Float64() >= f.cfg.LonerChance
	f.limit = f.cfg.LowLimit
	if f.cfg.LowLimit < f.cfg.HighLimit {
		f.limit += r.IntN(f.cfg.HighLimit - f.cfg.LowLimit)
	}
	f.leader, f.inFlock, f.members = false, false, nil
	if s, ok := followFlockSpeeds[m.Identifier()]; ok {
		m.Goals().Add(followFlockKey, int(s[1]), &followFlock{GoalBase: mob.GoalBase{Control: mob.FlagMove | mob.FlagLook}, interval: 2, speed: s[0]})
	}
}

func (f *Flocking) Detach(m *mob.Mob) {
	if _, ok := m.Goals().Goal(followFlockKey); ok {
		m.Goals().Remove(followFlockKey)
	}
	f.breakFlock(m)
}

// FlockOf returns m's flocking component.
func FlockOf(m *mob.Mob) (*Flocking, bool) { return mob.ComponentOf[*Flocking](m) }

// HoldHeading keeps the flock's goal heading (SwimIdleGoal::start: no goal,
// heading held, neighbourhood data refreshed).
func (f *Flocking) HoldHeading(m *mob.Mob) {
	f.heading = mgl64.Vec3{}
	f.hasGoal, f.goalHeld = false, true
	f.updateNeighbourhood(m)
}

// Leader, InFlock and Members describe the flock state (for tests and
// debugging).
func (f *Flocking) Leader() bool  { return f.leader }
func (f *Flocking) InFlock() bool { return f.inFlock }
func (f *Flocking) Members() int  { return len(f.members) }
func (f *Flocking) Loner() bool   { return !f.canFlock }

// Steering is the member's flock vector (FlockingComponent +60).
func (f *Flocking) Steering() mgl64.Vec3 { return f.steer }

// TickStage implements mob.StagedTicker.
func (f *Flocking) TickStage() mob.Stage { return mob.StageSensors }

// Tick is FlockingSystem::tickFlockingComponent.
func (f *Flocking) Tick(m *mob.Mob) {
	defer func() { f.prevPos, f.hasPrev = m.Position(), true }()
	if m.Dead() || !f.canFlock || (f.cfg.InWater && !m.Physics().InWater) {
		return
	}
	f.manage(m)
	if f.leader {
		f.goalHeading(m)
		f.merge(m)
		f.updateNeighbourhood(m)
		f.flockVector(m)
	}
}

// delta is pos - posPrev.
func (f *Flocking) delta(m *mob.Mob) mgl64.Vec3 {
	if !f.hasPrev {
		return mgl64.Vec3{}
	}
	return m.Position().Sub(f.prevPos)
}

type flockPeer struct {
	m *mob.Mob
	f *Flocking
}

// peers returns the same-type, adult mobs with a flocking component in m's
// box grown by r.
func (f *Flocking) peers(m *mob.Mob, r float64) []flockPeer {
	var out []flockPeer
	for e := range m.Tx().EntitiesWithin(m.BBox().Grow(r)) {
		o, ok := mob.Of(e)
		if !ok || o.H() == m.H() || o.Identifier() != m.Identifier() || o.Baby() {
			continue
		}
		of, ok := FlockOf(o)
		if !ok {
			continue
		}
		out = append(out, flockPeer{o, of})
	}
	return out
}

func (f *Flocking) sameVariant(m, o *mob.Mob) bool {
	if o.Identifier() != m.Identifier() {
		return false
	}
	if !f.cfg.MatchVariants {
		return true
	}
	a, b := m.Meta(), o.Meta()
	return a.Variant == b.Variant && a.MarkVariant == b.MarkVariant && a.Color == b.Color && a.Color2 == b.Color2
}

// CanJoin is FlockingComponent::canJoinFlock.
func (f *Flocking) CanJoin(m *mob.Mob) bool {
	if f.leader || f.inFlock {
		return false
	}
	for _, p := range f.peers(m, f.cfg.InfluenceRadius) {
		if !p.f.inFlock && !p.f.leader && p.f.canFlock && f.sameVariant(m, p.m) {
			return true
		}
		if p.f.inFlock && p.f.leader && f.sameVariant(m, p.m) && len(p.f.members) <= p.f.limit {
			return true
		}
	}
	return false
}

// join is FlockingComponent::joinFlock.
func (f *Flocking) join(m *mob.Mob) {
	if f.leader || f.inFlock {
		return
	}
	peers := f.peers(m, f.cfg.InfluenceRadius)
	for _, p := range peers {
		if p.f.inFlock && p.f.leader && f.sameVariant(m, p.m) && len(p.f.members) <= p.f.limit {
			p.f.members = append(p.f.members, m.H())
			f.leader, f.inFlock = false, true
			p.f.updateNeighbourhood(p.m)
			return
		}
	}
	for _, p := range peers {
		if !p.f.inFlock && !p.f.leader && p.f.canFlock && f.sameVariant(m, p.m) && len(f.members) <= f.limit {
			p.f.members = append(p.f.members[:0], m.H(), p.m.H())
			p.f.inFlock, p.f.leader = true, true
			p.f.limit = f.limit
			f.leader, f.inFlock = false, true
			p.f.updateNeighbourhood(p.m)
			return
		}
	}
}

func (f *Flocking) breakFlock(m *mob.Mob) {
	f.leader, f.inFlock = false, false
	f.updateNeighbourhood(m)
	f.members = nil
}

// resolve returns the mobs of the member list that are in the world.
func (f *Flocking) resolve(tx *world.Tx) []flockPeer {
	out := make([]flockPeer, 0, len(f.members))
	for _, h := range f.members {
		e, ok := h.Entity(tx)
		if !ok {
			continue
		}
		o, ok := mob.Of(e)
		if !ok {
			continue
		}
		of, ok := FlockOf(o)
		if !ok {
			continue
		}
		out = append(out, flockPeer{o, of})
	}
	return out
}

// manage is FlockingComponent::manageNeighborhood.
func (f *Flocking) manage(m *mob.Mob) {
	tx := m.Tx()
	switch {
	case f.leader:
		near := map[*world.EntityHandle]bool{}
		for _, p := range f.peers(m, f.cfg.InfluenceRadius) {
			near[p.m.H()] = true
		}
		kept := f.members[:0]
		for _, h := range f.members {
			e, ok := h.Entity(tx)
			if !ok || h == m.H() {
				kept = append(kept, h)
				continue
			}
			o, _ := mob.Of(e)
			if of, ok := FlockOf(o); ok && !near[h] && !of.leader {
				of.leader, of.inFlock = false, false
				of.updateNeighbourhood(o)
				of.members = nil
				continue
			}
			kept = append(kept, h)
		}
		f.members = kept
	case f.inFlock:
		found := false
		for _, p := range f.peers(m, 2*f.cfg.InfluenceRadius) {
			if !p.f.leader {
				continue
			}
			for _, h := range p.f.members {
				if h == m.H() {
					found = true
				}
			}
		}
		if !found {
			f.breakFlock(m)
		}
	}
	if f.inFlock && len(f.members) == 1 {
		f.breakFlock(m)
	}
	if !f.inFlock && len(f.members) > 0 {
		f.members = nil
	}
}

// merge is FlockingComponent::mergeNeighborhoods.
func (f *Flocking) merge(m *mob.Mob) {
	for _, p := range f.peers(m, f.cfg.InfluenceRadius) {
		if !p.f.leader || !f.sameVariant(m, p.m) {
			continue
		}
		if len(f.members)+len(p.f.members) <= f.limit {
			for _, o := range p.f.resolve(m.Tx()) {
				o.f.limit = f.limit
			}
			f.members = append(f.members, p.f.members...)
		}
		p.f.leader = false
		p.f.members = nil
	}
}

// goalHeading is FlockingComponent::calculateGoalHeading: the leader's next
// path node.
func (f *Flocking) goalHeading(m *mob.Mob) {
	nav := m.Navigation()
	if m.HasNavigation() && !nav.Done() {
		if p := nav.Path(); p != nil && p.Len() > 0 && p.Index() < p.Len() {
			n := p.Node(p.Index())
			f.hasGoal, f.goalHeld = true, false
			f.heading = mgl64.Vec3{float64(n[0]), float64(n[1]), float64(n[2])}
			return
		}
	}
	if !f.goalHeld {
		f.hasGoal = false
		f.heading = mgl64.Vec3{}
	}
}

// updateNeighbourhood is FlockingComponent::updateNeighborhoodData: the
// leader's goal state and velocity go to every member.
func (f *Flocking) updateNeighbourhood(m *mob.Mob) {
	f.vel = f.delta(m)
	if m.Tx() == nil {
		return
	}
	for _, p := range f.resolve(m.Tx()) {
		if p.m.H() != m.H() {
			p.f.hasGoal, p.f.goalHeld, p.f.inFlock = f.hasGoal, f.goalHeld, f.inFlock
			p.f.heading, p.f.canFlock = f.heading, f.canFlock
		}
		p.f.vel = f.vel
	}
}

// flockVector is FlockingComponent::calculateFlockVector, run by the leader.
func (f *Flocking) flockVector(m *mob.Mob) {
	tx := m.Tx()
	members := f.resolve(tx)
	com := m.Position()
	if f.cfg.UseCentreOfMass && len(members) > 0 {
		var sum mgl64.Vec3
		for _, p := range members {
			sum = sum.Add(p.m.Position())
		}
		com = sum.Mul(1 / float64(len(members)))
	}
	f.centre = com
	sepSq := f.cfg.SeparationThresh * f.cfg.SeparationThresh
	for _, p := range members {
		pos := p.m.Position()
		var sep mgl64.Vec3
		for _, o := range members {
			if o.m.H() == p.m.H() {
				continue
			}
			if d := pos.Sub(o.m.Position()); d.LenSqr() < sepSq {
				sep = sep.Add(d)
			}
		}
		sep = norm(sep)
		goal := f.heading
		if f.hasGoal && !f.goalHeld {
			goal = norm(f.heading.Sub(pos))
		}
		p.f.tooFar = false
		var coh mgl64.Vec3
		if d := pos.Sub(com).Len(); d > f.cfg.InnerCohesion {
			coh = norm(com.Sub(pos))
			if d > f.cfg.CohesionThresh {
				p.f.tooFar = true
			}
		}
		align := f.vel.Sub(p.f.delta(p.m))
		blk := f.blockAvoidance(p.m)
		vy := 0.0
		if f.cfg.InWater {
			vy = f.breach(m, p.m)
		}
		v := sep.Mul(p.f.cfg.SeparationWeight).Add(coh.Mul(p.f.cfg.CohesionWeight)).Add(goal.Mul(f.cfg.GoalWeight)).Add(blk).Add(align).Add(mgl64.Vec3{0, vy, 0})
		p.f.steer = norm(v)
	}
}

// blockAvoidance pushes away from solid blocks (and air, in water) within
// block_distance, scaled by block_weight.
func (f *Flocking) blockAvoidance(m *mob.Mob) mgl64.Vec3 {
	b := int(f.cfg.BlockDistance)
	if b <= 0 || f.cfg.BlockWeight == 0 {
		return mgl64.Vec3{}
	}
	tx := m.Tx()
	pos := m.Position()
	inWater := m.Physics().InWater
	var sum mgl64.Vec3
	for ix := -b; ix <= b; ix++ {
		for iy := -b; iy <= b; iy++ {
			for iz := -b; iz <= b; iz++ {
				q := pos.Add(mgl64.Vec3{float64(ix), float64(iy), float64(iz)})
				bp := cube.PosFromVec3(q)
				if solid, air := solidOrAirAt(tx, bp, inWater); solid || air {
					d := norm(pos.Sub(q))
					if clip(tx, pos, pos.Sub(d.Mul(f.cfg.BlockDistance))) {
						sum = sum.Add(d)
					}
				}
			}
		}
	}
	return sum.Mul(f.cfg.BlockWeight)
}

// breach is the in_water vertical part: -breach_influence when the member is
// near the top of the water band or has no water above.
func (f *Flocking) breach(leader, m *mob.Mob) float64 {
	tx := m.Tx()
	lp := cube.PosFromVec3(leader.Position())
	top := float64(aboveTopSolid(tx, lp[0], lp[2]))
	s := top
	for y := int(top); y < tx.Range()[1] && waterAt(tx, cube.Pos{lp[0], y, lp[2]}); y++ {
		s++
	}
	s--
	lim := math.Max(leader.Position()[1]-f.cfg.MaxHeight, top-20)
	if lim <= top {
		lim = top + 0.5
	}
	if lim > s-1.5 || !waterAt(tx, cube.PosFromVec3(m.Position().Add(mgl64.Vec3{0, 1, 0}))) {
		return -f.cfg.BreachInfluence
	}
	return 0
}

// followFlock is the hard-coded minecraft:follow_flock goal (BDS
// FollowFlockGoal): every third check, an airborne-or-swimming mob that can
// join a flock does; while in a flock (not leading) it steers along its flock
// vector at speed (twice that when too far from the flock), capped at 2 x
// speed. Flags move|look.
type followFlock struct {
	mob.GoalBase
	interval, counter int
	speed             float64
}

func (g *followFlock) CanUse(m *mob.Mob) bool {
	if g.counter++; g.counter <= g.interval {
		return false
	}
	g.counter = 0
	f, ok := FlockOf(m)
	return ok && !m.OnGround() && f.CanJoin(m)
}

func (g *followFlock) CanContinueToUse(m *mob.Mob) bool {
	f, ok := FlockOf(m)
	return ok && f.inFlock && !f.leader && !m.OnGround()
}

func (g *followFlock) Start(m *mob.Mob) {
	if f, ok := FlockOf(m); ok {
		f.join(m)
	}
	m.Navigation().SetSpeed(g.speed)
	m.Navigation().Stop(m)
}

func (g *followFlock) Stop(m *mob.Mob) {
	if f, ok := FlockOf(m); ok {
		f.breakFlock(m)
	}
}

func (g *followFlock) Tick(m *mob.Mob) {
	f, ok := FlockOf(m)
	if !ok {
		return
	}
	if !((f.heading != (mgl64.Vec3{}) && f.hasGoal) || f.goalHeld) {
		return
	}
	s := g.speed
	if f.tooFar {
		s *= 2
	}
	v := f.steer.Mul(s)
	if v.Len() > 2*g.speed {
		v = norm(v).Mul(2 * g.speed)
	}
	vel := m.Vel()
	m.MoveControl().SetWantedPosition(m.Position().Add(vel).Add(v.Sub(vel).Mul(0.22)), 1)
}

func (g *followFlock) AppendDebugInfo(b *strings.Builder) {
	fmt.Fprintf(b, "FollowFlock %.2f", g.speed)
}
