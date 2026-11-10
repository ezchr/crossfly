package block

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// TurtleEgg is a cluster of one to four turtle eggs. Turtles lay them on sand; there they crack and finally hatch
// into baby turtles, mostly at night. Mobs and players walking or falling on them may trample them.
type TurtleEgg struct {
	transparent

	// AdditionalCount is the amount of eggs in the cluster past the first, 0-3.
	AdditionalCount int
	// Cracks is how far the eggs cracked, 0-2. Cracked twice, the eggs hatch at their next step.
	Cracks int
}

// Model ...
func (TurtleEgg) Model() world.BlockModel {
	return turtleEggModel{}
}

// turtleEggModel is the model of turtle eggs: the shape TurtleEggBlock gives itself, 0.6 wide and deep and
// 0.45 high, whatever the count.
type turtleEggModel struct{}

// BBox ...
func (turtleEggModel) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	return []cube.BBox{cube.Box(0.2, 0, 0.2, 0.8, 0.45, 0.8)}
}

// FaceSolid ...
func (turtleEggModel) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool {
	return false
}

// turtleEggPlaceDelay is the delay of the first step of placed turtle eggs (TurtleEggBlock::onPlace queues its
// tick 1000 ticks out).
const turtleEggPlaceDelay = 1000 * time.Second / 20

// turtleEggTickDelay returns the delay to the next step of turtle eggs: 750-1499 ticks (TurtleEggBlock::tick).
func turtleEggTickDelay() time.Duration {
	return time.Duration(750+rand.IntN(750)) * time.Second / 20
}

// UseOnBlock adds an egg to the turtle eggs clicked (TurtleEggBlock::use, up to four), or places one turtle egg
// where TurtleEggBlock::mayPlace allows it: on top of a block that supports it, except magma and wooden fences.
func (t TurtleEgg) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if existing, ok := tx.Block(pos).(TurtleEgg); ok {
		if existing.AdditionalCount >= 3 {
			return false
		}
		existing.AdditionalCount++
		place(tx, pos, existing, user, ctx)
		if placed(ctx) {
			if turtleEggSoil(tx, pos) {
				tx.AddParticle(pos.Vec3(), particle.BoneMeal{})
			}
			existing.OnPlace(pos, tx)
		}
		return placed(ctx)
	}
	pos, _, used := firstReplaceable(tx, pos, face, t)
	if !used || !turtleEggSupported(tx, pos) {
		return false
	}
	t = TurtleEgg{}
	place(tx, pos, t, user, ctx)
	if placed(ctx) {
		t.OnPlace(pos, tx)
	}
	return placed(ctx)
}

// turtleEggSupported reports whether turtle eggs may be placed at pos (TurtleEggBlock::mayPlace): the block below
// must give support to its top, and must be neither magma nor a wooden fence.
func turtleEggSupported(tx *world.Tx, pos cube.Pos) bool {
	below := pos.Side(cube.FaceDown)
	b := tx.Block(below)
	switch b.(type) {
	case Magma, WoodFence:
		return false
	}
	return b.Model().FaceSolid(below, cube.FaceUp, tx)
}

// turtleEggSoil reports whether the block below pos is the sand turtle eggs hatch on (VanillaBlockTypeIds::Sand:
// not red sand).
func turtleEggSoil(tx *world.Tx, pos cube.Pos) bool {
	s, ok := tx.Block(pos.Side(cube.FaceDown)).(Sand)
	return ok && !s.Red
}

// OnPlace starts the steps of turtle eggs set at pos (TurtleEggBlock::onPlace): the first one comes 1000 ticks
// later. Turtles laying eggs call it too.
func (t TurtleEgg) OnPlace(pos cube.Pos, tx *world.Tx) {
	tx.ScheduleBlockUpdate(pos, t, turtleEggPlaceDelay)
}

// ScheduledTick is a step of the turtle eggs (TurtleEggBlock::tick). On sand, late at night (time of day
// 0.65-0.69, just before sunrise) or else with a 1 in 500 chance, the eggs crack a level; cracked twice they
// hatch instead: every egg becomes a baby turtle (minecraft:entity_born, persistent) whose home is the egg's
// block, and the block goes. The next step comes 750-1499 ticks later.
func (t TurtleEgg) ScheduledTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	defer func() {
		if b, ok := tx.Block(pos).(TurtleEgg); ok {
			tx.ScheduleBlockUpdate(pos, b, turtleEggTickDelay())
		}
	}()
	if !turtleEggHatchTime(tx, r) || !turtleEggSoil(tx, pos) {
		return
	}
	if t.Cracks < 2 {
		tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: "block.turtle_egg.crack", Volume: 1, Pitch: 1})
		t.Cracks++
		tx.SetBlock(pos, t, nil)
		return
	}
	tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: "mob.turtle_baby.born", Volume: 1, Pitch: 1})
	for i := 0; i <= t.AdditionalCount; i++ {
		tx.AddParticle(pos.Vec3Centre(), particle.BlockBreak{Block: t})
		if e, ok := SpawnMob(tx, "minecraft:turtle", pos.Vec3Middle(), MobSpawnOptions{Event: "minecraft:entity_born", Persistent: true}); ok {
			if h, ok := Mobs().(turtleHatchHost); ok {
				h.TurtleHatched(tx, e, pos)
			}
		}
	}
	tx.SetBlock(pos, nil, nil)
}

// turtleHatchHost is implemented by the MobHost of the mobs: it gives a turtle hatched from an egg the egg's
// block as its home (HomeComponent::setHome in TurtleEggBlock::tick).
type turtleHatchHost interface {
	TurtleHatched(tx *world.Tx, turtle world.Entity, home cube.Pos)
}

// turtleEggHatchTime reports whether turtle eggs move a step (TurtleEggBlock::tick): late at night, at a time of
// day of 0.65-0.69, or else with a 1 in 500 chance.
func turtleEggHatchTime(tx *world.Tx, r *rand.Rand) bool {
	f := celestialTimeOfDay(tx.World().Time())
	return f < 0.69 && f > 0.65 || r.IntN(500) == 0
}

// celestialTimeOfDay is Dimension::getTimeOfDay of a world time: 0 at noon, 0.25 at sunset, 0.5 at midnight,
// 0.75 at sunrise, smoothed as the game does.
func celestialTimeOfDay(time int) float64 {
	t := time % 24000
	if t < 0 {
		t += 24000
	}
	f := float64(t)/24000 - 0.25
	if f < 0 {
		f++
	}
	f2 := 1 - (math.Cos(f*math.Pi)+1)/2
	return f + (f2-f)/3
}

// EntityInside lets a mob or player standing in the middle of the eggs trample one, with a 1 in 100 chance a
// tick (TurtleEggBlock::entityInside).
func (t TurtleEgg) EntityInside(pos cube.Pos, tx *world.Tx, e world.Entity) {
	g, ok := e.(interface{ OnGround() bool })
	if !ok || !g.OnGround() {
		return
	}
	box := e.H().Type().BBox(e).Translate(e.Position())
	foot := mgl64.Vec3{(box.Min()[0] + box.Max()[0]) / 2, box.Min()[1], (box.Min()[2] + box.Max()[2]) / 2}
	c := pos.Vec3Middle()
	if foot[0] < c[0]-0.3 || foot[0] > c[0]+0.3 || foot[2] < c[2]-0.3 || foot[2] > c[2]+0.3 ||
		foot[1] < c[1]+0.4 || foot[1] > c[1]+1 {
		return
	}
	t.trample(pos, tx, e, 100)
}

// EntityLand lets a mob or player landing on the eggs trample one, with a 1 in 3 chance
// (TurtleEggBlock::transformOnFall). Zombies (zombie, husk, drowned, zombie villager) do not: they stomp on
// eggs on purpose instead.
func (t TurtleEgg) EntityLand(pos cube.Pos, tx *world.Tx, e world.Entity, _ *float64) {
	switch e.H().Type().EncodeEntity() {
	case "minecraft:zombie", "minecraft:husk", "minecraft:drowned", "minecraft:zombie_villager", "minecraft:zombie_villager_v2":
		return
	}
	t.trample(pos, tx, e, 3)
}

// trample breaks one of the eggs under e with a 1 in chance chance (TurtleEggBlock::_destroyEgg). Only mobs and
// players do, and only ones of at least 0.512 cubic blocks that are not turtles; players must be allowed to
// break blocks. (Dragonfly has no mobGriefing game rule: mobs always may.)
func (t TurtleEgg) trample(pos cube.Pos, tx *world.Tx, e world.Entity, chance int) {
	if _, living := e.(livingEntity); !living || e.H().Type().EncodeEntity() == "minecraft:turtle" {
		return
	}
	if gm, ok := e.(interface{ GameMode() world.GameMode }); ok && !gm.GameMode().AllowsEditing() {
		return
	}
	if e.H().Type().BBox(e).Volume() < 0.512 {
		return
	}
	if rand.IntN(chance) != 0 {
		return
	}
	t.decrease(pos, tx, false)
	tx.AddParticle(pos.Vec3Centre(), particle.BlockBreak{Block: t})
}

// decrease takes one egg away (TurtleEggBlock::_decreaseEggs): the last one leaves air. It plays the crack sound
// if silk is set (an egg taken with silk touch), else the break sound.
func (t TurtleEgg) decrease(pos cube.Pos, tx *world.Tx, silk bool) {
	name := "block.turtle_egg.break"
	if silk {
		name = "block.turtle_egg.crack"
	}
	tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: name, Volume: 1, Pitch: 1})
	if t.AdditionalCount == 0 {
		tx.SetBlock(pos, nil, nil)
		return
	}
	t.AdditionalCount--
	tx.SetBlock(pos, t, nil)
	tx.ScheduleBlockUpdate(pos, t, turtleEggTickDelay())
}

// HasLiquidDrops ...
func (TurtleEgg) HasLiquidDrops() bool {
	return false
}

// SideClosed ...
func (TurtleEgg) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// BreakInfo ...
func (t TurtleEgg) BreakInfo() BreakInfo {
	// A player breaks one egg at a time (TurtleEggBlock::playerWillDestroy puts the cluster back with one egg
	// less); only silk touch keeps the egg.
	return newBreakInfo(0.5, alwaysHarvestable, nothingEffective, silkTouchOnlyDrop(TurtleEgg{})).withBreakHandler(func(pos cube.Pos, tx *world.Tx, u item.User) {
		if u == nil {
			return
		}
		held, _ := u.HeldItems()
		t.decrease(pos, tx, hasSilkTouch(held.Enchantments()))
	})
}

// EncodeItem ...
func (TurtleEgg) EncodeItem() (name string, meta int16) {
	return "minecraft:turtle_egg", 0
}

// turtleEggCounts are the turtle_egg_count values of the counts.
var turtleEggCounts = [...]string{"one_egg", "two_egg", "three_egg", "four_egg"}

// crackedStates are the cracked_state values of the crack levels of eggs.
var crackedStates = [...]string{"no_cracks", "cracked", "max_cracked"}

// EncodeBlock ...
func (t TurtleEgg) EncodeBlock() (string, map[string]any) {
	return "minecraft:turtle_egg", map[string]any{
		"turtle_egg_count": turtleEggCounts[t.AdditionalCount],
		"cracked_state":    crackedStates[t.Cracks],
	}
}

// allTurtleEggs returns every state of turtle eggs.
func allTurtleEggs() (b []world.Block) {
	for c := 0; c <= 3; c++ {
		for cr := 0; cr <= 2; cr++ {
			b = append(b, TurtleEgg{AdditionalCount: c, Cracks: cr})
		}
	}
	return
}
