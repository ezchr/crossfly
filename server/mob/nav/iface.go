//go:build crossfly_full

package nav

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// BlockSource is the block access the pathfinder needs. *world.Tx satisfies it. It is also a
// world.BlockSource, so block models (fences, walls, panes) can look at their neighbours.
type BlockSource interface {
	Block(pos cube.Pos) world.Block
	Liquid(pos cube.Pos) (world.Liquid, bool)
}

// Optional extras of a BlockSource. *world.Tx implements all of them; tests may leave them out.
type (
	// rangeSource gives the world height range. Without it, the overworld range (-64..319) is used.
	rangeSource interface{ Range() cube.Range }
	// skySource is used for avoid_sun (BlockSource::canSeeSky). Without it, paths are never trimmed.
	skySource interface{ HighestLightBlocker(x, z int) int }
	// tickSource gives the current world tick, used by the per-world Budget.
	tickSource interface{ CurrentTick() int64 }
	// worldSource gives access to the World (time of day for avoid_sun).
	worldSource interface{ World() *world.World }
)

// Agent is the small part of a mob that navigation needs. Phase 0's Mob type implements it.
//
// Positions are feet positions (the bottom centre of the collision box), as in Dragonfly and BDS.
type Agent interface {
	// Position returns the feet position.
	Position() mgl64.Vec3
	// Size returns the collision box width and height (minecraft:collision_box, scaled).
	Size() (width, height float64)
	// OnGround reports whether the mob stands on a block.
	OnGround() bool
	// InWater and InLava report whether the collision box touches water or lava.
	InWater() bool
	InLava() bool
	// HorizontalCollision reports whether the last move was blocked horizontally (wall climbing).
	HorizontalCollision() bool
	// SetWantedPosition hands the next waypoint to the MoveControl (MoveControl::setWantedPosition).
	SetWantedPosition(pos mgl64.Vec3, speed float64)
	// ClearWantedPosition stops the MoveControl (MoveControl::setHasWantedPosition(false)).
	ClearWantedPosition()
}

// Climber is implemented by agents that can climb walls (navigation.climb, spiders). The navigation
// sets the climbing flag every tick, as WallClimberPathNavigation::tick does with Actor::setClimbing.
type Climber interface {
	SetClimbing(climbing bool)
}

// TraitSource is implemented by agents whose pathing traits change at run time (door breaking
// depends on difficulty, fire passing on having a target). When an Agent implements it, its traits
// replace Navigation.Traits for every search.
type TraitSource interface {
	PathTraits() Traits
}

// Target is an entity a mob can path to (PathFinder::findPath(Actor&, Actor&)). Its position is
// the feet position, which is the target AABB's minY, as BDS uses.
type Target interface {
	Position() mgl64.Vec3
}

// Traits are the per-mob facts the pathfinder reads from the actor (ActorPathingData), rather than
// from the navigation component.
type Traits struct {
	// StepHeight is the mob's max auto step (0.5625 for most mobs) or jump height. The pathfinder
	// uses max(StepHeight, 1.2) as the highest step it can jump onto (PathFinderUtils::getMaxJumpHeight).
	StepHeight float64
	// FollowRange is the follow_range attribute: nodes further than this from the goal are not
	// expanded. Zero means 16.
	FollowRange float64
	// FireImmune is fire_immune (or fire resistance): magma, lit campfires and fire are not avoided.
	FireImmune bool
	// PassFire makes fire blocks pathable. BDS (FireBlock::checkIsPathable) allows this for
	// fire-immune mobs and for zombie-family monsters that have a target.
	PassFire bool
	// DoorOpener is Actor::isDoorOpener (annotation.open_door).
	DoorOpener bool
	// DoorBreaker is Actor::isDoorBreaker (annotation.break_door active for the current difficulty).
	DoorBreaker bool
	// ClimbPowderSnow lets the mob climb powder snow (leather boots, can_stand_on_powder_snow).
	ClimbPowderSnow bool
	// BlockClimber lets the mob climb scaffolding (minecraft:block_climber).
	BlockClimber bool
	// BerryBushSafe makes grown sweet berry bushes pathable and not damaging: the mob's damage sensor
	// cancels their damage (SweetBerryBushBlock::checkIsPathable; foxes and bees).
	BerryBushSafe bool
	// Breakable, if set, reports blocks the mob may break through (break_blocks with mobGriefing on).
	// Such blocks become Breakable nodes.
	Breakable func(world.Block) bool
	// Preferred holds minecraft:preferred_path. Nil means the BDS defaults (no costs, 3 block fall).
	Preferred *PreferredPath
}
