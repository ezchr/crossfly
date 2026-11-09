//go:build crossfly_full

// Package tame holds the taming and pet components of Bedrock mobs:
// tameable, tamemount (horses), sittable, healable, is_dyeable (collars),
// giveable, trusting, trust and on_wake_with_owner. Owners are kept by
// package mob (owner.go: SetOwner, OwnerEntity). The goals that use them
// (follow_owner, sitting, begging, ...) are in package mob/goal/ride.
package tame

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func init() {
	mob.RegisterComponent("minecraft:tameable", newTameable)
	mob.RegisterComponent("minecraft:tamemount", newTameMount)
	mob.RegisterComponent("minecraft:sittable", newSittable)
	mob.RegisterComponent("minecraft:healable", newHealable)
	mob.RegisterComponent("minecraft:is_dyeable", newDyeable)
	mob.RegisterComponent("minecraft:giveable", newGiveable)
	mob.RegisterComponent("minecraft:trusting", newTrusting)
	mob.RegisterComponent("minecraft:trust", newTrust)
	mob.RegisterComponent("minecraft:on_wake_with_owner", newOnWake)
}

// TameSucceeded and TameFailed are the hearts and smoke of a taming attempt
// (actor events taming_succeeded/taming_failed).
type (
	TameSucceeded struct{}
	TameFailed    struct{}
)

func (TameSucceeded) EntityAction()     {}
func (TameSucceeded) ActorEvent() uint8 { return packet.ActorEventTamingSucceeded }
func (TameFailed) EntityAction()        {}
func (TameFailed) ActorEvent() uint8    { return packet.ActorEventTamingFailed }

// Tamed reports whether m is tamed (is_tamed).
func Tamed(m *mob.Mob) bool { return m.Meta().Tamed || m.HasComponent("minecraft:is_tamed") }

// IsOwner reports whether e owns m.
func IsOwner(m *mob.Mob, e world.Entity) bool {
	id, ok := m.OwnerUUID()
	return ok && e != nil && e.H().UUID() == id
}

// tameTo makes p the owner of m (TameableComponent::tame,
// MountTamingComponent::tameToPlayer): persistent, the tame event, hearts.
func tameTo(m *mob.Mob, p world.Entity, ev *core.Trigger) {
	m.SetOwner(p)
	m.SetPersistent(true)
	m.StopMovement()
	m.ResetTarget()
	if ev != nil {
		ctx := m.Ctx()
		ctx.Other, ctx.Player = p, p
		ev.Fire(m, ctx)
	}
	core.ShowAction(m, TameSucceeded{})
}

// ---- minecraft:tameable ----

// Tameable is minecraft:tameable (BDS TameableComponent): feeding a
// tame_items item tames the mob to the player with probability (0.33 in the
// vanilla files), consuming it (result_item replaces it, as a pufferfish
// bucket gives a water bucket back).
type Tameable struct {
	prob  float64
	items []tameItem
	event *core.Trigger
}

type tameItem struct {
	item   core.ItemDescriptor
	result string
}

func newTameable(key string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		Probability *float64        `json:"probability"`
		Items       json.RawMessage `json:"tame_items"`
		Event       *core.Trigger   `json:"tame_event"`
	}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	t := &Tameable{prob: 1, event: v.Event}
	if v.Probability != nil {
		t.prob = *v.Probability
	}
	items, err := parseItems(v.Items)
	if err != nil {
		return nil, fmt.Errorf("%v: tame_items: %w", key, err)
	}
	t.items = items
	return t, nil
}

// Attach ...
func (t *Tameable) Attach(*mob.Mob) {}

// Detach ...
func (t *Tameable) Detach(*mob.Mob) {}

// Interact feeds a tame item.
func (t *Tameable) Interact(m *mob.Mob, user item.User, held item.Stack, _ mgl64.Vec3) (item.Stack, bool) {
	p, ok := user.(*player.Player)
	if !ok || Tamed(m) {
		return held, false
	}
	i := slices.IndexFunc(t.items, func(it tameItem) bool { return it.item.Matches(held) })
	if i < 0 {
		return held, false
	}
	left := useItem(m, user, held, t.items[i].result)
	if m.Rand().Float64() < t.prob {
		tameTo(m, p, t.event)
		if s, ok := mob.ComponentOf[*Sittable](m); ok {
			// A freshly tamed wolf or cat sits (TameableComponent::tame).
			s.SetSitting(m, true)
		}
	} else {
		core.ShowAction(m, TameFailed{})
	}
	return left, true
}

// ---- minecraft:tamemount ----

// TameMount is minecraft:tamemount (BDS MountTamingComponent): feed_items
// raise the temper, auto_reject_items make the mob rear; ridden by a player,
// each tick there is a 1 in 50 chance of a taming roll: it tames when a roll
// below max_temper is under the temper, else the temper rises by 5 and the
// mob throws its rider off (java RunAroundLikeCrazyGoal, which BDS's
// MountTamingSystem matches).
type TameMount struct {
	minTemper, maxTemper int
	feed                 []feedItem
	reject               []core.ItemDescriptor
	event                *core.Trigger
	temper               int
	rearing              int
}

type feedItem struct {
	item   core.ItemDescriptor
	temper int
}

func newTameMount(key string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		Min  *int `json:"min_temper"`
		Max  *int `json:"max_temper"`
		Feed []struct {
			Item   string `json:"item"`
			Temper int    `json:"temper_mod"`
		} `json:"feed_items"`
		Reject json.RawMessage `json:"auto_reject_items"`
		Event  *core.Trigger   `json:"tame_event"`
	}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	t := &TameMount{maxTemper: 100, event: v.Event}
	if v.Min != nil {
		t.minTemper = *v.Min
	}
	if v.Max != nil {
		t.maxTemper = *v.Max
	}
	for _, f := range v.Feed {
		t.feed = append(t.feed, feedItem{item: core.ParseItemDescriptor(f.Item), temper: f.Temper})
	}
	reject, err := parseDescList(v.Reject)
	if err != nil {
		return nil, fmt.Errorf("%v: auto_reject_items: %w", key, err)
	}
	t.reject = reject
	t.temper = t.minTemper
	return t, nil
}

// Attach ...
func (t *TameMount) Attach(*mob.Mob) {}

// Detach ...
func (t *TameMount) Detach(m *mob.Mob) {
	if t.rearing > 0 {
		m.SetActorFlag(protocol.EntityDataFlagStanding, false)
	}
}

// Temper is the current temper.
func (t *TameMount) Temper() int { return t.temper }

// Interact feeds a feed item or rejects an auto_reject item
// (MountTamingComponent::getInteraction; never with a rider).
func (t *TameMount) Interact(m *mob.Mob, user item.User, held item.Stack, _ mgl64.Vec3) (item.Stack, bool) {
	if Tamed(m) || m.RiderCount() > 0 || held.Empty() {
		return held, false
	}
	for _, f := range t.feed {
		if f.item.Matches(held) {
			t.temper = min(t.temper+f.temper, t.maxTemper)
			core.PlayMobSound(m, "eat")
			core.ShowAction(m, core.FeedAction{})
			return core.ConsumeOne(user, held), true
		}
	}
	if core.MatchesAny(t.reject, held) {
		t.MakeMad(m)
		return held, true
	}
	return held, false
}

// MakeMad makes the mob rear and neigh (Horse::makeMad).
func (t *TameMount) MakeMad(m *mob.Mob) {
	t.rearing = 20
	m.SetActorFlag(protocol.EntityDataFlagStanding, true)
	core.PlayMobSound(m, "mad")
}

// Tick runs the taming rolls while a player rides the mob.
func (t *TameMount) Tick(m *mob.Mob) {
	if t.rearing > 0 {
		if t.rearing--; t.rearing == 0 {
			m.SetActorFlag(protocol.EntityDataFlagStanding, false)
		}
	}
	if Tamed(m) {
		return
	}
	ps := m.Passengers()
	if len(ps) == 0 {
		return
	}
	p, ok := ps[0].(*player.Player)
	if !ok || m.Rand().IntN(50) != 0 {
		return
	}
	if t.maxTemper > 0 && m.Rand().IntN(t.maxTemper) < t.temper {
		tameTo(m, p, t.event)
		return
	}
	t.temper = min(t.temper+5, t.maxTemper)
	m.EjectPassengers()
	t.MakeMad(m)
	core.ShowAction(m, TameFailed{})
}

// EncodeNBT ...
func (t *TameMount) EncodeNBT() map[string]any { return map[string]any{"Temper": int32(t.temper)} }

// DecodeNBT ...
func (t *TameMount) DecodeNBT(_ *mob.Mob, d map[string]any) {
	if v, ok := d["Temper"].(int32); ok {
		t.temper = int(v)
	}
}

// ---- minecraft:sittable ----

// Sittable is minecraft:sittable (BDS SitComponent): its owner makes the
// mob sit down or stand up by using it with anything else does nothing with
// (sit_event, stand_event). The sitting flag is saved.
type Sittable struct {
	sitEvent, standEvent *core.Trigger
	sitting              bool
}

func newSittable(key string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		Sit   *core.Trigger `json:"sit_event"`
		Stand *core.Trigger `json:"stand_event"`
	}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	return &Sittable{sitEvent: v.Sit, standEvent: v.Stand}, nil
}

// Attach ...
func (s *Sittable) Attach(m *mob.Mob) {
	m.UpdateMeta(func(meta *mob.Meta) { meta.Sitting = s.sitting })
}

// Detach ...
func (s *Sittable) Detach(m *mob.Mob) {
	m.UpdateMeta(func(meta *mob.Meta) { meta.Sitting = false })
}

// InteractLate: sitting down is tried after feeding and healing.
func (s *Sittable) InteractLate() {}

// Interact toggles sitting for the owner.
func (s *Sittable) Interact(m *mob.Mob, user item.User, held item.Stack, _ mgl64.Vec3) (item.Stack, bool) {
	p, ok := user.(*player.Player)
	if !ok || !IsOwner(m, p) || p.Sneaking() || m.Riding() {
		return held, false
	}
	s.SetSitting(m, !s.sitting)
	return held, true
}

// Sitting reports whether the mob sits.
func (s *Sittable) Sitting() bool { return s.sitting }

// SetSitting makes the mob sit down or stand up.
func (s *Sittable) SetSitting(m *mob.Mob, v bool) {
	if s.sitting == v {
		return
	}
	s.sitting = v
	m.UpdateMeta(func(meta *mob.Meta) { meta.Sitting = v })
	if v {
		m.StopMovement()
		m.ResetTarget()
		if s.sitEvent != nil {
			s.sitEvent.Fire(m, nil)
		}
	} else if s.standEvent != nil {
		s.standEvent.Fire(m, nil)
	}
}

// EncodeNBT ...
func (s *Sittable) EncodeNBT() map[string]any {
	if !s.sitting {
		return nil
	}
	return map[string]any{"Sitting": uint8(1)}
}

// DecodeNBT ...
func (s *Sittable) DecodeNBT(m *mob.Mob, d map[string]any) {
	if v, ok := d["Sitting"].(uint8); ok && v == 1 {
		s.sitting = true
		m.UpdateMeta(func(meta *mob.Meta) { meta.Sitting = true })
	}
}

// SittableOf returns the mob's sittable component.
func SittableOf(m *mob.Mob) (*Sittable, bool) { return mob.ComponentOf[*Sittable](m) }

// ---- minecraft:healable ----

// Healable is minecraft:healable (BDS HealableComponent): feeding one of
// items heals heal_amount when the mob is hurt (always with force_use) and
// the filters pass, applies the item's effects (a cookie poisons a parrot)
// and uses the item up.
type Healable struct {
	items    []healItem
	forceUse bool
	filter   mob.Filter
}

type healItem struct {
	item    core.ItemDescriptor
	amount  float64
	result  string
	effects []healEffect
}

type healEffect struct {
	name          string
	duration, amp int
	chance        float64
}

func newHealable(key string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		Items []struct {
			Item    string  `json:"item"`
			Amount  float64 `json:"heal_amount"`
			Result  string  `json:"result_item"`
			Effects []struct {
				Name      string   `json:"name"`
				Duration  float64  `json:"duration"`
				Amplifier int      `json:"amplifier"`
				Chance    *float64 `json:"chance"`
			} `json:"effects"`
		} `json:"items"`
		ForceUse bool            `json:"force_use"`
		Filters  json.RawMessage `json:"filters"`
	}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	f, err := mob.CompileFilter(v.Filters)
	if err != nil {
		return nil, fmt.Errorf("%v: filters: %w", key, err)
	}
	h := &Healable{forceUse: v.ForceUse, filter: f}
	for _, it := range v.Items {
		hi := healItem{item: core.ParseItemDescriptor(it.Item), amount: it.Amount, result: it.Result}
		for _, e := range it.Effects {
			he := healEffect{name: e.Name, duration: int(e.Duration * 20), amp: e.Amplifier, chance: 1}
			if e.Chance != nil {
				he.chance = *e.Chance
			}
			hi.effects = append(hi.effects, he)
		}
		h.items = append(h.items, hi)
	}
	return h, nil
}

// Attach ...
func (h *Healable) Attach(*mob.Mob) {}

// Detach ...
func (h *Healable) Detach(*mob.Mob) {}

// Interact heals with a heal item.
func (h *Healable) Interact(m *mob.Mob, user item.User, held item.Stack, _ mgl64.Vec3) (item.Stack, bool) {
	i := slices.IndexFunc(h.items, func(it healItem) bool { return it.item.Matches(held) })
	if i < 0 || (!h.forceUse && m.Health() >= m.MaxHealth()) {
		return held, false
	}
	if h.filter != nil && !h.filter.Test(core.InteractContext(m, user, held)) {
		return held, false
	}
	it := h.items[i]
	if it.amount > 0 {
		m.Heal(it.amount, nil)
	}
	for _, e := range it.effects {
		if m.Rand().Float64() >= e.chance {
			continue
		}
		if t, ok := effectByName(e.name); ok {
			m.AddEffect(effect.New(t, e.amp+1, time.Duration(e.duration)*time.Second/20))
		}
	}
	core.PlayMobSound(m, "eat")
	core.ShowAction(m, core.FeedAction{})
	return useItem(m, user, held, it.result), true
}

// ---- minecraft:is_dyeable ----

// Dyeable is minecraft:is_dyeable (BDS DyeableDefinition, first in
// Actor::getInteraction): a dye of another colour than the mob's
// minecraft:color (wool, collar) recolours it and is used up.
type Dyeable struct{}

func newDyeable(string, json.RawMessage) (mob.Component, error) { return Dyeable{}, nil }

// Attach ...
func (Dyeable) Attach(*mob.Mob) {}

// Detach ...
func (Dyeable) Detach(*mob.Mob) {}

// Interact dyes the mob.
func (Dyeable) Interact(m *mob.Mob, user item.User, held item.Stack, _ mgl64.Vec3) (item.Stack, bool) {
	d, ok := held.Item().(item.Dye)
	if !ok {
		return held, false
	}
	if (m.HasComponent("minecraft:is_tamed") || m.Meta().Tamed) && !IsOwner(m, user) {
		return held, false
	}
	c := d.Colour.Uint8()
	if m.Meta().Color == c {
		return held, false
	}
	core.SetColor(m, c)
	return core.ConsumeOne(user, held), true
}

// ---- minecraft:giveable ----

// Giveable is minecraft:giveable (BDS GiveableComponent, panda): giving one
// of items (cooldown seconds apart) fires on_give.
type Giveable struct {
	items    []core.ItemDescriptor
	cooldown int64
	onGive   *core.Trigger
	next     int64
}

func newGiveable(key string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		Triggers struct {
			Cooldown float64         `json:"cooldown"`
			Items    json.RawMessage `json:"items"`
			OnGive   *core.Trigger   `json:"on_give"`
		} `json:"triggers"`
	}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	items, err := parseDescList(v.Triggers.Items)
	if err != nil {
		return nil, fmt.Errorf("%v: items: %w", key, err)
	}
	return &Giveable{items: items, cooldown: int64(math.Round(v.Triggers.Cooldown * 20)), onGive: v.Triggers.OnGive}, nil
}

// Attach ...
func (g *Giveable) Attach(*mob.Mob) {}

// Detach ...
func (g *Giveable) Detach(*mob.Mob) {}

// Interact gives an item.
func (g *Giveable) Interact(m *mob.Mob, user item.User, held item.Stack, _ mgl64.Vec3) (item.Stack, bool) {
	if m.Ticks() < g.next || !core.MatchesAny(g.items, held) {
		return held, false
	}
	g.next = m.Ticks() + g.cooldown
	if g.onGive != nil {
		g.onGive.Fire(m, core.InteractContext(m, user, held))
	}
	core.PlayMobSound(m, "eat")
	return core.ConsumeOne(user, held), true
}

// ---- minecraft:trusting ----

// Trusting is minecraft:trusting (BDS TrustingComponent, ocelot): feeding a
// trust item makes the mob trust players with probability (trust_event, the
// trusting flag); no owner.
type Trusting struct {
	prob  float64
	items []tameItem
	event *core.Trigger
}

func newTrusting(key string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		Probability *float64        `json:"probability"`
		Items       json.RawMessage `json:"trust_items"`
		Event       *core.Trigger   `json:"trust_event"`
	}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	t := &Trusting{prob: 1, event: v.Event}
	if v.Probability != nil {
		t.prob = *v.Probability
	}
	items, err := parseItems(v.Items)
	if err != nil {
		return nil, fmt.Errorf("%v: trust_items: %w", key, err)
	}
	t.items = items
	return t, nil
}

// Attach ...
func (t *Trusting) Attach(*mob.Mob) {}

// Detach ...
func (t *Trusting) Detach(*mob.Mob) {}

// Interact feeds a trust item.
func (t *Trusting) Interact(m *mob.Mob, user item.User, held item.Stack, _ mgl64.Vec3) (item.Stack, bool) {
	if m.ActorFlag(protocol.EntityDataFlagTrusting) {
		return held, false
	}
	i := slices.IndexFunc(t.items, func(it tameItem) bool { return it.item.Matches(held) })
	if i < 0 {
		return held, false
	}
	left := useItem(m, user, held, t.items[i].result)
	if m.Rand().Float64() < t.prob {
		m.SetActorFlag(protocol.EntityDataFlagTrusting, true)
		m.SetPersistent(true)
		if t.event != nil {
			t.event.Fire(m, core.InteractContext(m, user, held))
		}
		core.ShowAction(m, TameSucceeded{})
	} else {
		core.ShowAction(m, TameFailed{})
	}
	return left, true
}

// ---- minecraft:trust ----

// Trust is minecraft:trust (fox): the players the mob trusts (filter
// trusts, behavior.defend_trusted_target). Add them with AddTrusted.
type Trust struct {
	trusted []uuid.UUID
}

func newTrust(string, json.RawMessage) (mob.Component, error) { return &Trust{}, nil }

// Attach ...
func (t *Trust) Attach(*mob.Mob) {}

// Detach ...
func (t *Trust) Detach(*mob.Mob) {}

// AddTrusted makes the mob trust e.
func (t *Trust) AddTrusted(e world.Entity) {
	if !slices.Contains(t.trusted, e.H().UUID()) {
		t.trusted = append(t.trusted, e.H().UUID())
	}
}

// Trusts reports whether the mob trusts e.
func (t *Trust) Trusts(e world.Entity) bool {
	return e != nil && slices.Contains(t.trusted, e.H().UUID())
}

// EncodeNBT ...
func (t *Trust) EncodeNBT() map[string]any {
	if len(t.trusted) == 0 {
		return nil
	}
	list := make([]any, len(t.trusted))
	for i, id := range t.trusted {
		list[i] = id.String()
	}
	return map[string]any{"Trusted": list}
}

// DecodeNBT ...
func (t *Trust) DecodeNBT(_ *mob.Mob, d map[string]any) {
	list, _ := d["Trusted"].([]any)
	for _, v := range list {
		if s, ok := v.(string); ok {
			if id, err := uuid.Parse(s); err == nil {
				t.trusted = append(t.trusted, id)
			}
		}
	}
}

// ---- minecraft:on_wake_with_owner ----

// OnWake is minecraft:on_wake_with_owner (cat): the event fired when the
// owner wakes up after the mob slept with them
// (behavior.pet_sleep_with_owner calls Fire).
type OnWake struct{ t core.Trigger }

func newOnWake(key string, raw json.RawMessage) (mob.Component, error) {
	var t core.Trigger
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	return &OnWake{t: t}, nil
}

// Attach ...
func (o *OnWake) Attach(*mob.Mob) {}

// Detach ...
func (o *OnWake) Detach(*mob.Mob) {}

// Fire runs the event with the owner as other.
func (o *OnWake) Fire(m *mob.Mob, owner world.Entity) {
	ctx := m.Ctx()
	ctx.Other, ctx.Player = owner, owner
	o.t.Fire(m, ctx)
}

// ---- helpers ----

// useItem uses one held item: it is consumed, or replaced by result
// (creative players keep theirs).
func useItem(m *mob.Mob, user item.User, held item.Stack, result string) item.Stack {
	if result != "" && !core.IsCreative(user) {
		if s, ok := core.ParseItemDescriptor(result).Stack(1); ok {
			return core.ReplaceOne(m, user, held, s)
		}
	}
	return core.ConsumeOne(user, held)
}

// parseItems reads a tame_items/trust_items list: names, or objects with
// item and result_item.
func parseItems(raw json.RawMessage) ([]tameItem, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, nil
	}
	if raw[0] != '[' {
		raw = append(append([]byte{'['}, raw...), ']')
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	var out []tameItem
	for _, e := range list {
		var s string
		if json.Unmarshal(e, &s) == nil {
			out = append(out, tameItem{item: core.ParseItemDescriptor(s)})
			continue
		}
		var o struct {
			Item   string `json:"item"`
			Result string `json:"result_item"`
		}
		if err := json.Unmarshal(e, &o); err != nil {
			return nil, err
		}
		out = append(out, tameItem{item: core.ParseItemDescriptor(o.Item), result: o.Result})
	}
	return out, nil
}

// parseDescList reads a list of item names or {"item": name} objects.
func parseDescList(raw json.RawMessage) ([]core.ItemDescriptor, error) {
	items, err := parseItems(raw)
	out := make([]core.ItemDescriptor, len(items))
	for i, it := range items {
		out[i] = it.item
	}
	return out, err
}

var effectsByName = map[string]effect.LastingType{
	"fatal_poison":    effect.FatalPoison,
	"poison":          effect.Poison,
	"regeneration":    effect.Regeneration,
	"speed":           effect.Speed,
	"slowness":        effect.Slowness,
	"weakness":        effect.Weakness,
	"strength":        effect.Strength,
	"wither":          effect.Wither,
	"nausea":          effect.Nausea,
	"hunger":          effect.Hunger,
	"resistance":      effect.Resistance,
	"fire_resistance": effect.FireResistance,
}

func effectByName(name string) (effect.LastingType, bool) {
	t, ok := effectsByName[name]
	return t, ok
}

func decode(raw json.RawMessage, v any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	return json.Unmarshal(raw, v)
}

// held is the main hand item of e.
func held(e world.Entity) item.Stack {
	if h, ok := e.(interface {
		HeldItems() (item.Stack, item.Stack)
	}); ok {
		main, _ := h.HeldItems()
		return main
	}
	return item.Stack{}
}

// InteractText implements mob.InteractTexter.
func (t *Tameable) InteractText(m *mob.Mob, user world.Entity) string {
	s := held(user)
	if Tamed(m) || !slices.ContainsFunc(t.items, func(it tameItem) bool { return it.item.Matches(s) }) {
		return ""
	}
	return "action.interact.tame"
}

// InteractText implements mob.InteractTexter (feed_text).
func (t *TameMount) InteractText(m *mob.Mob, user world.Entity) string {
	s := held(user)
	if Tamed(m) || m.RiderCount() > 0 || !slices.ContainsFunc(t.feed, func(f feedItem) bool { return f.item.Matches(s) }) {
		return ""
	}
	return "action.interact.feed"
}

// InteractText implements mob.InteractTexter for the owner.
func (s *Sittable) InteractText(m *mob.Mob, user world.Entity) string {
	p, ok := user.(*player.Player)
	if !ok || !IsOwner(m, p) || p.Sneaking() || m.Riding() {
		return ""
	}
	if s.sitting {
		return "action.interact.stand"
	}
	return "action.interact.sit"
}

// InteractText implements mob.InteractTexter.
func (h *Healable) InteractText(m *mob.Mob, user world.Entity) string {
	s := held(user)
	if !slices.ContainsFunc(h.items, func(it healItem) bool { return it.item.Matches(s) }) || (!h.forceUse && m.Health() >= m.MaxHealth()) {
		return ""
	}
	return "action.interact.feed"
}

// InteractText implements mob.InteractTexter.
func (Dyeable) InteractText(m *mob.Mob, user world.Entity) string {
	d, ok := held(user).Item().(item.Dye)
	if !ok || m.Meta().Color == d.Colour.Uint8() {
		return ""
	}
	if (m.HasComponent("minecraft:is_tamed") || m.Meta().Tamed) && !IsOwner(m, user) {
		return ""
	}
	return "action.interact.dye"
}

// Tame tames m to p the way a successful minecraft:tamemount or
// minecraft:tameable roll does (tame event, owner, hearts), for admin and
// debug commands. It reports false if m has neither component.
func Tame(m *mob.Mob, p world.Entity) bool {
	if c, ok := m.Component("minecraft:tamemount"); ok {
		tameTo(m, p, c.(*TameMount).event)
		return true
	}
	if c, ok := m.Component("minecraft:tameable"); ok {
		tameTo(m, p, c.(*Tameable).event)
		return true
	}
	return false
}
