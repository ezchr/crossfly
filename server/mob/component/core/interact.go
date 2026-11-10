//go:build crossfly_full

package core

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// interact is minecraft:interact: what players can do with the mob (milk a
// cow, shear a sheep, saddle a pig, ignite a creeper). The first interaction
// whose on_interact filters pass runs (BDS InteractComponent).
type interact struct {
	list     []*interaction
	cooldown int64 // mob tick before which interactions are refused
}

type interaction struct {
	OnInteract      Trigger         `json:"on_interact"`
	Cooldown        float64         `json:"cooldown"`
	CooldownHurt    float64         `json:"cooldown_after_being_attacked"`
	UseItem         bool            `json:"use_item"`
	HurtItem        int             `json:"hurt_item"`
	TransformToItem string          `json:"transform_to_item"`
	SpawnItems      *spawnItems     `json:"spawn_items"`
	PlaySounds      json.RawMessage `json:"play_sounds"`
	HealthAmount    float64         `json:"health_amount"`
	Swing           bool            `json:"swing"`
	// EquipItemSlot puts the held item into an equipment slot and
	// DropItemSlot drops what a slot holds (mob.EquipSlot/DropSlot: the
	// equippable slot number or a name such as slot.armor.body).
	EquipItemSlot   string  `json:"equip_item_slot"`
	DropItemSlot    string  `json:"drop_item_slot"`
	DropItemYOffset float64 `json:"drop_item_y_offset"`
	// Admire and Barter hand the held item to the piglin's admire_item and
	// barter components (interact_pickup.go).
	Admire bool `json:"admire"`
	Barter bool `json:"barter"`
	// AddItems, GiveItem/TakeItem, ParticleOnStart, Vibration and
	// InteractText are interact_items.go.
	AddItems        *spawnItems      `json:"add_items"`
	GiveItem        bool             `json:"give_item"`
	TakeItem        bool             `json:"take_item"`
	ParticleOnStart *particleOnStart `json:"particle_on_start"`
	Vibration       string           `json:"vibration"`
	InteractText    string           `json:"interact_text"`

	sounds []string
}

type spawnItems struct {
	Table   string  `json:"table"`
	YOffset float64 `json:"y_offset"`
}

func newInteract(key string, raw json.RawMessage) (mob.Component, error) {
	var j struct {
		Interactions json.RawMessage `json:"interactions"`
	}
	if err := decode(raw, &j); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	list := bytes.TrimSpace(j.Interactions)
	if len(list) == 0 {
		return &interact{}, nil
	}
	if list[0] != '[' {
		list = append(append([]byte{'['}, list...), ']')
	}
	c := &interact{}
	if err := json.Unmarshal(list, &c.list); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	for _, in := range c.list {
		s, err := stringList(in.PlaySounds)
		if err != nil {
			return nil, fmt.Errorf("%v: play_sounds: %w", key, err)
		}
		in.sounds = s
		if in.SpawnItems != nil && in.SpawnItems.Table != "" {
			warnMissingTable(in.SpawnItems.Table)
		}
		if in.AddItems != nil && in.AddItems.Table != "" {
			warnMissingTable(in.AddItems.Table)
		}
	}
	return c, nil
}

func (*interact) Attach(*mob.Mob) {}
func (*interact) Detach(*mob.Mob) {}

// Interact runs the first interaction whose filters pass.
func (c *interact) Interact(m *mob.Mob, user item.User, held item.Stack, _ mgl64.Vec3) (item.Stack, bool) {
	if m.Ticks() < c.cooldown {
		return held, false
	}
	ctx := InteractContext(m, user, held)
	for _, in := range c.list {
		if !in.OnInteract.Passes(ctx) || !in.mayRun(m, held) {
			continue
		}
		if in.CooldownHurt > 0 {
			if _, t, ok := m.LastHurtBy(); ok && m.Ticks()-t < int64(ticksOf(in.CooldownHurt)) {
				return held, false
			}
		}
		c.cooldown = m.Ticks() + int64(ticksOf(in.Cooldown))
		return in.run(m, user, held, ctx), true
	}
	return held, false
}

func (in *interaction) run(m *mob.Mob, user item.User, held item.Stack, ctx *mob.FilterContext) item.Stack {
	for _, s := range in.sounds {
		PlayMobSound(m, s)
	}
	before := held
	creative := IsCreative(user)
	if !creative {
		if in.HurtItem > 0 && !held.Empty() {
			held = held.Damage(in.HurtItem)
		}
		if in.TransformToItem != "" {
			if s, ok := ParseItemDescriptor(in.TransformToItem).Stack(1); ok {
				held = ReplaceOne(m, user, held, s)
			}
		} else if in.UseItem {
			held = held.Grow(-1)
		}
	} else if in.TransformToItem != "" {
		// Creative players keep their item and get the result as well.
		if s, ok := ParseItemDescriptor(in.TransformToItem).Stack(1); ok {
			GiveItem(m, user, s)
		}
	}
	held = in.runGiveTake(m, user, held, before, creative)
	held = in.runAddItems(m, user, held)
	if in.SpawnItems != nil && in.SpawnItems.Table != "" {
		if t, err := LoadLootTable(in.SpawnItems.Table); err == nil {
			DropItems(m, t.Roll(&LootContext{Mob: m, Killer: user}), in.SpawnItems.YOffset+m.BBox().Height()*0.5)
		}
	}
	if in.HealthAmount > 0 {
		m.Heal(in.HealthAmount, nil)
	}
	if in.EquipItemSlot != "" {
		// InteractComponent::_tryEquipItem equips the item as it was before use_item used it up
		// (the happy ghast's harness); otherwise equipping takes one item.
		eq := held
		used := in.UseItem && !creative && in.TransformToItem == ""
		if used {
			eq = before
		}
		if !eq.Empty() && m.EquipSlot(in.EquipItemSlot, eq) && !creative && !used {
			held = held.Grow(-1)
		}
	}
	if in.DropItemSlot != "" {
		if s, ok := m.DropSlot(in.DropItemSlot); ok {
			DropItems(m, []item.Stack{s}, in.DropItemYOffset)
		}
	}
	in.runPickup(m, user, before)
	in.runEffects(m, user)
	in.OnInteract.Fire(m, ctx)
	return held
}

// InteractContext is the filter context of a player using the mob: other
// and player are the user, the item is what they hold.
func InteractContext(m *mob.Mob, user item.User, held item.Stack) *mob.FilterContext {
	ctx := m.Ctx()
	ctx.Other, ctx.Player, ctx.Item = user, user, held
	return ctx
}

// IsCreative reports whether the user is in a game mode that does not use
// items up.
func IsCreative(user item.User) bool {
	if g, ok := user.(interface{ GameMode() world.GameMode }); ok {
		return g.GameMode().CreativeInventory()
	}
	return false
}

// ConsumeOne uses one of the held item up (not in creative) and returns what
// is left in the hand.
func ConsumeOne(user item.User, held item.Stack) item.Stack {
	if IsCreative(user) {
		return held
	}
	return held.Grow(-1)
}

// ReplaceOne turns one of the held stack into result: the hand gets result
// when it held one item, else one is taken and result goes to the user's
// inventory (or is dropped at the mob).
func ReplaceOne(m *mob.Mob, user item.User, held, result item.Stack) item.Stack {
	if held.Count() <= 1 {
		return result
	}
	GiveItem(m, user, result)
	return held.Grow(-1)
}

// GiveItem adds s to the user's inventory, or drops it at the mob.
func GiveItem(m *mob.Mob, user item.User, s item.Stack) {
	if inv, ok := user.(interface{ Inventory() *inventory.Inventory }); ok {
		if _, err := inv.Inventory().AddItem(s); err == nil {
			return
		}
	}
	DropItems(m, []item.Stack{s}, 0.5)
}
