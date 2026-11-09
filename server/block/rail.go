//go:build crossfly_full

package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// RailShape is the way a rail lies. The values are both editions' order: Bedrock's rail_direction and Java Edition's
// shape property.
type RailShape uint8

const (
	RailNorthSouth RailShape = iota
	RailEastWest
	RailAscendingEast
	RailAscendingWest
	RailAscendingNorth
	RailAscendingSouth
	RailSouthEast
	RailSouthWest
	RailNorthWest
	RailNorthEast
)

// Slope reports whether the shape goes up a block.
func (s RailShape) Slope() bool { return s >= RailAscendingEast && s <= RailAscendingSouth }

// railBlock is a rail of any kind.
type railBlock interface {
	world.Block
	railShape() RailShape
	withRailShape(s RailShape) world.Block
	// straight rails (powered, activator, detector) never curve.
	straight() bool
}

// railAt returns the rail at pos, if any.
func railAt(tx *world.Tx, pos cube.Pos) (railBlock, bool) {
	r, ok := tx.Block(pos).(railBlock)
	return r, ok
}

// railSupported reports whether a rail can lie at pos: on a sturdy top face.
func railSupported(tx *world.Tx, pos cube.Pos) bool {
	below := pos.Side(cube.FaceDown)
	if below.OutOfBounds(tx.Range()) {
		return false
	}
	return tx.Block(below).Model().FaceSolid(below, cube.FaceUp, tx)
}

// railSideSupported reports whether the block at pos has a sturdy top face, as Java Edition requires on the high side of
// a sloped rail.
func railSideSupported(tx *world.Tx, pos cube.Pos) bool {
	return tx.Block(pos).Model().FaceSolid(pos, cube.FaceUp, tx)
}

// railShouldBreak is Java Edition's BaseRailBlock.shouldBeRemoved.
func railShouldBreak(tx *world.Tx, pos cube.Pos, s RailShape) bool {
	if !railSupported(tx, pos) {
		return true
	}
	switch s {
	case RailAscendingEast:
		return !railSideSupported(tx, pos.Side(cube.FaceEast))
	case RailAscendingWest:
		return !railSideSupported(tx, pos.Side(cube.FaceWest))
	case RailAscendingNorth:
		return !railSideSupported(tx, pos.Side(cube.FaceNorth))
	case RailAscendingSouth:
		return !railSideSupported(tx, pos.Side(cube.FaceSouth))
	}
	return false
}

// railNeighbourSignal is Java Edition's hasNeighborSignal: whether anything around pos powers it.
func railNeighbourSignal(tx *world.Tx, pos cube.Pos) bool {
	for _, f := range cube.Faces() {
		if pistonSignal(pos.Side(f), f.Opposite(), tx) > 0 {
			return true
		}
	}
	return false
}

// railPlace places a rail, as Java Edition's getStateForPlacement and onPlace do.
func railPlace(r railBlock, pos cube.Pos, face cube.Face, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, r)
	if !used || !railSupported(tx, pos) {
		return false
	}
	shape := RailNorthSouth
	if d := user.Rotation().Direction(); d == cube.East || d == cube.West {
		shape = RailEastWest
	}
	place(tx, pos, r.withRailShape(shape), user, ctx)
	return placed(ctx)
}

// railPlaced is Java Edition's onPlace and updateState: the rail finds its shape, and a straight rail then checks its
// power.
func railPlaced(r railBlock, pos cube.Pos, previous world.Block, tx *world.Tx) {
	if prev, ok := previous.(railBlock); ok && sameRailKind(prev, r) {
		return
	}
	r = railUpdateDir(r, pos, tx, true)
	if r.straight() {
		railNeighbourChanged(r, pos, pos, tx)
	}
}

// railNeighbourChanged is Java Edition's BaseRailBlock.neighborChanged.
func railNeighbourChanged(r railBlock, pos, changed cube.Pos, tx *world.Tx) {
	if cur, ok := railAt(tx, pos); !ok || !sameRailKind(cur, r) {
		return
	}
	if railShouldBreak(tx, pos, r.railShape()) {
		breakBlock(r, pos, tx)
		return
	}
	switch b := r.(type) {
	case Rail:
		// A plain rail at a junction turns when a power source next to it changes.
		if _, source := tx.Block(changed).(world.RedstonePowerSource); source && changed != pos && newRailState(tx, pos, b).potentialConnections() == 3 {
			railUpdateDir(b, pos, tx, false)
		}
	case poweredRailBlock:
		poweredRailUpdate(b, pos, tx)
	}
}

// railRemoved is Java Edition's affectNeighborsAfterRemoval for rails.
func railRemoved(r railBlock, pos cube.Pos, tx *world.Tx) {
	if r.railShape().Slope() {
		railNotifyAround(pos.Side(cube.FaceUp), tx)
	}
	if r.straight() {
		railNotifyAround(pos.Side(cube.FaceDown), tx)
	}
}

// railNotifyAround gives the blocks around pos a neighbour update from pos, as Java Edition's updateNeighborsAt.
func railNotifyAround(pos cube.Pos, tx *world.Tx) {
	for _, f := range cube.Faces() {
		tx.NotifyNeighbour(pos.Side(f), pos)
	}
}

// railUpdateDir is Java Edition's updateDir.
func railUpdateDir(r railBlock, pos cube.Pos, tx *world.Tx, first bool) railBlock {
	return newRailState(tx, pos, r).place(railNeighbourSignal(tx, pos), first, r.railShape()).rail
}

// sameRailKind reports whether a and b are the same kind of rail, whatever their state.
func sameRailKind(a, b railBlock) bool {
	ah, _ := a.Hash()
	bh, _ := b.Hash()
	return ah == bh
}

// railState is Java Edition's RailState: a rail and the positions it connects to.
type railState struct {
	tx          *world.Tx
	pos         cube.Pos
	rail        railBlock
	connections []cube.Pos
}

func newRailState(tx *world.Tx, pos cube.Pos, r railBlock) *railState {
	s := &railState{tx: tx, pos: pos, rail: r}
	s.updateConnections(r.railShape())
	return s
}

func (s *railState) updateConnections(shape RailShape) {
	p := s.pos
	n, so, w, e := p.Side(cube.FaceNorth), p.Side(cube.FaceSouth), p.Side(cube.FaceWest), p.Side(cube.FaceEast)
	up := func(q cube.Pos) cube.Pos { return q.Side(cube.FaceUp) }
	switch shape {
	case RailNorthSouth:
		s.connections = []cube.Pos{n, so}
	case RailEastWest:
		s.connections = []cube.Pos{w, e}
	case RailAscendingEast:
		s.connections = []cube.Pos{w, up(e)}
	case RailAscendingWest:
		s.connections = []cube.Pos{up(w), e}
	case RailAscendingNorth:
		s.connections = []cube.Pos{up(n), so}
	case RailAscendingSouth:
		s.connections = []cube.Pos{n, up(so)}
	case RailSouthEast:
		s.connections = []cube.Pos{e, so}
	case RailSouthWest:
		s.connections = []cube.Pos{w, so}
	case RailNorthWest:
		s.connections = []cube.Pos{w, n}
	case RailNorthEast:
		s.connections = []cube.Pos{e, n}
	}
}

func (s *railState) removeSoftConnections() {
	kept := s.connections[:0]
	for _, c := range s.connections {
		if r := s.getRail(c); r != nil && r.connectsTo(s) {
			kept = append(kept, r.pos)
		}
	}
	s.connections = kept
}

func (s *railState) isRail(pos cube.Pos) bool {
	_, ok := railAt(s.tx, pos)
	return ok
}

func (s *railState) hasRail(pos cube.Pos) bool {
	return s.isRail(pos) || s.isRail(pos.Side(cube.FaceUp)) || s.isRail(pos.Side(cube.FaceDown))
}

func (s *railState) getRail(pos cube.Pos) *railState {
	for _, p := range []cube.Pos{pos, pos.Side(cube.FaceUp), pos.Side(cube.FaceDown)} {
		if r, ok := railAt(s.tx, p); ok {
			return newRailState(s.tx, p, r)
		}
	}
	return nil
}

func (s *railState) connectsTo(o *railState) bool { return s.hasConnection(o.pos) }

func (s *railState) hasConnection(pos cube.Pos) bool {
	for _, c := range s.connections {
		if c.X() == pos.X() && c.Z() == pos.Z() {
			return true
		}
	}
	return false
}

func (s *railState) potentialConnections() int {
	n := 0
	for _, f := range []cube.Face{cube.FaceNorth, cube.FaceEast, cube.FaceSouth, cube.FaceWest} {
		if s.hasRail(s.pos.Side(f)) {
			n++
		}
	}
	return n
}

func (s *railState) canConnectTo(o *railState) bool {
	return s.connectsTo(o) || len(s.connections) != 2
}

// ascend turns a flat straight shape into a slope towards a rail one block up.
func (s *railState) ascend(shape RailShape, n, so, w, e cube.Pos) RailShape {
	switch shape {
	case RailNorthSouth:
		if s.isRail(n.Side(cube.FaceUp)) {
			shape = RailAscendingNorth
		}
		if s.isRail(so.Side(cube.FaceUp)) {
			shape = RailAscendingSouth
		}
	case RailEastWest:
		if s.isRail(e.Side(cube.FaceUp)) {
			shape = RailAscendingEast
		}
		if s.isRail(w.Side(cube.FaceUp)) {
			shape = RailAscendingWest
		}
	}
	return shape
}

func (s *railState) connectTo(o *railState) {
	s.connections = append(s.connections, o.pos)
	p := s.pos
	north, south, west, east := p.Side(cube.FaceNorth), p.Side(cube.FaceSouth), p.Side(cube.FaceWest), p.Side(cube.FaceEast)
	n, so, w, e := s.hasConnection(north), s.hasConnection(south), s.hasConnection(west), s.hasConnection(east)
	shape, set := RailNorthSouth, false
	if n || so {
		shape, set = RailNorthSouth, true
	}
	if w || e {
		shape, set = RailEastWest, true
	}
	if !s.rail.straight() {
		switch {
		case so && e && !n && !w:
			shape, set = RailSouthEast, true
		case so && w && !n && !e:
			shape, set = RailSouthWest, true
		case n && w && !so && !e:
			shape, set = RailNorthWest, true
		case n && e && !so && !w:
			shape, set = RailNorthEast, true
		}
	}
	if set {
		shape = s.ascend(shape, north, south, west, east)
	}
	s.rail = s.rail.withRailShape(shape).(railBlock)
	s.tx.SetBlock(s.pos, s.rail, nil)
}

func (s *railState) hasNeighbourRail(pos cube.Pos) bool {
	o := s.getRail(pos)
	if o == nil {
		return false
	}
	o.removeSoftConnections()
	return o.canConnectTo(s)
}

// place is Java Edition's RailState.place.
func (s *railState) place(signal, first bool, def RailShape) *railState {
	p := s.pos
	north, south, west, east := p.Side(cube.FaceNorth), p.Side(cube.FaceSouth), p.Side(cube.FaceWest), p.Side(cube.FaceEast)
	n, so, w, e := s.hasNeighbourRail(north), s.hasNeighbourRail(south), s.hasNeighbourRail(west), s.hasNeighbourRail(east)
	var shape RailShape
	set := false
	ns, we := n || so, w || e
	if ns && !we {
		shape, set = RailNorthSouth, true
	}
	if we && !ns {
		shape, set = RailEastWest, true
	}
	se, sw, ne, nw := so && e, so && w, n && e, n && w
	if !s.rail.straight() {
		if se && !n && !w {
			shape, set = RailSouthEast, true
		}
		if sw && !n && !e {
			shape, set = RailSouthWest, true
		}
		if nw && !so && !e {
			shape, set = RailNorthWest, true
		}
		if ne && !so && !w {
			shape, set = RailNorthEast, true
		}
	}
	if !set {
		switch {
		case ns && we:
			shape, set = def, true
		case ns:
			shape, set = RailNorthSouth, true
		case we:
			shape, set = RailEastWest, true
		}
		if !s.rail.straight() {
			if signal {
				if se {
					shape, set = RailSouthEast, true
				}
				if sw {
					shape, set = RailSouthWest, true
				}
				if ne {
					shape, set = RailNorthEast, true
				}
				if nw {
					shape, set = RailNorthWest, true
				}
			} else {
				if nw {
					shape, set = RailNorthWest, true
				}
				if ne {
					shape, set = RailNorthEast, true
				}
				if sw {
					shape, set = RailSouthWest, true
				}
				if se {
					shape, set = RailSouthEast, true
				}
			}
		}
	}
	if set {
		shape = s.ascend(shape, north, south, west, east)
	} else {
		shape = def
	}
	s.updateConnections(shape)
	s.rail = s.rail.withRailShape(shape).(railBlock)
	if cur, ok := railAt(s.tx, s.pos); first || !ok || cur != s.rail {
		s.tx.SetBlock(s.pos, s.rail, nil)
		for _, c := range s.connections {
			if o := s.getRail(c); o != nil {
				o.removeSoftConnections()
				if o.canConnectTo(s) {
					o.connectTo(s)
				}
			}
		}
	}
	return s
}

// railBase holds what every rail shares.
type railBase struct {
	empty
	transparent
}

// SideClosed ...
func (railBase) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool { return false }

// Rail is the plain rail: it curves, and at a junction it turns when a power source next to it changes.
type Rail struct {
	railBase
	// Shape is the way the rail lies.
	Shape RailShape
}

func (r Rail) railShape() RailShape                  { return r.Shape }
func (r Rail) withRailShape(s RailShape) world.Block { r.Shape = s; return r }
func (Rail) straight() bool                          { return false }

// UseOnBlock ...
func (r Rail) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	return railPlace(r, pos, face, tx, user, ctx)
}

// BlockPlaced ...
func (r Rail) BlockPlaced(pos cube.Pos, previous world.Block, tx *world.Tx) {
	railPlaced(r, pos, previous, tx)
}

// NeighbourUpdateTick ...
func (r Rail) NeighbourUpdateTick(pos, changed cube.Pos, tx *world.Tx) {
	railNeighbourChanged(r, pos, changed, tx)
}

// BlockRemoved ...
func (r Rail) BlockRemoved(pos cube.Pos, replacement world.Block, tx *world.Tx) {
	if rep, ok := replacement.(railBlock); !ok || !sameRailKind(rep, r) {
		railRemoved(r, pos, tx)
	}
}

// BreakInfo ...
func (r Rail) BreakInfo() BreakInfo {
	return newBreakInfo(0.7, alwaysHarvestable, pickaxeEffective, oneOf(Rail{}))
}

// EncodeItem ...
func (Rail) EncodeItem() (string, int16) { return "minecraft:rail", 0 }

// EncodeBlock ...
func (r Rail) EncodeBlock() (string, map[string]any) {
	return "minecraft:rail", map[string]any{"rail_direction": int32(r.Shape)}
}

// poweredRailBlock is a powered or an activator rail: straight rails that pass power along up to 8 rails.
type poweredRailBlock interface {
	railBlock
	railPowered() bool
	withRailPowered(bool) world.Block
}

// poweredRailUpdate is Java Edition's PoweredRailBlock.updateState.
func poweredRailUpdate(r poweredRailBlock, pos cube.Pos, tx *world.Tx) {
	should := railNeighbourSignal(tx, pos) || poweredRailFind(r, pos, r.railShape(), true, 0, tx) ||
		poweredRailFind(r, pos, r.railShape(), false, 0, tx)
	if should == r.railPowered() {
		return
	}
	tx.SetBlock(pos, r.withRailPowered(should), nil)
	railNotifyAround(pos.Side(cube.FaceDown), tx)
	if r.railShape().Slope() {
		railNotifyAround(pos.Side(cube.FaceUp), tx)
	}
}

// poweredRailFind is Java Edition's findPoweredRailSignal: whether a powered rail of the same kind further along, in a
// line of at most 8, is powered from something other than rails.
func poweredRailFind(kind poweredRailBlock, pos cube.Pos, shape RailShape, forward bool, depth int, tx *world.Tx) bool {
	if depth >= 8 {
		return false
	}
	x, y, z := pos.X(), pos.Y(), pos.Z()
	below := true
	switch shape {
	case RailNorthSouth:
		if forward {
			z++
		} else {
			z--
		}
	case RailEastWest:
		if forward {
			x--
		} else {
			x++
		}
	case RailAscendingEast:
		if forward {
			x--
		} else {
			x, y, below = x+1, y+1, false
		}
		shape = RailEastWest
	case RailAscendingWest:
		if forward {
			x, y, below = x-1, y+1, false
		} else {
			x++
		}
		shape = RailEastWest
	case RailAscendingNorth:
		if forward {
			z++
		} else {
			z, y, below = z-1, y+1, false
		}
		shape = RailNorthSouth
	case RailAscendingSouth:
		if forward {
			z, y, below = z+1, y+1, false
		} else {
			z--
		}
		shape = RailNorthSouth
	}
	if poweredRailSameWithPower(kind, cube.Pos{x, y, z}, forward, depth, shape, tx) {
		return true
	}
	return below && poweredRailSameWithPower(kind, cube.Pos{x, y - 1, z}, forward, depth, shape, tx)
}

// poweredRailSameWithPower is Java Edition's isSameRailWithPower.
func poweredRailSameWithPower(kind poweredRailBlock, pos cube.Pos, forward bool, depth int, dir RailShape, tx *world.Tx) bool {
	b, ok := tx.Block(pos).(poweredRailBlock)
	if !ok || !sameRailKind(b, kind) {
		return false
	}
	my := b.railShape()
	if dir == RailEastWest && (my == RailNorthSouth || my == RailAscendingNorth || my == RailAscendingSouth) {
		return false
	}
	if dir == RailNorthSouth && (my == RailEastWest || my == RailAscendingEast || my == RailAscendingWest) {
		return false
	}
	if !b.railPowered() {
		return false
	}
	if railNeighbourSignal(tx, pos) {
		return true
	}
	return poweredRailFind(kind, pos, my, forward, depth+1, tx)
}

// PoweredRail speeds minecarts up while powered (golden rail). Power passes along up to 8 powered rails.
type PoweredRail struct {
	railBase
	// Shape is the way the rail lies. Powered rails never curve.
	Shape RailShape
	// Powered is true while the rail is powered.
	Powered bool
}

// ActivatorRail activates minecarts that pass over it while powered. Power passes along up to 8 activator rails.
type ActivatorRail struct {
	railBase
	// Shape is the way the rail lies. Activator rails never curve.
	Shape RailShape
	// Powered is true while the rail is powered.
	Powered bool
}

func (r PoweredRail) railShape() RailShape                    { return r.Shape }
func (r PoweredRail) withRailShape(s RailShape) world.Block   { r.Shape = s; return r }
func (PoweredRail) straight() bool                            { return true }
func (r PoweredRail) railPowered() bool                       { return r.Powered }
func (r PoweredRail) withRailPowered(p bool) world.Block      { r.Powered = p; return r }
func (r ActivatorRail) railShape() RailShape                  { return r.Shape }
func (r ActivatorRail) withRailShape(s RailShape) world.Block { r.Shape = s; return r }
func (ActivatorRail) straight() bool                          { return true }
func (r ActivatorRail) railPowered() bool                     { return r.Powered }
func (r ActivatorRail) withRailPowered(p bool) world.Block    { r.Powered = p; return r }

// UseOnBlock ...
func (r PoweredRail) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	return railPlace(r, pos, face, tx, user, ctx)
}

// UseOnBlock ...
func (r ActivatorRail) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	return railPlace(r, pos, face, tx, user, ctx)
}

// BlockPlaced ...
func (r PoweredRail) BlockPlaced(pos cube.Pos, previous world.Block, tx *world.Tx) {
	railPlaced(r, pos, previous, tx)
}

// BlockPlaced ...
func (r ActivatorRail) BlockPlaced(pos cube.Pos, previous world.Block, tx *world.Tx) {
	railPlaced(r, pos, previous, tx)
}

// NeighbourUpdateTick ...
func (r PoweredRail) NeighbourUpdateTick(pos, changed cube.Pos, tx *world.Tx) {
	railNeighbourChanged(r, pos, changed, tx)
}

// NeighbourUpdateTick ...
func (r ActivatorRail) NeighbourUpdateTick(pos, changed cube.Pos, tx *world.Tx) {
	railNeighbourChanged(r, pos, changed, tx)
}

// RedstonePowerActionUpdate checks the power when the circuit next to the rail changes.
func (r PoweredRail) RedstonePowerActionUpdate(pos cube.Pos, tx *world.Tx, u world.RedstoneUpdate) {
	if u.OldPower != u.NewPower {
		railNeighbourChanged(r, pos, pos, tx)
	}
}

// RedstonePowerActionUpdate checks the power when the circuit next to the rail changes.
func (r ActivatorRail) RedstonePowerActionUpdate(pos cube.Pos, tx *world.Tx, u world.RedstoneUpdate) {
	if u.OldPower != u.NewPower {
		railNeighbourChanged(r, pos, pos, tx)
	}
}

// BlockRemoved ...
func (r PoweredRail) BlockRemoved(pos cube.Pos, replacement world.Block, tx *world.Tx) {
	if rep, ok := replacement.(railBlock); !ok || !sameRailKind(rep, r) {
		railRemoved(r, pos, tx)
	}
}

// BlockRemoved ...
func (r ActivatorRail) BlockRemoved(pos cube.Pos, replacement world.Block, tx *world.Tx) {
	if rep, ok := replacement.(railBlock); !ok || !sameRailKind(rep, r) {
		railRemoved(r, pos, tx)
	}
}

// BreakInfo ...
func (r PoweredRail) BreakInfo() BreakInfo {
	return newBreakInfo(0.7, alwaysHarvestable, pickaxeEffective, oneOf(PoweredRail{}))
}

// BreakInfo ...
func (r ActivatorRail) BreakInfo() BreakInfo {
	return newBreakInfo(0.7, alwaysHarvestable, pickaxeEffective, oneOf(ActivatorRail{}))
}

// EncodeItem ...
func (PoweredRail) EncodeItem() (string, int16) { return "minecraft:golden_rail", 0 }

// EncodeItem ...
func (ActivatorRail) EncodeItem() (string, int16) { return "minecraft:activator_rail", 0 }

// EncodeBlock ...
func (r PoweredRail) EncodeBlock() (string, map[string]any) {
	return "minecraft:golden_rail", map[string]any{"rail_direction": int32(r.Shape), "rail_data_bit": boolByte(r.Powered)}
}

// EncodeBlock ...
func (r ActivatorRail) EncodeBlock() (string, map[string]any) {
	return "minecraft:activator_rail", map[string]any{"rail_direction": int32(r.Shape), "rail_data_bit": boolByte(r.Powered)}
}

// DetectorRail is a straight rail that gives power while a minecart is on it, as Java Edition's DetectorRailBlock: a
// minecart touching it turns it on, and it checks again every 20 ticks until no minecart is left on it.
type DetectorRail struct {
	railBase
	// Shape is the way the rail lies. Detector rails never curve.
	Shape RailShape
	// Powered is true while a minecart is on the rail.
	Powered bool
}

func (r DetectorRail) railShape() RailShape                  { return r.Shape }
func (r DetectorRail) withRailShape(s RailShape) world.Block { r.Shape = s; return r }
func (DetectorRail) straight() bool                          { return true }

// UseOnBlock ...
func (r DetectorRail) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	return railPlace(r, pos, face, tx, user, ctx)
}

// BlockPlaced ...
func (r DetectorRail) BlockPlaced(pos cube.Pos, previous world.Block, tx *world.Tx) {
	_, wasDetector := previous.(DetectorRail)
	railPlaced(r, pos, previous, tx)
	if wasDetector {
		return
	}
	if cur, ok := tx.Block(pos).(DetectorRail); ok {
		cur.checkPressed(pos, tx, false)
	}
}

// EntityInside checks for minecarts when anything touches the rail while it is off.
func (r DetectorRail) EntityInside(pos cube.Pos, tx *world.Tx, _ world.Entity) {
	if !r.Powered {
		r.checkPressed(pos, tx, false)
	}
}

// ScheduledTick checks whether a minecart is still on the rail.
func (r DetectorRail) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if r.Powered {
		r.checkPressed(pos, tx, true)
	}
}

// checkPressed is Java Edition's DetectorRailBlock.checkPressed. scheduled is true for the rail's own scheduled check.
func (r DetectorRail) checkPressed(pos cube.Pos, tx *world.Tx, scheduled bool) {
	if !railSupported(tx, pos) {
		return
	}
	if _, ok := tx.Block(pos).(DetectorRail); !ok {
		return
	}
	pressed := len(detectorMinecarts(pos, tx, scheduled)) > 0
	if pressed != r.Powered {
		r.Powered = pressed
		tx.SetBlock(pos, r, nil)
		// updatePowerToConnected, then updateNeighborsAt for the rail and the block under it.
		for _, c := range newRailState(tx, pos, r).connections {
			tx.NotifyNeighbour(c, pos)
		}
		railNotifyAround(pos, tx)
		railNotifyAround(pos.Side(cube.FaceDown), tx)
	}
	if pressed && !tx.BlockUpdateScheduled(pos, r) {
		tx.ScheduleBlockUpdate(pos, r, time.Second)
	}
	detectorOutputUpdate(pos, tx)
}

// detectorOutputUpdate is Java Edition's updateNeighbourForOutputSignal: the comparators next to the rail, or behind a
// solid block next to it, read it again. Comparators read a detector rail only then, not every tick.
func detectorOutputUpdate(pos cube.Pos, tx *world.Tx) {
	for _, f := range []cube.Face{cube.FaceNorth, cube.FaceEast, cube.FaceSouth, cube.FaceWest} {
		n := pos.Side(f)
		if n.OutOfBounds(tx.Range()) {
			continue
		}
		b := tx.Block(n)
		if _, ok := b.(RedstoneComparator); ok {
			tx.NotifyNeighbour(n, pos)
			continue
		}
		if world.RedstoneFullPowerConductor(n, b, tx) {
			if far := n.Side(f); !far.OutOfBounds(tx.Range()) {
				if _, ok := tx.Block(far).(RedstoneComparator); ok {
					tx.NotifyNeighbour(far, pos)
				}
			}
		}
	}
}

// RailMinecart is a minecart entity, which detector rails detect.
type RailMinecart interface {
	world.Entity
	// DetectorBox returns the bounding box of the minecart as a detector rail sees it. scheduled is true for the
	// rail's scheduled check, which Java Edition runs before entities move.
	DetectorBox(tx *world.Tx, scheduled bool) cube.BBox
	// MinecartInventory returns the items of a chest or hopper minecart, or nil.
	MinecartInventory() *inventory.Inventory
}

// detectorMinecarts returns the minecarts in a detector rail's search box.
func detectorMinecarts(pos cube.Pos, tx *world.Tx, scheduled bool) []RailMinecart {
	x, y, z := float64(pos[0]), float64(pos[1]), float64(pos[2])
	search := cube.Box(x+0.2, y, z+0.2, x+1-0.2, y+1-0.2, z+1-0.2)
	var carts []RailMinecart
	for e := range tx.EntitiesWithin(search.Grow(2)) {
		c, ok := e.(RailMinecart)
		if !ok {
			continue
		}
		b := c.DetectorBox(tx, scheduled)
		lo, hi, slo, shi := b.Min(), b.Max(), search.Min(), search.Max()
		if lo[0] < shi[0] && hi[0] > slo[0] && lo[1] < shi[1] && hi[1] > slo[1] && lo[2] < shi[2] && hi[2] > slo[2] {
			carts = append(carts, c)
		}
	}
	return carts
}

// NeighbourUpdateTick ...
func (r DetectorRail) NeighbourUpdateTick(pos, changed cube.Pos, tx *world.Tx) {
	railNeighbourChanged(r, pos, changed, tx)
}

// BlockRemoved ...
func (r DetectorRail) BlockRemoved(pos cube.Pos, replacement world.Block, tx *world.Tx) {
	if rep, ok := replacement.(railBlock); !ok || !sameRailKind(rep, r) {
		railRemoved(r, pos, tx)
	}
}

// RedstonePower gives power while a minecart is on the rail.
func (r DetectorRail) RedstonePower(cube.Pos, *world.Tx, cube.Face) int {
	if r.Powered {
		return 15
	}
	return 0
}

// RedstoneStrongPower strongly powers the block below while a minecart is on the rail.
func (r DetectorRail) RedstoneStrongPower(_ cube.Pos, _ *world.Tx, face cube.Face) int {
	if r.Powered && face == cube.FaceDown {
		return 15
	}
	return 0
}

// BreakInfo ...
func (r DetectorRail) BreakInfo() BreakInfo {
	return newBreakInfo(0.7, alwaysHarvestable, pickaxeEffective, oneOf(DetectorRail{}))
}

// EncodeItem ...
func (DetectorRail) EncodeItem() (string, int16) { return "minecraft:detector_rail", 0 }

// EncodeBlock ...
func (r DetectorRail) EncodeBlock() (string, map[string]any) {
	return "minecraft:detector_rail", map[string]any{"rail_direction": int32(r.Shape), "rail_data_bit": boolByte(r.Powered)}
}

// allRails ...
func allRails() (b []world.Block) {
	for s := RailNorthSouth; s <= RailNorthEast; s++ {
		b = append(b, Rail{Shape: s})
	}
	for s := RailNorthSouth; s <= RailAscendingSouth; s++ {
		for _, p := range []bool{false, true} {
			b = append(b, PoweredRail{Shape: s, Powered: p}, ActivatorRail{Shape: s, Powered: p}, DetectorRail{Shape: s, Powered: p})
		}
	}
	return
}
