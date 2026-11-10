//go:build crossfly_full

package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/gameevent"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Dispensers and droppers with redstone=dragonfly follow Bedrock Edition's rules (DispenserBlock, DropperBlock and
// DropperBlockActor in BDS):
//
//   - The power a dispenser receives is what the redstone engine resolves for it: no quasi-connectivity and no block
//     update detection. When the power turns on while the dispenser is not triggered, it becomes triggered and fires
//     4 ticks later; when the power turns off it is no longer triggered. Only a rising edge fires it.
//   - It fires an item from a random non-empty slot. A block item uses the block's dispense behaviour, any other
//     item its own; an item with none, or whose behaviour does not apply, is ejected: one item out of the front.
//   - A dropper puts one item of a random slot into the container in front (a block or a chest or hopper minecart),
//     by the container's sided rules. A full container keeps the item in the dropper. Facing down onto a composter
//     it composts the item. With no container in front it ejects the item.

// dfDispenserDelay is the delay between a dispenser starting to be powered and firing: 4 ticks.
const dfDispenserDelay = time.Second / 5

// dfPowerUpdate sets the dispenser triggered while it is powered and schedules it to fire when it starts to be.
func (d Dispenser) dfPowerUpdate(pos cube.Pos, tx *world.Tx, power int) {
	cur, ok := tx.Block(pos).(Dispenser)
	if !ok {
		return
	}
	powered := power > 0
	if cur.Triggered == powered {
		return
	}
	cur.Triggered = powered
	// Only the triggered bit changes: the power the engine holds for the dispenser stays.
	tx.SetBlock(pos, cur, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true, KeepRedstonePower: true})
	if !powered {
		return
	}
	// The tick fires whatever the triggered bit is by then (a short pulse still fires): scheduled ticks are keyed by
	// block state, so both states are queued and only the one left at that time runs.
	tx.ScheduleBlockUpdate(pos, cur, dfDispenserDelay)
	cur.Triggered = false
	tx.ScheduleBlockUpdate(pos, cur, dfDispenserDelay)
}

// dfFire fires the dispenser or dropper (DispenserBlock::dispenseFrom, DropperBlock::dispenseFrom).
func (d Dispenser) dfFire(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	slot, stack, ok := dfRandomSlot(d.inventory, r)
	if !ok {
		tx.PlaySound(pos.Vec3Centre(), sound.ClickFail{})
		if !d.Dropper {
			postBlockEvent(tx, gameevent.DispenseFail, pos, nil)
		}
		return
	}
	if d.Dropper {
		d.dfDrop(pos, tx, r)
		return
	}
	switch d.dfDispenseItem(slot, stack, pos, tx, r) {
	case item.DispenseSuccess:
	case item.DispenseFailure:
		tx.PlaySound(pos.Vec3Centre(), sound.ClickFail{})
		postBlockEvent(tx, gameevent.DispenseFail, pos, nil)
	default:
		d.dfEject(slot, stack, pos, tx, r)
	}
}

// dfRandomSlot picks one of the non-empty slots of inv at random (DispenserBlockActor::getRandomSlot).
func dfRandomSlot(inv *inventory.Inventory, r *rand.Rand) (int, item.Stack, bool) {
	if inv == nil {
		return 0, item.Stack{}, false
	}
	slot, n := -1, 0
	var picked item.Stack
	for i, s := range inv.Slots() {
		if s.Empty() {
			continue
		}
		// Reservoir sampling: each non-empty slot ends up picked with the same chance.
		n++
		if r.IntN(n) == 0 {
			slot, picked = i, s
		}
	}
	return slot, picked, slot >= 0
}

// dfDispenseItem runs the dispense behaviour of the item in slot. DispenseDefault means the item is ejected.
func (d Dispenser) dfDispenseItem(slot int, stack item.Stack, pos cube.Pos, tx *world.Tx, r *rand.Rand) item.DispenseResult {
	ctx := &item.DispenseContext{Rand: r}
	res := item.DispenseDefault
	if behaviour, ok := stack.Item().(item.Dispensable); ok {
		res = behaviour.Dispense(pos, d.Facing, tx, ctx)
	}
	if res == item.DispenseDefault {
		res = d.dfDispenseBedrock(stack, pos, tx, ctx)
	}
	if res == item.DispenseSuccess && !d.applyDispenseContext(slot, stack, ctx, pos, tx, r) {
		return item.DispenseFailure
	}
	return res
}

// dfDispenseBedrock runs the dispense behaviours BDS has on top of the ones items share with Java redstone: spawn
// eggs and armour stands, minecarts, shulker boxes, armour onto entities, water bottles onto dirt.
func (d Dispenser) dfDispenseBedrock(stack item.Stack, pos cube.Pos, tx *world.Tx, ctx *item.DispenseContext) item.DispenseResult {
	front := pos.Side(d.Facing)
	switch it := stack.Item().(type) {
	case item.SpawnEgg:
		at := dfActorPlacePos(pos, d.Facing)
		if !it.SpawnFromDispenser(tx, at, at.Add(dispenserDirection(d.Facing)), stack.CustomName()) {
			return item.DispenseDefault
		}
		ctx.SubtractFromCount(1)
		tx.PlaySound(pos.Vec3Centre(), sound.Click{})
		postBlockEvent(tx, gameevent.EntityPlace, front, nil)
		return item.DispenseSuccess
	case item.ArmourStand:
		create := tx.World().EntityRegistry().Config().ArmourStand
		if create == nil {
			return item.DispenseDefault
		}
		at := dfActorPlacePos(pos, d.Facing)
		tx.AddEntity(create(world.EntitySpawnOpts{Position: at, NameTag: stack.CustomName()}))
		ctx.SubtractFromCount(1)
		tx.PlaySound(at, sound.ArmourStandPlace{})
		postBlockEvent(tx, gameevent.EntityPlace, front, nil)
		return item.DispenseSuccess
	case item.Minecart:
		return dfDispenseMinecart(it, stack, pos, d.Facing, tx, ctx)
	case ShulkerBox:
		return dfDispenseShulkerBox(it, stack, front, d.Facing, tx, ctx)
	case item.Potion:
		return dfDispenseWaterBottle(it, pos, front, tx, ctx)
	}
	if dfDispenseEquipment(stack, front, tx, ctx) {
		return item.DispenseSuccess
	}
	return item.DispenseDefault
}

// dfActorPlacePos is where a dispenser places a mob or armour stand (ActorPlacerItem::dispense): in front of it, the
// side of an entity about 0.6 wide against the dispenser, at half its height for the sides.
func dfActorPlacePos(pos cube.Pos, facing cube.Face) mgl64.Vec3 {
	const halfWidth = 0.3
	step := dispenserDirection(facing)
	at := pos.Vec3Centre().Add(step.Mul(halfWidth + 0.5))
	switch facing {
	case cube.FaceUp:
		at[1] = float64(pos[1]) + 1
	case cube.FaceDown:
		at[1] = float64(pos[1]) - 1
	}
	return at
}

// dfDispenseMinecart puts a minecart on the rail in front of the dispenser, or on one a block lower when the block in
// front is air (MinecartItem::dispense). With no rail there the minecart is ejected.
func dfDispenseMinecart(m item.Minecart, stack item.Stack, pos cube.Pos, facing cube.Face, tx *world.Tx, ctx *item.DispenseContext) item.DispenseResult {
	create := tx.World().EntityRegistry().Config().Minecart
	if create == nil {
		return item.DispenseDefault
	}
	front := pos.Side(facing)
	y := float64(front[1])
	if _, ok := railAt(tx, front); !ok {
		if _, air := tx.Block(front).(Air); !air {
			return item.DispenseDefault
		}
		if _, ok := railAt(tx, front.Side(cube.FaceDown)); !ok {
			return item.DispenseDefault
		}
		y--
	}
	at := mgl64.Vec3{float64(front[0]) + 0.5, y + 0.5, float64(front[2]) + 0.5}
	switch facing {
	case cube.FaceWest:
		at[0] -= 0.125
	case cube.FaceEast:
		at[0] += 0.125
	case cube.FaceNorth:
		at[2] -= 0.125
	case cube.FaceSouth:
		at[2] += 0.125
	}
	tx.AddEntity(create(world.EntitySpawnOpts{Position: at, NameTag: stack.CustomName()}, int(m.Kind)))
	ctx.SubtractFromCount(1)
	tx.PlaySound(pos.Vec3Centre(), sound.Click{})
	postBlockEvent(tx, gameevent.EntityPlace, front, nil)
	return item.DispenseSuccess
}

// dfDispenseShulkerBox places the shulker box with its contents in front of the dispenser, opening towards where the
// dispenser faces (ShulkerBoxBlock::dispense). If it can't be placed there, it is ejected.
func dfDispenseShulkerBox(s ShulkerBox, stack item.Stack, front cube.Pos, facing cube.Face, tx *world.Tx, ctx *item.DispenseContext) item.DispenseResult {
	if !replaceableWith(tx, front, s) {
		return item.DispenseDefault
	}
	s = s.initialised()
	s.Facing = facing
	if name := stack.CustomName(); name != "" {
		s.CustomName = name
	}
	tx.SetBlock(front, s, nil)
	tx.PlaySound(front.Vec3Centre(), sound.BlockPlace{Block: s})
	postBlockEvent(tx, gameevent.BlockPlace, front, nil)
	ctx.SubtractFromCount(1)
	return item.DispenseSuccess
}

// dfDispenseWaterBottle turns dirt, coarse dirt or rooted dirt in front of the dispenser into mud and gives back a
// glass bottle (PotionItem::dispense with a water bottle). Other potions are ejected.
func dfDispenseWaterBottle(p item.Potion, pos, front cube.Pos, tx *world.Tx, ctx *item.DispenseContext) item.DispenseResult {
	if p.Type != potion.Water() {
		return item.DispenseDefault
	}
	switch tx.Block(front).(type) {
	case Dirt:
	default:
		if name, _ := tx.Block(front).EncodeBlock(); name != "minecraft:dirt_with_roots" {
			return item.DispenseDefault
		}
	}
	tx.SetBlock(front, Mud{}, nil)
	tx.PlaySound(pos.Vec3Centre(), sound.Click{})
	ctx.SubtractFromCount(1)
	ctx.NewItem = item.NewStack(item.GlassBottle{}, 1)
	return item.DispenseSuccess
}

// dfArmourWearer is an entity that wears armour: a player, a mob or an armour stand.
type dfArmourWearer interface {
	world.Entity
	Armour() *inventory.Armour
}

// dfDispenseEquipment puts a piece of armour (or another item worn on the head, such as a skull or a carved pumpkin)
// onto the first entity in the block in front of the dispenser that has nothing in that slot
// (DispensableItemUtils::tryDispenseEquipment). It reports whether it did.
func dfDispenseEquipment(stack item.Stack, front cube.Pos, tx *world.Tx, ctx *item.DispenseContext) bool {
	var get func(*inventory.Armour) item.Stack
	var set func(*inventory.Armour, item.Stack)
	switch it := stack.Item().(type) {
	case item.HelmetType:
		if !it.Helmet() {
			return false
		}
		get, set = (*inventory.Armour).Helmet, (*inventory.Armour).SetHelmet
	case item.ChestplateType:
		if !it.Chestplate() {
			return false
		}
		get, set = (*inventory.Armour).Chestplate, (*inventory.Armour).SetChestplate
	case item.LeggingsType:
		if !it.Leggings() {
			return false
		}
		get, set = (*inventory.Armour).Leggings, (*inventory.Armour).SetLeggings
	case item.BootsType:
		if !it.Boots() {
			return false
		}
		get, set = (*inventory.Armour).Boots, (*inventory.Armour).SetBoots
	case item.Elytra:
		get, set = (*inventory.Armour).Chestplate, (*inventory.Armour).SetChestplate
	case interface{ Helmet() bool }:
		// Skulls and carved pumpkins: worn, but no armour.
		if !it.Helmet() {
			return false
		}
		get, set = (*inventory.Armour).Helmet, (*inventory.Armour).SetHelmet
	default:
		return false
	}
	box := cube.Box(0, 0, 0, 1, 1, 1).Translate(front.Vec3())
	for e := range tx.EntitiesWithin(box.Grow(2)) {
		w, ok := e.(dfArmourWearer)
		if !ok || !entityIntersects(e, box) {
			continue
		}
		a := w.Armour()
		if a == nil || !get(a).Empty() {
			continue
		}
		set(a, stack.Grow(1-stack.Count()))
		ctx.SubtractFromCount(1)
		tx.PlaySound(e.Position(), sound.EquipItem{Item: stack.Item()})
		return true
	}
	return false
}

// dfEject ejects one item of the slot out of the front of the dispenser (DispenserBlock::ejectItem).
func (d Dispenser) dfEject(slot int, stack item.Stack, pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	create := tx.World().EntityRegistry().Config().Item
	if create == nil {
		return
	}
	if err := d.inventory.SetItem(slot, stack.Grow(-1)); err != nil {
		return
	}
	tx.AddEntity(create(dfEjectOpts(pos, d.Facing, r), stack.Grow(1-stack.Count())))
	tx.PlaySound(pos.Vec3Centre(), sound.Click{})
}

// dfDispensePos is where things leave a dispenser (DispenserBlock::getDispensePosition): 0.2 blocks out of the front
// face, 0.3 up from the bottom for the sides, 0.4 below the bottom facing down.
func dfDispensePos(pos cube.Pos, facing cube.Face) mgl64.Vec3 {
	p := pos.Vec3().Add(mgl64.Vec3{0.5, 0.3, 0.5})
	switch facing {
	case cube.FaceWest:
		p[0] = float64(pos[0]) - 0.2
	case cube.FaceEast:
		p[0] = float64(pos[0]) + 1.2
	case cube.FaceNorth:
		p[2] = float64(pos[2]) - 0.2
	case cube.FaceSouth:
		p[2] = float64(pos[2]) + 1.2
	case cube.FaceDown:
		p[1] = float64(pos[1]) - 0.4
	case cube.FaceUp:
		p[1] = float64(pos[1]) + 1
	}
	return p
}

// dfEjectOpts returns the spawn options of an item ejected by a dispenser or dropper: a little lower than the
// dispense position, thrown out at 0.2 to 0.3 blocks a tick and 0.2 up, with a small random spread.
func dfEjectOpts(pos cube.Pos, facing cube.Face, r *rand.Rand) world.EntitySpawnOpts {
	p := dfDispensePos(pos, facing)
	if facing == cube.FaceDown || facing == cube.FaceUp {
		p[1] -= 0.125
	} else {
		p[1] -= 0.15625
	}
	speed := r.Float64()*0.1 + 0.2
	step := dispenserDirection(facing)
	v := mgl64.Vec3{step[0] * speed, 0.2, step[2] * speed}
	for i := range v {
		v[i] += r.NormFloat64() * 0.0075 * 6
	}
	return world.EntitySpawnOpts{Position: p, Velocity: v}
}

// dfDrop is DropperBlock::dispenseFrom: one item of a random slot goes into the container in front, or is ejected
// when there is none.
func (d Dispenser) dfDrop(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	front := pos.Side(d.Facing)
	slot, stack, ok := dfRandomSlot(d.inventory, r)
	if !ok {
		return
	}
	if dest, ok := javaContainerAt(tx, front); ok {
		one := stack.Grow(1 - stack.Count())
		if _, composter := dest.block.(Composter); composter && d.Facing != cube.FaceDown {
			// Only a dropper above a composter fills it; one facing it from the side does nothing.
			return
		}
		if dest.inv == nil {
			if into, ok := dest.block.(HopperInsertable); ok {
				// Furnaces, brewing stands, composters and the like take it by their own sided rules.
				source := NewHopper()
				source.Facing = d.Facing
				_ = source.inventory.SetItem(0, one)
				if into.InsertItem(source, front, tx) {
					_ = d.inventory.SetItem(slot, stack.Grow(-1))
				}
			}
			return
		}
		if dfAddItem(dest, one, tx).Empty() {
			_ = d.inventory.SetItem(slot, stack.Grow(-1))
		}
		return
	}
	d.dfEject(slot, stack, pos, tx, r)
}
