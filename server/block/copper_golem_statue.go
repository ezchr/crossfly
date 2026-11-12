package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// CopperGolemStatue is the result of a copper golem fully oxidizing and petrifying into a block.
// It can be posed in four different ways.
type CopperGolemStatue struct {
	transparent
	sourceWaterDisplacer
	solid

	// Facing is the direction the copper golem statue is facing.
	Facing cube.Direction
	// Pose is the pose of the copper golem statue.
	Pose CopperGolemPose
	// Oxidation is the level of oxidation of the copper lantern.
	Oxidation OxidationType
	// Waxed bool is whether the copper lantern has been waxed with honeycomb.
	Waxed bool
}

// BreakInfo ...
func (c CopperGolemStatue) BreakInfo() BreakInfo {
	return newBreakInfo(3, alwaysHarvestable, pickaxeEffective, oneOf(c)).withBlastResistance(6)
}

// Activate ...
func (c CopperGolemStatue) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	held, _ := u.HeldItems()
	if !held.Empty() {
		if _, axe := held.Item().(item.Axe); axe {
			return c.revive(pos, tx, ctx)
		}
		// copper golems can't be activated while holding an item.
		return false
	}
	poses := CopperGolemPoses()
	nextIndex := int(c.Pose.Uint8()) + 1
	if nextIndex >= len(poses) {
		nextIndex = 0
	}
	c.Pose = poses[nextIndex]
	tx.SetBlock(pos, c, nil)
	return true
}

// revive turns an unwaxed, unoxidised statue that an axe is used on back into a copper golem (26.60
// CopperGolemStatueBlock use, 0x14908f420; Java's CopperGolemStatueBlock does the same): the statue goes, the golem
// stands where it stood looking the statue's way and gets minecraft:from_serialized_entity, the axe takes one damage
// and the scrape particles show. Statues hold no saved golem here, so the golem is a new one. Any other statue is
// left to the axe (wax off, scrape), as are all statues without mobs.
func (c CopperGolemStatue) revive(pos cube.Pos, tx *world.Tx, ctx *item.UseContext) bool {
	if c.Waxed || c.Oxidation != UnoxidisedOxidation() {
		return false
	}
	at := mgl64.Vec3{float64(pos[0]) + 0.5, float64(pos[1]), float64(pos[2]) + 0.5}
	if _, ok := SpawnMob(tx, "minecraft:copper_golem", at, MobSpawnOptions{Yaw: golemYaw(c.Facing), Event: "minecraft:from_serialized_entity"}); !ok {
		return false
	}
	tx.SetBlock(pos, nil, nil)
	tx.PlaySound(pos.Vec3Centre(), sound.CopperScraped{})
	if ctx != nil {
		ctx.DamageItem(1)
	}
	return true
}

// UseOnBlock ...
func (c CopperGolemStatue) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, _, used = firstReplaceable(tx, pos, face, c)
	if !used {
		return
	}

	c.Facing = user.Rotation().Direction().Opposite()

	place(tx, pos, c, user, ctx)
	return placed(ctx)
}

// Wax waxes the copper lantern to stop it from oxidising further.
func (c CopperGolemStatue) Wax(cube.Pos, mgl64.Vec3) (world.Block, bool) {
	if c.Waxed {
		return c, false
	}
	c.Waxed = true
	return c, true
}

// Strip ...
func (c CopperGolemStatue) Strip() (world.Block, world.Sound, bool) {
	if c.Waxed {
		c.Waxed = false
		return c, sound.WaxRemoved{}, true
	} else if ot, ok := c.Oxidation.Decrease(); ok {
		c.Oxidation = ot
		return c, sound.CopperScraped{}, true
	}
	return c, nil, false
}

// CanOxidate ...
func (c CopperGolemStatue) CanOxidate() bool {
	return !c.Waxed
}

// OxidationLevel ...
func (c CopperGolemStatue) OxidationLevel() OxidationType {
	return c.Oxidation
}

// WithOxidationLevel ...
func (c CopperGolemStatue) WithOxidationLevel(o OxidationType) Oxidisable {
	c.Oxidation = o
	return c
}

// RandomTick ...
func (c CopperGolemStatue) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	attemptOxidation(pos, tx, r, c)
}

// DecodeNBT ...
func (c CopperGolemStatue) DecodeNBT(data map[string]any) any {
	// Poses out of range (client packets can hold any) keep the statue's pose.
	if p := nbtconv.Int32(data, "Pose"); p >= 0 && int(p) < len(CopperGolemPoses()) {
		c.Pose = CopperGolemPose{pose(p)}
	}
	return c
}

// EncodeNBT ...
func (c CopperGolemStatue) EncodeNBT() map[string]any {
	return map[string]any{
		"Pose": int32(c.Pose.Uint8()),
		"id":   "CopperGolemStatue",
	}
}

// EncodeItem ...
func (c CopperGolemStatue) EncodeItem() (name string, meta int16) {
	return copperBlockName("copper_golem_statue", c.Oxidation, c.Waxed), 0
}

// EncodeBlock ...
func (c CopperGolemStatue) EncodeBlock() (string, map[string]any) {
	return copperBlockName("copper_golem_statue", c.Oxidation, c.Waxed), map[string]any{"minecraft:cardinal_direction": c.Facing.String()}
}

// allCopperGolemStatues ...
func allCopperGolemStatues() (golems []world.Block) {
	f := func(waxed bool) {
		for _, o := range OxidationTypes() {
			for _, direction := range cube.Directions() {
				golems = append(golems, CopperGolemStatue{Facing: direction, Oxidation: o, Waxed: waxed})
			}
		}
	}
	f(true)
	f(false)
	return
}
