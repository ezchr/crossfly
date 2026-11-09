//go:build crossfly_full

package mob

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Navigation is a mob's path navigation (BDS NavigationComponent +
// PathNavigation). Package mob/nav (B) implements it and attaches it from
// its minecraft:navigation.* components with Mob.SetNavigation. A mob
// without one has a navigation that never moves.
type Navigation interface {
	// Tick advances along the current path and sets the move control's
	// wanted position (StageNavigation).
	Tick(m *Mob)
	// CreatePath finds a path to pos without following it (nil if none).
	CreatePath(m *Mob, pos mgl64.Vec3) Path
	// CreatePathToEntity finds a path to e without following it.
	CreatePathToEntity(m *Mob, e world.Entity) Path
	// MoveTo paths to pos and follows it at speed (a multiplier of the
	// movement attribute). It reports whether a path was found.
	MoveTo(m *Mob, pos mgl64.Vec3, speed float64) bool
	// MoveToEntity is MoveTo towards an entity's position.
	MoveToEntity(m *Mob, e world.Entity, speed float64) bool
	// MoveAlong follows a path made by CreatePath.
	MoveAlong(m *Mob, p Path, speed float64) bool
	// Path is the path being followed, or nil.
	Path() Path
	// Stop drops the path.
	Stop(m *Mob)
	// Done reports whether there is no path left to follow.
	Done() bool
	// SetSpeed changes the speed multiplier of the current path.
	SetSpeed(speed float64)
}

// Path is a found path.
type Path interface {
	// Len is the number of nodes, Index the next node to reach.
	Len() int
	Index() int
	// Node returns node i (block position the mob stands in).
	Node(i int) cube.Pos
	// End is the last node.
	End() cube.Pos
	// Complete reports whether the path reaches its destination (BDS
	// partial paths end at the closest node instead).
	Complete() bool
	// Done reports whether every node was reached.
	Done() bool
}

// MoveControl turns a wanted position into movement input (BDS MoveControl
// and its variants). Package mob/control (C) implements it, attached by the
// minecraft:movement.* components.
type MoveControl interface {
	Tick(m *Mob)
	// SetWantedPosition makes the mob walk to pos at speed (multiplier).
	SetWantedPosition(pos mgl64.Vec3, speed float64)
	// HasWanted reports whether a wanted position is set.
	HasWanted() bool
	// WantedPosition returns it.
	WantedPosition() mgl64.Vec3
	// Speed is the current speed multiplier.
	Speed() float64
	// Strafe makes the mob move relative to its facing (ranged attack).
	Strafe(forward, right float64)
	// Stop clears the wanted position and input.
	Stop()
}

// LookControl turns the head towards a point (BDS LookControl).
type LookControl interface {
	Tick(m *Mob)
	// SetLookAt looks at pos, turning at most yawMax/pitchMax degrees a
	// tick.
	SetLookAt(pos mgl64.Vec3, yawMax, pitchMax float64)
	// SetLookAtEntity looks at e's eyes.
	SetLookAtEntity(e world.Entity, yawMax, pitchMax float64)
	// HasWanted reports whether there is something to look at this tick.
	HasWanted() bool
}

// JumpControl makes the mob jump (BDS JumpControlComponent, jump.static /
// jump.dynamic).
type JumpControl interface {
	Tick(m *Mob)
	// Jump asks for a jump on the next travel step.
	Jump()
	// Jumping reports whether a jump is pending.
	Jumping() bool
}

// BodyControl turns the body after the head and movement (BDS BodyControl).
type BodyControl interface {
	Tick(m *Mob)
}

// Input is what the controls hand to travel each tick. Package mob/control
// writes it and package mob/physics reads it; other packages only read it.
type Input struct {
	// Forward and Strafe are the movement input (-1..1 scaled by Speed in
	// BDS terms: xxa/zza), Vertical is used by fliers and swimmers.
	Forward, Strafe, Vertical float64
	// Speed is the movement speed for this tick (attribute x multiplier).
	Speed float64
	// Jump is set by the jump control for this travel step.
	Jump bool
}

// Physics is the result of the last travel step. Package mob/physics writes
// it; everyone else reads it.
type Physics struct {
	OnGround            bool
	HorizontalCollision bool
	VerticalCollision   bool
	InWater, InLava     bool
	// Underwater is whether the eyes are in water.
	Underwater bool
	// FallDistance is how far the mob has fallen since it last stood.
	FallDistance float64
}

type nopNavigation struct{}

func (nopNavigation) Tick(*Mob)                                     {}
func (nopNavigation) CreatePath(*Mob, mgl64.Vec3) Path              { return nil }
func (nopNavigation) CreatePathToEntity(*Mob, world.Entity) Path    { return nil }
func (nopNavigation) MoveTo(*Mob, mgl64.Vec3, float64) bool         { return false }
func (nopNavigation) MoveToEntity(*Mob, world.Entity, float64) bool { return false }
func (nopNavigation) MoveAlong(*Mob, Path, float64) bool            { return false }
func (nopNavigation) Path() Path                                    { return nil }
func (nopNavigation) Stop(*Mob)                                     {}
func (nopNavigation) Done() bool                                    { return true }
func (nopNavigation) SetSpeed(float64)                              {}

type nopMove struct{}

func (nopMove) Tick(*Mob)                             {}
func (nopMove) SetWantedPosition(mgl64.Vec3, float64) {}
func (nopMove) HasWanted() bool                       { return false }
func (nopMove) WantedPosition() mgl64.Vec3            { return mgl64.Vec3{} }
func (nopMove) Speed() float64                        { return 0 }
func (nopMove) Strafe(float64, float64)               {}
func (nopMove) Stop()                                 {}

type nopLook struct{}

func (nopLook) Tick(*Mob)                                      {}
func (nopLook) SetLookAt(mgl64.Vec3, float64, float64)         {}
func (nopLook) SetLookAtEntity(world.Entity, float64, float64) {}
func (nopLook) HasWanted() bool                                { return false }

type nopJump struct{}

func (nopJump) Tick(*Mob)     {}
func (nopJump) Jump()         {}
func (nopJump) Jumping() bool { return false }

type nopBody struct{}

func (nopBody) Tick(*Mob) {}
