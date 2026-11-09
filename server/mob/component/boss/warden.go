//go:build crossfly_full

package boss

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// WardenID is the warden's identifier.
const WardenID = "minecraft:warden"

// --- minecraft:anger_level (BDS AngerLevelComponent/AngerLevelSystem) ---

// angerSound is one on_increase_sounds entry.
type angerSound struct {
	sound string
	cond  core.Expr
}

// nuisance is one entity the mob is angry at.
type nuisance struct {
	h      *world.EntityHandle
	anger  int
	player bool
}

// AngerLevel is minecraft:anger_level: anger per entity, raised by
// vibrations, sniffing, hits and pushes, falling by 1 every
// anger_decrement_interval. The entity it is angriest at (players first)
// past angry_threshold is its top nuisance; the roar goal makes that the
// target, and remove_targets_below_angry_threshold drops a target whose
// anger fell below it.
//
//	default_annoyingness             0 (warden 35)
//	default_projectile_annoyingness  0 (warden 10)
//	max_anger                        100 (warden 150)
//	angry_threshold                  80
//	angry_boost                      20
//	anger_decrement_interval         1 s
//	remove_targets_below_angry_threshold true
//	nuisance_filter, on_increase_sounds [{sound, condition}]
//
// It also holds the warden's hard-coded reactions (Warden::_hurt,
// isInvulnerableTo, setTarget): hits make it furious (threshold + boost) at
// the attacker, it cannot be hurt while digging or emerging, and a new
// target delays the sonic boom by 10 s.
type AngerLevel struct {
	Annoyingness, ProjectileAnnoyingness int
	MaxAnger, Threshold, Boost           int
	interval                             int
	removeBelow                          bool
	filter                               mob.Filter
	sounds                               []angerSound

	list    []*nuisance
	next    int64
	maxSeen int
	cur     *world.EntityHandle
}

func newAngerLevel(key string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		Annoy       *int            `json:"default_annoyingness"`
		ProjAnnoy   *int            `json:"default_projectile_annoyingness"`
		MaxAnger    *int            `json:"max_anger"`
		Threshold   *int            `json:"angry_threshold"`
		Boost       *int            `json:"angry_boost"`
		Interval    *float64        `json:"anger_decrement_interval"`
		RemoveBelow *bool           `json:"remove_targets_below_angry_threshold"`
		Filter      json.RawMessage `json:"nuisance_filter"`
		Sounds      []struct {
			Sound     string `json:"sound"`
			Condition string `json:"condition"`
		} `json:"on_increase_sounds"`
	}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	a := &AngerLevel{
		Annoyingness: or(v.Annoy, 0), ProjectileAnnoyingness: or(v.ProjAnnoy, 0),
		MaxAnger: or(v.MaxAnger, 100), Threshold: or(v.Threshold, 80), Boost: or(v.Boost, 20),
		interval: max(1, Ticks(or(v.Interval, 1))), removeBelow: or(v.RemoveBelow, true),
	}
	f, err := mob.CompileFilter(v.Filter)
	if err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	a.filter = f
	for _, s := range v.Sounds {
		var cond core.Expr = core.Const(1)
		if s.Condition != "" {
			if cond, err = core.CompileMolang(s.Condition); err != nil {
				return nil, fmt.Errorf("%v: %w", key, err)
			}
		}
		a.sounds = append(a.sounds, angerSound{sound: s.Sound, cond: cond})
	}
	return a, nil
}

// AngerOf returns m's anger_level component.
func AngerOf(m *mob.Mob) (*AngerLevel, bool) { return mob.ComponentOf[*AngerLevel](m) }

// Attach ...
func (a *AngerLevel) Attach(m *mob.Mob) { a.next = m.Ticks() + int64(a.interval) }

// Detach ...
func (*AngerLevel) Detach(*mob.Mob) {}

// CanBeNuisance is AngerLevelComponent::canBeNuisance: alive, passing
// nuisance_filter, and an attackable player or a mob.
func (a *AngerLevel) CanBeNuisance(m *mob.Mob, e world.Entity) bool {
	if e == nil || e.H() == m.H() || !Living(e) {
		return false
	}
	ctx := m.Ctx()
	ctx.Other = e
	if !a.filter.Test(ctx) {
		return false
	}
	if sense.IsPlayer(e) {
		return !sense.Invulnerable(e)
	}
	_, ok := mob.Of(e)
	return ok
}

func (a *AngerLevel) find(h *world.EntityHandle) *nuisance {
	for _, n := range a.list {
		if n.h == h {
			return n
		}
	}
	return nil
}

// AngerAt returns the anger at e.
func (a *AngerLevel) AngerAt(e world.Entity) int {
	if e == nil {
		return 0
	}
	if n := a.find(e.H()); n != nil {
		return n.anger
	}
	return 0
}

// sorted orders the list as NuisanceCompare: angry ones first, players
// before mobs, then by anger.
func (a *AngerLevel) sort() {
	slices.SortStableFunc(a.list, func(x, y *nuisance) int {
		xa, ya := x.anger >= a.Threshold, y.anger >= a.Threshold
		if xa != ya {
			if xa {
				return -1
			}
			return 1
		}
		if x.player != y.player {
			if x.player {
				return -1
			}
			return 1
		}
		return y.anger - x.anger
	})
}

// modify is _createOrModifyAngerLevel.
func (a *AngerLevel) modify(e world.Entity, f func(int) int) int {
	n := a.find(e.H())
	old := 0
	if n != nil {
		old = n.anger
	}
	v := f(old)
	if v > a.MaxAnger {
		v = a.MaxAnger
	}
	v = max(v, 0)
	a.maxSeen = max(a.maxSeen, v)
	switch {
	case n == nil && v > 0:
		a.list = append(a.list, &nuisance{h: e.H(), anger: v, player: sense.IsPlayer(e)})
	case n != nil && v > 0:
		n.anger = v
	case n != nil:
		a.list = slices.DeleteFunc(a.list, func(x *nuisance) bool { return x == n })
	}
	a.sort()
	return v
}

// IncreaseAngerAt raises the anger at e by amount with the
// on_increase_sounds (goal/misc sniff calls it).
func (a *AngerLevel) IncreaseAngerAt(m *mob.Mob, e world.Entity, amount int) {
	a.TryIncreaseAngerAt(m, e, amount, true)
}

// TryIncreaseAngerAt is AngerLevelComponent::tryIncreaseAngerAt: e must be
// a nuisance; the first on_increase_sounds entry whose condition holds
// plays; a player becoming angry-worthy replaces a mob target.
func (a *AngerLevel) TryIncreaseAngerAt(m *mob.Mob, e world.Entity, amount int, sound bool) {
	if !a.CanBeNuisance(m, e) {
		return
	}
	v := a.modify(e, func(old int) int { return old + amount })
	if sound {
		for _, s := range a.sounds {
			if s.cond.Eval(m) > 0 {
				m.PlaySound(s.sound)
				break
			}
		}
	}
	if t, ok := m.Target(); ok && !sense.IsPlayer(t) && sense.IsPlayer(e) && v >= a.Threshold {
		m.ResetTarget()
	}
}

// IsAngry reports whether the angriest nuisance is past the threshold.
func (a *AngerLevel) IsAngry() bool {
	if len(a.list) == 0 {
		return a.Threshold <= 0
	}
	return a.list[0].anger >= a.Threshold
}

// TopNuisance is getTopActiveNuisance: the first loaded valid nuisance.
func (a *AngerLevel) TopNuisance(m *mob.Mob) (world.Entity, bool) {
	tx := m.Tx()
	for _, n := range a.list {
		if e, ok := n.h.Entity(tx); ok && a.CanBeNuisance(m, e) {
			return e, true
		}
	}
	return nil, false
}

// BelowThreshold reports whether the anger at e is below angry_threshold.
func (a *AngerLevel) BelowThreshold(e world.Entity) bool { return a.AngerAt(e) < a.Threshold }

// AngerLevel is query.anger_level: the target's anger when it is in the
// list, else the highest anger seen.
func (a *AngerLevel) AngerLevel() int {
	if a.cur != nil {
		if n := a.find(a.cur); n != nil {
			return n.anger
		}
	}
	return a.maxSeen
}

// Tick is AngerLevelSystem: every interval all anger falls by 1 (invalid
// entries go); a target below the threshold is dropped.
func (a *AngerLevel) Tick(m *mob.Mob) {
	a.cur = m.TargetHandle()
	tx := m.Tx()
	for m.Ticks() >= a.next {
		a.next += int64(a.interval)
		best := 0
		a.list = slices.DeleteFunc(a.list, func(n *nuisance) bool {
			n.anger--
			if e, ok := n.h.Entity(tx); ok {
				if !a.CanBeNuisance(m, e) {
					return true
				}
				best = max(best, n.anger)
			}
			return n.anger < 0
		})
		a.list = slices.DeleteFunc(a.list, func(n *nuisance) bool { return n.anger <= 0 })
		a.maxSeen = best
		a.sort()
	}
	if t, ok := m.Target(); ok && a.removeBelow && a.BelowThreshold(t) {
		m.ResetTarget()
	}
	angry := a.IsAngry() && len(a.list) > 0
	if m.Meta().Angry != angry {
		m.UpdateMeta(func(meta *mob.Meta) { meta.Angry = angry })
	}
}

// ModifyDamage: a digging or emerging warden cannot be hurt
// (Warden::isInvulnerableTo).
func (a *AngerLevel) ModifyDamage(m *mob.Mob, d *mob.Damage) {
	if m.ActorFlag(protocol.EntityDataFlagDigging) || m.ActorFlag(protocol.EntityDataFlagEmerging) {
		if _, void := d.Source.(entity.VoidDamageSource); !void {
			d.Cancel = true
		}
	}
}

// OnHurt is Warden::_hurt: the attacker (a projectile's shooter) gets
// threshold + boost anger, and becomes the target when there is none,
// unless it shot from more than 5 blocks.
func (a *AngerLevel) OnHurt(m *mob.Mob, _ float64, src world.DamageSource) {
	if m.Identifier() != WardenID {
		return
	}
	attacker := attackerOf(src)
	if attacker == nil || attacker.H() == m.H() {
		return
	}
	a.TryIncreaseAngerAt(m, attacker, a.Threshold+a.Boost, false)
	if _, ok := m.Target(); ok || !a.CanBeNuisance(m, attacker) {
		return
	}
	_, projectile := src.(entity.ProjectileDamageSource)
	if !projectile || attacker.Position().Sub(m.Position()).Len() <= 5 {
		m.SetTarget(attacker)
	}
}

// OnTargetChanged is Warden::setTarget: a new target delays the sonic boom
// by 200 ticks.
func (a *AngerLevel) OnTargetChanged(m *mob.Mob, _, n world.Entity) {
	if m.Identifier() == WardenID && n != nil {
		WardenOf(m).SonicCooldown = m.Ticks() + 200
	}
}

// --- The warden's own state (Warden) ---

type wardenKey struct{}

// Warden is the state BDS keeps on the Warden actor.
type Warden struct {
	// SonicCooldown is the tick before which no sonic boom starts.
	SonicCooldown int64
	lastPush      int64
}

// WardenOf returns m's warden state.
func WardenOf(m *mob.Mob) *Warden {
	if v, ok := m.Data(wardenKey{}); ok {
		return v.(*Warden)
	}
	w := &Warden{}
	m.SetData(wardenKey{}, w)
	return w
}

// wardenTick runs after the goals: a melee hit delays the sonic boom by 100
// ticks (Warden::doHurtTarget); being pushed by a nuisance makes the
// warden suspicious and annoyed, at most once a second (Warden::onPush).
func wardenTick(m *mob.Mob) {
	if m.Identifier() != WardenID {
		return
	}
	w := WardenOf(m)
	if e, ok := m.LastHurtMob(); ok && e != nil {
		w.SonicCooldown = m.Ticks() + 100
		m.SetLastHurtMob(nil)
	}
	if m.Ticks()-w.lastPush < 20 {
		return
	}
	a, ok := AngerOf(m)
	if !ok {
		return
	}
	for e := range m.Tx().EntitiesWithin(m.BBox()) {
		if e.H() == m.H() || !a.CanBeNuisance(m, e) {
			continue
		}
		w.lastPush = m.Ticks()
		if s, ok := SuspectOf(m); ok {
			s.SetSuspiciousPos(m, cube.PosFromVec3(e.Position()))
		}
		a.TryIncreaseAngerAt(m, e, a.Annoyingness, true)
		break
	}
}

// --- minecraft:suspect_tracking (BDS SuspectTrackingComponent) ---

// SuspectTracking is minecraft:suspect_tracking: the suspicious position
// the investigate goal walks to, and when it was last set.
type SuspectTracking struct {
	pos      *cube.Pos
	LastTick int64
}

func newSuspectTracking(string, json.RawMessage) (mob.Component, error) {
	return &SuspectTracking{LastTick: -1 << 40}, nil
}

// SuspectOf returns m's suspect_tracking component.
func SuspectOf(m *mob.Mob) (*SuspectTracking, bool) { return mob.ComponentOf[*SuspectTracking](m) }

// Attach ...
func (*SuspectTracking) Attach(*mob.Mob) {}

// Detach ...
func (*SuspectTracking) Detach(*mob.Mob) {}

// SuspiciousPos returns the suspicious position.
func (s *SuspectTracking) SuspiciousPos() (cube.Pos, bool) {
	if s.pos == nil {
		return cube.Pos{}, false
	}
	return *s.pos, true
}

// SetSuspiciousPos sets it now.
func (s *SuspectTracking) SetSuspiciousPos(m *mob.Mob, p cube.Pos) {
	s.pos = &p
	s.LastTick = m.Ticks()
}

// Clear forgets the position (the time stays).
func (s *SuspectTracking) Clear() { s.pos = nil }

// --- minecraft:vibration_listener (BDS VibrationListener, WardenVibrationConfig) ---

// Vibration game events (BDS GameEvent ids) with their frequencies
// (VibrationListenerUtils::getGameEventFrequency by GameEvent id: entity_move
// 21 and swim 60 and flap 27 are 1, hit_ground 30 and projectile_land 38 are
// 2, projectile_shoot 39 is 3, entity_damage 16 is 7, explode 26 is 15).
const (
	VibrationStep           = "entity_move"
	VibrationSwim           = "swim"
	VibrationHitGround      = "hit_ground"
	VibrationProjectileLand = "projectile_land"
	VibrationProjectileShot = "projectile_shoot"
	VibrationDamage         = "entity_damage"
	VibrationExplode        = "explode"
	VibrationFlap           = "flap"
)

var vibrationFrequency = map[string]int{
	VibrationStep: 1, VibrationSwim: 1, VibrationHitGround: 2, VibrationProjectileLand: 2,
	VibrationProjectileShot: 3, VibrationDamage: 7, VibrationExplode: 15, VibrationFlap: 1,
}

// Vibration is one signal heard.
type Vibration struct {
	Event string
	Pos   mgl64.Vec3
	// Source is the entity that made it, Owner a projectile's shooter.
	Source, Owner *world.EntityHandle
	dist          float64
	arrive        int64
}

// VibrationListener is minecraft:vibration_listener: vibrations within 16
// blocks of the head reach the listener after distance ticks, at most one
// every 40 ticks (WardenVibrationConfig cooldown), the closest of a tick
// winning (a higher frequency on ties). Not while digging or emerging,
// not from itself, from sneaking entities, entities with
// minecraft:vibration_damper or steps on wool; a line through wool
// blocks it.
//
// Dragonfly has no game events: the listener notices entities that move
// (steps, swimming, landing; game_event_movement_tracking may turn steps and
// swims off), projectiles flying or landing near it, mobs being hurt
// (HurtHook) and explosions near it; flapping mobs send their flaps to it
// (MovementTracking).
//
// A warden reacts as WardenVibrationConfig::onSignalReceive: actor event
// 77 (tendrils), anger at the source (default_annoyingness; a projectile's
// shooter within 30 gets default_projectile_annoyingness, or the full
// annoyingness for a second shot within 5 s) and, while not angry, the
// position becomes suspicious.
type VibrationListener struct {
	Range    float64
	cooldown int

	lastAccept int64
	// LastTick is the tick of the last vibration received
	// (VibrationDataComponent), LastPos where it came from.
	LastTick       int64
	LastPos        mgl64.Vec3
	lastProjectile int64
	candidate      *Vibration
	flying         *Vibration
	seen           map[*world.EntityHandle]seenEntity
	// Received counts the signals received (tests).
	Received int
}

type seenEntity struct {
	pos      mgl64.Vec3
	onGround bool
	tick     int64
}

func newVibrationListener(string, json.RawMessage) (mob.Component, error) {
	return &VibrationListener{Range: 16, cooldown: 40, lastAccept: -1 << 40, lastProjectile: -1 << 40}, nil
}

// ListenerOf returns m's vibration_listener.
func ListenerOf(m *mob.Mob) (*VibrationListener, bool) { return mob.ComponentOf[*VibrationListener](m) }

// Attach ...
func (v *VibrationListener) Attach(m *mob.Mob) {
	v.LastTick = m.Ticks()
	addListener(m)
}

// Detach ...
func (*VibrationListener) Detach(m *mob.Mob) {
	if tx := m.Tx(); tx != nil {
		forgetListener(tx, m.H())
	}
}

// dampens reports whether b is wool or carpet (BlockLegacy::canDamperVibrations).
func dampens(b world.Block) bool {
	switch b.(type) {
	case block.Wool, block.Carpet:
		return true
	}
	return false
}

// occluded reports whether wool lies on the line from a to b.
func occluded(tx *world.Tx, a, b mgl64.Vec3) bool {
	d := b.Sub(a)
	n := int(d.Len() * 2)
	for i := 1; i < n; i++ {
		p := cube.PosFromVec3(a.Add(d.Mul(float64(i) / float64(n))))
		if dampens(tx.Block(p)) {
			return true
		}
	}
	return false
}

// Hear offers a vibration to the listener (VibrationListener::
// handleGameEvent): it is checked, then competes with the other signals of
// this tick.
func (v *VibrationListener) Hear(m *mob.Mob, vib Vibration) {
	if v.flying != nil {
		return
	}
	tx := m.Tx()
	head := m.EyePosition()
	if vib.Pos.Sub(head).Len() > v.Range {
		return
	}
	if m.Ticks()-v.lastAccept < int64(v.cooldown) {
		return
	}
	if m.ActorFlag(protocol.EntityDataFlagDigging) || m.ActorFlag(protocol.EntityDataFlagEmerging) {
		return
	}
	if vib.Source == m.H() {
		return
	}
	var src world.Entity
	if vib.Source != nil {
		src, _ = vib.Source.Entity(tx)
	}
	if src != nil {
		if om, ok := mob.Of(src); ok && om.HasComponent("minecraft:vibration_damper") {
			return
		}
		if a, ok := AngerOf(m); ok && m.Identifier() == WardenID && !a.CanBeNuisance(m, src) && src.H().Type() != entity.ItemType {
			if vib.Owner == nil {
				return
			}
		}
	}
	v.lastAccept = m.Ticks()
	if occluded(tx, vib.Pos, head) {
		return
	}
	vib.dist = math.Sqrt(cube.PosFromVec3(vib.Pos).Vec3().Sub(cube.PosFromVec3(head).Vec3()).LenSqr())
	if c := v.candidate; c != nil {
		if c.dist < vib.dist-0.005 || (math.Abs(c.dist-vib.dist) <= 0.005 && vibrationFrequency[c.Event] >= vibrationFrequency[vib.Event]) {
			return
		}
	}
	v.candidate = &vib
}

// Tick sends the chosen vibration on its way and delivers the one that
// arrived.
func (v *VibrationListener) Tick(m *mob.Mob) {
	v.scan(m)
	if v.flying == nil && v.candidate != nil {
		c := v.candidate
		v.candidate = nil
		c.arrive = m.Ticks() + int64(c.dist)
		v.flying = c
	}
	if v.flying != nil && m.Ticks() >= v.flying.arrive {
		vib := *v.flying
		v.flying = nil
		v.receive(m, vib)
	}
}

// scan turns what moved around the listener into vibrations.
func (v *VibrationListener) scan(m *mob.Mob) {
	tx := m.Tx()
	if v.seen == nil {
		v.seen = map[*world.EntityHandle]seenEntity{}
	}
	now := m.Ticks()
	for e := range tx.EntitiesWithin(m.BBox().Grow(v.Range)) {
		if e.H() == m.H() {
			continue
		}
		pos := e.Position()
		last, known := v.seen[e.H()]
		onGround := entityOnGround(e)
		v.seen[e.H()] = seenEntity{pos: pos, onGround: onGround, tick: now}
		if isProjectile(e) {
			owner := projectileOwner(e)
			if !known {
				v.Hear(m, Vibration{Event: VibrationProjectileShot, Pos: pos, Source: e.H(), Owner: owner})
			} else if pos.Sub(last.pos).LenSqr() < 1e-6 && e.H().Type() != entity.ItemType {
				if last.pos != pos || !last.onGround {
					v.Hear(m, Vibration{Event: VibrationProjectileLand, Pos: pos, Source: e.H(), Owner: owner})
				}
				v.seen[e.H()] = seenEntity{pos: pos, onGround: true, tick: now}
			}
			continue
		}
		if !known || !IsLiving(e) || sense.Sneaking(e) {
			continue
		}
		moved := pos.Sub(last.pos)
		switch {
		case onGround && !last.onGround && moved[1] < -0.3:
			v.Hear(m, Vibration{Event: VibrationHitGround, Pos: pos, Source: e.H()})
		case math.Hypot(moved[0], moved[2]) > 0.05 && onGround && tracksMove(e, false):
			if dampens(tx.Block(cube.PosFromVec3(pos).Side(cube.FaceDown))) || dampens(tx.Block(cube.PosFromVec3(pos))) {
				continue
			}
			v.Hear(m, Vibration{Event: VibrationStep, Pos: pos, Source: e.H()})
		case moved.Len() > 0.05 && inWater(tx, pos) && tracksMove(e, true):
			v.Hear(m, Vibration{Event: VibrationSwim, Pos: pos, Source: e.H()})
		}
	}
	for h, s := range v.seen {
		if s.tick != now {
			delete(v.seen, h)
		}
	}
}

func entityOnGround(e world.Entity) bool {
	if m, ok := mob.Of(e); ok {
		return m.OnGround()
	}
	if g, ok := e.(interface{ OnGround() bool }); ok {
		return g.OnGround()
	}
	return false
}

func isProjectile(e world.Entity) bool {
	if ent, ok := e.(*entity.Ent); ok {
		_, ok := ent.Behaviour().(*entity.ProjectileBehaviour)
		return ok
	}
	return false
}

func inWater(tx *world.Tx, pos mgl64.Vec3) bool {
	l, ok := tx.Liquid(cube.PosFromVec3(pos))
	if !ok {
		return false
	}
	_, water := l.(block.Water)
	return water
}

// receive is WardenVibrationConfig::onSignalReceive.
func (v *VibrationListener) receive(m *mob.Mob, vib Vibration) {
	tx := m.Tx()
	v.Received++
	v.LastTick, v.LastPos = m.Ticks(), vib.Pos
	if m.Identifier() != WardenID {
		return
	}
	ShowActorEvent(tx, m, packet.ActorEventVibrationDetected)
	a, ok := AngerOf(m)
	if !ok {
		return
	}
	s, hasSuspect := SuspectOf(m)
	var source world.Entity
	if vib.Source != nil {
		source, _ = vib.Source.Entity(tx)
	}
	var owner world.Entity
	if vib.Owner != nil {
		owner, _ = vib.Owner.Entity(tx)
	}
	switch {
	case owner != nil:
		if a.CanBeNuisance(m, owner) && owner.Position().Sub(m.Position()).Len() <= 30 {
			if m.Ticks()-v.lastProjectile <= 100 {
				if hasSuspect {
					s.SetSuspiciousPos(m, cube.PosFromVec3(owner.Position()))
				}
				a.TryIncreaseAngerAt(m, owner, a.Annoyingness, true)
			} else {
				a.TryIncreaseAngerAt(m, owner, a.ProjectileAnnoyingness, true)
			}
		}
		v.lastProjectile = m.Ticks()
	case source != nil:
		a.TryIncreaseAngerAt(m, source, a.Annoyingness, true)
	}
	if !a.IsAngry() && hasSuspect {
		top, hasTop := a.TopNuisance(m)
		if owner != nil || !hasTop || (source != nil && top.H() == source.H()) {
			s.SetSuspiciousPos(m, cube.PosFromVec3(vib.Pos))
		}
	}
}

// vibrationHurtHook offers every mob hit as a vibration to the listeners
// near it (game event entity_damage).
func vibrationHurtHook(m *mob.Mob, _ float64, src world.DamageSource) {
	tx := m.Tx()
	for e := range tx.EntitiesWithin(m.BBox().Grow(16)) {
		lm, ok := mob.Of(e)
		if !ok || lm.H() == m.H() {
			continue
		}
		if l, ok := ListenerOf(lm); ok {
			vib := Vibration{Event: VibrationDamage, Pos: m.Position(), Source: m.H()}
			if a := attackerOf(src); a != nil {
				vib.Owner = a.H()
			}
			l.Hear(lm, vib)
		}
	}
}

// --- small client components ---

// flagComponent is a component with no state of its own
// (minecraft:vibration_damper: its entity makes no vibrations).
type flagComponent struct{}

func newFlag(string, json.RawMessage) (mob.Component, error) { return flagComponent{}, nil }

// Attach ...
func (flagComponent) Attach(*mob.Mob) {}

// Detach ...
func (flagComponent) Detach(*mob.Mob) {}

// Heartbeat is minecraft:heartbeat: the interval (Molang seconds, default
// 1) goes to the client as actor data 125 in ticks, with the sound
// (sound_event, default "heartbeat", LevelSoundEvent 376) as 126; the client
// plays it (HeartbeatServerSystem).
type Heartbeat struct {
	interval core.Expr
	sound    int32
}

func newHeartbeat(key string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		Interval json.RawMessage `json:"interval"`
	}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	h := &Heartbeat{interval: core.Const(1), sound: 376}
	if len(v.Interval) > 0 {
		e, err := core.ParseExpr(v.Interval)
		if err != nil {
			return nil, fmt.Errorf("%v: %w", key, err)
		}
		h.interval = e
	}
	return h, nil
}

// Attach ...
func (h *Heartbeat) Attach(m *mob.Mob) {
	m.SetActorData(protocol.EntityDataKeyHeartbeatSoundEvent, h.sound)
	h.Tick(m)
}

// Detach ...
func (*Heartbeat) Detach(m *mob.Mob) {
	m.SetActorData(protocol.EntityDataKeyHeartbeatIntervalTicks, nil)
	m.SetActorData(protocol.EntityDataKeyHeartbeatSoundEvent, nil)
}

// IntervalTicks is the current interval in ticks.
func (h *Heartbeat) IntervalTicks(m *mob.Mob) int32 {
	return int32(max(0, math.Round(h.interval.Eval(m)*20)))
}

// Tick ...
func (h *Heartbeat) Tick(m *mob.Mob) {
	m.SetActorData(protocol.EntityDataKeyHeartbeatIntervalTicks, h.IntervalTicks(m))
}

// soundOffset is minecraft:movement_sound_distance_offset: value (default
// 1) is actor data 124, which the client uses for step sounds.
type soundOffset struct{ v float32 }

func newSoundOffset(key string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		Value *float32 `json:"value"`
	}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	return soundOffset{v: or(v.Value, 1)}, nil
}

// Attach ...
func (c soundOffset) Attach(m *mob.Mob) {
	m.SetActorData(protocol.EntityDataKeyMovementSoundDistanceOffset, c.v)
}

// Detach ...
func (soundOffset) Detach(m *mob.Mob) {
	m.SetActorData(protocol.EntityDataKeyMovementSoundDistanceOffset, nil)
}
