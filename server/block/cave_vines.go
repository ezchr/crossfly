package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// CaveVines is a vine that hangs from the ceiling of lush caves and grows downwards. Vines may carry glow
// berries, which light them up. Glow berries are the item of the vines, as sweet berries are of their bush.
// BDS CaveVinesBlock.
type CaveVines struct {
	transparent
	empty

	// Age is the growing_plant_age of the vine, 0-25: the tip of a vine stops growing at 25.
	Age int
	// Berries is true for a vine with glow berries.
	Berries bool
	// Body is true for a vine with berries that has more vine below it (cave_vines_body_with_berries) rather
	// than being the tip (cave_vines_head_with_berries). A vine without berries has one form for both.
	Body bool
}

// caveVinesMaxAge is the age at which a vine stops growing.
const caveVinesMaxAge = 25

func init() {
	for age := 0; age <= caveVinesMaxAge; age++ {
		world.RegisterBlock(CaveVines{Age: age})
		world.RegisterBlock(CaveVines{Age: age, Berries: true})
		world.RegisterBlock(CaveVines{Age: age, Berries: true, Body: true})
	}
	world.RegisterItem(CaveVines{})
}

// LightEmissionLevel is 14 for vines with berries.
func (c CaveVines) LightEmissionLevel() uint8 {
	if c.Berries {
		return 14
	}
	return 0
}

// HasLiquidDrops ...
func (CaveVines) HasLiquidDrops() bool {
	return true
}

// AlwaysConsumable ...
func (CaveVines) AlwaysConsumable() bool {
	return false
}

// ConsumeDuration ...
func (CaveVines) ConsumeDuration() time.Duration {
	return item.DefaultConsumeDuration
}

// Consume eats glow berries: 2 food, saturation modifier "low" (glow_berries.json, minecraft:food).
func (CaveVines) Consume(_ *world.Tx, c item.Consumer) item.Stack {
	c.Saturate(2, 1.2)
	return item.Stack{}
}

// CompostChance is the 30% of ComposterBlock's table for glow berries.
func (CaveVines) CompostChance() float64 {
	return 0.3
}

// isCaveVines is CaveVinesBlock::_isCaveVinesBlock.
func isCaveVines(b world.Block) bool {
	_, ok := b.(CaveVines)
	return ok
}

// caveVinesSurvive is CaveVinesBlock::canSurvive: no liquid in the block, and cave vines or a block with a solid
// bottom above it.
func caveVinesSurvive(pos cube.Pos, tx *world.Tx) bool {
	if _, ok := tx.Liquid(pos); ok {
		return false
	}
	above := pos.Side(cube.FaceUp)
	b := tx.Block(above)
	return isCaveVines(b) || b.Model().FaceSolid(above, cube.FaceDown, tx)
}

// caveVinesPlaceAge is the age CaveVinesBlock::onPlace gives a vine placed with age 0.
func caveVinesPlaceAge() int {
	return min(rand.IntN(25)+rand.IntN(12), caveVinesMaxAge)
}

// UseOnBlock plants a vine under the bottom of a solid block or the tip of a vine (glow_berries.json,
// minecraft:seed: plant_at cave_vines and cave_vines_head_with_berries or any solid surface, face down).
func (c CaveVines) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if face != cube.FaceDown {
		return false
	}
	if v, ok := tx.Block(pos).(CaveVines); ok && v.Body {
		return false
	}
	at := pos.Side(cube.FaceDown)
	if !replaceableWith(tx, at, c) || !caveVinesSurvive(at, tx) {
		return false
	}
	place(tx, at, CaveVines{Age: caveVinesPlaceAge()}, user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick breaks a vine that lost its support, and turns a vine with berries into the tip or the
// body form as the block below changes (CaveVinesBlock::neighborChanged, _updateBlockBasedOnNeighborBelow).
func (c CaveVines) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !caveVinesSurvive(pos, tx) {
		breakBlock(c, pos, tx)
		return
	}
	c.updateForm(pos, tx)
}

// updateForm is CaveVinesBlock::_updateBlockBasedOnNeighborBelow.
func (c CaveVines) updateForm(pos cube.Pos, tx *world.Tx) {
	if !c.Berries {
		return
	}
	if body := isCaveVines(tx.Block(pos.Side(cube.FaceDown))); body != c.Body {
		c.Body = body
		tx.SetBlock(pos, c, nil)
	}
}

// RandomTick updates the form of a vine with berries and lets the tip grow (CaveVinesBlock::randomTick).
func (c CaveVines) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	c.updateForm(pos, tx)
	if !isCaveVines(tx.Block(pos.Side(cube.FaceDown))) {
		c.grow(pos, tx, r.Float64(), r.Float64())
	}
}

// grow is CaveVinesBlock::tryGrow: a tip younger than 25 grows one vine into the air below, 1 in 10, with
// berries 11% of the time, one age older.
func (c CaveVines) grow(pos cube.Pos, tx *world.Tx, chance, berries float64) {
	below := pos.Side(cube.FaceDown)
	if c.Age >= caveVinesMaxAge || chance >= 0.1 {
		return
	}
	if _, ok := tx.Block(below).(Air); !ok {
		return
	}
	tx.SetBlock(below, CaveVines{Age: c.Age + 1, Berries: berries < 0.11}, nil)
}

// BoneMeal grows glow berries on a vine without them (CaveVinesBlock::onFertilized).
func (c CaveVines) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	if c.Berries {
		return item.BoneMealResultNone
	}
	c.Berries, c.Body = true, isCaveVines(tx.Block(pos.Side(cube.FaceDown)))
	tx.SetBlock(pos, c, nil)
	return item.BoneMealResultSmall
}

// Activate picks the glow berries of a vine (CaveVinesBlock::use).
func (c CaveVines) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, _ item.User, _ *item.UseContext) bool {
	return c.pick(pos, tx)
}

// pick is CaveVinesBlock::_pickBerries: one glow berry drops, the pick sound plays and the vine loses its
// berries, getting a new age as a placed vine does.
func (c CaveVines) pick(pos cube.Pos, tx *world.Tx) bool {
	if !c.Berries {
		return false
	}
	dropItem(tx, item.NewStack(CaveVines{}, 1), pos.Vec3Centre())
	tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: "pick_berries.cave_vines", Volume: 1, Pitch: 1})
	tx.SetBlock(pos, CaveVines{Age: caveVinesPlaceAge()}, nil)
	return true
}

// Raidable reports whether a mob raiding gardens may raid the vine: it has berries (CaveVinesBlock::hasTag
// "minecraft:is_raidable").
func (c CaveVines) Raidable() bool {
	return c.Berries
}

// Raid picks the berries for a mob that raided the vine (CaveVinesBlock::executeEvent "minecraft:on_raided").
func (c CaveVines) Raid(pos cube.Pos, tx *world.Tx, _ world.Entity) {
	c.pick(pos, tx)
}

// BreakInfo: a vine drops its glow berry, nothing without one (VanillaBlockDrops).
func (c CaveVines) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, func(item.Tool, []item.Enchantment) []item.Stack {
		if c.Berries {
			return []item.Stack{item.NewStack(CaveVines{}, 1)}
		}
		return nil
	})
}

// EncodeItem ...
func (CaveVines) EncodeItem() (name string, meta int16) {
	return "minecraft:glow_berries", 0
}

// EncodeBlock ...
func (c CaveVines) EncodeBlock() (string, map[string]any) {
	name := "minecraft:cave_vines"
	if c.Berries && c.Body {
		name = "minecraft:cave_vines_body_with_berries"
	} else if c.Berries {
		name = "minecraft:cave_vines_head_with_berries"
	}
	return name, map[string]any{"growing_plant_age": int32(c.Age)}
}
