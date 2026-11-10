//go:build crossfly_full

package block

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
)

// redstoneAnalogOutput reports the reading a comparator takes from the block b at pos, and whether b gives one. The
// readings are Java Edition's.
func redstoneAnalogOutput(pos cube.Pos, b world.Block, tx *world.Tx) (int, bool) {
	switch b := b.(type) {
	case EnderChest:
		return 0, false // its contents are per player: no reading, as in Java Edition
	case Jukebox:
		if disc, ok := b.Disc(); ok {
			return javaDiscSignal[disc.String()], true
		}
		return 0, true
	case Lectern:
		if b.Book.Empty() {
			return 0, true
		}
		book, ok := b.Book.Item().(readableBook)
		if !ok {
			return 0, true
		}
		if book.TotalPages() <= 1 {
			return 15, true
		}
		page := min(max(0, b.Page), book.TotalPages()-1)
		return int(float32(page)/float32(book.TotalPages()-1)*14) + 1, true
	case DetectorRail:
		// While on, the first container minecart on it, as Java Edition.
		if b.Powered {
			for _, c := range detectorMinecarts(pos, tx, false) {
				if inv := c.MinecartInventory(); inv != nil {
					return containerSignal(inv), true
				}
			}
		}
		return 0, true
	case CopperBulb:
		if b.Lit {
			return 15, true
		}
		return 0, true
	case Cake:
		return (7 - b.Bites) * 2, true
	case Cauldron:
		if b.Level == 0 {
			return 0, true
		}
		if b.Liquid == CauldronLava() {
			return 3, true
		}
		return (b.Level + 1) / 2, true // Bedrock levels 0 to 6 are Java's 0 to 3
	case Composter:
		return b.Level, true
	case EndPortalFrame:
		if b.Eye {
			return 15, true
		}
		return 0, true
	case ItemFrame:
		if b.Item.Empty() {
			return 0, true
		}
		return b.Rotations%8 + 1, true
	case sculkSensorBlock:
		// The frequency of the vibration received while active (SculkSensorBlock::getComparatorSignal, the
		// same in Java Edition).
		return sculkSensorComparatorOutput(b), true
	case Beehive:
		// Java's BeehiveBlock.getAnalogOutputSignal: the honey level.
		return b.HoneyLevel, true
	case CopperGolemStatue:
		// Java's CopperGolemStatueBlock.getAnalogOutputSignal: standing 1, sitting 2, running 3, star 4.
		return int(b.Pose.Uint8()) + 1, true
	case DecoratedPot:
		if b.Item.Empty() {
			return 0, true
		}
		return 1 + int(math.Floor(float64(b.Item.Count())/float64(b.Item.MaxCount())*14)), true
	case interface {
		Inventory(*world.Tx, cube.Pos) *inventory.Inventory
	}:
		return containerSignal(b.Inventory(tx, pos)), true
	}
	return 0, false
}

// containerSignal is Java Edition's comparator reading of a container: 0 if empty, else 1 to 15 by how full its slots
// are, each slot counting as full at the most its item stacks to.
func containerSignal(inv *inventory.Inventory) int {
	if inv == nil || inv.Size() == 0 {
		return 0
	}
	full, any := 0.0, false
	for _, s := range inv.Slots() {
		if s.Empty() {
			continue
		}
		any = true
		full += float64(s.Count()) / float64(s.MaxCount())
	}
	if !any {
		return 0
	}
	full /= float64(inv.Size())
	return int(math.Floor(full*14)) + 1
}

// Java 26.3 jukebox_song data; these values are not the disc registry order.
var javaDiscSignal = map[string]int{
	"11":                11,
	"13":                1,
	"5":                 15,
	"blocks":            3,
	"bounce":            8,
	"cat":               2,
	"chirp":             4,
	"creator":           12,
	"creator_music_box": 11,
	"far":               5,
	"lava_chicken":      9,
	"mall":              6,
	"mellohi":           7,
	"otherside":         14,
	"pigstep":           13,
	"precipice":         13,
	"relic":             14,
	"stal":              8,
	"strad":             9,
	"tears":             10,
	"wait":              12,
	"ward":              10,
}
