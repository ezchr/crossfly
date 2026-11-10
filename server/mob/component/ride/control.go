//go:build crossfly_full

package ride

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// ---- minecraft:input_ground_controlled ----

// GroundControlled is minecraft:input_ground_controlled (BDS
// WASDControlledDefinition): the controlling player steers the mob with its
// move input. As the Java horse (AbstractHorse.getRiddenInput/travel): the
// mob turns to the rider's yaw, strafes at half speed, backs up at a
// quarter, at its movement speed. The Bedrock flag keyboard_controlled lets
// the client steer (and predict) it.
type GroundControlled struct{}

func newGroundControlled(string, json.RawMessage) (mob.Component, error) {
	return GroundControlled{}, nil
}

// Attach ...
func (GroundControlled) Attach(m *mob.Mob) {
	m.SetActorFlag(protocol.EntityDataFlagKeyboardControlled, true)
}

// Detach ...
func (GroundControlled) Detach(m *mob.Mob) {
	m.SetActorFlag(protocol.EntityDataFlagKeyboardControlled, false)
}

// TickStage: after the move control, before travel.
func (GroundControlled) TickStage() mob.Stage { return mob.StageControls }

// Tick ...
func (GroundControlled) Tick(m *mob.Mob) {
	rider, ok := m.ControllingPassenger()
	if !ok || m.ClientMoved() {
		return
	}
	in, _ := m.RiderInput()
	if !m.Physics().InWater || !m.HasComponent("minecraft:free_camera_controlled") {
		steer(m, rider, in, 0.5, 0.25)
	}
}

// steer turns m to the rider and sets its move input from the rider's.
func steer(m *mob.Mob, rider world.Entity, in mob.RiderInput, strafeMod, backMod float64) {
	yaw, pitch := rider.Rotation().Elem()
	m.SetYaw(yaw)
	m.SetBodyYaw(yaw)
	m.SetHeadYaw(yaw)
	m.SetPitch(pitch * 0.5)
	m.Navigation().Stop(m)
	strafe, forward := in.Move[0]*strafeMod, in.Move[1]
	if forward <= 0 {
		forward *= backMod
	}
	input := m.Input()
	input.Strafe, input.Forward, input.Vertical = strafe, forward, 0
	input.Speed = m.Speed()
	input.Jump = false
}

// ---- minecraft:free_camera_controlled ----

// FreeCamera is minecraft:free_camera_controlled (happy ghast, nautilus in
// water): the mob moves where the rider looks, up and down included, with
// backwards_movement_modifier and strafe_speed_modifier.
type FreeCamera struct {
	back, strafe float64
}

func newFreeCamera(key string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		Back   *float64 `json:"backwards_movement_modifier"`
		Strafe *float64 `json:"strafe_speed_modifier"`
	}{}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	c := &FreeCamera{back: 0.5, strafe: 1}
	if v.Back != nil {
		c.back = *v.Back
	}
	if v.Strafe != nil {
		c.strafe = *v.Strafe
	}
	return c, nil
}

// Attach ...
func (c *FreeCamera) Attach(m *mob.Mob) {
	m.SetActorFlag(protocol.EntityDataFlagWASDAirControlled, true)
}

// Detach ...
func (c *FreeCamera) Detach(m *mob.Mob) {
	m.SetActorFlag(protocol.EntityDataFlagWASDAirControlled, false)
}

// TickStage ...
func (c *FreeCamera) TickStage() mob.Stage { return mob.StageControls }

// Tick ...
func (c *FreeCamera) Tick(m *mob.Mob) {
	rider, ok := m.ControllingPassenger()
	if !ok || m.ClientMoved() {
		return
	}
	if m.HasComponent("minecraft:input_ground_controlled") && !m.Physics().InWater {
		return
	}
	in, _ := m.RiderInput()
	steer(m, rider, in, c.strafe, c.back)
	pitch := rider.Rotation().Pitch() * math.Pi / 180
	input := m.Input()
	f := input.Forward
	input.Forward = f * math.Cos(pitch)
	input.Vertical = -f * math.Sin(pitch)
	if m.Physics().InWater {
		input.Speed = m.Attr(mob.AttrUnderwaterMovement)
	} else if fs := m.Attr(mob.AttrFlyingSpeed); fs > 0 {
		input.Speed = fs
		if !m.OnGround() {
			// Travel accelerates in the air by 0.02 (the default flying speed) x the input: scale the input
			// to the mob's flying speed (happy ghast 0.016: about 0.17 blocks a tick).
			k := fs / 0.02
			input.Forward, input.Strafe, input.Vertical = input.Forward*k, input.Strafe*k, input.Vertical*k
		}
	}
}

// ---- minecraft:item_controllable ----

// ItemControllable is minecraft:item_controllable (pig, strider): the
// controlling player steers the mob while holding control_items; the
// behavior.controlled_by_player goal sets the course (Steer), this
// component applies it after the move control.
type ItemControllable struct {
	items []core.ItemDescriptor
	// course is set by the goal while it runs.
	course *Course
}

// Course is what behavior.controlled_by_player asks of the mob this tick.
type Course struct {
	// Speed is movement x mount_speed_multiplier (x boost).
	Speed float64
}

func newItemControllable(key string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		Items json.RawMessage `json:"control_items"`
	}{}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	items, err := core.ParseItemList(v.Items)
	if err != nil {
		return nil, fmt.Errorf("%v: control_items: %w", key, err)
	}
	return &ItemControllable{items: items}, nil
}

// Attach ...
func (c *ItemControllable) Attach(*mob.Mob) {}

// Detach ...
func (c *ItemControllable) Detach(*mob.Mob) {}

// Holds reports whether rider holds one of the control items in a hand.
func (c *ItemControllable) Holds(rider world.Entity) bool {
	h, ok := rider.(interface {
		HeldItems() (item.Stack, item.Stack)
	})
	if !ok {
		return false
	}
	main, off := h.HeldItems()
	return core.MatchesAny(c.items, main) || core.MatchesAny(c.items, off)
}

// SetCourse is called by behavior.controlled_by_player each tick it runs
// (nil when it stops).
func (c *ItemControllable) SetCourse(co *Course) { c.course = co }

// TickStage ...
func (c *ItemControllable) TickStage() mob.Stage { return mob.StageControls }

// Tick moves the mob forward at the course speed (ControlledByPlayerGoal::tick
// moveRelative(0, 0, speed)).
func (c *ItemControllable) Tick(m *mob.Mob) {
	if c.course == nil || m.ClientMoved() {
		return
	}
	speed := c.course.Speed
	if b, ok := mob.ComponentOf[*Boostable](m); ok {
		speed *= b.Multiplier(m)
	}
	input := m.Input()
	input.Forward, input.Strafe, input.Vertical, input.Speed = speed, 0, 0, speed
	input.Jump = false
	if m.Physics().HorizontalCollision && m.OnGround() {
		m.JumpControl().Jump()
		input.Jump = true
	}
}

// ItemControllableOf returns the mob's item_controllable component.
func ItemControllableOf(m *mob.Mob) (*ItemControllable, bool) {
	return mob.ComponentOf[*ItemControllable](m)
}

// ---- minecraft:boostable ----

// Boostable is minecraft:boostable (BDS BoostableComponent): the
// controlling rider uses a boost item (carrot on a stick) to speed the mob
// up by speed_multiplier for duration seconds; the item takes damage and
// turns into replace_item when it breaks.
type Boostable struct {
	items      []boostItem
	duration   int
	multiplier float64
	left       int
}

type boostItem struct {
	item    core.ItemDescriptor
	damage  int
	replace string
}

func newBoostable(key string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		Items []struct {
			Item    string `json:"item"`
			Damage  *int   `json:"damage"`
			Replace string `json:"replace_item"`
		} `json:"boost_items"`
		Duration   *float64 `json:"duration"`
		Multiplier *float64 `json:"speed_multiplier"`
	}{}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	b := &Boostable{duration: 60, multiplier: 1}
	if v.Duration != nil {
		b.duration = int(math.Round(*v.Duration * 20))
	}
	if v.Multiplier != nil {
		b.multiplier = *v.Multiplier
	}
	for _, it := range v.Items {
		bi := boostItem{item: core.ParseItemDescriptor(it.Item), damage: 1, replace: it.Replace}
		if it.Damage != nil {
			bi.damage = *it.Damage
		}
		b.items = append(b.items, bi)
	}
	return b, nil
}

// Attach ...
func (b *Boostable) Attach(*mob.Mob) {}

// Detach ...
func (b *Boostable) Detach(*mob.Mob) {}

// Boost implements mob.VehicleBooster.
func (b *Boostable) Boost(m *mob.Mob, _ world.Entity, held item.Stack) (int, bool) {
	if b.left > 0 {
		return 0, false
	}
	for _, it := range b.items {
		if it.item.Matches(held) {
			b.left = b.duration
			return it.damage, true
		}
	}
	return 0, false
}

// Boosting reports whether a boost runs.
func (b *Boostable) Boosting() bool { return b.left > 0 }

// Multiplier is the speed multiplier of a running boost (1 otherwise).
func (b *Boostable) Multiplier(*mob.Mob) float64 {
	if b.left > 0 {
		return b.multiplier
	}
	return 1
}

// Tick counts the boost down.
func (b *Boostable) Tick(*mob.Mob) {
	if b.left > 0 {
		b.left--
	}
}

// ---- minecraft:can_power_jump ----

// PowerJump is minecraft:can_power_jump (horses): the controlling rider
// charges the jump bar and the mob jumps by horse.jump_strength x the bar
// (Java AbstractHorse.executeRidersJump, also what BDS sends the bar for),
// forward too when the rider moves forward.
type PowerJump struct {
	jumping bool
}

func newPowerJump(string, json.RawMessage) (mob.Component, error) { return &PowerJump{}, nil }

// Attach ...
func (j *PowerJump) Attach(m *mob.Mob) { m.SetActorFlag(protocol.EntityDataFlagPowerJump, true) }

// Detach ...
func (j *PowerJump) Detach(m *mob.Mob) { m.SetActorFlag(protocol.EntityDataFlagPowerJump, false) }

// RiderJump implements mob.VehicleJump.
func (j *PowerJump) RiderJump(m *mob.Mob, rider world.Entity, strength float64) {
	if !m.OnGround() || j.jumping || strength <= 0 {
		return
	}
	power := m.Attr(mob.AttrJumpStrength)
	if power <= 0 {
		power = 0.7
	}
	power *= strength
	if e, ok := m.Effect(effect.JumpBoost); ok {
		power += float64(e.Level()) * 0.1
	}
	vel := m.Vel()
	vel[1] = power
	if in, ok := m.RiderInput(); ok && in.Move[1] > 0 {
		sin, cos := math.Sincos(m.Yaw() * math.Pi / 180)
		vel[0] += -0.4 * sin * strength
		vel[2] += 0.4 * cos * strength
	}
	m.SetVelocity(vel)
	j.jumping = true
	core.PlayMobSound(m, "jump")
}

// Tick ends the jump on landing.
func (j *PowerJump) Tick(m *mob.Mob) {
	if j.jumping && m.OnGround() && m.Vel()[1] <= 0 {
		j.jumping = false
	}
}

// ---- minecraft:dash_action ----

// Dash is minecraft:dash_action (camel, nautilus): the controlling rider's
// jump dashes the mob forward (ApplyDashSystem::doDash): horizontal
// horizontal_momentum x jump bar x movement speed along the yaw (or the
// rider's look with direction "passenger"), up vertical_momentum x bar,
// then cooldown_time seconds with the has_dash_cooldown flag.
type Dash struct {
	cooldown   int
	horizontal float64
	vertical   float64
	underwater bool
	passenger  bool
	left       int
}

func newDash(key string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		Cooldown   *float64 `json:"cooldown_time"`
		Horizontal *float64 `json:"horizontal_momentum"`
		Vertical   *float64 `json:"vertical_momentum"`
		Underwater bool     `json:"can_dash_underwater"`
		Direction  string   `json:"direction"`
	}{}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	d := &Dash{cooldown: 20, horizontal: 1, vertical: 1, underwater: v.Underwater, passenger: v.Direction == "passenger"}
	if v.Cooldown != nil {
		d.cooldown = int(math.Round(*v.Cooldown * 20))
	}
	if v.Horizontal != nil {
		d.horizontal = *v.Horizontal
	}
	if v.Vertical != nil {
		d.vertical = *v.Vertical
	}
	return d, nil
}

// Attach ...
func (d *Dash) Attach(m *mob.Mob) { m.SetActorFlag(protocol.EntityDataFlagDash, true) }

// Detach ...
func (d *Dash) Detach(m *mob.Mob) {
	m.SetActorFlag(protocol.EntityDataFlagDash, false)
	m.SetActorFlag(protocol.EntityDataFlagHasDashTimeout, false)
}

// RiderJump implements mob.VehicleJump.
func (d *Dash) RiderJump(m *mob.Mob, rider world.Entity, strength float64) {
	if d.left > 0 || strength <= 0 {
		return
	}
	inWater := m.Physics().InWater
	if inWater && !d.underwater || !inWater && !m.OnGround() {
		return
	}
	yaw := m.Yaw()
	if d.passenger {
		yaw = rider.Rotation().Yaw()
	}
	h := d.horizontal * strength * m.Speed()
	sin, cos := math.Sincos(yaw * math.Pi / 180)
	mult := 1.0
	if e, ok := m.Effect(effect.JumpBoost); ok {
		mult += float64(e.Level())
	}
	vel := m.Vel()
	vel[0] -= sin * h
	vel[2] += cos * h
	vel[1] += strength * d.vertical * mult
	m.SetVelocity(vel)
	d.left = d.cooldown
	m.SetActorFlag(protocol.EntityDataFlagHasDashTimeout, true)
	core.PlayMobSound(m, "dash")
}

// CoolingDown reports whether the dash is on cooldown.
func (d *Dash) CoolingDown() bool { return d.left > 0 }

// Tick counts the cooldown down.
func (d *Dash) Tick(m *mob.Mob) {
	if d.left > 0 {
		if d.left--; d.left == 0 {
			m.SetActorFlag(protocol.EntityDataFlagHasDashTimeout, false)
		}
	}
}

// ---- minecraft:vertical_movement_action ----

// VerticalMovement is minecraft:vertical_movement_action (happy ghast): the
// controlling rider's jump lifts the mob by vertical_velocity.
type VerticalMovement struct{ v float64 }

func newVerticalMovement(key string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		V *float64 `json:"vertical_velocity"`
	}{}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	c := &VerticalMovement{v: 0.5}
	if v.V != nil {
		c.v = *v.V
	}
	return c, nil
}

// Attach ...
func (c *VerticalMovement) Attach(m *mob.Mob) {
	m.SetActorFlag(protocol.EntityDataFlagCanUseVerticalMovementAction, true)
}

// Detach ...
func (c *VerticalMovement) Detach(m *mob.Mob) {
	m.SetActorFlag(protocol.EntityDataFlagCanUseVerticalMovementAction, false)
}

// RiderJump implements mob.VehicleJump.
func (c *VerticalMovement) RiderJump(m *mob.Mob, _ world.Entity, _ float64) {
	vel := m.Vel()
	vel[1] = c.v
	m.SetVelocity(vel)
}

// ---- minecraft:underwater_mount_breathing ----

// MountBreathing is minecraft:underwater_mount_breathing (nautilus): its
// riders do not run out of air under water.
type MountBreathing struct{}

func newMountBreathing(string, json.RawMessage) (mob.Component, error) { return MountBreathing{}, nil }

// Attach ...
func (MountBreathing) Attach(*mob.Mob) {}

// Detach ...
func (MountBreathing) Detach(*mob.Mob) {}

// Tick keeps the riders' air full.
func (MountBreathing) Tick(m *mob.Mob) {
	for _, e := range m.Passengers() {
		if p, ok := e.(*player.Player); ok {
			p.SetAirSupply(p.MaxAirSupply())
		}
	}
}

// ---- markers ----

// marker is a component whose behaviour lives elsewhere
// (rotation_locked_to_vehicle: package mob riding.go reads it).
type marker struct{ flag uint8 }

func newMarker(flag uint8) mob.ComponentFactory {
	return func(string, json.RawMessage) (mob.Component, error) { return marker{flag: flag}, nil }
}

// Attach ...
func (c marker) Attach(m *mob.Mob) {
	if c.flag != 0 {
		m.SetActorFlag(c.flag, true)
	}
}

// Detach ...
func (c marker) Detach(m *mob.Mob) {
	if c.flag != 0 {
		m.SetActorFlag(c.flag, false)
	}
}
