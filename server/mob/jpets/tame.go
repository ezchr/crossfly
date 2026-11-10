//go:build crossfly_full

package jpets

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// Tamed animals (TamableAnimal): wolves, cats, parrots and nautiluses keep
// a tame flag, an owner, whether the owner ordered them to sit
// (orderedToSit, saved as "Sitting") and whether they show the sitting pose
// (DATA_FLAGS_ID 0x01, which SitWhenOrderedToGoal sets).

// TeleportWhenDistanceSq is TamableAnimal.TELEPORT_WHEN_DISTANCE_IS_SQ: a pet
// 12 blocks or more from its owner teleports instead of walking.
const TeleportWhenDistanceSq = 144.0

// TamableState is the state TamableAnimal adds.
type TamableState struct {
	Tame bool
	// OrderedToSit is what the owner asked (right click).
	OrderedToSit bool
	// InSittingPose is what clients show.
	InSittingPose bool
}

// TeleportWorld answers the checks of TamableAnimal.canTeleportTo for one
// mob.
type TeleportWorld interface {
	// Walkable reports whether WalkNodeEvaluator.getPathTypeStatic(mob, pos)
	// is WALKABLE (floor below, room for the mob, nothing dangerous).
	Walkable(pos cube.Pos) bool
	// Leaves reports whether the block at pos is leaves.
	Leaves(pos cube.Pos) bool
	// Free reports whether the mob's box moved to stand at pos (its block
	// position moved by pos - its own block position) collides with nothing
	// (level.noCollision).
	Free(pos cube.Pos) bool
}

// TeleportSpot is TamableAnimal.teleportToAroundBlockPos: up to 10 tries at
// x, z in -3..3 around the owner's block (at least 2 away on one axis) and
// y -1..1, each taken if canTeleportTo holds: a walkable spot, not on leaves
// unless the pet flies to its owner (parrots), and room for the box. The
// pet is then put at the block centre (x+0.5, y, z+0.5) and stops its path.
func TeleportSpot(r *rand.Rand, owner cube.Pos, canFly bool, w TeleportWorld) (cube.Pos, bool) {
	for range 10 {
		xd := r.IntN(7) - 3 // nextIntBetweenInclusive(-3, 3)
		zd := r.IntN(7) - 3
		if iabs(xd) < 2 && iabs(zd) < 2 {
			continue
		}
		yd := r.IntN(3) - 1
		pos := owner.Add(cube.Pos{xd, yd, zd})
		if canTeleportTo(pos, canFly, w) {
			return pos, true
		}
	}
	return cube.Pos{}, false
}

// canTeleportTo is TamableAnimal.canTeleportTo.
func canTeleportTo(pos cube.Pos, canFly bool, w TeleportWorld) bool {
	if !w.Walkable(pos) {
		return false
	}
	if !canFly && w.Leaves(pos.Side(cube.FaceDown)) {
		return false
	}
	return w.Free(pos)
}

// PetMobility is what TamableAnimal.unableToMoveToOwner reads.
type PetMobility struct {
	OrderedToSit, Passenger, Leashed, OwnerSpectator bool
}

// UnableToMoveToOwner is TamableAnimal.unableToMoveToOwner: a sitting,
// riding or leashed pet stays, and so does a pet whose owner spectates.
// (Leashed is Leashable.mayBeLeashed: a leash holder, or one still being
// read from disk.)
func (p PetMobility) UnableToMoveToOwner() bool {
	return p.OrderedToSit || p.Passenger || p.Leashed || p.OwnerSpectator
}

// SitWhenOrderedTo is SitWhenOrderedToGoal.canUse: a pet in water or in the
// air never sits; a tame pet whose owner is gone (or in another dimension)
// sits; one near an owner something just hurt (within 12 blocks) gets up to
// help, else it sits when ordered to.
func SitWhenOrderedTo(orderedToSit, tame, inWater, onGround, ownerHere bool, ownerDistSq float64, ownerHurtByMob bool) bool {
	if !orderedToSit && !tame {
		return false
	}
	if inWater || !onGround {
		return false
	}
	if !ownerHere {
		return true
	}
	if ownerDistSq < TeleportWhenDistanceSq && ownerHurtByMob {
		return false
	}
	return orderedToSit
}

func iabs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
