//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"math"
	"math/rand/v2"
	"time"
)

// Target produces a face-accuracy-dependent weak signal for 20 game ticks for arrows or eight for other projectiles.
// Bedrock's vanilla target has no power block state. Its server state is saved separately and only Java receives
// the power property; both clients continue to use the vanilla target model without a resource pack.
type Target struct {
	solid
	sourceWaterDisplacer
	Power   int
	Expires int64
}

func (Target) EncodeBlock() (string, map[string]any) { return "minecraft:target", nil }
func (Target) EncodeItem() (string, int16)           { return "minecraft:target", 0 }
func (Target) BreakInfo() BreakInfo {
	return newBreakInfo(.5, alwaysHarvestable, hoeEffective, oneOf(Target{}))
}
func (t Target) BlockStateKey() uint64                            { return uint64(t.Power) }
func (t Target) RedstonePower(cube.Pos, *world.Tx, cube.Face) int { return t.Power }
func (t Target) JavaState() int32                                 { return targetJavaStates[world.ClampRedstonePower(t.Power)] }
func (Target) JavaBlockEntity() (string, []byte)                  { return "", nil }
func (Target) EncodeNetworkNBT() map[string]any                   { return nil }
func (t Target) EncodeNBT() map[string]any {
	return map[string]any{"id": "DragonflyTarget", "Power": int32(t.Power), "Expires": t.Expires}
}
func (t Target) DecodeNBT(data map[string]any) any {
	t.Power = world.ClampRedstonePower(int(nbtconv.Int32(data, "Power")))
	t.Expires = nbtconv.Int64(data, "Expires")
	return t
}
func (t Target) BlockPlaced(pos cube.Pos, previous world.Block, tx *world.Tx) {
	if _, same := previous.(Target); !same && t.Power > 0 && !tx.BlockUpdateScheduled(pos, t) {
		t.Power = 0
		t.Expires = 0
		tx.SetBlock(pos, t, &world.SetOpts{DisableBlockUpdates: true})
	}
}
func (t Target) ProjectileHitAt(pos cube.Pos, tx *world.Tx, projectile world.Entity, face cube.Face, hit mgl64.Vec3) {
	if tx.BlockUpdateScheduled(pos, t) {
		return
	}
	ticks := 8
	switch projectile.H().Type().EncodeEntity() {
	case "minecraft:arrow", "minecraft:spectral_arrow":
		ticks = 20
	}
	t.Power = targetStrength(face, hit)
	t.Expires = tx.CurrentTick() + int64(ticks)
	tx.SetBlock(pos, t, nil)
	tx.ScheduleBlockUpdate(pos, t, time.Duration(ticks)*time.Second/20)
}
func targetStrength(face cube.Face, hit mgl64.Vec3) int {
	dist := [3]float64{}
	for i, v := range hit {
		dist[i] = math.Abs(v - math.Floor(v) - .5)
	}
	distance := max(dist[1], dist[2])
	if face.Axis() == cube.Y {
		distance = max(dist[0], dist[2])
	} else if face.Axis() == cube.Z {
		distance = max(dist[0], dist[1])
	}
	return max(1, int(math.Ceil(15*min(1, max(0, (.5-distance)/.5)))))
}
func (t Target) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if t.Power > 0 {
		t.Power = 0
		t.Expires = 0
		tx.SetBlock(pos, t, nil)
	}
}
func (t Target) Tick(tick int64, pos cube.Pos, tx *world.Tx) {
	// Scheduled ticks are transient; a saved powered target still expires correctly after a chunk/world reload.
	if t.Power > 0 && tick >= t.Expires {
		t.ScheduledTick(pos, tx, nil)
	}
}
