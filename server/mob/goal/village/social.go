//go:build crossfly_full

package village

import (
	"encoding/json"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	ccore "github.com/df-mc/dragonfly/server/mob/component/core"
	vc "github.com/df-mc/dragonfly/server/mob/component/village"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type jsonRaw = json.RawMessage

// actorEvent is an entity action shown as a Bedrock actor event.
type actorEvent uint8

func (a actorEvent) ActorEvent() uint8 { return uint8(a) }

// EntityAction ...
func (actorEvent) EntityAction() {}

// Actor events (gophertunnel packet.ActorEvent*).
const (
	eventStartOfferFlower actorEvent = 19
	eventStopOfferFlower  actorEvent = 20
	eventLoveHearts       actorEvent = 21
	eventDrinkPotion      actorEvent = 29
	eventDrinkMilk        actorEvent = 78
)

func broadcast(m *mob.Mob, a world.EntityAction) {
	for _, v := range m.Tx().Viewers(m.Position()) {
		v.ViewEntityAction(m, a)
	}
}

// shareItems is minecraft:behavior.share_items (BDS ShareItemsGoal): take a
// surplus item (crafting wheat into bread) and throw it to a nearby mob of
// entity_types that wants more of it.
type shareItems struct {
	mob.GoalBase
	types    filterList
	speed    float64
	maxDist  float64
	radiusSq float64

	target *world.EntityHandle
	item   item.Stack
	timer  int
	repath int
}

func newShareItems(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Types   filterList `json:"entity_types"`
		Speed   *float64   `json:"speed_multiplier"`
		MaxDist *float64   `json:"max_dist"`
		Radius  *float64   `json:"goal_radius"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	g := &shareItems{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, types: j.Types, speed: 1, maxDist: 1, radiusSq: 0.25}
	if j.Speed != nil {
		g.speed = *j.Speed
	}
	if j.MaxDist != nil {
		g.maxDist = float64(int(*j.MaxDist))
	}
	if j.Radius != nil {
		g.radiusSq = *j.Radius * *j.Radius
	}
	return g, nil
}

func (g *shareItems) CanUse(m *mob.Mob) bool {
	sh, ok := vc.ShareablesOf(m)
	if !ok || !hasNav(m) {
		return false
	}
	for _, s := range vc.Inventory(m).Slots() {
		if s.Empty() {
			continue
		}
		out, take := sh.Surplus(m, s)
		if take <= 0 || out.Empty() {
			continue
		}
		for _, e := range nearby(m, g.maxDist, g.maxDist) {
			o, ok := mob.Of(e)
			if !ok || o.Dead() || !g.types.any(m, o) {
				continue
			}
			osh, ok := vc.ShareablesOf(o)
			if !ok || osh.WantsMore(o, out) <= 0 {
				continue
			}
			e, _ := sh.Entry(s)
			vc.RemoveItems(m, e.Item, take)
			g.item, g.target = out, o.H()
			return true
		}
	}
	return false
}

func (g *shareItems) Start(m *mob.Mob) {
	g.timer, g.repath = 20, 10
	if t, ok := g.target.Entity(m.Tx()); ok {
		m.Navigation().MoveToEntity(m, t, g.speed)
	}
}

func (g *shareItems) CanContinueToUse(m *mob.Mob) bool {
	t, ok := g.target.Entity(m.Tx())
	return g.timer >= -20 && ok && alive(m.Tx(), t) && distSq(t.Position(), m.Position()) <= 16+g.maxDist*g.maxDist
}

func (g *shareItems) Tick(m *mob.Mob) {
	t, ok := g.target.Entity(m.Tx())
	if !ok {
		return
	}
	lookAt(m, t, 30, 30)
	d := distSq(t.Position(), m.Position())
	if d >= g.radiusSq {
		if g.repath--; g.repath <= 0 {
			g.repath = 4 + m.Rand().IntN(7)
			if !m.Navigation().MoveToEntity(m, t, g.speed) {
				g.repath += 15
			}
		}
	}
	if g.timer > 0 {
		g.timer--
		return
	}
	if !g.item.Empty() {
		vc.DropTowards(m, g.item, t.Position())
		g.item = item.Stack{}
	}
	g.timer--
}

func (g *shareItems) Stop(m *mob.Mob) {
	if !g.item.Empty() {
		_, _ = vc.Inventory(m).AddItem(g.item)
		g.item = item.Stack{}
	}
	m.Navigation().Stop(m)
}

// offerFlower is minecraft:behavior.offer_flower (BDS OfferFlowerGoal):
// with chance_to_start per tick an iron golem holds out a poppy to a mob
// with take_flower within search_area for max_offer_flower_duration.
type offerFlower struct {
	mob.GoalBase
	duration int
	chance   float64
	filter   mob.Filter
	area     mgl64.Vec3
	target   *world.EntityHandle
	end      int64
}

type offerKey struct{}

// offering returns the ticks an offering golem has left.
func offering(m *mob.Mob) (int64, bool) {
	v, ok := m.Data(offerKey{})
	if !ok {
		return 0, false
	}
	return v.(int64) - m.Ticks(), true
}

func newOfferFlower(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Duration *float64    `json:"max_offer_flower_duration"`
		Chance   *float64    `json:"chance_to_start"`
		Filters  jsonRaw     `json:"filters"`
		Area     *[3]float64 `json:"search_area"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	g := &offerFlower{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, duration: 400, chance: 0.000125,
		filter: compile(j.Filters), area: mgl64.Vec3{6, 2, 6}}
	if j.Duration != nil {
		g.duration = int(*j.Duration * 20)
	}
	if j.Chance != nil {
		g.chance = *j.Chance
	}
	if j.Area != nil {
		g.area = mgl64.Vec3(*j.Area)
	}
	return g, nil
}

func (g *offerFlower) CanUse(m *mob.Mob) bool {
	if m.Rand().Float64() > g.chance || !pass(g.filter, m) {
		return false
	}
	for _, e := range nearby(m, g.area[0], g.area[1]) {
		if o, ok := mob.Of(e); ok && o.HasComponent("minecraft:behavior.take_flower") {
			g.target = o.H()
			return true
		}
	}
	return false
}

func (g *offerFlower) Start(m *mob.Mob) {
	g.end = m.Ticks() + int64(g.duration)
	m.SetData(offerKey{}, g.end)
	_, off := m.HeldItems()
	m.SetHeldItems(item.NewStack(flowerItem(), 1), off)
	broadcast(m, eventStartOfferFlower)
}

func (g *offerFlower) CanContinueToUse(m *mob.Mob) bool {
	_, offer := offering(m)
	t, ok := g.target.Entity(m.Tx())
	return offer && ok && alive(m.Tx(), t) && m.Ticks() < g.end
}

func (g *offerFlower) Tick(m *mob.Mob) {
	if t, ok := g.target.Entity(m.Tx()); ok {
		lookAt(m, t, 30, 30)
	}
}

func (g *offerFlower) Stop(m *mob.Mob) {
	stopOffer(m)
	g.target = nil
}

func stopOffer(m *mob.Mob) {
	m.SetData(offerKey{}, nil)
	_, off := m.HeldItems()
	m.SetHeldItems(item.Stack{}, off)
	broadcast(m, eventStopOfferFlower)
}

// takeFlower is minecraft:behavior.take_flower (BDS TakeFlowerGoal): a mob
// near an offering golem waits a random time, walks up and takes the flower
// (it disappears; nothing is transferred).
type takeFlower struct {
	mob.GoalBase
	speed            float64
	filter           mob.Filter
	minWait, maxWait int
	area             mgl64.Vec3
	minDistSq        float64
	onTake           ccore.Trigger
	golem            *world.EntityHandle
	takeAt           int64
	approaching      bool
}

func newTakeFlower(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Speed   *float64      `json:"speed_multiplier"`
		Filters jsonRaw       `json:"filters"`
		MinWait *float64      `json:"min_wait_time"`
		MaxWait *float64      `json:"max_wait_time"`
		Area    *[3]float64   `json:"search_area"`
		MinDist *float64      `json:"min_distance_to_target"`
		OnTake  ccore.Trigger `json:"on_take_flower"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	g := &takeFlower{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, speed: 0.5, filter: compile(j.Filters),
		minWait: 80, maxWait: 400, area: mgl64.Vec3{6, 2, 6}, minDistSq: 4, onTake: j.OnTake}
	if j.Speed != nil {
		g.speed = *j.Speed
	}
	if j.MinWait != nil {
		g.minWait = int(*j.MinWait * 20)
	}
	if j.MaxWait != nil {
		g.maxWait = int(*j.MaxWait * 20)
	}
	if j.Area != nil {
		g.area = mgl64.Vec3(*j.Area)
	}
	if j.MinDist != nil {
		g.minDistSq = *j.MinDist * *j.MinDist
	}
	return g, nil
}

func (g *takeFlower) CanUse(m *mob.Mob) bool {
	if !hasNav(m) || !pass(g.filter, m) {
		return false
	}
	for _, e := range nearby(m, g.area[0], g.area[1]) {
		if o, ok := mob.Of(e); ok {
			if _, offer := offering(o); offer {
				g.golem = o.H()
				return true
			}
		}
	}
	return false
}

func (g *takeFlower) Start(m *mob.Mob) {
	gm, ok := mob.OfHandle(m.Tx(), g.golem)
	if !ok {
		return
	}
	left, _ := offering(gm)
	g.takeAt = left - int64(randInt(m, g.minWait, g.maxWait))
	g.approaching = false
	gm.Navigation().Stop(gm)
}

func (g *takeFlower) CanContinueToUse(m *mob.Mob) bool {
	gm, ok := mob.OfHandle(m.Tx(), g.golem)
	if !ok {
		return false
	}
	_, offer := offering(gm)
	return offer
}

func (g *takeFlower) Tick(m *mob.Mob) {
	gm, ok := mob.OfHandle(m.Tx(), g.golem)
	if !ok {
		return
	}
	lookAt(m, gm, 30, 30)
	left, _ := offering(gm)
	if !g.approaching && left <= g.takeAt {
		g.approaching = true
		m.Navigation().MoveToEntity(m, gm, g.speed)
	}
	if g.approaching && distSq(gm.Position(), m.Position()) < g.minDistSq+1 {
		stopOffer(gm)
		m.Navigation().Stop(m)
		g.onTake.Fire(m, nil)
	}
}

func (g *takeFlower) Stop(m *mob.Mob) { m.Navigation().Stop(m) }

// celebrateSurvive is minecraft:behavior.celebrate_survive (BDS
// VillagerCelebrationGoal): after a won raid villagers set off fireworks
// (fireworks_interval seconds apart, when the sky is clear above them) for
// duration seconds, then fire on_celebration_end_event.
type celebrateSurvive struct {
	mob.GoalBase
	duration int
	interval rangeF
	onEnd    ccore.Trigger
	end      int64
	next     int64
}

func newCelebrateSurvive(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Duration *float64      `json:"duration"`
		Interval *rangeF       `json:"fireworks_interval"`
		OnEnd    ccore.Trigger `json:"on_celebration_end_event"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	g := &celebrateSurvive{GoalBase: mob.GoalBase{Control: spec.FlagsOr(0)}, duration: 600, interval: rangeF{Min: 0, Max: 20}, onEnd: j.OnEnd}
	if j.Duration != nil {
		g.duration = int(*j.Duration * 20)
	}
	if j.Interval != nil {
		g.interval = *j.Interval
	}
	return g, nil
}

func (g *celebrateSurvive) CanUse(*mob.Mob) bool { return true }

func (g *celebrateSurvive) CanContinueToUse(m *mob.Mob) bool { return m.Ticks() < g.end }

func (g *celebrateSurvive) Start(m *mob.Mob) {
	g.end = m.Ticks() + int64(g.duration)
	vc.SetCelebrating(m, true, false)
	g.next = m.Ticks() + int64(randIn(m, g.interval)*20)
}

func (g *celebrateSurvive) Tick(m *mob.Mob) {
	if m.Ticks() <= g.next || !canSeeSky(m.Tx(), blockPos(m.Position())) {
		return
	}
	g.next = m.Ticks() + int64(randIn(m, g.interval)*20)
	cs := item.Colours()
	fw := item.Firework{Duration: time.Second, Explosions: []item.FireworkExplosion{{
		Shape: item.FireworkShapeSmallSphere(), Colour: cs[m.Rand().IntN(len(cs))], Fade: cs[m.Rand().IntN(len(cs))],
	}}}
	m.Tx().AddEntity(entity.NewFirework(world.EntitySpawnOpts{Position: m.Position().Add(mgl64.Vec3{0, m.Meta().Height})}, fw))
}

func (g *celebrateSurvive) Stop(m *mob.Mob) {
	vc.SetCelebrating(m, false, false)
	g.onEnd.Fire(m, nil)
}

func flowerItem() world.Item {
	if it, ok := world.ItemByName("minecraft:poppy", 0); ok {
		return it
	}
	it, _ := world.ItemByName("minecraft:red_flower", 0)
	return it
}
