//go:build crossfly_full

package item

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// BoatWood is the wood a boat is made of: Bedrock Edition's boat Variant (Boat::getWoodID).
type BoatWood uint8

// The woods of boats, in the order of their Bedrock Variant.
const (
	BoatOak BoatWood = iota
	BoatSpruce
	BoatBirch
	BoatJungle
	BoatAcacia
	BoatDarkOak
	BoatMangrove
	BoatBamboo
	BoatCherry
	BoatPaleOak
)

// BoatWoods returns every wood a boat is made of.
func BoatWoods() []BoatWood {
	return []BoatWood{BoatOak, BoatSpruce, BoatBirch, BoatJungle, BoatAcacia, BoatDarkOak, BoatMangrove, BoatBamboo,
		BoatCherry, BoatPaleOak}
}

var boatWoodNames = [...]string{"oak", "spruce", "birch", "jungle", "acacia", "dark_oak", "mangrove", "bamboo", "cherry",
	"pale_oak"}

// String returns the name of the wood ("dark_oak").
func (w BoatWood) String() string {
	if int(w) < len(boatWoodNames) {
		return boatWoodNames[w]
	}
	return "oak"
}

// Boat is a boat or chest boat item (a raft for bamboo), placed on water or on top of a block.
type Boat struct {
	// Wood is what the boat is made of.
	Wood BoatWood
	// Chest is true for a chest boat.
	Chest bool
}

// EncodeItem ...
func (b Boat) EncodeItem() (name string, meta int16) {
	if b.Wood == BoatBamboo {
		if b.Chest {
			return "minecraft:bamboo_chest_raft", 0
		}
		return "minecraft:bamboo_raft", 0
	}
	if b.Chest {
		return "minecraft:" + b.Wood.String() + "_chest_boat", 0
	}
	return "minecraft:" + b.Wood.String() + "_boat", 0
}

// MaxCount is 1, as BoatItem's setMaxStackSize(1).
func (Boat) MaxCount() int { return 1 }

// FuelInfo: a boat burns for 60 seconds (1200 ticks) in a furnace, as in both editions.
func (Boat) FuelInfo() FuelInfo { return newFuelInfo(60_000_000_000) }

// UseOnBlock places the boat where the top face of a block was clicked, if the block above it is air, as
// BoatItem::_useOn: the boat goes to the clicked point, facing the user's yaw plus 90 degrees.
func (b Boat) UseOnBlock(pos cube.Pos, face cube.Face, clickPos mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	if face != cube.FaceUp {
		return false
	}
	if above := pos.Side(cube.FaceUp); above.OutOfBounds(tx.Range()) || !isAir(tx.Block(above)) {
		return false
	}
	at := mgl64.Vec3{float64(pos[0]) + clickPos[0], float64(pos[1]) + clickPos[1], float64(pos[2]) + clickPos[2]}
	return b.place(tx, user, ctx, at)
}

// Use places the boat on the water the user looks at, for a click in the air: Bedrock clients send a click on water
// as a click in the air. Java Edition fallback (BoatItem.use rays to the first fluid); the 1.21.3 dump has only
// BoatItem::_useOn, which needs a block.
func (b Boat) Use(tx *world.Tx, user User, ctx *UseContext) bool {
	eye := eyePosition(user)
	yaw, pitch := user.Rotation().Yaw()*math.Pi/180, user.Rotation().Pitch()*math.Pi/180
	dir := mgl64.Vec3{-math.Sin(yaw) * math.Cos(pitch), -math.Sin(pitch), math.Cos(yaw) * math.Cos(pitch)}
	const step = 0.05
	for d := 0.0; d <= 5; d += step {
		p := eye.Add(dir.Mul(d))
		bp := cube.PosFromVec3(p)
		if bp.OutOfBounds(tx.Range()) {
			return false
		}
		if l, ok := tx.Liquid(bp); ok && isWaterName(l) {
			top := float64(bp[1]) + 1
			if above, ok := tx.Liquid(bp.Side(cube.FaceUp)); !ok || !isWaterName(above) {
				top = float64(bp[1]) + float64(l.LiquidDepth())/9
			}
			return b.place(tx, user, ctx, mgl64.Vec3{p[0], top, p[2]})
		}
		for _, bb := range tx.Block(bp).Model().BBox(bp, tx) {
			if bb = bb.Translate(bp.Vec3()); bb.Vec3Within(p) {
				// A click in the air never reaches a block's top here unless the ray came down onto it: the client
				// sends block clicks itself (UseOnBlock).
				return false
			}
		}
	}
	return false
}

// place spawns the boat at pos.
func (b Boat) place(tx *world.Tx, user User, ctx *UseContext, pos mgl64.Vec3) bool {
	spawn := tx.World().EntityRegistry().Config().Boat
	if spawn == nil {
		return false
	}
	// BoatItem::_useOn (1.20.50 and later): no boat where it would overlap another entity. Dropped items and
	// experience orbs do not count.
	half := 0.7
	box := cube.Box(pos[0]-half, pos[1], pos[2]-half, pos[0]+half, pos[1]+0.455, pos[2]+half)
	for e := range tx.EntitiesWithin(box.Grow(2)) {
		if e.H() == user.H() {
			continue
		}
		switch e.H().Type().EncodeEntity() {
		case "minecraft:item", "minecraft:xp_orb":
			continue
		}
		if e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
			return false
		}
	}
	var name string
	if held, _ := user.HeldItems(); held.Item() == b {
		name = held.CustomName()
	}
	opts := world.EntitySpawnOpts{Position: pos, Rotation: cube.Rotation{user.Rotation().Yaw() + 90, 0}, NameTag: name}
	tx.AddEntity(spawn(opts, int(b.Wood), b.Chest))
	ctx.SubtractFromCount(1)
	return true
}

// isAir reports whether bl is air.
func isAir(bl world.Block) bool {
	name, _ := bl.EncodeBlock()
	return name == "minecraft:air"
}

// isWaterName reports whether l is water.
func isWaterName(l world.Liquid) bool {
	name, _ := l.EncodeBlock()
	return name == "minecraft:water" || name == "minecraft:flowing_water"
}
