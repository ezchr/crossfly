//go:build crossfly_full

package core

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// spawnEntity is minecraft:spawn_entity: every min..max_wait_time seconds
// each entry drops an item (chicken eggs) or spawns an entity, if its
// filters pass (BDS SpawnActorComponent / SpawnActorEntry).
type spawnEntity struct {
	entries []*spawnEntry
}

type spawnEntry struct {
	MinWait   float64         `json:"min_wait_time"`
	MaxWait   float64         `json:"max_wait_time"`
	Entity    string          `json:"spawn_entity"`
	Event     string          `json:"spawn_event"`
	Method    string          `json:"spawn_method"`
	Item      string          `json:"spawn_item"`
	Sound     string          `json:"spawn_sound"`
	Num       int             `json:"num_to_spawn"`
	SingleUse bool            `json:"single_use"`
	Filters   json.RawMessage `json:"filters"`
	ItemEvent *Trigger        `json:"spawn_item_event"`

	filter mob.Filter
	left   int
	used   bool
}

func newSpawnEntity(key string, raw json.RawMessage) (mob.Component, error) {
	var j struct {
		Entities json.RawMessage `json:"entities"`
	}
	if err := decode(raw, &j); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	list := bytes.TrimSpace(j.Entities)
	if len(list) == 0 {
		// Old format: the entry fields on the component itself.
		list = raw
	}
	if len(list) > 0 && list[0] != '[' {
		list = append(append([]byte{'['}, list...), ']')
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(list, &raws); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	c := &spawnEntity{}
	for _, r := range raws {
		e := &spawnEntry{MinWait: 300, MaxWait: 600, Event: mob.EventBorn, Sound: "plop", Num: 1}
		if err := json.Unmarshal(r, e); err != nil {
			return nil, fmt.Errorf("%v: %w", key, err)
		}
		f, err := mob.CompileFilter(e.Filters)
		if err != nil {
			return nil, fmt.Errorf("%v: %w", key, err)
		}
		e.filter = f
		if e.Item == "" && e.Entity == "" {
			e.Item = "egg"
		}
		c.entries = append(c.entries, e)
	}
	return c, nil
}

func (e *spawnEntry) reset(m *mob.Mob) {
	lo, hi := ticksOf(e.MinWait), ticksOf(e.MaxWait)
	if hi <= lo {
		e.left = lo
		return
	}
	e.left = lo + m.Rand().IntN(hi-lo+1)
}

func (c *spawnEntity) Attach(m *mob.Mob) {
	for _, e := range c.entries {
		e.reset(m)
	}
}
func (c *spawnEntity) Detach(*mob.Mob) {}

// Tick counts every entry down and spawns when one runs out.
func (c *spawnEntity) Tick(m *mob.Mob) {
	for _, e := range c.entries {
		if e.used {
			continue
		}
		if e.left > 0 {
			e.left--
			continue
		}
		e.reset(m)
		if !e.filter.Test(m.Ctx()) {
			continue
		}
		e.spawn(m)
		if e.SingleUse {
			e.used = true
		}
		if m.Dead() {
			return
		}
	}
}

func (e *spawnEntry) spawn(m *mob.Mob) {
	tx, pos := m.Tx(), m.Position()
	if e.Item != "" {
		if s, ok := ParseItemDescriptor(e.Item).Stack(max(e.Num, 1)); ok {
			tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: pos, Velocity: mgl64.Vec3{0, 0.2, 0}}, s))
		}
		if e.ItemEvent != nil {
			e.ItemEvent.Fire(m, nil)
		}
	}
	if e.Entity != "" {
		for i := 0; i < max(e.Num, 1); i++ {
			ev := e.Event
			if ev == "" {
				ev = mob.EventBorn
			}
			_, _ = mob.Spawn(tx, e.Entity, pos, mob.SpawnOptions{Rotation: m.Rotation(), Event: ev})
		}
	}
	if e.Sound != "" {
		PlayMobSound(m, e.Sound)
	}
}

func (c *spawnEntity) EncodeNBT() map[string]any {
	left := make([]int32, len(c.entries))
	for i, e := range c.entries {
		left[i] = int32(e.left)
		if e.used {
			left[i] = -1
		}
	}
	return map[string]any{"Left": left}
}

func (c *spawnEntity) DecodeNBT(_ *mob.Mob, d map[string]any) {
	left, _ := d["Left"].([]int32)
	for i, e := range c.entries {
		if i < len(left) {
			if left[i] < 0 {
				e.used = true
			} else {
				e.left = int(left[i])
			}
		}
	}
}
