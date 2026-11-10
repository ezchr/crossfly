//go:build crossfly_full

package ride

import (
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/component/tame"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// ---- behavior.follow_owner ----

// followOwner is minecraft:behavior.follow_owner (BDS 1.21.3
// FollowOwnerGoal): a tamed mob that is not sitting follows its player owner
// once it is start_distance away, repathing every 10 ticks at
// speed_multiplier, until it is within stop_distance. When no path is found,
// the mob is not leashed and the owner is 12 or more blocks away, it
// teleports next to the owner (can_teleport; FollowOwnerGoal::tick). Without
// can_teleport it only starts and keeps going while the owner is within
// max_distance and a path to the owner exists (_canReachOwner). Unless
// ignore_vibration, it does not start within 600 ticks of a vibration (no
// vibrations are modelled, so it always may). It does not avoid water while
// it follows.
//
//	speed_multiplier 1, start_distance 10, stop_distance 2, can_teleport
//	true, ignore_vibration true, max_distance 60 (LegacyGoalDefinition).
//	Flags move|look.
type followOwner struct {
	mob.GoalBase
	speed, start, stop, max float64
	teleport                bool
	owner                   *world.EntityHandle
	timer                   int
	avoidWater              bool
}

func newFollowOwner(spec mob.GoalSpec) (mob.Goal, error) {
	v := struct {
		Speed    *float64 `json:"speed_multiplier"`
		Start    *float64 `json:"start_distance"`
		Stop     *float64 `json:"stop_distance"`
		Max      *float64 `json:"max_distance"`
		Teleport *bool    `json:"can_teleport"`
	}{}
	if err := spec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	g := &followOwner{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, speed: 1, start: 10, stop: 2, max: 60, teleport: true}
	setF(&g.speed, v.Speed)
	setF(&g.start, v.Start)
	setF(&g.stop, v.Stop)
	setF(&g.max, v.Max)
	if v.Teleport != nil {
		g.teleport = *v.Teleport
	}
	return g, nil
}

// playerOwner is the mob's owner if it is a player in the world who is not
// a spectator.
func playerOwner(m *mob.Mob) (*player.Player, bool) {
	o, ok := m.OwnerEntity()
	if !ok {
		return nil, false
	}
	p, ok := o.(*player.Player)
	if !ok || p.GameMode() == world.GameModeSpectator || p.Dead() {
		return nil, false
	}
	return p, true
}

func (g *followOwner) CanUse(m *mob.Mob) bool {
	if !m.HasNavigation() || orderedToSit(m) {
		return false
	}
	p, ok := playerOwner(m)
	if !ok || p.Position().Sub(m.Pos()).LenSqr() < g.start*g.start || !g.canReachOwner(m, p) {
		return false
	}
	g.owner = p.H()
	return true
}

func (g *followOwner) CanContinueToUse(m *mob.Mob) bool {
	if orderedToSit(m) {
		return false
	}
	p, ok := playerOwner(m)
	if !ok || p.H() != g.owner {
		return false
	}
	return p.Position().Sub(m.Pos()).LenSqr() > g.stop*g.stop && !m.Navigation().Done() && g.canReachOwner(m, p)
}

// canReachOwner is FollowOwnerGoal::_canReachOwner: always with
// can_teleport, else the owner must be within max_distance and a path to
// them must exist.
func (g *followOwner) canReachOwner(m *mob.Mob, o world.Entity) bool {
	if g.teleport {
		return true
	}
	if o.Position().Sub(m.Pos()).LenSqr() > g.max*g.max {
		return false
	}
	return m.Navigation().CreatePathToEntity(m, o) != nil
}

func (g *followOwner) Start(m *mob.Mob) {
	g.timer = 0
	if n, ok := m.Navigation().(interface {
		AvoidWater() bool
		SetAvoidWater(bool)
	}); ok {
		g.avoidWater = n.AvoidWater()
		n.SetAvoidWater(false)
	}
	if o, ok := g.owner.Entity(m.Tx()); ok {
		g.moveOrTeleport(m, o)
	}
}

func (g *followOwner) Stop(m *mob.Mob) {
	g.owner = nil
	m.Navigation().Stop(m)
	if n, ok := m.Navigation().(interface{ SetAvoidWater(bool) }); ok {
		n.SetAvoidWater(g.avoidWater)
	}
}

func (g *followOwner) Tick(m *mob.Mob) {
	o, ok := g.owner.Entity(m.Tx())
	if !ok {
		return
	}
	m.LookControl().SetLookAtEntity(o, 10, 40)
	if orderedToSit(m) || m.Riding() {
		return
	}
	if g.timer--; g.timer > 0 {
		return
	}
	g.timer = 10
	g.moveOrTeleport(m, o)
}

// moveOrTeleport paths to the owner. When that fails, a mob that may
// teleport, is not on a lead and is 12 or more blocks from its owner
// teleports next to them (FollowOwnerGoal::tick checks !isLeashed()).
func (g *followOwner) moveOrTeleport(m *mob.Mob, o world.Entity) {
	if m.Navigation().MoveToEntity(m, o, g.speed) {
		return
	}
	if g.teleport && !core.Leashed(m) && o.Position().Sub(m.Pos()).LenSqr() >= 144 {
		if teleportNear(m, o) {
			m.Navigation().Stop(m)
		}
	}
}

// teleportNear puts m on a free spot next to o
// (FollowOwnerGoal::_tryToGetTeleportPosition, 1.21.3, with
// RANDOMIZE_AND_CHECK_FOR_CLERANCE_WHEN_TELEPORTING, which every world since
// base game version 1.20.80 has): of the blocks 2 away from the block of o
// in x or z (the ring around the 3x3 around o), at the height of the feet of
// o, those whose block below is solid and whose feet and head blocks are
// neither solid nor liquid, in random order; the first of at most 9 of them
// where the box of m (centred on the spot) holds no other entity, moved by
// up to 0.05 in x and z. It reports whether m was teleported.
func teleportNear(m *mob.Mob, o world.Entity) bool {
	pos, ok := teleportSpot(m, o)
	if !ok {
		return false
	}
	m.Teleport(pos)
	m.ResetFallDistance()
	return true
}

func teleportSpot(m *mob.Mob, o world.Entity) (mgl64.Vec3, bool) {
	tx := m.Tx()
	op := o.Position()
	ox, oz := int(math.Floor(op[0])), int(math.Floor(op[2]))
	y := int(math.Floor(o.H().Type().BBox(o).Translate(op).Min()[1]))
	var spots []mgl64.Vec3
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			if dx >= -1 && dz >= -1 && dx <= 1 && dz <= 1 {
				continue
			}
			p := cube.Pos{ox + dx, y, oz + dz}
			if !solidBlock(tx, p.Side(cube.FaceDown)) || solidBlock(tx, p) || liquidAt(tx, p) ||
				solidBlock(tx, p.Side(cube.FaceUp)) || liquidAt(tx, p.Side(cube.FaceUp)) {
				continue
			}
			spots = append(spots, mgl64.Vec3{float64(p[0]) + 0.5, float64(p[1]), float64(p[2]) + 0.5})
		}
	}
	r := m.Rand()
	r.Shuffle(len(spots), func(i, j int) { spots[i], spots[j] = spots[j], spots[i] })
	box := m.H().Type().BBox(m)
	w, h, d := box.Width()/2, box.Height()/2, box.Length()/2
	for _, s := range spots[:min(len(spots), 9)] {
		at := cube.Box(s[0]-w, s[1]-h, s[2]-d, s[0]+w, s[1]+h, s[2]+d)
		if !unobstructedByEntities(tx, at, m) {
			continue
		}
		s[0] += r.Float64()*0.1 - 0.05
		s[2] += r.Float64()*0.1 - 0.05
		return s, true
	}
	return mgl64.Vec3{}, false
}

// solidBlock is Material::isSolid for the teleport spot: a block with a
// collision box.
func solidBlock(tx *world.Tx, p cube.Pos) bool {
	return len(tx.Block(p).Model().BBox(p, tx)) > 0
}

func liquidAt(tx *world.Tx, p cube.Pos) bool {
	_, ok := tx.Liquid(p)
	return ok
}

// unobstructedByEntities is BlockSource::isUnobstructedByEntities: no player
// or mob other than m in box.
func unobstructedByEntities(tx *world.Tx, box cube.BBox, m *mob.Mob) bool {
	for e := range tx.EntitiesWithin(box) {
		if e.H() == m.H() {
			continue
		}
		if _, ok := e.(*player.Player); ok {
			return false
		}
		if om, ok := mob.Of(e); ok && !om.Dead() {
			return false
		}
	}
	return true
}

// ---- behavior.stay_while_sitting ----

// stayWhileSitting is minecraft:behavior.stay_while_sitting: a sitting mob
// stays where it is.
//
//	Flags move|jump.
type stayWhileSitting struct{ mob.GoalBase }

func newStayWhileSitting(spec mob.GoalSpec) (mob.Goal, error) {
	return &stayWhileSitting{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagJump)}}, nil
}

func (*stayWhileSitting) CanUse(m *mob.Mob) bool { return orderedToSit(m) }
func (*stayWhileSitting) Start(m *mob.Mob)       { m.StopMovement() }
func (*stayWhileSitting) Tick(m *mob.Mob)        { m.Navigation().Stop(m) }

// ---- behavior.teleport_to_owner ----

// teleportToOwner is minecraft:behavior.teleport_to_owner: when its filters
// pass (far from a panicking or fighting pet's owner) the mob teleports next
// to the owner, at most once every cooldown seconds.
//
//	cooldown 1. Flags none.
type teleportToOwner struct {
	mob.GoalBase
	filter   mob.Filter
	cooldown int64
	next     int64
}

func newTeleportToOwner(spec mob.GoalSpec) (mob.Goal, error) {
	v := struct {
		Filters  json.RawMessage `json:"filters"`
		Cooldown *float64        `json:"cooldown"`
	}{}
	if err := spec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	f, err := mob.CompileFilter(v.Filters)
	if err != nil {
		return nil, fmt.Errorf("%v: filters: %w", spec.Key, err)
	}
	g := &teleportToOwner{GoalBase: mob.GoalBase{Control: spec.FlagsOr(0)}, filter: f, cooldown: 20}
	if v.Cooldown != nil {
		g.cooldown = int64(*v.Cooldown * 20)
	}
	return g, nil
}

func (g *teleportToOwner) CanUse(m *mob.Mob) bool {
	// Never on a lead, as the teleport of follow_owner (FollowOwnerGoal::tick).
	if m.Ticks() < g.next || orderedToSit(m) || m.Riding() || core.Leashed(m) {
		return false
	}
	o, ok := playerOwner(m)
	if !ok {
		return false
	}
	ctx := m.Ctx()
	ctx.Other = o
	return g.filter == nil || g.filter.Test(ctx)
}

func (g *teleportToOwner) CanContinueToUse(*mob.Mob) bool { return false }

func (g *teleportToOwner) Start(m *mob.Mob) {
	g.next = m.Ticks() + g.cooldown
	if o, ok := playerOwner(m); ok && teleportNear(m, o) {
		m.Navigation().Stop(m)
		m.ResetTarget()
	}
}

// ---- behavior.owner_hurt_by_target / owner_hurt_target ----

// The last mob each player hurt (owner_hurt_target), recorded by a hurt
// hook. seq grows with every hit so a goal sees each one once.
var (
	ownerHitsMu sync.Mutex
	ownerHits   = map[*world.EntityHandle]ownerHit{}
	ownerSeq    uint64
)

type ownerHit struct {
	victim *world.EntityHandle
	seq    uint64
}

func recordOwnerHit(m *mob.Mob, _ float64, src world.DamageSource) {
	var attacker world.Entity
	switch s := src.(type) {
	case entity.AttackDamageSource:
		attacker = s.Attacker
	case entity.ProjectileDamageSource:
		attacker = s.Owner
	}
	if _, ok := attacker.(*player.Player); !ok {
		return
	}
	ownerHitsMu.Lock()
	ownerSeq++
	ownerHits[attacker.H()] = ownerHit{victim: m.H(), seq: ownerSeq}
	ownerHitsMu.Unlock()
}

func lastOwnerHit(p *world.EntityHandle) (ownerHit, bool) {
	ownerHitsMu.Lock()
	defer ownerHitsMu.Unlock()
	h, ok := ownerHits[p]
	return h, ok
}

// attackable reports whether a pet may attack t for its owner o (java
// Wolf.wantsToAttack: not the owner, not another pet of the owner, not a
// creeper or ghast, not dead).
func attackable(m *mob.Mob, t world.Entity, o world.Entity) bool {
	if t == nil || t.H() == m.H() || t.H() == o.H() {
		return false
	}
	if tm, ok := mob.Of(t); ok {
		if tm.Dead() || tm.HasFamily("creeper") || tm.HasFamily("ghast") {
			return false
		}
		if id, ok := tm.OwnerUUID(); ok && id == o.H().UUID() {
			return false
		}
	}
	if p, ok := t.(*player.Player); ok && (p.Dead() || !p.GameMode().AllowsTakingDamage()) {
		return false
	}
	return true
}

// ownerHurtBy is minecraft:behavior.owner_hurt_by_target (BDS
// OwnerHurtByTargetGoal): a tamed mob targets what last hurt its owner, a mob
// or a player (player.LastAttacker).
// ownerHurt is minecraft:behavior.owner_hurt_target (BDS
// OwnerHurtTargetGoal): it targets what its owner hit last, once per hit.
//
//	Flags none (target goals).
type ownerHurtBy struct {
	mob.GoalBase
	hurt    bool // owner_hurt_target
	target  *world.EntityHandle
	lastSeq uint64
	lastAt  time.Time
}

func newOwnerHurtBy(hurt bool) mob.GoalFactory {
	return func(spec mob.GoalSpec) (mob.Goal, error) {
		return &ownerHurtBy{GoalBase: mob.GoalBase{Control: spec.FlagsOr(0)}, hurt: hurt}, nil
	}
}

func (*ownerHurtBy) IsTargetGoal() bool { return true }

func (g *ownerHurtBy) CanUse(m *mob.Mob) bool {
	if !tame.Tamed(m) || orderedToSit(m) {
		return false
	}
	o, ok := playerOwner(m)
	if !ok {
		return false
	}
	var t world.Entity
	if g.hurt {
		hit, ok := lastOwnerHit(o.H())
		if !ok || hit.seq == g.lastSeq {
			return false
		}
		g.lastSeq = hit.seq
		if t, ok = hit.victim.Entity(m.Tx()); !ok {
			return false
		}
	} else {
		// Who last hurt the owner (player.LastAttacker: mobs and players), once
		// per hit, as BDS compares the last hurt by timestamp.
		h, at, ok := o.LastAttacker()
		if !ok || at.Equal(g.lastAt) || time.Since(at) > 5*time.Second {
			return false
		}
		g.lastAt = at
		if t, ok = h.Entity(m.Tx()); !ok {
			return false
		}
	}
	if !attackable(m, t, o) || t.Position().Sub(m.Pos()).Len() > m.Attr(mob.AttrFollowRange) {
		return false
	}
	g.target = t.H()
	return true
}

func (g *ownerHurtBy) CanContinueToUse(m *mob.Mob) bool {
	t, ok := m.Target()
	if !ok || t.H() != g.target {
		return false
	}
	if l, ok := t.(interface{ Dead() bool }); ok && l.Dead() {
		return false
	}
	return t.Position().Sub(m.Pos()).Len() <= m.Attr(mob.AttrFollowRange)
}

func (g *ownerHurtBy) Start(m *mob.Mob) {
	if t, ok := g.target.Entity(m.Tx()); ok {
		m.SetTarget(t)
	}
}

func (g *ownerHurtBy) Stop(m *mob.Mob) {
	if m.TargetHandle() == g.target {
		m.ResetTarget()
	}
	g.target = nil
}

// ---- behavior.beg ----

// beg is minecraft:behavior.beg (BDS BegGoal, java BegGoal): the mob looks
// interested at the nearest player within look_distance holding one of
// items, for look_time ticks (the interested flag tilts a wolf's head).
//
//	look_distance 8, look_time 2-4. Flags look.
type beg struct {
	mob.GoalBase
	items      []core.ItemDescriptor
	dist       float64
	minT, maxT int
	left       int
	player     *world.EntityHandle
}

func newBeg(spec mob.GoalSpec) (mob.Goal, error) {
	v := struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
		Dist *float64 `json:"look_distance"`
		Time *struct {
			Min float64 `json:"min"`
			Max float64 `json:"max"`
		} `json:"look_time"`
	}{}
	if err := spec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	g := &beg{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagLook)}, dist: 8, minT: 2, maxT: 4}
	setF(&g.dist, v.Dist)
	if v.Time != nil {
		g.minT, g.maxT = int(v.Time.Min), int(v.Time.Max)
	}
	for _, it := range v.Items {
		g.items = append(g.items, core.ParseItemDescriptor(it.Name))
	}
	return g, nil
}

func (g *beg) wants(e world.Entity) bool {
	return holding(e, func(s item.Stack) bool { return core.MatchesAny(g.items, s) })
}

func (g *beg) CanUse(m *mob.Mob) bool {
	p, ok := nearestPlayer(m, g.dist)
	if !ok || !g.wants(p) {
		return false
	}
	g.player = p.H()
	return true
}

func (g *beg) CanContinueToUse(m *mob.Mob) bool {
	e, ok := g.player.Entity(m.Tx())
	if !ok || g.left <= 0 || e.Position().Sub(m.Pos()).LenSqr() > g.dist*g.dist {
		return false
	}
	if p, ok := e.(*player.Player); ok && p.Dead() {
		return false
	}
	return g.wants(e)
}

func (g *beg) Start(m *mob.Mob) {
	g.left = g.minT
	if g.maxT > g.minT {
		g.left += m.Rand().IntN(g.maxT - g.minT + 1)
	}
	m.SetActorFlag(protocol.EntityDataFlagInterested, true)
}

func (g *beg) Stop(m *mob.Mob) {
	m.SetActorFlag(protocol.EntityDataFlagInterested, false)
	g.player = nil
}

func (g *beg) Tick(m *mob.Mob) {
	if e, ok := g.player.Entity(m.Tx()); ok {
		m.LookControl().SetLookAtEntity(e, 10, 40)
	}
	g.left--
}

// ---- behavior.pet_sleep_with_owner ----

// petSleepWithOwner is minecraft:behavior.pet_sleep_with_owner (BDS
// PetSleepWithOwnerGoal, java CatLieOnBedGoal/CatRelaxOnOwnerGoal): a tamed
// mob whose owner sleeps in a bed within 10 blocks (search_height up or
// down) walks onto the bed at speed_multiplier and lies down within
// goal_radius; when the owner wakes, on_wake_with_owner fires.
//
//	speed_multiplier 1, search_radius 10, search_height 1, goal_radius 1.
//	Flags move|look|jump.
type petSleepWithOwner struct {
	mob.GoalBase
	speed, radius, height, goal float64
	bed                         cube.Pos
	owner                       *world.EntityHandle
	lying                       bool
}

func newPetSleepWithOwner(spec mob.GoalSpec) (mob.Goal, error) {
	v := struct {
		Speed  *float64 `json:"speed_multiplier"`
		Radius *float64 `json:"search_radius"`
		Height *float64 `json:"search_height"`
		Goal   *float64 `json:"goal_radius"`
	}{}
	if err := spec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	g := &petSleepWithOwner{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook | mob.FlagJump)}, speed: 1, radius: 10, height: 1, goal: 1}
	setF(&g.speed, v.Speed)
	setF(&g.radius, v.Radius)
	setF(&g.height, v.Height)
	setF(&g.goal, v.Goal)
	return g, nil
}

// ownerBed is the bed the sleeping owner of m lies in, if close enough.
func (g *petSleepWithOwner) ownerBed(m *mob.Mob) (*player.Player, cube.Pos, bool) {
	if !tame.Tamed(m) {
		return nil, cube.Pos{}, false
	}
	p, ok := playerOwner(m)
	if !ok {
		return nil, cube.Pos{}, false
	}
	bed, sleeping := p.Sleeping()
	if !sleeping {
		return nil, cube.Pos{}, false
	}
	d := bed.Vec3Centre().Sub(m.Pos())
	if math.Abs(d[1]) > g.height || d[0]*d[0]+d[2]*d[2] > g.radius*g.radius {
		return nil, cube.Pos{}, false
	}
	return p, bed, true
}

func (g *petSleepWithOwner) CanUse(m *mob.Mob) bool {
	if orderedToSit(m) {
		return false
	}
	p, bed, ok := g.ownerBed(m)
	if !ok {
		return false
	}
	g.owner, g.bed = p.H(), bed
	return true
}

func (g *petSleepWithOwner) CanContinueToUse(m *mob.Mob) bool {
	p, bed, ok := g.ownerBed(m)
	return ok && p.H() == g.owner && bed == g.bed
}

func (g *petSleepWithOwner) Start(m *mob.Mob) {
	g.lying = false
	m.Navigation().MoveTo(m, g.bed.Vec3Middle().Add(mgl64.Vec3{0, 0.5625}), g.speed)
}

func (g *petSleepWithOwner) Tick(m *mob.Mob) {
	target := g.bed.Vec3Middle().Add(mgl64.Vec3{0, 0.5625})
	d := target.Sub(m.Pos())
	if d[0]*d[0]+d[2]*d[2] <= (g.goal+0.5)*(g.goal+0.5) {
		if !g.lying {
			g.lying = true
			m.StopMovement()
			m.SetActorFlag(protocol.EntityDataFlagSleeping, true)
		}
		return
	}
	if m.Navigation().Done() {
		m.Navigation().MoveTo(m, target, g.speed)
	}
}

func (g *petSleepWithOwner) Stop(m *mob.Mob) {
	if g.lying {
		m.SetActorFlag(protocol.EntityDataFlagSleeping, false)
		if o, ok := g.owner.Entity(m.Tx()); ok {
			if p, ok := o.(*player.Player); ok {
				if _, sleeping := p.Sleeping(); !sleeping {
					if w, ok := mob.ComponentOf[*tame.OnWake](m); ok {
						w.Fire(m, p)
					}
				}
			}
		}
	}
	g.lying, g.owner = false, nil
}

// ---- behavior.ocelot_sit_on_block ----

// ocelotSitOnBlock is minecraft:behavior.ocelot_sit_on_block (java
// CatSitOnBlockGoal): every 10-20 s a cat that is not sitting looks for a
// chest that is not open, a lit furnace or a bed within 8 blocks (2 up or
// down), walks onto it at speed_multiplier and sits there for up to a
// minute.
//
//	speed_multiplier 1. Flags move|jump.
type ocelotSitOnBlock struct {
	mob.GoalBase
	speed   float64
	next    int64
	target  cube.Pos
	sitting bool
	ticks   int
}

func newOcelotSitOnBlock(spec mob.GoalSpec) (mob.Goal, error) {
	v := struct {
		Speed *float64 `json:"speed_multiplier"`
	}{}
	if err := spec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%v: %w", spec.Key, err)
	}
	g := &ocelotSitOnBlock{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagJump)}, speed: 1}
	setF(&g.speed, v.Speed)
	return g, nil
}

// sitBlock reports whether a cat sits on b.
func sitBlock(tx *world.Tx, pos cube.Pos) bool {
	switch b := tx.Block(pos).(type) {
	case block.Chest:
		return !tx.Block(pos.Side(cube.FaceUp)).Model().FaceSolid(pos.Side(cube.FaceUp), cube.FaceDown, tx)
	case block.Furnace:
		return b.Lit
	case block.Bed:
		return b.Sleeper == nil
	}
	return false
}

func (g *ocelotSitOnBlock) CanUse(m *mob.Mob) bool {
	if m.Ticks() < g.next || orderedToSit(m) || m.Riding() {
		return false
	}
	g.next = m.Ticks() + 200 + int64(m.Rand().IntN(200))
	base := cube.PosFromVec3(m.Pos())
	best, bestDist := cube.Pos{}, -1.0
	for dy := -2; dy <= 2; dy++ {
		for dx := -8; dx <= 8; dx++ {
			for dz := -8; dz <= 8; dz++ {
				p := base.Add(cube.Pos{dx, dy, dz})
				if !sitBlock(m.Tx(), p) {
					continue
				}
				if d := p.Vec3Centre().Sub(m.Pos()).LenSqr(); bestDist < 0 || d < bestDist {
					best, bestDist = p, d
				}
			}
		}
	}
	if bestDist < 0 {
		return false
	}
	g.target = best
	return true
}

func (g *ocelotSitOnBlock) CanContinueToUse(m *mob.Mob) bool {
	return g.ticks < 1200 && sitBlock(m.Tx(), g.target)
}

func (g *ocelotSitOnBlock) top() mgl64.Vec3 {
	return mgl64.Vec3{float64(g.target[0]) + 0.5, float64(g.target[1]) + 1, float64(g.target[2]) + 0.5}
}

func (g *ocelotSitOnBlock) Start(m *mob.Mob) {
	g.ticks, g.sitting = 0, false
	m.Navigation().MoveTo(m, g.top(), g.speed)
}

func (g *ocelotSitOnBlock) Tick(m *mob.Mob) {
	g.ticks++
	d := g.top().Sub(m.Pos())
	if d[0]*d[0]+d[2]*d[2] < 0.5 && math.Abs(d[1]) < 0.6 {
		if !g.sitting {
			g.sitting = true
			m.StopMovement()
			m.UpdateMeta(func(meta *mob.Meta) { meta.Sitting = true })
		}
		return
	}
	if g.sitting {
		g.sitting = false
		m.UpdateMeta(func(meta *mob.Meta) { meta.Sitting = false })
	}
	if g.ticks%40 == 0 || m.Navigation().Done() {
		m.Navigation().MoveTo(m, g.top(), g.speed)
	}
}

func (g *ocelotSitOnBlock) Stop(m *mob.Mob) {
	if g.sitting {
		ordered := false
		if s, ok := tame.SittableOf(m); ok {
			ordered = s.Sitting()
		}
		m.UpdateMeta(func(meta *mob.Meta) { meta.Sitting = ordered })
	}
	g.sitting = false
	m.Navigation().Stop(m)
}

// orderedToSit reports whether m sits because its owner told it to
// (sittable); without sittable, whether it sits at all.
func orderedToSit(m *mob.Mob) bool {
	if s, ok := tame.SittableOf(m); ok {
		return s.Sitting()
	}
	return m.Sitting()
}
