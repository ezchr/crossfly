//go:build crossfly_full

package misc

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Trail is minecraft:trail (BDS TrailDescription, TrailComponent,
// TrailSystem::_tickComponent): every tick the mob leaves block_type in the
// air blocks under its four corners (its position +-0.25 on x and z, floored)
// that may hold it, when mobGriefing is on and spawn_filter passes. The snow
// golem leaves snow layers where the temperature is below 0.81.
//
//	block_type     the block placed (default air: nothing)
//	spawn_offset   offset of the four blocks, turned with the mob's facing:
//	               x sideways, z ahead (BDS swaps and flips it by the larger
//	               of the forward x and z)
//	spawn_filter   filter on the mob (BDS ActorFilterGroup::evaluateActor)
//
// Nothing happens for a zero offset and a solid block (it would fill the
// mob's own space). A block that may not stand there (a snow layer on ice,
// on another layer, on a slab top...) is skipped, as BlockLegacy::mayPlace
// does.
type Trail struct {
	name   string
	offset mgl64.Vec3
	filter mob.Filter

	b        world.Block
	resolved bool
}

func newTrail(_ string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		Block  string          `json:"block_type"`
		Offset []float64       `json:"spawn_offset"`
		Filter json.RawMessage `json:"spawn_filter"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	t := &Trail{name: v.Block}
	if t.name == "" {
		t.name = "minecraft:air"
	}
	if len(v.Offset) != 0 {
		if len(v.Offset) != 3 {
			return nil, fmt.Errorf("spawn_offset: want 3 numbers, got %v", v.Offset)
		}
		t.offset = mgl64.Vec3{v.Offset[0], v.Offset[1], v.Offset[2]}
	}
	f, err := mob.CompileFilter(v.Filter)
	if err != nil {
		return nil, err
	}
	t.filter = f
	return t, nil
}

// Attach ...
func (*Trail) Attach(*mob.Mob) {}

// Detach ...
func (*Trail) Detach(*mob.Mob) {}

// TickStage ...
func (*Trail) TickStage() mob.Stage { return mob.StageEnvironment }

// block resolves block_type once the block registry is finalised.
func (t *Trail) block() world.Block {
	if !t.resolved {
		t.resolved = true
		props, _ := world.DefaultBlockRegistry.DefaultProperties(t.name)
		if b, ok := world.BlockByName(t.name, props); ok {
			if _, air := b.(block.Air); !air {
				t.b = b
			}
		}
	}
	return t.b
}

// Tick ...
func (t *Trail) Tick(m *mob.Mob) {
	b := t.block()
	if b == nil || t.offset == (mgl64.Vec3{}) && solid(b) {
		return
	}
	tx := m.Tx()
	if !combat.MobGriefing(tx) || !t.filter.Test(m.Ctx()) {
		return
	}
	for _, pos := range trailPositions(m.Position(), m.Yaw(), t.offset) {
		if _, ok := tx.Block(pos).(block.Air); !ok {
			continue
		}
		if s, ok := b.(interface {
			CanSurvive(cube.Pos, *world.Tx) bool
		}); ok && !s.CanSurvive(pos, tx) {
			continue
		}
		tx.SetBlock(pos, b, nil)
	}
}

// trailPositions are the four blocks of TrailSystem::_tickComponent: the
// floored corners pos +-0.25 on x and z, at floor(y), with offset turned
// by the facing.
func trailPositions(pos mgl64.Vec3, yaw float64, off mgl64.Vec3) [4]cube.Pos {
	r := mgl64.DegToRad(yaw)
	fx, fz := -math.Sin(r), math.Cos(r)
	var out [4]cube.Pos
	for i := 0; i < 4; i++ {
		x := math.Floor(float64(2*(i&1)-1)*0.25 + pos[0])
		z := math.Floor(float64(2*((i>>1)&1)-1)*0.25 + pos[2])
		y := math.Floor(pos[1]) + off[1]
		switch {
		case math.Abs(fx) <= math.Abs(fz) && fz <= 0:
			x, z = x+off[0], z-off[2]
		case math.Abs(fx) <= math.Abs(fz):
			x, z = x-off[0], z+off[2]
		case fx <= 0:
			x, z = x-off[2], z-off[0]
		default:
			x, z = x+off[2], z+off[0]
		}
		out[i] = cube.Pos{int(math.Floor(x)), int(math.Floor(y)), int(math.Floor(z))}
	}
	return out
}

// solid reports whether b fills its whole block (BlockLegacy::isSolid).
func solid(b world.Block) bool {
	_, ok := b.Model().(model.Solid)
	return ok
}
