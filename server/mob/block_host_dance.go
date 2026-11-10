//go:build crossfly_full

package mob

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// The blocks mobs listen to: note blocks (allays) call these through block.Mobs().
var noteBlockHooks []func(tx *world.Tx, pos cube.Pos)

// Compile time check that BlockHost hears the blocks.
var _ block.NoteListener = BlockHost{}

// RegisterNoteBlockHook registers f to be called each time a note block plays (package
// mob/goal/misc/social: allays like the note block). Call it from an init function.
func RegisterNoteBlockHook(f func(tx *world.Tx, pos cube.Pos)) {
	noteBlockHooks = append(noteBlockHooks, f)
}

// NotePlayed implements block.NoteListener.
func (BlockHost) NotePlayed(tx *world.Tx, pos cube.Pos) {
	for _, f := range noteBlockHooks {
		f(tx, pos)
	}
}
