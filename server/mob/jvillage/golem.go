//go:build crossfly_full

package jvillage

import (
	"math"
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/mob/jbrain/jmob"
	"github.com/df-mc/dragonfly/server/mob/jpoi"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// IronGolemID is the iron golem's identifier.
const IronGolemID = "minecraft:iron_golem"

func init() {
	javamob.Register(javamob.Type{
		ID: IronGolemID, Category: javamob.CategoryMisc,
		Width: 1.4, Height: 2.7,
		// IronGolem.createAttributes.
		Attributes: func() map[javamob.Attribute]float64 {
			a := javamob.LivingAttributes()
			a[javamob.AttrFollowRange] = 16
			a[javamob.AttrMaxHealth] = 100
			a[javamob.AttrMovementSpeed] = 0.25
			a[javamob.AttrKnockbackResistance] = 1
			a[javamob.AttrAttackDamage] = 15
			a[javamob.AttrStepHeight] = 1
			return a
		},
		Families: []string{"irongolem", "mob"},
		New:      func() javamob.Behaviour { return &IronGolem{} },
	})
}

// IronGolem is Java's IronGolem: a neutral mob that defends villagers.
type IronGolem struct {
	javamob.NeutralMob
	attackAnim, offerFlowerTick int
	// PlayerCreated golems never attack players.
	PlayerCreated bool
}

// GolemOf returns the iron golem behaviour of m.
func GolemOf(m *javamob.Mob) (*IronGolem, bool) {
	g, ok := m.Behaviour().(*IronGolem)
	return g, ok
}

// RegisterGoals is IronGolem.registerGoals.
func (g *IronGolem) RegisterGoals(m *javamob.Mob) {
	m.Goals().Add(1, goal.NewMeleeAttack(1, true))
	m.Goals().Add(2, &moveTowardsTarget{GoalBase: javamob.GoalBase{Control: javamob.FlagMove}, speed: 0.9, within: 32})
	m.Goals().Add(2, moveBackToVillage(0.6))
	m.Goals().Add(4, golemRandomStrollInVillage(0.6))
	m.Goals().Add(5, &offerFlower{GoalBase: javamob.GoalBase{Control: javamob.FlagMove | javamob.FlagLook}})
	m.Goals().Add(7, goal.NewLookAtPlayer(6))
	m.Goals().Add(8, goal.NewRandomLookAround())
	m.Targets().Add(1, &defendVillageTarget{GoalBase: javamob.GoalBase{Control: javamob.FlagTarget}})
	m.Targets().Add(2, goal.NewHurtByTarget())
	angry := goal.NewNearestAttackableTarget(nil, true, false)
	angry.Selector = func(e world.Entity, tx *world.Tx) bool {
		gm, ok := javamob.OfHandle(tx, m.H())
		return ok && g.IsAngryAt(gm, e)
	}
	m.Targets().Add(3, angry)
	enemies := goal.NewNearestAttackableTarget(isEnemyNotCreeper, false, false)
	enemies.RandomInterval = javamob.ReducedTickDelay(5)
	m.Targets().Add(3, enemies)
	// ResetUniversalAngerTargetGoal (4) only acts with the universalAnger game rule, off by default.
}

// enemies lists the Enemy mobs that are not monsters by category.
var enemies = []string{"minecraft:slime", "minecraft:magma_cube", "minecraft:ghast", "minecraft:phantom", "minecraft:shulker", "minecraft:hoglin", "minecraft:zoglin", "minecraft:ender_dragon", "minecraft:wither", "minecraft:happy_ghast"}

// isEnemyNotCreeper is the IronGolem target selector: Enemy and not a creeper.
func isEnemyNotCreeper(e world.Entity) bool {
	id := e.H().Type().EncodeEntity()
	if id == "minecraft:creeper" || id == "minecraft:happy_ghast" {
		return false
	}
	if jm, ok := javamob.Of(e); ok {
		return jm.JavaType().Category.Name == "monster" || slices.Contains(enemies, id)
	}
	if f, ok := e.(interface{ HasFamily(string) bool }); ok {
		return f.HasFamily("monster")
	}
	return false
}

// AiStep is IronGolem.aiStep: animation timers and persistent anger.
func (g *IronGolem) AiStep(m *javamob.Mob) {
	if g.attackAnim > 0 {
		g.attackAnim--
	}
	if g.offerFlowerTick > 0 {
		g.offerFlowerTick--
	}
	g.UpdatePersistentAnger(m, true)
}

// CanAttack is IronGolem.canAttack: player-made golems spare players; no
// golem attacks creepers.
func (g *IronGolem) CanAttack(m *javamob.Mob, e world.Entity) bool {
	if _, ok := e.(*player.Player); ok && g.PlayerCreated {
		return false
	}
	return e.H().Type().EncodeEntity() != "minecraft:creeper"
}

// DoHurtTarget is IronGolem.doHurtTarget: damage/2 + rand(damage), and a
// 0.4 upward fling scaled by knockback resistance.
func (g *IronGolem) DoHurtTarget(m *javamob.Mob, target world.Entity) bool {
	g.attackAnim = 10
	broadcastEvent(m, EventAttack)
	dmg := float32(m.Attr(javamob.AttrAttackDamage))
	if int(dmg) > 0 {
		dmg = dmg/2 + float32(m.Rand().IntN(int(dmg)))
	}
	l, ok := target.(entity.Living)
	if !ok {
		return false
	}
	_, res := l.Hurt(float64(dmg), entity.AttackDamageSource{Attacker: m})
	hurt := res.Accepted()
	if hurt {
		res := 0.0
		if jm, ok := javamob.Of(target); ok {
			res = jm.Attr(javamob.AttrKnockbackResistance)
		}
		scale := math.Max(0, 1-res)
		if v, ok := target.(interface {
			Velocity() mgl64.Vec3
			SetVelocity(mgl64.Vec3)
		}); ok {
			v.SetVelocity(v.Velocity().Add(mgl64.Vec3{0, float64(float32(0.4)) * scale, 0}))
		}
		m.SetLastHurtMob(target)
	}
	m.PlaySound("attack.strong")
	return hurt
}

// MobInteract is IronGolem.mobInteract: an iron ingot heals 25.
func (g *IronGolem) MobInteract(m *javamob.Mob, user item.User, held item.Stack) (item.Stack, bool) {
	if _, ok := held.Item().(item.IronIngot); !ok {
		return held, false
	}
	before := m.Health()
	m.Heal(25, entity.FoodHealingSource{})
	if m.Health() == before {
		return held, false
	}
	m.PlaySound("repair")
	if p, ok := user.(*player.Player); ok && p.GameMode().CreativeInventory() {
		return held, true
	}
	return held.Grow(-1), true
}

// OfferFlower is IronGolem.offerFlower.
func (g *IronGolem) OfferFlower(m *javamob.Mob, offer bool) {
	if offer {
		g.offerFlowerTick = 400
		broadcastEvent(m, EventFlower)
	} else {
		g.offerFlowerTick = 0
		broadcastEvent(m, EventStopFlower)
	}
}

// EncodeNBT writes PlayerCreated and the anger.
func (g *IronGolem) EncodeNBT(data map[string]any) {
	data["PlayerCreated"] = boolByte(g.PlayerCreated)
	g.EncodeAnger(data)
}

// DecodeNBT reads EncodeNBT's data.
func (g *IronGolem) DecodeNBT(m *javamob.Mob, data map[string]any) {
	if b, ok := data["PlayerCreated"].(uint8); ok {
		g.PlayerCreated = b != 0
	}
	g.DecodeAnger(m, data)
}

// RemoveWhenFarAway: golems are persistent (MobCategory.MISC).
func (g *IronGolem) RemoveWhenFarAway(*javamob.Mob, float64) bool { return false }

// moveTowardsTarget is MoveTowardsTargetGoal.
type moveTowardsTarget struct {
	javamob.GoalBase
	speed  float64
	within float64
	target *world.EntityHandle
	wanted mgl64.Vec3
}

func (g *moveTowardsTarget) CanUse(m *javamob.Mob) bool {
	t, ok := m.Target()
	if !ok || t.Position().Sub(m.Position()).LenSqr() > g.within*g.within {
		return false
	}
	p, ok := jmob.DefaultRandomPosTowards(m, 16, 7, t.Position(), math.Pi/2)
	if !ok {
		return false
	}
	g.target, g.wanted = t.H(), p
	return true
}

func (g *moveTowardsTarget) CanContinueToUse(m *javamob.Mob) bool {
	t, ok := g.target.Entity(m.Tx())
	if !ok {
		return false
	}
	l, living := t.(entity.Living)
	return !m.Navigation().Done() && (!living || !l.Dead()) && t.Position().Sub(m.Position()).LenSqr() < g.within*g.within
}

func (g *moveTowardsTarget) Stop(*javamob.Mob) { g.target = nil }

func (g *moveTowardsTarget) Start(m *javamob.Mob) {
	m.Navigation().MoveTo(m, g.wanted, 0, g.speed)
}

// moveBackToVillage is MoveBackToVillageGoal: outside a village, stroll
// (interval 10) towards the closest village section.
type villageStroll struct {
	*goal.RandomStroll
	outsideOnly bool
}

func (g villageStroll) CanUse(m *javamob.Mob) bool {
	if g.outsideOnly && jpoi.For(m.Tx()).IsVillage(m.Tx(), BlockPos(m)) {
		return false
	}
	return g.RandomStroll.CanUse(m)
}

func moveBackToVillage(speed float64) villageStroll {
	s := goal.NewRandomStroll(speed)
	s.Interval, s.CheckNoActionTime = 10, false
	s.Position = func(m *javamob.Mob) (mgl64.Vec3, bool) {
		tx := m.Tx()
		best, moved := closestVillageSection(tx, jpoi.For(tx), BlockPos(m), 2)
		if !moved {
			return mgl64.Vec3{}, false
		}
		center := mgl64.Vec3{float64(best[0]<<4) + 8, float64(best[1]<<4) + 8, float64(best[2]<<4) + 8}
		return jmob.DefaultRandomPosTowards(m, 10, 7, mgl64.Vec3{center[0], center[1] - .5 + .5, center[2]}, math.Pi/2)
	}
	return villageStroll{RandomStroll: s, outsideOnly: true}
}

// golemRandomStrollInVillage is GolemRandomStrollInVillageGoal (interval 240):
// towards a villager that wants a golem, a village PoI, or anywhere.
func golemRandomStrollInVillage(speed float64) villageStroll {
	s := goal.NewRandomStroll(speed)
	s.Interval, s.CheckNoActionTime = 240, false
	s.Position = func(m *javamob.Mob) (mgl64.Vec3, bool) {
		r := m.Rand()
		if r.Float32() < 0.3 {
			return m.LandRandomPos(10, 7)
		}
		var p mgl64.Vec3
		var ok bool
		if r.Float32() < 0.7 {
			if p, ok = towardsVillagerWantingGolem(m); !ok {
				p, ok = towardsPoi(m)
			}
		} else if p, ok = towardsPoi(m); !ok {
			p, ok = towardsVillagerWantingGolem(m)
		}
		if !ok {
			return m.LandRandomPos(10, 7)
		}
		return p, true
	}
	return villageStroll{RandomStroll: s}
}

func towardsVillagerWantingGolem(m *javamob.Mob) (mgl64.Vec3, bool) {
	tx := m.Tx()
	now := tx.CurrentTick()
	var wanting []*javamob.Mob
	for _, vm := range villagersIn(tx, m.BBox().Grow(32)) {
		if v, ok := Of(vm); ok && v.WantsToSpawnGolem(now) {
			wanting = append(wanting, vm)
		}
	}
	if len(wanting) == 0 {
		return mgl64.Vec3{}, false
	}
	return m.LandRandomPosTowards(10, 7, wanting[m.Rand().IntN(len(wanting))].Position())
}

func towardsPoi(m *javamob.Mob) (mgl64.Vec3, bool) {
	tx := m.Tx()
	pm := jpoi.For(tx)
	pos := BlockPos(m)
	c := cube.Pos{pos[0] >> 4, pos[1] >> 4, pos[2] >> 4}
	var sections []cube.Pos
	for x := -2; x <= 2; x++ {
		for y := -2; y <= 2; y++ {
			for z := -2; z <= 2; z++ {
				s := c.Add(cube.Pos{x, y, z})
				if pm.SectionsToVillage(tx, cube.Pos{s[0] << 4, s[1] << 4, s[2] << 4}) == 0 {
					sections = append(sections, s)
				}
			}
		}
	}
	if len(sections) == 0 {
		return mgl64.Vec3{}, false
	}
	s := sections[m.Rand().IntN(len(sections))]
	center := cube.Pos{s[0]<<4 + 8, s[1]<<4 + 8, s[2]<<4 + 8}
	recs := pm.InRange(tx, jpoi.Any, center, 8, jpoi.IsOccupied)
	if len(recs) == 0 {
		return mgl64.Vec3{}, false
	}
	t := recs[m.Rand().IntN(len(recs))].Pos
	return m.LandRandomPosTowards(10, 7, bottomCenter(t))
}

// offerFlower is OfferFlowerGoal.
type offerFlower struct {
	javamob.GoalBase
	target *world.EntityHandle
	tick   int
}

func (o *offerFlower) box(m *javamob.Mob) cube.BBox {
	return m.BBox().GrowVec3(mgl64.Vec3{6, 2, 6})
}

func (o *offerFlower) CanUse(m *javamob.Mob) bool {
	if !isBrightOutside(m.Tx()) || m.Rand().IntN(8000) != 0 {
		return false
	}
	e, ok := m.NearestEntity(o.box(m), func(e world.Entity) bool {
		id := e.H().Type().EncodeEntity()
		return id == VillagerID || id == CopperGolemID
	}, javamob.ForNonCombat(6))
	if !ok {
		return false
	}
	o.target = e.H()
	return true
}

func (o *offerFlower) CanContinueToUse(*javamob.Mob) bool { return o.tick > 0 }

func (o *offerFlower) Start(m *javamob.Mob) {
	o.tick = javamob.AdjustedTickDelay(o, 400)
	if g, ok := GolemOf(m); ok {
		g.OfferFlower(m, true)
	}
}

func (o *offerFlower) Stop(m *javamob.Mob) {
	if g, ok := GolemOf(m); ok {
		g.OfferFlower(m, false)
	}
	if o.tick == 0 {
		if e, ok := o.target.Entity(m.Tx()); ok {
			giveCopperGolemPoppy(m, e, o.box(m))
		}
	}
	o.target = nil
}

func (o *offerFlower) Tick(m *javamob.Mob) {
	if e, ok := o.target.Entity(m.Tx()); ok {
		m.LookControl().SetLookAtEntity(e, 30, 30)
	}
	o.tick--
}

// isBrightOutside is Level.isBrightOutside: daytime in the overworld.
func isBrightOutside(tx *world.Tx) bool {
	if tx.World().Dimension() != world.Overworld {
		return false
	}
	t := tx.World().Time() % 24000
	return t < 12542 || t > 23460
}

// defendVillageTarget is DefendVillageTargetGoal: target a player whom a
// villager within 10/8/10 blocks dislikes (reputation <= -100).
type defendVillageTarget struct {
	javamob.GoalBase
	potential *world.EntityHandle
}

func (d *defendVillageTarget) CanUse(m *javamob.Mob) bool {
	tx := m.Tx()
	box := m.BBox().GrowVec3(mgl64.Vec3{10, 8, 10})
	tc := javamob.ForCombat(64)
	var players []*player.Player
	for e := range tx.EntitiesWithin(box) {
		if p, ok := e.(*player.Player); ok && tc.Test(m, p) {
			players = append(players, p)
		}
	}
	for _, vm := range villagersIn(tx, box) {
		v, _ := Of(vm)
		if !tc.Test(m, vm) {
			continue
		}
		for _, p := range players {
			if v.PlayerReputation(p) <= -100 {
				d.potential = p.H()
			}
		}
	}
	if d.potential == nil {
		return false
	}
	e, ok := d.potential.Entity(tx)
	if !ok {
		return false
	}
	if p, ok := e.(*player.Player); ok && (p.GameMode() == world.GameModeSpectator || p.GameMode().CreativeInventory()) {
		return false
	}
	return true
}

func (d *defendVillageTarget) Start(m *javamob.Mob) {
	if e, ok := d.potential.Entity(m.Tx()); ok {
		m.SetTarget(e)
	}
}

func (d *defendVillageTarget) CanContinueToUse(m *javamob.Mob) bool {
	t, ok := m.Target()
	return ok && jmobAlive(t) && t.Position().Sub(m.Position()).LenSqr() <= m.FollowRange()*m.FollowRange()
}

func (d *defendVillageTarget) Stop(m *javamob.Mob) {
	m.SetTarget(nil)
	d.potential = nil
}

func jmobAlive(e world.Entity) bool {
	l, ok := e.(entity.Living)
	return !ok || !l.Dead()
}

// OnMobEvent marks golems built by players (CarvedPumpkinBlock: the
// "minecraft:from_player" spawn event sets PlayerCreated).
func (g *IronGolem) OnMobEvent(_ *javamob.Mob, event string) {
	if event == "minecraft:from_player" {
		g.PlayerCreated = true
	}
}
