//go:build crossfly_full

package locomotion

import (
	"encoding/json"
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/control"
	"github.com/df-mc/dragonfly/server/mob/physics"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// Hard-coded actor behaviour of the water and flying mobs: what BDS keeps in
// the Dolphin, Guardian, Slime, Ghast/Bat (FloatNavigation) and Phantom
// classes rather than in components.

// initActor installs the per-type defaults (Actor constructors).
func initActor(m *mob.Mob) {
	switch m.Identifier() {
	case "minecraft:dolphin":
		// Dolphin hard-codes DolphinMoveControl (movement.dolphin, max turn
		// 10): no 1.26 definition has a movement component for it.
		m.SetMoveControl(control.NewMoveControl(control.Generic, 10))
	case "minecraft:elder_guardian":
		m.SetActorFlag(protocol.EntityDataFlagElder, true)
	}
}

// floatNavigation is FloatNavigation::travel (navigation.float: ghast, bat):
// the mob moves by its velocity without gravity and with air drag (0.91, or
// block friction on the ground) on every axis; package physics does exactly
// that for flying mobs, so the mob is kept flying while the navigation is
// active and not resting. After travel it faces its velocity, or its target
// within 64 blocks.
func floatNavigationBefore(m *mob.Mob) {
	if !m.HasComponent("minecraft:navigation.float") {
		return
	}
	physics.SetFlying(m, !m.ActorFlag(protocol.EntityDataFlagResting))
}

func floatNavigationAfter(m *mob.Mob) {
	if m.Dead() || !m.HasComponent("minecraft:navigation.float") || m.ActorFlag(protocol.EntityDataFlagResting) {
		return
	}
	d := m.Vel()
	if t, ok := m.Target(); ok && t.Position().Sub(m.Position()).LenSqr() < 4096 {
		d = t.Position().Sub(m.Position())
	}
	if d[0]*d[0]+d[2]*d[2] < 1e-8 {
		return
	}
	yaw := -mgl64.RadToDeg(math.Atan2(d[0], d[2]))
	m.SetYaw(yaw)
	m.SetBodyYaw(yaw)
	m.SetHeadYaw(yaw)
}

// flightSwap replaces the fly, hover and glide controls of package control,
// which accelerate a flier through the 0.02 air speed of travel (bees at 0.7
// blocks/s), for mobs that fly (navigation.fly, navigation.hover or can_fly):
//
//   - movement.glide (phantom) gets GlideControl, a port of BDS
//     GlideMoveControl;
//   - movement.fly (parrot) and movement.hover (bee, allay, happy ghast) get
//     FlyControl.
//
// Ground mobs keep their control.
func flightSwap(m *mob.Mob) {
	c, ok := m.MoveControl().(*control.MoveControl)
	if !ok {
		return
	}
	switch c.Kind() {
	case control.Glide:
		raw, _ := m.ComponentJSON("minecraft:movement.glide")
		m.SetMoveControl(newGlideControl(c.MaxTurn(), raw))
	case control.Fly, control.Hover:
		if !m.HasComponent("minecraft:navigation.fly") && !m.HasComponent("minecraft:navigation.hover") && !m.HasComponent("minecraft:can_fly") {
			return
		}
		m.SetMoveControl(&FlyControl{hover: c.Kind() == control.Hover, maxTurn: c.MaxTurn(), speedMod: 1})
	}
}

// FlyControl is the move control of fliers (movement.fly, movement.hover).
//
// movement.fly is BDS FlyMoveControl: while it has a wanted position the
// velocity gets 0.05 x speed modifier x movement speed a tick towards it
// (quartered within the mob's size of it), and the mob faces its velocity.
// Out of moves it lets gravity back (parrots land).
//
// movement.hover: the BDS HoverMoveControl speed is not recoverable from the
// dump, so it follows Java's FlyingMoveControl (speed = speed modifier x
// flying_speed in the air, x movement speed on the ground; the yaw turns up
// to 90 degrees a tick, the pitch up to max_turn) with the speed applied as
// direct movement: the velocity eases halfway a tick to that speed along the
// line to the target (capped at the distance left), so a bee (0.15) cruises
// at about 2.7 blocks/s and an allay (0.1) at about 1.8. Without a wanted
// position it keeps flying and brakes like BDS (x0.35 within its size of the
// last target, else x0.95).
type FlyControl struct {
	hover    bool
	maxTurn  float64
	wanted   mgl64.Vec3
	speedMod float64
	has      bool
	strafeF  float64
	strafeR  float64
	strafing bool
	speed    float64
}

func (f *FlyControl) SetWantedPosition(pos mgl64.Vec3, speed float64) {
	f.wanted, f.speedMod, f.has, f.strafing = pos, speed, true, false
}
func (f *FlyControl) HasWanted() bool            { return f.has }
func (f *FlyControl) WantedPosition() mgl64.Vec3 { return f.wanted }
func (f *FlyControl) Speed() float64             { return f.speedMod }
func (f *FlyControl) Stop()                      { f.has, f.strafing, f.speed = false, false, 0 }
func (f *FlyControl) MaxTurn() float64           { return f.maxTurn }

// Strafe implements mob.MoveControl: one tick of sideways/forward flight at
// a quarter of the speed.
func (f *FlyControl) Strafe(forward, right float64) {
	f.strafeF, f.strafeR, f.strafing, f.has = forward, right, true, false
}

// CurrentSpeed is the speed of the last move (blocks a tick, before drag).
func (f *FlyControl) CurrentSpeed() float64 { return f.speed }

// Hovering reports whether this is a movement.hover control.
func (f *FlyControl) Hovering() bool { return f.hover }

// flySpeed is the speed modifier times flying_speed in the air or movement
// speed on the ground (Java FlyingMoveControl).
func (f *FlyControl) flySpeed(m *mob.Mob, mod float64) float64 {
	if m.OnGround() {
		return mod * m.Speed()
	}
	return mod * m.Attr(mob.AttrFlyingSpeed)
}

func (f *FlyControl) Tick(m *mob.Mob) {
	in := m.Input()
	in.Forward, in.Strafe, in.Vertical, in.Speed = 0, 0, 0, 0
	if f.strafing {
		f.strafing = false
		physics.SetFlying(m, true)
		s := f.flySpeed(m, 0.25)
		yaw := mgl64.DegToRad(m.Yaw())
		fwd := mgl64.Vec3{-math.Sin(yaw), 0, math.Cos(yaw)}
		right := mgl64.Vec3{-math.Cos(yaw), 0, -math.Sin(yaw)}
		v := m.Vel()
		t := fwd.Mul(f.strafeF * s).Add(right.Mul(f.strafeR * s))
		m.SetVelocity(mgl64.Vec3{v[0] + (t[0]-v[0])*0.5, v[1], v[2] + (t[2]-v[2])*0.5})
		return
	}
	if !f.has {
		f.speed = 0
		if !f.hover {
			physics.SetFlying(m, false)
			return
		}
		physics.SetFlying(m, true)
		v := m.Vel()
		d := f.wanted.Sub(m.Position())
		damp := 0.95
		if w := m.BBox().Width(); d[0]*d[0]+d[2]*d[2] < w*w {
			damp = 0.35
		}
		m.SetVelocity(v.Mul(damp))
		return
	}
	f.has = false
	physics.SetFlying(m, true)
	m.ResetFallDistance()
	d := f.wanted.Sub(m.Position())
	dist := d.Len()
	if dist*dist < 2.5e-7 {
		f.speed = 0
		return
	}
	if f.hover {
		f.tickHover(m, d, dist)
	} else {
		f.tickFly(m, d, dist)
	}
	h := math.Hypot(d[0], d[2])
	if h > 1e-5 {
		yaw := rotlerp(m.Yaw(), mgl64.RadToDeg(math.Atan2(d[2], d[0]))-90, 90)
		m.SetYaw(yaw)
		m.SetBodyYaw(yaw)
	}
	if math.Abs(d[1]) > 1e-5 || h > 1e-5 {
		m.SetPitch(rotlerp(m.Pitch(), -mgl64.RadToDeg(math.Atan2(d[1], h)), math.Max(f.maxTurn, 1)))
	}
}

// tickHover is the Java FlyingMoveControl speed as direct movement.
func (f *FlyControl) tickHover(m *mob.Mob, d mgl64.Vec3, dist float64) {
	speed := f.flySpeed(m, f.speedMod)
	f.speed = speed
	t := d.Mul(math.Min(speed, dist) / dist)
	v := m.Vel()
	m.SetVelocity(v.Add(t.Sub(v).Mul(0.5)))
}

// tickFly is BDS FlyMoveControl::tick.
func (f *FlyControl) tickFly(m *mob.Mob, d mgl64.Vec3, dist float64) {
	v := m.Vel()
	b := m.BBox()
	if size := (b.Width() + b.Height() + b.Length()) / 3; dist < size {
		v = v.Mul(0.25)
	}
	f.speed = 0.05 * f.speedMod * m.Speed()
	m.SetVelocity(v.Add(d.Mul(f.speed / dist)))
}

// rotlerp turns from towards to by at most max degrees (Java
// MoveControl.rotlerp).
func rotlerp(from, to, max float64) float64 {
	return from + mgl64.Clamp(wrap(to-from), -max, max)
}

// GlideControl is BDS GlideMoveControl (movement.glide, phantom): the mob
// glides towards the wanted position, turning at most 4 degrees a tick; its
// speed approaches speed_multiplier x movement speed (by 0.009/speed a tick)
// while flying straight and speed_when_turning (by 0.025) while turning; the
// velocity eases 20% a tick towards that speed along the way to the target.
// Bumping into a wall turns it around and resets the speed to start_speed.
type GlideControl struct {
	maxTurn, startSpeed, turnSpeed float64
	cur                            float64
	wanted                         mgl64.Vec3
	speedMod                       float64
	has                            bool
	stuck                          int
}

func newGlideControl(maxTurn float64, raw json.RawMessage) *GlideControl {
	j := struct {
		Start *float64 `json:"start_speed"`
		Turn  *float64 `json:"speed_when_turning"`
	}{}
	_ = decode(raw, &j)
	g := &GlideControl{maxTurn: maxTurn, startSpeed: 0.1, turnSpeed: 0.2, speedMod: 1}
	if j.Start != nil {
		g.startSpeed = *j.Start
	}
	if j.Turn != nil {
		g.turnSpeed = *j.Turn
	}
	g.cur = g.startSpeed
	return g
}

func (g *GlideControl) SetWantedPosition(pos mgl64.Vec3, speed float64) {
	if g.has && pos != g.wanted {
		g.stuck = 0
	}
	g.wanted, g.speedMod, g.has = pos, speed, true
}
func (g *GlideControl) HasWanted() bool            { return g.has }
func (g *GlideControl) WantedPosition() mgl64.Vec3 { return g.wanted }
func (g *GlideControl) Speed() float64             { return g.speedMod }
func (g *GlideControl) Strafe(float64, float64)    {}
func (g *GlideControl) Stop()                      { g.has = false }
func (g *GlideControl) MaxTurn() float64           { return g.maxTurn }

// CurrentSpeed is the glide speed.
func (g *GlideControl) CurrentSpeed() float64 { return g.cur }

func approach(cur, target, step float64) float64 {
	if cur < target {
		return math.Min(cur+step, target)
	}
	return math.Max(cur-step, target)
}

func (g *GlideControl) Tick(m *mob.Mob) {
	physics.SetFlying(m, true)
	m.ResetFallDistance()
	if !g.has {
		return
	}
	if m.Physics().HorizontalCollision {
		m.SetYaw(m.Yaw() + 180)
		g.cur = g.startSpeed
		if g.stuck++; g.stuck > 3 {
			g.has = false
			return
		}
	}
	pos := m.Position()
	d := g.wanted.Sub(pos)
	hd := math.Hypot(d[0], d[2])
	if hd > 0 {
		f := 1 - math.Abs(d[1]*0.7)/hd
		d[0] *= f
		d[2] *= f
	}
	old := m.Yaw()
	target := mgl64.RadToDeg(math.Atan2(d[2], d[0]))
	yaw := wrap(old+90) + mgl64.Clamp(wrap(target-wrap(old+90)), -4, 4) - 90
	m.SetYaw(yaw)
	m.SetBodyYaw(yaw)
	if math.Abs(wrap(yaw-old)) >= 3 {
		g.cur = approach(g.cur, g.speedMod*g.turnSpeed, 0.025)
	} else {
		g.cur = approach(g.cur, g.speedMod*m.Speed(), 0.009/math.Max(g.cur, 1e-3))
	}
	h := math.Hypot(d[0], d[2])
	pitch := mgl64.RadToDeg(math.Atan2(d[1], h))
	m.SetPitch(-pitch)
	l := d.Len()
	var tgt mgl64.Vec3
	if l > 1.1920929e-7 {
		r := mgl64.DegToRad(yaw + 90)
		tgt = mgl64.Vec3{
			math.Abs(d[0]/l) * math.Cos(r) * g.cur,
			math.Sin(mgl64.DegToRad(pitch)) * g.cur * math.Abs(d[1]/l),
			math.Abs(d[2]/l) * math.Sin(r) * g.cur,
		}
	}
	v := m.Vel()
	m.SetVelocity(v.Add(tgt.Sub(v).Mul(0.2)))
	if m.LookControl() != nil {
		m.LookControl().SetLookAt(g.wanted, 60, 20)
	}
}

func wrap(a float64) float64 {
	a = math.Mod(a+180, 360)
	if a < 0 {
		a += 360
	}
	return a - 180
}

// isSlime reports the mobs Slime::remove splits (slime, magma cube).
func isSlime(m *mob.Mob) bool {
	id := m.Identifier()
	return id == "minecraft:slime" || id == "minecraft:magma_cube"
}

// slimeSplit is Slime::remove: when its corpse is removed, a dead slime of
// size 2 or more splits into 2-4 slimes of half its size (spawn_medium /
// spawn_small of the definition) where it lies.
func slimeSplit(m *mob.Mob) {
	size := m.Meta().Variant
	if size <= 1 {
		return
	}
	event := "spawn_small"
	if size/2 >= 2 {
		event = "spawn_medium"
	}
	tx := m.Tx()
	n := 2 + m.Rand().IntN(3)
	s := float64(size)
	for i := 0; i < n; i++ {
		// source: java Slime.remove offsets.
		off := mgl64.Vec3{(float64(i%2) - 0.5) * s / 4, 0.5, (float64(i/2) - 0.5) * s / 4}
		persistent := m.Persistent()
		_, _ = mob.Spawn(tx, m.Identifier(), m.Position().Add(off), mob.SpawnOptions{
			Event:    event,
			Rotation: cube.Rotation{m.Rand().Float64() * 360, 0},
			Configure: func(c *mob.Mob) {
				c.SetPersistent(persistent)
			},
		})
	}
}

// isGuardian reports guardians and elder guardians (GuardianFlag).
func isGuardian(m *mob.Mob) bool {
	id := m.Identifier()
	return id == "minecraft:guardian" || id == "minecraft:elder_guardian"
}

// thornsSource is the guardian spikes' damage (cause thorns).
type thornsSource struct{ Attacker *mob.Mob }

func (thornsSource) ReducedByArmour() bool     { return false }
func (thornsSource) ReducedByResistance() bool { return true }
func (thornsSource) Fire() bool                { return false }
func (thornsSource) IgnoreTotem() bool         { return false }

// guardianThorns is Guardian::_hurt: hurt by a mob (players included) while
// its spikes are out (not moving), the mob takes 2 thorns damage, whatever
// the cause except magic, projectiles and block explosions: melee and
// contact hits (entity.AttackDamageSource) and the explosion of an exploding
// mob (a creeper).
func guardianThorns(m *mob.Mob, _ float64, src world.DamageSource) {
	if !isGuardian(m) || m.ActorFlag(protocol.EntityDataFlagMoving) {
		return
	}
	var attacker world.Entity
	switch s := src.(type) {
	case entity.AttackDamageSource:
		attacker = s.Attacker
	case entity.ExplosionDamageSource:
		switch es := s.Source.(type) {
		case world.EntityExplosionSource:
			attacker = es.Entity
		case world.Entity:
			attacker = es
		}
	}
	if attacker == nil || attacker.H() == m.H() {
		return
	}
	if l, ok := attacker.(entity.Living); ok {
		l.Hurt(2, core.CauseSource{Cause: "thorns"})
	}
}

// curseAction is the elder guardian ghost the cursed player sees (actor
// event 35).
type curseAction struct{}

func (curseAction) EntityAction()     {}
func (curseAction) ActorEvent() uint8 { return packet.ActorEventGuardianMiningFatigue }

// guardianTick is Guardian::preAiStep (server part): the moving flag (spikes
// in) in water, flopping on land, and the elder guardian's curse.
func guardianTick(m *mob.Mob) {
	if !isGuardian(m) || m.Dead() {
		return
	}
	p := m.Physics()
	v := m.Vel()
	moving := p.InWater && v[0]*v[0]+v[2]*v[2] > 1e-4
	if moving != m.ActorFlag(protocol.EntityDataFlagMoving) {
		m.SetActorFlag(protocol.EntityDataFlagMoving, moving)
	}
	if !p.InWater && p.OnGround {
		if _, ok := m.Target(); !ok {
			r := m.Rand()
			m.SetVelocity(mgl64.Vec3{v[0] + (r.Float64()*2-1)*0.4, v[1] + 0.5, v[2] + (r.Float64()*2-1)*0.4})
			m.SetYaw(r.Float64() * 360)
			m.PlaySound("flop")
		}
	}
	if m.Identifier() != "minecraft:elder_guardian" {
		return
	}
	id := m.H().UUID()
	if (m.Ticks()+int64(id[0])%100)%100 != 0 {
		return
	}
	tx := m.Tx()
	for e := range tx.EntitiesWithin(m.BBox().Grow(50)) {
		pl, ok := e.(*player.Player)
		if !ok || !pl.GameMode().AllowsTakingDamage() || pl.Position().Sub(m.Position()).LenSqr() >= 2500 {
			continue
		}
		if cur, ok := pl.Effect(effect.MiningFatigue); ok && cur.Level() >= 3 && cur.Duration() >= time.Minute {
			continue
		}
		pl.AddEffect(effect.New(effect.MiningFatigue, 3, 5*time.Minute))
		for _, vw := range tx.Viewers(pl.Position()) {
			vw.ViewEntityAction(pl, curseAction{})
		}
	}
}

var _ = sense.IsPlayer
