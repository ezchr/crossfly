//go:build crossfly_full

package jvillage

import (
	"slices"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/jbrain"
	"github.com/df-mc/dragonfly/server/mob/jbrain/jmob"
	"github.com/df-mc/dragonfly/server/mob/jpoi"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// playWorkSound is Villager.playWorkSound.
func (v *Villager) playWorkSound(m *javamob.Mob) {
	if s := v.profession().WorkSound; s != "" {
		m.PlaySound(s)
	}
}

// compostable are WorkAtComposter.COMPOSTABLE_ITEMS.
var compostable = []string{"wheat_seeds", "beetroot_seeds"}

// workAtComposter is WorkAtComposter.useWorkstation: bake bread from wheat,
// then compost seeds beyond 10 of each kind (20 at most).
func (v *Villager) workAtComposter(c *jbrain.Ctx, m *javamob.Mob) {
	g, ok := jbrain.Get(c.Mem, jbrain.MemJobSite)
	if !ok {
		return
	}
	comp, ok := c.Tx.Block(g.Pos).(block.Composter)
	if !ok {
		return
	}
	v.makeBread(m)
	if comp.Level == 8 {
		// ComposterBlock.extractProduce: bone meal pops out on top.
		comp.Level = 0
		c.Tx.SetBlock(g.Pos, comp, nil)
		top := mgl64.Vec3{float64(g.Pos[0]) + .5, float64(g.Pos[1]) + 1.01, float64(g.Pos[2]) + .5}
		c.Tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: top}, item.NewStack(item.BoneMeal{}, 1)))
	}
	left := 20
	seen := make([]int, len(compostable))
	for i := len(v.inventory) - 1; i >= 0 && left > 0; i-- {
		s := v.inventory[i]
		idx := slices.Index(compostable, javaNameOf(s))
		if idx < 0 {
			continue
		}
		seen[idx] += s.Count()
		use := min(seen[idx]-10, left, s.Count())
		if use <= 0 {
			continue
		}
		left -= use
		for range use {
			// ComposterBlock.insertItem: seeds have a 0.3 compost chance.
			s = s.Grow(-1)
			v.inventory[i] = s
			if comp.Level < 7 && m.Rand().Float64() < 0.3 {
				comp.Level++
				c.Tx.SetBlock(g.Pos, comp, nil)
				if comp.Level == 7 {
					c.Tx.ScheduleBlockUpdate(g.Pos, comp, time.Second)
					return
				}
			}
		}
	}
}

// makeBread is WorkAtComposter.makeBread.
func (v *Villager) makeBread(m *javamob.Mob) {
	if v.CountItem("bread") > 36 {
		return
	}
	n := min(3, v.CountItem("wheat")/3)
	if n == 0 {
		return
	}
	v.removeItemType("wheat", n*3)
	if rest := v.AddItem(item.NewStack(item.Bread{}, n)); !rest.Empty() {
		m.SpawnAtLocation(rest)
	}
}

// removeItemType is SimpleContainer.removeItemType.
func (v *Villager) removeItemType(javaName string, n int) {
	for i, s := range v.inventory {
		if n == 0 {
			return
		}
		if javaNameOf(s) != javaName {
			continue
		}
		take := min(n, s.Count())
		v.inventory[i] = s.Grow(-take)
		n -= take
	}
}

// harvestFarmland is HarvestFarmland: farmers break ripe crops and plant
// seeds on bare farmland around them.
type harvestFarmland struct {
	jmob.TaskBase
	target   cube.Pos
	hasTgt   bool
	nextOk   int64
	worked   int
	farmland []cube.Pos
}

// HarvestFarmland is new HarvestFarmland().
func HarvestFarmland() jmob.Control {
	return jmob.Behaviour("HarvestFarmland", []jbrain.Req{jbrain.Absent(jbrain.MemLookTarget), jbrain.Absent(jbrain.MemWalkTarget), jbrain.Present(jbrain.MemSecondaryJobSite)}, jbrain.DefaultDuration, jbrain.DefaultDuration, &harvestFarmland{})
}

func ripe(b world.Block) bool {
	cr, ok := b.(block.Crop)
	return ok && cr.GrowthStage() >= 7
}

func (h *harvestFarmland) valid(tx *world.Tx, p cube.Pos) bool {
	b := tx.Block(p)
	if ripe(b) {
		return true
	}
	_, air := b.(block.Air)
	_, farm := tx.Block(p.Side(cube.FaceDown)).(block.Farmland)
	return air && farm
}

func (h *harvestFarmland) pick(c *jbrain.Ctx) {
	if len(h.farmland) == 0 {
		h.hasTgt = false
		return
	}
	h.target, h.hasTgt = h.farmland[c.Rand.IntN(len(h.farmland))], true
}

func (h *harvestFarmland) CanStart(c *jbrain.Ctx, m *javamob.Mob) bool {
	if !mobGriefing(c.Tx) {
		return false
	}
	v, ok := Of(m)
	if !ok || v.profession() != ProfFarmer {
		return false
	}
	h.farmland = h.farmland[:0]
	pos := m.Position()
	for x := -1; x <= 1; x++ {
		for y := -1; y <= 1; y++ {
			for z := -1; z <= 1; z++ {
				p := jbrain.Containing(pos.Add(mgl64.Vec3{float64(x), float64(y), float64(z)}))
				if h.valid(c.Tx, p) {
					h.farmland = append(h.farmland, p)
				}
			}
		}
	}
	h.pick(c)
	return h.hasTgt
}

func (h *harvestFarmland) Start(c *jbrain.Ctx, m *javamob.Mob) {
	if c.Time > h.nextOk && h.hasTgt {
		t := jbrain.TrackBlock(h.target)
		jbrain.Set[jbrain.PositionTracker](c.Mem, jbrain.MemLookTarget, t)
		jbrain.Set(c.Mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: t, Speed: 0.5, CloseEnough: 1})
	}
}

func (h *harvestFarmland) Stop(c *jbrain.Ctx, _ *javamob.Mob) {
	c.Mem.Erase(jbrain.MemLookTarget.ID())
	c.Mem.Erase(jbrain.MemWalkTarget.ID())
	h.worked = 0
	h.nextOk = c.Time + 40
}

func (h *harvestFarmland) CanStillUse(*jbrain.Ctx, *javamob.Mob) bool { return h.worked < 200 }

func (h *harvestFarmland) Tick(c *jbrain.Ctx, m *javamob.Mob) {
	if h.hasTgt && !jmob.CloserToCenterThan(h.target, m.Position(), 1) {
		return
	}
	if h.hasTgt && c.Time > h.nextOk {
		v, _ := Of(m)
		b := c.Tx.Block(h.target)
		if ripe(b) {
			breakBlockAsMob(c.Tx, h.target, b)
		}
		_, air := c.Tx.Block(h.target).(block.Air)
		_, farm := c.Tx.Block(h.target.Side(cube.FaceDown)).(block.Farmland)
		if air && farm && v.HasFarmSeeds() {
			for i, s := range v.inventory {
				if !slices.Contains(villagerPlantableSeeds, javaNameOf(s)) {
					continue
				}
				if pl, ok := s.Item().(world.Block); ok || plantOf(s) != nil {
					if !ok {
						pl = plantOf(s)
					}
					c.Tx.SetBlock(h.target, pl, nil)
					c.Tx.PlaySound(h.target.Vec3Centre(), nil)
					v.inventory[i] = s.Grow(-1)
					break
				}
			}
		}
		if cr, ok := c.Tx.Block(h.target).(block.Crop); ok && cr.GrowthStage() < 7 {
			h.farmland = slices.DeleteFunc(h.farmland, func(p cube.Pos) bool { return p == h.target })
			h.pick(c)
			if h.hasTgt {
				h.nextOk = c.Time + 20
				t := jbrain.TrackBlock(h.target)
				jbrain.Set(c.Mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: t, Speed: 0.5, CloseEnough: 1})
				jbrain.Set[jbrain.PositionTracker](c.Mem, jbrain.MemLookTarget, t)
			}
		}
	}
	h.worked++
}

// plantOf returns the crop block a seed item plants.
func plantOf(s item.Stack) world.Block {
	switch javaNameOf(s) {
	case "wheat_seeds":
		return block.WheatSeeds{}
	case "potato":
		return block.Potato{}
	case "carrot":
		return block.Carrot{}
	case "beetroot_seeds":
		return block.BeetrootSeeds{}
	case "torchflower_seeds":
		if b, ok := world.BlockByName("minecraft:torchflower_crop", map[string]any{"growth": int32(0)}); ok {
			return b
		}
	case "pitcher_pod":
		if b, ok := world.BlockByName("minecraft:pitcher_crop", map[string]any{"growth": int32(0), "upper_block_bit": false}); ok {
			return b
		}
	}
	return nil
}

// breakBlockAsMob is Level.destroyBlock(pos, true, mob): break and drop.
func breakBlockAsMob(tx *world.Tx, pos cube.Pos, b world.Block) {
	tx.SetBlock(pos, nil, nil)
	if br, ok := b.(block.Breakable); ok {
		for _, d := range br.BreakInfo().Drops(item.ToolNone{}, nil) {
			tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: pos.Vec3Centre()}, d))
		}
	}
	world.BlockBroken(tx, pos, b, nil)
}

// mobGriefing reads the mobGriefing game rule (on unless switched off).
var mobGriefing = func(tx *world.Tx) bool { return true }

// useBonemeal is UseBonemeal: farmers bone meal growing crops around them.
type useBonemeal struct {
	jmob.TaskBase
	nextCycle, lastSession int64
	worked                 int
	crop                   cube.Pos
	hasCrop                bool
}

// UseBonemeal is new UseBonemeal().
func UseBonemeal() jmob.Control {
	return jmob.Behaviour("UseBonemeal", []jbrain.Req{jbrain.Absent(jbrain.MemLookTarget), jbrain.Absent(jbrain.MemWalkTarget)}, jbrain.DefaultDuration, jbrain.DefaultDuration, &useBonemeal{})
}

func (u *useBonemeal) CanStart(c *jbrain.Ctx, m *javamob.Mob) bool {
	tick := int64(m.TickCount())
	if tick%10 != 0 || (u.lastSession != 0 && u.lastSession+160 > tick) {
		return false
	}
	v, ok := Of(m)
	if !ok || v.CountItem("bone_meal") <= 0 {
		return false
	}
	u.pick(c, m)
	return u.hasCrop
}

func (u *useBonemeal) pick(c *jbrain.Ctx, m *javamob.Mob) {
	u.hasCrop = false
	n := 0
	base := BlockPos(m)
	for x := -1; x <= 1; x++ {
		for y := -1; y <= 1; y++ {
			for z := -1; z <= 1; z++ {
				p := base.Add(cube.Pos{x, y, z})
				if cr, ok := c.Tx.Block(p).(block.Crop); ok && cr.GrowthStage() < 7 {
					n++
					if c.Rand.IntN(n) == 0 {
						u.crop, u.hasCrop = p, true
					}
				}
			}
		}
	}
}

func (u *useBonemeal) target(c *jbrain.Ctx) {
	if !u.hasCrop {
		return
	}
	t := jbrain.TrackBlock(u.crop)
	jbrain.Set[jbrain.PositionTracker](c.Mem, jbrain.MemLookTarget, t)
	jbrain.Set(c.Mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: t, Speed: 0.5, CloseEnough: 1})
}

func (u *useBonemeal) CanStillUse(*jbrain.Ctx, *javamob.Mob) bool { return u.worked < 80 && u.hasCrop }

func (u *useBonemeal) Start(c *jbrain.Ctx, m *javamob.Mob) {
	u.target(c)
	setMainHand(m, item.NewStack(item.BoneMeal{}, 1))
	u.nextCycle, u.worked = c.Time, 0
}

func (u *useBonemeal) Stop(_ *jbrain.Ctx, m *javamob.Mob) {
	setMainHand(m, item.Stack{})
	u.lastSession = int64(m.TickCount())
}

func (u *useBonemeal) Tick(c *jbrain.Ctx, m *javamob.Mob) {
	if c.Time < u.nextCycle || !jmob.CloserToCenterThan(u.crop, m.Position(), 1) {
		return
	}
	v, _ := Of(m)
	for i, s := range v.inventory {
		if javaNameOf(s) != "bone_meal" {
			continue
		}
		if bm, ok := c.Tx.Block(u.crop).(item.BoneMealAffected); ok && bm.BoneMeal(u.crop, c.Tx) != item.BoneMealResultNone {
			v.inventory[i] = s.Grow(-1)
			u.pick(c, m)
			u.target(c)
			u.nextCycle = c.Time + 40
		}
		break
	}
	u.worked++
}

// SetHiddenState is SetHiddenState.create(seconds, closeEnough).
func SetHiddenState(seconds, closeEnough int) jmob.Control {
	stay := seconds * 20
	hidden := 0
	return jmob.Trigger("SetHiddenState", []jbrain.Req{jbrain.Present(jbrain.MemHidingPlace), jbrain.Present(jbrain.MemHeardBellTime)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		heard, _ := jbrain.Get(c.Mem, jbrain.MemHeardBellTime)
		if hidden <= stay && heard+300 > c.Time {
			hp, _ := jbrain.Get(c.Mem, jbrain.MemHidingPlace)
			if distSqPos(hp.Pos, BlockPos(m)) < float64(closeEnough*closeEnough) {
				hidden++
			}
			return true
		}
		c.Mem.Erase(jbrain.MemHeardBellTime.ID())
		c.Mem.Erase(jbrain.MemHidingPlace.ID())
		if b, ok := jmob.Of(m); ok {
			b.UpdateActivityFromSchedule(c)
		}
		hidden = 0
		return true
	})
}

// LocateHidingPlace is LocateHidingPlace.create(radius, speed, closeEnough).
func LocateHidingPlace(radius int, speed float32, closeEnough int) jmob.Control {
	return jmob.Trigger("LocateHidingPlace", []jbrain.Req{jbrain.Absent(jbrain.MemWalkTarget), jbrain.Reg(jbrain.MemHome), jbrain.Reg(jbrain.MemHidingPlace), jbrain.Reg(jbrain.MemPath), jbrain.Reg(jbrain.MemLookTarget), jbrain.Reg(jbrain.MemBreedTarget), jbrain.Reg(jbrain.MemInteractionTarget)}, func(c *jbrain.Ctx, m *javamob.Mob) bool {
		pm := jpoi.For(c.Tx)
		pos := BlockPos(m)
		target, ok := pm.Find(c.Tx, jpoi.Is(jpoi.Home), nil, pos, closeEnough+1, jpoi.AnyOccupancy)
		if ok && !jmob.CloserToCenterThan(target, m.Position(), float64(closeEnough)) {
			ok = false
		}
		if !ok {
			target, ok = pm.Random(c.Tx, jpoi.Is(jpoi.Home), nil, jpoi.AnyOccupancy, pos, radius, m.Rand().IntN)
		}
		if !ok {
			if h, has := jbrain.Get(c.Mem, jbrain.MemHome); has {
				target, ok = h.Pos, true
			}
		}
		if ok {
			for _, id := range []jbrain.MemoryID{jbrain.MemPath.ID(), jbrain.MemLookTarget.ID(), jbrain.MemBreedTarget.ID(), jbrain.MemInteractionTarget.ID()} {
				c.Mem.Erase(id)
			}
			jbrain.Set(c.Mem, jbrain.MemHidingPlace, jbrain.GlobalPos{Dim: c.Tx.World().Dimension(), Pos: target})
			if !jmob.CloserToCenterThan(target, m.Position(), float64(closeEnough)) {
				jbrain.Set(c.Mem, jbrain.MemWalkTarget, jbrain.WalkTarget{Target: jbrain.TrackBlock(target), Speed: speed, CloseEnough: closeEnough})
			}
		}
		return true
	})
}
