//go:build crossfly_full

package mob

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Stage is one step of the fixed per-tick order, mirroring the order of the
// BDS systems.
type Stage uint8

const (
	// StageSensors: timers and sensors (timer, environment_sensor,
	// damage_sensor bookkeeping, target_nearby_sensor, ageable, breathable,
	// ...). Component Tickers without a stage run here.
	StageSensors Stage = iota
	// StageGoals: built-in step = the goal selector (target, then main).
	StageGoals
	// StageNavigation: built-in step = Navigation().Tick.
	StageNavigation
	// StageControls: built-in step = MoveControl, then JumpControl, then
	// LookControl Tick.
	StageControls
	// StageTravel: built-in step = the travel hook (SetTravel).
	StageTravel
	// StageBody: built-in step = BodyControl().Tick.
	StageBody
	// StageEnvironment: built-in step = fire damage and the blocks the mob
	// is in (lava, fire, cactus, water putting fire out).
	StageEnvironment
	// StageDespawn: despawn checks (package mob/spawn).
	StageDespawn
	// StageSend: built-in step = showing the movement to viewers.
	StageSend

	stageCount
)

var stageNames = [...]string{"sensors", "goals", "navigation", "controls", "travel", "body", "environment", "despawn", "send"}

// String ...
func (s Stage) String() string {
	if s < stageCount {
		return stageNames[s]
	}
	return "invalid"
}

// deathTicks is how long a dead mob lies before it is removed (BDS
// Mob::tickDeath removes at deathTime 20).
const deathTicks = 20

// Tick implements entity.Behaviour. It runs the stages in order. A dying mob
// (and one that died earlier in this tick) only runs travel, despawn and send:
// it still falls and is still shown until it is removed.
func (s *state) Tick(e *entity.Ent, tx *world.Tx) *entity.Movement {
	m := &Mob{Ent: e, data: s.data, s: s}
	s.cur = m
	m.ensureInit()
	s.ticks++
	if s.immune > 0 {
		s.immune--
	}
	if s.dying > 0 {
		if s.dying++; s.dying > deathTicks {
			_ = m.Close()
			return nil
		}
	} else {
		s.effects.Tick(m, tx)
		s.noAction++
	}
	for st := StageSensors; st < stageCount; st++ {
		if s.dying > 0 && st != StageTravel && st != StageDespawn && st != StageSend {
			continue
		}
		m.runStage(st)
		if m.closed() {
			return nil
		}
	}
	// Movement is sent in StageSend, so Ent.Tick gets nothing to send.
	return nil
}

// closed reports whether the mob was closed or removed through Mob.Close or
// Mob.Remove during this tick.
func (m *Mob) closed() bool { return m.s.removed || m.H().Closed() }

// runStage runs the built-in step of st, its systems, then its component
// tickers.
func (m *Mob) runStage(st Stage) {
	s := m.s
	switch st {
	case StageGoals:
		if !m.Dead() {
			s.goals.Tick(m)
		}
	case StageNavigation:
		s.nav.Tick(m)
	case StageControls:
		s.move.Tick(m)
		s.jump.Tick(m)
		s.look.Tick(m)
	case StageTravel:
		if m.rideTick() {
			// Riding, or placed by the rider's client: no travel (riding.go).
		} else if travelHook != nil {
			travelHook(m)
		} else {
			m.defaultTravel()
		}
	case StageBody:
		s.body.Tick(m)
	case StageEnvironment:
		m.defaultEnvironment()
	case StageSend:
		if movementSender != nil {
			movementSender(m)
		} else {
			m.SendMovement()
		}
	}
	for _, sys := range systems[st] {
		sys.Tick(m)
		if m.closed() {
			return
		}
	}
	for _, t := range s.tickers[st] {
		t.Tick(m)
		if m.closed() {
			return
		}
	}
}

// defaultTravel is the travel step used until package mob/physics installs
// its own: gravity, drag and block collision only.
func (m *Mob) defaultTravel() {
	s := m.s
	if s.mc == nil {
		s.mc = &entity.MovementComputer{Gravity: 0.08, Drag: 0.02, DragBeforeGravity: true}
	}
	if s.meta.NoGravity {
		s.mc.Gravity = 0
	}
	mv := s.mc.TickMovement(m, m.Position(), m.Velocity(), m.Rotation(), m.Tx())
	m.data.Pos, m.data.Vel = mv.Position(), mv.Velocity()
	s.physics.OnGround = s.mc.OnGround()
}

// defaultEnvironment hurts a burning mob once a second and runs the blocks
// it is inside (Dragonfly only runs EntityInsider for players).
func (m *Mob) defaultEnvironment() {
	s := m.s
	tx := m.Tx()
	if m.OnFireDuration() > 0 && s.ticks%20 == 0 {
		m.Hurt(1, block.FireDamageSource{})
		if m.Dead() || m.closed() {
			return
		}
	}
	box := m.H().Type().BBox(m).Translate(m.Position()).Grow(-0.0001)
	low, high := cube.PosFromVec3(box.Min()), cube.PosFromVec3(box.Max())
	for pos := range cube.Range3D(low, high) {
		b := tx.Block(pos)
		if _, web := b.(block.Cobweb); web && s.ignoreWebs {
			continue
		}
		if in, ok := b.(block.EntityInsider); ok {
			in.EntityInside(pos, tx, m)
		}
		if _, liquid := b.(world.Liquid); !liquid {
			if l, ok := tx.Liquid(pos); ok {
				if in, ok := l.(block.EntityInsider); ok {
					in.EntityInside(pos, tx, m)
				}
			}
		}
		if m.Dead() || m.closed() {
			return
		}
	}
	if m.OnFireDuration() > 0 {
		if l, ok := tx.Liquid(cube.PosFromVec3(m.Position().Add(mgl64.Vec3{0, 0.1, 0}))); ok {
			if _, water := l.(block.Water); water {
				m.Extinguish()
			}
		}
	}
}

// SendMovement is the default StageSend step: it shows position, rotation
// (yaw, pitch, head and body yaw) and velocity changes since the last send to
// the viewers of the mob.
func (m *Mob) SendMovement() {
	s := m.s
	pos, rot, vel := m.Position(), m.Rotation(), m.Velocity()
	moved := !pos.ApproxEqualThreshold(s.sent.pos, 0.001) || rot != s.sent.rot ||
		s.headYaw != s.sent.headYaw || s.bodyYaw != s.sent.bodyYaw || s.physics.OnGround != s.sent.onGround
	velChanged := !vel.ApproxEqualThreshold(s.sent.vel, 0.001)
	if !moved && !velChanged {
		return
	}
	for _, v := range m.Tx().Viewers(pos) {
		if velChanged {
			v.ViewEntityVelocity(m, vel)
		}
		if moved {
			v.ViewEntityMovement(m, pos, rot, s.physics.OnGround)
		}
	}
	s.MarkSent(pos, vel, rot)
}

// MarkSent records what viewers were last shown, for custom movement
// senders.
func (s *state) MarkSent(pos, vel mgl64.Vec3, rot cube.Rotation) {
	s.sent.pos, s.sent.vel, s.sent.rot = pos, vel, rot
	s.sent.headYaw, s.sent.bodyYaw, s.sent.onGround = s.headYaw, s.bodyYaw, s.physics.OnGround
}

// MarkSent records that viewers were shown the mob's current movement (for
// custom movement senders).
func (m *Mob) MarkSent() { m.s.MarkSent(m.Position(), m.Velocity(), m.Rotation()) }
