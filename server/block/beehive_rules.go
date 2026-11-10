package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// BeehiveRules replace the BDS behaviour of beehives and bee nests. The Java mob mode (mobs=java) installs
// Java Edition's BeehiveBlockEntity rules: bees enter through their own goal (no
// minecraft:bee_returned_to_hive event), stay ticks_in_hive past min_ticks_in_hive, leave unless bees stay
// in the hive (night, rain) and fill the hive with honey.
type BeehiveRules interface {
	// Tick is BeehiveBlockEntity.serverTick, run for a hive with occupants.
	Tick(h Beehive, pos cube.Pos, tx *world.Tx)
	// EmptyAll is BeehiveBlockEntity.emptyAllLivingFromHive(player, state, EMERGENCY): every occupant leaves
	// (fire next to the hive, the hive broken or harvested without smoke). by is the player (nil for fire),
	// whom the released bees within 4 blocks attack unless the hive is smoked.
	EmptyAll(h Beehive, pos cube.Pos, tx *world.Tx, by world.Entity)
	// AngerNearby is BeehiveBlock.angerNearbyBees: bees within 8/6/8 blocks without a target pick a random
	// player in that box.
	AngerNearby(pos cube.Pos, tx *world.Tx)
}

var beehiveRules BeehiveRules

// SetBeehiveRules installs r for every beehive and bee nest. Call it once at start-up, before worlds tick.
func SetBeehiveRules(r BeehiveRules) { beehiveRules = r }

// WithOccupants returns the hive holding o (nil for none), keeping its saved flower.
func (h Beehive) WithOccupants(o []BeehiveOccupant) Beehive {
	b := &beehiveBees{}
	if h.bees != nil {
		b.flowerPos, b.spawnBees = h.bees.flowerPos, h.bees.spawnBees
	}
	b.occupants = o
	h.bees = b
	return h
}

// FlowerPos is Java's BeehiveBlockEntity.savedFlowerPos (flower_pos).
func (h Beehive) FlowerPos() (cube.Pos, bool) {
	if h.bees == nil || h.bees.flowerPos == nil {
		return cube.Pos{}, false
	}
	return *h.bees.flowerPos, true
}

// WithFlowerPos returns the hive with its saved flower set (ok false clears it).
func (h Beehive) WithFlowerPos(p cube.Pos, ok bool) Beehive {
	b := &beehiveBees{}
	if h.bees != nil {
		*b = *h.bees
	}
	b.flowerPos = nil
	if ok {
		b.flowerPos = &p
	}
	h.bees = b
	return h
}

// BeehiveSmoked is CampfireBlock.isSmokeyPos as Crossfly sees it: a lit campfire at most five blocks below
// with only air between.
func BeehiveSmoked(pos cube.Pos, tx *world.Tx) bool { return beehiveCampfireBelow(pos, tx) }

// activateJava is Java's BeehiveBlock.useItemOn with the rules installed: shears drop three honeycombs
// (loot table harvest_beehive) and lose one durability, a glass bottle becomes a honey bottle; the honey
// level goes back to 0 and, unless the hive is smoked, nearby bees are angered and the bees inside come out
// after the player.
func (h Beehive) activateJava(pos cube.Pos, tx *world.Tx, u item.User, ctx *item.UseContext, shears bool) bool {
	if shears {
		for range 3 {
			dropItem(tx, item.NewStack(item.Honeycomb{}, 1), pos.Vec3Centre())
		}
		tx.PlaySound(u.Position(), beehiveSound("block.beehive.shear"))
		ctx.DamageItem(1)
	} else {
		ctx.SubtractFromCount(1)
		ctx.NewItem = item.NewStack(item.HoneyBottle{}, 1)
		tx.PlaySound(u.Position(), sound.BucketFill{Liquid: Water{Still: true, Depth: 8}})
	}
	smoked := beehiveCampfireBelow(pos, tx)
	hasBees := h.bees != nil && len(h.bees.occupants) > 0
	h.HoneyLevel = 0
	tx.SetBlock(pos, h, nil)
	if !smoked && hasBees {
		beehiveRules.AngerNearby(pos, tx)
		beehiveRules.EmptyAll(h, pos, tx, u)
	}
	return true
}
