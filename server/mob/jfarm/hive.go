//go:build crossfly_full

package jfarm

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob/janimal"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The Java beehive (BeehiveBlockEntity), as block.BeehiveRules.

const (
	// HiveMaxOccupants is BeehiveBlockEntity.MAX_OCCUPANTS (Paper's max-bees default: 3).
	HiveMaxOccupants = 3
	// HiveMinTicksNectar and HiveMinTicksNectarless are MIN_OCCUPATION_TICKS_NECTAR / _NECTARLESS.
	HiveMinTicksNectar     = 2400
	HiveMinTicksNectarless = 600
	// BeeStayOutAfterSedatedRelease is the stayOutOfHiveCountdown bees get when a smoked hive is emptied
	// with a player near (emptyAllLivingFromHive: 400).
	BeeStayOutAfterSedatedRelease = 400
)

// ReleaseStatus is BeehiveBlockEntity.BeeReleaseStatus.
type ReleaseStatus uint8

const (
	HoneyDelivered ReleaseStatus = iota
	BeeReleased
	Emergency
)

// ignoredBeeTags is BeehiveBlockEntity.IGNORED_BEE_TAGS: removed from a bee's save data when it enters and
// when it leaves.
var ignoredBeeTags = []string{"Air", "drop_chances", "equipment", "Brain", "CanPickUpLoot", "DeathTime",
	"fall_distance", "FallFlying", "Fire", "HurtTime", "LeftHanded", "Motion", "NoGravity", "OnGround",
	"PortalCooldown", "Pos", "Rotation", "sleeping_pos", "CannotEnterHiveTicks", "TicksSincePollination",
	"CropsGrownSincePollination", "hive_pos", "Passengers", "leash", "UUID"}

// StripBeeData removes the IGNORED_BEE_TAGS from a bee's save data.
func StripBeeData(data map[string]any) {
	for _, k := range ignoredBeeTags {
		delete(data, k)
	}
}

// NewOccupant is BeehiveBlockEntity.Occupant.of: the bee's save data (stripped) with 0 ticks in the hive and
// 2400 minimum ticks if it carries nectar, else 600.
func NewOccupant(id string, data map[string]any) block.BeehiveOccupant {
	StripBeeData(data)
	min := HiveMinTicksNectarless
	if janimal.NBTBool(data, "HasNectar", false) {
		min = HiveMinTicksNectar
	}
	return block.BeehiveOccupant{Identifier: id, Data: data, MinTicksInHive: min}
}

// BeesStayInHive is the environment attribute gameplay/bees_stay_in_hive: in the overworld from tick 12542
// to 23460 of the day (timeline minecraft:day) and while it rains or thunders (WeatherAttributes); never in
// other dimensions.
func BeesStayInHive(tx *world.Tx) bool {
	if tx.World().Dimension() != world.Overworld {
		return false
	}
	if tx.Raining() || tx.Thundering() {
		return true
	}
	t := (tx.World().Time()%24000 + 24000) % 24000
	return t >= 12542 && t < 23460
}

// HoneyIncrease is the honey a bee with nectar adds on leaving (releaseOccupant): 1, or 2 one time in 100,
// never past 5.
func HoneyIncrease(r *rand.Rand, level int) int {
	if level >= 5 {
		return level
	}
	add := 1
	if r.IntN(100) == 0 {
		add = 2
	}
	if level+add > 5 {
		add--
	}
	return level + add
}

// TickOccupant is BeeData.tick: one more tick in the hive; true once the bee has stayed long enough to try
// to leave (ticksInHive++ > minTicksInHive).
func TickOccupant(o *block.BeehiveOccupant) bool {
	if o.MinTicksInHive == 0 {
		// Saved by the BDS rules: TicksLeftToStay becomes the Java counters.
		o.MinTicksInHive = HiveMinTicksNectarless
		if janimal.NBTBool(o.Data, "HasNectar", false) {
			o.MinTicksInHive = HiveMinTicksNectar
		}
		o.TicksInHive = max(0, o.MinTicksInHive-o.TicksLeft)
	}
	t := o.TicksInHive
	o.TicksInHive++
	return t > o.MinTicksInHive
}

// ReleasePos is where releaseOccupant puts a bee of width w and height ht: 0.55 + w/2 in front of the
// hive's facing (in the hive if the front is blocked), centred at half its height.
func ReleasePos(pos cube.Pos, facing cube.Direction, w, ht float64, frontBlocked bool) mgl64.Vec3 {
	delta := 0.0
	if !frontBlocked {
		delta = 0.55 + w/2
	}
	f := facing.Face()
	step := cube.Pos{}.Side(f)
	return mgl64.Vec3{float64(pos[0]) + 0.5 + delta*float64(step[0]), float64(pos[1]) + 0.5 - ht/2,
		float64(pos[2]) + 0.5 + delta*float64(step[2])}
}

// FrontBlocked reports whether the block in front of the hive has a collision shape.
func FrontBlocked(tx *world.Tx, pos cube.Pos, facing cube.Direction) bool {
	front := pos.Side(facing.Face())
	return len(tx.Block(front).Model().BBox(front, tx)) != 0
}

// BeeReleaseAge is setBeeReleaseData's age part (updateBeeAge): an unlocked baby grows and an adult's
// breeding cooldown runs down by the ticks spent in the hive; the love time goes down as well.
func BeeReleaseAge(a *janimal.Animal, ticksInHive int32) {
	if !a.AgeLocked() {
		switch age := a.Age(); {
		case age < 0:
			a.SetAge(min(0, age+ticksInHive))
		case age > 0:
			a.SetAge(max(0, age-ticksInHive))
		}
	}
	a.SetInLoveTime(max(0, a.InLoveTime()-ticksInHive))
}
