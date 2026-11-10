//go:build crossfly_full

package hostile

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

const dolphinID = "minecraft:dolphin"

// Dolphin constants (Dolphin).
const (
	DolphinMaxAir       = 4800
	dolphinMaxMoistness = 2400
)

// Dolphin is the per-mob state of a dolphin (Dolphin).
type Dolphin struct {
	// gotFish is GOT_FISH: fed a fish, it leads to treasure.
	gotFish bool
	// moistness is MOISTNESS_LEVEL: out of water and rain it dries out.
	moistness int
	// air is the air supply: Dolphin.getMaxAirSupply is 4800 and it refills
	// at once at the surface (increaseAirSupply).
	air int
	// treasure is treasurePos.
	treasure *cube.Pos
}

// dolphinOf returns the dolphin state of m.
func dolphinOf(m *mob.Mob) *Dolphin {
	return stateOf(m, func() *Dolphin { return &Dolphin{moistness: dolphinMaxMoistness, air: DolphinMaxAir} })
}

// DolphinAttributes are Dolphin.createAttributes: health, speed, attack.
func DolphinAttributes() (health, speed, attack float64) { return 10, float64(float32(1.2)), 3 }

// GotFish is Dolphin.gotFish.
func (d *Dolphin) GotFish() bool { return d.gotFish }

// Moistness is Dolphin.getMoistnessLevel.
func (d *Dolphin) Moistness() int { return d.moistness }

// Air is the dolphin's air supply.
func (d *Dolphin) Air() int { return d.air }

// DolphinBaseTick is the air part of LivingEntity.baseTick for a dolphin
// (it cannot breathe under water; Dolphin.handleAirSupply does nothing and
// increaseAirSupply refills at once): with the eyes under water the air runs
// down by one a tick and from -20 the dolphin takes 2 drowning damage every
// 20 ticks; at the surface it is full again.
func DolphinBaseTick(m *mob.Mob, noAI bool, drown func(m *mob.Mob, dmg float64)) {
	d := dolphinOf(m)
	if noAI {
		d.air = DolphinMaxAir
		return
	}
	if m.Dead() {
		return
	}
	if !m.Physics().Underwater {
		d.air = DolphinMaxAir
		return
	}
	d.air--
	if d.air <= -20 {
		d.air = 0
		drown(m, 2)
	}
}

// DolphinTick is the server part of Dolphin.tick after the base tick: wet
// in water or rain, drying out (1 damage a tick once dry) otherwise, and
// flopping on land.
func DolphinTick(m *mob.Mob, inWaterOrRain bool, dryOut func(m *mob.Mob)) {
	d := dolphinOf(m)
	if inWaterOrRain {
		d.moistness = dolphinMaxMoistness
		return
	}
	d.moistness--
	if d.moistness <= 0 {
		dryOut(m)
	}
	if m.OnGround() {
		r := m.Rand()
		m.SetVelocity(m.Vel().Add(mgl64.Vec3{(nextFloat(r)*2 - 1) * 0.2, 0.5, (nextFloat(r)*2 - 1) * 0.2}))
		m.SetYaw(nextFloat(r) * 360)
		m.Physics().OnGround = false
		m.SyncMovement()
	}
}

// DolphinTravelInWater is Dolphin.travelInWater: the input at the mob's
// speed, 0.9 drag, sinking slowly without a target.
func DolphinTravelInWater(m *mob.Mob, input mgl64.Vec3, speed float64) {
	v := m.Vel().Add(inputVector(input, speed, m.Yaw()))
	v = Hooks.Move(m, v).Mul(0.9)
	if _, ok := m.Target(); !ok {
		v[1] -= 0.005
	}
	m.SetVelocity(v)
}

// fishItem reports whether s is in #minecraft:fishes (Dolphin.mobInteract).
func fishItem(s item.Stack) bool {
	if s.Empty() {
		return false
	}
	name, _ := s.Item().EncodeItem()
	switch name {
	case "minecraft:cod", "minecraft:cooked_cod", "minecraft:salmon", "minecraft:cooked_salmon",
		"minecraft:pufferfish", "minecraft:tropical_fish":
		return true
	}
	return false
}

// DolphinInteract is Dolphin.mobInteract with a fish: a baby that may grow
// grows up faster, an adult gets the fish (and will try to lead to
// treasure). It reports whether the fish was used.
func DolphinInteract(m *mob.Mob, held item.Stack, canAgeUp bool, ageUp func()) bool {
	if !fishItem(held) {
		return false
	}
	if canAgeUp {
		ageUp()
		return true
	}
	dolphinOf(m).gotFish = true
	return true
}

// DolphinCanAttack is Dolphin.canAttack: babies never attack.
func DolphinCanAttack(m *mob.Mob) bool { return !m.Baby() }

// treasureFinder is ServerLevel.findNearestMapStructure(#dolphin_located,
// pos, 50, false). Crossfly has no generated ocean ruins or shipwrecks yet,
// so it finds none (decided 2026-11-10: no treasure leading until real world
// generation); the goal then gives the fish up at once, as vanilla does
// where there is no treasure.
var treasureFinder = func(tx *world.Tx, pos cube.Pos) (cube.Pos, bool) { return cube.Pos{}, false }

// dolphinTreasureGoal is Dolphin.DolphinSwimToTreasureGoal (priority 1,
// MOVE|LOOK, not interruptible). Without structures it only clears the fish.
type dolphinTreasureGoal struct {
	stuck bool
}

func (g *dolphinTreasureGoal) Flags() mob.ControlFlags { return mob.FlagMove | mob.FlagLook }

func (g *dolphinTreasureGoal) IsInterruptable() bool { return false }

func (g *dolphinTreasureGoal) CanUse(m *mob.Mob) bool {
	d := dolphinOf(m)
	return d.gotFish && d.air >= 100
}

func (g *dolphinTreasureGoal) CanContinueToUse(m *mob.Mob) bool {
	d := dolphinOf(m)
	if d.treasure == nil {
		return false
	}
	return !g.closeToTreasure(m) && !g.stuck && d.air >= 100
}

func (g *dolphinTreasureGoal) closeToTreasure(m *mob.Mob) bool {
	t := dolphinOf(m).treasure
	c := mgl64.Vec3{float64(t[0]) + 0.5, math.Floor(m.Pos()[1]) + 0.5, float64(t[2]) + 0.5}
	return c.Sub(m.Pos()).LenSqr() < 16
}

func (g *dolphinTreasureGoal) Start(m *mob.Mob) {
	g.stuck = false
	m.Navigation().Stop(m)
	if p, ok := treasureFinder(m.Tx(), blockPos(m.Pos())); ok {
		dolphinOf(m).treasure = &p
		return
	}
	g.stuck = true
}

func (g *dolphinTreasureGoal) Stop(m *mob.Mob) {
	d := dolphinOf(m)
	if d.treasure == nil || g.closeToTreasure(m) || g.stuck {
		d.gotFish = false
	}
}

// dolphinSwimWithPlayerGoal is Dolphin.DolphinSwimWithPlayerGoal (priority
// 2, MOVE|LOOK): swim alongside the nearest swimming player within 10 blocks
// (no line of sight needed) and give them Dolphin's Grace for 5 s, renewed
// one tick in six.
type dolphinSwimWithPlayerGoal struct {
	speed  float64
	player *world.EntityHandle
}

func (g *dolphinSwimWithPlayerGoal) Flags() mob.ControlFlags { return mob.FlagMove | mob.FlagLook }

func (g *dolphinSwimWithPlayerGoal) playerEntity(m *mob.Mob) (world.Entity, bool) {
	if g.player == nil {
		return nil, false
	}
	return g.player.Entity(m.Tx())
}

func (g *dolphinSwimWithPlayerGoal) CanUse(m *mob.Mob) bool {
	g.player = nil
	cond := Targeting{Range: 10, IgnoreLineOfSight: true}
	var best world.Entity
	bestDist := math.MaxFloat64
	for e := range m.Tx().Players() {
		if !cond.Test(m, e) {
			continue
		}
		if d := distanceSqr(m, e); d < bestDist {
			best, bestDist = e, d
		}
	}
	if best == nil {
		return false
	}
	g.player = best.H()
	t := m.TargetHandle()
	return swimming(best) && t != best.H()
}

func (g *dolphinSwimWithPlayerGoal) CanContinueToUse(m *mob.Mob) bool {
	p, ok := g.playerEntity(m)
	return ok && swimming(p) && distanceSqr(m, p) < 256
}

func (g *dolphinSwimWithPlayerGoal) Start(m *mob.Mob) {
	if p, ok := g.playerEntity(m); ok {
		giveGrace(p)
	}
}

func (g *dolphinSwimWithPlayerGoal) Stop(m *mob.Mob) {
	g.player = nil
	m.Navigation().Stop(m)
}

func (g *dolphinSwimWithPlayerGoal) Tick(m *mob.Mob) {
	p, ok := g.playerEntity(m)
	if !ok {
		return
	}
	m.LookControl().SetLookAtEntity(p, 1+20, 1) // getMaxHeadYRot() + 20, getMaxHeadXRot()
	if distanceSqr(m, p) < 6.25 {
		m.Navigation().Stop(m)
	} else {
		m.Navigation().MoveToEntity(m, p, g.speed)
	}
	if swimming(p) && nextInt(m.Rand(), 6) == 0 {
		giveGrace(p)
	}
}

// swimming is Entity.isSwimming: a player in the swimming pose.
func swimming(e world.Entity) bool {
	s, ok := e.(interface{ Swimming() bool })
	return ok && s.Swimming()
}

// giveGrace is addEffect(DOLPHINS_GRACE, 100 ticks).
func giveGrace(e world.Entity) {
	if l, ok := e.(interface{ AddEffect(effect.Effect) }); ok {
		l.AddEffect(effect.New(DolphinsGrace, 1, 5*time.Second))
	}
}

// DolphinsGrace is the Dolphin's Grace effect type (the base or the effect
// package sets it; Dragonfly has no such effect of its own).
var DolphinsGrace effect.LastingType = effect.Speed

// dolphinItems are the item entities a dolphin goes for: in water, not
// waiting to be picked up (Dolphin.ALLOWED_ITEMS), within 8 blocks.
func dolphinItems(m *mob.Mob) []*entity.Ent {
	var out []*entity.Ent
	for e := range m.Tx().EntitiesWithin(m.BBox().Grow(8)) {
		it, ok := e.(*entity.Ent)
		if !ok {
			continue
		}
		if _, ok := it.Behaviour().(*entity.ItemBehaviour); !ok || ItemPickupDelayed(it) {
			continue
		}
		if _, water := m.Tx().Liquid(blockPos(it.Position())); !water {
			continue
		}
		out = append(out, it)
	}
	return out
}

// ItemPickupDelayed is ItemEntity.hasPickUpDelay (the base sets it; the
// default treats every item as ready).
var ItemPickupDelayed = func(it *entity.Ent) bool { return false }

// dolphinMoveToItemGoal is Dolphin.MoveToItemGoal (priority 7, MOVE): swim to
// an item in the water (picking it up is pickUpItem); stopping throws the
// held item away and waits up to 5 s.
type dolphinMoveToItemGoal struct {
	cooldown int64
}

func (g *dolphinMoveToItemGoal) Flags() mob.ControlFlags { return mob.FlagMove }

func (g *dolphinMoveToItemGoal) CanUse(m *mob.Mob) bool {
	if g.cooldown > m.Ticks() {
		return false
	}
	return len(dolphinItems(m)) > 0
}

func (g *dolphinMoveToItemGoal) Start(m *mob.Mob) {
	if items := dolphinItems(m); len(items) > 0 {
		m.Navigation().MoveToEntity(m, items[0], 1.2)
	}
	g.cooldown = 0
}

func (g *dolphinMoveToItemGoal) Tick(m *mob.Mob) {
	if items := dolphinItems(m); len(items) > 0 {
		m.Navigation().MoveToEntity(m, items[0], 1.2)
	}
}

func (g *dolphinMoveToItemGoal) Stop(m *mob.Mob) {
	if dolphinDropItem(m) {
		g.cooldown = m.Ticks() + int64(nextInt(m.Rand(), 100))
	}
}

// dolphinPlayGoal is Dolphin.PlayWithItemsGoal (priority 8, no flags): a
// held item is thrown away at once.
type dolphinPlayGoal struct{}

func (dolphinPlayGoal) Flags() mob.ControlFlags { return 0 }

func (dolphinPlayGoal) CanUse(m *mob.Mob) bool {
	main, _ := m.HeldItems()
	return !main.Empty()
}

func (dolphinPlayGoal) Tick(m *mob.Mob) { dolphinDropItem(m) }

func (dolphinPlayGoal) Stop(m *mob.Mob) { dolphinDropItem(m) }

// DropItemEntity adds a thrown item entity (the base sets it: Java's
// ItemEntity with a pickup delay and thrower).
var DropItemEntity = func(tx *world.Tx, pos, vel mgl64.Vec3, s item.Stack, pickupDelay int, thrower world.Entity) {
	opts := world.EntitySpawnOpts{Position: pos, Velocity: vel}
	tx.AddEntity(opts.New(entity.ItemType, entity.ItemBehaviourConfig{Item: s, PickupDelay: time.Duration(pickupDelay) * time.Second / 20}))
}

// dolphinDropItem is Dolphin.ItemGoal.dropItem/drop: throw the held item
// forward (0.3 along the look, a little random spread), 40 ticks before it
// can be picked up.
func dolphinDropItem(m *mob.Mob) bool {
	main, off := m.HeldItems()
	if main.Empty() {
		return false
	}
	r := m.Rand()
	yaw, pitch := m.Yaw()*math.Pi/180, m.Pitch()*math.Pi/180
	dir := nextFloat(r) * 2 * math.Pi
	pow2 := 0.02 * nextFloat(r)
	vel := mgl64.Vec3{
		0.3*-math.Sin(yaw)*math.Cos(pitch) + math.Cos(dir)*pow2,
		0.3 * math.Sin(pitch) * 1.5,
		0.3*math.Cos(yaw)*math.Cos(pitch) + math.Sin(dir)*pow2,
	}
	pos := m.Pos()
	DropItemEntity(m.Tx(), mgl64.Vec3{pos[0], m.EyePosition()[1] - 0.3, pos[2]}, vel, main, 40, m)
	m.SetHeldItems(item.Stack{}, off)
	return true
}

// DolphinPickUpItem is Dolphin.pickUpItem: with an empty mouth the dolphin
// takes the whole stack (it always drops when killed). It reports whether
// it took it.
func DolphinPickUpItem(m *mob.Mob, s item.Stack) bool {
	main, off := m.HeldItems()
	if !main.Empty() {
		return false
	}
	m.SetHeldItems(s, off)
	return true
}

// dolphinJumpGoal is DolphinJumpGoal (priority 5, interval 10 -> 1 in 5
// checks, MOVE|JUMP, not interruptible): leap out of clear water ahead.
type dolphinJumpGoal struct {
	interval int
	breached bool
}

// dolphinJumpSteps is DolphinJumpGoal.STEPS_TO_CHECK.
var dolphinJumpSteps = []int{0, 1, 4, 5, 6, 7}

func (g *dolphinJumpGoal) Flags() mob.ControlFlags { return mob.FlagMove | mob.FlagJump }

func (g *dolphinJumpGoal) IsInterruptable() bool { return false }

func (g *dolphinJumpGoal) CanUse(m *mob.Mob) bool {
	if nextInt(m.Rand(), g.interval) != 0 {
		return false
	}
	step := directionStep(m.Yaw())
	base := blockPos(m.Pos())
	tx := m.Tx()
	for _, i := range dolphinJumpSteps {
		at := base.Add(cube.Pos{step[0] * i, 0, step[2] * i})
		liq, ok := tx.Liquid(at)
		if !ok || !isWaterLiquid(liq) {
			return false
		}
		if blocksMotion(tx, at) {
			return false
		}
		if !isAir(tx, at.Add(cube.Pos{0, 1, 0})) || !isAir(tx, at.Add(cube.Pos{0, 2, 0})) {
			return false
		}
	}
	return true
}

// blocksMotion is #blocks_dolphin_jump (= #blocks_motion) for the water
// check of the jump: the water must not hold a solid block (a waterlogged
// one); plain water and plants in it do not count.
func blocksMotion(tx *world.Tx, pos cube.Pos) bool {
	switch tx.Block(pos).(type) {
	case block.Water, block.Air:
		return false
	}
	return len(tx.Block(pos).Model().BBox(pos, tx)) > 0
}

// directionStep is Direction.getStepX/Z of the facing yaw.
func directionStep(yaw float64) cube.Pos {
	switch (cube.Rotation{yaw, 0}).Direction() {
	case cube.North:
		return cube.Pos{0, 0, -1}
	case cube.South:
		return cube.Pos{0, 0, 1}
	case cube.West:
		return cube.Pos{-1, 0, 0}
	}
	return cube.Pos{1, 0, 0}
}

func (g *dolphinJumpGoal) CanContinueToUse(m *mob.Mob) bool {
	yd := m.Vel()[1]
	pitch := m.Pitch()
	return (yd*yd >= 0.03 || pitch == 0 || math.Abs(pitch) >= 10 || !m.Physics().InWater) && !m.OnGround()
}

func (g *dolphinJumpGoal) Start(m *mob.Mob) {
	step := directionStep(m.Yaw())
	m.SetVelocity(m.Vel().Add(mgl64.Vec3{float64(step[0]) * 0.6, 0.7, float64(step[2]) * 0.6}))
	m.Navigation().Stop(m)
}

func (g *dolphinJumpGoal) Stop(m *mob.Mob) { m.SetPitch(0) }

func (g *dolphinJumpGoal) Tick(m *mob.Mob) {
	already := g.breached
	if !already {
		liq, ok := m.Tx().Liquid(blockPos(m.Pos()))
		g.breached = ok && isWaterLiquid(liq)
	}
	if g.breached && !already {
		playSound(m, "entity.dolphin.jump", 1, 1)
	}
	v := m.Vel()
	if v[1]*v[1] < 0.03 && m.Pitch() != 0 {
		m.SetPitch(m.Pitch() + wrapDegrees(0-m.Pitch())*0.2)
	} else if v.Len() > 1e-5 {
		m.SetPitch(math.Atan2(-v[1], horizontalDistance(v)) * 180 / math.Pi)
	}
}

// breathAirGoal is BreathAirGoal (priority 0, MOVE|LOOK, not
// interruptible): with under 140 air, swim to the nearest air above (or 8
// blocks up) and paddle there.
type breathAirGoal struct{}

func (breathAirGoal) Flags() mob.ControlFlags { return mob.FlagMove | mob.FlagLook }

func (breathAirGoal) IsInterruptable() bool { return false }

func (breathAirGoal) CanUse(m *mob.Mob) bool { return dolphinOf(m).air < 140 }

func (g breathAirGoal) Start(m *mob.Mob) {
	m.Navigation().Stop(m)
	g.findAirPosition(m)
}

func (g breathAirGoal) Tick(m *mob.Mob) {
	g.findAirPosition(m)
	in := m.Input()
	m.SetVelocity(m.Vel().Add(inputVector(mgl64.Vec3{in.Strafe, in.Vertical, in.Forward}, 0.02, m.Yaw())))
	m.SetVelocity(Hooks.Move(m, m.Vel()))
}

// findAirPosition is BreathAirGoal.findAirPosition: the first block from the
// mob up to 8 above (BlockPos.neighborColumn... a column above the mob)
// without fluid (or a bubble column) the mob could stand in.
func (breathAirGoal) findAirPosition(m *mob.Mob) {
	base := blockPos(m.Pos())
	tx := m.Tx()
	dest := base.Add(cube.Pos{0, 8, 0})
	for y := 0; y <= 8; y++ {
		at := base.Add(cube.Pos{0, y, 0})
		_, liquid := tx.Liquid(at)
		if !liquid && isAir(tx, at) {
			dest = at
			break
		}
	}
	m.Navigation().MoveTo(m, mgl64.Vec3{float64(dest[0]), float64(dest[1] + 1), float64(dest[2])}, 1)
}
