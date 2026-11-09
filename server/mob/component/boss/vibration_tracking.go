//go:build crossfly_full

package boss

import (
	"encoding/json"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
)

func init() {
	mob.RegisterComponent("minecraft:game_event_movement_tracking", newMovementTracking)
}

// MovementTracking is minecraft:game_event_movement_tracking (BDS
// GameEventMovementTrackingDefinition/Component,
// GameEventMovementTrackingSystem::_trackMovement): which game events the
// mob's own movement makes.
//
//	emit_move  (default true)  steps on the ground: entity_move
//	emit_swim  (default true)  moving in water or lava: swim
//	emit_flap  (default false) in the air (not on the ground, a ladder,
//	                           scaffolding, powder snow or cobweb, nor in a
//	                           liquid): flap, at most once every 10 ticks
//
// Allays, bats, bees, chickens, the ender dragon, parrots and phantoms flap;
// the vex makes no steps or swims. Dragonfly has no game event dispatcher
// and no sculk blocks: the only listener is the warden's
// minecraft:vibration_listener, which notices steps and swims itself
// (VibrationListener.scan) and asks this component whether to; flaps are
// sent to the listeners within 16 blocks.
type MovementTracking struct {
	EmitMove, EmitSwim, EmitFlap bool
	nextFlap                     int64
}

func newMovementTracking(_ string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		Move *bool `json:"emit_move"`
		Swim *bool `json:"emit_swim"`
		Flap *bool `json:"emit_flap"`
	}{}
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
	}
	or := func(p *bool, d bool) bool {
		if p == nil {
			return d
		}
		return *p
	}
	return &MovementTracking{EmitMove: or(v.Move, true), EmitSwim: or(v.Swim, true), EmitFlap: or(v.Flap, false)}, nil
}

// Attach ...
func (*MovementTracking) Attach(*mob.Mob) {}

// Detach ...
func (*MovementTracking) Detach(*mob.Mob) {}

// Tick sends a flap every 10 ticks while the mob is in the air.
func (c *MovementTracking) Tick(m *mob.Mob) {
	if !c.EmitFlap || m.Dead() {
		return
	}
	now := m.Ticks()
	if now < c.nextFlap || !airborne(m) {
		return
	}
	c.nextFlap = now + 10
	vib := Vibration{Event: VibrationFlap, Pos: m.Position(), Source: m.H()}
	tx := m.Tx()
	for _, h := range listenersIn(tx) {
		e, ok := h.Entity(tx)
		if !ok {
			forgetListener(tx, h)
			continue
		}
		if lm, ok := mob.Of(e); ok && lm.H() != m.H() {
			if l, ok := ListenerOf(lm); ok {
				l.Hear(lm, vib) // Hear checks the range.
			} else {
				forgetListener(tx, h)
			}
		}
	}
}

// airborne is the else branch of _trackMovement: not on the ground, a
// climbable block, scaffolding, powder snow or cobweb, and not in water or
// lava.
func airborne(m *mob.Mob) bool {
	p := m.Physics()
	if m.OnGround() || p.InWater || p.InLava || m.Meta().Climbing {
		return false
	}
	switch blockName(m.Tx().Block(cube.PosFromVec3(m.Position()))) {
	case "minecraft:ladder", "minecraft:vine", "minecraft:scaffolding", "minecraft:powder_snow", "minecraft:web",
		"minecraft:twisting_vines", "minecraft:weeping_vines", "minecraft:cave_vines", "minecraft:cave_vines_body_with_berries",
		"minecraft:cave_vines_head_with_berries":
		return false
	}
	return true
}

// tracksMove reports whether e's steps (swim: swims) make vibrations: every
// entity unless a game_event_movement_tracking turns them off.
func tracksMove(e world.Entity, swim bool) bool {
	m, ok := mob.Of(e)
	if !ok {
		return true
	}
	c, ok := mob.ComponentOf[*MovementTracking](m)
	if !ok {
		return true
	}
	if swim {
		return c.EmitSwim
	}
	return c.EmitMove
}

// listeners are the mobs with a vibration_listener of each world, added when
// the component is attached (also after loading). Components are not
// detached when a mob is removed or unloaded, so flaps drop the handles that
// are no longer in the world. With no warden about, a flap costs one map
// lookup.
var listeners = struct {
	sync.Mutex
	m map[*world.World]map[*world.EntityHandle]struct{}
}{m: map[*world.World]map[*world.EntityHandle]struct{}{}}

func addListener(m *mob.Mob) {
	tx := m.Tx()
	if tx == nil {
		return
	}
	listeners.Lock()
	defer listeners.Unlock()
	set, ok := listeners.m[tx.World()]
	if !ok {
		set = map[*world.EntityHandle]struct{}{}
		listeners.m[tx.World()] = set
	}
	set[m.H()] = struct{}{}
}

func forgetListener(tx *world.Tx, h *world.EntityHandle) {
	listeners.Lock()
	defer listeners.Unlock()
	if set, ok := listeners.m[tx.World()]; ok {
		delete(set, h)
		if len(set) == 0 {
			delete(listeners.m, tx.World())
		}
	}
}

func listenersIn(tx *world.Tx) []*world.EntityHandle {
	listeners.Lock()
	defer listeners.Unlock()
	set := listeners.m[tx.World()]
	if len(set) == 0 {
		return nil
	}
	out := make([]*world.EntityHandle, 0, len(set))
	for h := range set {
		out = append(out, h)
	}
	return out
}
