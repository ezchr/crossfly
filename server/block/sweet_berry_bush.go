package block

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// SweetBerryBush is a bush that grows sweet berries. Placing sweet berries plants one, and the sweet berries
// item is the bush itself, as carrots are. Grown bushes hurt and slow down mobs and players walking through
// them. BDS SweetBerryBushBlock.
type SweetBerryBush struct {
	transparent
	empty

	// Age is the growth stage of the bush: 0 is a sapling, 1 a small bush, 2 a bush with unripe berries and 3
	// one with ripe berries. Bedrock has states up to 7; they act as 3.
	Age int
}

func init() {
	for age := 0; age <= 7; age++ {
		world.RegisterBlock(SweetBerryBush{Age: age})
	}
	world.RegisterItem(SweetBerryBush{})
}

// stage is the age the bush acts with (SweetBerryBushBlock::getVariant: states above 3 are 3).
func (b SweetBerryBush) stage() int {
	return min(b.Age, 3)
}

// HasLiquidDrops ...
func (SweetBerryBush) HasLiquidDrops() bool {
	return true
}

// AlwaysConsumable ...
func (SweetBerryBush) AlwaysConsumable() bool {
	return false
}

// ConsumeDuration ...
func (SweetBerryBush) ConsumeDuration() time.Duration {
	return item.DefaultConsumeDuration
}

// Consume eats sweet berries: 2 food, saturation modifier "low" (sweet_berries.json, minecraft:food).
func (SweetBerryBush) Consume(_ *world.Tx, c item.Consumer) item.Stack {
	c.Saturate(2, 1.2)
	return item.Stack{}
}

// CompostChance is the 30% of ComposterBlock's table for sweet berries.
func (SweetBerryBush) CompostChance() float64 {
	return 0.3
}

// sweetBerrySoils are the blocks BushBlock::mayPlaceOn plants on.
var sweetBerrySoils = map[string]bool{
	"minecraft:grass_block": true, "minecraft:dirt": true, "minecraft:farmland": true, "minecraft:moss_block": true,
	"minecraft:dirt_with_roots": true, "minecraft:podzol": true, "minecraft:mycelium": true, "minecraft:mud": true,
	"minecraft:muddy_mangrove_roots": true,
}

// sweetBerrySoil reports whether a bush may stand on b (BushBlock::mayPlaceOn).
func sweetBerrySoil(b world.Block) bool {
	if b == nil {
		return false
	}
	name, _ := b.EncodeBlock()
	return sweetBerrySoils[name]
}

// UseOnBlock plants a sweet berry bush of age 0 on a soil block.
func (b SweetBerryBush) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, b)
	if !used || !sweetBerrySoil(tx.Block(pos.Side(cube.FaceDown))) {
		return false
	}
	place(tx, pos, SweetBerryBush{}, user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick breaks the bush when its soil goes (BushBlock::neighborChanged then tick).
func (b SweetBerryBush) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !sweetBerrySoil(tx.Block(pos.Side(cube.FaceDown))) {
		breakBlock(b, pos, tx)
	}
}

// RandomTick grows the bush one stage, 1 in 5 (SweetBerryBushBlock::randomTick). Unlike crops it needs no light.
func (b SweetBerryBush) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	if r.IntN(5) == 0 {
		b.grow(pos, tx, false)
	}
}

// grow is SweetBerryBushBlock::_growBush: one stage, or straight to ripe when full is set.
func (b SweetBerryBush) grow(pos cube.Pos, tx *world.Tx, full bool) bool {
	if b.Age >= 3 {
		return false
	}
	if full {
		b.Age = 3
	} else {
		b.Age++
	}
	tx.SetBlock(pos, b, nil)
	return true
}

// BoneMeal grows the bush one stage (SweetBerryBushBlock::onFertilized with plain bone meal).
func (b SweetBerryBush) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	if !b.grow(pos, tx, false) {
		return item.BoneMealResultNone
	}
	return item.BoneMealResultSmall
}

// Activate picks the berries of a bush of age 2 or 3 (SweetBerryBushBlock::use). Holding bone meal on a bush
// that is not ripe uses the bone meal instead.
func (b SweetBerryBush) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, _ *item.UseContext) bool {
	if u != nil {
		if held, _ := u.HeldItems(); !held.Empty() && b.stage() < 3 {
			if _, ok := held.Item().(item.BoneMeal); ok {
				return false
			}
		}
	}
	return b.pick(pos, tx)
}

// pick is SweetBerryBushBlock::_pickBerries: the berries of the stage drop, the bush goes back to age 1 and
// the pick sound plays.
func (b SweetBerryBush) pick(pos cube.Pos, tx *world.Tx) bool {
	if b.stage() <= 1 {
		return false
	}
	for _, s := range b.berries(0) {
		dropItem(tx, s, pos.Vec3Centre())
	}
	tx.SetBlock(pos, SweetBerryBush{Age: 1}, nil)
	tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: "block.sweet_berry_bush.pick", Volume: 1, Pitch: 1})
	return true
}

// berries is VanillaBlockDrops createDropForSweetBerryBush: 1-2 berries at age 2, 2-3 at age 3, plus 0 to the
// fortune level; nothing younger.
func (b SweetBerryBush) berries(fortune int) []item.Stack {
	var n int
	switch b.stage() {
	case 2:
		n = 1 + rand.IntN(2)
	case 3:
		n = 2 + rand.IntN(2)
	default:
		return nil
	}
	if fortune > 0 {
		n += rand.IntN(fortune + 1)
	}
	return []item.Stack{item.NewStack(SweetBerryBush{}, n)}
}

// Raidable reports whether a mob raiding gardens (minecraft:behavior.raid_garden) may raid the bush: it has
// berries (SweetBerryBushBlock::hasTag "minecraft:is_raidable").
func (b SweetBerryBush) Raidable() bool {
	return b.stage() > 1
}

// Raid picks the berries for a mob that raided the bush (SweetBerryBushBlock::executeEvent
// "minecraft:on_raided").
func (b SweetBerryBush) Raid(pos cube.Pos, tx *world.Tx, _ world.Entity) {
	b.pick(pos, tx)
}

// EntityInside slows down mobs walking through the bush and hurts mobs and players moving in a bush older than
// a sapling for 1 (SweetBerryBushBlock::entityInside and dealsContactDamage: actors of type Mob only, with a
// "contact" damage of the block that damage sensors see; foxes and bees ignore it through theirs).
func (b SweetBerryBush) EntityInside(_ cube.Pos, _ *world.Tx, e world.Entity) {
	l, ok := e.(livingEntity)
	if !ok {
		return
	}
	moving := true
	if _, player := e.(interface{ GameMode() world.GameMode }); !player {
		// Players run this when they move and slow themselves down client-side.
		if v, ok := e.(velocityEntity); ok {
			vel := v.Velocity()
			moving = math.Abs(vel[0]) > 0.003 || math.Abs(vel[2]) > 0.003
			// Java fallback: no Bedrock data (the 1.21.3 decompile lacks the body of
			// SweetBerryBushMovementSlowdownSystem): Java's makeStuckInBlock(0.8, 0.75).
			vel[0] *= 0.8
			vel[1] *= 0.75
			vel[2] *= 0.8
			v.SetVelocity(vel)
		}
	}
	if b.Age >= 1 && moving {
		l.Hurt(1, DamageSource{Block: b})
	}
}

// BreakInfo ...
func (b SweetBerryBush) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, func(_ item.Tool, enchantments []item.Enchantment) []item.Stack {
		return b.berries(fortuneLevel(enchantments))
	})
}

// EncodeItem ...
func (SweetBerryBush) EncodeItem() (name string, meta int16) {
	return "minecraft:sweet_berries", 0
}

// EncodeBlock ...
func (b SweetBerryBush) EncodeBlock() (string, map[string]any) {
	return "minecraft:sweet_berry_bush", map[string]any{"growth": int32(b.Age)}
}
