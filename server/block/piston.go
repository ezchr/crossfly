//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Piston uses Java movement rules while retaining Bedrock's PistonArm block entity for rendering.
// Facing is always the actual extension direction; only EncodeBlock translates Bedrock's reversed horizontal axis.
type Piston struct {
	solid
	sourceWaterDisplacer
	Facing           cube.Face
	Sticky, Extended bool
	// State is Bedrock's phase: 0 retracted, 1 extending, 2 extended, 3 retracting.
	State                  byte
	Progress, LastProgress float64
	Started                int64
	Attached               []cube.Pos
}

func pistonBedrockFace(f cube.Face) cube.Face {
	if f.Axis() != cube.Y {
		return f.Opposite()
	}
	return f
}
func (p Piston) EncodeBlock() (string, map[string]any) {
	n := "minecraft:piston"
	if p.Sticky {
		n = "minecraft:sticky_piston"
	}
	return n, map[string]any{"facing_direction": int32(pistonBedrockFace(p.Facing))}
}
func (p Piston) EncodeItem() (string, int16) { n, _ := p.EncodeBlock(); return n, 0 }
func (p Piston) BreakInfo() BreakInfo {
	return newBreakInfo(1.5, alwaysHarvestable, pickaxeEffective, oneOf(Piston{Sticky: p.Sticky}))
}
func (p Piston) Model() world.BlockModel {
	if p.State == 3 {
		return retractingPistonModel{Facing: p.Facing, Progress: p.Progress}
	}
	return pistonModel{Facing: p.Facing, Extended: p.Extended}
}
func (p Piston) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, p)
	if !used {
		return false
	}
	p.Facing = calculateFace(user, pos)
	p.Extended = false
	p.State = 0
	place(tx, pos, p, user, ctx)
	return placed(ctx)
}
func (p Piston) BlockPlaced(pos cube.Pos, previous world.Block, tx *world.Tx) {
	if old, ok := previous.(Piston); !ok || old.Sticky != p.Sticky {
		p.check(pos, tx)
	}
}
func (p Piston) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) { p.check(pos, tx) }
func (p Piston) RedstonePowerActionUpdate(pos cube.Pos, tx *world.Tx, u world.RedstoneUpdate) {
	if u.OldPower == u.NewPower {
		return
	}
	if u.Cause == world.RedstoneUpdateCauseBlockUpdate && u.HasChangedNeighbour && pistonManhattan(u.ChangedNeighbour, pos) <= 1 {
		// The block that changed is the piston or beside it, so the piston was updated as its neighbour already, which
		// is all a Java piston reacts to (neighborChanged). Looking again once the redstone engine catches up would
		// let it push a line whose other blocks were still moving when the update came, as in TNT duplicators.
		return
	}
	p.check(pos, tx)
}

func pistonManhattan(a, b cube.Pos) int {
	d := 0
	for i := range a {
		if a[i] > b[i] {
			d += a[i] - b[i]
		} else {
			d += b[i] - a[i]
		}
	}
	return d
}

// powered implements PistonBaseBlock.getNeighborSignal, including the explicit below check and Java QC.
func (p Piston) powered(pos cube.Pos, tx *world.Tx) bool {
	for _, f := range cube.Faces() {
		if f != p.Facing && pistonSignal(pos.Side(f), f.Opposite(), tx) > 0 {
			return true
		}
	}
	if pistonSignal(pos, cube.FaceUp, tx) > 0 {
		return true
	}
	above := pos.Side(cube.FaceUp)
	for _, f := range cube.Faces() {
		if f != cube.FaceDown && pistonSignal(above.Side(f), f.Opposite(), tx) > 0 {
			return true
		}
	}
	return false
}

// pistonSignal reads a source in the direction it really emits, including dust orientation.
func pistonSignal(pos cube.Pos, towards cube.Face, tx *world.Tx) int {
	b := tx.Block(pos)
	power := 0
	if source, ok := b.(world.RedstonePowerSource); ok {
		power = source.RedstonePower(pos, tx, towards)
	}
	if world.RedstoneFullPowerConductor(pos, b, tx) {
		power = max(power, tx.RedstoneConductivePower(pos))
	}
	return power
}
func (p Piston) check(pos cube.Pos, tx *world.Tx) {
	if p.State == 3 {
		return
	}
	on := p.powered(pos, tx)
	if on && !p.Extended {
		if _, ok := resolvePiston(tx, pos, p.Facing, true); ok {
			tx.QueueBlockEvent(pos, p, 0)
		}
	} else if !on && p.Extended {
		event := 1
		if m, ok := tx.Block(pos.Side(p.Facing).Side(p.Facing)).(MovingPiston); ok && m.Extending && m.Facing == p.Facing && tx.CurrentTick()-m.Started < 2 {
			event = 2
		}
		tx.QueueBlockEvent(pos, p, event)
	}
}
func (p Piston) BlockEvent(pos cube.Pos, tx *world.Tx, event int) {
	on := p.powered(pos, tx)
	if event == 0 {
		if !on || p.Extended || p.State == 3 {
			return
		}
		r, ok := resolvePiston(tx, pos, p.Facing, true)
		if !ok {
			return
		}
		p.Extended = true
		p.State = 1
		p.Progress = 0
		p.LastProgress = 0
		p.Started = tx.CurrentTick()
		p.Attached = append([]cube.Pos(nil), r.push...)
		p.move(pos, tx, r, true)
		tx.SetBlock(pos.Side(p.Facing), MovingPiston{Moving: PistonHead{Facing: p.Facing, Sticky: p.Sticky}, Piston: pos, Facing: p.Facing, Extending: true, Source: true, Started: tx.CurrentTick()}, nil)
		// Java's moveBlocks runs all its updates (the head's last) before the base is marked extended, and that
		// setBlock updates the base's neighbours but not the base: a base the move took the power from stays out
		// until something updates it.
		tx.FlushNeighbourUpdates()
		tx.SetBlock(pos, p, &world.SetOpts{DisableBlockUpdates: true})
		for _, f := range cube.Faces() {
			tx.NotifyNeighbour(pos.Side(f), pos)
		}
		tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: "tile.piston.out", Volume: .5, Pitch: .7})
		return
	}
	if on || !p.Extended {
		return
	}
	front := pos.Side(p.Facing)
	if m, ok := tx.Block(front).(MovingPiston); ok {
		m.finish(front, tx, true)
	}
	p.Extended = false
	p.State = 3
	p.Progress = 1
	p.LastProgress = 1
	p.Started = tx.CurrentTick()
	p.Attached = nil
	tx.SetBlock(pos, p, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true})
	tx.SetBlock(front, nil, nil)
	two := front.Side(p.Facing)
	dropped := false
	if m, ok := tx.Block(two).(MovingPiston); p.Sticky && ok && m.Extending && m.Facing == p.Facing {
		m.finish(two, tx, true)
		dropped = true
	}
	if p.Sticky && event == 1 && !dropped {
		if r, ok := resolvePiston(tx, pos, p.Facing, false); ok {
			p.Attached = append([]cube.Pos(nil), r.push...)
			p.move(pos, tx, r, false)
		}
	}
	tx.SetBlock(pos, p, nil)
	tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: "tile.piston.in", Volume: .5, Pitch: .7})
}

func (p Piston) move(pos cube.Pos, tx *world.Tx, r *pistonResolver, extending bool) {
	blocks := make([]world.Block, len(r.push))
	waterlogged := make([]bool, len(r.push))
	for i, at := range r.push {
		blocks[i] = tx.Block(at)
		tx.CancelBlockUpdate(at, blocks[i])
		if l, ok := tx.Liquid(at); ok {
			if w, ok := l.(Water); ok && w.Depth == 8 && !w.Falling {
				if _, isWater := blocks[i].(Water); !isWater {
					waterlogged[i] = true
				}
			}
		}
	}
	for i := len(r.destroy) - 1; i >= 0; i-- {
		at := r.destroy[i]
		breakBlock(tx.Block(at), at, tx)
	}
	if !tx.World().AllowPistonDuplication() {
		// Breaking the blocks in the way may have changed the blocks being pushed (a TNT ignited by the update): move
		// them as they are now, as Paper does, instead of as they were before (vanilla, which duplicates them).
		for i, at := range r.push {
			blocks[i] = tx.Block(at)
		}
	}
	destinations := make(map[cube.Pos]bool, len(r.push))
	for i := len(r.push) - 1; i >= 0; i-- {
		target := r.push[i].Side(r.direction)
		destinations[target] = true
		moving := MovingPiston{Moving: blocks[i], Piston: pos, Facing: p.Facing, Extending: extending, Waterlogged: waterlogged[i], Started: tx.CurrentTick()}
		// Placing the moving block makes shape updates, which can destroy a block at once (a coral fan on the block
		// that moved) and so prime TNT in the line before it moves: vanilla moves the block as it was.
		tx.SetBlock(target, moving, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true})
		if !tx.World().AllowPistonDuplication() {
			// Paper moves the block as it is after that, and clears where it was at once, so that nothing later in
			// the move (another TNT chain) finds it there again.
			at := r.push[i]
			if m, ok := tx.Block(at).(MovingPiston); !ok || m.Piston != pos {
				if now := tx.Block(at); world.BlockRuntimeID(now) != world.BlockRuntimeID(blocks[i]) {
					blocks[i] = now
					if _, air := now.(Air); air {
						moving.Moving = nil
					} else {
						moving.Moving = now
					}
					if m, ok := tx.Block(target).(MovingPiston); ok && m.Piston == pos {
						tx.SetBlock(target, moving, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true, DisableShapeUpdates: true})
					}
				}
				if !destinations[at] {
					tx.SetBlock(at, nil, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true, DisableShapeUpdates: true})
				}
			}
		}
	}
	for i, at := range r.push {
		if !destinations[at] {
			tx.SetBlock(at, nil, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true})
		}
		if waterlogged[i] {
			// The water moves with the block (Java sets the old position to air).
			if _, ok := tx.Liquid(at); ok && !destinations[at] {
				tx.SetLiquid(at, nil)
			}
		}
	}
	for i := len(r.push) - 1; i >= 0; i-- {
		pistonNotify(r.push[i], tx)
		pistonNotify(r.push[i].Side(r.direction), tx)
	}
}
func pistonNotify(pos cube.Pos, tx *world.Tx) {
	tx.Redstone().ScheduleUpdate(pos)
	for _, f := range cube.Faces() {
		tx.NotifyNeighbour(pos.Side(f), pos)
	}
}
func (p Piston) Tick(tick int64, pos cube.Pos, tx *world.Tx) {
	if p.State != 1 && p.State != 3 {
		return
	}

	if p.State == 3 && tick-p.Started < 2 {
		moving := MovingPiston{Moving: PistonHead{Facing: p.Facing, Sticky: p.Sticky}, Piston: pos, Facing: p.Facing, Progress: 1 - p.Progress, Source: true}
		moving.pushEntities(pos, tx, min(.5, p.Progress))
	}
	p.LastProgress = p.Progress
	progress := min(1, float64(tick-p.Started+1)*.5)
	if p.State == 3 {
		p.Progress = 1 - progress
	} else {
		p.Progress = progress
	}
	if tick-p.Started >= 2 {
		if p.State == 1 {
			p.State = 2
		} else {
			p.State = 0
		}
		p.Attached = nil
		tx.SetBlock(pos, p, &world.SetOpts{DisableBlockUpdates: true})
		p.check(pos, tx)
	} else {
		tx.SetBlock(pos, p, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true, KeepRedstonePower: true})
	}
}

func (p Piston) EncodeNBT() map[string]any {
	attached := make([]any, 0, len(p.Attached)*3)
	for _, at := range p.Attached {
		for _, v := range at {
			attached = append(attached, int32(v))
		}
	}
	return map[string]any{"id": "PistonArm", "Progress": float32(p.Progress), "LastProgress": float32(p.LastProgress), "State": p.State, "NewState": p.State, "Sticky": boolByte(p.Sticky), "AttachedBlocks": attached, "BreakBlocks": []any{}, "dfStarted": p.Started, "dfExtended": boolByte(p.Extended)}
}
func (p Piston) DecodeNBT(m map[string]any) any {
	p.State = nbtconv.Uint8(m, "State")
	p.Progress = float64(nbtconv.Float32(m, "Progress"))
	p.LastProgress = float64(nbtconv.Float32(m, "LastProgress"))
	p.Started = nbtconv.Int64(m, "dfStarted")
	p.Extended = p.State == 1 || p.State == 2

	p.Attached = nil
	list := nbtconv.Slice(m, "AttachedBlocks")
	if ints, ok := m["AttachedBlocks"].([]int32); ok {
		for _, n := range ints {
			list = append(list, n)
		}
	}
	for i := 0; i+2 < len(list); i += 3 {
		x, xok := list[i].(int32)
		y, yok := list[i+1].(int32)
		z, zok := list[i+2].(int32)
		if xok && yok && zok {
			p.Attached = append(p.Attached, cube.Pos{int(x), int(y), int(z)})
		}
	}
	return p
}
func allPistons() (blocks []world.Block) {
	for _, f := range cube.Faces() {
		for _, sticky := range []bool{false, true} {
			blocks = append(blocks, Piston{Facing: f, Sticky: sticky})
		}
	}
	return
}
