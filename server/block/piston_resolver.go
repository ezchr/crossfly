//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"strings"
)

// pistonResolver follows Java's ordered line/branch resolution. Order matters when two slime branches collide.
type pistonResolver struct {
	tx                *world.Tx
	piston, start     cube.Pos
	facing, direction cube.Face
	extending         bool
	push, destroy     []cube.Pos
}

func resolvePiston(tx *world.Tx, pos cube.Pos, facing cube.Face, extending bool) (*pistonResolver, bool) {
	r := &pistonResolver{tx: tx, piston: pos, facing: facing, direction: facing, extending: extending, start: pos.Side(facing)}
	if !extending {
		r.direction = facing.Opposite()
		r.start = r.start.Side(facing)
	}
	b := tx.Block(r.start)
	if !r.pushable(r.start, b, false, facing) {
		if extending && pistonReaction(b) == pistonDestroy {
			r.destroy = append(r.destroy, r.start)
			return r, true
		}
		return r, false
	}
	if !r.line(r.start, r.direction) {
		return r, false
	}
	for i := 0; i < len(r.push); i++ {
		if pistonSticky(tx.Block(r.push[i])) && !r.branch(r.push[i]) {
			return r, false
		}
	}
	return r, true
}

func (r *pistonResolver) pushable(pos cube.Pos, b world.Block, allowDestroy bool, connection cube.Face) bool {
	if pos.OutOfBounds(r.tx.Range()) || pos.Side(r.direction).OutOfBounds(r.tx.Range()) {
		return false
	}
	if _, ok := b.(Air); ok {
		return true
	}
	if p, ok := b.(Piston); ok {
		return !p.Extended
	}
	switch pistonReaction(b) {
	case pistonBlock:
		return false
	case pistonDestroy:
		return allowDestroy
	case pistonPushOnly:
		return r.direction == connection
	}

	name, _ := b.EncodeBlock()
	if javaPistonImmovable[name] {
		return false
	}
	if entity, ok := b.(world.NBTer); ok && javaPistonReactions[name] == pistonNormal && !hasJavaPistonRule(name) && len(entity.EncodeNBT()) > 0 {
		return false
	}
	return true
}

func (r *pistonResolver) index(pos cube.Pos) int {
	for i, p := range r.push {
		if p == pos {
			return i
		}
	}
	return -1
}
func (r *pistonResolver) line(start cube.Pos, connection cube.Face) bool {
	b := r.tx.Block(start)
	if _, air := b.(Air); air {
		return true
	}
	if !r.pushable(start, b, false, connection) || start == r.piston || r.index(start) >= 0 {
		return true
	}
	count := 1
	if count+len(r.push) > 12 {
		return false
	}
	for pistonSticky(b) {
		pos := start
		for j := 0; j < count; j++ {
			pos = pos.Side(r.direction.Opposite())
		}
		previous := b
		b = r.tx.Block(pos)
		if _, air := b.(Air); air {
			break
		}
		if !pistonSticks(previous, b) || !r.pushable(pos, b, false, r.direction.Opposite()) || pos == r.piston {
			break
		}
		count++
		if count+len(r.push) > 12 {
			return false
		}
	}
	added := 0
	for i := count - 1; i >= 0; i-- {
		pos := start
		for j := 0; j < i; j++ {
			pos = pos.Side(r.direction.Opposite())
		}
		r.push = append(r.push, pos)
		added++
	}
	for pos := start.Side(r.direction); ; pos = pos.Side(r.direction) {
		collision := r.index(pos)
		if collision >= 0 {
			old := append([]cube.Pos(nil), r.push...)
			r.push = append(r.push[:0], old[:collision]...)
			r.push = append(r.push, old[len(old)-added:]...)
			r.push = append(r.push, old[collision:len(old)-added]...)
			for j := 0; j <= collision+added; j++ {
				if pistonSticky(r.tx.Block(r.push[j])) && !r.branch(r.push[j]) {
					return false
				}
			}
			return true
		}
		b = r.tx.Block(pos)
		if _, air := b.(Air); air {
			return true
		}
		if !r.pushable(pos, b, true, r.direction) || pos == r.piston {
			return false
		}
		if pistonReaction(b) == pistonDestroy {
			r.destroy = append(r.destroy, pos)
			return true
		}
		if len(r.push) >= 12 {
			return false
		}
		r.push = append(r.push, pos)
		added++
	}
}
func (r *pistonResolver) branch(pos cube.Pos) bool {
	b := r.tx.Block(pos)
	for _, f := range cube.Faces() {
		if f.Axis() != r.direction.Axis() {
			next := pos.Side(f)
			if pistonSticks(b, r.tx.Block(next)) && !r.line(next, f) {
				return false
			}
		}
	}
	return true
}
func pistonSticky(b world.Block) bool {
	n, _ := b.EncodeBlock()
	return n == "minecraft:slime" || n == "minecraft:honey_block"
}
func pistonSticks(a, b world.Block) bool {
	an, _ := a.EncodeBlock()
	bn, _ := b.EncodeBlock()
	if an == "minecraft:slime" && bn == "minecraft:honey_block" || an == "minecraft:honey_block" && bn == "minecraft:slime" {
		return false
	}
	return pistonSticky(a) || pistonSticky(b)
}

const (
	pistonNormal = iota
	pistonBlock
	pistonDestroy
	pistonPushOnly
)

// pistonReaction describes Java's push reaction. NBT and extended-piston checks are handled separately.
func pistonReaction(b world.Block) int {
	n, _ := b.EncodeBlock()
	switch n {
	case "minecraft:obsidian", "minecraft:crying_obsidian", "minecraft:respawn_anchor", "minecraft:reinforced_deepslate",
		"minecraft:bedrock", "minecraft:barrier", "minecraft:light_block", "minecraft:end_portal", "minecraft:end_portal_frame",
		"minecraft:nether_portal", "minecraft:portal", "minecraft:structure_block", "minecraft:structure_void", "minecraft:jigsaw",
		"minecraft:command_block", "minecraft:repeating_command_block", "minecraft:chain_command_block",
		"minecraft:enchanting_table", "minecraft:ender_chest", "minecraft:spawner", "minecraft:mob_spawner",
		"minecraft:moving_block", "minecraft:piston_arm_collision", "minecraft:sticky_piston_arm_collision":
		return pistonBlock
	}
	if reaction, ok := javaPistonReactions[n]; ok {
		return reaction
	}
	if strings.HasSuffix(n, "glazed_terracotta") {
		return pistonPushOnly
	}
	if _, ok := b.(world.Liquid); ok {
		return pistonDestroy
	}
	switch b.(type) {
	case RedstoneWire, RedstoneTorch, RedstoneRepeater, RedstoneComparator, Lever, Button, PressurePlate:
		return pistonDestroy
	}
	for _, suffix := range []string{"_flower", "_sapling", "_tulip", "_rail", "_torch", "_button", "_pressure_plate", "_carpet", "_coral", "_coral_fan", "_wall_sign", "_banner", "_door", "_bed"} {
		if strings.HasSuffix(n, suffix) {
			return pistonDestroy
		}
	}
	switch n {
	case "minecraft:short_grass", "minecraft:tall_grass", "minecraft:fern", "minecraft:large_fern", "minecraft:deadbush",
		"minecraft:red_flower", "minecraft:yellow_flower", "minecraft:flower_pot", "minecraft:snow_layer", "minecraft:vine",
		"minecraft:ladder", "minecraft:rail", "minecraft:golden_rail", "minecraft:detector_rail", "minecraft:activator_rail",
		"minecraft:fire", "minecraft:soul_fire", "minecraft:wheat", "minecraft:carrots", "minecraft:potatoes", "minecraft:beetroot",
		"minecraft:pumpkin_stem", "minecraft:melon_stem", "minecraft:reeds", "minecraft:cactus", "minecraft:bamboo",
		"minecraft:brown_mushroom", "minecraft:red_mushroom", "minecraft:cocoa", "minecraft:trip_wire", "minecraft:tripwire_hook":
		return pistonDestroy
	}
	if breakable, ok := b.(Breakable); ok && breakable.BreakInfo().Hardness < 0 {
		return pistonBlock
	}
	return pistonNormal
}

func hasJavaPistonRule(name string) bool { _, ok := javaPistonReactions[name]; return ok }
