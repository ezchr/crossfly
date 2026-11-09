//go:build crossfly_full

package mob

import (
	"cmp"
	"math"

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
	simulated := tx.Simulated(e.Position())
	if !simulated && (!s.initialised || len(s.frozen) == 0) {
		// Beyond the simulation distance of every player: frozen, as in
		// Bedrock Edition (no AI, physics, despawning or sending).
		return nil
	}
	m := s.cur
	if m == nil || m.Ent != e {
		m = &Mob{Ent: e, data: s.data, s: s}
		s.cur = m
	}
	if !simulated {
		for _, f := range s.frozen {
			f.TickFrozen(m)
			if m.closed() {
				return nil
			}
		}
		return nil
	}
	m.ensureInit()
	if s.t.removeInPeaceful && s.dying == 0 && tx.World().Difficulty() == world.DifficultyPeaceful {
		// Monster::normalTick, Slime::preNormalTick.
		_ = m.Close()
		return nil
	}
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

// sentMovement is the movement of a mob as its viewers of one edition were
// last shown it.
type sentMovement struct {
	pos, vel         mgl64.Vec3
	rot              cube.Rotation
	headYaw, bodyYaw float64
	onGround         bool
}

// movementNow is the mob's current movement, to compare with what was sent.
func (m *Mob) movementNow() sentMovement {
	s := m.s
	return sentMovement{pos: m.Position(), vel: m.Velocity(), rot: m.Rotation(), headYaw: s.headYaw, bodyYaw: s.bodyYaw, onGround: s.physics.OnGround}
}

// javaViewer is a viewer that is a Java Edition client (package
// javasession). Java Edition shows entity movement with its own rules and
// cadence (ServerEntity.sendChanges), so the mob keeps what it last showed
// Java viewers apart from what it showed Bedrock viewers.
type javaViewer interface {
	// EntityUpdateInterval returns the update interval of the Java entity
	// type e is shown as (EntityType.updateInterval, 3 for most mobs): its
	// movement is sent every that many ticks, and the client spreads each
	// move over as many ticks.
	EntityUpdateInterval(e world.Entity) int
	// ViewEntityMovementOver is ViewEntityMovement for a move made over
	// ticks ticks since the last time the mob's movement was due (the
	// one-step PositionPath ServerEntity sends with each move).
	ViewEntityMovementOver(e world.Entity, pos mgl64.Vec3, rot cube.Rotation, onGround bool, ticks int)
}

// SendMovement is the default StageSend step: it shows the mob's movement to
// its viewers by the rules of their edition, as the vanilla servers do.
//
// Bedrock viewers (BDS SpatialActorNetworkData::sendUpdate and
// Actor::sendMotionPacketIfNeeded, every tick): the position and rotation
// when MoveActorAbsoluteData::isDifferenceSignificant, the velocity when it
// changed by more than 0.2 a tick or became zero.
//
// Java viewers (ServerEntity.sendChanges): every update interval of the
// mob's Java type, or at once after a push or knockback (Entity.needsSync),
// the position, rotation, head turn and velocity that changed.
func (m *Mob) SendMovement() {
	s := m.s
	needsSync := s.needsSync
	s.needsSync = false
	now := m.movementNow()

	moved, velChanged := bedrockMoved(now, s.sent), bedrockVelocityChanged(now.vel, s.sent.vel)
	var javaDue, javaMoved, javaVelChanged bool
	var javaTicks int
	lastDue := s.javaDueTick
	javaDecide := func(interval int) {
		javaMoved, javaVelChanged = false, false
		if javaDue = needsSync || s.ticks%int64(interval) == 0; javaDue {
			javaMoved, javaVelChanged = javaMovedFrom(now, s.sentJava), javaVelocityChanged(now.vel, s.sentJava.vel)
			// The move covers the ticks since the movement was last due
			// (SteppedInterpolationTracker: one step per due tick, sent or
			// not).
			javaTicks = int(min(max(s.ticks-lastDue, 1), int64(interval)))
		}
	}
	// The Java update interval is asked of the first Java viewer met while
	// sending; until then it is taken to be the usual 3.
	javaDecide(cmp.Or(s.javaInterval, 3))
	if javaDue {
		s.javaDueTick = s.ticks
	}
	if !moved && !velChanged && !javaMoved && !javaVelChanged {
		return
	}
	for _, v := range m.Tx().Viewers(now.pos) {
		jv, java := v.(javaViewer)
		if !java {
			if velChanged {
				v.ViewEntityVelocity(m, now.vel)
			}
			if moved {
				v.ViewEntityMovement(m, now.pos, now.rot, now.onGround)
			}
			continue
		}
		if s.javaInterval <= 0 {
			s.javaInterval = max(jv.EntityUpdateInterval(m), 1)
			javaDecide(s.javaInterval)
		}
		if javaVelChanged {
			v.ViewEntityVelocity(m, now.vel)
		}
		if javaMoved {
			jv.ViewEntityMovementOver(m, now.pos, now.rot, now.onGround, javaTicks)
		}
	}
	// Only what was sent: changes too small to send add up until they are.
	s.sent.update(now, moved, velChanged)
	s.sentJava.update(now, javaMoved, javaVelChanged)
}

// update records what was sent of now.
func (s *sentMovement) update(now sentMovement, moved, velChanged bool) {
	if moved {
		vel := s.vel
		*s = now
		s.vel = vel
	}
	if velChanged {
		s.vel = now.vel
	}
}

// bedrockMoved is BDS MoveActorAbsoluteData::isDifferenceSignificant: the
// pitch, yaw and head yaw (as the bytes the packet carries) turned by more
// than 4 degrees together, the mob moved more than 0.01 blocks, or it moved
// more than 0.001 blocks and has (almost) no velocity left. The body yaw and
// the on-ground flag are not compared: the client turns the body itself.
func bedrockMoved(now, sent sentMovement) bool {
	turn := byteAngleDiff(now.rot[1], sent.rot[1]) + byteAngleDiff(now.rot[0], sent.rot[0]) + byteAngleDiff(now.headYaw, sent.headYaw)
	d := now.pos.Sub(sent.pos).LenSqr()
	return turn > 4 || d > 1e-4 || now.vel.LenSqr() < 1e-6 && d > 1e-6
}

// byteAngleDiff is the difference in degrees between two angles as BDS
// compares them: each packed into a byte (PacketUtil::degreesToByte) and
// read back as a signed byte (byteToDegrees), without wrapping around.
func byteAngleDiff(a, b float64) float64 {
	const step = 360.0 / 256
	return math.Abs(float64(int8(angleStep(a)))-float64(int8(angleStep(b)))) * step
}

// bedrockVelocityChanged is Actor::sendMotionPacketIfNeeded: a
// SetActorMotion when the velocity moved more than 0.2 a tick from the one
// last sent, or became zero.
func bedrockVelocityChanged(vel, sent mgl64.Vec3) bool {
	d := vel.Sub(sent).LenSqr()
	return d > 0.04 || d > 0 && vel.LenSqr() == 0
}

// javaMovedFrom is ServerEntity's test for a move packet: the position moved
// by at least sqrt(7.6293945e-6) blocks from the one last sent, the yaw or
// pitch byte changed (the move packets carry the body yaw), the head yaw byte
// changed (rotate_head), or the mob landed or left the ground (vanilla then
// sends a full entity_position_sync).
func javaMovedFrom(now, sent sentMovement) bool {
	return now.pos.Sub(sent.pos).LenSqr() >= 7.6293945e-6 || angleChanged(now.bodyYaw, sent.bodyYaw) ||
		angleChanged(now.rot[1], sent.rot[1]) || angleChanged(now.headYaw, sent.headYaw) || now.onGround != sent.onGround
}

// javaVelocityChanged is ServerEntity's rule for set_entity_motion: the
// velocity moved by more than sqrt(1e-7) from the one last sent, or it became
// zero.
func javaVelocityChanged(vel, sent mgl64.Vec3) bool {
	d := vel.Sub(sent).LenSqr()
	return d > 1e-7 || d > 0 && vel.LenSqr() == 0
}

// angleChanged reports whether an angle (degrees) changed by enough to show:
// both editions send entity angles as one byte (360/256 degrees a step).
func angleChanged(a, sent float64) bool {
	return angleStep(a) != angleStep(sent)
}

func angleStep(a float64) int64 { return int64(math.Floor(a*256/360)) & 255 }

// SyncMovement shows the mob's movement to Java viewers in this tick's
// StageSend even if their update interval is not due (vanilla
// Entity.needsSync, set by pushes and knockback). Bedrock viewers are shown
// every tick anyway.
func (m *Mob) SyncMovement() { m.s.needsSync = true }

// MarkSent records what viewers were last shown, for custom movement
// senders.
func (s *state) MarkSent(pos, vel mgl64.Vec3, rot cube.Rotation) {
	s.sent = sentMovement{pos: pos, vel: vel, rot: rot, headYaw: s.headYaw, bodyYaw: s.bodyYaw, onGround: s.physics.OnGround}
	s.sentJava = s.sent
}

// MarkSent records that viewers were shown the mob's current movement (for
// custom movement senders).
func (m *Mob) MarkSent() { m.s.MarkSent(m.Position(), m.Velocity(), m.Rotation()) }
