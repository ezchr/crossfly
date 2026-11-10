package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// noteJavaBase is the Java Edition 26.3 state id of note_block[instrument=harp,note=0,powered=true]: the states run
// by instrument (50 each), then note (2 each), then powered (true first).
const noteJavaBase = 680

// JavaState returns the Java Edition block state of the note block: its instrument, note and whether it is powered.
// Java clients draw and play note blocks from this state; without it every note block is an unpowered harp at note 0.
func (n Note) JavaState() int32 {
	powered := int32(1)
	if n.Powered {
		powered = 0
	}
	return noteJavaBase + int32(n.javaInstrument)*50 + int32(min(max(n.Pitch, 0), 24))*2 + powered
}

// BlockStateKey makes a change of the note block's Java state a state change, as it is in Java Edition: an observer
// watching a note block sees it become powered or unpowered, or change note or instrument.
func (n Note) BlockStateKey() uint64 { return uint64(n.JavaState()) }

// JavaBlockEntity reports that Java clients draw note blocks without a block entity.
func (Note) JavaBlockEntity() (string, []byte) { return "", nil }

// EncodeNetworkNBT is the block entity data Bedrock clients get: the instrument stays on the server.
func (n Note) EncodeNetworkNBT() map[string]any {
	return map[string]any{"note": byte(n.Pitch), "powered": boolByte(n.Powered)}
}

// noteInstrumentKey is the server-only NBT key of the instrument Java Edition keeps in the note block's state.
const noteInstrumentKey = "df_instrument"

func (n Note) decodeInstrument(data map[string]any) Note {
	n.javaInstrument = nbtconv.Uint8(data, noteInstrumentKey)
	return n
}

// javaInstrumentAt is the instrument Java Edition gives a note block at pos (NoteBlock.setInstrument): the block
// below picks it, harp when it has none.
func javaInstrumentAt(pos cube.Pos, tx *world.Tx) uint8 {
	if b, ok := tx.Block(pos.Side(cube.FaceDown)).(interface{ Instrument() sound.Instrument }); ok {
		if i := b.Instrument().Int32(); i >= 0 && i < 16 {
			return uint8(i) // Bedrock's 16 instruments are in Java's order
		}
	}
	return 0
}

// updateInstrument stores the instrument for pos when it changed, without block updates (a shape update in Java).
func (n Note) updateInstrument(pos cube.Pos, tx *world.Tx) {
	if i := javaInstrumentAt(pos, tx); i != n.javaInstrument {
		n.javaInstrument = i
		tx.SetBlock(pos, n, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true, KeepRedstonePower: true})
	}
}

// BlockPlaced sets the instrument from the block below, as Java does when a note block is placed.
func (n Note) BlockPlaced(pos cube.Pos, _ world.Block, tx *world.Tx) {
	n.updateInstrument(pos, tx)
}

// BlockStateChanged sets the instrument again when the block above or below changes (Java's updateShape on the Y
// axis).
func (n Note) BlockStateChanged(pos, changed cube.Pos, tx *world.Tx) {
	if changed == pos.Side(cube.FaceDown) || changed == pos.Side(cube.FaceUp) {
		n.updateInstrument(pos, tx)
	}
}
