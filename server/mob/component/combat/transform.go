//go:build crossfly_full

package combat

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Transformation is minecraft:transformation (BDS TransformationComponent):
// after "delay" seconds the mob is replaced by "into" ("id<event>": the new
// mob spawns with that event, then minecraft:entity_transformed fires on it
// with the old mob as "other"). Name tag, persistence, rotation and, with
// preserve_equipment, armour and held items carry over; drop_equipment
// drops them instead.
type Transformation struct {
	into, event       string
	delay             int
	preserveEquipment bool
	dropEquipment     bool
	keepOwner         bool

	left int
	done bool
}

func newTransformation(_ string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		Into              string          `json:"into"`
		Delay             json.RawMessage `json:"delay"`
		PreserveEquipment bool            `json:"preserve_equipment"`
		DropEquipment     bool            `json:"drop_equipment"`
		KeepOwner         bool            `json:"keep_owner"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("minecraft:transformation: %w", err)
	}
	t := &Transformation{preserveEquipment: v.PreserveEquipment, dropEquipment: v.DropEquipment, keepOwner: v.KeepOwner}
	t.into, t.event = SplitIdentifier(v.Into)
	if len(v.Delay) > 0 {
		var d float64
		if v.Delay[0] == '{' {
			var o struct {
				Value float64 `json:"value"`
			}
			if err := json.Unmarshal(v.Delay, &o); err != nil {
				return nil, fmt.Errorf("minecraft:transformation: delay: %w", err)
			}
			d = o.Value
		} else if err := json.Unmarshal(v.Delay, &d); err != nil {
			return nil, fmt.Errorf("minecraft:transformation: delay: %w", err)
		}
		t.delay = int(d * 20)
	}
	t.left = t.delay
	return t, nil
}

// SplitIdentifier splits "minecraft:drowned<minecraft:as_adult>" into the
// identifier and the spawn event.
func SplitIdentifier(s string) (id, event string) {
	if i := strings.IndexByte(s, '<'); i >= 0 && strings.HasSuffix(s, ">") {
		return s[:i], s[i+1 : len(s)-1]
	}
	return s, ""
}

// Attach starts the delay.
func (t *Transformation) Attach(m *mob.Mob) {
	if t.delay > 0 {
		m.UpdateMeta(func(meta *mob.Meta) { meta.Converting = true })
	}
}

// Detach cancels it.
func (t *Transformation) Detach(m *mob.Mob) {
	m.UpdateMeta(func(meta *mob.Meta) { meta.Converting = false })
}

// Left is the number of ticks before the transformation.
func (t *Transformation) Left() int { return t.left }

func (t *Transformation) tick(m *mob.Mob) {
	if t.done || m.Dead() {
		return
	}
	if t.left > 0 {
		t.left--
		return
	}
	t.done = true
	if _, err := Transform(m, t.into, t.event, t.preserveEquipment, t.dropEquipment, t.keepOwner); err != nil {
		slog.Default().Warn("mob: transformation failed", "entity", m.Identifier(), "into", t.into, "err", err)
	}
}

// Transform replaces m by a new mob of id, fired with event (or
// minecraft:entity_transformed when empty).
func Transform(m *mob.Mob, id, event string, preserveEquipment, dropEquipment, keepOwner bool) (*mob.Mob, error) {
	tx := m.Tx()
	if _, ok := mob.TypeFor(id); !ok {
		return nil, fmt.Errorf("no mob type %q", id)
	}
	main, off := m.HeldItems()
	a := m.Armour()
	armour := []item.Stack{a.Helmet(), a.Chestplate(), a.Leggings(), a.Boots()}
	opts := mob.SpawnOptions{Rotation: m.Rotation(), Velocity: m.Velocity(), Event: event}
	if event == "" {
		opts.Event = mob.EventTransformed
	}
	opts.NameTag = m.NameTag()
	owner, persistent := m.Owner(), m.Persistent()
	opts.Configure = func(n *mob.Mob) {
		n.SetPersistent(persistent)
		if keepOwner && owner != nil {
			n.UpdateMeta(func(meta *mob.Meta) { meta.Owner = owner })
		}
		if preserveEquipment {
			n.SetHeldItems(main, off)
			n.Armour().Set(armour[0], armour[1], armour[2], armour[3])
		}
		if d := m.OnFireDuration(); d > 0 {
			n.SetOnFire(d)
		}
	}
	pos := m.Pos()
	n, err := mob.Spawn(tx, id, pos, opts)
	if err != nil {
		return nil, err
	}
	if event != "" {
		ctx := n.Ctx()
		ctx.Other = m
		n.Fire(mob.EventTransformed, ctx)
	}
	if dropEquipment && !preserveEquipment {
		for _, s := range append([]item.Stack{main, off}, armour...) {
			if !s.Empty() {
				tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: pos.Add(mgl64.Vec3{0, 0.5})}, s))
			}
		}
	}
	_ = m.Close()
	return n, nil
}

// BreakDoor is minecraft:annotation.break_door: the mob may break wooden
// doors that block its path. Package mob/nav reads
// m.HasComponent("minecraft:annotation.break_door") to path through closed
// doors; this component breaks the door the mob walks into after break_time
// seconds (BDS BreakDoorGoal, Java BreakDoorGoal: only from min_difficulty).
type BreakDoor struct {
	breakTime     int
	minDifficulty int

	door     cube.Pos
	progress int
	working  bool
}

func newBreakDoor(_ string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		BreakTime     *float64 `json:"break_time"`
		MinDifficulty string   `json:"min_difficulty"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("minecraft:annotation.break_door: %w", err)
	}
	b := &BreakDoor{breakTime: 240, minDifficulty: 3}
	if v.BreakTime != nil {
		b.breakTime = int(*v.BreakTime * 20)
	}
	if d, ok := map[string]int{"peaceful": 0, "easy": 1, "normal": 2, "hard": 3}[v.MinDifficulty]; ok {
		b.minDifficulty = d
	}
	return b, nil
}

// Attach ...
func (*BreakDoor) Attach(*mob.Mob) {}

// Detach ...
func (*BreakDoor) Detach(*mob.Mob) {}

// Progress returns the door being broken and the ticks spent on it.
func (b *BreakDoor) Progress() (cube.Pos, int, bool) { return b.door, b.progress, b.working }

func (b *BreakDoor) tick(m *mob.Mob) {
	tx := m.Tx()
	door, ok := b.blockingDoor(m)
	if !ok || difficultyID(tx) < b.minDifficulty || !MobGriefing(tx) {
		b.working, b.progress = false, 0
		return
	}
	if !b.working || door != b.door {
		b.working, b.door, b.progress = true, door, 0
	}
	b.progress++
	if b.progress%20 == 0 || m.Rand().IntN(20) == 0 {
		tx.PlaySound(door.Vec3Centre(), sound.DoorCrash{})
	}
	if b.progress < b.breakTime {
		return
	}
	d := tx.Block(door).(block.WoodDoor)
	other := door.Side(cube.FaceUp)
	if d.Top {
		other = door.Side(cube.FaceDown)
	}
	tx.SetBlock(door, nil, nil)
	if _, ok := tx.Block(other).(block.WoodDoor); ok {
		tx.SetBlock(other, nil, nil)
	}
	tx.AddParticle(door.Vec3Centre(), particle.BlockBreak{Block: d})
	tx.PlaySound(door.Vec3Centre(), sound.DoorCrash{})
	b.working, b.progress = false, 0
}

// blockingDoor finds a closed wooden door in the block the mob moves into
// (at its feet or head, along its facing) while it is pathing or blocked.
func (b *BreakDoor) blockingDoor(m *mob.Mob) (cube.Pos, bool) {
	if m.Navigation().Done() && !m.Physics().HorizontalCollision {
		return cube.Pos{}, false
	}
	tx := m.Tx()
	dir := mgl64.Vec3{-sin(m.Yaw()), 0, cos(m.Yaw())}
	base := m.Pos().Add(dir.Mul(0.6 + m.BBox().Width()/2))
	for _, dy := range []float64{0.2, 1.2} {
		p := cube.PosFromVec3(base.Add(mgl64.Vec3{0, dy}))
		if d, ok := tx.Block(p).(block.WoodDoor); ok && !d.Open {
			return p, true
		}
	}
	return cube.Pos{}, false
}

func difficultyID(tx *world.Tx) int {
	id, ok := world.DifficultyID(tx.World().Difficulty())
	if !ok {
		return 2
	}
	return id
}

func sin(deg float64) float64 { return math.Sin(deg * math.Pi / 180) }
func cos(deg float64) float64 { return math.Cos(deg * math.Pi / 180) }
