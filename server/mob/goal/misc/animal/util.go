//go:build crossfly_full

package animal

import (
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// params is implemented by every goal of the package: the parsed JSON
// values (BDS defaults filled in), for tests.
type params interface{ params() map[string]float64 }

// Params returns the parsed parameters of a goal of this package (nil for
// other goals). Tests compare them with the BDS defaults and the JSON.
func Params(g mob.Goal) map[string]float64 {
	if p, ok := g.(params); ok {
		return p.params()
	}
	return nil
}

// debugName is a goal's BDS class name for /mobdebug.
type debugName string

// AppendDebugInfo ...
func (d debugName) AppendDebugInfo(b *strings.Builder) { b.WriteString(string(d)) }

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// forward is the unit horizontal vector m faces (BDS yaw: -sin, cos).
func forward(m *mob.Mob) mgl64.Vec3 {
	r := mgl64.DegToRad(m.Yaw())
	return mgl64.Vec3{-math.Sin(r), 0, math.Cos(r)}
}

// faceTowards turns m's body and head to look at pos at once.
func faceTowards(m *mob.Mob, pos mgl64.Vec3) {
	d := pos.Sub(m.Position())
	yaw := mgl64.RadToDeg(math.Atan2(-d[0], d[2]))
	m.SetYaw(yaw)
	m.SetBodyYaw(yaw)
	m.SetHeadYaw(yaw)
}

// sit shows m sitting (Meta.Sitting) or standing.
func sit(m *mob.Mob, v bool) { m.UpdateMeta(func(meta *mob.Meta) { meta.Sitting = v }) }

// itemEntity returns the item stack of an item entity.
func itemEntity(e world.Entity) (item.Stack, bool) {
	ent, ok := e.(*entity.Ent)
	if !ok {
		return item.Stack{}, false
	}
	b, ok := ent.Behaviour().(*entity.ItemBehaviour)
	if !ok {
		return item.Stack{}, false
	}
	return b.Item(), true
}

// dropAt drops stacks as item entities at pos.
func dropAt(tx *world.Tx, m *mob.Mob, pos mgl64.Vec3, stacks []item.Stack) {
	for _, s := range stacks {
		if s.Empty() {
			continue
		}
		r := m.Rand()
		vel := mgl64.Vec3{r.Float64()*0.2 - 0.1, 0.2, r.Float64()*0.2 - 0.1}
		tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: pos, Velocity: vel}, s))
	}
}

// rollLoot rolls a loot table with m as "this".
func rollLoot(m *mob.Mob, table string) []item.Stack {
	if table == "" {
		return nil
	}
	t, err := core.LoadLootTable(table)
	if err != nil || t == nil {
		return nil
	}
	return t.Roll(&core.LootContext{Mob: m})
}

// blockAt returns the block position of a vec.
func blockAt(v mgl64.Vec3) cube.Pos { return cube.PosFromVec3(v) }

// air reports whether the block at p is air.
func air(tx *world.Tx, p cube.Pos) bool {
	n, _ := tx.Block(p).EncodeBlock()
	return n == "minecraft:air"
}

// thundering reports whether there is a thunderstorm in m's world (BDS
// Weather::isLightning, dimension wide).
func thundering(m *mob.Mob) bool { return m.Tx().Thundering() }
