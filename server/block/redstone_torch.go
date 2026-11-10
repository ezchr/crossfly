package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

var (
	_ world.RedstonePowerSource        = RedstoneTorch{}
	_ world.RedstonePowerContextAction = RedstoneTorch{}
	_ world.ScheduledTicker            = RedstoneTorch{}
)

// RedstoneTorch is a non-solid block that emits light and provides a full-strength redstone signal when lit.
type RedstoneTorch struct {
	transparent
	empty

	// Facing is the direction from the torch to the block it is attached to.
	Facing cube.Face
	// Lit indicates whether the redstone torch is currently lit and emitting power.
	Lit bool
}

func (RedstoneTorch) HasLiquidDrops() bool {
	return true
}

func (t RedstoneTorch) LightEmissionLevel() uint8 {
	if t.Lit {
		return 7
	}
	return 0
}

func (t RedstoneTorch) facing() cube.Face {
	if t.Facing == unknownFace {
		return cube.FaceDown
	}
	return t.Facing
}

func (t RedstoneTorch) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(t)).withBreakHandler(func(pos cube.Pos, tx *world.Tx, _ item.User) {
		tx.Redstone().Torch(pos).ClearBurnout()
	})
}

func (t RedstoneTorch) UseOnBlock(pos cube.Pos, face cube.Face, clickPos mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if world.JavaRedstone() {
		return t.javaUseOnBlock(pos, face, clickPos, tx, user, ctx)
	}
	pos, face, used := firstReplaceable(tx, pos, face, t)
	if !used || face == cube.FaceDown {
		return false
	}
	if _, ok := tx.Block(pos).(world.Liquid); ok {
		return false
	}
	if !tx.Block(pos.Side(face.Opposite())).Model().FaceSolid(pos.Side(face.Opposite()), face, tx) {
		fallbackFace, ok := findTorchPlacementFace(pos, tx)
		if !ok {
			return false
		}
		face = fallbackFace
	}
	t.Facing = face.Opposite()
	t.Lit = !t.attachmentPowered(pos, tx)

	place(tx, pos, t, user, ctx)
	ok := placed(ctx)
	if ok {
		tx.ScheduleBlockUpdate(pos, t, redstoneTicks(1))
	}
	return ok
}

// NeighbourUpdateTick breaks unsupported torches and otherwise schedules inverse-state refreshes.
func (t RedstoneTorch) NeighbourUpdateTick(pos, changed cube.Pos, tx *world.Tx) {
	if world.JavaRedstone() {
		t.javaNeighbourUpdateTick(pos, tx)
		return
	}
	facing := t.facing()
	if !tx.Block(pos.Side(facing)).Model().FaceSolid(pos.Side(facing), facing.Opposite(), tx) {
		tx.Redstone().Torch(pos).ClearBurnout()
		breakBlock(t, pos, tx)
		return
	}
	if burnedOut, _ := tx.Redstone().Torch(pos).BurnoutStatus(); burnedOut {
		t.tryRecover(pos, tx)
		return
	}
	tx.ScheduleBlockUpdate(pos, t, redstoneTicks(1))
}

// ScheduledTick refreshes the lit state after the torch's one-redstone-tick inversion delay.
func (t RedstoneTorch) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if tx == nil {
		return
	}
	if world.JavaRedstone() {
		t.javaScheduledTick(pos, tx)
		return
	}
	var ok bool
	if t, ok = redstoneTorchAt(pos, tx); !ok {
		return
	}
	redstone := tx.Redstone()
	torch := redstone.Torch(pos)
	self := torch.ConsumeSelfTriggered()
	if burnedOut, _ := torch.BurnoutStatus(); burnedOut {
		// A burned-out torch relights once its own output no longer runs back into its input, as Bedrock Edition
		// finds when its strongest input is not the torch itself: the loop through dust is broken, or something
		// else powers the block it hangs on (the torch then stays off for that, and switches again when it goes).
		// This runs a redstone tick after the update that asked for it, when the power the loop held is gone.
		if !t.attachmentPowered(pos, tx) && t.loopsBack(pos, tx) {
			return
		}
		torch.ClearBurnout()
		self = false
	}
	lit := !t.attachmentPowered(pos, tx)
	if t.Lit == lit {
		return
	}
	// Bedrock Edition's torch (RedstoneTorchCapacitor) counts the switches in a row its own output caused, through
	// dust back into the block it hangs on; at the sixteenth it goes out and stays out until the loop is gone.
	if torch.RecordToggle(self) {
		tx.PlaySound(pos.Vec3Centre(), sound.Fizz{})
		if !t.Lit {
			return
		}
		lit = false
	}
	t.Lit = lit
	redstone.SetOutput(pos, t)
}

// redstoneTorchAt returns the live redstone torch at pos. Scheduled tick callers may carry stale block state, so
// mutation paths must reload the world block before writing torch state back.
func redstoneTorchAt(pos cube.Pos, tx *world.Tx) (RedstoneTorch, bool) {
	t, ok := tx.Block(pos).(RedstoneTorch)
	if !ok {
		tx.Redstone().Torch(pos).ClearBurnout()
	}
	return t, ok
}

// RedstonePower emits power from every side except the attached block while lit.
func (t RedstoneTorch) RedstonePower(_ cube.Pos, _ *world.Tx, face cube.Face) int {
	if t.Lit && face != t.facing() {
		return 15
	}
	return 0
}

// RedstoneStrongPower strongly powers the block above the torch while lit.
func (t RedstoneTorch) RedstoneStrongPower(pos cube.Pos, tx *world.Tx, face cube.Face) int {
	if face == cube.FaceUp {
		return t.RedstonePower(pos, tx, face)
	}
	return 0
}

// RedstonePowerActionUpdate schedules torch refreshes and keeps burnout from self-recovering through its own loop.
func (t RedstoneTorch) RedstonePowerActionUpdate(pos cube.Pos, tx *world.Tx, update world.RedstoneUpdate) {
	if tx == nil {
		return
	}
	if world.JavaRedstone() {
		t.check(pos, tx)
		return
	}
	torch := tx.Redstone().Torch(pos)
	self := update.SelfCaused()
	if burnedOut, _ := torch.BurnoutStatus(); burnedOut {
		if !self {
			t.tryRecover(pos, tx)
		}
		return
	}
	if t.Lit == !t.attachmentPowered(pos, tx) {
		return
	}
	if self {
		torch.MarkSelfTriggered()
	}
	tx.ScheduleBlockUpdate(pos, t, redstoneTicks(1))
}

// tryRecover has a burned-out torch check, a redstone tick later, whether it may relight (ScheduledTick).
func (t RedstoneTorch) tryRecover(pos cube.Pos, tx *world.Tx) {
	tx.ScheduleBlockUpdate(pos, t, redstoneTicks(1))
}

// loopsBack reports whether the torch at pos, lit, would power the block it hangs on through redstone dust: the
// loop that burns a torch out. It follows the dust the torch powers (beside it, below it, and beside the block above
// it, which it powers strongly) as far as the signal reaches.
func (t RedstoneTorch) loopsBack(pos cube.Pos, tx *world.Tx) bool {
	attached := pos.Side(t.facing())
	type step struct {
		pos   cube.Pos
		power int
	}
	var queue []step
	seen := map[cube.Pos]int{}
	add := func(p cube.Pos, power int) {
		if power <= 0 || p.OutOfBounds(tx.Range()) {
			return
		}
		if old, ok := seen[p]; ok && old >= power {
			return
		}
		if b, ok := tx.BlockLoaded(p); ok {
			if _, ok := b.(RedstoneWire); ok {
				seen[p] = power
				queue = append(queue, step{p, power})
			}
		}
	}
	for _, face := range cube.Faces() {
		if face != t.facing() && face != cube.FaceUp {
			add(pos.Side(face), 15)
		}
	}
	if above := pos.Side(cube.FaceUp); world.RedstoneFullPowerConductor(above, tx.Block(above), tx) {
		for _, face := range cube.Faces() {
			if face != cube.FaceDown {
				add(above.Side(face), 15)
			}
		}
	}
	src := loadedSource{tx: tx}
	for head := 0; head < len(queue); head++ {
		s := queue[head]
		if s.pos == attached.Side(cube.FaceUp) {
			return true
		}
		for i, face := range cube.HorizontalFaces() {
			if s.pos.Side(face) == attached && redstoneWirePoweredFaces(s.pos, src)[i] {
				return true
			}
		}
		for _, next := range (RedstoneWire{}).RedstoneRelayerNeighbours(s.pos, tx) {
			add(next, s.power-1)
		}
	}
	return false
}

// attachmentPowered reports whether the block the torch is attached to is powered.
func (t RedstoneTorch) attachmentPowered(pos cube.Pos, tx *world.Tx) bool {
	if tx == nil {
		return false
	}
	attached := pos.Side(t.facing())
	attachedBlock := tx.Block(attached)
	if source, ok := attachedBlock.(world.RedstonePowerSource); ok {
		for _, face := range cube.Faces() {
			if world.ClampRedstonePower(source.RedstonePower(attached, tx, face)) > 0 {
				return true
			}
		}
	}
	return world.RedstoneFullPowerConductor(attached, attachedBlock, tx) && tx.RedstoneConductivePower(attached) > 0
}

func (RedstoneTorch) EncodeItem() (name string, meta int16) {
	return "minecraft:redstone_torch", 0
}

func (t RedstoneTorch) EncodeBlock() (name string, properties map[string]any) {
	name = "minecraft:unlit_redstone_torch"
	if t.Lit {
		name = "minecraft:redstone_torch"
	}
	var direction string
	switch t.Facing {
	case cube.FaceDown:
		direction = "top"
	case unknownFace:
		direction = "unknown"
	default:
		direction = t.Facing.String()
	}
	return name, map[string]any{"torch_facing_direction": direction}
}

// redstoneTorchFallbackSides lists the faces to check for placing a redstone torch on a non-solid block.
var redstoneTorchFallbackSides = [...]cube.Face{
	cube.FaceSouth,
	cube.FaceWest,
	cube.FaceNorth,
	cube.FaceEast,
	cube.FaceDown,
}

// findTorchPlacementFace finds a valid face for placing a redstone torch on a non-solid block.
func findTorchPlacementFace(pos cube.Pos, tx *world.Tx) (cube.Face, bool) {
	for _, side := range redstoneTorchFallbackSides {
		if tx.Block(pos.Side(side)).Model().FaceSolid(pos.Side(side), side.Opposite(), tx) {
			return side.Opposite(), true
		}
	}
	return 0, false
}

func allRedstoneTorches() (all []world.Block) {
	for _, face := range cube.Faces() {
		if face == cube.FaceUp {
			face = unknownFace
		}
		all = append(all, RedstoneTorch{Facing: face}, RedstoneTorch{Facing: face, Lit: true})
	}
	return
}
