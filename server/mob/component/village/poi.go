//go:build crossfly_full

package village

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// blueprint is a BDS POIBlueprint (VillageManager::_loadPOIBlueprints).
type blueprint struct {
	name      string
	typ       POIType
	radius    float64
	capacity  int
	weight    int
	initEvent string
	sound     string
}

func (b blueprint) instance(pos cube.Pos) *POI {
	p := &POI{Pos: pos, Type: b.typ, Name: b.name, Radius: b.radius, Capacity: b.capacity, Weight: b.weight, InitEvent: b.initEvent, Sound: b.sound}
	if b.typ == POIJob {
		p.EndEvent = "minecraft:become_unskilled"
	}
	return p
}

func job(name, event, sound string, weight int) blueprint {
	return blueprint{name: name, typ: POIJob, radius: 2, capacity: 1, weight: weight, initEvent: event, sound: sound}
}

// blueprints by block identifier.
var blueprints = map[string]blueprint{
	"minecraft:bed":               {name: "villager", typ: POIBed, radius: 0.75, capacity: 1, weight: 1},
	"minecraft:bell":              {name: "villager", typ: POIMeeting, radius: 6, capacity: 20, weight: 1},
	"minecraft:composter":         job("farmer", "minecraft:become_farmer", "block.composter.fill", 1),
	"minecraft:lectern":           job("librarian", "minecraft:become_librarian", "item.book.put", 8),
	"minecraft:grindstone":        job("weaponsmith", "minecraft:become_weaponsmith", "block.grindstone.use", 5),
	"minecraft:blast_furnace":     job("armorer", "minecraft:become_armorer", "block.blastfurnace.fire_crackle", 5),
	"minecraft:lit_blast_furnace": job("armorer", "minecraft:become_armorer", "block.blastfurnace.fire_crackle", 5),
	"minecraft:smoker":            job("butcher", "minecraft:become_butcher", "block.smoker.smoke", 5),
	"minecraft:lit_smoker":        job("butcher", "minecraft:become_butcher", "block.smoker.smoke", 5),
	"minecraft:fletching_table":   job("fletcher", "minecraft:become_fletcher", "block.fletching_table.use", 5),
	"minecraft:cartography_table": job("cartographer", "minecraft:become_cartographer", "block.cartography_table.use", 6),
	"minecraft:brewing_stand":     job("cleric", "minecraft:become_cleric", "potion.brewed", 8),
	"minecraft:smithing_table":    job("toolsmith", "minecraft:become_toolsmith", "block.smithing_table.use", 5),
	"minecraft:cauldron":          job("leatherworker", "minecraft:become_leatherworker", "bucket.fill.water", 5),
	"minecraft:barrel":            job("fisherman", "minecraft:become_fisherman", "block.barrel.open", 3),
	"minecraft:loom":              job("shepherd", "minecraft:become_sheperd", "block.loom.use", 3), // sic, as BDS and the JSON
	"minecraft:stonecutter_block": job("mason", "minecraft:become_mason", "block.stonecutter.use", 3),
	"minecraft:stonecutter":       job("mason", "minecraft:become_mason", "block.stonecutter.use", 3),
}

// blueprintOf is Village::isVillagePOI + the blueprint lookup: beds count
// only by their unoccupied head piece.
func blueprintOf(b world.Block) (blueprint, bool) {
	name, props := b.EncodeBlock()
	bp, ok := blueprints[name]
	if !ok {
		return bp, false
	}
	if bp.typ == POIBed {
		if bed, ok := b.(block.Bed); ok {
			return bp, bed.Head && !bed.Occupied
		}
		head, _ := props["head_piece_bit"].(uint8)
		occ, _ := props["occupied_bit"].(uint8)
		return bp, head == 1 && occ == 0
	}
	return bp, true
}

// IsPOIBlock reports whether b is a village POI block, and its type.
func IsPOIBlock(b world.Block) (POIType, string, bool) {
	bp, ok := blueprintOf(b)
	return bp.typ, bp.name, ok
}

// dead reports whether e is dead or gone.
func dead(e world.Entity) bool {
	if e == nil || e.H().Closed() {
		return true
	}
	if l, ok := e.(interface{ Dead() bool }); ok {
		return l.Dead()
	}
	return false
}

// solid reports whether a mob could stand on the block at pos.
func solid(tx *world.Tx, pos cube.Pos) bool {
	if pos[1] < tx.Range()[0] || pos[1] > tx.Range()[1] {
		return false
	}
	b, ok := tx.BlockLoaded(pos)
	if !ok {
		return false
	}
	if _, ok := b.(block.Air); ok {
		return false
	}
	if _, ok := b.(world.Liquid); ok {
		return false
	}
	return len(b.Model().BBox(pos, tx)) > 0
}

// Solid is solid for the goal package.
func Solid(tx *world.Tx, pos cube.Pos) bool { return solid(tx, pos) }

// spawningAllowed is the spawn gate of Village::tick (mob spawning on, not
// peaceful).
func spawningAllowed(tx *world.Tx) bool {
	return tx.World().Difficulty() != world.DifficultyPeaceful
}

// TopSolid is BlockSource::getAboveTopSolidBlock: the first free block above
// the highest solid block of the column, searching down from y+16.
func TopSolid(tx *world.Tx, x, y, z int) int {
	top := min(y+16, tx.Range()[1])
	for yy := top; yy > tx.Range()[0]; yy-- {
		if solid(tx, cube.Pos{x, yy - 1, z}) && !solid(tx, cube.Pos{x, yy, z}) {
			return yy
		}
	}
	return y
}
