//go:build crossfly_full

package block

import (
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// The redstone torch with Java redstone (Crossfly Full): Java Edition's RedstoneTorchBlock and
// RedstoneWallTorchBlock. It inverts the block it is attached to two game ticks later and burns out when it toggles
// too often.

const (
	// redstoneTorchDelay is Java Edition's TOGGLE_DELAY: 2 game ticks.
	redstoneTorchDelay = time.Second / 10
	// redstoneTorchToggleWindow is RECENT_TOGGLE_TIMER, in game ticks.
	redstoneTorchToggleWindow = 60
	// redstoneTorchMaxToggles is MAX_RECENT_TOGGLES.
	redstoneTorchMaxToggles = 8
	// redstoneTorchRestartDelay is RESTART_DELAY: a burned out torch looks again after 160 game ticks.
	redstoneTorchRestartDelay = 8 * time.Second
)

// javaUseOnBlock places the torch lit, as Java Edition does: its placement update then turns it off two ticks later
// if the block it is on is powered.
func (t RedstoneTorch) javaUseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, face, used := firstReplaceable(tx, pos, face, t)
	if !used || face == cube.FaceDown {
		return false
	}
	if !tx.Block(pos.Side(face.Opposite())).Model().FaceSolid(pos.Side(face.Opposite()), face, tx) {
		fallbackFace, ok := findTorchPlacementFace(pos, tx)
		if !ok {
			return false
		}
		face = fallbackFace
	}
	t.Facing = face.Opposite()
	t.Lit = true
	place(tx, pos, t, user, ctx)
	return placed(ctx)
}

// BlockPlaced is Java Edition's onPlace: the blocks around each neighbour are updated, the torch itself among them.
func (t RedstoneTorch) BlockPlaced(pos cube.Pos, _ world.Block, tx *world.Tx) {
	redstoneTorchNotify(pos, tx)
}

// BlockRemoved is Java Edition's affectNeighborsAfterRemoval.
func (t RedstoneTorch) BlockRemoved(pos cube.Pos, replacement world.Block, tx *world.Tx) {
	if _, ok := replacement.(RedstoneTorch); ok {
		return
	}
	redstoneTorchNotify(pos, tx)
}

// redstoneTorchNotify updates the blocks around each of the torch's neighbours (updateNeighborsAt for every side).
func redstoneTorchNotify(pos cube.Pos, tx *world.Tx) {
	for _, f := range cube.Faces() {
		n := pos.Side(f)
		for _, g := range cube.Faces() {
			tx.NotifyNeighbour(n.Side(g), n)
		}
	}
}

// javaNeighbourUpdateTick breaks an unsupported torch and otherwise looks whether it has to change.
func (t RedstoneTorch) javaNeighbourUpdateTick(pos cube.Pos, tx *world.Tx) {
	facing := t.facing()
	if !tx.Block(pos.Side(facing)).Model().FaceSolid(pos.Side(facing), facing.Opposite(), tx) {
		breakBlock(t, pos, tx)
		return
	}
	t.check(pos, tx)
}

// check is Java Edition's neighborChanged: a torch whose lit state equals its input (lit and powered, or off and
// unpowered) changes two ticks later, unless a tick is already on its way.
func (t RedstoneTorch) check(pos cube.Pos, tx *world.Tx) {
	if t.Lit != t.input(pos, tx) || redstoneTorchScheduled(pos, t, tx) {
		return
	}
	redstoneTorchSchedule(pos, t, tx, redstoneTorchDelay)
}

// input is Java Edition's hasNeighborSignal for torches: the signal from the block the torch is attached to.
func (t RedstoneTorch) input(pos cube.Pos, tx *world.Tx) bool {
	f := t.facing()
	return pistonSignal(pos.Side(f), f.Opposite(), tx) > 0
}

// javaScheduledTick is Java Edition's tick: a lit, powered torch turns off (and burns out if it did so too often);
// an unlit, unpowered torch lights again unless it is burned out.
func (t RedstoneTorch) javaScheduledTick(pos cube.Pos, tx *world.Tx) {
	cur, ok := tx.Block(pos).(RedstoneTorch)
	if !ok {
		return
	}
	t = cur
	input := t.input(pos, tx)
	now := tx.CurrentTick()
	torchToggles.prune(tx.World(), now)
	if t.Lit {
		if input {
			t.Lit = false
			tx.SetBlock(pos, t, nil)
			if torchToggles.tooFrequent(tx.World(), pos, now, true) {
				tx.PlaySound(pos.Vec3Centre(), sound.Fizz{})
				redstoneTorchSchedule(pos, t, tx, redstoneTorchRestartDelay)
			}
		}
	} else if !input && !torchToggles.tooFrequent(tx.World(), pos, now, false) {
		t.Lit = true
		tx.SetBlock(pos, t, nil)
	}
}

// redstoneTorchScheduled reports whether a tick of the torch at pos is on its way, in either lit state. Java Edition
// keeps one scheduled tick per block type and position; here ticks are kept per state.
func redstoneTorchScheduled(pos cube.Pos, t RedstoneTorch, tx *world.Tx) bool {
	for _, lit := range []bool{false, true} {
		v := t
		v.Lit = lit
		if tx.BlockUpdateScheduled(pos, v) {
			return true
		}
	}
	return false
}

// redstoneTorchSchedule schedules the torch's tick for both lit states, so that it runs whichever state the torch
// has by then.
func redstoneTorchSchedule(pos cube.Pos, t RedstoneTorch, tx *world.Tx, delay time.Duration) {
	for _, lit := range []bool{false, true} {
		v := t
		v.Lit = lit
		tx.ScheduleBlockUpdate(pos, v, delay)
	}
}

// torchToggleLog is Java Edition's RECENT_TOGGLES: per world, when each torch last turned off.
type torchToggleLog struct {
	mu      sync.Mutex
	toggles map[*world.World][]torchToggle
}

type torchToggle struct {
	pos  cube.Pos
	when int64
}

var torchToggles = &torchToggleLog{toggles: map[*world.World][]torchToggle{}}

// prune drops the toggles older than the window, from the oldest, as Java Edition does at the start of a tick.
func (l *torchToggleLog) prune(w *world.World, now int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	t := l.toggles[w]
	for len(t) > 0 && now-t[0].when > redstoneTorchToggleWindow {
		t = t[1:]
	}
	if len(t) == 0 {
		delete(l.toggles, w)
		return
	}
	l.toggles[w] = t
}

// tooFrequent is Java Edition's isToggledTooFrequently: it records a toggle if add is set, then reports whether the
// torch at pos toggled 8 times within the window.
func (l *torchToggleLog) tooFrequent(w *world.World, pos cube.Pos, now int64, add bool) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if add {
		l.toggles[w] = append(l.toggles[w], torchToggle{pos: pos, when: now})
	}
	n := 0
	for _, t := range l.toggles[w] {
		if t.pos == pos {
			if n++; n >= redstoneTorchMaxToggles {
				return true
			}
		}
	}
	return false
}
