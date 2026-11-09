//go:build crossfly_full

package ride

import (
	"encoding/json"
	"fmt"

	"github.com/df-mc/dragonfly/server/mob"
)

// AddRider is minecraft:addrider (BDS AddRiderComponent/AddRiderDefinition):
// once, when the component is first added (a spider jockey's group, a
// ravager spawned for a raid), it spawns each rider entity_type with its
// spawn_event and puts it on the mob. Both the list form ("riders") and the
// old single form (entity_type, spawn_event) are read.
type AddRider struct {
	riders []riderSpec
	done   bool
}

type riderSpec struct {
	Type  string `json:"entity_type"`
	Event string `json:"spawn_event"`
}

func newAddRider(key string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		riderSpec
		Riders []riderSpec `json:"riders"`
	}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	c := &AddRider{riders: v.Riders}
	if v.Type != "" {
		c.riders = append(c.riders, v.riderSpec)
	}
	return c, nil
}

// Attach ...
func (c *AddRider) Attach(*mob.Mob) {}

// Detach ...
func (c *AddRider) Detach(*mob.Mob) {}

// Tick spawns the riders on the first tick (the mob is in the world then,
// its rideable attached).
func (c *AddRider) Tick(m *mob.Mob) {
	if c.done {
		return
	}
	c.done = true
	if m.Dead() {
		return
	}
	for _, r := range c.riders {
		opts := mob.SpawnOptions{Rotation: m.Rotation(), Event: r.Event}
		rider, err := mob.Spawn(m.Tx(), r.Type, m.Pos(), opts)
		if err != nil {
			continue
		}
		if !rider.StartRiding(m) {
			// No free seat for it (or the vehicle refuses it): BDS keeps it
			// standing next to the vehicle.
			continue
		}
	}
}

// Done reports whether the riders were spawned.
func (c *AddRider) Done() bool { return c.done }

// EncodeNBT remembers that the riders were spawned.
func (c *AddRider) EncodeNBT() map[string]any {
	if !c.done {
		return nil
	}
	return map[string]any{"Spawned": uint8(1)}
}

// DecodeNBT ...
func (c *AddRider) DecodeNBT(_ *mob.Mob, d map[string]any) {
	if v, ok := d["Spawned"].(uint8); ok && v == 1 {
		c.done = true
	}
}
