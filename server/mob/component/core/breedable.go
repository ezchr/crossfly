//go:build crossfly_full

package core

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Love mode lasts 600 ticks (BDS BreedableComponent::setInLove) and a parent
// cannot breed again for 60 seconds (breed_cooldown default).
const (
	loveTicks           = 600
	defaultBreedCooling = 60.0
)

// Breedable is minecraft:breedable: feeding a breed item puts an adult in
// love; two mobs in love that may breed (behavior.breed brings them
// together) make a baby (BDS BreedableComponent/BreedableDefinition, the
// 1.26 offspring settings from minecraft:offspring).
//
//	require_tame (default true), require_full_health, breed_items,
//	breeds_with ({mate: {baby_type, breed_event}} or a list of
//	{mate_type, baby_type, breed_event}), love_filters, allow_sitting,
//	extra_baby_chance, breed_cooldown (seconds, default 60),
//	environment_requirements ({blocks, count, radius} or a list: panda
//	bamboo), and the 1.21 inheritance keys minecraft:offspring took over
//	(mutation_factor, blend_attributes, ...; see inheritance)
type Breedable struct {
	requireTame, requireFullHealth, allowSitting bool
	items                                        []breedItem
	with                                         map[string]breedType
	loveFilter                                   mob.Filter
	extraBabyChance                              float64
	cooldownTicks                                int
	environment                                  []envRequirement
	inherit                                      inheritance

	love      int // ticks of love mode left
	cooldown  int // ticks before it may breed again
	loveCause *world.EntityHandle
}

type breedItem struct {
	desc   ItemDescriptor
	result string
}

type breedType struct {
	Mate  string `json:"mate_type"`
	Baby  string `json:"baby_type"`
	Event string `json:"-"`
}

func newBreedable(key string, raw json.RawMessage) (mob.Component, error) {
	j := struct {
		RequireTame       *bool           `json:"require_tame"`
		RequireFullHealth bool            `json:"require_full_health"`
		AllowSitting      bool            `json:"allow_sitting"`
		BreedItems        json.RawMessage `json:"breed_items"`
		BreedsWith        json.RawMessage `json:"breeds_with"`
		LoveFilters       json.RawMessage `json:"love_filters"`
		ExtraBabyChance   float64         `json:"extra_baby_chance"`
		BreedCooldown     *float64        `json:"breed_cooldown"`
		Environment       json.RawMessage `json:"environment_requirements"`
	}{}
	if err := decode(raw, &j); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	inherit, err := parseInheritance(raw)
	if err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	env, err := parseEnvRequirements(j.Environment)
	if err != nil {
		return nil, fmt.Errorf("%v: environment_requirements: %w", key, err)
	}
	b := &Breedable{requireTame: true, requireFullHealth: j.RequireFullHealth, allowSitting: j.AllowSitting,
		extraBabyChance: j.ExtraBabyChance, cooldownTicks: ticksOf(defaultBreedCooling), with: map[string]breedType{},
		environment: env, inherit: inherit}
	if j.RequireTame != nil {
		b.requireTame = *j.RequireTame
	}
	if j.BreedCooldown != nil {
		b.cooldownTicks = ticksOf(*j.BreedCooldown)
	}
	feed, err := parseFeedItems(j.BreedItems)
	if err != nil {
		return nil, fmt.Errorf("%v: breed_items: %w", key, err)
	}
	for _, f := range feed {
		b.items = append(b.items, breedItem{desc: f.desc, result: f.result})
	}
	if err := b.parseBreedsWith(j.BreedsWith); err != nil {
		return nil, fmt.Errorf("%v: breeds_with: %w", key, err)
	}
	if b.loveFilter, err = mob.CompileFilter(j.LoveFilters); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	return b, nil
}

func (b *Breedable) parseBreedsWith(raw json.RawMessage) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil
	}
	type entry struct {
		Mate       string  `json:"mate_type"`
		Baby       string  `json:"baby_type"`
		BreedEvent Trigger `json:"breed_event"`
	}
	add := func(mate string, e entry) {
		mate = normaliseID(mate)
		b.with[mate] = breedType{Mate: mate, Baby: normaliseID(e.Baby), Event: e.BreedEvent.Event}
	}
	switch raw[0] {
	case '[':
		var list []entry
		if err := json.Unmarshal(raw, &list); err != nil {
			return err
		}
		for _, e := range list {
			add(e.Mate, e)
		}
	case '{':
		var single entry
		if json.Unmarshal(raw, &single) == nil && single.Mate != "" {
			add(single.Mate, single)
			return nil
		}
		objs, err := mob.ParseOrderedObject(raw)
		if err != nil {
			return err
		}
		for _, o := range objs {
			var e entry
			_ = decode(o.Raw, &e)
			add(o.Key, e)
		}
	}
	return nil
}

func (b *Breedable) Attach(*mob.Mob) {}
func (b *Breedable) Detach(*mob.Mob) {}

// Tick counts love mode and the breeding cooldown down (BDS
// BreedableSystem).
func (b *Breedable) Tick(m *mob.Mob) {
	if b.cooldown > 0 {
		b.cooldown--
	}
	if b.love > 0 {
		b.love--
	}
	// Bedrock shows love mode with the in_love actor flag (hearts while it
	// is set); Java shows the love-hearts entity event sent by SetInLove.
	if in := b.love > 0; m.Meta().InLove != in {
		m.UpdateMeta(func(meta *mob.Meta) { meta.InLove = in })
	}
}

// InLove reports whether the mob is in love mode.
func (b *Breedable) InLove() bool { return b.love > 0 }

// SetInLove puts the mob in love (cause is the player who fed it, or nil).
func (b *Breedable) SetInLove(m *mob.Mob, cause world.Entity) {
	b.love = loveTicks
	b.loveCause = nil
	if cause != nil {
		b.loveCause = cause.H()
	}
	m.UpdateMeta(func(meta *mob.Meta) { meta.InLove = true })
	ShowAction(m, LoveHeartsAction{})
}

// LoveCause returns the player who put the mob in love, if still there
// (BreedableComponent::getLoveCause).
func (b *Breedable) LoveCause(m *mob.Mob) (world.Entity, bool) {
	if b.loveCause == nil || m.Tx() == nil {
		return nil, false
	}
	return b.loveCause.Entity(m.Tx())
}

// ResetLove ends love mode and starts the breeding cooldown.
func (b *Breedable) ResetLove() {
	b.love, b.cooldown = 0, b.cooldownTicks
}

// Cooldown is the number of ticks before the mob may breed again.
func (b *Breedable) Cooldown() int { return b.cooldown }

// BreedableOf returns m's breedable component.
func BreedableOf(m *mob.Mob) (*Breedable, bool) { return mob.ComponentOf[*Breedable](m) }

// InLove reports whether m is in love mode.
func InLove(m *mob.Mob) bool {
	b, ok := BreedableOf(m)
	return ok && b.InLove()
}

func (b *Breedable) canBreed(m *mob.Mob, user item.User, held item.Stack) (breedItem, bool) {
	if held.Empty() || b.cooldown > 0 || IsBaby(m) || b.InLove() {
		return breedItem{}, false
	}
	if b.requireTame && !m.Meta().Tamed {
		return breedItem{}, false
	}
	if b.requireFullHealth && m.Health() < m.MaxHealth() {
		return breedItem{}, false
	}
	var it breedItem
	found := false
	for _, bi := range b.items {
		if bi.desc.Matches(held) {
			it, found = bi, true
			break
		}
	}
	if !found {
		return breedItem{}, false
	}
	return it, b.loveFilter.Test(InteractContext(m, user, held))
}

// Interact feeds a breed item.
func (b *Breedable) Interact(m *mob.Mob, user item.User, held item.Stack, _ mgl64.Vec3) (item.Stack, bool) {
	it, ok := b.canBreed(m, user, held)
	if !ok {
		return held, false
	}
	if !b.meetsEnvironment(m) {
		// BreedableComponent::getInteraction: the breed item is kept and the
		// mob plays its can't-breed sound (panda without bamboo around).
		PlayMobSound(m, "cant_breed")
		return held, false
	}
	var cause world.Entity
	if e, ok := user.(world.Entity); ok {
		cause = e
	}
	b.SetInLove(m, cause)
	PlayMobSound(m, "eat")
	left := ConsumeOne(user, held)
	if it.result != "" && !IsCreative(user) {
		if s, ok := ParseItemDescriptor(it.result).Stack(1); ok {
			left = ReplaceOne(m, user, held, s)
		}
	}
	return left, true
}

// breedTypeFor returns how m breeds with a partner of identifier id.
func (b *Breedable) breedTypeFor(m *mob.Mob, id string) (breedType, bool) {
	t, ok := b.with[id]
	if !ok {
		return breedType{}, false
	}
	if off, ok := mob.ComponentOf[*offspring](m); ok {
		if baby, ok := off.pairs[id]; ok {
			t.Baby = baby
		}
	}
	if t.Baby == "" {
		t.Baby = m.Identifier()
	}
	return t, true
}

// CanMate reports whether m and other may make a baby now (BDS
// BreedableComponent::canMate): both in love, off cooldown, compatible
// types and tame states.
func (b *Breedable) CanMate(m *mob.Mob, other world.Entity) bool {
	om, ok := mob.Of(other)
	if !ok || om.H() == m.H() || om.Dead() || b.cooldown > 0 {
		return false
	}
	ob, ok := BreedableOf(om)
	if !ok || ob.cooldown > 0 {
		return false
	}
	if _, ok := b.breedTypeFor(m, om.Identifier()); !ok {
		return false
	}
	if b.requireTame {
		if !m.Meta().Tamed || !om.Meta().Tamed {
			return false
		}
	} else if m.Meta().Tamed != om.Meta().Tamed {
		return false
	}
	if !b.allowSitting && (m.Meta().Sitting || om.Meta().Sitting) {
		return false
	}
	return b.InLove() && ob.InLove()
}

// Mate makes the baby (or babies, with extra_baby_chance) of m and partner
// and returns them (BDS BreedableComponent::mate / _handleMate).
func (b *Breedable) Mate(m, partner *mob.Mob) []*mob.Mob {
	if !b.InLove() {
		return nil
	}
	var babies []*mob.Mob
	for i := 0; i < 16; i++ {
		if baby := b.handleMate(m, partner); baby != nil {
			babies = append(babies, baby)
		}
		if m.Rand().Float64() >= b.extraBabyChance {
			break
		}
	}
	return babies
}

func (b *Breedable) handleMate(m, partner *mob.Mob) *mob.Mob {
	t, ok := b.breedTypeFor(m, partner.Identifier())
	if !ok {
		return nil
	}
	ev := t.Event
	if ev == "" {
		ev = mob.EventBorn
	}
	off, _ := mob.ComponentOf[*offspring](m)
	baby, err := mob.Spawn(m.Tx(), t.Baby, m.Position(), mob.SpawnOptions{
		Rotation: m.Rotation(),
		Event:    ev,
		Configure: func(baby *mob.Mob) {
			if off != nil {
				off.inheritProperties(m, partner, baby)
			}
		},
	})
	b.ResetLove()
	if pb, ok := BreedableOf(partner); ok {
		pb.ResetLove()
	}
	if err != nil {
		return nil
	}
	inh := b.inherit
	if off != nil {
		inh = off.inherit
	}
	if inh.blend {
		inh.apply(m, partner, baby)
	}
	if off != nil {
		off.inheritAfterBirth(m, partner, baby)
	}
	b.assignTrust(m, partner, baby)
	DropExperience(m, BredExperience(m))
	return baby
}

func (b *Breedable) EncodeNBT() map[string]any {
	return map[string]any{"InLove": int32(b.love), "BreedCooldown": int32(b.cooldown)}
}

func (b *Breedable) DecodeNBT(_ *mob.Mob, d map[string]any) {
	if v, ok := d["InLove"].(int32); ok {
		b.love = int(v)
	}
	if v, ok := d["BreedCooldown"].(int32); ok {
		b.cooldown = int(v)
	}
}

// offspring is minecraft:offspring (1.26): which baby each mate makes and
// what the baby inherits.
//
//	offspring_pairs        {mate identifier: baby identifier}
//	combine_parent_colors  mix the parents' minecraft:color (sheep)
//	property_inheritance   {property: {}} copied from a random parent
//	inherit_tamed          (default true) the baby is tamed by the owner
//	mutation_factor, mutation_strategy, random_*_mutation_interval,
//	deny_parents_variant, blend_attributes, parent_centric_attribute_blending
//	                       variant, markings, colour, attribute and strength
//	                       inheritance (inheritance, inherit.go)
type offspring struct {
	pairs         map[string]string
	combineColors bool
	properties    []string
	inheritTamed  bool
	inherit       inheritance
}

func newOffspring(key string, raw json.RawMessage) (mob.Component, error) {
	j := struct {
		Pairs         map[string]string `json:"offspring_pairs"`
		CombineColors bool              `json:"combine_parent_colors"`
		Properties    json.RawMessage   `json:"property_inheritance"`
		InheritTamed  *bool             `json:"inherit_tamed"`
	}{}
	if err := decode(raw, &j); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	inherit, err := parseInheritance(raw)
	if err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	o := &offspring{pairs: map[string]string{}, combineColors: j.CombineColors, inheritTamed: true, inherit: inherit}
	for k, v := range j.Pairs {
		o.pairs[normaliseID(k)] = normaliseID(v)
	}
	if j.InheritTamed != nil {
		o.inheritTamed = *j.InheritTamed
	}
	if len(bytes.TrimSpace(j.Properties)) > 0 {
		props, err := mob.ParseOrderedObject(j.Properties)
		if err != nil {
			return nil, fmt.Errorf("%v: property_inheritance: %w", key, err)
		}
		for _, p := range props {
			o.properties = append(o.properties, p.Key)
		}
	}
	return o, nil
}

func (*offspring) Attach(*mob.Mob) {}
func (*offspring) Detach(*mob.Mob) {}

func (o *offspring) parent(m, partner *mob.Mob) *mob.Mob {
	if m.Rand().IntN(2) == 0 {
		return partner
	}
	return m
}

// inheritProperties runs before the baby's born event, so the event's
// filters see the inherited properties.
func (o *offspring) inheritProperties(m, partner, baby *mob.Mob) {
	for _, p := range o.properties {
		if v, ok := o.parent(m, partner).Property(p); ok {
			_ = baby.SetProperty(p, v)
		}
	}
}

// inheritAfterBirth runs after the born event added the baby's groups.
func (o *offspring) inheritAfterBirth(m, partner, baby *mob.Mob) {
	if o.combineColors {
		SetColor(baby, mixColours(m, m.Meta().Color, partner.Meta().Color))
	}
	if o.inheritTamed && m.Meta().Tamed && m.Meta().Owner != nil {
		owner := m.Meta().Owner
		baby.UpdateMeta(func(meta *mob.Meta) { meta.Tamed, meta.Owner = true, owner })
	}
}

// colourMixes are the dye pairs that craft into another dye (Java
// Sheep.getOffspringColor uses the crafting recipe), by Bedrock colour index.
var colourMixes = map[[2]uint8]uint8{
	{0, 15}: 7, {0, 7}: 8, {4, 14}: 1, {0, 14}: 6, {0, 11}: 3, {11, 13}: 9,
	{11, 14}: 10, {6, 10}: 2, {0, 13}: 5,
}

func mixColours(m *mob.Mob, a, b uint8) uint8 {
	if a == b {
		return a
	}
	if c, ok := colourMixes[[2]uint8{min(a, b), max(a, b)}]; ok {
		return c
	}
	if m.Rand().IntN(2) == 0 {
		return a
	}
	return b
}
