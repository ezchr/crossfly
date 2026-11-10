//go:build crossfly_full

package animal

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

// The goals have Java's Goal methods with the host as parameter: CanUse, CanContinueToUse, Start, Stop,
// Tick, plus Flags. None of them requires an update every tick or is uninterruptible, so the selector
// ticks them every other tick (Mob.serverAiStep) and AdjustedTickDelay halves their delays.

func distSq(a, b mgl64.Vec3) float64 { return a.Sub(b).LenSqr() }

// BreedGoal is net.minecraft.world.entity.ai.goal.BreedGoal: an animal in love walks to the nearest
// partner of its class that can mate within 8 blocks and, after 60 ticks of love time, breeds when
// closer than 3 blocks.
type BreedGoal[H Host[H]] struct {
	Speed float64
	// CanMate is Animal.canMate between the host and a candidate of the same class: nil is the default
	// (both in love). Types override it (wolves: both tame and not sitting...).
	CanMate func(self, partner H) bool

	partner  H
	has      bool
	loveTime int
}

// Flags is MOVE and LOOK.
func (g *BreedGoal[H]) Flags() GoalFlags { return FlagMove | FlagLook }

func (g *BreedGoal[H]) canMate(h, p H) bool {
	if g.CanMate != nil {
		return g.CanMate(h, p)
	}
	return h.AnimalState().CanMate(p.AnimalState())
}

// CanUse is BreedGoal.canUse.
func (g *BreedGoal[H]) CanUse(h H) bool {
	if !h.AnimalState().InLove() {
		return false
	}
	g.partner, g.has = g.freePartner(h)
	return g.has
}

// freePartner is BreedGoal.getFreePartner: PARTNER_TARGETING (non-combat, range 8, no line of sight) over
// the same-class animals in the box inflated by 8; the nearest one that can mate and is not panicking.
func (g *BreedGoal[H]) freePartner(h H) (H, bool) {
	var best H
	found := false
	dist := math.MaxFloat64
	pos := h.Position()
	for _, p := range h.SameTypeInBox(8, 8, 8) {
		if p.Self() == h.Self() || !p.Alive() {
			continue
		}
		d := distSq(pos, p.Position())
		if d > 64 {
			continue // TargetingConditions range 8
		}
		if g.canMate(h, p) && !p.Panicking() && d < dist {
			best, found, dist = p, true, d
		}
	}
	return best, found
}

// CanContinueToUse is BreedGoal.canContinueToUse.
func (g *BreedGoal[H]) CanContinueToUse(_ H) bool {
	p := g.partner
	return g.has && p.Alive() && p.AnimalState().InLove() && g.loveTime < 60 && !p.Panicking()
}

// Start does nothing (Goal.start).
func (g *BreedGoal[H]) Start(H) {}

// Stop is BreedGoal.stop.
func (g *BreedGoal[H]) Stop(H) {
	var zero H
	g.partner, g.has, g.loveTime = zero, false, 0
}

// Tick is BreedGoal.tick.
func (g *BreedGoal[H]) Tick(h H) {
	if !g.has {
		return
	}
	p := g.partner
	h.LookAt(p.Self(), 10, h.MaxHeadXRot())
	h.MoveToEntity(p.Self(), g.Speed)
	g.loveTime++
	if g.loveTime >= AdjustedTickDelay(false, 60) && distSq(h.Position(), p.Position()) < 9 {
		SpawnChildFromBreeding(h, p)
	}
}

// SpawnChildFromBreeding is BreedGoal.breed -> Animal.spawnChildFromBreeding and
// finalizeSpawnChildFromBreeding: the baby (if the class has one), both parents on 5 minute cooldown and
// out of love, hearts on h and 1 to 7 experience if the mobDrops rule is on.
func SpawnChildFromBreeding[H Host[H]](h, partner H) {
	if !h.BreedOffspring(partner) {
		return
	}
	h.AnimalState().FinishBreeding()
	partner.AnimalState().FinishBreeding()
	h.BroadcastEvent(EventLoveHearts)
	if h.MobDrops() {
		h.DropXP(BreedingXP(h.Rand()))
	}
}

// TemptGoal is net.minecraft.world.entity.ai.goal.TemptGoal: the mob follows the nearest player holding
// one of its items (main or off hand) within its tempt range, stopping 2.5 blocks away. After it stops it
// waits 100 ticks (50 goal ticks) before it can be tempted again. A scared mob (CanScare: ocelots, cats)
// gives up when the player moves or turns while it is close.
type TemptGoal[H Host[H]] struct {
	Speed float64
	Items func(item.Stack) bool
	// CanScare is the canScare argument.
	CanScare bool
	// StopDistance is 2.5 unless the mob sets its own.
	StopDistance float64

	player       world.Entity
	px, py, pz   float64
	pRotX, pRotY float64
	calmDown     int
	running      bool
}

// NewTemptGoal is TemptGoal(mob, speed, items, canScare) with the default stop distance 2.5.
func NewTemptGoal[H Host[H]](speed float64, items func(item.Stack) bool, canScare bool) *TemptGoal[H] {
	return &TemptGoal[H]{Speed: speed, Items: items, CanScare: canScare, StopDistance: 2.5}
}

// Flags is MOVE and LOOK.
func (g *TemptGoal[H]) Flags() GoalFlags { return FlagMove | FlagLook }

// Running is TemptGoal.isRunning.
func (g *TemptGoal[H]) Running() bool { return g.running }

// Player is the player being followed (nil if none).
func (g *TemptGoal[H]) Player() world.Entity { return g.player }

// CanUse is TemptGoal.canUse.
func (g *TemptGoal[H]) CanUse(h H) bool {
	if g.calmDown > 0 {
		g.calmDown--
		return false
	}
	g.player = h.NearestPlayer(h.TemptRange(), func(p world.Entity) bool { return HoldsItem(p, g.Items) })
	return g.player != nil
}

// CanContinueToUse is TemptGoal.canContinueToUse.
func (g *TemptGoal[H]) CanContinueToUse(h H) bool {
	if g.CanScare && g.player != nil {
		pp := g.player.Position()
		rot := g.player.Rotation()
		if distSq(h.Position(), pp) < 36 {
			if distSq(pp, mgl64.Vec3{g.px, g.py, g.pz}) > 0.010000000000000002 {
				return false
			}
			if math.Abs(rot.Pitch()-g.pRotX) > 5 || math.Abs(rot.Yaw()-g.pRotY) > 5 {
				return false
			}
		} else {
			g.px, g.py, g.pz = pp[0], pp[1], pp[2]
		}
		g.pRotX, g.pRotY = rot.Pitch(), rot.Yaw()
	}
	return g.CanUse(h)
}

// Start is TemptGoal.start.
func (g *TemptGoal[H]) Start(H) {
	if g.player != nil {
		p := g.player.Position()
		g.px, g.py, g.pz = p[0], p[1], p[2]
	}
	g.running = true
}

// Stop is TemptGoal.stop.
func (g *TemptGoal[H]) Stop(h H) {
	g.player = nil
	h.StopNavigation()
	g.calmDown = ReducedTickDelay(100)
	g.running = false
}

// Tick is TemptGoal.tick.
func (g *TemptGoal[H]) Tick(h H) {
	if g.player == nil {
		return
	}
	h.LookAt(g.player, h.MaxHeadYRot()+20, h.MaxHeadXRot())
	if distSq(h.Position(), g.player.Position()) < g.StopDistance*g.StopDistance {
		h.StopNavigation()
	} else {
		h.MoveToEntity(g.player, g.Speed)
	}
}

// FollowParentGoal is net.minecraft.world.entity.ai.goal.FollowParentGoal: a baby walks to the nearest
// adult of its class within 8 blocks (4 up/down) once it is 3 blocks away, and gives up past 16.
type FollowParentGoal[H Host[H]] struct {
	Speed float64

	parent           H
	has              bool
	timeToRecalcPath int
}

// Flags is none: following runs alongside other movement goals' flags (the goal sets no flags in Java).
func (g *FollowParentGoal[H]) Flags() GoalFlags { return 0 }

// CanUse is FollowParentGoal.canUse.
func (g *FollowParentGoal[H]) CanUse(h H) bool {
	if h.AnimalState().Age() >= 0 {
		return false
	}
	var closest H
	found := false
	best := math.MaxFloat64
	pos := h.Position()
	for _, p := range h.SameTypeInBox(8, 4, 8) {
		if p.AnimalState().Age() < 0 {
			continue
		}
		d := distSq(pos, p.Position())
		if !(d > best) {
			best, closest, found = d, p, true
		}
	}
	if !found || best < 9 {
		return false
	}
	g.parent, g.has = closest, true
	return true
}

// CanContinueToUse is FollowParentGoal.canContinueToUse.
func (g *FollowParentGoal[H]) CanContinueToUse(h H) bool {
	if h.AnimalState().Age() >= 0 || !g.has || !g.parent.Alive() {
		return false
	}
	d := distSq(h.Position(), g.parent.Position())
	return !(d < 9) && !(d > 256)
}

// Start is FollowParentGoal.start.
func (g *FollowParentGoal[H]) Start(H) { g.timeToRecalcPath = 0 }

// Stop is FollowParentGoal.stop.
func (g *FollowParentGoal[H]) Stop(H) {
	var zero H
	g.parent, g.has = zero, false
}

// Tick is FollowParentGoal.tick.
func (g *FollowParentGoal[H]) Tick(h H) {
	g.timeToRecalcPath--
	if g.timeToRecalcPath <= 0 && g.has {
		g.timeToRecalcPath = AdjustedTickDelay(false, 10)
		h.MoveToEntity(g.parent.Self(), g.Speed)
	}
}

// PanicGoal is net.minecraft.world.entity.ai.goal.PanicGoal: after damage of a panic type (the last 40
// ticks) the mob runs to a random spot within 5 blocks (4 up/down), or to water within 5 blocks when it
// burns, until its path is done.
type PanicGoal[H Host[H]] struct {
	Speed float64
	// ShouldPanic replaces PanicGoal.shouldPanic (nil: the last damage type is in PanicCauses).
	ShouldPanic func(h H) bool

	target  mgl64.Vec3
	running bool
}

// Flags is MOVE.
func (g *PanicGoal[H]) Flags() GoalFlags { return FlagMove }

// Running is PanicGoal.isRunning (PathfinderMob.isPanicking reads it).
func (g *PanicGoal[H]) Running() bool { return g.running }

func (g *PanicGoal[H]) shouldPanic(h H) bool {
	if g.ShouldPanic != nil {
		return g.ShouldPanic(h)
	}
	_, ok := PanicCauses[h.LastDamageType()]
	return ok
}

// CanUse is PanicGoal.canUse.
func (g *PanicGoal[H]) CanUse(h H) bool {
	if !g.shouldPanic(h) {
		return false
	}
	if h.OnFire() {
		if p, ok := LookForWater(h.Tx(), h.Position(), 5); ok {
			g.target = mgl64.Vec3{float64(p[0]), float64(p[1]), float64(p[2])}
			return true
		}
	}
	p, ok := h.DefaultRandomPos(5, 4)
	if !ok {
		return false
	}
	g.target = p
	return true
}

// Start is PanicGoal.start.
func (g *PanicGoal[H]) Start(h H) {
	h.MoveTo(g.target, g.Speed)
	g.running = true
}

// Stop is PanicGoal.stop.
func (g *PanicGoal[H]) Stop(H) { g.running = false }

// CanContinueToUse is PanicGoal.canContinueToUse.
func (g *PanicGoal[H]) CanContinueToUse(h H) bool { return !h.NavigationDone() }

// Tick does nothing (Goal.tick).
func (g *PanicGoal[H]) Tick(H) {}

// LookForWater is PanicGoal.lookForWater: nil when the mob stands in a block with a collision shape, else
// the first water block (by Manhattan distance, Java's order) within xz blocks and 1 up/down.
func LookForWater(tx *world.Tx, pos mgl64.Vec3, xz int) (cube.Pos, bool) {
	origin := cube.PosFromVec3(pos)
	if len(tx.Block(origin).Model().BBox(origin, tx)) != 0 {
		return cube.Pos{}, false
	}
	var found cube.Pos
	ok := false
	ManhattanOrdered(origin, xz, 1, xz, xz+1+xz, func(p cube.Pos) bool {
		if isWater(tx, p) {
			found, ok = p, true
			return false
		}
		return true
	})
	return found, ok
}

// isWater is FluidState.is(FluidTags.WATER) at p: water as the block or its liquid layer.
func isWater(tx *world.Tx, p cube.Pos) bool {
	if l, ok := tx.Liquid(p); ok {
		_, w := l.(block.Water)
		return w
	}
	return false
}

// EventEatGrass is EntityEvent.EAT_GRASS (10): the sheep lowers its head for 40 ticks.
const EventEatGrass byte = 10

// EatBlockGoal is net.minecraft.world.entity.ai.goal.EatBlockGoal (sheep): now and then (1 in 1000 goal
// ticks for adults, 1 in 50 for babies) the mob eats the grass plant it stands in or turns the grass block
// below into dirt, with the 40 tick head animation.
type EatBlockGoal[H Host[H]] struct {
	eatAnimationTick int
}

// Flags is MOVE, LOOK and JUMP.
func (g *EatBlockGoal[H]) Flags() GoalFlags { return FlagMove | FlagLook | FlagJump }

// EatAnimationTick is EatBlockGoal.getEatAnimationTick.
func (g *EatBlockGoal[H]) EatAnimationTick() int { return g.eatAnimationTick }

// EdibleForSheep is the block tag minecraft:edible_for_sheep: short grass, short and tall dry grass, fern.
func EdibleForSheep(b world.Block) bool {
	if b == nil {
		return false
	}
	name, _ := b.EncodeBlock()
	switch name {
	case "minecraft:short_grass", "minecraft:short_dry_grass", "minecraft:tall_dry_grass", "minecraft:fern":
		return true
	}
	return false
}

func isGrassBlock(b world.Block) bool {
	_, ok := b.(block.Grass)
	return ok
}

// CanUse is EatBlockGoal.canUse.
func (g *EatBlockGoal[H]) CanUse(h H) bool {
	n := 1000
	if h.AnimalState().Baby() {
		n = 50
	}
	if h.Rand().IntN(AdjustedTickDelay(false, n)) != 0 {
		return false
	}
	pos := cube.PosFromVec3(h.Position())
	tx := h.Tx()
	return EdibleForSheep(tx.Block(pos)) || isGrassBlock(tx.Block(pos.Side(cube.FaceDown)))
}

// Start is EatBlockGoal.start.
func (g *EatBlockGoal[H]) Start(h H) {
	g.eatAnimationTick = AdjustedTickDelay(false, 40)
	h.BroadcastEvent(EventEatGrass)
	h.StopNavigation()
}

// Stop is EatBlockGoal.stop.
func (g *EatBlockGoal[H]) Stop(H) { g.eatAnimationTick = 0 }

// CanContinueToUse is EatBlockGoal.canContinueToUse.
func (g *EatBlockGoal[H]) CanContinueToUse(H) bool { return g.eatAnimationTick > 0 }

// Tick is EatBlockGoal.tick: the bite comes at tick 4 of the countdown (2 goal ticks before the end).
func (g *EatBlockGoal[H]) Tick(h H) {
	g.eatAnimationTick = max(0, g.eatAnimationTick-1)
	if g.eatAnimationTick != AdjustedTickDelay(false, 4) {
		return
	}
	pos := cube.PosFromVec3(h.Position())
	tx := h.Tx()
	if b := tx.Block(pos); EdibleForSheep(b) {
		if h.MobGriefing() {
			// Level.destroyBlock(pos, false): break particles and sound, no drops.
			tx.AddParticle(pos.Vec3Centre(), breakParticle(b))
			tx.SetBlock(pos, nil, nil)
		}
		h.Ate()
		return
	}
	below := pos.Side(cube.FaceDown)
	if b := tx.Block(below); isGrassBlock(b) {
		if h.MobGriefing() {
			// levelEvent 2001 (block break particles of grass) and grass -> dirt with flag 2.
			tx.AddParticle(below.Vec3Centre(), breakParticle(b))
			tx.SetBlock(below, block.Dirt{}, nil)
		}
		h.Ate()
	}
}

// breakParticle is level event 2001 (LevelEvent.PARTICLES_DESTROY_BLOCK): break particles and sound.
func breakParticle(b world.Block) world.Particle { return particle.BlockBreak{Block: b} }
