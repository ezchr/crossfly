//go:build crossfly_full

package misc

import (
	"encoding/json"
	"math"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// The happy ghast's, creaking's and sulfur cube's body and picking flags.
// None of them is in the 1.21.3 dump; BDS 26.60 (bedrock_server.exe RTTI)
// has BodyRotationAlwaysFollowsHeadDefinition (v1_21_120) and
// NotPickableFromInsideDefinition (v1_26_40) with NoRuntimeTypeTag: no
// server component, only the actor flag the client acts on.
// RotationAxisAlignedDefinition (v1_21_120) adds AxisAlignedComponent and
// is_collidable a CollidableMobFlagComponent.
func init() {
	// The client keeps the body facing the head.
	mob.RegisterComponent("minecraft:body_rotation_always_follows_head", newFollowsHead)
	// The happy ghast standing still with players on it: body on a block axis.
	mob.RegisterComponent("minecraft:rotation_axis_aligned", newAxisAligned)
	// Players inside the mob's box cannot target it (creaking, sulfur cube).
	mob.RegisterComponent("minecraft:not_pickable_from_inside", newActorFlag(protocol.EntityDataFlagNotPickableFromInside, nil))
	// Other entities collide with the mob's box and may stand on it (the
	// happy ghast standing still, the shulker).
	mob.RegisterComponent("minecraft:is_collidable", newActorFlag(protocol.EntityDataFlagCollidable, nil))
}

// followsHead is minecraft:body_rotation_always_follows_head: actor flag
// body_rotation_always_follows_head, and the body yaw the server sends
// follows the head too, as the client turns it.
type followsHead struct{}

func newFollowsHead(string, json.RawMessage) (mob.Component, error) { return followsHead{}, nil }

// Attach ...
func (followsHead) Attach(m *mob.Mob) {
	m.SetActorFlag(protocol.EntityDataFlagBodyRotationAlwaysFollowsHead, true)
}

// Detach ...
func (followsHead) Detach(m *mob.Mob) {
	m.SetActorFlag(protocol.EntityDataFlagBodyRotationAlwaysFollowsHead, false)
}

// TickStage runs after the body control.
func (followsHead) TickStage() mob.Stage { return mob.StageBody }

// Tick ...
func (followsHead) Tick(m *mob.Mob) { m.SetBodyYaw(m.HeadYaw()) }

// axisAligned is minecraft:rotation_axis_aligned: actor flag
// body_rotation_axis_aligned, and the body (and facing) snapped to the
// nearest of the four block axes, so the box players stand on lines up with
// the blocks around it.
type axisAligned struct{}

func newAxisAligned(string, json.RawMessage) (mob.Component, error) { return axisAligned{}, nil }

// Attach ...
func (axisAligned) Attach(m *mob.Mob) {
	m.SetActorFlag(protocol.EntityDataFlagBodyRotationAxisAligned, true)
}

// Detach ...
func (axisAligned) Detach(m *mob.Mob) {
	m.SetActorFlag(protocol.EntityDataFlagBodyRotationAxisAligned, false)
}

// TickStage runs after every body ticker (body control,
// body_rotation_blocked, body_rotation_always_follows_head), which tick in
// StageBody in definition order.
func (axisAligned) TickStage() mob.Stage { return mob.StageEnvironment }

// Tick ...
func (axisAligned) Tick(m *mob.Mob) {
	y := AxisYaw(m.BodyYaw())
	m.SetBodyYaw(y)
	m.SetYaw(y)
}

// AxisYaw is yaw rounded to the nearest multiple of 90 degrees.
func AxisYaw(yaw float64) float64 {
	y := math.Round(yaw/90) * 90
	if y == -0 {
		y = 0
	}
	return y
}
