//go:build crossfly_full

package block

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Dispenser is a nine-slot container that dispenses an item when activated by redstone.
type Dispenser struct {
	solid
	sourceWaterDisplacer

	// Facing is the direction items are dispensed towards.
	Facing cube.Face
	// Dropper selects ordinary item transfer/ejection instead of specialised dispenser item behaviour.
	Dropper bool
	// Triggered is whether the dispenser currently receives redstone power.
	Triggered bool
	// CustomName is the custom name displayed when the dispenser is opened.
	CustomName string

	inventory *inventory.Inventory
	viewerMu  *sync.RWMutex
	viewers   map[ContainerViewer]struct{}
}

var _ world.RedstonePowerContextAction = Dispenser{}

const dispenserDelay = time.Second / 5

// NewDispenser creates an initialised dispenser.
func NewDispenser() Dispenser {
	m := new(sync.RWMutex)
	v := make(map[ContainerViewer]struct{}, 1)
	return Dispenser{
		inventory: inventory.New(9, func(slot int, _, stack item.Stack) {
			m.RLock()
			defer m.RUnlock()
			for viewer := range v {
				viewer.ViewSlotChange(slot, stack)
			}
		}),
		viewerMu: m,
		viewers:  v,
	}
}

// Inventory returns the dispenser inventory.
func (d Dispenser) Inventory(*world.Tx, cube.Pos) *inventory.Inventory { return d.inventory }

// ContainerSize ...
func (Dispenser) ContainerSize() int { return 9 }

// WithName returns the dispenser with a custom name.
func (d Dispenser) WithName(a ...any) world.Item {
	d.CustomName = strings.TrimSuffix(fmt.Sprintln(a...), "\n")
	return d
}

// AddViewer adds a viewer to the dispenser inventory.
func (d Dispenser) AddViewer(v ContainerViewer, _ *world.Tx, _ cube.Pos) {
	d.viewerMu.Lock()
	defer d.viewerMu.Unlock()
	d.viewers[v] = struct{}{}
}

// RemoveViewer removes a viewer from the dispenser inventory.
func (d Dispenser) RemoveViewer(v ContainerViewer, _ *world.Tx, _ cube.Pos) {
	d.viewerMu.Lock()
	defer d.viewerMu.Unlock()
	delete(d.viewers, v)
}

// Activate opens the dispenser inventory.
func (Dispenser) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, _ *item.UseContext) bool {
	if opener, ok := u.(ContainerOpener); ok {
		opener.OpenBlockContainer(pos, tx)
		return true
	}
	return false
}

// RedstonePowerActionUpdate runs when the power reaching the dispenser may have changed. As in Java Edition, the
// dispenser checks its power only when something next to it changes: here, when the power reaching it changed (the
// dust or the source beside it did), and in NeighbourUpdateTick for any other block beside it. A circuit changing
// elsewhere doesn't make it look again, which is what makes a dispenser a block update detector with the power it
// also takes at the block above it (quasi-connectivity).
func (d Dispenser) RedstonePowerActionUpdate(pos cube.Pos, tx *world.Tx, update world.RedstoneUpdate) {

	if update.OldPower == update.NewPower {
		return
	}
	// Placement records the input power but Java does not act on it until a real neighbour changes.
	if update.Cause == world.RedstoneUpdateCauseBlockUpdate && update.HasSource && update.Source == pos {
		return
	}
	d.check(pos, tx, update.NewPower > 0)
}

// NeighbourUpdateTick checks the dispenser's power when a block next to it changes.
func (d Dispenser) NeighbourUpdateTick(pos, changed cube.Pos, tx *world.Tx) {
	if changed == pos {
		return // the update a block gets when it is placed: a Java dispenser doesn't look at its power then
	}
	d.check(pos, tx, tx.RedstoneInputPower(pos) > 0)
}

// check sets the dispenser triggered while it is powered, or the block above it is, and schedules a dispense when it
// first is.
func (d Dispenser) check(pos cube.Pos, tx *world.Tx, powered bool) {
	powered = powered || dispenserQuasiPower(pos.Side(cube.FaceUp), tx)
	if d.Triggered == powered {
		return
	}
	d.Triggered = powered
	// Only the dispenser changes: no neighbour updates (as Java sets it), and the power it holds stays.
	tx.SetBlock(pos, d, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true, KeepRedstonePower: true})
	if !powered {
		return
	}
	// Scheduled block updates are keyed by block state. Queue both states so a short pulse still fires after the
	// delay, while only the state that remains at execution time is run.
	tx.ScheduleBlockUpdate(pos, d, dispenserDelay)
	d.Triggered = false
	tx.ScheduleBlockUpdate(pos, d, dispenserDelay)
}

// ScheduledTick dispenses one item after the activation delay.
func (d Dispenser) ScheduledTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	if !d.dispense(pos, tx, r) {
		tx.PlaySound(pos.Vec3Centre(), sound.ClickFail{})
	}
}

func (d Dispenser) dispense(pos cube.Pos, tx *world.Tx, r *rand.Rand) bool {
	if d.inventory == nil {
		return false
	}
	slots := d.inventory.Slots()
	nonEmpty := make([]int, 0, len(slots))
	for slot, stack := range slots {
		if !stack.Empty() {
			nonEmpty = append(nonEmpty, slot)
		}
	}
	if len(nonEmpty) == 0 {
		return false
	}
	slot := nonEmpty[r.IntN(len(nonEmpty))]
	stack := slots[slot]
	if d.Dropper {
		return d.dropperDispense(slot, stack, pos, tx, r)
	}
	if behaviour, ok := stack.Item().(item.Dispensable); ok {
		ctx := &item.DispenseContext{Rand: r}
		switch behaviour.Dispense(pos, d.Facing, tx, ctx) {
		case item.DispenseSuccess:
			return d.applyDispenseContext(slot, stack, ctx, pos, tx, r)
		case item.DispenseFailure:
			return false
		}
	}
	return d.dropDispensedItem(slot, stack, pos, tx, r)
}

func (d Dispenser) applyDispenseContext(slot int, stack item.Stack, ctx *item.DispenseContext, pos cube.Pos, tx *world.Tx, r *rand.Rand) bool {
	stack = stack.Damage(ctx.Damage).Grow(-ctx.CountSub)
	if ctx.NewItem.Empty() {
		return d.inventory.SetItem(slot, stack) == nil
	}
	if stack.Empty() {
		return d.inventory.SetItem(slot, ctx.NewItem) == nil
	}
	added, err := d.inventory.AddItem(ctx.NewItem)
	if err != nil {
		create := tx.World().EntityRegistry().Config().Item
		if create == nil {
			return false
		}
		remaining := ctx.NewItem.Grow(added - ctx.NewItem.Count())
		tx.AddEntity(create(dispenserDropOpts(pos, d.Facing, r), remaining))
	}
	return d.inventory.SetItem(slot, stack) == nil
}

func (d Dispenser) dropDispensedItem(slot int, stack item.Stack, pos cube.Pos, tx *world.Tx, r *rand.Rand) bool {
	create := tx.World().EntityRegistry().Config().Item
	if create == nil {
		return false
	}

	opts := dispenserDropOpts(pos, d.Facing, r)
	dropped := stack.Grow(1 - stack.Count())
	if err := d.inventory.SetItem(slot, stack.Grow(-1)); err != nil {
		return false
	}
	tx.AddEntity(create(opts, dropped))
	tx.PlaySound(pos.Vec3Centre(), sound.Click{})
	return true
}

// dispenserDirection returns the unit vector pointing out of the front of a dispenser with the face passed. An invalid
// face yields a zero vector.
func dispenserDirection(face cube.Face) mgl64.Vec3 {
	return cube.Pos{}.Side(face).Vec3()
}

// dispenserDropOpts returns the spawn options for an item dropped out of the front of a dispenser.
func dispenserDropOpts(pos cube.Pos, facing cube.Face, r *rand.Rand) world.EntitySpawnOpts {
	direction := dispenserDirection(facing)
	return world.EntitySpawnOpts{
		Position: pos.Vec3Centre().Add(direction.Mul(0.7)),
		Velocity: direction.Mul(0.25).Add(mgl64.Vec3{r.Float64()*0.04 - 0.02, 0.1, r.Float64()*0.04 - 0.02}),
	}
}

// UseOnBlock places the dispenser facing the player.
func (d Dispenser) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, d)
	if !used {
		return false
	}
	dropper := d.Dropper
	d = NewDispenser()
	d.Dropper = dropper
	d.Facing = calculateFace(user, pos)
	place(tx, pos, d, user, ctx)
	return placed(ctx)
}

// BreakInfo returns the dispenser's breaking properties.
func (d Dispenser) BreakInfo() BreakInfo {
	return newBreakInfo(3.5, pickaxeHarvestable, pickaxeEffective, oneOf(Dispenser{Dropper: d.Dropper})).withBlastResistance(17.5).withBreakHandler(func(pos cube.Pos, tx *world.Tx, u item.User) {
		for _, stack := range d.Inventory(tx, pos).Clear() {
			dropItem(tx, stack, pos.Vec3())
		}
	})
}

// DecodeNBT decodes dispenser block-entity data.
func (d Dispenser) DecodeNBT(data map[string]any) any {
	facing, triggered, dropper := d.Facing, d.Triggered, d.Dropper
	d = NewDispenser()
	d.Facing, d.Triggered, d.Dropper = facing, triggered, dropper
	d.CustomName = nbtconv.String(data, "CustomName")
	nbtconv.InvFromNBT(d.inventory, nbtconv.Slice(data, "Items"))
	return d
}

// EncodeNBT encodes dispenser block-entity data.
func (d Dispenser) EncodeNBT() map[string]any {
	if d.inventory == nil {
		facing, triggered, customName, dropper := d.Facing, d.Triggered, d.CustomName, d.Dropper
		d = NewDispenser()
		d.Facing, d.Triggered, d.CustomName, d.Dropper = facing, triggered, customName, dropper
	}
	id := "Dispenser"
	if d.Dropper {
		id = "Dropper"
	}
	m := map[string]any{"Items": nbtconv.InvToNBT(d.inventory), "id": id}
	if d.CustomName != "" {
		m["CustomName"] = d.CustomName
	}
	return m
}

// EncodeBlock encodes the dispenser block state.
func (d Dispenser) EncodeBlock() (string, map[string]any) {
	name := "minecraft:dispenser"
	if d.Dropper {
		name = "minecraft:dropper"
	}
	return name, map[string]any{"facing_direction": int32(d.Facing), "triggered_bit": boolByte(d.Triggered)}
}

// EncodeItem encodes the dispenser item.
func (d Dispenser) EncodeItem() (string, int16) {
	if d.Dropper {
		return "minecraft:dropper", 0
	}
	return "minecraft:dispenser", 0
}

func allDispensers() (blocks []world.Block) {
	for _, f := range cube.Faces() {
		for _, dropper := range []bool{false, true} {
			blocks = append(blocks, Dispenser{Facing: f, Dropper: dropper}, Dispenser{Facing: f, Triggered: true, Dropper: dropper})
		}
	}
	return
}

// dispenserQuasiPower evaluates power at the block above using the source's actual output face, including dust
// orientation. The cached activation power used by ordinary consumers is too broad for this Java QC check.
func dispenserQuasiPower(pos cube.Pos, tx *world.Tx) bool {
	for _, face := range cube.Faces() {
		if pistonSignal(pos.Side(face), face.Opposite(), tx) > 0 {
			return true
		}
	}
	return false
}

// DispenserContainer reports whether the block is a dropper, and its custom name, for the sessions showing its window.
func (d Dispenser) DispenserContainer() (dropper bool, customName string) {
	return d.Dropper, d.CustomName
}
