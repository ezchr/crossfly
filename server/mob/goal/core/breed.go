//go:build crossfly_full

package core

import (
	"fmt"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/mob"
	ccore "github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// breed is minecraft:behavior.breed: a mob in love walks to the nearest
// partner it can mate with and, once they are close, makes a baby (BDS
// BreedGoal; the mating itself is component/core Breedable.Mate).
//
//	speed_multiplier 1. Flags move|look.
type breed struct {
	mob.GoalBase
	speed float64

	partner *world.EntityHandle
	ticks   int
}

func newBreed(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Speed float64 `json:"speed_multiplier"`
	}{Speed: 1}
	if err := spec.Decode(&j); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	return &breed{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, speed: j.Speed}, nil
}

// closeEnough is BreedGoal::_isCloseEnoughToBreed: two boxes (width x height,
// centred on the positions) whose points nearest the midpoint are within 0.4
// blocks, or for small mobs centres within 0.6 of touching.
func closeEnough(p1 mgl64.Vec3, w1, h1 float64, p2 mgl64.Vec3, w2, h2 float64) bool {
	r1, r2 := math.Max(w1, h1)*0.5, math.Max(w2, h2)*0.5
	if r1 < 0.5 && r2 < 0.5 {
		return p1.Sub(p2).Len()-(r1+r2) <= 0.6
	}
	mid := p1.Add(p2).Mul(0.5)
	clamp := func(p mgl64.Vec3, w, h float64) mgl64.Vec3 {
		lo := p.Sub(mgl64.Vec3{w * 0.5, h * 0.5, w * 0.5})
		hi := p.Add(mgl64.Vec3{w * 0.5, h * 0.5, w * 0.5})
		return mgl64.Vec3{math.Max(lo[0], math.Min(hi[0], mid[0])), math.Max(lo[1], math.Min(hi[1], mid[1])), math.Max(lo[2], math.Min(hi[2], mid[2]))}
	}
	return clamp(p1, w1, h1).Sub(clamp(p2, w2, h2)).LenSqr() <= 0.16000001
}

func dims(e world.Entity) (w, h float64) {
	b := e.H().Type().BBox(e)
	return b.Width(), b.Height()
}

func pathEndVec(p mob.Path) mgl64.Vec3 {
	end := p.End()
	return mgl64.Vec3{float64(end[0]) + 0.5, float64(end[1]), float64(end[2]) + 0.5}
}

func (g *breed) closeToPath(m *mob.Mob, other world.Entity) bool {
	p := m.Navigation().Path()
	if p == nil {
		return false
	}
	w1, h1 := dims(m)
	w2, h2 := dims(other)
	return closeEnough(pathEndVec(p), w1, h1, other.Position(), w2, h2)
}

// freePartner is BreedGoal::_moveToFreePartner: the nearest mob in a box
// grown by 8 that can mate and whose path ends close to it.
func (g *breed) freePartner(m *mob.Mob, b *ccore.Breedable) world.Entity {
	var best world.Entity
	bestD := math.MaxFloat64
	for _, e := range nearby(m, 8, 8) {
		om, ok := mob.Of(e)
		if !ok || !b.CanMate(m, om) {
			continue
		}
		d := distSq(om.Position(), m.Position())
		if d < bestD && m.Navigation().MoveToEntity(m, om, g.speed) && g.closeToPath(m, om) {
			best, bestD = om, d
		}
	}
	return best
}

func (g *breed) CanUse(m *mob.Mob) bool {
	b, ok := ccore.BreedableOf(m)
	if !ok || !b.InLove() || !hasNavigation(m) {
		return false
	}
	g.partner = nil
	if p := g.freePartner(m, b); p != nil {
		g.partner = p.H()
	}
	return g.partner != nil
}

func (g *breed) partnerMob(m *mob.Mob) (*mob.Mob, bool) {
	if g.partner == nil {
		return nil, false
	}
	return mob.OfHandle(m.Tx(), g.partner)
}

func (g *breed) CanContinueToUse(m *mob.Mob) bool {
	p, ok := g.partnerMob(m)
	if !ok || p.Dead() || !ccore.InLove(p) || g.ticks >= 60 {
		return false
	}
	if distSq(m.Position(), p.Position()) <= 9 {
		return true
	}
	if !hasNavigation(m) {
		return true
	}
	if path := m.Navigation().Path(); path != nil {
		end := path.End()
		if end[0] == int(math.Floor(p.Position()[0])) && end[2] == int(math.Floor(p.Position()[2])) {
			return true
		}
	}
	return m.Navigation().MoveToEntity(m, p, g.speed) && g.closeToPath(m, p)
}

func (g *breed) Stop(m *mob.Mob) {
	g.partner, g.ticks = nil, 0
	m.Navigation().Stop(m)
}

func (g *breed) Tick(m *mob.Mob) {
	p, ok := g.partnerMob(m)
	if !ok {
		return
	}
	m.LookControl().SetLookAtEntity(p, 10, maxHeadXRot)
	m.Navigation().MoveToEntity(m, p, g.speed)
	if g.ticks++; g.ticks%60 != 0 {
		return
	}
	w1, h1 := dims(m)
	w2, h2 := dims(p)
	if !closeEnough(m.Position(), w1, h1, p.Position(), w2, h2) {
		return
	}
	if b, ok := ccore.BreedableOf(m); ok {
		b.Mate(m, p)
	}
}

func (g *breed) AppendDebugInfo(b *strings.Builder) { fmt.Fprintf(b, "Breed %d", g.ticks) }

// followParent is minecraft:behavior.follow_parent: a baby walks after the
// nearest adult of its kind that is 3 to 16 blocks away (BDS
// FollowParentGoal), repathing every 10 ticks.
//
//	speed_multiplier 1. No flags.
type followParent struct {
	mob.GoalBase
	speed float64

	parent *world.EntityHandle
	repath int
}

func newFollowParent(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Speed float64 `json:"speed_multiplier"`
	}{Speed: 1}
	if err := spec.Decode(&j); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	return &followParent{GoalBase: mob.GoalBase{Control: spec.FlagsOr(0)}, speed: j.Speed}, nil
}

func (g *followParent) CanUse(m *mob.Mob) bool {
	if !hasNavigation(m) || !ccore.IsBaby(m) {
		return false
	}
	var best *mob.Mob
	bestD := math.MaxFloat64
	for _, e := range nearby(m, 8, 4) {
		om, ok := mob.Of(e)
		if !ok || om.Identifier() != m.Identifier() || ccore.IsBaby(om) || om.Dead() {
			continue
		}
		if d := distSq(om.Position(), m.Position()); d <= bestD {
			best, bestD = om, d
		}
	}
	if best == nil || bestD < 9 {
		return false
	}
	g.parent = best.H()
	return true
}

func (g *followParent) CanContinueToUse(m *mob.Mob) bool {
	p, ok := mob.OfHandle(m.Tx(), g.parent)
	if !ok || p.Dead() {
		return false
	}
	d := distSq(m.Position(), p.Position())
	return d >= 9 && d <= 256
}

func (g *followParent) Start(*mob.Mob) { g.repath = 0 }

func (g *followParent) Stop(m *mob.Mob) {
	g.parent = nil
	m.Navigation().Stop(m)
}

func (g *followParent) Tick(m *mob.Mob) {
	if g.repath--; g.repath > 0 {
		return
	}
	g.repath = 10
	if p, ok := mob.OfHandle(m.Tx(), g.parent); ok && !p.Dead() {
		m.Navigation().MoveToEntity(m, p, g.speed)
	}
}

// mountPathing is minecraft:behavior.mount_pathing (BDS MountPathingGoal): a
// mob ridden by a hostile mob (spider and chicken jockeys) carries it to the
// rider's target: it paths there at speed_multiplier, looks at it, and every
// 4-10 ticks paths again while farther than target_dist (always with
// track_target, else only while it sees the target).
//
//	speed_multiplier 1, target_dist 0, track_target false. Flags move|look.
type mountPathing struct {
	mob.GoalBase
	speed, dist float64
	track       bool
	target      *world.EntityHandle
	repath      int
}

func newMountPathing(spec mob.GoalSpec) (mob.Goal, error) {
	v := struct {
		Speed *float64 `json:"speed_multiplier"`
		Dist  float64  `json:"target_dist"`
		Track bool     `json:"track_target"`
	}{}
	if err := spec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	g := &mountPathing{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, speed: 1, dist: v.Dist, track: v.Track}
	if v.Speed != nil {
		g.speed = *v.Speed
	}
	return g, nil
}

// riderTarget is the target of the mob's first rider, if that is a mob.
func (*mountPathing) riderTarget(m *mob.Mob) (world.Entity, bool) {
	ps := m.Passengers()
	if len(ps) == 0 {
		return nil, false
	}
	rider, ok := mob.Of(ps[0])
	if !ok {
		return nil, false
	}
	return rider.Target()
}

func (g *mountPathing) CanUse(m *mob.Mob) bool {
	if !m.HasNavigation() {
		return false
	}
	t, ok := g.riderTarget(m)
	if !ok {
		return false
	}
	g.target = t.H()
	g.repath = 4 + m.Rand().IntN(7)
	return m.Navigation().MoveToEntity(m, t, g.speed)
}

func (g *mountPathing) CanContinueToUse(m *mob.Mob) bool {
	t, ok := g.riderTarget(m)
	if !ok || t.H() != g.target {
		return false
	}
	if l, ok := t.(interface{ Dead() bool }); ok && l.Dead() {
		return false
	}
	return g.track || !m.Navigation().Done()
}

func (g *mountPathing) Stop(m *mob.Mob) {
	g.target = nil
	m.Navigation().Stop(m)
}

func (g *mountPathing) Tick(m *mob.Mob) {
	t, ok := g.target.Entity(m.Tx())
	if !ok {
		return
	}
	m.LookControl().SetLookAtEntity(t, 30, 30)
	if g.repath--; g.repath > 0 {
		return
	}
	g.repath = 4 + m.Rand().IntN(7)
	pos := t.Position()
	if m.Pos().Sub(pos).LenSqr() >= g.dist*g.dist {
		m.Navigation().MoveToEntity(m, t, g.speed)
	}
}
