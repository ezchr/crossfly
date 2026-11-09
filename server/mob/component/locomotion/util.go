//go:build crossfly_full

package locomotion

import (
	"bytes"
	"encoding/json"
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func decode(raw json.RawMessage, v any) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	return json.Unmarshal(raw, v)
}

func norm(v mgl64.Vec3) mgl64.Vec3 {
	l := v.Len()
	if l < 1e-4 {
		return mgl64.Vec3{}
	}
	return v.Mul(1 / l)
}

func inRange(tx *world.Tx, pos cube.Pos) bool {
	r := tx.Range()
	return pos[1] >= r[0] && pos[1] <= r[1]
}

func waterAt(tx *world.Tx, pos cube.Pos) bool {
	if !inRange(tx, pos) {
		return false
	}
	l, ok := tx.Liquid(pos)
	if !ok {
		return false
	}
	_, w := l.(block.Water)
	return w
}

func airAt(tx *world.Tx, pos cube.Pos) bool {
	if !inRange(tx, pos) {
		return pos[1] > tx.Range()[1]
	}
	if _, ok := tx.Block(pos).(block.Air); !ok {
		return false
	}
	_, liquid := tx.Liquid(pos)
	return !liquid
}

// solidAt reports whether the block at pos has a collision box (liquids do
// not).
func solidAt(tx *world.Tx, pos cube.Pos) bool {
	if !inRange(tx, pos) {
		return false
	}
	b := tx.Block(pos)
	switch b.(type) {
	case block.Air, world.Liquid:
		return false
	}
	return len(b.Model().BBox(pos, tx)) > 0
}

// aboveTopSolid is BlockSource::getAboveTopSolidBlock.
func aboveTopSolid(tx *world.Tx, x, z int) int {
	y := int(tx.HighestBlock(x, z))
	for y >= tx.Range()[0] && !solidAt(tx, cube.Pos{x, y, z}) {
		y--
	}
	return y + 1
}

// clip reports whether the segment hits a block with a collision box.
func clip(tx *world.Tx, from, to mgl64.Vec3) bool {
	hit := false
	trace.TraverseBlocks(from, to, func(pos cube.Pos) bool {
		if !inRange(tx, pos) {
			return true
		}
		b := tx.Block(pos)
		if _, ok := b.(block.Air); ok {
			return true
		}
		if _, ok := trace.BlockIntercept(pos, tx, b, from, to); ok {
			hit = true
			return false
		}
		return true
	})
	return hit
}

// blockFriction is the friction factor of the block under m's feet (block
// friction x 0.91, 0.546 over air), as travel uses it on the ground.
func blockFriction(m *mob.Mob) float64 {
	tx := m.Tx()
	pos := m.Position()
	b := tx.Block(cube.Pos{int(math.Floor(pos[0])), int(math.Floor(m.BBox().Min()[1] - 0.1)), int(math.Floor(pos[2]))})
	if _, air := b.(block.Air); air {
		return 0.546
	}
	f := 0.6
	if fr, ok := b.(interface{ Friction() float64 }); ok {
		f = fr.Friction()
	}
	if f <= 0 {
		return 0.546
	}
	return f * 0.91
}

// nop is a component without run-time effect.
type nop struct{}

func (nop) Attach(*mob.Mob) {}
func (nop) Detach(*mob.Mob) {}
