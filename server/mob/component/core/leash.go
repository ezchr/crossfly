//go:build crossfly_full

package core

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// Leads, after BDS 1.21.3: LeashableDefinition (soft_distance 4,
// hard_distance 6, max_distance 10, on_leash, on_unleash, can_be_stolen),
// LeashableComponent::getInteraction/leash/unleash, the LeashableSystem tick
// (_tickLeashableComponent), Actor::setLeashHolder/dropLeash/isLeashed (actor
// flag LEASHED 30 and actor data LEASH_HOLDER 37, saved as LeasherID),
// LeadItem::_useOn/bindPlayerMobs, FenceBlock::use and LeashFenceKnotActor
// (leash_knot.go). The 1.26 JSON adds presets (distances picked by a filter
// on the holder when the mob is leashed) and unleash_on_removal.
//
// The leash state is kept in the mob's scratch data, not in the component,
// so it survives the component being rebuilt by a group change. The leashed
// flag and the holder data are what the clients and the filters read
// (is_leashed, is_leashed_to): package mob/filter reads them through
// Mob.ActorFlag and Mob.ActorData.

func init() {
	mob.RegisterComponent("minecraft:leashable", newLeashable)
	item.SetLeadBinder(func(tx *world.Tx, user item.User, pos cube.Pos) bool { return BindPlayerMobs(tx, user, pos) })
	block.SetFenceLeashBinder(func(pos cube.Pos, tx *world.Tx, user item.User) bool { return BindPlayerMobs(tx, user, pos) })
	mob.RegisterEntityType(KnotType)
}

// Defaults of LeashableDefinition (and of a 1.26 preset).
const (
	leashSoftDistance = 4.0
	leashHardDistance = 6.0
	leashMaxDistance  = 10.0
)

// leashKey keys the leash state in the mob's scratch data.
type leashKey struct{}

// leashState is the leash of one mob.
type leashState struct {
	// on is Actor::isLeashed (the LEASHED flag).
	on bool
	// holder is the holder's handle; nil while only id is known (a mob
	// loaded from disk, or a holder that came back with a new handle).
	holder *world.EntityHandle
	// id is the holder's UUID (BDS LeasherID).
	id uuid.UUID
	// preset is the index of the preset picked when the mob was leashed,
	// -1 for the defaults.
	preset int
}

func leashStateOf(m *mob.Mob) *leashState {
	v, _ := m.Data(leashKey{})
	st, _ := v.(*leashState)
	return st
}

func ensureLeashState(m *mob.Mob) *leashState {
	if st := leashStateOf(m); st != nil {
		return st
	}
	st := &leashState{preset: -1}
	m.SetData(leashKey{}, st)
	if c, ok := mob.ComponentOf[*leashable](m); ok {
		c.st = st
	}
	return st
}

// leashPreset is one entry of presets (or the defaults).
type leashPreset struct {
	filter          mob.Filter
	soft, hard, max float64
}

// leashable is minecraft:leashable.
type leashable struct {
	def              leashPreset
	presets          []leashPreset
	canBeStolen      bool
	unleashOnRemoval bool
	onLeash          *Trigger
	onUnleash        *Trigger

	// st is the mob's leash state (nil until it is first leashed).
	st *leashState
}

func newLeashable(key string, raw json.RawMessage) (mob.Component, error) {
	type preset struct {
		Filter json.RawMessage `json:"filter"`
		Soft   *float64        `json:"soft_distance"`
		Hard   *float64        `json:"hard_distance"`
		Max    *float64        `json:"max_distance"`
		// spring_type and rotation_adjustment (1.26) are not modelled:
		// every leash pulls like the 1.21.3 LeashableSystem.
	}
	var j struct {
		preset
		CanBeStolen      bool     `json:"can_be_stolen"`
		UnleashOnRemoval *bool    `json:"unleash_on_removal"`
		OnLeash          *Trigger `json:"on_leash"`
		OnUnleash        *Trigger `json:"on_unleash"`
		Presets          []preset `json:"presets"`
	}
	if err := decode(raw, &j); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	distances := func(p preset, def leashPreset) leashPreset {
		out := def
		if p.Soft != nil {
			out.soft = *p.Soft
		}
		if p.Hard != nil {
			out.hard = *p.Hard
		}
		if p.Max != nil {
			out.max = *p.Max
		}
		return out
	}
	base := leashPreset{soft: leashSoftDistance, hard: leashHardDistance, max: leashMaxDistance}
	c := &leashable{
		def:              distances(j.preset, base),
		canBeStolen:      j.CanBeStolen,
		unleashOnRemoval: j.UnleashOnRemoval == nil || *j.UnleashOnRemoval,
		onLeash:          j.OnLeash,
		onUnleash:        j.OnUnleash,
	}
	for _, p := range j.Presets {
		f, err := mob.CompileFilter(p.Filter)
		if err != nil {
			return nil, fmt.Errorf("%v: presets: %w", key, err)
		}
		lp := distances(p, base)
		lp.filter = f
		c.presets = append(c.presets, lp)
	}
	return c, nil
}

func (c *leashable) Attach(m *mob.Mob) { c.st = leashStateOf(m) }

// Detach unleashes the mob when the component goes away for good
// (unleash_on_removal, default true), not when a group change only rebuilds
// it. The on_unleash trigger of the removed component does not fire (BDS:
// dropLeash runs the trigger only while the mob has a LeashableComponent).
func (c *leashable) Detach(m *mob.Mob) {
	st := c.st
	c.st = nil
	if st == nil || !st.on || !c.unleashOnRemoval {
		return
	}
	if inst := m.Instance(); inst != nil {
		for _, rc := range inst.Components() {
			if rc.Key == "minecraft:leashable" {
				return
			}
		}
	}
	dropLeash(m, st, true, false, nil)
}

// distances are the soft, hard and max distances of the preset picked for
// the mob's leash.
func (c *leashable) distances(st *leashState) leashPreset {
	if st.preset >= 0 && st.preset < len(c.presets) {
		return c.presets[st.preset]
	}
	return c.def
}

// pickPreset is the first preset whose filter passes with the holder as
// other (-1: none, the defaults).
func (c *leashable) pickPreset(m *mob.Mob, holder world.Entity) int {
	if len(c.presets) == 0 {
		return -1
	}
	ctx := m.Ctx()
	ctx.Other = holder
	for i, p := range c.presets {
		if p.filter == nil || p.filter.Test(ctx) {
			return i
		}
	}
	return -1
}

// Tick is LeashableSystem's _tickLeashableComponent: a leash breaks when
// the holder is gone, dead or a spectator, or farther than max_distance;
// beyond soft_distance the mob paths to the holder, beyond hard_distance it
// is pulled towards it. Mobs that are not leashed return at once.
func (c *leashable) Tick(m *mob.Mob) {
	st := c.st
	if st == nil || !st.on {
		return
	}
	holder, ok := resolveLeashHolder(m, st)
	if !ok {
		// BDS: an unknown holder breaks the leash once the chunks around
		// the mob (its box grown by 16) are loaded; until then the mob
		// stands still.
		if leashChunksLoaded(m) {
			dropLeash(m, st, true, true, c)
		} else if m.HasNavigation() {
			m.Navigation().Stop(m)
		}
		return
	}
	if !leashHolderValid(holder) {
		dropLeash(m, st, true, true, c)
		return
	}
	p := c.distances(st)
	hp := holder.Position()
	d := hp.Sub(m.Position())
	d2 := d.LenSqr()
	if d2 > p.max*p.max {
		dropLeash(m, st, true, true, c)
		return
	}
	if m.Sitting() || hasPlayerPassenger(m) {
		return
	}
	if d2 > p.soft*p.soft && m.HasNavigation() {
		m.Navigation().MoveTo(m, hp, 1)
	}
	if d2 > p.hard*p.hard {
		// posDelta += |dir|*dir*0.2 per axis (dir normalised).
		dir := d.Mul(1 / math.Sqrt(d2))
		m.SetVelocity(m.Vel().Add(mgl64.Vec3{abs(dir[0]) * dir[0] * 0.2, abs(dir[1]) * dir[1] * 0.2, abs(dir[2]) * dir[2] * 0.2}))
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// OnDeath drops the leash and its lead (Actor::die -> dropLeash(true, false)).
func (c *leashable) OnDeath(m *mob.Mob, _ world.DamageSource) {
	if st := c.st; st != nil && st.on {
		dropLeash(m, st, true, false, c)
	}
}

// Interact is LeashableComponent::getInteraction: a player holding a lead
// leashes a mob that is not leashed (or whose leash can_be_stolen from a mob
// holder: not from a player or a fence knot), using the lead up outside
// creative; the holder using the mob unleashes it (the lead drops).
func (c *leashable) Interact(m *mob.Mob, user item.User, held item.Stack, _ mgl64.Vec3) (item.Stack, bool) {
	st := c.st
	leashed := st != nil && st.on
	steal := false
	if leashed && c.canBeStolen {
		if h, ok := resolveLeashHolder(m, st); ok {
			_, isPlayer := h.(*player.Player)
			steal = !isPlayer && h.H().Type() != KnotType
		}
	}
	if !leashed || steal {
		if _, ok := held.Item().(item.Lead); !ok || held.Empty() {
			return held, false
		}
		if steal {
			dropLeash(m, st, false, true, c)
		}
		Leash(m, user)
		return ConsumeOne(user, held), true
	}
	if leashHeldBy(st, user) {
		dropLeash(m, st, true, false, c)
		return held, true
	}
	return held, false
}

// EncodeNBT saves the holder as LeasherID (its UUID) and the picked preset.
func (c *leashable) EncodeNBT() map[string]any {
	st := c.st
	if st == nil || !st.on || st.id == uuid.Nil {
		return nil
	}
	return map[string]any{"LeasherID": st.id.String(), "Preset": int32(st.preset)}
}

// DecodeNBT restores the leash; the holder is looked up by its UUID on the
// next tick.
func (c *leashable) DecodeNBT(m *mob.Mob, d map[string]any) {
	s, _ := d["LeasherID"].(string)
	id, err := uuid.Parse(s)
	if err != nil || id == uuid.Nil {
		return
	}
	st := ensureLeashState(m)
	c.st = st
	st.on, st.id, st.holder, st.preset = true, id, nil, -1
	if p, ok := d["Preset"].(int32); ok {
		st.preset = int(p)
	}
	m.SetActorFlag(protocol.EntityDataFlagLeashed, true)
}

// Leash leashes m to holder (LeashableComponent::leash): the holder is set,
// the mob gets off what it rides and its on_leash trigger fires.
func Leash(m *mob.Mob, holder world.Entity) {
	if holder == nil || holder.H() == m.H() {
		return
	}
	setLeashHolder(m, holder)
	if m.Riding() {
		m.StopRiding()
	}
	if c, ok := mob.ComponentOf[*leashable](m); ok && c.onLeash != nil {
		c.onLeash.Fire(m, nil)
	}
}

// setLeashHolder is Actor::setLeashHolder: no triggers. The preset is
// picked again for the new holder.
func setLeashHolder(m *mob.Mob, holder world.Entity) {
	st := ensureLeashState(m)
	st.on, st.holder, st.id, st.preset = true, holder.H(), holder.H().UUID(), -1
	if c, ok := mob.ComponentOf[*leashable](m); ok {
		st.preset = c.pickPreset(m, holder)
	}
	m.SetActorFlag(protocol.EntityDataFlagLeashed, true)
	m.SetActorData(protocol.EntityDataKeyLeashHolder, holder.H())
}

// Unleash drops m's leash and the lead (LeashableComponent::unleash).
func Unleash(m *mob.Mob) { DropLeash(m, true, false) }

// DropLeash is Actor::dropLeash: it unleashes m, drops a lead if drop, shows
// the leash breaking (snap: the rope is cut in two, as when it was pulled
// too far) and runs on_unleash.
func DropLeash(m *mob.Mob, drop, snap bool) {
	if st := leashStateOf(m); st != nil && st.on {
		c, _ := mob.ComponentOf[*leashable](m)
		dropLeash(m, st, drop, snap, c)
	}
}

func dropLeash(m *mob.Mob, st *leashState, drop, snap bool, c *leashable) {
	if !st.on {
		return
	}
	tx := m.Tx()
	var old world.Entity
	if st.holder != nil && tx != nil {
		old, _ = st.holder.Entity(tx)
	}
	st.on, st.holder, st.id, st.preset = false, nil, uuid.Nil, -1
	if tx != nil {
		for _, v := range tx.Viewers(m.Position()) {
			v.ViewEntityAction(m, LeashDestroyedAction{Snap: snap})
		}
	}
	m.SetActorFlag(protocol.EntityDataFlagLeashed, false)
	if k, ok := old.(*Knot); ok && !k.removing() && leashedToCount(tx, k) == 0 {
		k.remove(false)
	}
	m.SetActorData(protocol.EntityDataKeyLeashHolder, int64(-1))
	if drop && tx != nil {
		DropItems(m, []item.Stack{item.NewStack(item.Lead{}, 1)}, 1)
	}
	if c != nil && c.onUnleash != nil {
		c.onUnleash.Fire(m, nil)
	}
}

// Leashed reports whether m is leashed (Actor::isLeashed).
func Leashed(m *mob.Mob) bool {
	st := leashStateOf(m)
	return st != nil && st.on
}

// LeashHolder returns the entity m is leashed to, if it is in m's world.
func LeashHolder(m *mob.Mob) (world.Entity, bool) {
	st := leashStateOf(m)
	if st == nil || !st.on {
		return nil, false
	}
	return resolveLeashHolder(m, st)
}

// leashHeldBy reports whether e is the holder of the leash.
func leashHeldBy(st *leashState, e world.Entity) bool {
	if st == nil || !st.on || e == nil {
		return false
	}
	return st.holder == e.H() || (st.id != uuid.Nil && st.id == e.H().UUID())
}

// resolveLeashHolder returns the holder: its handle, or else the entity with
// its UUID (a player that joined again, a mob loaded from disk) within 16
// blocks of the mob's box, which becomes the handle.
func resolveLeashHolder(m *mob.Mob, st *leashState) (world.Entity, bool) {
	tx := m.Tx()
	if tx == nil {
		return nil, false
	}
	if st.holder != nil {
		if e, ok := st.holder.Entity(tx); ok {
			return e, true
		}
	}
	if st.id == uuid.Nil {
		return nil, false
	}
	var found world.Entity
	for p := range tx.Players() {
		if p.H().UUID() == st.id {
			found = p
			break
		}
	}
	if found == nil {
		for e := range tx.EntitiesWithin(m.BBox().Grow(16)) {
			if e.H().UUID() == st.id {
				found = e
				break
			}
		}
	}
	if found == nil {
		return nil, false
	}
	if st.holder != found.H() {
		st.holder = found.H()
		m.SetActorData(protocol.EntityDataKeyLeashHolder, found.H())
	}
	return found, true
}

// leashChunksLoaded reports whether the chunks under the mob's box grown by
// 16 are all loaded (BDS ChunksLoadedInfo::areAllChunksLoadedAndTicking).
func leashChunksLoaded(m *mob.Mob) bool {
	tx := m.Tx()
	box := m.BBox().Grow(16)
	y := int(m.Position()[1])
	for x := int(box.Min()[0]) >> 4; x <= int(box.Max()[0])>>4; x++ {
		for z := int(box.Min()[2]) >> 4; z <= int(box.Max()[2])>>4; z++ {
			if _, ok := tx.BlockLoaded(cube.Pos{x << 4, y, z << 4}); !ok {
				return false
			}
		}
	}
	return true
}

// leashHolderValid: the holder is alive and not a spectator.
func leashHolderValid(e world.Entity) bool {
	if l, ok := e.(interface{ Dead() bool }); ok && l.Dead() {
		return false
	}
	if g, ok := e.(interface{ GameMode() world.GameMode }); ok && g.GameMode() == world.GameModeSpectator {
		return false
	}
	return true
}

// hasPlayerPassenger reports whether a player rides m.
func hasPlayerPassenger(m *mob.Mob) bool {
	if m.RiderCount() == 0 {
		return false
	}
	for _, p := range m.Passengers() {
		if _, ok := p.(*player.Player); ok {
			return true
		}
	}
	return false
}

// leashedTo returns the mobs within 10 blocks of e that are leashed to it
// (Actor::forEachLeashedActor).
func leashedTo(tx *world.Tx, e world.Entity) []*mob.Mob {
	var out []*mob.Mob
	p := e.Position()
	box := cube.Box(p[0]-10, p[1]-10, p[2]-10, p[0]+10, p[1]+10, p[2]+10)
	for o := range tx.EntitiesWithin(box) {
		if m, ok := mob.Of(o); ok {
			if st := leashStateOf(m); st != nil && st.on && st.holder == e.H() {
				out = append(out, m)
			}
		}
	}
	return out
}

func leashedToCount(tx *world.Tx, e world.Entity) int {
	if tx == nil {
		return 0
	}
	return len(leashedTo(tx, e))
}

// BindPlayerMobs is LeadItem::bindPlayerMobs: every mob within 7 blocks of
// the fence at pos that user leads is tied to the fence knot there (made if
// there is none yet), without leash events. It reports whether a mob was
// tied. A lead used on a fence (LeadItem::_useOn) and a player using a fence
// (FenceBlock::use) run it.
func BindPlayerMobs(tx *world.Tx, user world.Entity, pos cube.Pos) bool {
	if user == nil {
		return false
	}
	x, y, z := float64(pos[0]), float64(pos[1]), float64(pos[2])
	var mobs []*mob.Mob
	for e := range tx.EntitiesWithin(cube.Box(x-7, y-7, z-7, x+7, y+7, z+7)) {
		if m, ok := mob.Of(e); ok && leashHeldBy(leashStateOf(m), user) {
			mobs = append(mobs, m)
		}
	}
	if len(mobs) == 0 {
		return false
	}
	k := knotAt(tx, pos)
	if k == nil {
		k = newKnot(tx, pos)
		if k == nil {
			return false
		}
	}
	for _, m := range mobs {
		setLeashHolder(m, k)
	}
	tx.PlaySound(k.Position(), sound.Custom{Name: "leashknot.place"})
	return true
}

// LeashDestroyedAction is shown when a leash is dropped (ActorEvent
// LEASH_DESTROYED with data 1 when the rope snaps).
type LeashDestroyedAction struct {
	// Snap: the leash broke (too far from its holder), as opposed to being
	// taken off.
	Snap bool
}

func (LeashDestroyedAction) EntityAction()     {}
func (LeashDestroyedAction) ActorEvent() uint8 { return packet.ActorEventLeashDestroyed }

// ActorEventData is the event's data: 1 when the rope snaps.
func (a LeashDestroyedAction) ActorEventData() int32 {
	if a.Snap {
		return 1
	}
	return 0
}
