//go:build crossfly_full

package commands

import (
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/debugcmd"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// RegisterExtra registers the extra commands of Crossfly Full: /setblock and /fill for everyone with operator
// status, and with the mobs on, /summon and /mobdebug.
func RegisterExtra() {
	cmd.Register(cmd.New("setblock", "Changes a block to another block.", nil, setBlock{}))
	cmd.Register(cmd.New("fill", "Fills all or parts of a region with a specific block.", nil, fill{}))
	if entity.Mobs() {
		cmd.Register(cmd.New("summon", "Summons a mob.", nil, summon{}))
		cmd.Register(debugcmd.Command(isOp))
	}
}

// --- /summon ---

// mobName is the identifier of a registered mob.
type mobName string

func (mobName) Type() string { return "EntityType" }
func (mobName) Options(cmd.Source) []string {
	var out []string
	for _, t := range mob.Types() {
		if _, ok := t.(*mob.Type); !ok {
			continue // the leash knot
		}
		out = append(out, strings.TrimPrefix(t.EncodeEntity(), "minecraft:"), t.EncodeEntity())
	}
	slices.Sort(out)
	return out
}

type summon struct {
	op
	Mob      mobName                  `cmd:"entityType"`
	Position cmd.Optional[mgl64.Vec3] `cmd:"spawnPos"`
}

func (s summon) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	id := "minecraft:" + strings.TrimPrefix(string(s.Mob), "minecraft:")
	pos, ok := s.Position.Load()
	if !ok {
		pos = src.Position()
	}
	if _, err := mob.Spawn(tx, id, pos, mob.SpawnOptions{}); err != nil {
		o.Errorf("Could not summon %v: %v", id, err)
		return
	}
	o.Printf("Summoned %v", id)
}

// --- /setblock, /fill ---

// The block arguments take states in either edition's syntax: see BlockArg in blockarg.go.

type setBlock struct {
	op
	Position mgl64.Vec3 `cmd:"position"`
	Block    BlockArg   `cmd:"block"`
}

func (s setBlock) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	b, err := ParseBlock(string(s.Block))
	if err != nil {
		o.Error(err)
		return
	}
	pos := cube.PosFromVec3(s.Position)
	place(tx, pos, b)
	o.Printf("Block placed at %v", pos)
}

// place sets the block typed at pos, with a fresh block entity and with water for a waterlogged Java state.
func place(tx *world.Tx, pos cube.Pos, b BlockState) {
	bl := b.Block()
	if nbt, ok := bl.(world.NBTer); ok {
		// A value of its own per block (one chest is not another's), and never the nil data of an
		// unimplemented block.
		bl = nbt.DecodeNBT(map[string]any{}).(world.Block)
	}
	tx.SetBlock(pos, bl, nil)
	if l := b.Liquid(); l != nil {
		tx.SetLiquid(pos, l)
	}
}

// maxFill is the most blocks /fill changes at once, as in vanilla.
const maxFill = 32768

type fill struct {
	op
	From   mgl64.Vec3                   `cmd:"from"`
	To     mgl64.Vec3                   `cmd:"to"`
	Block  BlockArg                     `cmd:"block"`
	Mode   cmd.Optional[fillMode]       `cmd:"mode"`
	Filter cmd.Optional[BlockFilterArg] `cmd:"filter"`
}

// fillMode is /fill's mode. replace, with or without a filter, is vanilla's default.
type fillMode string

func (fillMode) Type() string                { return "FillMode" }
func (fillMode) Options(cmd.Source) []string { return []string{"replace"} }

func (f fill) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	b, err := ParseBlock(string(f.Block))
	if err != nil {
		o.Error(err)
		return
	}
	var filter *BlockState
	if text, ok := f.Filter.Load(); ok {
		fb, err := ParseBlock(string(text))
		if err != nil {
			o.Error(err)
			return
		}
		filter = &fb
	}
	a, c := cube.PosFromVec3(f.From), cube.PosFromVec3(f.To)
	lo := cube.Pos{min(a[0], c[0]), min(a[1], c[1]), min(a[2], c[2])}
	hi := cube.Pos{max(a[0], c[0]), max(a[1], c[1]), max(a[2], c[2])}
	if n := (hi[0] - lo[0] + 1) * (hi[1] - lo[1] + 1) * (hi[2] - lo[2] + 1); n > maxFill {
		o.Errorf("Too many blocks in the specified area (%v > %v)", n, maxFill)
		return
	}
	n := 0
	for pos := range cube.Range3D(lo, hi) {
		if filter != nil && !filter.Matches(tx.Block(pos)) {
			continue
		}
		place(tx, pos, b)
		n++
	}
	o.Printf("%v blocks filled", n)
}
