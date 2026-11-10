//go:build crossfly_full

package item

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/gameevent"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// What items do in a dispenser (Crossfly Full, Java redstone: block.Dispenser).

// Dispensable represents an item with specialised behaviour when used by a dispenser.
type Dispensable interface {
	// Dispense performs the item's dispenser behaviour. The position and face are those of the dispenser. Mutations to
	// the dispensed stack are recorded in ctx and applied by the dispenser after Dispense returns successfully.
	Dispense(pos cube.Pos, face cube.Face, tx *world.Tx, ctx *DispenseContext) DispenseResult
}

// DispenseResult describes the outcome of specialised item behaviour in a dispenser.
type DispenseResult uint8

const (
	// DispenseDefault falls back to ejecting the item normally.
	DispenseDefault DispenseResult = iota
	// DispenseSuccess applies the DispenseContext to the selected dispenser slot.
	DispenseSuccess
	// DispenseFailure leaves the selected item unchanged and plays the dispenser failure sound.
	DispenseFailure
)

// DispenseContext records mutations to a dispensed item and provides the random source of the dispenser activation.
type DispenseContext struct {
	// Damage is the amount of damage applied to the selected item after a successful dispense.
	Damage int
	// CountSub is the number of items removed from the selected stack after a successful dispense.
	CountSub int
	// NewItem is returned to the dispenser inventory after a successful dispense.
	NewItem Stack
	// Rand is the random source of the dispenser activation.
	Rand *rand.Rand
}

// DamageItem damages the selected item by d after a successful dispense.
func (ctx *DispenseContext) DamageItem(d int) { ctx.Damage += d }

// SubtractFromCount subtracts d from the selected stack after a successful dispense.
func (ctx *DispenseContext) SubtractFromCount(d int) { ctx.CountSub += d }

// dispenseProjectile launches a projectile out of a dispenser.
func dispenseProjectile(pos cube.Pos, face cube.Face, tx *world.Tx, ctx *DispenseContext, played world.Sound, create func(world.EntitySpawnOpts) *world.EntityHandle) DispenseResult {
	direction := cube.Pos{}.Side(face).Vec3()
	r := ctx.Rand
	opts := world.EntitySpawnOpts{
		Position: pos.Vec3Centre().Add(direction.Mul(0.7)),
		Velocity: direction.Mul(1.1).Add(mgl64.Vec3{r.Float64()*0.1 - 0.05, r.Float64()*0.1 - 0.05, r.Float64()*0.1 - 0.05}),
	}
	ctx.SubtractFromCount(1)
	tx.AddEntity(create(opts))
	tx.PlaySound(pos.Vec3Centre(), played)
	return DispenseSuccess
}

// Dispense launches the arrow from a dispenser.
func (a Arrow) Dispense(pos cube.Pos, face cube.Face, tx *world.Tx, ctx *DispenseContext) DispenseResult {
	create := tx.World().EntityRegistry().Config().Arrow
	if create == nil {
		return DispenseFailure
	}
	return dispenseProjectile(pos, face, tx, ctx, sound.BowShoot{}, func(opts world.EntitySpawnOpts) *world.EntityHandle {
		return create(opts, world.ArrowSpawnConfig{Damage: 2, ObtainArrowOnPickup: true, Tip: a.Tip})
	})
}

// Dispense applies bone meal to the block in front of a dispenser.
func (b BoneMeal) Dispense(pos cube.Pos, face cube.Face, tx *world.Tx, ctx *DispenseContext) DispenseResult {
	front := pos.Side(face)
	affected, ok := tx.Block(front).(BoneMealAffected)
	if !ok {
		return DispenseFailure
	}
	result := affected.BoneMeal(front, tx)
	if result == BoneMealResultNone {
		return DispenseFailure
	}
	ctx.SubtractFromCount(1)
	tx.AddParticle(front.Vec3(), particle.BoneMeal{Area: result == BoneMealResultArea})
	return DispenseSuccess
}

// Dispense launches the bottle from a dispenser.
func (b BottleOfEnchanting) Dispense(pos cube.Pos, face cube.Face, tx *world.Tx, ctx *DispenseContext) DispenseResult {
	create := tx.World().EntityRegistry().Config().BottleOfEnchanting
	if create == nil {
		return DispenseFailure
	}
	return dispenseProjectile(pos, face, tx, ctx, sound.ItemThrow{}, func(opts world.EntitySpawnOpts) *world.EntityHandle {
		return create(opts, nil)
	})
}

// Dispense fills or empties the bucket at the block in front of a dispenser, or returns DispenseDefault if the bucket
// cannot interact with the target.
func (b Bucket) Dispense(pos cube.Pos, face cube.Face, tx *world.Tx, ctx *DispenseContext) DispenseResult {
	front := pos.Side(face)
	if b.Empty() {
		liquid, ok := tx.Liquid(front)
		if !ok || liquid.LiquidDepth() != 8 || liquid.LiquidFalling() {
			return DispenseDefault
		}
		ctx.NewItem = NewStack(Bucket{Content: LiquidBucketContent(liquid)}, 1)
		ctx.SubtractFromCount(1)
		tx.SetLiquid(front, nil)
		tx.PlaySound(front.Vec3Centre(), sound.BucketFill{Liquid: liquid})
		// BucketItem::dispense: fluid_pickup, made by nobody.
		postDispenseEvent(tx, gameevent.FluidPickup, front)
		return DispenseSuccess
	}

	liquid, ok := b.Content.Liquid()
	if !ok {
		return DispenseDefault
	}
	liquid = liquid.WithDepth(8, false)
	if target := tx.Block(front); !canDisplace(target, liquid) && !replaceableWith(target, liquid) {
		return DispenseDefault
	}
	ctx.NewItem = NewStack(Bucket{}, 1)
	ctx.SubtractFromCount(1)
	tx.SetLiquid(front, liquid)
	tx.PlaySound(front.Vec3Centre(), sound.BucketEmpty{Liquid: liquid})
	postDispenseEvent(tx, gameevent.FluidPlace, front)
	return DispenseSuccess
}

// Dispense launches the egg from a dispenser.
func (e Egg) Dispense(pos cube.Pos, face cube.Face, tx *world.Tx, ctx *DispenseContext) DispenseResult {
	create := tx.World().EntityRegistry().Config().Egg
	if create == nil {
		return DispenseFailure
	}
	return dispenseProjectile(pos, face, tx, ctx, sound.ItemThrow{}, func(opts world.EntitySpawnOpts) *world.EntityHandle {
		return e.entity(tx, opts, nil)
	})
}

// Dispense launches the firework from a dispenser.
func (f Firework) Dispense(pos cube.Pos, face cube.Face, tx *world.Tx, ctx *DispenseContext) DispenseResult {
	create := tx.World().EntityRegistry().Config().Firework
	if create == nil {
		return DispenseFailure
	}
	return dispenseProjectile(pos, face, tx, ctx, sound.FireworkLaunch{}, func(opts world.EntitySpawnOpts) *world.EntityHandle {
		return create(opts, f, nil, 1.15, 0.04, false)
	})
}

// Dispense ignites the block in front of a dispenser.
func (FlintAndSteel) Dispense(pos cube.Pos, face cube.Face, tx *world.Tx, ctx *DispenseContext) DispenseResult {
	front := pos.Side(face)
	if target, ok := tx.Block(front).(ignitable); ok {
		if !target.Ignite(front, tx, nil) {
			return DispenseFailure
		}
		ctx.DamageItem(1)
		return DispenseSuccess
	}

	beforeName, _ := tx.Block(front).EncodeBlock()
	if beforeName == "minecraft:fire" || beforeName == "minecraft:soul_fire" {
		return DispenseFailure
	}
	starter, ok := fire(front, tx).(interface {
		Start(*world.Tx, cube.Pos)
	})
	if !ok {
		return DispenseFailure
	}
	starter.Start(tx, front)
	afterName, _ := tx.Block(front).EncodeBlock()
	if beforeName == afterName {
		return DispenseFailure
	}
	tx.PlaySound(front.Vec3Centre(), sound.Ignite{})
	ctx.DamageItem(1)
	return DispenseSuccess
}

// Dispense fills the bottle from the block or liquid in front of a dispenser, falling back to ordinary ejection when
// no bottle-filling target is present.
func (GlassBottle) Dispense(pos cube.Pos, face cube.Face, tx *world.Tx, ctx *DispenseContext) DispenseResult {
	front := pos.Side(face)
	b := tx.Block(front)
	filler, fromBlock := b.(bottleFiller)
	if !fromBlock {
		liquid, ok := tx.Liquid(front)
		if !ok {
			return DispenseDefault
		}
		filler, ok = liquid.(bottleFiller)
		if !ok {
			return DispenseDefault
		}
	}
	result, filled, ok := filler.FillBottle()
	if !ok {
		return DispenseDefault
	}
	if fromBlock && result != b {
		tx.SetBlock(front, result, nil)
	}
	ctx.NewItem = filled
	ctx.SubtractFromCount(1)
	return DispenseSuccess
}

// Dispense waxes the block in front of a dispenser.
func (Honeycomb) Dispense(pos cube.Pos, face cube.Face, tx *world.Tx, ctx *DispenseContext) DispenseResult {
	if !waxBlock(pos.Side(face), pos.Vec3Centre(), tx) {
		return DispenseFailure
	}
	ctx.SubtractFromCount(1)
	return DispenseSuccess
}

// Dispense launches the potion from a dispenser.
func (l LingeringPotion) Dispense(pos cube.Pos, face cube.Face, tx *world.Tx, ctx *DispenseContext) DispenseResult {
	create := tx.World().EntityRegistry().Config().LingeringPotion
	if create == nil {
		return DispenseFailure
	}
	return dispenseProjectile(pos, face, tx, ctx, sound.ItemThrow{}, func(opts world.EntitySpawnOpts) *world.EntityHandle {
		return create(opts, l.Type, nil)
	})
}

// dispenserShear shears an entity for a dispenser holding shears and reports whether it did. Package mob installs
// it with SetDispenserShear; without mobs it is nil.
var dispenserShear func(tx *world.Tx, e world.Entity) bool

// SetDispenserShear installs the function dispensers holding shears shear entities with. Call it once, from an init
// function.
func SetDispenserShear(f func(tx *world.Tx, e world.Entity) bool) { dispenserShear = f }

// Dispense shears the first mob in the block in front of a dispenser that can be sheared: a sheep, mooshroom, snow
// golem, bogged or sulfur cube that is neither a baby nor sheared already (ShearsItem::dispense, 26.60). The shears
// take one damage and the dispenser clicks. As on Bedrock, nothing to shear is no failure: the dispenser stays
// silent and keeps the shears as they were. Harvesting a beehive's honeycomb, which BDS tries first, is left to the
// hive.
func (s Shears) Dispense(pos cube.Pos, face cube.Face, tx *world.Tx, ctx *DispenseContext) DispenseResult {
	if dispenserShear == nil {
		return DispenseSuccess
	}
	front := pos.Side(face)
	box := cube.Box(float64(front[0]), float64(front[1]), float64(front[2]), float64(front[0]+1), float64(front[1]+1), float64(front[2]+1))
	// BDS fetches the entities whose box meets the block's (BlockSource::fetchEntities).
	for e := range tx.EntitiesWithin(box.Grow(2)) {
		if !e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
			continue
		}
		if dispenserShear(tx, e) {
			ctx.DamageItem(1)
			tx.PlaySound(front.Vec3Centre(), sound.Click{})
			// ShearsItem::dispense: shear, made by nobody.
			postDispenseEvent(tx, gameevent.Shear, front)
			return DispenseSuccess
		}
	}
	return DispenseSuccess
}

// Dispense launches the snowball from a dispenser.
func (s Snowball) Dispense(pos cube.Pos, face cube.Face, tx *world.Tx, ctx *DispenseContext) DispenseResult {
	create := tx.World().EntityRegistry().Config().Snowball
	if create == nil {
		return DispenseFailure
	}
	return dispenseProjectile(pos, face, tx, ctx, sound.ItemThrow{}, func(opts world.EntitySpawnOpts) *world.EntityHandle {
		return create(opts, nil)
	})
}

// Dispense launches the potion from a dispenser.
func (s SplashPotion) Dispense(pos cube.Pos, face cube.Face, tx *world.Tx, ctx *DispenseContext) DispenseResult {
	create := tx.World().EntityRegistry().Config().SplashPotion
	if create == nil {
		return DispenseFailure
	}
	return dispenseProjectile(pos, face, tx, ctx, sound.ItemThrow{}, func(opts world.EntitySpawnOpts) *world.EntityHandle {
		return create(opts, s.Type, nil)
	})
}

// waxBlock waxes the block at pos if it supports waxing.
func waxBlock(pos cube.Pos, source mgl64.Vec3, tx *world.Tx) bool {
	wa, ok := tx.Block(pos).(waxable)
	if !ok {
		return false
	}
	res, ok := wa.Wax(pos, source)
	if !ok {
		return false
	}
	tx.SetBlock(pos, res, nil)
	tx.PlaySound(pos.Vec3(), sound.SignWaxed{})
	return true
}

// postDispenseEvent posts ev at the centre of pos for a dispenser's doing, made by nobody. Nothing happens in a
// world without vibration listeners.
func postDispenseEvent(tx *world.Tx, ev gameevent.Event, pos cube.Pos) {
	if gameevent.Active(tx) {
		gameevent.Post(tx, ev, pos.Vec3Centre(), nil)
	}
}
