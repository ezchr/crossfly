//go:build crossfly_full

package mob

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// The blocks mobs listen to: note blocks (allays) and jukeboxes (dancing allays and parrots) call
// these through block.Mobs().
var (
	noteBlockHooks []func(tx *world.Tx, pos cube.Pos)
	jukeboxHooks   []func(tx *world.Tx, pos cube.Pos, playing bool)
)

// Compile time checks that BlockHost hears the blocks.
var (
	_ block.NoteListener    = BlockHost{}
	_ block.JukeboxListener = BlockHost{}
)

// RegisterNoteBlockHook registers f to be called each time a note block plays (package
// mob/goal/misc/social: allays like the note block). Call it from an init function.
func RegisterNoteBlockHook(f func(tx *world.Tx, pos cube.Pos)) {
	noteBlockHooks = append(noteBlockHooks, f)
}

// RegisterJukeboxHook registers f to be called when a jukebox starts playing, once a second while
// it plays, and when it stops (block.JukeboxListener). Call it from an init function.
func RegisterJukeboxHook(f func(tx *world.Tx, pos cube.Pos, playing bool)) {
	jukeboxHooks = append(jukeboxHooks, f)
}

// NotePlayed implements block.NoteListener.
func (BlockHost) NotePlayed(tx *world.Tx, pos cube.Pos) {
	for _, f := range noteBlockHooks {
		f(tx, pos)
	}
}

// JukeboxPlaying implements block.JukeboxListener.
func (BlockHost) JukeboxPlaying(tx *world.Tx, pos cube.Pos, playing bool) {
	for _, f := range jukeboxHooks {
		f(tx, pos, playing)
	}
}
