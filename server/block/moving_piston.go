//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// MovingPiston stores the displaced state independently of the Bedrock moving_block runtime state.
type MovingPiston struct {
	transparent
	Moving            world.Block
	Piston            cube.Pos
	Facing            cube.Face
	Extending, Source bool
	// Waterlogged is true if the moved block held water. Java keeps it only if the move is cut short (the
	// piston retracts and drops the block); a move that completes places the block dry.
	Waterlogged            bool
	Started                int64
	Progress, LastProgress float64
}

func (MovingPiston) EncodeBlock() (string, map[string]any) { return "minecraft:moving_block", nil }

// FreshBlockEntity ...: every moving block is a new block entity, as in Java.
func (MovingPiston) FreshBlockEntity() bool    { return true }
func (m MovingPiston) Model() world.BlockModel { return movingPistonModel{m} }
func (m MovingPiston) Tick(tick int64, pos cube.Pos, tx *world.Tx) {
	if m.Moving == nil {
		tx.SetBlock(pos, nil, nil)
		return
	}
	if tick-m.Started >= 2 {
		m.finish(pos, tx, false)
		return
	}
	next := min(1, float64(tick-m.Started+1)*.5)
	m.LastProgress = m.Progress
	delta := next - m.Progress
	if delta > 0 {
		m.pushEntities(pos, tx, delta)
	}
	m.Progress = next
	// Java clients advance the animation locally; this update preserves authoritative saved progress without
	// repeatedly replacing the client block entity and restarting its animation.
	tx.SetBlockEntity(pos, m)
}

// finish places the moved block. dropped is Java's finalTick (the piston moved on before the block arrived),
// which keeps a waterlogged block's water; otherwise the block is placed dry, as Java's tick does.
func (m MovingPiston) finish(pos cube.Pos, tx *world.Tx, dropped bool) {
	if current, ok := tx.Block(pos).(MovingPiston); !ok || current.Piston != m.Piston {
		return
	}
	b := m.Moving
	if b == nil {
		b = Air{}
	}
	tx.SetBlock(pos, b, nil)
	if m.Waterlogged && dropped {
		tx.SetLiquid(pos, Water{Depth: 8, Still: true})
	}
	// Java places a moved block through Block.updateFromNeighbourShapes, which makes an observer that arrives
	// unpowered fire: what drives flying machines.
	ObserverSetblockPulse(pos, tx)
}
func (m MovingPiston) pushEntities(pos cube.Pos, tx *world.Tx, delta float64) {
	direction := m.Facing
	if !m.Extending {
		direction = direction.Opposite()
	}
	step := cube.Pos{}.Side(direction)
	axis := 0
	if direction.Axis() == cube.Y {
		axis = 1
	} else if direction.Axis() == cube.Z {
		axis = 2
	}
	offsets := make(map[*world.EntityHandle]float64)
	entities := make(map[*world.EntityHandle]world.Entity)
	for _, box := range (movingPistonModel{m}).BBox(pos, tx) {
		swept := box.Translate(pos.Vec3()).Extend(step.Vec3().Mul(delta))
		for e := range tx.EntitiesWithin(swept) {
			bounds := e.H().Type().BBox(e).Translate(e.Position())
			if !bounds.IntersectsWith(swept) {
				continue
			}
			amount := bounds.Max()[axis] - swept.Min()[axis]
			if step[axis] > 0 {
				amount = swept.Max()[axis] - bounds.Min()[axis]
			}
			if amount <= 0 {
				continue
			}
			offsets[e.H()] = max(offsets[e.H()], min(delta, amount)+.01)
			entities[e.H()] = e
		}
	}
	name, _ := m.Moving.EncodeBlock()
	if name == "minecraft:honey_block" && direction.Axis() != cube.Y {
		offset := m.Progress - 1
		if !m.Extending {
			offset = 1 - m.Progress
		}
		top := cube.Box(0, 15.0/16, 0, 1, 1.500001, 1).Translate(pos.Vec3()).TranslateTowards(m.Facing, offset)
		for e := range tx.EntitiesWithin(top) {
			ground, ok := e.(interface{ OnGround() bool })
			if !ok || !ground.OnGround() {
				continue
			}
			p := e.Position()
			if p[0] < top.Min()[0] || p[0] > top.Max()[0] || p[2] < top.Min()[2] || p[2] > top.Max()[2] {
				continue
			}
			offsets[e.H()] = max(offsets[e.H()], delta)
			entities[e.H()] = e
		}
	}
	for id, amount := range offsets {
		e := entities[id]
		move := step.Vec3().Mul(amount)
		move = tx.LimitPistonDisplacement(e, move)
		move = pistonClipMovement(e.H().Type().BBox(e).Translate(e.Position()), move, tx, m.Piston)

		if move.LenSqr() > 0 {
			if mover, ok := e.(interface{ MoveByPiston(mgl64.Vec3) }); ok {
				mover.MoveByPiston(move)
			} else if mover, ok := e.(interface{ Teleport(mgl64.Vec3) }); ok {
				mover.Teleport(e.Position().Add(move))
			}
		}
		if _, slime := m.Moving.(Slime); slime {
			if v, ok := e.(velocityEntity); ok {
				vel := v.Velocity()
				vel[axis] = float64(step[axis])
				v.SetVelocity(vel)
			}
		}
	}
}
func (m MovingPiston) EncodeNBT() map[string]any {
	b := m.Moving
	if b == nil {
		b = Air{}
	}
	out := map[string]any{"id": "MovingBlock", "movingBlock": nbtconv.WriteBlock(b), "movingBlockExtra": nbtconv.WriteBlock(Air{}), "pistonPosX": int32(m.Piston[0]), "pistonPosY": int32(m.Piston[1]), "pistonPosZ": int32(m.Piston[2]), "expanding": boolByte(m.Extending), "dfFacing": int32(m.Facing), "dfSource": boolByte(m.Source), "dfStarted": m.Started, "dfProgress": float32(m.Progress), "dfLastProgress": float32(m.LastProgress), "dfWaterlogged": boolByte(m.Waterlogged)}
	if data, ok := b.(world.NBTer); ok {
		out["movingEntity"] = data.EncodeNBT()
	}
	return out
}
func (m MovingPiston) DecodeNBT(data map[string]any) any {
	m.Moving = nbtconv.Block(data, "movingBlock")
	m.Piston = cube.Pos{int(nbtconv.Int32(data, "pistonPosX")), int(nbtconv.Int32(data, "pistonPosY")), int(nbtconv.Int32(data, "pistonPosZ"))}
	m.Extending = nbtconv.Bool(data, "expanding")
	m.Facing = cube.Face(nbtconv.Int32(data, "dfFacing"))
	m.Source = nbtconv.Bool(data, "dfSource")
	m.Started = nbtconv.Int64(data, "dfStarted")
	m.Progress = float64(nbtconv.Float32(data, "dfProgress"))
	m.LastProgress = float64(nbtconv.Float32(data, "dfLastProgress"))
	m.Waterlogged = nbtconv.Bool(data, "dfWaterlogged")
	if n, ok := m.Moving.(world.NBTer); ok {
		if entity, ok := data["movingEntity"].(map[string]any); ok {
			m.Moving = n.DecodeNBT(entity).(world.Block)
		}
	}
	return m
}

type movingPistonModel struct{ moving MovingPiston }

func (m movingPistonModel) BBox(pos cube.Pos, src world.BlockSource) []cube.BBox {
	b := m.moving.Moving
	if b == nil {
		return nil
	}
	direction := m.moving.Facing
	offset := m.moving.Progress - 1
	if !m.moving.Extending {
		offset = 1 - m.moving.Progress
	}
	boxes := b.Model().BBox(pos, src)
	out := make([]cube.BBox, len(boxes))
	for i, box := range boxes {
		out[i] = box.TranslateTowards(direction, offset)
	}
	return out
}

// FaceSolid is Javas isFaceSturdy of a moving block: its shape is the moved blocks, offset by how far it still has
// to go, so it is sturdy only once it has arrived (progress 1) and the moved block is. A fan landing beside glass that
// is still moving survives on it.
func (m movingPistonModel) FaceSolid(pos cube.Pos, face cube.Face, src world.BlockSource) bool {
	if m.moving.Moving == nil || m.moving.Progress < 1 || m.moving.Source {
		return false
	}
	return m.moving.Moving.Model().FaceSolid(pos, face, src)
}
