//go:build crossfly_full

package jfarm

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob/janimal"
	"github.com/df-mc/dragonfly/server/world"
)

// BeeState is the server state of a Java Bee (net.minecraft.world.entity.animal.bee.Bee) apart from its
// Animal and anger parts: flags, timers, hive and flower. The goals and the hive read and change it.
type BeeState struct {
	// Flags is Bee.DATA_FLAGS_ID: BeeRoll, BeeHasStung, BeeHasNectar.
	Flags uint8
	// TimeSinceSting counts up after a sting; the bee dies at a random point of the next 1200 ticks.
	TimeSinceSting int32
	// TicksWithoutNectar is ticksWithoutNectarSinceExitingHive (TicksSincePollination).
	TicksWithoutNectar int32
	// StayOutOfHive is stayOutOfHiveCountdown (CannotEnterHiveTicks).
	StayOutOfHive int32
	// CropsGrown is numCropsGrownSincePollination (CropsGrownSincePollination).
	CropsGrown int32
	// HiveCooldown and FlowerCooldown are remainingCooldownBeforeLocatingNewHive/Flower.
	HiveCooldown, FlowerCooldown int32
	// Hive and Flower are hivePos and savedFlowerPos.
	Hive, Flower *cube.Pos
	// UnderWater is underWaterTicks.
	UnderWater int32
}

const (
	BeeRoll      uint8 = 2
	BeeHasStung  uint8 = 4
	BeeHasNectar uint8 = 8

	// BeeStingDeathCountdown is Bee.STING_DEATH_COUNTDOWN.
	BeeStingDeathCountdown = 1200
	// BeeTooFar is TOO_FAR_DISTANCE: a hive farther than 48 blocks is lost.
	BeeTooFar = 48
	// BeeHiveSearch is HIVE_SEARCH_DISTANCE (POI bee_home within 20 blocks).
	BeeHiveSearch = 20
	// BeeMaxCropsGrown is MAX_CROPS_GROWABLE.
	BeeMaxCropsGrown = 10
	// BeeTiredOfNectar is TICKS_WITHOUT_NECTAR_BEFORE_GOING_HOME.
	BeeTiredOfNectar = 3600
	// BeeKnownFlowerTicks is TICKS_BEFORE_GOING_TO_KNOWN_FLOWER.
	BeeKnownFlowerTicks = 600
	// BeeMinPollinationTicks is BeePollinateGoal.MIN_POLLINATION_TICKS; MaxPollinatingTicks MAX_POLLINATING_TICKS.
	BeeMinPollinationTicks = 400
	BeeMaxPollinatingTicks = 600
)

// NewBeeState is a fresh bee: the flower cooldown starts at 20-60 ticks.
func NewBeeState(r *rand.Rand) BeeState {
	return BeeState{FlowerCooldown: int32(20 + r.IntN(41))}
}

func (b *BeeState) flag(f uint8) bool { return b.Flags&f != 0 }
func (b *BeeState) setFlag(f uint8, v bool) {
	if v {
		b.Flags |= f
	} else {
		b.Flags &^= f
	}
}

// HasNectar, HasStung and Rolling read the flags.
func (b *BeeState) HasNectar() bool { return b.flag(BeeHasNectar) }
func (b *BeeState) HasStung() bool  { return b.flag(BeeHasStung) }
func (b *BeeState) Rolling() bool   { return b.flag(BeeRoll) }

// SetHasNectar is Bee.setHasNectar: picking up nectar resets the ticks without nectar.
func (b *BeeState) SetHasNectar(v bool) {
	if v {
		b.TicksWithoutNectar = 0
	}
	b.setFlag(BeeHasNectar, v)
}

// SetHasStung and SetRolling set the flags.
func (b *BeeState) SetHasStung(v bool) { b.setFlag(BeeHasStung, v) }
func (b *BeeState) SetRolling(v bool)  { b.setFlag(BeeRoll, v) }

// DropOffNectar is Bee.dropOffNectar (the hive got the honey): no nectar, no crops counted.
func (b *BeeState) DropOffNectar() {
	b.SetHasNectar(false)
	b.CropsGrown = 0
}

// DropHive is Bee.dropHive: forget the hive and wait 200 ticks before looking for another.
func (b *BeeState) DropHive() {
	b.Hive = nil
	b.HiveCooldown = 200
}

// DropFlower is Bee.dropFlower.
func (b *BeeState) DropFlower(r *rand.Rand) {
	b.Flower = nil
	b.FlowerCooldown = int32(20 + r.IntN(41))
}

// TiredOfLookingForNectar is Bee.isTiredOfLookingForNectar.
func (b *BeeState) TiredOfLookingForNectar() bool { return b.TicksWithoutNectar > BeeTiredOfNectar }

// WantsToEnterHive is Bee.wantsToEnterHive: not kept out, not pollinating, not stung, no target, and with
// nectar, tired of looking, or bees stay in (night, rain), with no fire next to the hive.
func (b *BeeState) WantsToEnterHive(tx *world.Tx, pollinating, hasTarget, hiveNearFire bool) bool {
	if b.StayOutOfHive > 0 || pollinating || b.HasStung() || hasTarget {
		return false
	}
	return (b.HasNectar() || b.TiredOfLookingForNectar() || BeesStayInHive(tx)) && !hiveNearFire
}

// ServerAiStep is the Bee.customServerAiStep part without anger: drowning after 20 ticks under water (1
// damage), the sting death, and the nectarless counter. It reports the damage to deal (0 for none) and
// whether the bee dies of its sting.
func (b *BeeState) ServerAiStep(r *rand.Rand, inWater bool) (drown bool, stingDeath bool) {
	if inWater {
		b.UnderWater++
	} else {
		b.UnderWater = 0
	}
	drown = b.UnderWater > 20
	if b.HasStung() {
		b.TimeSinceSting++
		if b.TimeSinceSting%5 == 0 && r.IntN(max(1, min(BeeStingDeathCountdown-int(b.TimeSinceSting), BeeStingDeathCountdown))) == 0 {
			stingDeath = true
		}
	}
	if !b.HasNectar() {
		b.TicksWithoutNectar++
	}
	return
}

// AiStep is the server part of Bee.aiStep: the cooldowns count down.
func (b *BeeState) AiStep() {
	if b.StayOutOfHive > 0 {
		b.StayOutOfHive--
	}
	if b.HiveCooldown > 0 {
		b.HiveCooldown--
	}
	if b.FlowerCooldown > 0 {
		b.FlowerCooldown--
	}
}

// AttractsBees is Bee.attractsBees: a block in the tag minecraft:bee_attractive that is not waterlogged; of a
// sunflower only the upper half.
func AttractsBees(b world.Block) bool {
	if b == nil {
		return false
	}
	name, props := b.EncodeBlock()
	if _, ok := beeAttractive[name]; !ok {
		return false
	}
	if w, ok := props["waterlogged"].(bool); ok && w {
		return false
	}
	if name == "minecraft:sunflower" {
		up, _ := props["upper_block_bit"].(bool)
		return up
	}
	return true
}

// beeAttractive is the block tag minecraft:bee_attractive of 26.3 (Bedrock names).
var beeAttractive = set(
	"minecraft:dandelion", "minecraft:open_eyeblossom", "minecraft:poppy", "minecraft:blue_orchid", "minecraft:allium",
	"minecraft:azure_bluet", "minecraft:red_tulip", "minecraft:orange_tulip", "minecraft:white_tulip",
	"minecraft:pink_tulip", "minecraft:oxeye_daisy", "minecraft:cornflower", "minecraft:lily_of_the_valley",
	"minecraft:wither_rose", "minecraft:torchflower", "minecraft:sunflower", "minecraft:lilac", "minecraft:peony",
	"minecraft:rose_bush", "minecraft:pitcher_plant", "minecraft:azalea_leaves_flowered", "minecraft:flowering_azalea",
	"minecraft:mangrove_propagule", "minecraft:cherry_leaves", "minecraft:pink_petals", "minecraft:wildflowers",
	"minecraft:chorus_flower", "minecraft:spore_blossom", "minecraft:cactus_flower",
)

// GrowCrop is the block part of BeeGrowCropGoal.tick for one of the two blocks below a bee carrying nectar
// (tag minecraft:bee_growables): a crop not fully grown grows one stage (Bedrock beetroots have two stages
// per Java age), a stem up to age 7, a sweet berry bush up to age 3, and a cave vine without glow berries
// gets them (bone meal). It reports whether something grew (the caller counts it and shows the bone meal
// particles, level event 2011).
func GrowCrop(tx *world.Tx, pos cube.Pos) bool {
	var nb world.Block
	switch b := tx.Block(pos).(type) {
	case block.WheatSeeds:
		if b.Growth < 7 {
			b.Growth++
			nb = b
		}
	case block.Carrot:
		if b.Growth < 7 {
			b.Growth++
			nb = b
		}
	case block.Potato:
		if b.Growth < 7 {
			b.Growth++
			nb = b
		}
	case block.BeetrootSeeds:
		if b.Growth < 7 {
			b.Growth = min(7, b.Growth+2)
			nb = b
		}
	case block.MelonSeeds:
		if b.Growth < 7 {
			b.Growth++
			nb = b
		}
	case block.PumpkinSeeds:
		if b.Growth < 7 {
			b.Growth++
			nb = b
		}
	case block.SweetBerryBush:
		if b.Age < 3 {
			b.Age++
			nb = b
		}
	case block.CaveVines:
		if !b.Berries {
			b.Berries = true
			nb = b
		}
	}
	if nb == nil {
		return false
	}
	tx.SetBlock(pos, nb, nil)
	return true
}

// EncodeNBT is Bee.addAdditionalSaveData (without the Animal and anger parts).
func (b *BeeState) EncodeNBT(m map[string]any) {
	if b.Hive != nil {
		m["hive_pos"] = posInts(*b.Hive)
	}
	if b.Flower != nil {
		m["flower_pos"] = posInts(*b.Flower)
	}
	m["HasNectar"] = boolByte(b.HasNectar())
	m["HasStung"] = boolByte(b.HasStung())
	m["TicksSincePollination"] = b.TicksWithoutNectar
	m["CannotEnterHiveTicks"] = b.StayOutOfHive
	m["CropsGrownSincePollination"] = b.CropsGrown
}

// DecodeNBT is Bee.readAdditionalSaveData.
func (b *BeeState) DecodeNBT(m map[string]any) {
	b.SetHasNectar(janimal.NBTBool(m, "HasNectar", false))
	b.SetHasStung(janimal.NBTBool(m, "HasStung", false))
	b.TicksWithoutNectar = janimal.NBTInt(m, "TicksSincePollination", 0)
	b.StayOutOfHive = janimal.NBTInt(m, "CannotEnterHiveTicks", 0)
	b.CropsGrown = janimal.NBTInt(m, "CropsGrownSincePollination", 0)
	b.Hive, b.Flower = nbtPos(m, "hive_pos"), nbtPos(m, "flower_pos")
}

func posInts(p cube.Pos) []int32 { return []int32{int32(p[0]), int32(p[1]), int32(p[2])} }

func nbtPos(m map[string]any, k string) *cube.Pos {
	v, ok := m[k].([]int32)
	if !ok || len(v) != 3 {
		return nil
	}
	p := cube.Pos{int(v[0]), int(v[1]), int(v[2])}
	return &p
}

func boolByte(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}
