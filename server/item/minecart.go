//go:build crossfly_full

package item

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// MinecartKind is the kind of a minecart: what it carries.
type MinecartKind uint8

// The kinds of minecarts.
const (
	MinecartPlain MinecartKind = iota
	MinecartChest
	MinecartHopper
	MinecartTNT
)

// MinecartKinds returns all kinds of minecarts.
func MinecartKinds() []MinecartKind {
	return []MinecartKind{MinecartPlain, MinecartChest, MinecartHopper, MinecartTNT}
}

// Minecart is a minecart item, placed on rails.
type Minecart struct {
	// Kind is what the minecart carries.
	Kind MinecartKind
}

// EntityName returns the id of the minecart, both as an item and as an entity.
func (m Minecart) EntityName() string {
	switch m.Kind {
	case MinecartChest:
		return "minecraft:chest_minecart"
	case MinecartHopper:
		return "minecraft:hopper_minecart"
	case MinecartTNT:
		return "minecraft:tnt_minecart"
	}
	return "minecraft:minecart"
}

// UseOnBlock places the minecart on the rail clicked, as Java Edition's MinecartItem.
func (m Minecart) UseOnBlock(pos cube.Pos, _ cube.Face, _ mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	slope, ok := minecartRail(tx.Block(pos))
	if !ok {
		return false
	}
	y := float64(pos[1]) + 0.0625
	if slope {
		y += 0.5
	}
	var name string
	if held, _ := user.HeldItems(); held.Item() == m {
		name = held.CustomName()
	}
	spawn := tx.World().EntityRegistry().Config().Minecart
	if spawn == nil {
		return false
	}
	opts := world.EntitySpawnOpts{Position: mgl64.Vec3{float64(pos[0]) + 0.5, y, float64(pos[2]) + 0.5}, NameTag: name}
	tx.AddEntity(spawn(opts, int(m.Kind)))
	ctx.SubtractFromCount(1)
	return true
}

// minecartRail reports whether b is a rail (Java Edition's #rails), and whether it is sloped.
func minecartRail(b world.Block) (slope, ok bool) {
	name, props := b.EncodeBlock()
	switch name {
	case "minecraft:rail", "minecraft:golden_rail", "minecraft:detector_rail", "minecraft:activator_rail":
		d, _ := props["rail_direction"].(int32)
		return d >= 2 && d <= 5, true
	}
	return false, false
}

// MaxCount ...
func (Minecart) MaxCount() int {
	return 1
}

// EncodeItem ...
func (m Minecart) EncodeItem() (name string, meta int16) {
	return m.EntityName(), 0
}
