//go:build crossfly_full

package block

import (
	"cmp"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/gameevent"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Pistons with redstone = "dragonfly": Bedrock Edition's piston rules (PistonBlockActor) run on Dragonfly's own
// clock, one game tick at a time. The Java Edition pistons (piston.go) are untouched; every Piston method checks
// world.JavaRedstone() first and comes here when it is off.
//
// The rules:
//   - A piston is powered only by power reaching the piston itself, from any side but the one its arm comes out of
//     (PistonConsumer). No quasi-connectivity, no update detection: a piston reacts to the redstone engine telling it
//     its power changed, never to a plain block update.
//   - A move takes two game ticks, the arm going 0, 0.5, 1 (0.5 progress a tick), and the blocks land on the tick
//     after that. A move always completes: a short pulse extends the piston fully, and a sticky piston then pulls
//     its blocks back instead of dropping them.
//   - What moves is worked out as PistonBlockActor::_attachedBlockWalker does (dfPistonResolver): at most 12 blocks,
//     slime and honey blocks drag what touches them but not each other, glazed terracotta is pushed but never
//     pulled or dragged, and blocks with block entities move with their contents (dfPistonReaction).
//   - A powered piston that cannot push tries again every redstone tick (two game ticks) for as long as it is
//     powered: removing what was in the way lets it extend without an update.
//
// The state lives in the Piston itself (State, Progress, Attached) and the moving blocks (MovingPiston), and every
// step runs as a scheduled block tick of the piston, so the moves of many pistons happen in the order they were
// started and survive a restart (scheduled ticks are saved with the chunk).

const (
	// dfPistonPushLimit is how many blocks a piston moves at most.
	dfPistonPushLimit = 12
	// dfPistonRetry is how long a powered piston that could not push waits before it tries again: one Bedrock
	// redstone tick.
	dfPistonRetry = 2
)

// dfPistonPowered reports whether redstone power reaches the piston at pos from any side but its front.
func dfPistonPowered(pos cube.Pos, facing cube.Face, tx *world.Tx) bool {
	for _, f := range cube.Faces() {
		if f != facing && tx.RedstonePowerFrom(pos, f) > 0 {
			return true
		}
	}
	return false
}

// dfSchedule schedules the next step of the piston at pos the given number of game ticks from now.
func dfPistonSchedule(pos cube.Pos, p Piston, tx *world.Tx, ticks int) {
	tx.ScheduleBlockUpdate(pos, p, time.Duration(ticks)*time.Second/20)
}

// dfRedstoneUpdate is the redstone engine telling the piston its power may have changed.
func (p Piston) dfRedstoneUpdate(pos cube.Pos, tx *world.Tx) {
	on := dfPistonPowered(pos, p.Facing, tx)
	if on == p.Powered {
		return
	}
	p.Powered = on
	tx.SetBlock(pos, p, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true, KeepRedstonePower: true, DisableShapeUpdates: true})
	if (on && p.State == 0) || (!on && p.State == 2) {
		// The piston starts moving on the next tick, as Bedrock's piston looks at its power when it ticks.
		dfPistonSchedule(pos, p, tx, 1)
	}
}

// ScheduledTick runs the steps of a piston with redstone = "dragonfly".
func (p Piston) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if world.JavaRedstone() {
		return
	}
	switch p.State {
	case 0:
		if p.Powered {
			p.dfExtend(pos, tx)
		}
	case 2:
		if !p.Powered {
			p.dfRetract(pos, tx)
		}
	case 1, 3:
		p.dfStep(pos, tx)
	}
}

// dfMoveDirection is the direction the blocks of the piston's current move go in.
func (p Piston) dfMoveDirection() cube.Face {
	if p.State == 3 {
		return p.Facing.Opposite()
	}
	return p.Facing
}

// dfExtend starts pushing out, or tries again later if the blocks in front cannot be moved.
func (p Piston) dfExtend(pos cube.Pos, tx *world.Tx) {
	r, ok := dfResolvePiston(tx, pos, p.Facing, true)
	if !ok {
		dfPistonSchedule(pos, p, tx, dfPistonRetry)
		return
	}
	now := tx.CurrentTick()
	p.State, p.Extended, p.Started = 1, true, now
	p.LastProgress, p.Progress = 0, .5
	p.Attached = r.attached
	tx.SetBlock(pos, p, &world.SetOpts{DisableRedstoneUpdates: true, KeepRedstonePower: true})
	r.move(pos, p, tx)
	head := pos.Side(p.Facing)
	tx.SetBlock(head, MovingPiston{Moving: PistonHead{Facing: p.Facing, Sticky: p.Sticky}, Piston: pos, Facing: p.Facing, Extending: true, Source: true, Started: now, Progress: .5}, nil)
	tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: "tile.piston.out", Volume: .5, Pitch: .7})
	postBlockEvent(tx, gameevent.PistonExtend, pos, nil)
	dfPistonMoveEntities(pos, p, tx, 0, .5)
	dfPistonSchedule(pos, p, tx, 1)
}

// dfRetract pulls the arm in and, for a sticky piston, the blocks in front of it.
func (p Piston) dfRetract(pos cube.Pos, tx *world.Tx) {
	now := tx.CurrentTick()
	p.State, p.Extended, p.Started = 3, false, now
	p.LastProgress, p.Progress = 1, .5
	p.Attached = nil
	head := pos.Side(p.Facing)
	if h, ok := tx.Block(head).(PistonHead); ok && h.Facing == p.Facing {
		tx.SetBlock(head, nil, nil)
	}
	var r *dfPistonResolver
	if p.Sticky {
		if res, ok := dfResolvePiston(tx, pos, p.Facing, false); ok {
			r = res
			p.Attached = r.attached
		}
	}
	tx.SetBlock(pos, p, &world.SetOpts{DisableRedstoneUpdates: true, KeepRedstonePower: true})
	if r != nil {
		r.move(pos, p, tx)
		dfPistonMoveEntities(pos, p, tx, 0, .5)
	}
	tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: "tile.piston.in", Volume: .5, Pitch: .7})
	postBlockEvent(tx, gameevent.PistonContract, pos, nil)
	dfPistonSchedule(pos, p, tx, 1)
}

// dfStep advances a moving piston: half a block on the tick after it started, then the blocks land.
func (p Piston) dfStep(pos cube.Pos, tx *world.Tx) {
	if tx.CurrentTick()-p.Started < 2 {
		p.LastProgress = p.Progress
		if p.State == 1 {
			p.Progress = 1
		} else {
			p.Progress = 0
		}
		tx.SetBlock(pos, p, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true, KeepRedstonePower: true, DisableShapeUpdates: true})
		for _, at := range p.dfMovingPositions(pos) {
			if m, ok := tx.Block(at).(MovingPiston); ok && m.Piston == pos {
				m.LastProgress, m.Progress = m.Progress, 1
				tx.SetBlockEntity(at, m)
			}
		}
		if p.State == 1 || p.Sticky {
			dfPistonMoveEntities(pos, p, tx, .5, 1)
		}
		dfPistonSchedule(pos, p, tx, 1)
		return
	}
	extending := p.State == 1
	p.dfLand(pos, tx)
	if extending {
		p.State, p.Progress, p.LastProgress = 2, 1, 1
	} else {
		p.State, p.Progress, p.LastProgress = 0, 0, 0
	}
	p.Attached = nil
	tx.SetBlock(pos, p, &world.SetOpts{DisableRedstoneUpdates: true, KeepRedstonePower: true})
	if extending != p.Powered {
		// The power changed while the piston moved: it goes back on the next tick.
		dfPistonSchedule(pos, p, tx, 1)
	}
}

// dfMovingPositions returns where the moving blocks of the piston's current move are, the ones furthest along the
// move first, followed by the arm when it is extending.
func (p Piston) dfMovingPositions(pos cube.Pos) []cube.Pos {
	dir := p.dfMoveDirection()
	out := make([]cube.Pos, 0, len(p.Attached)+1)
	for _, at := range dfSortedForMove(p.Attached, dir) {
		out = append(out, at.Side(dir))
	}
	if p.State == 1 {
		out = append(out, pos.Side(p.Facing))
	}
	return out
}

// dfLand places the moving blocks of the piston's move where they arrived (PistonBlockActor::_spawnBlocks), the
// ones furthest along first.
func (p Piston) dfLand(pos cube.Pos, tx *world.Tx) {
	for _, at := range p.dfMovingPositions(pos) {
		if m, ok := tx.Block(at).(MovingPiston); ok && m.Piston == pos {
			m.dfPlace(at, tx)
		}
	}
}

// dfPlace puts the block a moving block carries at pos, which is where it arrived or, for a moving block whose
// piston is gone, where it got to (MovingBlockActor::tick).
func (m MovingPiston) dfPlace(pos cube.Pos, tx *world.Tx) {
	b := m.Moving
	if b == nil {
		tx.SetBlock(pos, nil, nil)
		return
	}
	if _, ok := b.(PistonHead); ok && !m.Source {
		b = nil
	}
	if b == nil {
		tx.SetBlock(pos, nil, nil)
		return
	}
	tx.SetBlock(pos, b, nil)
	dfWaterlogLanded(pos, b, tx)
	if l, ok := b.(interface {
		MovedByPiston(pos cube.Pos, tx *world.Tx)
	}); ok {
		l.MovedByPiston(pos, tx)
	}
}

// dfWaterlogLanded fills a block that can hold water with water when it lands between two water sources or more:
// Bedrock moves no water with a block, but one that lands in water fills (PistonBlockActor::_spawnBlocks).
func dfWaterlogLanded(pos cube.Pos, b world.Block, tx *world.Tx) {
	d, ok := b.(world.LiquidDisplacer)
	source := Water{Depth: 8, Still: true}
	if !ok || !d.CanDisplace(source) {
		return
	}
	sources := 0
	for _, f := range cube.HorizontalFaces() {
		if l, ok := tx.Liquid(pos.Side(f)); ok {
			if w, ok := l.(Water); ok && w.Depth == 8 && !w.Falling {
				sources++
			}
		}
	}
	if sources >= 2 {
		tx.SetLiquid(pos, source)
	}
}

// dfTick is the moving block's own tick: a moving block whose piston is gone or no longer moving it is placed at
// once where it is (MovingBlockActor::tick).
func (m MovingPiston) dfTick(pos cube.Pos, tx *world.Tx) {
	if p, ok := tx.Block(m.Piston).(Piston); ok && (p.State == 1 || p.State == 3) {
		return
	}
	if _, arm := m.Moving.(PistonHead); arm {
		tx.SetBlock(pos, nil, nil)
		return
	}
	m.dfPlace(pos, tx)
}

// dfSortedForMove returns the positions sorted the furthest along dir first.
func dfSortedForMove(positions []cube.Pos, dir cube.Face) []cube.Pos {
	step := cube.Pos{}.Side(dir)
	along := func(p cube.Pos) int { return p[0]*step[0] + p[1]*step[1] + p[2]*step[2] }
	out := slices.Clone(positions)
	slices.SortStableFunc(out, func(a, b cube.Pos) int { return cmp.Compare(along(b), along(a)) })
	return out
}

// dfPistonResolver works out which blocks a piston moves and which it breaks, with the rules of Bedrock's
// PistonBlockActor::_attachedBlockWalker and _handleSlimeConnections.
type dfPistonResolver struct {
	tx      *world.Tx
	piston  cube.Pos
	facing  cube.Face
	dir     cube.Face
	pulling bool

	attached, breaks []cube.Pos
}

// dfResolvePiston works out the blocks the piston at pos moves when it extends, or, for extending false, the blocks a
// sticky piston pulls back when it retracts (its arm already gone).
func dfResolvePiston(tx *world.Tx, pos cube.Pos, facing cube.Face, extending bool) (*dfPistonResolver, bool) {
	r := &dfPistonResolver{tx: tx, piston: pos, facing: facing, dir: facing, pulling: !extending}
	start := pos.Side(facing)
	if !extending {
		r.dir = facing.Opposite()
		start = start.Side(facing)
	}
	ok := r.walk(start, facing) && len(r.attached) <= dfPistonPushLimit
	return r, ok
}

func (r *dfPistonResolver) has(pos cube.Pos) bool { return slices.Contains(r.attached, pos) }

// walk looks at the block at pos, reached going towards from, and adds it and what it drags to the move. It reports
// false if the move cannot happen.
func (r *dfPistonResolver) walk(pos cube.Pos, from cube.Face) bool {
	if r.has(pos) {
		return true
	}
	if pos.OutOfBounds(r.tx.Range()) {
		return from != r.dir
	}
	b := r.tx.Block(pos)
	reaction := dfPistonReaction(b)
	if pos == r.piston {
		reaction = dfPistonImmovable
	}
	switch reaction {
	case dfPistonAir:
		return true
	case dfPistonImmovable:
		// Something that cannot move only stops the move when it is in the way; one beside a slime block stays.
		return from != r.dir
	case dfPistonBreak:
		if from == r.dir || (!r.pulling && dfAttachedTo(b, pos, pos.Side(from.Opposite()))) {
			r.addBreak(pos)
		}
		return true
	case dfPistonGrabber:
		return r.grab(pos, b, from)
	case dfPistonPushOnly:
		if from != r.dir {
			// Glazed terracotta is pushed, but never pulled or dragged along.
			return true
		}
	}
	r.attached = append(r.attached, pos)
	if len(r.attached) > dfPistonPushLimit {
		return false
	}
	next := pos.Side(r.dir)
	if next.OutOfBounds(r.tx.Range()) {
		return false
	}
	return r.walk(next, r.dir)
}

// grab adds a slime or honey block to the move and everything it drags: what touches it, apart from the block it
// was reached from, and a slime block and a honey block only push each other.
func (r *dfPistonResolver) grab(pos cube.Pos, b world.Block, from cube.Face) bool {
	r.attached = append(r.attached, pos)
	if len(r.attached) > dfPistonPushLimit {
		return false
	}
	if pos.Side(r.dir).OutOfBounds(r.tx.Range()) {
		return false
	}
	back := from.Opposite()
	for _, f := range cube.Faces() {
		if f == back {
			continue
		}
		n := pos.Side(f)
		if f != r.dir && dfPistonNonSticking(b, r.tx.Block(n)) {
			continue
		}
		if !r.walk(n, f) {
			return false
		}
	}
	return true
}

func (r *dfPistonResolver) addBreak(pos cube.Pos) {
	if !slices.Contains(r.breaks, pos) {
		r.breaks = append(r.breaks, pos)
	}
}

// move breaks the blocks in the way and turns the moved blocks into moving blocks one block further along
// (PistonBlockActor::_spawnMovingBlocks), the ones furthest along first.
func (r *dfPistonResolver) move(pos cube.Pos, p Piston, tx *world.Tx) {
	for _, at := range r.breaks {
		b := tx.Block(at)
		if _, liquid := b.(world.Liquid); liquid {
			tx.SetBlock(at, nil, nil)
			continue
		}
		breakBlock(b, at, tx)
	}
	now := tx.CurrentTick()
	extending := !r.pulling
	destinations := make(map[cube.Pos]struct{}, len(r.attached))
	for _, at := range r.attached {
		destinations[at.Side(r.dir)] = struct{}{}
	}
	for _, at := range dfSortedForMove(r.attached, r.dir) {
		b := dfDetachForMove(at, tx)
		tx.SetBlock(at.Side(r.dir), MovingPiston{Moving: b, Piston: pos, Facing: p.Facing, Extending: extending, Started: now, Progress: .5}, nil)
		if _, ok := destinations[at]; !ok {
			tx.SetBlock(at, nil, nil)
			// Bedrock moves no water with a block: the water a block held is gone.
			if _, ok := tx.Liquid(at); ok {
				tx.SetLiquid(at, nil)
			}
		}
	}
}

// dfDetachForMove readies the block at pos to be moved and returns it: a double chest comes apart, its viewers
// closed (PistonBlockActor::_spawnMovingBlock).
func dfDetachForMove(pos cube.Pos, tx *world.Tx) world.Block {
	b := tx.Block(pos)
	if c, ok := b.(Chest); ok && c.paired {
		pairPos := c.pairPos(pos)
		if ch, pair, ok := c.unpair(tx, pos); ok {
			tx.SetBlock(pairPos, pair, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true})
			return ch
		}
	}
	return b
}

// dfAttachedTo reports whether the block b at pos hangs on the block at support: a block that breaks when pushed
// and is reached from the side of a moving slime block breaks only if it hangs on that slime block.
func dfAttachedTo(b world.Block, pos, support cube.Pos) bool {
	var on cube.Pos
	switch b := b.(type) {
	case Torch:
		on = pos.Side(b.Facing)
	case Ladder:
		on = pos.Side(b.Facing.Face().Opposite())
	case ItemFrame:
		on = pos.Side(b.Facing)
	case Button:
		on = pos.Side(b.Facing.Opposite())
	case Lever:
		on = pos.Side(b.Facing.Opposite())
	default:
		on = pos.Side(cube.FaceDown)
	}
	return on == support
}

// dfPistonMoveEntities moves the entities in the way of the arm and the moving blocks of the piston at pos while
// their progress goes from a to b (PistonBlockActor::_moveCollidedEntities, MovingBlockActor::moveCollidedEntities):
// pushed out of the way along the move, carried along on top of a block moving sideways, and launched by a slime
// block at one block a tick.
func dfPistonMoveEntities(pos cube.Pos, p Piston, tx *world.Tx, a, b float64) {
	dir := p.dfMoveDirection()
	step := cube.Pos{}.Side(dir).Vec3()
	axis := 0
	switch dir.Axis() {
	case cube.Y:
		axis = 1
	case cube.Z:
		axis = 2
	}
	positive := step[axis] > 0
	delta := b - a

	type moved struct {
		e      world.Entity
		amount float64
		launch bool
	}
	var order []*world.EntityHandle
	moves := make(map[*world.EntityHandle]*moved)
	add := func(e world.Entity, amount float64, launch bool) {
		if amount <= 0 {
			return
		}
		m, ok := moves[e.H()]
		if !ok {
			m = &moved{e: e}
			moves[e.H()] = m
			order = append(order, e.H())
		}
		m.amount = max(m.amount, min(amount, delta+.01))
		m.launch = m.launch || launch
	}
	push := func(box, before cube.BBox, slime bool) {
		for e := range tx.EntitiesWithin(box.Grow(.01)) {
			bounds := e.H().Type().BBox(e).Translate(e.Position())
			if !bounds.IntersectsWith(box) {
				continue
			}
			amount := bounds.Max()[axis] - box.Min()[axis]
			if positive {
				amount = box.Max()[axis] - bounds.Min()[axis]
			}
			add(e, amount+.01, slime)
		}
		if dir.Axis() == cube.Y {
			return
		}
		// Entities standing on a block that moves sideways go along with it.
		top := cube.Box(before.Min()[0], before.Max()[1], before.Min()[2], before.Max()[0], before.Max()[1]+.05, before.Max()[2])
		for e := range tx.EntitiesWithin(top.Grow(.01)) {
			at := e.Position()
			if at[1] < top.Min()[1]-.01 || at[0] < top.Min()[0] || at[0] > top.Max()[0] || at[2] < top.Min()[2] || at[2] > top.Max()[2] {
				continue
			}
			add(e, delta, false)
		}
	}

	positions := p.dfMovingPositions(pos)
	if p.State == 3 {
		positions = positions[:len(p.Attached)]
	}
	for _, at := range positions {
		m, ok := tx.Block(at).(MovingPiston)
		if !ok || m.Piston != pos || m.Moving == nil {
			continue
		}
		_, slime := m.Moving.(Slime)
		for _, box := range m.Moving.Model().BBox(at, tx) {
			box = box.Translate(at.Vec3())
			push(box.Translate(step.Mul(b-1)), box.Translate(step.Mul(a-1)), slime)
		}
	}
	if p.State == 1 {
		// The arm, a full block that slides out of the base.
		arm := cube.Box(0, 0, 0, 1, 1, 1).Translate(pos.Vec3())
		push(arm.Translate(step.Mul(b)), arm.Translate(step.Mul(a)), false)
	}

	for _, h := range order {
		m := moves[h]
		e := m.e
		move := step.Mul(m.amount)
		move = pistonClipMovement(e.H().Type().BBox(e).Translate(e.Position()), move, tx, pos)
		if move.LenSqr() > 0 {
			if mover, ok := e.(interface{ MoveByPiston(mgl64.Vec3) }); ok {
				mover.MoveByPiston(move)
			} else if mover, ok := e.(interface{ Teleport(mgl64.Vec3) }); ok {
				mover.Teleport(e.Position().Add(move))
			}
		}
		if m.launch {
			if v, ok := e.(velocityEntity); ok {
				v.SetVelocity(step)
			}
		}
	}
}
