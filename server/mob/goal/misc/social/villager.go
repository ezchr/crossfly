//go:build crossfly_full

package social

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/component/village"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// loveKey keys a villager's love partner (BDS Actor::lovePartnerId,
// mob.Data).
type loveKey struct{}

type love struct {
	partner   *world.EntityHandle
	initiator bool
}

func loveOf(m *mob.Mob) (love, bool) {
	v, ok := m.Data(loveKey{})
	if !ok {
		return love{}, false
	}
	return v.(love), true
}

// setInLove is Actor::setInLove(partner): nil clears it. Meta.InLove shows
// the hearts flag.
func setInLove(m *mob.Mob, partner *mob.Mob, initiator bool) {
	if partner == nil {
		m.SetData(loveKey{}, nil)
		m.UpdateMeta(func(meta *mob.Meta) { meta.InLove = false })
		return
	}
	m.SetData(loveKey{}, love{partner: partner.H(), initiator: initiator})
	m.UpdateMeta(func(meta *mob.Meta) { meta.InLove = true })
}

func partnerOf(m *mob.Mob) (*mob.Mob, bool) {
	l, ok := loveOf(m)
	if !ok {
		return nil, false
	}
	return mob.OfHandle(m.Tx(), l.partner)
}

func isVillager(m *mob.Mob) bool {
	return m.HasFamily("villager") || strings.HasPrefix(m.Identifier(), "minecraft:villager")
}

// needMoreVillagers is Village::checkNeedMoreVillagers through the mob's
// village (component/village). Without a village it falls back to more bed
// heads within 16 blocks (+/-4 y) than villagers within 32.
func needMoreVillagers(m *mob.Mob) bool {
	if v, ok := village.VillageOf(m); ok {
		return v.NeedsMoreVillagers()
	}
	beds := 0
	kit.FindBlock(m, 16, 4, "random", func(_ cube.Pos, b world.Block) bool {
		if bed, ok := b.(block.Bed); ok && bed.Head {
			beds++
		}
		return false
	})
	if beds == 0 {
		return false
	}
	villagers := 0
	for _, e := range kit.Around(m, 32, 16) {
		if om, ok := mob.Of(e); ok && isVillager(om) && !om.Dead() {
			villagers++
		}
	}
	return villagers+1 <= beds
}

// makeLove is minecraft:behavior.make_love (BDS MakeLoveGoal): an adult
// villager, with a 1/500 chance per tick while the village needs more
// villagers, picks the nearest willing villager without a partner within
// 8/3/8 blocks that it can path to (speed 0.6), and both fall in love. For
// 300 ticks it looks at the partner (love hearts 1 in 35 ticks); when the
// time is up and they are within 1.5 blocks a baby villager is born
// (entity_born, persistent).
//
// The village check uses component/village (beds near the mob stand in for
// a village when the mob has none). The partner must be willing
// (VillagerBase::isWillingToBreed: 3 bread or 12 carrots, potatoes or
// beetroots in its inventory), and both use that food up when the baby is
// born (consumeLoveFood).
type makeLove struct {
	mob.GoalBase
	timer  int
	repath int
}

func newMakeLove(spec mob.GoalSpec) (mob.Goal, error) {
	return &makeLove{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}}, nil
}

// CanUse ...
func (g *makeLove) CanUse(m *mob.Mob) bool {
	if !m.HasNavigation() || m.Baby() {
		return false
	}
	if _, ok := loveOf(m); ok {
		return false
	}
	if m.Rand().IntN(500) != 0 || !needMoreVillagers(m) {
		return false
	}
	mate := findMate(m)
	if mate == nil {
		return false
	}
	setInLove(m, mate, true)
	setInLove(mate, m, false)
	mate.Navigation().Stop(mate)
	return true
}

// findMate is MakeLoveGoal::_findMate.
func findMate(m *mob.Mob) *mob.Mob {
	var best *mob.Mob
	bestD := 1e300
	b := m.BBox()
	box := cube.Box(b.Min()[0]-8, b.Min()[1]-3, b.Min()[2]-8, b.Max()[0]+8, b.Max()[1]+3, b.Max()[2]+8)
	for e := range m.Tx().EntitiesWithin(box) {
		om, ok := mob.Of(e)
		if !ok || om.H() == m.H() || om.Dead() || om.Baby() || !isVillager(om) {
			continue
		}
		if _, ok := loveOf(om); ok || !willingToBreed(om) {
			continue
		}
		d := kit.DistSq(om.Position(), m.Position())
		if d > bestD {
			continue
		}
		if m.Navigation().CreatePathToEntity(m, om) == nil {
			continue
		}
		best, bestD = om, d
	}
	if best != nil {
		m.Navigation().MoveToEntity(m, best, 0.6)
	}
	return best
}

// CanContinueToUse ...
func (g *makeLove) CanContinueToUse(m *mob.Mob) bool {
	if g.timer < 0 || m.Baby() {
		return false
	}
	if _, ok := loveOf(m); !ok {
		return false
	}
	if p, ok := partnerOf(m); !ok || !willingToBreed(p) {
		return false
	}
	return needMoreVillagers(m)
}

// breedingStackIndex is VillagerBase::getBreedingStackIndex: the first
// inventory slot holding at least 3 bread or 12 carrots, potatoes or
// beetroots, -1 without one.
func breedingStackIndex(m *mob.Mob) int {
	inv := village.Inventory(m)
	for i, s := range inv.Slots() {
		if loveFoodCost(s) > 0 && s.Count() >= loveFoodCost(s) {
			return i
		}
	}
	return -1
}

// loveFoodCost is how much of s a villager uses up to breed: 3 bread or 12
// carrots, potatoes or beetroots (VillagerBase::consumeLoveFood), 0 for
// anything else.
func loveFoodCost(s item.Stack) int {
	if s.Empty() {
		return 0
	}
	switch s.Item().(type) {
	case item.Bread:
		return 3
	case block.Carrot, block.Potato, item.Beetroot:
		return 12
	}
	return 0
}

// willingToBreed is VillagerBase::isWillingToBreed(true): the villager has
// its breeding food.
func willingToBreed(m *mob.Mob) bool { return breedingStackIndex(m) >= 0 }

// consumeLoveFood is VillagerBase::consumeLoveFood.
func consumeLoveFood(m *mob.Mob) {
	i := breedingStackIndex(m)
	if i < 0 {
		return
	}
	inv := village.Inventory(m)
	s, _ := inv.Item(i)
	_ = inv.SetItem(i, s.Grow(-loveFoodCost(s)))
}

// Start ...
func (g *makeLove) Start(*mob.Mob) { g.timer, g.repath = 300, 0 }

// Stop ...
func (g *makeLove) Stop(m *mob.Mob) {
	if p, ok := partnerOf(m); ok {
		setInLove(p, nil, false)
	}
	setInLove(m, nil, false)
	kit.Stop(m)
}

// Tick ...
func (g *makeLove) Tick(m *mob.Mob) {
	g.timer--
	p, ok := partnerOf(m)
	if !ok || p.Dead() {
		return
	}
	m.LookControl().SetLookAt(p.EyePosition(), 10, 30)
	close := kit.DistSq(m.Position(), p.Position()) <= 2.25
	if !close {
		if g.repath--; g.repath <= 0 {
			g.repath = 10
			m.Navigation().MoveToEntity(m, p, 0.6)
		}
	}
	if g.timer == 0 {
		if _, inLove := loveOf(p); inLove && close {
			breed(m, p)
		}
	}
	if m.Rand().IntN(35) == 0 {
		kit.Show(m, core.LoveHeartsAction{})
	}
}

// breed is MakeLoveGoal::_breed.
func breed(m, p *mob.Mob) {
	baby, err := mob.Spawn(m.Tx(), m.Identifier(), m.Position(), mob.SpawnOptions{Event: mob.EventBorn, Rotation: m.Rotation()})
	if err == nil {
		baby.SetPersistent(true)
	}
	consumeLoveFood(m)
	consumeLoveFood(p)
	setInLove(p, nil, false)
	setInLove(m, nil, false)
}

// AppendDebugInfo ...
func (g *makeLove) AppendDebugInfo(b *strings.Builder) { fmt.Fprintf(b, "MakeLove[%d]", g.timer) }

// receiveLove is minecraft:behavior.receive_love (BDS ReceiveLoveGoal): the
// villager a make_love villager chose stays with it (looks at it and walks
// to it at 0.6) while it is in love.
type receiveLove struct {
	mob.GoalBase
	repath int
}

func newReceiveLove(spec mob.GoalSpec) (mob.Goal, error) {
	return &receiveLove{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}}, nil
}

// CanUse ...
func (g *receiveLove) CanUse(m *mob.Mob) bool {
	l, ok := loveOf(m)
	return ok && !l.initiator
}

// CanContinueToUse ...
func (g *receiveLove) CanContinueToUse(m *mob.Mob) bool {
	_, ok := loveOf(m)
	return ok
}

// Start ...
func (g *receiveLove) Start(*mob.Mob) { g.repath = 0 }

// Stop ...
func (g *receiveLove) Stop(m *mob.Mob) { kit.Stop(m) }

// Tick ...
func (g *receiveLove) Tick(m *mob.Mob) {
	p, ok := partnerOf(m)
	if !ok {
		return
	}
	m.LookControl().SetLookAt(p.EyePosition(), 10, 30)
	if kit.DistSq(m.Position(), p.Position()) > 2.25 {
		if g.repath--; g.repath <= 0 {
			g.repath = 10
			m.Navigation().MoveToEntity(m, p, 0.6)
		}
	}
}

// AppendDebugInfo ...
func (g *receiveLove) AppendDebugInfo(b *strings.Builder) { b.WriteString("ReceiveLove") }

// play is minecraft:behavior.play (BDS PlayGoal, baby villagers): with
// chance_to_start per tick the mob plays for up to
// max_play_duration_seconds: it chases a friend found in friend_search_area
// that passes friend_types (any other baby of its type when there are
// none) to within follow_distance, then runs to a random spot within
// random_pos_search_range/height and chases again; without a friend it
// only runs around.
//
//	speed_multiplier           1.0
//	chance_to_start            0.005 (BDS +64)
//	max_play_duration_seconds  50 (1000 ticks)
//	friend_search_area         [6, 3, 6]
//	follow_distance            2
//	random_pos_search_range    16, random_pos_search_height 3
//
// Control flag move (BDS SetRotation(1)).
type play struct {
	mob.GoalBase
	speed          float64
	chance         float64
	duration       int
	area           mgl64.Vec3
	follow         float64
	posRange, posH int
	friends        []descriptor
	left, repath   int
	friend         *world.EntityHandle
	chasing        bool
	dest           mgl64.Vec3
}

func newPlay(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Speed     *float64        `json:"speed_multiplier"`
		Chance    *float64        `json:"chance_to_start"`
		Duration  *float64        `json:"max_play_duration_seconds"`
		Area      []float64       `json:"friend_search_area"`
		Follow    *float64        `json:"follow_distance"`
		PosRange  *int            `json:"random_pos_search_range"`
		PosHeight *int            `json:"random_pos_search_height"`
		Friends   json.RawMessage `json:"friend_types"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	g := &play{
		GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)},
		speed:    kit.Or(v.Speed, 1), chance: kit.Or(v.Chance, 0.005), duration: kit.Ticks(kit.Or(v.Duration, 50)),
		area: mgl64.Vec3{6, 3, 6}, follow: kit.Or(v.Follow, 2), posRange: kit.Or(v.PosRange, 16), posH: kit.Or(v.PosHeight, 3),
	}
	if len(v.Area) == 3 {
		g.area = mgl64.Vec3{v.Area[0], v.Area[1], v.Area[2]}
	}
	var err error
	if g.friends, err = parseDescriptors(v.Friends, 1e9); err != nil {
		return nil, fmt.Errorf("%v: friend_types: %w", spec.Key, err)
	}
	return g, nil
}

func (g *play) findFriend(m *mob.Mob) world.Entity {
	for _, e := range kit.Around(m, max(g.area[0], g.area[2]), g.area[1]) {
		om, ok := mob.Of(e)
		if !ok || om.Dead() {
			continue
		}
		if len(g.friends) == 0 {
			if om.Identifier() == m.Identifier() && om.Baby() {
				return e
			}
			continue
		}
		if match(m, g.friends, e) {
			return e
		}
	}
	return nil
}

// CanUse ...
func (g *play) CanUse(m *mob.Mob) bool {
	return m.HasNavigation() && m.Rand().Float64() < g.chance
}

// CanContinueToUse ...
func (g *play) CanContinueToUse(*mob.Mob) bool { return g.left > 0 }

// Start ...
func (g *play) Start(m *mob.Mob) {
	g.left, g.repath = g.duration, 0
	g.friend = nil
	if f := g.findFriend(m); f != nil {
		g.friend, g.chasing = f.H(), true
	} else {
		g.runOff(m)
	}
}

func (g *play) runOff(m *mob.Mob) {
	g.chasing = false
	if p, ok := kit.RandomPos(m, g.posRange, g.posH); ok {
		g.dest = p
		kit.MoveTo(m, p, g.speed)
	}
}

// Tick ...
func (g *play) Tick(m *mob.Mob) {
	g.left--
	f, ok := entityOf(m, g.friend)
	if !ok || !kit.Alive(f) {
		g.friend = nil
	}
	switch {
	case g.chasing && g.friend != nil:
		if kit.DistSq(m.Position(), f.Position()) <= g.follow*g.follow {
			g.runOff(m)
			return
		}
		if g.repath--; g.repath <= 0 {
			g.repath = 10
			m.Navigation().MoveToEntity(m, f, g.speed)
		}
	case m.Navigation().Done():
		if g.friend != nil {
			g.chasing = true
			return
		}
		g.runOff(m)
	}
}

// Stop ...
func (g *play) Stop(m *mob.Mob) { kit.Stop(m); g.friend = nil }

// AppendDebugInfo ...
func (g *play) AppendDebugInfo(b *strings.Builder) { b.WriteString("Play") }

// alarmKey marks a villager told to hide (bell, raid).
type alarmKey struct{}

// Alarm makes the villagers with behavior.hide within radius of pos hide
// (BDS: a rung bell or a raid starts HideComponent's reaction). Dragonfly
// has no raids and does not tell mobs about bells; the bell use handler can
// call this.
func Alarm(tx *world.Tx, pos mgl64.Vec3, radius float64) {
	for e := range tx.EntitiesWithin(cube.Box(pos[0]-radius, pos[1]-radius, pos[2]-radius, pos[0]+radius, pos[1]+radius, pos[2]+radius)) {
		if m, ok := mob.Of(e); ok && m.HasComponent("minecraft:behavior.hide") {
			m.SetData(alarmKey{}, true)
		}
	}
}

// hide is minecraft:behavior.hide (BDS HideGoal on MoveToPOIGoal, villager
// v2): when alarmed (Alarm, a raid in progress in the mob's village, or -
// without a village - a raider mob of family illager, ravager or witch with
// a target within 16 blocks, checked every 20 ticks) the villager walks at
// speed_multiplier to its bed POI (owned, else the village's closest; the
// nearest bed within 16/4 blocks without a village) and stays there for
// duration seconds.
//
//	duration          1 s
//	speed_multiplier  1.0
//	poi_type          bed (the only kind supported)
//
// Control flags move|look.
type hide struct {
	mob.GoalBase
	duration int
	speed    float64

	bed     cube.Pos
	left    int
	check   int
	arrived bool
	repath  int
}

func newHide(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Duration *float64 `json:"duration"`
		Speed    *float64 `json:"speed_multiplier"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	return &hide{
		GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)},
		duration: kit.Ticks(kit.Or(v.Duration, 1)), speed: kit.Or(v.Speed, 1),
	}, nil
}

func (g *hide) alarmed(m *mob.Mob) bool {
	if v, ok := m.Data(alarmKey{}); ok && v == true {
		return true
	}
	if v, ok := village.VillageOf(m); ok {
		if r, ok := v.Raid(); ok && r.State >= village.RaidFindSpawn && r.State <= village.RaidInPlay {
			return true
		}
	}
	if g.check--; g.check > 0 {
		return false
	}
	g.check = 20
	_, ok := kit.Nearest(m, 16, 8, func(e world.Entity) bool {
		om, ok := mob.Of(e)
		if !ok || om.Dead() || !(om.HasFamily("illager") || om.HasFamily("ravager") || om.HasFamily("witch")) {
			return false
		}
		_, hasTarget := om.Target()
		return hasTarget
	})
	return ok
}

// CanUse ...
func (g *hide) CanUse(m *mob.Mob) bool {
	if !m.HasNavigation() || !g.alarmed(m) {
		return false
	}
	p, ok := villageBed(m)
	if !ok {
		p, ok = kit.FindBlock(m, 16, 4, "nearest", func(_ cube.Pos, b world.Block) bool {
			bed, ok := b.(block.Bed)
			return ok && bed.Head
		})
	}
	if !ok {
		m.SetData(alarmKey{}, nil)
		return false
	}
	g.bed = p
	return true
}

// villageBed is the bed POI the villager owns, else the village's closest
// one (HideGoal::_getOwnedPOI on MoveToPOIGoal).
func villageBed(m *mob.Mob) (cube.Pos, bool) {
	v, ok := village.VillageOf(m)
	if !ok {
		return cube.Pos{}, false
	}
	p, ok := v.OwnedPOI(m.H(), village.POIBed)
	if !ok {
		p, ok = v.ClosestPOI(village.POIBed, m.Position())
	}
	if !ok || !p.Pathable() {
		return cube.Pos{}, false
	}
	return p.Pos, true
}

// CanContinueToUse ...
func (g *hide) CanContinueToUse(m *mob.Mob) bool {
	if _, ok := m.Tx().Block(g.bed).(block.Bed); !ok {
		return false
	}
	return !g.arrived || g.left > 0
}

// Start ...
func (g *hide) Start(m *mob.Mob) {
	g.left, g.arrived, g.repath = g.duration, false, 0
	m.SetData(alarmKey{}, nil)
}

// Tick ...
func (g *hide) Tick(m *mob.Mob) {
	dest := blockCentre(g.bed)
	if !g.arrived && kit.HDistSq(m.Position(), dest) <= 2.25 {
		g.arrived = true
		kit.Stop(m)
	}
	if g.arrived {
		g.left--
		return
	}
	if g.repath--; g.repath <= 0 || m.Navigation().Done() {
		g.repath = 20
		kit.MoveTo(m, dest, g.speed)
	}
}

// Stop ...
func (g *hide) Stop(m *mob.Mob) { kit.Stop(m) }

// Hiding reports whether m reached its hiding place and waits there.
func (g *hide) Hiding() bool { return g.arrived && g.left > 0 }

// AppendDebugInfo ...
func (g *hide) AppendDebugInfo(b *strings.Builder) { b.WriteString("Hide") }
