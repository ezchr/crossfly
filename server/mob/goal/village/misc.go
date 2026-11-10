//go:build crossfly_full

package village

import (
	"encoding/json"
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/mob"
	ccore "github.com/df-mc/dragonfly/server/mob/component/core"
	vc "github.com/df-mc/dragonfly/server/mob/component/village"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/gameevent"
	"github.com/go-gl/mathgl/mgl64"
)

// drinkPotion is minecraft:behavior.drink_potion (BDS DrinkPotionGoal):
// every 6th check, each potion in order with chance and passing filters is
// drunk (32 ticks holding it, slowed by speed_modifier), then its effects
// apply.
type drinkPotion struct {
	mob.GoalBase
	speedMod float64
	potions  []drinkEntry
	counter  int
	end      int64
	potion   potion.Potion
	oldSpeed float64
}

type drinkEntry struct {
	id     int32
	chance float64
	filter mob.Filter
}

const drinkTicks = 32

func newDrinkPotion(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Speed   float64 `json:"speed_modifier"`
		Potions []struct {
			ID      int32   `json:"id"`
			Chance  float64 `json:"chance"`
			Filters jsonRaw `json:"filters"`
		} `json:"potions"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	g := &drinkPotion{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, speedMod: j.Speed}
	for _, p := range j.Potions {
		g.potions = append(g.potions, drinkEntry{id: p.ID, chance: p.Chance, filter: compile(p.Filters)})
	}
	return g, nil
}

func (g *drinkPotion) CanUse(m *mob.Mob) bool {
	if g.counter++; g.counter < 6 {
		return false
	}
	g.counter = 0
	for _, p := range g.potions {
		if m.Rand().Float64() < p.chance && pass(p.filter, m) {
			g.potion = potion.From(p.id)
			return true
		}
	}
	return false
}

func (g *drinkPotion) Start(m *mob.Mob) {
	g.end = m.Ticks() + drinkTicks
	_, off := m.HeldItems()
	m.SetHeldItems(item.NewStack(item.Potion{Type: g.potion}, 1), off)
	m.UpdateMeta(func(meta *mob.Meta) { meta.UsingItem = true })
	a := m.Attributes()
	g.oldSpeed = a.Value(mob.AttrMovement)
	a.SetBase(mob.AttrMovement, g.oldSpeed*(1+g.speedMod))
}

func (g *drinkPotion) CanContinueToUse(m *mob.Mob) bool { return m.Ticks() < g.end+1 }

func (g *drinkPotion) Tick(m *mob.Mob) {
	if m.Ticks() < g.end {
		return
	}
	for _, e := range g.potion.Effects() {
		m.AddEffect(e)
	}
	broadcast(m, eventDrinkPotion)
	g.end = m.Ticks() - 1
}

func (g *drinkPotion) Stop(m *mob.Mob) {
	_, off := m.HeldItems()
	m.SetHeldItems(item.Stack{}, off)
	m.UpdateMeta(func(meta *mob.Meta) { meta.UsingItem = false })
	m.Attributes().SetBase(mob.AttrMovement, g.oldSpeed)
}

// drinkMilk is minecraft:behavior.drink_milk (BDS DrinkMilkGoal): when
// filters pass (at most every cooldown_seconds), drink a milk bucket and
// lose every effect.
type drinkMilk struct {
	mob.GoalBase
	cooldown int64
	filter   mob.Filter
	next     int64
	end      int64
}

func newDrinkMilk(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Cooldown *float64 `json:"cooldown_seconds"`
		Filters  jsonRaw  `json:"filters"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	g := &drinkMilk{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, cooldown: 100, filter: compile(j.Filters)}
	if j.Cooldown != nil {
		g.cooldown = int64(*j.Cooldown * 20)
	}
	return g, nil
}

func (g *drinkMilk) CanUse(m *mob.Mob) bool {
	if m.Ticks() < g.next {
		return false
	}
	g.next = m.Ticks() + g.cooldown
	return pass(g.filter, m)
}

func (g *drinkMilk) Start(m *mob.Mob) {
	g.end = m.Ticks() + drinkTicks
	_, off := m.HeldItems()
	m.SetHeldItems(item.NewStack(item.Bucket{Content: item.MilkBucketContent()}, 1), off)
	m.UpdateMeta(func(meta *mob.Meta) { meta.UsingItem = true })
}

func (g *drinkMilk) CanContinueToUse(m *mob.Mob) bool { return m.Ticks() <= g.end }

func (g *drinkMilk) Tick(m *mob.Mob) {
	if m.Ticks() == g.end {
		for _, e := range m.Effects() {
			m.RemoveEffect(e.Type())
		}
		broadcast(m, eventDrinkMilk)
	}
}

func (g *drinkMilk) Stop(m *mob.Mob) {
	_, off := m.HeldItems()
	m.SetHeldItems(item.Stack{}, off)
	m.UpdateMeta(func(meta *mob.Meta) { meta.UsingItem = false })
}

// followCaravan is minecraft:behavior.follow_caravan (BDS
// FollowCaravanGoal): an unleashed llama joins the tail of a caravan (or a
// leashed llama) within (9, 4, 9), up to entity_count long, and follows it
// 2 blocks behind, speeding up when it falls far behind.
type followCaravan struct {
	mob.GoalBase
	base, speed float64
	count       int
	types       filterList
	timer       int
}

func newFollowCaravan(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Speed *float64   `json:"speed_multiplier"`
		Count *int       `json:"entity_count"`
		Types filterList `json:"entity_types"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	g := &followCaravan{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)}, base: 1, count: 1, types: j.Types}
	if j.Speed != nil {
		g.base = *j.Speed
	}
	if j.Count != nil {
		g.count = *j.Count
	}
	g.speed = g.base
	return g, nil
}

// firstIsLeashed walks the caravan up from m until a leashed mob (depth <
// entity_count - 1).
func (g *followCaravan) firstIsLeashed(m *mob.Mob, depth int) bool {
	for d := depth; d < g.count-1; d++ {
		h, ok := vc.CaravanHead(m)
		if !ok {
			return false
		}
		if vc.Leashed(h) {
			return true
		}
		m = h
	}
	return false
}

func (g *followCaravan) CanUse(m *mob.Mob) bool {
	if vc.Leashed(m) || !hasNav(m) {
		return false
	}
	if _, in := vc.CaravanHead(m); in {
		return false
	}
	var pick *mob.Mob
	cands := nearby(m, 9, 4)
	for _, e := range cands {
		o, ok := mob.Of(e)
		if !ok || vc.HasCaravanTail(o) {
			continue
		}
		if _, in := vc.CaravanHead(o); in && g.types.any(m, o) {
			pick = o
			break
		}
	}
	if pick == nil {
		for _, e := range cands {
			o, ok := mob.Of(e)
			if ok && vc.Leashed(o) && !vc.HasCaravanTail(o) {
				pick = o
				break
			}
		}
	}
	if pick == nil || distSq(pick.Position(), m.Position()) < 4 {
		return false
	}
	if !vc.Leashed(pick) && !g.firstIsLeashed(pick, 1) {
		return false
	}
	vc.JoinCaravan(m, pick)
	return true
}

func (g *followCaravan) CanContinueToUse(m *mob.Mob) bool {
	h, ok := vc.CaravanHead(m)
	if !ok || (!vc.Leashed(h) && !g.firstIsLeashed(m, 0)) {
		return false
	}
	if distSq(h.Position(), m.Position()) > 676 {
		if g.speed <= g.base*1.5 {
			g.speed *= 1.2
			g.timer = 40
			return true
		}
		if g.timer == 0 {
			return false
		}
		g.timer--
	}
	return true
}

func (g *followCaravan) Tick(m *mob.Mob) {
	h, ok := vc.CaravanHead(m)
	if !ok {
		return
	}
	d := h.Position().Sub(m.Position())
	if dist := d.Len(); dist > 2 {
		m.Navigation().MoveTo(m, m.Position().Add(d.Normalize().Mul(math.Max(dist-2, 0))), g.speed)
	} else {
		m.Navigation().Stop(m)
	}
}

func (g *followCaravan) Start(*mob.Mob) { g.speed = g.base }

func (g *followCaravan) Stop(m *mob.Mob) {
	g.speed = g.base
	vc.LeaveCaravan(m)
	m.Navigation().Stop(m)
}

// admireItem is minecraft:behavior.admire_item (BDS AdmireItemGoal): while
// the admire_item component admires an item, hold it in the off hand, set
// the admiring flag and play admire_item_sound; afterwards keep it if it
// belongs in the inventory.
type admireItem struct {
	mob.GoalBase
	sound    string
	interval rangeF
	onStart  ccore.Trigger
	onStop   ccore.Trigger
	next     int64
	item     item.Stack
}

func newAdmireItem(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Sound    string        `json:"admire_item_sound"`
		Interval *rangeF       `json:"sound_interval"`
		OnStart  ccore.Trigger `json:"on_admire_item_start"`
		OnStop   ccore.Trigger `json:"on_admire_item_stop"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	g := &admireItem{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, sound: j.Sound, onStart: j.OnStart, onStop: j.OnStop}
	if j.Interval != nil {
		g.interval = *j.Interval
	}
	return g, nil
}

func (g *admireItem) CanUse(m *mob.Mob) bool {
	a, ok := vc.AdmireOf(m)
	if !ok || vc.HurtByMobWithin(m, a.Cooldown()) {
		return false
	}
	return a.Admiring
}

func (g *admireItem) Start(m *mob.Mob) {
	a, _ := vc.AdmireOf(m)
	main, off := m.HeldItems()
	if !off.Empty() {
		vc.ThrowItems(m, []item.Stack{off})
	}
	g.item = a.Item
	m.SetHeldItems(main, a.Item)
	m.SetActorFlag(vc.FlagAdmiring, true)
	g.onStart.Fire(m, nil)
	g.next = 0
}

func (g *admireItem) Tick(m *mob.Mob) {
	if a, ok := vc.AdmireOf(m); ok && a.Admiring && a.End < m.Ticks() {
		a.Admiring = false
	}
	if g.next < m.Ticks() {
		m.PlaySound(g.sound)
		g.next = m.Ticks() + int64(randIn(m, g.interval)*20)
	}
}

func (g *admireItem) Stop(m *mob.Mob) {
	main, _ := m.HeldItems()
	m.SetHeldItems(main, item.Stack{})
	m.SetActorFlag(vc.FlagAdmiring, false)
	if sh, ok := vc.ShareablesOf(m); ok && !g.item.Empty() {
		if e, ok := sh.Entry(g.item); ok && e.StoredInInventory {
			_, _ = vc.Inventory(m).AddItem(g.item)
		}
	}
	if a, ok := vc.AdmireOf(m); ok {
		a.Stop()
	}
	g.item = item.Stack{}
	g.onStop.Fire(m, nil)
}

// barter is minecraft:behavior.barter (BDS BarterGoal): once, roll the
// barter table and throw the items, unless the piglin was hurt by a mob
// within the cooldown.
type barter struct{ mob.GoalBase }

func newBarter(spec mob.GoalSpec) (mob.Goal, error) {
	return &barter{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}}, nil
}

func (g *barter) CanUse(m *mob.Mob) bool {
	b, ok := vc.BarterOf(m)
	if !ok {
		return false
	}
	if vc.HurtByMobWithin(m, b.Cooldown()) {
		b.Stop()
		return false
	}
	return b.Wants
}

func (g *barter) Tick(m *mob.Mob) {
	b, ok := vc.BarterOf(m)
	if !ok || !b.Wants {
		return
	}
	vc.ThrowItems(m, b.Roll(m))
	b.Stop()
}

// takeBlock is minecraft:behavior.take_block (1.21.50+; BDS 1.21.3
// EndermanTakeBlockGoal): with chance per tick pick up one of blocks at a
// random offset in xz_range/y_range.
type takeBlock struct {
	mob.GoalBase
	chance   float64
	xz, y    [2]int
	blocks   []string
	griefing bool
	onTake   ccore.Trigger
}

type intPair [2]int

func (p *intPair) UnmarshalJSON(b []byte) error {
	var n int
	if json.Unmarshal(b, &n) == nil {
		p[0], p[1] = n, n
		return nil
	}
	var o struct{ Min, Max int }
	if err := json.Unmarshal(b, &o); err != nil {
		return err
	}
	p[0], p[1] = o.Min, o.Max
	return nil
}

func newTakeBlock(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Chance   *float64      `json:"chance"`
		XZ       *intPair      `json:"xz_range"`
		Y        *intPair      `json:"y_range"`
		Blocks   []string      `json:"blocks"`
		Griefing *bool         `json:"affected_by_griefing_rule"`
		OnTake   ccore.Trigger `json:"on_take"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	g := &takeBlock{GoalBase: mob.GoalBase{Control: spec.FlagsOr(0)}, chance: 0.05, xz: [2]int{-2, 2}, y: [2]int{0, 3}, griefing: true, onTake: j.OnTake}
	if j.Chance != nil {
		g.chance = *j.Chance
	}
	if j.XZ != nil {
		g.xz = *j.XZ
	}
	if j.Y != nil {
		g.y = *j.Y
	}
	if j.Griefing != nil {
		g.griefing = *j.Griefing
	}
	for _, b := range j.Blocks {
		g.blocks = append(g.blocks, ccore.ParseItemDescriptor(b).Name)
	}
	return g, nil
}

func offset(m *mob.Mob, r [2]int) int {
	if r[1] <= r[0] {
		return r[0]
	}
	return r[0] + m.Rand().IntN(r[1]-r[0]+1)
}

func (g *takeBlock) CanUse(m *mob.Mob) bool {
	migrateCarried(m)
	if _, ok := Carried(m); ok || (g.griefing && !mobGriefing(m)) {
		return false
	}
	return m.Rand().Float64() < g.chance
}

func (g *takeBlock) CanContinueToUse(*mob.Mob) bool { return false }

func (g *takeBlock) Start(m *mob.Mob) {
	p := blockPos(m.Position()).Add(cube.Pos{offset(m, g.xz), offset(m, g.y), offset(m, g.xz)})
	b, ok := m.Tx().BlockLoaded(p)
	if !ok {
		return
	}
	name, _ := b.EncodeBlock()
	for _, n := range g.blocks {
		if n == name {
			SetCarried(m, b)
			m.Tx().SetBlock(p, nil, nil)
			// EndermanTakeBlockGoal: block_destroy, made by the enderman.
			if gameevent.Active(m.Tx()) {
				gameevent.Post(m.Tx(), gameevent.BlockDestroy, p.Vec3Centre(), m)
			}
			g.onTake.Fire(m, nil)
			return
		}
	}
}

// placeBlock is minecraft:behavior.place_block (1.21.50+; BDS 1.21.3
// EndermanLeaveBlockGoal): with chance per tick put the carried block down
// on a solid block at a random offset. Mobs with randomly_placeable_blocks
// (copper golem statues) place one of those whose filter passes.
type placeBlock struct {
	mob.GoalBase
	chance   float64
	xz, y    [2]int
	griefing bool
	random   []placeable
	onPlace  ccore.Trigger
}

type placeable struct {
	block  world.Block
	filter mob.Filter
}

func newPlaceBlock(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Chance   *float64 `json:"chance"`
		XZ       *intPair `json:"xz_range"`
		Y        *intPair `json:"y_range"`
		Griefing *bool    `json:"affected_by_griefing_rule"`
		Random   []struct {
			Block struct {
				Name   string         `json:"name"`
				States map[string]any `json:"states"`
			} `json:"block"`
			Filter jsonRaw `json:"filter"`
		} `json:"randomly_placeable_blocks"`
		OnPlace ccore.Trigger `json:"on_place"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	g := &placeBlock{GoalBase: mob.GoalBase{Control: spec.FlagsOr(0)}, chance: 0.0005, xz: [2]int{-1, 1}, y: [2]int{0, 2}, griefing: true, onPlace: j.OnPlace}
	if j.Chance != nil {
		g.chance = *j.Chance
	}
	if j.XZ != nil {
		g.xz = *j.XZ
	}
	if j.Y != nil {
		g.y = *j.Y
	}
	if j.Griefing != nil {
		g.griefing = *j.Griefing
	}
	for _, r := range j.Random {
		if b, ok := world.BlockByName(r.Block.Name, r.Block.States); ok {
			g.random = append(g.random, placeable{block: b, filter: compile(r.Filter)})
		}
	}
	return g, nil
}

func (g *placeBlock) CanUse(m *mob.Mob) bool {
	if g.griefing && !mobGriefing(m) {
		return false
	}
	migrateCarried(m)
	if _, ok := Carried(m); !ok && len(g.random) == 0 {
		return false
	}
	return m.Rand().Float64() < g.chance
}

func (g *placeBlock) CanContinueToUse(*mob.Mob) bool { return false }

func (g *placeBlock) Start(m *mob.Mob) {
	b, carried := Carried(m)
	if !carried {
		for _, r := range g.random {
			if pass(r.filter, m) {
				b = r.block
				break
			}
		}
		if b == nil {
			return
		}
	}
	p := blockPos(m.Position()).Add(cube.Pos{offset(m, g.xz), offset(m, g.y), offset(m, g.xz)})
	tx := m.Tx()
	if _, air := tx.Block(p).(block.Air); !air || !vc.Solid(tx, p.Side(cube.FaceDown)) {
		return
	}
	tx.SetBlock(p, b, nil)
	if carried {
		SetCarried(m, nil)
	}
	// EndermanLeaveBlockGoal: block_place, made by the enderman.
	if gameevent.Active(tx) {
		gameevent.Post(tx, gameevent.BlockPlace, p.Vec3Centre(), m)
	}
	g.onPlace.Fire(m, nil)
}

// transportItems is minecraft:behavior.transport_items (copper golem,
// 1.21.110+; not in the BDS 1.21.3 dump, modelled on the JSON and Java's
// TransportItemsBetweenContainers): take up to max_stack_size items from
// the nearest source container within search_distance and put them into
// the nearest destination container holding the same item or with room.
type transportItems struct {
	mob.GoalBase
	sources, dests    []string
	maxStack          int
	dist              [2]int
	maxVisited        int
	initialCD, idleCD int
	cooldown          int
	target            cube.Pos
	toDest            bool
	visited           []cube.Pos
	timer             int
	// interact counts the ticks since the golem reached its container (Java ticksSinceReachingTarget), 0 while it
	// walks; user is the golem as a viewer of the container it has open.
	interact int
	user     *transportUser
}

// transportInteractTicks is how long a copper golem stands at a container, its lid open, before it takes or puts
// the items. Java fallback: no Bedrock data (TransportItemsBetweenContainers.TARGET_INTERACTION_TIME; CopperGolemAi
// opens the container on the first tick).
const transportInteractTicks = 60

// Events of minecraft:behavior.transport_items, fired when the golem starts taking from or putting into a container
// (succeeding or not) and when it is done: the copper golem sets minecraft:chest_interaction from them, which
// Bedrock clients animate.
const (
	transportTakeSucceed  = "minecraft:transport_items.start_take_succeed"
	transportTakeFail     = "minecraft:transport_items.start_take_fail"
	transportPlaceSucceed = "minecraft:transport_items.start_place_succeed"
	transportPlaceFail    = "minecraft:transport_items.start_place_fail"
	transportStop         = "minecraft:transport_items.stop_interaction"
)

// transportUser is a copper golem using a container (Java ContainerUser): added as a viewer of the container, a
// chest opens its lid for Bedrock and Java viewers and plays its sound, and closes it once the golem is done, as for
// a player.
type transportUser struct {
	world.NopViewer
	pos cube.Pos
}

// ViewSlotChange ...
func (*transportUser) ViewSlotChange(int, item.Stack) {}

type carriedItemKey struct{}

func newTransportItems(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Sources []struct {
			Name string `json:"name"`
		} `json:"source_container_types"`
		Dests []struct {
			Name string `json:"name"`
		} `json:"destination_container_types"`
		MaxStack *int     `json:"max_stack_size"`
		Dist     *[2]int  `json:"search_distance"`
		Visited  *int     `json:"max_visited_containers"`
		Initial  *float64 `json:"initial_cooldown"`
		Idle     *float64 `json:"idle_cooldown"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	g := &transportItems{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, maxStack: 16, dist: [2]int{32, 8}, maxVisited: 10, initialCD: 60, idleCD: 140}
	for _, s := range j.Sources {
		g.sources = append(g.sources, s.Name)
	}
	for _, s := range j.Dests {
		g.dests = append(g.dests, s.Name)
	}
	if j.MaxStack != nil {
		g.maxStack = *j.MaxStack
	}
	if j.Dist != nil {
		g.dist = *j.Dist
	}
	if j.Visited != nil {
		g.maxVisited = *j.Visited
	}
	if j.Initial != nil {
		g.initialCD = int(*j.Initial * 20)
	}
	if j.Idle != nil {
		g.idleCD = int(*j.Idle * 20)
	}
	g.cooldown = g.initialCD
	return g, nil
}

func carriedItem(m *mob.Mob) item.Stack {
	main, _ := m.HeldItems()
	return main
}

func containerInv(tx *world.Tx, p cube.Pos) (*inventory.Inventory, string, bool) {
	b, ok := tx.BlockLoaded(p)
	if !ok {
		return nil, "", false
	}
	name, _ := b.EncodeBlock()
	c, ok := b.(interface {
		Inventory(*world.Tx, cube.Pos) *inventory.Inventory
	})
	if !ok {
		return nil, name, false
	}
	return c.Inventory(tx, p), name, true
}

func (g *transportItems) find(m *mob.Mob, names []string, want item.Stack) (cube.Pos, bool) {
	base := blockPos(m.Position())
	best, found := 0.0, false
	var out cube.Pos
	for x := -g.dist[0]; x <= g.dist[0]; x++ {
		for z := -g.dist[0]; z <= g.dist[0]; z++ {
			for y := -g.dist[1]; y <= g.dist[1]; y++ {
				p := base.Add(cube.Pos{x, y, z})
				inv, name, ok := containerInv(m.Tx(), p)
				if !ok || !contains(names, name) || containsPos(g.visited, p) {
					continue
				}
				if want.Empty() {
					if inv.Empty() {
						continue
					}
				} else if !hasRoomFor(inv, want) {
					continue
				}
				if d := distSq(p.Vec3Centre(), m.Position()); !found || d < best {
					best, found, out = d, true, p
				}
			}
		}
	}
	return out, found
}

func contains(l []string, s string) bool {
	for _, v := range l {
		if v == s {
			return true
		}
	}
	return false
}

func containsPos(l []cube.Pos, p cube.Pos) bool {
	for _, v := range l {
		if v == p {
			return true
		}
	}
	return false
}

// hasRoomFor is place_strategy with_matching_or_empty: a container that
// holds the item already, or is empty.
func hasRoomFor(inv *inventory.Inventory, s item.Stack) bool {
	if inv.Empty() {
		return true
	}
	for _, it := range inv.Slots() {
		if !it.Empty() && it.Comparable(s) && it.Count() < it.MaxCount() {
			return true
		}
	}
	return false
}

func (g *transportItems) CanUse(m *mob.Mob) bool {
	if g.cooldown > 0 {
		g.cooldown--
		return false
	}
	if !hasNav(m) {
		return false
	}
	held := carriedItem(m)
	names := g.sources
	if !held.Empty() {
		names = g.dests
	}
	p, ok := g.find(m, names, held)
	if !ok {
		g.visited = nil
		g.cooldown = g.idleCD
		return false
	}
	g.target, g.toDest = p, !held.Empty()
	return true
}

func (g *transportItems) Start(m *mob.Mob) {
	g.timer = 0
	m.Navigation().MoveTo(m, g.target.Vec3Middle(), 1)
}

func (g *transportItems) CanContinueToUse(*mob.Mob) bool { return g.timer < 600 }

func (g *transportItems) Tick(m *mob.Mob) {
	g.timer++
	lookAtPos(m, g.target.Vec3Centre(), 30, maxHeadXRot)
	if g.interact == 0 {
		if distSq(m.Position(), g.target.Vec3Centre()) > 4 {
			if m.Navigation().Done() && g.timer%20 == 0 {
				m.Navigation().MoveTo(m, g.target.Vec3Middle(), 1)
			}
			return
		}
		m.Navigation().Stop(m)
		if !g.startInteraction(m) {
			g.timer = 600
		}
		return
	}
	if g.interact++; g.interact < transportInteractTicks {
		return
	}
	defer g.endInteraction(m)
	inv, _, ok := containerInv(m.Tx(), g.target)
	if !ok {
		g.timer = 600
		return
	}
	held := carriedItem(m)
	if g.toDest {
		// AddItem reports how many it added: the golem keeps the rest.
		added, _ := inv.AddItem(held)
		m.SetHeldItems(held.Grow(-added), item.Stack{})
	} else {
		for i, s := range inv.Slots() {
			if s.Empty() {
				continue
			}
			n := min(s.Count(), g.maxStack)
			_ = inv.SetItem(i, s.Grow(-n))
			m.SetHeldItems(s.Grow(n-s.Count()), item.Stack{})
			break
		}
	}
	if g.visited = append(g.visited, g.target); len(g.visited) > g.maxVisited {
		g.visited = g.visited[1:]
	}
	g.timer = 600
}

func (g *transportItems) Stop(m *mob.Mob) {
	g.endInteraction(m)
	m.Navigation().Stop(m)
	g.cooldown = 20
}

// startInteraction starts taking from or putting into the container the golem reached: it fires the start event of
// what it will do, succeeding or failing (the source holds nothing, the destination has no room), and opens the
// container. It reports false if the container is gone.
func (g *transportItems) startInteraction(m *mob.Mob) bool {
	tx := m.Tx()
	inv, _, ok := containerInv(tx, g.target)
	if !ok {
		return false
	}
	event := transportTakeFail
	if g.toDest {
		event = transportPlaceFail
		if hasRoomFor(inv, carriedItem(m)) {
			event = transportPlaceSucceed
		}
	} else if !inv.Empty() {
		event = transportTakeSucceed
	}
	g.interact = 1
	m.Fire(event, nil)
	if c, ok := tx.Block(g.target).(block.Container); ok {
		g.user = &transportUser{pos: g.target}
		c.AddViewer(g.user, tx, g.target)
	}
	return true
}

// endInteraction closes the container the golem had open and fires the stop event, if it was using one.
func (g *transportItems) endInteraction(m *mob.Mob) {
	if g.interact == 0 {
		return
	}
	g.interact = 0
	tx := m.Tx()
	if tx == nil || tx.Closed() {
		g.user = nil
		return
	}
	if g.user != nil {
		if c, ok := tx.Block(g.user.pos).(block.Container); ok {
			c.RemoveViewer(g.user, tx, g.user.pos)
		}
		g.user = nil
	}
	m.Fire(transportStop, nil)
}

var _ = time.Second
var _ = mgl64.Vec3{}
