//go:build crossfly_full

package village

import (
	"encoding/json"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	vc "github.com/df-mc/dragonfly/server/mob/component/village"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// moveToBlock is BDS BaseMoveToBlockGoal: find a valid block around the
// mob (rings outwards, y order 0, +1, -1, ..., or search_count random
// samples) and walk to it.
type moveToBlock struct {
	speed            float64
	rng, height, cnt int
	radiusSq         float64
	target           cube.Pos
	valid            func(m *mob.Mob, p cube.Pos) bool
	stuck            int
}

func (b *moveToBlock) find(m *mob.Mob) bool {
	base := blockPos(m.Position()).Side(cube.FaceDown)
	if b.cnt > 0 {
		found, best := false, 0.0
		for range b.cnt {
			p, ok := randomPos(m, max(1, b.rng), max(1, b.height))
			if !ok {
				continue
			}
			c := blockPos(p).Side(cube.FaceDown)
			if !b.valid(m, c) {
				continue
			}
			if d := distSq(c.Vec3Centre(), m.Position()); !found || d < best {
				found, best, b.target = true, d, c
			}
		}
		return found
	}
	found, best := false, 0.0
	for _, dy := range yOrder(b.height) {
		for r := 0; r < max(1, b.rng); r++ {
			for x := -r; x <= r; x++ {
				for z := -r; z <= r; z++ {
					if abs(x) != r && abs(z) != r {
						continue
					}
					c := base.Add(cube.Pos{x, dy, z})
					if !b.valid(m, c) {
						continue
					}
					d := distSq(c.Vec3Centre(), m.Position())
					if d <= b.radiusSq {
						b.target = c
						return true
					}
					if !found || d < best {
						found, best, b.target = true, d, c
					}
				}
			}
		}
	}
	return found
}

// yOrder is the y offset order of findTargetBlock: 0, 1, -1, 2, -2, ...
func yOrder(h int) []int {
	out := []int{0}
	for i := 1; i <= h; i++ {
		out = append(out, i, -i)
	}
	return out
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (b *moveToBlock) startMove(m *mob.Mob) {
	b.stuck = 0
	m.Navigation().MoveTo(m, b.target.Side(cube.FaceUp).Vec3Middle(), b.speed)
}

// reached reports whether the mob stands within goal_radius of the
// target's top, repathing now and then.
func (b *moveToBlock) reached(m *mob.Mob) bool {
	top := b.target.Side(cube.FaceUp).Vec3Middle()
	if distSq(m.Position(), top) <= max(b.radiusSq, 1.0)+0.75 {
		return true
	}
	if b.stuck++; m.Navigation().Done() && b.stuck%40 == 0 {
		m.Navigation().MoveTo(m, top, b.speed)
	}
	return false
}

func (b *moveToBlock) continues() bool { return b.stuck < 1200 }

// harvestFarmBlock is minecraft:behavior.harvest_farm_block (BDS
// HarvestFarmBlockGoal): find farmland with a ripe crop (reap: break it and
// replant from the first seeds in the inventory) or an empty one when it
// has seeds (sow).
type harvestFarmBlock struct {
	mob.GoalBase
	moveToBlock
	beforeSearch, searchCooldown, newTask int
	cooldown                              int
	state                                 int
	seedSlot                              int
}

func newHarvestFarmBlock(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		MaxBefore *float64 `json:"max_seconds_before_search"`
		CooldownS *float64 `json:"search_cooldown_max_seconds"`
		NewTask   *float64 `json:"seconds_until_new_task"`
		Count     int      `json:"search_count"`
		Height    *int     `json:"search_height"`
		Range     *int     `json:"search_range"`
		Radius    *float64 `json:"goal_radius"`
		Speed     *float64 `json:"speed_multiplier"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	g := &harvestFarmBlock{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook | mob.FlagJump)}, beforeSearch: 20, searchCooldown: 160, newTask: 10, state: -1}
	g.speed, g.rng, g.height, g.cnt, g.radiusSq = 0.5, 16, 1, j.Count, 1.5*1.5
	if j.MaxBefore != nil {
		g.beforeSearch = int(*j.MaxBefore * 20)
	}
	if j.CooldownS != nil {
		g.searchCooldown = int(*j.CooldownS * 20)
	}
	if j.NewTask != nil {
		g.newTask = int(*j.NewTask * 20)
	}
	if j.Height != nil {
		g.height = *j.Height
	}
	if j.Range != nil {
		g.rng = *j.Range
	}
	if j.Radius != nil {
		g.radiusSq = *j.Radius * *j.Radius
	}
	if j.Speed != nil {
		g.speed = *j.Speed
	}
	g.valid = g.isValid
	return g, nil
}

func ripe(tx *world.Tx, p cube.Pos) bool {
	c, ok := tx.Block(p).(block.Crop)
	return ok && c.GrowthStage() >= 7
}

func isAir(tx *world.Tx, p cube.Pos) bool {
	_, ok := tx.Block(p).(block.Air)
	return ok
}

func farmland(tx *world.Tx, p cube.Pos) bool {
	b, ok := tx.BlockLoaded(p)
	if !ok {
		return false
	}
	_, f := b.(block.Farmland)
	return f
}

func (g *harvestFarmBlock) isValid(m *mob.Mob, p cube.Pos) bool {
	if !farmland(m.Tx(), p) {
		return false
	}
	up := p.Side(cube.FaceUp)
	return (g.seedSlot >= 0 && isAir(m.Tx(), up)) || ripe(m.Tx(), up)
}

func seedSlot(m *mob.Mob) int {
	for i, s := range vc.Inventory(m).Slots() {
		if !s.Empty() && plantable(s) {
			return i
		}
	}
	return -1
}

// plantable reports whether s is a crop seed (wheat, beetroot seeds,
// carrots, potatoes).
func plantable(s item.Stack) bool {
	b, ok := s.Item().(world.Block)
	if !ok {
		return false
	}
	_, crop := b.(block.Crop)
	return crop
}

func (g *harvestFarmBlock) CanUse(m *mob.Mob) bool {
	if g.cooldown > 0 {
		g.cooldown--
		return false
	}
	g.cooldown = m.Rand().IntN(max(1, g.beforeSearch))
	if !hasNav(m) || !mobGriefing(m) {
		return false
	}
	g.state = -1
	g.seedSlot = seedSlot(m)
	if g.find(m) {
		up := g.target.Side(cube.FaceUp)
		if g.seedSlot >= 0 && isAir(m.Tx(), up) {
			g.state = 1
			return true
		}
		if ripe(m.Tx(), up) {
			g.state = 0
			return true
		}
	}
	g.cooldown = m.Rand().IntN(max(1, g.searchCooldown))
	return false
}

func (g *harvestFarmBlock) CanContinueToUse(m *mob.Mob) bool {
	return g.state >= 0 && g.continues() && g.isValid(m, g.target)
}

func (g *harvestFarmBlock) Start(m *mob.Mob) { g.startMove(m) }

func (g *harvestFarmBlock) Tick(m *mob.Mob) {
	lookAtPos(m, g.target.Vec3().Add(mgl64.Vec3{0.5, 1, 0.5}), 10, maxHeadXRot)
	if !g.reached(m) {
		return
	}
	tx := m.Tx()
	up := g.target.Side(cube.FaceUp)
	switch g.state {
	case 0:
		if ripe(tx, up) {
			harvest(m, up)
			if g.seedSlot = seedSlot(m); g.seedSlot >= 0 {
				sow(m, up, g.seedSlot)
			}
		}
	case 1:
		if g.seedSlot >= 0 {
			sow(m, up, g.seedSlot)
		} else {
			g.cooldown = 0
		}
	}
	g.cooldown = g.newTask
	g.state = -1
}

func (g *harvestFarmBlock) Stop(m *mob.Mob) {
	m.Navigation().Stop(m)
	g.state = -1
}

// harvest breaks the crop at p (Actor::destroyBlock): the drops the mob's
// shareables want go into its inventory (it would pick them up), the rest
// falls.
func harvest(m *mob.Mob, p cube.Pos) {
	tx := m.Tx()
	b := tx.Block(p)
	var drops []item.Stack
	if br, ok := b.(block.Breakable); ok {
		drops = br.BreakInfo().Drops(item.ToolNone{}, nil)
	}
	tx.SetBlock(p, nil, nil)
	tx.AddParticle(p.Vec3Centre(), particle.BlockBreak{Block: b})
	sh, hasShare := vc.ShareablesOf(m)
	for _, d := range drops {
		if hasShare && sh.CountToPickUp(m, d) > 0 {
			left, _ := vc.Inventory(m).AddItem(d)
			if left > 0 {
				vc.ThrowItems(m, []item.Stack{d.Grow(left - d.Count())})
			}
			continue
		}
		vc.ThrowItems(m, []item.Stack{d})
	}
}

// sow plants the seed of slot on p (ItemStack::useOn on the farmland).
func sow(m *mob.Mob, p cube.Pos, slot int) bool {
	tx := m.Tx()
	if !isAir(tx, p) || !farmland(tx, p.Side(cube.FaceDown)) {
		return false
	}
	inv := vc.Inventory(m)
	s, err := inv.Item(slot)
	if err != nil || s.Empty() {
		return false
	}
	b, ok := s.Item().(world.Block)
	if !ok {
		return false
	}
	tx.SetBlock(p, b, nil)
	_ = inv.SetItem(slot, s.Grow(-1))
	return true
}

func mobGriefing(*mob.Mob) bool { return MobGriefing }

// MobGriefing is the mobGriefing game rule for the farm, raid_garden and
// enderman goals (Dragonfly has no game rules).
var MobGriefing = true

// fertilizeFarmBlock is minecraft:behavior.fertilize_farm_block (BDS
// FertilizeFarmBlockGoal): bone meal on unripe crops near the mob.
type fertilizeFarmBlock struct {
	mob.GoalBase
	moveToBlock
	maxUse   int
	cooldown int
	next     int64
	slot     int
	uses     int
}

func newFertilizeFarmBlock(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		MaxUse    *int     `json:"max_fertilizer_usage"`
		CooldownS *float64 `json:"search_cooldown_max_seconds"`
		Count     *int     `json:"search_count"`
		Height    *int     `json:"search_height"`
		Range     *int     `json:"search_range"`
		Radius    *float64 `json:"goal_radius"`
		Speed     *float64 `json:"speed_multiplier"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	g := &fertilizeFarmBlock{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook | mob.FlagJump)}, maxUse: 1, cooldown: 160, slot: -1}
	g.speed, g.rng, g.height, g.cnt, g.radiusSq = 0.5, 1, 1, 9, 1.5*1.5
	if j.MaxUse != nil {
		g.maxUse = *j.MaxUse
	}
	if j.CooldownS != nil {
		g.cooldown = int(*j.CooldownS * 20)
	}
	if j.Count != nil {
		g.cnt = *j.Count
	}
	if j.Height != nil {
		g.height = *j.Height
	}
	if j.Range != nil {
		g.rng = *j.Range
	}
	if j.Radius != nil {
		g.radiusSq = *j.Radius * *j.Radius
	}
	if j.Speed != nil {
		g.speed = *j.Speed
	}
	g.valid = func(m *mob.Mob, p cube.Pos) bool {
		if !farmland(m.Tx(), p) {
			return false
		}
		c, ok := m.Tx().Block(p.Side(cube.FaceUp)).(block.Crop)
		return ok && c.GrowthStage() < 7
	}
	return g, nil
}

func boneMealSlot(m *mob.Mob) int {
	for i, s := range vc.Inventory(m).Slots() {
		if _, ok := s.Item().(item.BoneMeal); ok && !s.Empty() {
			return i
		}
	}
	return -1
}

func (g *fertilizeFarmBlock) CanUse(m *mob.Mob) bool {
	if m.Ticks() < g.next {
		return false
	}
	g.next = m.Ticks() + int64(m.Rand().IntN(max(1, g.cooldown)))
	if !hasNav(m) || !mobGriefing(m) {
		return false
	}
	if g.slot = boneMealSlot(m); g.slot < 0 {
		return false
	}
	return g.find(m)
}

func (g *fertilizeFarmBlock) CanContinueToUse(m *mob.Mob) bool {
	return g.slot >= 0 && g.uses < g.maxUse && g.continues() && g.valid(m, g.target)
}

func (g *fertilizeFarmBlock) Start(m *mob.Mob) { g.uses = 0; g.startMove(m) }

func (g *fertilizeFarmBlock) Tick(m *mob.Mob) {
	lookAtPos(m, g.target.Vec3().Add(mgl64.Vec3{0.5, 1, 0.5}), 10, maxHeadXRot)
	if !g.reached(m) {
		return
	}
	up := g.target.Side(cube.FaceUp)
	inv := vc.Inventory(m)
	s, _ := inv.Item(g.slot)
	if bm, ok := m.Tx().Block(up).(interface {
		BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult
	}); ok && !s.Empty() && bm.BoneMeal(up, m.Tx()) != item.BoneMealResultNone {
		_ = inv.SetItem(g.slot, s.Grow(-1))
		m.Tx().AddParticle(up.Vec3Centre(), particle.BoneMeal{})
		g.uses++
		g.slot = boneMealSlot(m)
		return
	}
	if vc.GrowCrop(m.Tx(), up) && !s.Empty() {
		_ = inv.SetItem(g.slot, s.Grow(-1))
		m.Tx().AddParticle(up.Vec3Centre(), particle.BoneMeal{})
		g.uses++
		g.slot = boneMealSlot(m)
		return
	}
	g.slot = -1
}

func (g *fertilizeFarmBlock) Stop(m *mob.Mob) {
	m.Navigation().Stop(m)
	g.next = m.Ticks() + int64(m.Rand().IntN(max(1, g.cooldown)))
}

// inspectBookshelf is minecraft:behavior.inspect_bookshelf (BDS
// InspectBookshelfGoal): walk next to a bookshelf and look at it.
type inspectBookshelf struct {
	mob.GoalBase
	moveToBlock
	shelf cube.Pos
	timer int
}

func newInspectBookshelf(spec mob.GoalSpec) (mob.Goal, error) {
	j := struct {
		Speed  *float64 `json:"speed_multiplier"`
		Range  *int     `json:"search_range"`
		Height *int     `json:"search_height"`
		Count  *int     `json:"search_count"`
		Radius *float64 `json:"goal_radius"`
	}{}
	if err := decode(spec, &j); err != nil {
		return nil, err
	}
	g := &inspectBookshelf{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}}
	g.speed, g.rng, g.height, g.cnt, g.radiusSq = 1, 0, 1, 10, 0.25
	if j.Speed != nil {
		g.speed = *j.Speed
	}
	if j.Range != nil {
		g.rng = *j.Range
	}
	if j.Height != nil {
		g.height = *j.Height
	}
	if j.Count != nil {
		g.cnt = *j.Count
	}
	if j.Radius != nil {
		g.radiusSq = *j.Radius * *j.Radius
	}
	g.valid = func(m *mob.Mob, p cube.Pos) bool {
		_, ok := m.Tx().Block(p).(block.Bookshelf)
		return ok
	}
	return g, nil
}

func (g *inspectBookshelf) CanUse(m *mob.Mob) bool {
	if !hasNav(m) || m.Rand().IntN(20) != 0 || !g.find(m) {
		return false
	}
	g.shelf = g.target
	tx := m.Tx()
	start := m.Rand().IntN(4)
	for i := range 4 {
		d := cube.Directions()[(start+i)%4]
		n := g.shelf.Side(d.Face())
		y := vc.TopSolid(tx, n[0], g.shelf[1], n[2])
		stand := cube.Pos{n[0], y, n[2]}
		if _, ok := tx.Block(stand.Side(cube.FaceDown)).(block.Bookshelf); ok {
			continue
		}
		if distSq(stand.Vec3Centre(), m.Position()) < float64(g.rng*g.rng)+64 {
			g.target = stand.Side(cube.FaceDown)
			return true
		}
	}
	return false
}

func (g *inspectBookshelf) CanContinueToUse(m *mob.Mob) bool {
	_, ok := m.Tx().Block(g.shelf).(block.Bookshelf)
	return ok && g.timer < 200
}

func (g *inspectBookshelf) Start(m *mob.Mob) { g.timer = 0; g.startMove(m) }

func (g *inspectBookshelf) Tick(m *mob.Mob) {
	g.timer++
	lookAtPos(m, g.shelf.Vec3Centre(), 60, maxHeadXRot)
	g.reached(m)
}

func (g *inspectBookshelf) Stop(m *mob.Mob) { m.Navigation().Stop(m) }
