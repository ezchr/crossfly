package block

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

// golemPattern is a block pattern golems are built from (BDS BlockPatternBuilder): rows of cells (BDS aisles), each
// cell a character the pattern defines. A space matches any block.
type golemPattern struct {
	rows  []string
	cells map[byte]func(world.Block) bool
}

// patternMatch is where a golemPattern matched (BDS BuildMatch).
type patternMatch struct {
	ok bool
	// forward and up are the facings the pattern matched with: the rows run along up, the cells of a row along
	// forward x up.
	forward, up cube.Face
	// origin is the position of the first cell of the first row.
	origin cube.Pos
}

// fits reports whether b fits the cell at row and col (BlockPatternBuilder::_fitsBlockPatternEntry).
func (p golemPattern) fits(row, col int, b world.Block) bool {
	c := p.rows[row][col]
	if c == ' ' {
		return true
	}
	f, ok := p.cells[c]
	return ok && f(b)
}

// golemCellPos returns the position of the cell at row and col of a match with the facings and origin passed.
func golemCellPos(origin cube.Pos, forward, up cube.Face, row, col int) cube.Pos {
	f, u := cube.Pos{}.Side(forward), cube.Pos{}.Side(up)
	side := cube.Pos{f[1]*u[2] - f[2]*u[1], f[2]*u[0] - f[0]*u[2], f[0]*u[1] - f[1]*u[0]}
	return cube.Pos{
		origin[0] + u[0]*row + side[0]*col,
		origin[1] + u[1]*row + side[1]*col,
		origin[2] + u[2]*row + side[2]*col,
	}
}

// matchAt reports whether the pattern is in the world with its first cell at origin and the facings passed
// (BlockPatternBuilder::match(BlockPos, int, int, uchar, uchar)).
func (p golemPattern) matchAt(tx *world.Tx, origin cube.Pos, forward, up cube.Face) bool {
	for col := range len(p.rows[0]) {
		for row := range p.rows {
			if !p.fits(row, col, tx.Block(golemCellPos(origin, forward, up, row, col))) {
				return false
			}
		}
	}
	return true
}

// matchFacings tries the 24 orientations with the first cell at origin, in BDS order: the first that matches wins.
func (p golemPattern) matchFacings(tx *world.Tx, origin cube.Pos) patternMatch {
	for forward := cube.FaceDown; forward <= cube.FaceEast; forward++ {
		for up := cube.FaceDown; up <= cube.FaceEast; up++ {
			if up == forward || up == forward.Opposite() {
				continue
			}
			if p.matchAt(tx, origin, forward, up) {
				return patternMatch{ok: true, forward: forward, up: up, origin: origin}
			}
		}
	}
	return patternMatch{}
}

// matchFrom matches the pattern with its first cell at pos, in any orientation (BlockPatternBuilder::match(BlockPos,
// 0, 0)).
func (p golemPattern) matchFrom(tx *world.Tx, pos cube.Pos) patternMatch {
	return p.matchFacings(tx, pos)
}

// find looks for the pattern around pos (BlockPatternBuilder::match(BlockPos)): BDS tries every first cell in the
// box from pos-n to pos, n the larger of the row count and the row length, x first, then y, then z, and each one
// that fits any cell in every orientation.
func (p golemPattern) find(tx *world.Tx, pos cube.Pos) patternMatch {
	n := max(len(p.rows), len(p.rows[0]))
	for x := pos[0] - n; x <= pos[0]; x++ {
		for y := pos[1] - n; y <= pos[1]; y++ {
			for z := pos[2] - n; z <= pos[2]; z++ {
				origin := cube.Pos{x, y, z}
				if !p.fitsAny(tx.Block(origin)) {
					continue
				}
				if m := p.matchFacings(tx, origin); m.ok {
					return m
				}
			}
		}
	}
	return patternMatch{}
}

// fitsAny reports whether b fits any cell of the pattern.
func (p golemPattern) fitsAny(b world.Block) bool {
	for row := range p.rows {
		for col := range len(p.rows[row]) {
			if p.fits(row, col, b) {
				return true
			}
		}
	}
	return false
}

// spawnPos is the position BDS works out for a match (BuildMatch): the middle of the row's width and the last row,
// as if the pattern stood upright.
func (p golemPattern) spawnPos(m patternMatch) mgl64.Vec3 {
	half := float64(len(p.rows[0])-1) * 0.5
	return mgl64.Vec3{float64(m.origin[0]) + half, float64(m.origin[1] - (len(p.rows) - 1)), float64(m.origin[2]) + half}
}

// replace sets the cells of a match whose character is c (every cell for ' ') to the block passed, showing break
// particles of the blocks that were there (BlockPatternBuilder::replaceBlocks with the lambda of
// PumpkinBlock::_canSpawnGolem: LevelEvent ParticlesDestroyBlockNoSound for each block that isn't air).
func (p golemPattern) replace(tx *world.Tx, m patternMatch, c byte, with world.Block) {
	for row, cells := range p.rows {
		for col := range len(cells) {
			if c != ' ' && cells[col] != c {
				continue
			}
			pos := golemCellPos(m.origin, m.forward, m.up, row, col)
			if old := tx.Block(pos); !isGolemAir(old) {
				tx.AddParticle(pos.Vec3Centre(), particle.BlockBreakNoSound{Block: old})
			}
			tx.SetBlock(pos, with, nil)
		}
	}
}

// isGolemAir reports whether b is air.
func isGolemAir(b world.Block) bool {
	_, ok := b.(Air)
	return ok
}

// isGolemHead reports whether b may be the head of a golem: a carved pumpkin or a jack o'lantern. The 26.60
// predicate of PumpkinBlock (0x143b33300) takes plain pumpkins as well, but only in worlds of an old base game
// version.
func isGolemHead(b world.Block) bool {
	switch b := b.(type) {
	case Pumpkin:
		return b.Carved
	case LitPumpkin:
		return true
	}
	return false
}

// isGolemCopper reports whether b is a block a copper golem is built on: one of the eight full copper blocks, waxed
// or not (the 26.60 list at 0x14d55a050).
func isGolemCopper(b world.Block) bool {
	c, ok := b.(Copper)
	return ok && c.Type == NormalCopper()
}

var (
	// snowGolemPattern is the snow golem of PumpkinBlock::_canSpawnGolem: a head on two snow blocks.
	snowGolemPattern = golemPattern{rows: []string{"^", "#", "#"}, cells: map[byte]func(world.Block) bool{
		'^': isGolemHead,
		'#': func(b world.Block) bool { _, ok := b.(Snow); return ok },
	}}
	// ironGolemPattern is the iron golem: a head on a T of four iron blocks, the two cells beside the leg air.
	ironGolemPattern = golemPattern{rows: []string{"~^~", "###", "~#~"}, cells: map[byte]func(world.Block) bool{
		'~': isGolemAir,
		'^': isGolemHead,
		'#': func(b world.Block) bool { _, ok := b.(Iron); return ok },
	}}
	// copperGolemPattern is the copper golem of 26.60: a head on a copper block.
	copperGolemPattern = golemPattern{rows: []string{"^", "#"}, cells: map[byte]func(world.Block) bool{
		'^': isGolemHead,
		'#': isGolemCopper,
	}}
)

// copperGolemEvents are the spawn events of copper golems built on copper of each oxidation level (26.60
// _canSpawnGolem: "minecraft:from_player_{}" with the copper's level, "default" for unoxidised copper).
var copperGolemEvents = [...]string{
	"minecraft:from_player_default", "minecraft:from_player_exposed", "minecraft:from_player_weathered",
	"minecraft:from_player_oxidized",
}

// spawnGolem builds the golem whose pattern the golem head at pos completes (PumpkinBlock::onPlace, which runs
// _canSpawnGolem; 26.60 with the copper golem). The patterns are tried in BDS order: snow golem, iron golem, copper
// golem. The pattern's blocks are removed with break particles once the golem is in the world; without mobs nothing
// happens. It reports whether a golem was built.
func spawnGolem(tx *world.Tx, pos cube.Pos) bool {
	if Mobs() == nil || !isGolemHead(tx.Block(pos)) {
		return false
	}
	if m := snowGolemPattern.matchFrom(tx, pos); m.ok {
		// The snow golem stands on the lowest cell of an upright pattern; BDS raises it by two blocks for any
		// other orientation.
		at := snowGolemPattern.spawnPos(m).Add(mgl64.Vec3{0.5, 0.05, 0.5})
		if m.up != cube.FaceDown {
			at[1] += 2
		}
		if _, ok := SpawnMob(tx, "minecraft:snow_golem", at, MobSpawnOptions{}); ok {
			snowGolemPattern.replace(tx, m, ' ', Air{})
			return true
		}
	}
	if m := ironGolemPattern.find(tx, pos); m.ok {
		// BDS spawns the iron golem at the BuildMatch position without centring it on a block, turned by 90 degrees
		// when the pattern faces north or south.
		at := ironGolemPattern.spawnPos(m)
		if m.up != cube.FaceDown {
			at[1] += 2
		}
		var yaw float64
		if m.forward == cube.FaceNorth || m.forward == cube.FaceSouth {
			yaw = 90
		}
		if _, ok := SpawnMob(tx, "minecraft:iron_golem", at, MobSpawnOptions{Yaw: yaw, Event: "minecraft:from_player"}); ok {
			ironGolemPattern.replace(tx, m, ' ', Air{})
			return true
		}
	}
	if m := copperGolemPattern.find(tx, pos); m.ok {
		head := cube.PosFromVec3(copperGolemPattern.spawnPos(m)).Add(cube.Pos{0, 1, 0})
		copper, _ := tx.Block(head.Add(cube.Pos{}.Side(m.up))).(Copper)
		facing := golemHeadFacing(tx.Block(head))
		ox := copper.Oxidation
		at := mgl64.Vec3{float64(head[0]) + 0.5, float64(head[1]), float64(head[2]) + 0.5}
		if _, ok := SpawnMob(tx, "minecraft:copper_golem", at, MobSpawnOptions{Yaw: golemYaw(facing), Event: copperGolemEvents[ox.Uint8()]}); ok {
			// The copper block turns into a copper chest of its oxidation level (not waxed), facing as the head did.
			chest, ok := golemCopperChest(ox, facing)
			if !ok {
				chest = Air{}
			}
			copperGolemPattern.replace(tx, m, '#', chest)
			copperGolemPattern.replace(tx, m, '^', Air{})
			return true
		}
	}
	return false
}

// golemHeadFacing returns the facing of a golem head.
func golemHeadFacing(b world.Block) cube.Direction {
	switch b := b.(type) {
	case Pumpkin:
		return b.Facing
	case LitPumpkin:
		return b.Facing
	}
	return cube.South
}

// golemYaw returns the yaw of an entity looking in the direction passed, as BDS works it out from the direction's
// vector: atan2(z, x) in degrees, less 90.
func golemYaw(d cube.Direction) float64 {
	v := cube.Pos{}.Side(d.Face())
	return math.Atan2(float64(v[2]), float64(v[0]))*180/math.Pi - 90
}

// golemCopperChest returns the copper chest of the oxidation level passed, facing d, if copper chests are registered. A
// block entity it has is set up through DecodeNBT, as when it is loaded.
func golemCopperChest(o OxidationType, d cube.Direction) (world.Block, bool) {
	b, ok := world.BlockByName(copperBlockName("copper_chest", o, false), map[string]any{"minecraft:cardinal_direction": d.String()})
	if !ok {
		return nil, false
	}
	if _, container := b.(Container); !container {
		// A known block state without a registered block.
		return nil, false
	}
	if n, ok := b.(world.NBTer); ok {
		if fresh, ok := n.DecodeNBT(map[string]any{}).(world.Block); ok {
			b = fresh
		}
	}
	return b, true
}
