//go:build crossfly_full

package mob

import (
	"encoding/json"
	"math/rand/v2"
	"slices"
	"unsafe"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Mob is a definition-driven mob opened in a transaction. It is only valid
// inside the transaction it was opened in; keep its handle (H) across ticks,
// never the *Mob.
type Mob struct {
	*entity.Ent
	data *world.EntityData
	s    *state
}

// Of returns the mob behind e, if e is one.
func Of(e world.Entity) (*Mob, bool) {
	switch e := e.(type) {
	case *Mob:
		return e, true
	case *entity.Ent:
		if s, ok := e.Behaviour().(*state); ok {
			if c := s.cur; c != nil && c.Ent == e {
				return c, true
			}
			m := &Mob{Ent: e, data: s.data, s: s}
			s.cur = m
			return m, true
		}
	}
	return nil, false
}

// OfHandle opens the mob behind h in tx.
func OfHandle(tx *world.Tx, h *world.EntityHandle) (*Mob, bool) {
	if h == nil {
		return nil, false
	}
	e, ok := h.Entity(tx)
	if !ok {
		return nil, false
	}
	return Of(e)
}

// Type returns the mob's entity type.
func (m *Mob) Type() *Type { return m.s.t }

// Identifier is the entity identifier ("minecraft:zombie").
func (m *Mob) Identifier() string { return m.s.t.id }

// Definition returns the mob's static definition.
func (m *Mob) Definition() Definition { return m.s.t.def }

// Instance returns the mob's live definition (groups, properties). It is
// nil until the mob is initialised (Spawn or first tick).
func (m *Mob) Instance() Instance { return m.s.inst }

// Initialised reports whether the mob's components are attached.
func (m *Mob) Initialised() bool { return m.s.initialised }

// Rand is the mob's own random source. Use it (not the global one) for all
// of the mob's rolls so tests can seed it with SetRandSeed.
func (m *Mob) Rand() *rand.Rand {
	if m.s.rnd == nil {
		m.ensureInit()
	}
	return m.s.rnd
}

// SetRandSeed reseeds the mob's random source (tests).
func (m *Mob) SetRandSeed(a, b uint64) { m.s.rnd = rand.New(rand.NewPCG(a, b)) }

// Ticks is how many ticks the mob has been ticked since it was loaded.
func (m *Mob) Ticks() int64 { return m.s.ticks }

// Pos is Position.
func (m *Mob) Pos() mgl64.Vec3 { return m.data.Pos }

// Vel is Velocity.
func (m *Mob) Vel() mgl64.Vec3 { return m.data.Vel }

// SetPosition moves the mob without telling viewers (physics; StageSend
// shows it). Use Teleport to jump it somewhere.
func (m *Mob) SetPosition(pos mgl64.Vec3) { m.data.Pos = pos }

// EyePosition is the position of the mob's eyes (BDS: 0.85 of the height;
// definitions don't override it).
func (m *Mob) EyePosition() mgl64.Vec3 {
	return m.data.Pos.Add(mgl64.Vec3{0, m.EyeHeight()})
}

// EyeHeight is the eye height above the feet.
func (m *Mob) EyeHeight() float64 { return m.s.meta.Height * m.s.meta.Scale * 0.85 }

// BBox returns the mob's collision box at its position.
func (m *Mob) BBox() cube.BBox { return m.s.t.BBox(m).Translate(m.data.Pos) }

// Yaw is the facing yaw (BDS mRot.y, set by the move control), in degrees.
// Rotation().Yaw() is the same value.
func (m *Mob) Yaw() float64 { return m.data.Rot[0] }

// Pitch is the head pitch in degrees.
func (m *Mob) Pitch() float64 { return m.data.Rot[1] }

// SetYaw and SetPitch change the rotation without telling viewers.
func (m *Mob) SetYaw(v float64)   { m.data.Rot[0] = v }
func (m *Mob) SetPitch(v float64) { m.data.Rot[1] = v }

// SetRotation sets yaw and pitch.
func (m *Mob) SetRotation(r cube.Rotation) { m.data.Rot = r }

// SetBodyYaw sets the body yaw (BDS mYBodyRot, BodyYaw), turned by the
// body control.
func (m *Mob) SetBodyYaw(v float64) { m.s.bodyYaw = v }

// SetHeadYaw sets the head yaw (BDS mYHeadRot, HeadYaw), turned by the look
// control.
func (m *Mob) SetHeadYaw(v float64) { m.s.headYaw = v }

// Physics is the result of the last travel step.
func (m *Mob) Physics() *Physics { return &m.s.physics }

// OnGround reports whether the mob stood on the ground after its last
// travel step.
func (m *Mob) OnGround() bool { return m.s.physics.OnGround }

// Input is this tick's movement input from the controls.
func (m *Mob) Input() *Input { return &m.s.input }

// Attributes returns the mob's attributes.
func (m *Mob) Attributes() *Attributes { return m.s.attrs }

// Attr returns the current value of an attribute.
func (m *Mob) Attr(name string) float64 { return m.s.attrs.Value(name) }

// Families returns the mob's type families (minecraft:type_family).
func (m *Mob) Families() []string { return m.s.families }

// HasFamily reports whether the mob is of family f.
func (m *Mob) HasFamily(f string) bool { return slices.Contains(m.s.families, f) }

// SetFamilies replaces the mob's families (the type_family component calls
// it on attach and detach).
func (m *Mob) SetFamilies(f []string) { m.s.families = slices.Clone(f) }

// Target returns the mob's current target, if it is in the same world.
func (m *Mob) Target() (world.Entity, bool) {
	if m.s.target == nil {
		return nil, false
	}
	e, ok := m.s.target.Entity(m.Tx())
	if !ok {
		return nil, false
	}
	return e, true
}

// TargetHandle returns the target's handle, or nil.
func (m *Mob) TargetHandle() *world.EntityHandle { return m.s.target }

// SetTarget changes the target (nil clears it) and tells the
// TargetListener components when it changed.
func (m *Mob) SetTarget(e world.Entity) {
	var h *world.EntityHandle
	if e != nil {
		h = e.H()
	}
	if h == m.s.target {
		return
	}
	old, _ := m.Target()
	m.s.target = h
	for _, c := range m.s.comps {
		if l, ok := c.comp.(TargetListener); ok {
			l.OnTargetChanged(m, old, e)
		}
	}
	m.RefreshState()
}

// ResetTarget clears the target (event response reset_target).
func (m *Mob) ResetTarget() { m.SetTarget(nil) }

// LastHurtBy returns the entity that last hurt the mob (an attacker or a
// projectile's owner) and the tick (Ticks) it happened.
func (m *Mob) LastHurtBy() (world.Entity, int64, bool) {
	if m.s.lastHurtBy == nil {
		return nil, 0, false
	}
	e, ok := m.s.lastHurtBy.Entity(m.Tx())
	return e, m.s.lastHurtTick, ok
}

// LastHurtByPlayerTick is the tick a player last hurt the mob (experience
// drops only after a recent player hit).
func (m *Mob) LastHurtByPlayerTick() int64 { return m.s.lastPlayerHurtTick }

// SetLastHurtBy forgets (nil) or sets who last hurt the mob.
func (m *Mob) SetLastHurtBy(e world.Entity) {
	if e == nil {
		m.s.lastHurtBy = nil
		return
	}
	m.s.lastHurtBy, m.s.lastHurtTick = e.H(), m.s.ticks
	if _, ok := e.(*player.Player); ok {
		m.s.lastPlayerHurtTick = m.s.ticks
	}
}

// LastHurtMob returns the entity the mob last attacked.
func (m *Mob) LastHurtMob() (world.Entity, bool) {
	if m.s.lastHurtMob == nil {
		return nil, false
	}
	return m.s.lastHurtMob.Entity(m.Tx())
}

// SetLastHurtMob records the entity the mob attacked (melee goals call it).
func (m *Mob) SetLastHurtMob(e world.Entity) {
	if e == nil {
		m.s.lastHurtMob = nil
		return
	}
	m.s.lastHurtMob = e.H()
}

// Navigation returns the mob's navigation (a no-op one when none is
// attached).
func (m *Mob) Navigation() Navigation { return m.s.nav }

// SetNavigation attaches a navigation (nil restores the no-op one).
func (m *Mob) SetNavigation(n Navigation) {
	if n == nil {
		n = nopNavigation{}
	}
	m.s.nav = n
}

// MoveControl, LookControl, JumpControl and BodyControl return the mob's
// controls (no-op ones until a package sets them).
func (m *Mob) MoveControl() MoveControl { return m.s.move }
func (m *Mob) LookControl() LookControl { return m.s.look }
func (m *Mob) JumpControl() JumpControl { return m.s.jump }
func (m *Mob) BodyControl() BodyControl { return m.s.body }

// SetMoveControl etc. attach controls (nil restores the no-op one).
func (m *Mob) SetMoveControl(c MoveControl) {
	if c == nil {
		c = nopMove{}
	}
	m.s.move = c
}
func (m *Mob) SetLookControl(c LookControl) {
	if c == nil {
		c = nopLook{}
	}
	m.s.look = c
}
func (m *Mob) SetJumpControl(c JumpControl) {
	if c == nil {
		c = nopJump{}
	}
	m.s.jump = c
}
func (m *Mob) SetBodyControl(c BodyControl) {
	if c == nil {
		c = nopBody{}
	}
	m.s.body = c
}

// StopMovement stops navigation and the move control and zeroes horizontal
// velocity (event response stop_movement).
func (m *Mob) StopMovement() {
	m.s.nav.Stop(m)
	m.s.move.Stop()
	m.data.Vel[0], m.data.Vel[2] = 0, 0
}

// Goals returns the mob's goal selector (target and main goals together, as
// in BDS).
func (m *Mob) Goals() *GoalSelector { return m.s.goals }

// HasComponent reports whether a component key is active, supported or not
// (filter has_component).
func (m *Mob) HasComponent(key string) bool {
	for _, c := range m.s.comps {
		if c.key == key {
			return true
		}
	}
	return false
}

// ComponentJSON returns the JSON of an active component.
func (m *Mob) ComponentJSON(key string) (json.RawMessage, bool) {
	for _, c := range m.s.comps {
		if c.key == key {
			return c.raw, true
		}
	}
	return nil, false
}

// Component returns the attached (supported, non-goal) component under key.
func (m *Mob) Component(key string) (Component, bool) {
	for _, c := range m.s.comps {
		if c.key == key && c.comp != nil {
			return c.comp, true
		}
	}
	return nil, false
}

// Components returns the active component keys in definition order.
func (m *Mob) Components() []string {
	out := make([]string, len(m.s.comps))
	for i, c := range m.s.comps {
		out[i] = c.key
	}
	return out
}

// ComponentOf returns the first attached component of type T.
func ComponentOf[T any](m *Mob) (T, bool) {
	// Systems look up their component on every mob every tick, and most
	// mobs do not have it: the answer is kept until the components change.
	s := m.s
	key := typeKey[T]()
	for _, e := range s.compOf {
		if e.key == key {
			if e.comp == nil {
				var zero T
				return zero, false
			}
			return e.comp.(T), true
		}
	}
	for _, c := range s.comps {
		if t, ok := c.comp.(T); ok {
			s.compOf = append(s.compOf, compOfEntry{key: key, comp: c.comp})
			return t, true
		}
	}
	s.compOf = append(s.compOf, compOfEntry{key: key})
	var zero T
	return zero, false
}

// compOfEntry is a ComponentOf answer: the component of the type key, or
// nil if the mob has none.
type compOfEntry struct {
	key  unsafe.Pointer
	comp Component
}

// typeKey identifies T: the type word of a *T in an interface.
func typeKey[T any]() unsafe.Pointer {
	k := any((*T)(nil))
	return (*[2]unsafe.Pointer)(unsafe.Pointer(&k))[0]
}

// Fire runs an entity event (from the definition's "events") on the mob and
// applies the resulting component changes. ctx may be nil; Self and Tx are
// filled in. It reports whether the active components changed.
func (m *Mob) Fire(event string, ctx *FilterContext) bool {
	m.ensureInit()
	if ctx == nil {
		ctx = m.Ctx()
	} else {
		ctx.Self, ctx.Tx = m, m.Tx()
	}
	if !m.s.inst.Fire(m, event, ctx) {
		return false
	}
	m.applyComponents()
	return true
}

// ReapplyComponents re-reads the instance's active components (for package
// mob/def after it changed the instance outside Fire).
func (m *Mob) ReapplyComponents() { m.applyComponents() }

// Property returns a property value (description.properties).
func (m *Mob) Property(name string) (any, bool) {
	m.ensureInit()
	return m.s.inst.Property(name)
}

// SetProperty sets a property value and shows the change to viewers.
func (m *Mob) SetProperty(name string, v any) error {
	m.ensureInit()
	if err := m.s.inst.SetProperty(name, v); err != nil {
		return err
	}
	m.RefreshState()
	return nil
}

// Properties returns every property with its value (for client sync).
func (m *Mob) Properties() []PropertyValue {
	if m.s.inst == nil {
		return nil
	}
	return m.s.inst.Properties()
}

// Home returns the mob's home position (set_home_position, minecraft:home).
func (m *Mob) Home() (cube.Pos, bool) {
	if m.s.home == nil {
		return cube.Pos{}, false
	}
	return *m.s.home, true
}

// SetHome sets the home position.
func (m *Mob) SetHome(p cube.Pos) { m.s.home = &p }

// ClearHome forgets the home position.
func (m *Mob) ClearHome() { m.s.home = nil }

// Persistent reports whether the mob must never despawn (persistent
// component, name tag, picked up items, ...).
func (m *Mob) Persistent() bool { return m.s.persistent }

// SetPersistent marks the mob persistent or not.
func (m *Mob) SetPersistent(v bool) { m.s.persistent = v }

// Data returns per-mob data a package stored under its own key type, so
// packages can keep state on a mob without fields in this package.
func (m *Mob) Data(key any) (any, bool) {
	v, ok := m.s.scratch[key]
	return v, ok
}

// SetData stores per-mob data under key (nil deletes it). Use an unexported
// key type of your package.
func (m *Mob) SetData(key, v any) {
	if v == nil {
		delete(m.s.scratch, key)
		return
	}
	if m.s.scratch == nil {
		m.s.scratch = map[any]any{}
	}
	m.s.scratch[key] = v
}

// HeldItems returns the items in the mob's hands.
func (m *Mob) HeldItems() (mainHand, offHand item.Stack) { return m.s.mainHand, m.s.offHand }

// SetHeldItems changes them and shows the change (MobEquipment / Java Set
// Equipment to every viewer).
func (m *Mob) SetHeldItems(mainHand, offHand item.Stack) {
	m.s.mainHand, m.s.offHand = mainHand, offHand
	m.RefreshState()
	m.viewEquipment(world.Viewer.ViewEntityItems)
}

// viewEquipment sends the held items or the armour (f) to the viewers of the
// mob. Viewers that see the mob later get both through world.showEntity.
func (m *Mob) viewEquipment(f func(world.Viewer, world.Entity)) {
	tx := m.Tx()
	if tx == nil || tx.Closed() {
		return
	}
	for _, v := range tx.Viewers(m.data.Pos) {
		f(v, m)
	}
}

// Armour returns the mob's armour slots.
func (m *Mob) Armour() *inventory.Armour {
	m.s.cur = m
	if m.s.armour == nil {
		m.s.armour = m.s.newArmour()
	}
	return m.s.armour
}

// Close removes the mob from the world for good.
func (m *Mob) Close() error {
	if !m.s.removed {
		for _, c := range m.s.comps {
			if r, ok := c.comp.(RemoveListener); ok {
				r.OnRemove(m)
			}
		}
	}
	m.s.removed = true
	return m.Ent.Close()
}

// Interact runs the mob's Interactor components for a player using it.
func (m *Mob) Interact(user item.User, held item.Stack, clickPos mgl64.Vec3) (item.Stack, bool) {
	m.ensureInit()
	if m.Dead() {
		return held, false
	}
	for _, late := range [2]bool{false, true} {
		if late {
			// Interactions BDS hard-codes per actor type (bucket.go).
			if left, done := m.runInteractHooks(user, held, clickPos); done {
				return left, true
			}
		}
		for _, c := range m.s.comps {
			if i, ok := c.comp.(Interactor); ok {
				if _, isLate := c.comp.(LateInteractor); isLate != late {
					continue
				}
				if left, done := i.Interact(m, user, held, clickPos); done {
					return left, true
				}
			}
		}
	}
	return held, false
}

// NameTaggable reports whether a name tag may rename the mob
// (minecraft:nameable).
func (m *Mob) NameTaggable() bool { return m.s.NameTaggable() }

// InactivityTicks is BDS Mob::getNoActionTime: ticks since the mob was last
// hurt or had a player close by (package mob/spawn resets it from the
// despawn rules). The inactivity_timer filter reads it in seconds.
func (m *Mob) InactivityTicks() int { return m.s.noAction }

// ResetInactivity resets the inactivity timer (Mob::resetNoActionTime).
func (m *Mob) ResetInactivity() { m.s.noAction = 0 }

// FallDistance is how far the mob has fallen since it last stood
// (Physics().FallDistance). Blocks read it through this method.
func (m *Mob) FallDistance() float64 { return m.s.physics.FallDistance }

// ResetFallDistance clears the fall distance (water, lava, cobwebs, hay
// bales call it from EntityInside/EntityLand).
func (m *Mob) ResetFallDistance() { m.s.physics.FallDistance = 0 }

// SetIgnoresCobwebs makes the environment step skip cobwebs
// (minecraft:block_movement_slowdown_immunity with minecraft:web).
func (m *Mob) SetIgnoresCobwebs(v bool) { m.s.ignoreWebs = v }

// IgnoresCobwebs reports SetIgnoresCobwebs.
func (m *Mob) IgnoresCobwebs() bool { return m.s.ignoreWebs }

// HasNavigation reports whether a navigation component attached a
// navigation (BDS Mob::hasComponent<NavigationComponent>).
func (m *Mob) HasNavigation() bool {
	_, nop := m.s.nav.(nopNavigation)
	return !nop
}

// Riding: Vehicle, Passengers and Riding are in riding.go.
