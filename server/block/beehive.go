package block

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Beehive is a beehive or a bee nest (BDS BeehiveBlock, BeehiveBlockActor). Up to three bees stay in it, fill it
// with honey from the nectar they bring and leave it in the daytime when it does not rain. Full of honey it gives
// honeycomb to shears and a honey bottle to a glass bottle; the bees inside come out angry unless a lit campfire is
// below. Only Crossfly Full has bees: without mobs a hive never gets any.
type Beehive struct {
	solid
	bass

	// Nest is true for a bee nest, false for a beehive.
	Nest bool
	// Facing is the direction the entrance faces. Bees leave through it.
	Facing cube.Direction
	// HoneyLevel is how full of honey the hive is, from 0 to 5. At 5 it can be harvested.
	HoneyLevel int

	// bees holds the block entity data. It is a pointer so the bees inside are not copied with every change of the
	// block's state, and so the block stays comparable. nil is a hive that never had any.
	bees *beehiveBees
}

// BeehiveOccupant is a mob staying in a beehive (BeehiveBlockActor::Occupant).
type BeehiveOccupant struct {
	// Identifier is the entity type of the mob, "minecraft:bee".
	Identifier string
	// Data is the saved mob (SaveData), as a world saves the entity. Empty data releases a new mob.
	Data map[string]any
	// TicksLeft is how many ticks the mob still stays (TicksLeftToStay): 600 when it entered, 2400 when it
	// brought nectar.
	TicksLeft int
}

// beehiveBees is the block entity data of a Beehive.
type beehiveBees struct {
	occupants []BeehiveOccupant
	// spawnBees is ShouldSpawnBees: a hive placed by world generation gets 2 or 3 bees on its first tick. Crossfly
	// does not generate nests, so it is only kept.
	spawnBees bool
}

// BeehiveHost is the part of the MobHost a beehive needs: it stores bees in the hive and lets them out again.
// Package mob implements it on its MobHost.
type BeehiveHost interface {
	// StoreMob saves the mob e as a world saves an entity, removes it from the world and returns its entity type
	// and data. It reports false if e cannot be stored.
	StoreMob(tx *world.Tx, e world.Entity) (id string, data map[string]any, ok bool)
	// ReleaseMob adds a mob saved by StoreMob (or a new mob of the type id if data is empty) at pos, facing yaw,
	// with home as its home position.
	ReleaseMob(tx *world.Tx, id string, data map[string]any, pos mgl64.Vec3, yaw float64, home cube.Pos) (world.Entity, bool)
	// HasNectar reports whether the mob e carries nectar (the minecraft:has_nectar property of bees).
	HasNectar(e world.Entity) bool
	// FireEvent fires a definition event on the mob e.
	FireEvent(e world.Entity, event string)
}

// beehiveHost returns the MobHost as a BeehiveHost, or nil without mobs.
func beehiveHost() BeehiveHost {
	h, _ := Mobs().(BeehiveHost)
	return h
}

// MobEventHandler is implemented by blocks that run the events mobs send to them (BDS Block::executeEvent): a
// trigger of a mob with "target": "block" fires its event on the block the trigger is about.
type MobEventHandler interface {
	// MobEvent runs the event the mob e sent to the block at pos.
	MobEvent(pos cube.Pos, tx *world.Tx, event string, e world.Entity)
}

// ExecuteMobEvent sends the event of the mob e to the block at pos, if that block handles mob events.
func ExecuteMobEvent(tx *world.Tx, pos cube.Pos, event string, e world.Entity) {
	if h, ok := tx.Block(pos).(MobEventHandler); ok {
		h.MobEvent(pos, tx, event, e)
	}
}

const (
	// beeReturnedToHive is the event bee.json sends to its hive.
	beeReturnedToHive = "minecraft:bee_returned_to_hive"
	// Events fired on the bees a hive lets out (BeehiveBlockActor::_tickOccupant, evictAll, tryAdmit).
	beeExitedHive         = "minecraft:exited_hive"
	beeExitedDisturbed    = "minecraft:exited_disturbed_hive"
	beeExitedHiveOnFire   = "minecraft:exited_hive_on_fire"
	beeHiveFull           = "minecraft:hive_full"
	beehiveMaxOccupants   = 3
	beehiveStayTicks      = 600
	beehiveNectarTicks    = 2400
	beehiveMaxHoneyLevel  = 5
	beehiveCampfireBlocks = 5
)

// Occupants returns the mobs staying in the hive.
func (h Beehive) Occupants() []BeehiveOccupant {
	if h.bees == nil {
		return nil
	}
	return append([]BeehiveOccupant(nil), h.bees.occupants...)
}

// MobEvent handles minecraft:bee_returned_to_hive (BeehiveBlock::executeEvent): the bee enters the hive, or gets
// minecraft:hive_full if three bees are in already.
func (h Beehive) MobEvent(pos cube.Pos, tx *world.Tx, event string, e world.Entity) {
	if event != beeReturnedToHive {
		return
	}
	host := beehiveHost()
	if host == nil {
		return
	}
	if !h.tryAdmit(pos, tx, e, host) {
		host.FireEvent(e, beeHiveFull)
	}
}

// tryAdmit is BeehiveBlockActor::tryAdmit: the hive takes the mob in if it holds fewer than three, keeping it for 600
// ticks, or 2400 if it brought nectar.
func (h Beehive) tryAdmit(pos cube.Pos, tx *world.Tx, e world.Entity, host BeehiveHost) bool {
	if h.bees != nil && len(h.bees.occupants) >= beehiveMaxOccupants {
		return false
	}
	ticks := beehiveStayTicks
	if host.HasNectar(e) {
		ticks = beehiveNectarTicks
	}
	id, data, ok := host.StoreMob(tx, e)
	if !ok {
		return false
	}
	if h.bees == nil {
		h.bees = &beehiveBees{}
		tx.SetBlock(pos, h, nil)
	}
	h.bees.occupants = append(h.bees.occupants, BeehiveOccupant{Identifier: id, Data: data, TicksLeft: ticks})
	tx.PlaySound(pos.Vec3Centre(), beehiveSound("block.beehive.enter"))
	return true
}

// Tick is BeehiveBlockActor::tick. A hive without bees does nothing; one with bees hums now and then and lets out
// the bees that may leave.
func (h Beehive) Tick(_ int64, pos cube.Pos, tx *world.Tx) {
	if h.bees == nil || len(h.bees.occupants) == 0 {
		return
	}
	host := beehiveHost()
	if host == nil {
		return
	}
	if rand.IntN(1000) < 5 {
		tx.PlaySound(pos.Vec3Centre(), beehiveSound("block.beehive.work"))
	}
	level := h.HoneyLevel
	kept := h.bees.occupants[:0]
	for _, o := range h.bees.occupants {
		if !h.tickOccupant(pos, tx, &o, host, &level) {
			kept = append(kept, o)
		}
	}
	clear(h.bees.occupants[len(kept):])
	h.bees.occupants = kept
	if level != h.HoneyLevel {
		h.HoneyLevel = level
		tx.SetBlock(pos, h, nil)
	}
}

// tickOccupant is BeehiveBlockActor::_tickOccupant: it counts down the occupant's stay and lets it out once the stay
// is over, in the daytime of a world with a sky, while it does not rain, with a 1 in 20 chance a tick and only
// through a free entrance. A bee with nectar fills the hive with honey on leaving. It reports whether the occupant
// left.
func (h Beehive) tickOccupant(pos cube.Pos, tx *world.Tx, o *BeehiveOccupant, host BeehiveHost, level *int) bool {
	if o.TicksLeft > 0 {
		o.TicksLeft--
	}
	if tx.World().Dimension() == world.Overworld && !beehiveDaytime(tx) {
		return false
	}
	if tx.Raining() || o.TicksLeft > 0 || rand.IntN(20) != 0 {
		return false
	}
	face := h.Facing.Face()
	if _, air := tx.Block(pos.Side(face)).(Air); !air {
		return false
	}
	e, ok := h.release(pos, tx, *o, face, host)
	if !ok {
		return false
	}
	if host.HasNectar(e) {
		*level = deliverNectar(*level)
	}
	tx.PlaySound(pos.Vec3(), beehiveSound("block.beehive.exit"))
	host.FireEvent(e, beeExitedHive)
	return true
}

// deliverNectar is BeehiveBlock::deliverNectar: a bee with nectar raises the honey level by one, by two in one of
// 100 cases, up to 5.
func deliverNectar(level int) int {
	if level >= beehiveMaxHoneyLevel {
		return level
	}
	add := 1
	if rand.IntN(100) == 0 {
		add = 2
	}
	return min(level+add, beehiveMaxHoneyLevel)
}

// release is BeehiveBlockActor::_revive: the occupant comes back in the middle of the block on the side face of the
// hive, facing away from it, with the hive as its home.
func (h Beehive) release(pos cube.Pos, tx *world.Tx, o BeehiveOccupant, face cube.Face, host BeehiveHost) (world.Entity, bool) {
	at := pos.Side(face).Vec3().Add(mgl64.Vec3{0.5, 0.5, 0.5})
	yaw := 0.0
	if face != cube.FaceUp && face != cube.FaceDown {
		yaw = directionYaw(face.Direction())
	}
	return host.ReleaseMob(tx, o.Identifier, o.Data, at, yaw, pos)
}

// directionYaw is the yaw of an entity looking towards d.
func directionYaw(d cube.Direction) float64 {
	switch d {
	case cube.West:
		return 90
	case cube.North:
		return 180
	case cube.East:
		return -90
	}
	return 0
}

// evictAll is BeehiveBlockActor::evictAll: every occupant leaves at once, through a random free side of the hive (a
// random horizontal side if none is free). Disturbed bees (the hive broken or harvested) get
// minecraft:exited_disturbed_hive and go for the nearest player, bees driven out by fire
// minecraft:exited_hive_on_fire.
func (h Beehive) evictAll(pos cube.Pos, tx *world.Tx, disturbed bool) {
	if h.bees == nil || len(h.bees.occupants) == 0 {
		return
	}
	host := beehiveHost()
	if host == nil {
		return
	}
	var free []cube.Face
	for _, f := range cube.Faces() {
		if _, air := tx.Block(pos.Side(f)).(Air); air {
			free = append(free, f)
		}
	}
	event := beeExitedHiveOnFire
	if disturbed {
		event = beeExitedDisturbed
	}
	occupants := h.bees.occupants
	h.bees.occupants = nil
	for _, o := range occupants {
		var face cube.Face
		if len(free) == 0 {
			face = cube.HorizontalFaces()[rand.IntN(4)]
		} else {
			face = free[rand.IntN(len(free))]
		}
		if e, ok := h.release(pos, tx, o, face, host); ok {
			host.FireEvent(e, event)
		}
	}
}

// fireNearby is called by fire next to the hive (FireBlock::checkBurn): the bees inside flee.
func (h Beehive) fireNearby(pos cube.Pos, tx *world.Tx) {
	h.evictAll(pos, tx, false)
}

// Activate harvests a hive full of honey (BeehiveBlock::use): shears drop three honeycombs, a glass bottle becomes a
// honey bottle. Without a lit campfire below, the bees inside come out angry.
func (h Beehive) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	if h.HoneyLevel < beehiveMaxHoneyLevel {
		return false
	}
	held, _ := u.HeldItems()
	_, shears := held.Item().(item.Shears)
	_, bottle := held.Item().(item.GlassBottle)
	if !shears && !bottle {
		return false
	}
	if !beehiveCampfireBelow(pos, tx) {
		h.evictAll(pos, tx, true)
	}
	if shears {
		for range 3 {
			dropItem(tx, item.NewStack(item.Honeycomb{}, 1), pos.Vec3Centre())
		}
		tx.PlaySound(pos.Vec3Centre(), beehiveSound("block.beehive.shear"))
	} else {
		ctx.SubtractFromCount(1)
		ctx.NewItem = item.NewStack(item.HoneyBottle{}, 1)
		tx.PlaySound(pos.Vec3Centre(), sound.BucketFill{Liquid: Water{Still: true, Depth: 8}})
	}
	h.HoneyLevel = 0
	tx.SetBlock(pos, h, nil)
	return true
}

// beehiveCampfireBelow reports whether a lit campfire (or soul campfire) is at most five blocks below pos with only
// air between them, which keeps the bees calm when the hive is harvested.
func beehiveCampfireBelow(pos cube.Pos, tx *world.Tx) bool {
	for i := 1; i <= beehiveCampfireBlocks; i++ {
		switch b := tx.Block(pos.Sub(cube.Pos{0, i, 0})).(type) {
		case Campfire:
			return !b.Extinguished
		case Air:
		default:
			return false
		}
	}
	return false
}

// beehiveDaytime is Dimension::isDay for the overworld: the sky is less than 4 out of 11 dark, rain included.
func beehiveDaytime(tx *world.Tx) bool {
	f := float64((tx.World().Time()%24000+24000)%24000)/24000 - 0.25
	if f < 0 {
		f++
	}
	if f > 1 {
		f--
	}
	g := 1 - (math.Cos(f*math.Pi)+1)/2
	a := f + (g-f)/3
	d := 1 - (math.Cos(a*2*math.Pi)*2 + 0.5)
	d = 1 - math.Max(0, math.Min(1, d))
	if tx.Raining() {
		d *= 1 - 5.0/16
	}
	if tx.Thundering() {
		d *= 1 - 5.0/16
	}
	return int((1-d)*11) < 4
}

// BreakInfo ...
func (h Beehive) BreakInfo() BreakInfo {
	hardness := 0.6
	if h.Nest {
		hardness = 0.3
	}
	return newBreakInfo(hardness, alwaysHarvestable, axeEffective, func(_ item.Tool, enchantments []item.Enchantment) []item.Stack {
		if hasSilkTouch(enchantments) {
			return []item.Stack{item.NewStack(h.withOccupantsItem(), 1)}
		}
		if h.Nest {
			return nil
		}
		return []item.Stack{item.NewStack(Beehive{}, 1)}
	}).withBreakHandler(func(pos cube.Pos, tx *world.Tx, u item.User) {
		h.playerBroke(pos, tx, u)
	})
}

// playerBroke is BeehiveBlock::playerWillDestroy. In creative mode the hive drops as an item that keeps its bees
// and honey. Broken without silk touch, its bees come out angry.
func (h Beehive) playerBroke(pos cube.Pos, tx *world.Tx, u item.User) {
	g, ok := u.(interface{ GameMode() world.GameMode })
	if !ok {
		return
	}
	if g.GameMode().CreativeInventory() {
		dropItem(tx, item.NewStack(h.withOccupantsItem(), 1), pos.Vec3Centre())
		return
	}
	held, _ := u.HeldItems()
	if hasSilkTouch(held.Enchantments()) {
		return
	}
	h.evictAll(pos, tx, true)
}

// withOccupantsItem is BeehiveBlock::getHiveItemWithOccupants: the hive as an item with a copy of its bees and its
// honey level.
func (h Beehive) withOccupantsItem() Beehive {
	return Beehive{Nest: h.Nest, HoneyLevel: h.HoneyLevel, bees: h.bees.clone()}
}

// clone copies the block entity data, nil if there are no bees to keep.
func (b *beehiveBees) clone() *beehiveBees {
	if b == nil || (len(b.occupants) == 0 && !b.spawnBees) {
		return nil
	}
	return &beehiveBees{occupants: append([]BeehiveOccupant(nil), b.occupants...), spawnBees: b.spawnBees}
}

// FlammabilityInfo ...
func (h Beehive) FlammabilityInfo() FlammabilityInfo {
	// Java fallback: no Bedrock data (Blocks.BEE_NEST 30/20, Blocks.BEEHIVE 5/20 in FireBlock.bootStrap).
	if h.Nest {
		return newFlammabilityInfo(30, 20, true)
	}
	return newFlammabilityInfo(5, 20, true)
}

// UseOnBlock places the hive with its entrance towards the player. A hive item that kept its bees places them
// with it.
func (h Beehive) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, _, used = firstReplaceable(tx, pos, face, h)
	if !used {
		return
	}
	h.Facing = user.Rotation().Direction().Opposite()
	// BeehiveBlock::onPlayerPlace disables the bees of world generation.
	h.bees = h.bees.clone()
	if h.bees != nil {
		h.bees.spawnBees = false
	}
	place(tx, pos, h, user, ctx)
	return placed(ctx)
}

// EncodeNBT is the Beehive block entity (BeehiveBlockActor::save): Occupants with ActorIdentifier,
// TicksLeftToStay and SaveData, and ShouldSpawnBees. HoneyLevel is written too when above 0, as
// BeehiveBlock::getHiveItemWithOccupants does for the item.
func (h Beehive) EncodeNBT() map[string]any {
	m := map[string]any{"id": "Beehive", "ShouldSpawnBees": boolByte(h.bees != nil && h.bees.spawnBees)}
	if h.bees != nil && len(h.bees.occupants) > 0 {
		list := make([]any, 0, len(h.bees.occupants))
		for _, o := range h.bees.occupants {
			data := o.Data
			if data == nil {
				data = map[string]any{}
			}
			list = append(list, map[string]any{
				"ActorIdentifier": o.Identifier,
				"TicksLeftToStay": int32(o.TicksLeft),
				"SaveData":        data,
			})
		}
		m["Occupants"] = list
	}
	if h.HoneyLevel > 0 {
		m["HoneyLevel"] = int32(h.HoneyLevel)
	}
	return m
}

// DecodeNBT is BeehiveBlockActor::load (and the HoneyLevel of a hive item).
func (h Beehive) DecodeNBT(data map[string]any) any {
	b := &beehiveBees{spawnBees: nbtconv.Bool(data, "ShouldSpawnBees")}
	for _, v := range nbtconv.Slice(data, "Occupants") {
		o, ok := v.(map[string]any)
		if !ok {
			continue
		}
		id := nbtconv.String(o, "ActorIdentifier")
		save, ok := o["SaveData"].(map[string]any)
		if id == "" || !ok {
			continue
		}
		b.occupants = append(b.occupants, BeehiveOccupant{Identifier: id, Data: save, TicksLeft: int(nbtconv.Int32(o, "TicksLeftToStay"))})
	}
	if len(b.occupants) > beehiveMaxOccupants {
		b.occupants = b.occupants[:beehiveMaxOccupants]
	}
	h.bees = b
	if _, ok := data["HoneyLevel"]; ok {
		h.HoneyLevel = min(max(int(nbtconv.Int32(data, "HoneyLevel")), 0), beehiveMaxHoneyLevel)
	}
	return h
}

// EncodeItem ...
func (h Beehive) EncodeItem() (name string, meta int16) {
	if h.Nest {
		return "minecraft:bee_nest", 0
	}
	return "minecraft:beehive", 0
}

// EncodeBlock ...
func (h Beehive) EncodeBlock() (string, map[string]any) {
	name, _ := h.EncodeItem()
	return name, map[string]any{"direction": int32(horizontalDirection(h.Facing)), "honey_level": int32(h.HoneyLevel)}
}

// beehiveSound is a level sound event of a hive (block.beehive.*), sent to Bedrock clients as it is and to Java
// clients as the minecraft:block.beehive.* sound.
type beehiveSound string

// Play ...
func (beehiveSound) Play(*world.World, mgl64.Vec3) {}

// LevelSoundEvent ...
func (s beehiveSound) LevelSoundEvent() (event, entityType string, baby bool) {
	return string(s), ":", false
}

// allBeehives returns every beehive and bee nest state.
func allBeehives() (b []world.Block) {
	for _, nest := range []bool{false, true} {
		for _, d := range cube.Directions() {
			for level := 0; level <= beehiveMaxHoneyLevel; level++ {
				b = append(b, Beehive{Nest: nest, Facing: d, HoneyLevel: level})
			}
		}
	}
	return
}

func init() {
	registerAll(allBeehives())
	world.RegisterItem(Beehive{})
	world.RegisterItem(Beehive{Nest: true})
}
