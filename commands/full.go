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

// blockName is the name of a block, suggested from the registered block items.
type blockName string

func (blockName) Type() string { return "Block" }
func (blockName) Options(cmd.Source) []string {
	seen := map[string]bool{}
	var out []string
	for _, it := range world.Items() {
		if _, ok := it.(world.Block); !ok {
			continue
		}
		name, _ := it.EncodeItem()
		short := strings.TrimPrefix(name, "minecraft:")
		if !seen[short] {
			seen[short] = true
			out = append(out, short, name)
		}
	}
	slices.Sort(out)
	return out
}

// lookupBlock returns the block a block name stands for: the block of its item, or air.
func lookupBlock(name blockName) (world.Block, bool) {
	n := strings.TrimPrefix(string(name), "minecraft:")
	if n == "air" {
		return nil, true
	}
	it, ok := world.ItemByName("minecraft:"+n, 0)
	if !ok {
		return nil, false
	}
	b, ok := it.(world.Block)
	return b, ok
}

type setBlock struct {
	op
	Position mgl64.Vec3 `cmd:"position"`
	Block    blockName  `cmd:"block"`
}

func (s setBlock) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	b, ok := lookupBlock(s.Block)
	if !ok {
		o.Errorf("Unknown block: %v", s.Block)
		return
	}
	pos := cube.PosFromVec3(s.Position)
	tx.SetBlock(pos, b, nil)
	o.Printf("Block placed at %v", pos)
}

// maxFill is the most blocks /fill changes at once, as in vanilla.
const maxFill = 32768

type fill struct {
	op
	From  mgl64.Vec3 `cmd:"from"`
	To    mgl64.Vec3 `cmd:"to"`
	Block blockName  `cmd:"block"`
}

func (f fill) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	b, ok := lookupBlock(f.Block)
	if !ok {
		o.Errorf("Unknown block: %v", f.Block)
		return
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
		tx.SetBlock(pos, b, nil)
		n++
	}
	o.Printf("%v blocks filled", n)
}
