//go:build crossfly_full

package animal

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Host is what the animal goals need from a mob of the Java mob base, H being the mob's own (opened)
// type. It is a small adapter: everything maps onto one Java method, named in each comment.
//
// Goals take the host as a parameter on every call (they hold no *Mob), so one goal value lives in the
// mob's state between transactions.
type Host[H any] interface {
	// AnimalState is the mob's Animal state.
	AnimalState() *Animal
	// Self is the mob as an entity (navigation and look targets of other animals).
	Self() world.Entity
	Tx() *world.Tx
	Rand() *rand.Rand
	Position() mgl64.Vec3
	// Alive is LivingEntity.isAlive.
	Alive() bool
	// OnFire is Entity.isOnFire.
	OnFire() bool
	// Panicking is PathfinderMob.isPanicking: a running PanicGoal (or the IS_PANICKING memory).
	Panicking() bool
	// LastDamageType is the Java damage type id of LivingEntity.getLastDamageSource() (kept 40 ticks),
	// "" if there is none.
	LastDamageType() string

	// MoveToEntity is PathNavigation.moveTo(entity, speed).
	MoveToEntity(e world.Entity, speed float64) bool
	// MoveTo is PathNavigation.moveTo(x, y, z, speed).
	MoveTo(pos mgl64.Vec3, speed float64) bool
	// StopNavigation is PathNavigation.stop.
	StopNavigation()
	// NavigationDone is PathNavigation.isDone.
	NavigationDone() bool
	// LookAt is LookControl.setLookAt(entity, maxYaw, maxPitch).
	LookAt(e world.Entity, maxYaw, maxPitch float64)
	// MaxHeadXRot and MaxHeadYRot are Mob.getMaxHeadXRot (40) and getMaxHeadYRot (75) or the mob's own.
	MaxHeadXRot() float64
	MaxHeadYRot() float64
	// DefaultRandomPos is DefaultRandomPos.getPos(mob, xz, y).
	DefaultRandomPos(xz, y int) (mgl64.Vec3, bool)

	// SameTypeInBox lists the living mobs of the host's class (Level.getEntitiesOfClass(getClass(), box))
	// whose bounding box touches the host's inflated by dx, dy, dz, the host included.
	SameTypeInBox(dx, dy, dz float64) []H
	// NearestPlayer is ServerLevel.getNearestPlayer(TargetingConditions.forNonCombat()
	// .ignoreLineOfSight().range(r).selector(pred), mob): the nearest player that can be seen by anyone
	// and is within max(r * LivingEntity.getVisibilityPercent, 2) blocks, nil if there is none.
	NearestPlayer(r float64, pred func(p world.Entity) bool) world.Entity
	// TemptRange is the TEMPT_RANGE attribute (10 for animals).
	TemptRange() float64

	// BroadcastEvent is Level.broadcastEntityEvent(mob, id).
	BroadcastEvent(id byte)
	// MobGriefing and MobDrops are the game rules.
	MobGriefing() bool
	MobDrops() bool
	// DropXP adds an experience orb of n at the mob (Animal.finalizeSpawnChildFromBreeding).
	DropXP(n int)
	// BreedOffspring is the class's getBreedOffspring + Animal.spawnChildFromBreeding: it creates the
	// baby (variants from the parents), makes it a baby (setBaby(true)), places it at the host with
	// rotation 0, 0 and adds it with its passengers. False when there is no offspring.
	BreedOffspring(partner H) bool
	// Ate is Mob.ate (a sheep grows its wool back) after EatBlockGoal.
	Ate()
}

// GoalFlags are the Goal.Flag set of a goal (bit = 1 << ordinal: MOVE, LOOK, JUMP, TARGET).
type GoalFlags uint8

const (
	FlagMove GoalFlags = 1 << iota
	FlagLook
	FlagJump
	FlagTarget
)

// ReducedTickDelay is Goal.reducedTickDelay: half the ticks, rounded up (goals that do not need an update
// every tick run every other tick).
func ReducedTickDelay(ticks int) int { return (ticks + 1) / 2 }

// AdjustedTickDelay is Goal.adjustedTickDelay for a goal that does (everyTick) or does not require an
// update every tick.
func AdjustedTickDelay(everyTick bool, ticks int) int {
	if everyTick {
		return ticks
	}
	return ReducedTickDelay(ticks)
}

// PanicCauses is the damage type tag minecraft:panic_causes (with panic_environmental_causes and
// is_player_attack expanded): damage of these types makes PanicGoal run.
var PanicCauses = map[string]struct{}{}

func init() {
	for _, n := range []string{"cactus", "freeze", "hot_floor", "sulfur_cube_hot", "in_fire", "lava", "lightning_bolt",
		"on_fire", "arrow", "dragon_breath", "explosion", "fireball", "fireworks", "indirect_magic", "magic", "mob_attack",
		"mob_projectile", "player_explosion", "sonic_boom", "sting", "thrown", "trident", "unattributed_fireball",
		"wind_charge", "wither", "wither_skull", "player_attack", "spear", "mace_smash"} {
		PanicCauses[n] = struct{}{}
		PanicCauses["minecraft:"+n] = struct{}{}
	}
}

// heldItems is a player's main and off hand.
type heldItems interface {
	HeldItems() (mainHand, offHand item.Stack)
}

// HoldsItem reports whether p holds an item matching pred in either hand (TemptGoal.shouldFollow).
func HoldsItem(p world.Entity, pred func(item.Stack) bool) bool {
	h, ok := p.(heldItems)
	if !ok {
		return false
	}
	main, off := h.HeldItems()
	return pred(main) || pred(off)
}

// StackIn returns a predicate matching stacks whose item is in one of the tags.
func StackIn(tags ...ItemTag) func(item.Stack) bool {
	return func(s item.Stack) bool {
		if s.Empty() {
			return false
		}
		for _, t := range tags {
			if t.Has(s.Item()) {
				return true
			}
		}
		return false
	}
}

// ManhattanOrdered is BlockPos.manhattanOrdered: the positions around origin within rx, ry, rz in order of
// Manhattan distance (up to maxDepth), in Java's exact order, until yield returns false.
// BlockPos.withinBoxByManhattanDistance(origin, rx, ry, rz) uses maxDepth = rx + ry + rz.
func ManhattanOrdered(origin cube.Pos, rx, ry, rz, maxDepth int, yield func(cube.Pos) bool) {
	for depth := 0; depth <= maxDepth; depth++ {
		maxX := min(rx, depth)
		for x := -maxX; x <= maxX; x++ {
			maxY := min(ry, depth-abs(x))
			for y := -maxY; y <= maxY; y++ {
				z := depth - abs(x) - abs(y)
				if z > rz {
					continue
				}
				if !yield(cube.Pos{origin[0] + x, origin[1] + y, origin[2] + z}) {
					return
				}
				if z != 0 && !yield(cube.Pos{origin[0] + x, origin[1] + y, origin[2] - z}) {
					return
				}
			}
		}
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
