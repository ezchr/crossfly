//go:build crossfly_full

package combat

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Transformation is minecraft:transformation (BDS TransformationComponent):
// after the delay the mob is replaced by "into" ("id<event>": the new mob
// gets that event, then minecraft:entity_transformed, both with the old mob
// as "other").
//
// The delay (TransformationComponent::reloadComponent, on attach) is
// delay.value plus range_min plus a random [0, range_max-range_min) ticks,
// so a zombie villager cure (value 100, range 80..200) takes 180-300 s. Each
// tick (tickTransformationComponent) counts down by one, and with chance
// block_assist_chance by one more for every block_types block within
// block_radius that passes block_chance (at most block_max blocks looked at
// with a match). begin_transform_sound plays on attach.
//
// A transformation that is due when the mob dies still happens (the
// villager_v2 "become_zombie" group is added by a damage_sensor on the
// fatal hit, BDS transforms right in reloadComponent; here in OnDeath).
type Transformation struct {
	into, event          string
	delay                int // ticks: delay.value
	rangeMin, rangeMax   int // ticks
	assistChance         float64
	blockRadius          int
	blockMax             int
	blockChance          float64
	blockTypes           []string
	beginSounds, sounds  []string
	preserveEquipment    bool
	dropEquipment        bool
	dropInventory        bool
	keepOwner, keepLevel bool

	left int
	done bool
}

// soundList is a Parser vector<LevelSoundEvent>: one name or a list.
type soundList []string

func (s *soundList) UnmarshalJSON(b []byte) error {
	var one string
	if json.Unmarshal(b, &one) == nil {
		if one != "" {
			*s = soundList{one}
		}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*s = many
	return nil
}

func newTransformation(_ string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		Into              string          `json:"into"`
		Delay             json.RawMessage `json:"delay"`
		PreserveEquipment bool            `json:"preserve_equipment"`
		DropEquipment     bool            `json:"drop_equipment"`
		DropInventory     bool            `json:"drop_inventory"`
		KeepOwner         bool            `json:"keep_owner"`
		KeepLevel         bool            `json:"keep_level"`
		BeginSound        soundList       `json:"begin_transform_sound"`
		Sound             soundList       `json:"transformation_sound"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("minecraft:transformation: %w", err)
	}
	t := &Transformation{preserveEquipment: v.PreserveEquipment, dropEquipment: v.DropEquipment, dropInventory: v.DropInventory,
		keepOwner: v.KeepOwner, keepLevel: v.KeepLevel, beginSounds: v.BeginSound, sounds: v.Sound}
	t.into, t.event = SplitIdentifier(v.Into)
	// TransformationDescription::deserializeData: a number or an object;
	// every time is (int)(seconds*20).
	ticks := func(f float64) int { return int(float32(f) * 20) }
	if len(v.Delay) > 0 {
		if v.Delay[0] == '{' {
			var o struct {
				Value             float64  `json:"value"`
				RangeMin          float64  `json:"range_min"`
				RangeMax          float64  `json:"range_max"`
				BlockAssistChance float64  `json:"block_assist_chance"`
				BlockRadius       int      `json:"block_radius"`
				BlockMax          int      `json:"block_max"`
				BlockChance       float64  `json:"block_chance"`
				BlockTypes        []string `json:"block_types"`
			}
			if err := json.Unmarshal(v.Delay, &o); err != nil {
				return nil, fmt.Errorf("minecraft:transformation: delay: %w", err)
			}
			t.delay, t.rangeMin, t.rangeMax = ticks(o.Value), ticks(o.RangeMin), ticks(o.RangeMax)
			t.assistChance, t.blockRadius, t.blockMax, t.blockChance = o.BlockAssistChance, o.BlockRadius, o.BlockMax, o.BlockChance
			if t.blockMax <= 0 {
				t.blockMax = t.blockRadius
			}
			for _, b := range o.BlockTypes {
				if !strings.Contains(b, ":") {
					b = "minecraft:" + b
				}
				t.blockTypes = append(t.blockTypes, b)
			}
		} else {
			var d float64
			if err := json.Unmarshal(v.Delay, &d); err != nil {
				return nil, fmt.Errorf("minecraft:transformation: delay: %w", err)
			}
			t.delay = ticks(d)
		}
	}
	t.left = t.delay + t.rangeMin
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

// Attach is TransformationComponent::reloadComponent: the begin sounds play
// and the delay is rolled.
func (t *Transformation) Attach(m *mob.Mob) {
	for _, s := range t.beginSounds {
		m.PlaySound(s)
	}
	t.left = t.delay + t.rangeMin
	if t.rangeMin < t.rangeMax {
		t.left += m.Rand().IntN(t.rangeMax - t.rangeMin)
	}
	if t.left > 0 {
		m.UpdateMeta(func(meta *mob.Meta) { meta.Converting = true })
	}
}

// Detach cancels it.
func (t *Transformation) Detach(m *mob.Mob) {
	m.UpdateMeta(func(meta *mob.Meta) { meta.Converting = false })
}

// Left is the number of ticks before the transformation.
func (t *Transformation) Left() int { return t.left }

// SetLeft sets the ticks left (commands, tests).
func (t *Transformation) SetLeft(n int) { t.left = n }

// OnDeath transforms a mob whose transformation was added by the hit that
// killed it (delay 0). BDS does it as the component is added, before the
// damage lands; here the death sound has played by then.
func (t *Transformation) OnDeath(m *mob.Mob, _ world.DamageSource) {
	if !t.done && t.left <= 0 {
		t.transform(m)
	}
}

// tick is tickTransformationComponent.
func (t *Transformation) tick(m *mob.Mob) {
	if t.done {
		return
	}
	step := 1
	if t.assistChance > 0 && m.Rand().Float64() < t.assistChance {
		step += t.assist(m)
	}
	if t.left -= step; t.left <= 0 {
		t.transform(m)
	}
}

// assist counts the block_types blocks around the mob that speed the
// transformation up: the cube from (int)pos-radius to (int)pos+radius
// (exclusive), stopping after block_max matching blocks, each adding one
// tick with block_chance.
func (t *Transformation) assist(m *mob.Mob) int {
	if len(t.blockTypes) == 0 || t.blockMax <= 0 {
		return 0
	}
	tx, r := m.Tx(), m.Rand()
	p := m.Pos()
	px, py, pz := int(p[0]), int(p[1]), int(p[2])
	rad := t.blockRadius
	extra, found := 0, 0
	for x := px - rad; x < px+rad && found < t.blockMax; x++ {
		for y := py - rad; y < py+rad && found < t.blockMax; y++ {
			for z := pz - rad; z < pz+rad && found < t.blockMax; z++ {
				b := tx.Block(cube.Pos{x, y, z})
				if _, air := b.(block.Air); air {
					continue
				}
				name, _ := b.EncodeBlock()
				if !slices.Contains(t.blockTypes, name) {
					continue
				}
				if r.Float64() < t.blockChance {
					extra++
				}
				found++
			}
		}
	}
	return extra
}

func (t *Transformation) transform(m *mob.Mob) {
	t.done = true
	if _, err := TransformWith(m, TransformOptions{Into: t.into, Event: t.event, PreserveEquipment: t.preserveEquipment,
		DropEquipment: t.dropEquipment, DropInventory: t.dropInventory, KeepOwner: t.keepOwner, KeepLevel: t.keepLevel,
		Sounds: t.sounds}); err != nil {
		slog.Default().Warn("mob: transformation failed", "entity", m.Identifier(), "into", t.into, "err", err)
	}
}

// TransformOptions are the minecraft:transformation fields TransformWith
// follows.
type TransformOptions struct {
	// Into is the new mob's identifier, Event the event it spawns with
	// (minecraft:entity_transformed always fires after it).
	Into, Event       string
	PreserveEquipment bool
	DropEquipment     bool
	// DropInventory drops the mob's container (Mob::dropContainer).
	DropInventory bool
	KeepOwner     bool
	// KeepLevel carries the trade tier, max tier and trade experience over.
	KeepLevel bool
	// Sounds are the transformation_sound level sound events, played at the
	// old mob.
	Sounds []string
}

// TransformHook runs in TransformWith after the new mob spawned and saw its
// events, before the old one is removed: the parts of BDS
// TransformationComponent::maintainOldData and transformIfAble that other
// packages own (package village: trades, keep_level, cured discounts).
type TransformHook func(old, n *mob.Mob, o TransformOptions)

var transformHooks []TransformHook

// RegisterTransformHook adds h (call from init).
func RegisterTransformHook(h TransformHook) { transformHooks = append(transformHooks, h) }

// Transform replaces m by a new mob of id, fired with event (and
// minecraft:entity_transformed).
func Transform(m *mob.Mob, id, event string, preserveEquipment, dropEquipment, keepOwner bool) (*mob.Mob, error) {
	return TransformWith(m, TransformOptions{Into: id, Event: event, PreserveEquipment: preserveEquipment,
		DropEquipment: dropEquipment, KeepOwner: keepOwner})
}

// TransformWith is TransformationComponent::transformIfAble: drops the
// equipment and container if asked, spawns the new mob where the old one is
// (ActorFactory::createTransformedActor: position, rotation, head and body
// yaw, name tag, velocity), copies equipment with preserve_equipment,
// persistence, the owner with keep_owner, plays transformation_sound, runs
// the TransformHooks and removes the old mob. Health and fire do not carry
// over.
func TransformWith(m *mob.Mob, o TransformOptions) (*mob.Mob, error) {
	tx := m.Tx()
	o.Into = ActorID(o.Into)
	if _, ok := mob.TypeFor(o.Into); !ok {
		return nil, fmt.Errorf("no mob type %q", o.Into)
	}
	pos := m.Pos()
	drop := func(stacks []item.Stack) {
		for _, s := range stacks {
			if !s.Empty() {
				tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: pos.Add(mgl64.Vec3{0, 0.5})}, s))
			}
		}
	}
	main, off := m.HeldItems()
	a := m.Armour()
	armour := []item.Stack{a.Helmet(), a.Chestplate(), a.Leggings(), a.Boots()}
	if o.DropEquipment && !o.PreserveEquipment {
		drop(append([]item.Stack{main, off}, armour...))
	}
	if o.DropInventory {
		if c, ok := mob.ComponentOf[interface {
			Inventory(*mob.Mob) *inventory.Inventory
		}](m); ok {
			if inv := c.Inventory(m); inv != nil {
				drop(inv.Clear())
			}
		}
	}
	owner, persistent := m.Owner(), m.Persistent()
	headYaw, bodyYaw := m.HeadYaw(), m.BodyYaw()
	opts := mob.SpawnOptions{Rotation: m.Rotation(), Velocity: m.Velocity(), NameTag: m.NameTag(), NoEvent: true}
	opts.Configure = func(n *mob.Mob) {
		n.SetHeadYaw(headYaw)
		n.SetBodyYaw(bodyYaw)
		n.SetPersistent(persistent)
		if o.KeepOwner && owner != nil {
			n.UpdateMeta(func(meta *mob.Meta) { meta.Owner = owner })
		}
		if o.PreserveEquipment {
			n.SetHeldItems(main, off)
			n.Armour().Set(armour[0], armour[1], armour[2], armour[3])
		}
	}
	n, err := mob.Spawn(tx, o.Into, pos, opts)
	if err != nil {
		return nil, err
	}
	// The new mob's events see the old one as "other" (VariantParameterList
	// other = the transformed actor), e.g. villager_v2 picking the cured
	// zombie villager's profession and skin.
	fire := func(event string) {
		ctx := n.Ctx()
		ctx.Other = m
		n.Fire(event, ctx)
	}
	if o.Event != "" {
		fire(o.Event)
	}
	fire(mob.EventTransformed)
	for _, s := range o.Sounds {
		m.PlaySound(s)
	}
	for _, h := range transformHooks {
		h(m, n, o)
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
