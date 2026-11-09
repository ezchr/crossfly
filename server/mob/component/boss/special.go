//go:build crossfly_full

package boss

import (
	"encoding/json"

	"github.com/df-mc/dragonfly/server/mob"
)

// --- minecraft:instant_despawn (BDS InstantDespawnComponent) ---

// instantDespawn is minecraft:instant_despawn: the mob despawns (no drops)
// on its next tick (the creaking crumbling). remove_child_entities is not
// modelled (no leashes).
type instantDespawn struct{}

func newInstantDespawn(string, json.RawMessage) (mob.Component, error) { return instantDespawn{}, nil }

// Attach ...
func (instantDespawn) Attach(*mob.Mob) {}

// Detach ...
func (instantDespawn) Detach(*mob.Mob) {}

// Tick ...
func (instantDespawn) Tick(m *mob.Mob) { _ = m.Close() }

// TickStage despawns after the other sensors ran.
func (instantDespawn) TickStage() mob.Stage { return mob.StageDespawn }

// --- minecraft:body_rotation_blocked ---

// bodyRotationBlocked is minecraft:body_rotation_blocked (the creaking
// frozen while looked at, the armadillo rolled up): the body keeps the yaw
// it had when the component was added; the head still turns.
type bodyRotationBlocked struct{ yaw float64 }

func newBodyRotationBlocked(string, json.RawMessage) (mob.Component, error) {
	return &bodyRotationBlocked{}, nil
}

// Attach ...
func (c *bodyRotationBlocked) Attach(m *mob.Mob) { c.yaw = m.BodyYaw() }

// Detach ...
func (*bodyRotationBlocked) Detach(*mob.Mob) {}

// TickStage runs after the body control.
func (*bodyRotationBlocked) TickStage() mob.Stage { return mob.StageBody }

// Tick puts the body back.
func (c *bodyRotationBlocked) Tick(m *mob.Mob) {
	m.SetBodyYaw(c.yaw)
	m.SetYaw(c.yaw)
}
