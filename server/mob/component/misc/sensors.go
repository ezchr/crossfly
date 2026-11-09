//go:build crossfly_full

package misc

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// EntitySensor is minecraft:entity_sensor (BDS EntitySensorComponent::tick,
// EntitySubsensor::senseEntities). Every subsensor not on cooldown counts the
// entities in its range that pass event_filters (the entity as "other") and
// fires its event when the count is within [minimum_count, maximum_count].
//
//	relative_range     true: the range is added to the mob's box
//	find_players_only  false
//	subsensors: range [h, v] (10, 10), minimum_count 1, maximum_count -1
//	(no maximum), cooldown -1 s (sense every tick), y_offset 0,
//	event_filters, event.
//
// The pre-1.21 form (sensor_range, event, event_filters, ... at the top) is
// one subsensor.
type EntitySensor struct {
	relative    bool
	playersOnly bool
	subs        []subsensor
}

type subsensor struct {
	h, v     float64
	yOffset  float64
	min, max int
	cooldown int
	filter   mob.Filter
	event    string
	next     int64
}

type rawSubsensor struct {
	Range        json.RawMessage `json:"range"`
	SensorRange  json.RawMessage `json:"sensor_range"`
	MinimumCount *int            `json:"minimum_count"`
	MaximumCount *int            `json:"maximum_count"`
	Cooldown     *float64        `json:"cooldown"`
	YOffset      float64         `json:"y_offset"`
	EventFilters json.RawMessage `json:"event_filters"`
	Event        string          `json:"event"`
}

func newEntitySensor(key string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		rawSubsensor
		RelativeRange   *bool          `json:"relative_range"`
		FindPlayersOnly bool           `json:"find_players_only"`
		Subsensors      []rawSubsensor `json:"subsensors"`
	}{}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	c := &EntitySensor{relative: true, playersOnly: v.FindPlayersOnly}
	if v.RelativeRange != nil {
		c.relative = *v.RelativeRange
	}
	list := v.Subsensors
	if len(list) == 0 && v.Event != "" {
		list = []rawSubsensor{v.rawSubsensor}
	}
	for _, r := range list {
		s := subsensor{h: 10, v: 10, min: 1, max: -1, cooldown: -1, yOffset: r.YOffset, event: r.Event}
		rng := r.Range
		if isNull(rng) {
			rng = r.SensorRange
		}
		if !isNull(rng) {
			var pair []float64
			if json.Unmarshal(rng, &pair) == nil && len(pair) > 0 {
				s.h, s.v = pair[0], pair[len(pair)-1]
			} else {
				var f float64
				if err := json.Unmarshal(rng, &f); err != nil {
					return nil, fmt.Errorf("%v: range: %w", key, err)
				}
				s.h, s.v = f, f
			}
		}
		if r.MinimumCount != nil {
			s.min = *r.MinimumCount
		}
		if r.MaximumCount != nil {
			s.max = *r.MaximumCount
		}
		if r.Cooldown != nil && *r.Cooldown >= 0 {
			s.cooldown = ticks(*r.Cooldown)
		}
		f, err := mob.CompileFilter(r.EventFilters)
		if err != nil {
			return nil, fmt.Errorf("%v: event_filters: %w", key, err)
		}
		s.filter = f
		c.subs = append(c.subs, s)
	}
	return c, nil
}

// Attach ...
func (*EntitySensor) Attach(*mob.Mob) {}

// Detach ...
func (*EntitySensor) Detach(*mob.Mob) {}

// box is the sensing box of a subsensor (EntitySensorUtils::getSensorBox).
func (c *EntitySensor) box(m *mob.Mob, s *subsensor) cube.BBox {
	if c.relative {
		b := m.BBox()
		return boxOf(b.Min()[0]-s.h, b.Min()[1]-s.v+s.yOffset, b.Min()[2]-s.h, b.Max()[0]+s.h, b.Max()[1]+s.v+s.yOffset, b.Max()[2]+s.h)
	}
	p := m.Position()
	return boxOf(p[0]-s.h, p[1]-s.v+s.yOffset, p[2]-s.h, p[0]+s.h, p[1]+s.v+s.yOffset, p[2]+s.h)
}

func (c *EntitySensor) tick(m *mob.Mob) {
	if m.Dead() {
		return
	}
	now := m.Ticks()
	for i := range c.subs {
		s := &c.subs[i]
		if s.cooldown >= 0 && now < s.next {
			continue
		}
		if s.cooldown >= 0 {
			s.next = now + int64(max(s.cooldown, 1))
		}
		count := 0
		var last world.Entity
		for _, e := range entitiesTouching(m, c.box(m, s)) {
			if !sense.Alive(e) {
				continue
			}
			if _, ok := e.(*player.Player); c.playersOnly && !ok {
				continue
			}
			if !s.filter.Test(otherCtx(m, e)) {
				continue
			}
			count++
			last = e
		}
		if count < s.min || (s.max >= 0 && count > s.max) || s.event == "" {
			continue
		}
		ctx := m.Ctx()
		ctx.Other = last
		m.Fire(s.event, ctx)
		if m.Dead() || m.H().Closed() {
			return
		}
		if cur, ok := mob.ComponentOf[*EntitySensor](m); !ok || cur != c {
			return // the event replaced the sensor
		}
	}
}

// BlockSensor is minecraft:block_sensor (bees, piglins): when a block of a
// block_list is broken within sensor_radius (16) by a source passing
// "sources" (the breaker as "other"), on_block_broken fires.
//
// Player breaking and explosions report breaks through world.BlockBroken,
// which calls BlockBroken (block_break.go).
type BlockSensor struct {
	radius  float64
	sources mob.Filter
	onBreak []blockBreak
}

type blockBreak struct {
	blocks []string
	event  string
}

func newBlockSensor(key string, raw json.RawMessage) (mob.Component, error) {
	v := struct {
		Radius  *float64        `json:"sensor_radius"`
		Sources json.RawMessage `json:"sources"`
		OnBreak []struct {
			BlockList []string `json:"block_list"`
			Event     string   `json:"on_block_broken"`
		} `json:"on_break"`
	}{}
	if err := decode(raw, &v); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	c := &BlockSensor{radius: 16}
	if v.Radius != nil {
		c.radius = *v.Radius
	}
	f, err := mob.CompileFilter(v.Sources)
	if err != nil {
		return nil, fmt.Errorf("%v: sources: %w", key, err)
	}
	c.sources = f
	for _, b := range v.OnBreak {
		bb := blockBreak{event: b.Event}
		for _, n := range b.BlockList {
			bb.blocks = append(bb.blocks, NormaliseBlockName(n))
		}
		c.onBreak = append(c.onBreak, bb)
	}
	return c, nil
}

// Attach registers the mob as a block break listener of its world
// (BlockBreakSensorComponent::initialize -> BlockEventDispatcher::registerListener).
func (*BlockSensor) Attach(m *mob.Mob) { blockSensors.add(m) }

// Detach ...
func (*BlockSensor) Detach(m *mob.Mob) { blockSensors.remove(m) }

// BlockBroken tells the mobs with minecraft:block_sensor around pos that b
// was broken there by breaker (nil when nothing broke it). The world calls
// it through world.BlockBroken (player breaking, explosions; see
// block_break.go). Like BDS BlockEventDispatcher::dispatchEvent it only
// visits the registered sensors of the world, each within its radius of
// its box centre.
func BlockBroken(tx *world.Tx, pos cube.Pos, b world.Block, breaker world.Entity) {
	if blockSensors.n.Load() == 0 {
		return
	}
	hs := blockSensors.handles(tx.World())
	if len(hs) == 0 {
		return
	}
	name, _ := b.EncodeBlock()
	name = NormaliseBlockName(name)
	c := pos.Vec3Centre()
	for _, h := range hs {
		m, ok := (*mob.Mob)(nil), false
		if !h.Closed() {
			m, ok = mob.OfHandle(tx, h)
		}
		if !ok {
			blockSensors.drop(tx.World(), h)
			continue
		}
		if m.Dead() {
			continue
		}
		s, ok := mob.ComponentOf[*BlockSensor](m)
		if !ok || boxCentre(m.BBox()).Sub(c).Len() > s.radius {
			continue
		}
		ctx := m.Ctx()
		ctx.Block, ctx.BlockPos, ctx.HasBlock = b, pos, true
		if breaker != nil {
			ctx.Other = breaker
			if _, ok := breaker.(*player.Player); ok {
				ctx.Player = breaker
			}
		}
		if !s.sources.Test(ctx) {
			continue
		}
		for _, ob := range s.onBreak {
			if matchBlock(ob.blocks, name) && ob.event != "" {
				m.Fire(ob.event, ctx)
				break
			}
		}
	}
}

func matchBlock(list []string, name string) bool {
	for _, n := range list {
		if n == name {
			return true
		}
	}
	return false
}

// blockAliases maps names entity JSON uses to the names Dragonfly encodes.
var blockAliases = map[string]string{
	"minecraft:grass":               "minecraft:grass_block",
	"minecraft:item.soul_campfire":  "minecraft:soul_campfire",
	"minecraft:item.campfire":       "minecraft:campfire",
	"minecraft:waterlily":           "minecraft:waterlily",
	"minecraft:portal":              "minecraft:portal",
	"minecraft:flowing_water":       "minecraft:water",
	"minecraft:stationary_water":    "minecraft:water",
	"minecraft:frog_spawn":          "minecraft:frog_spawn",
	"minecraft:cave_vines_head":     "minecraft:cave_vines_head_with_berries",
	"minecraft:sweet_berry_bush":    "minecraft:sweet_berry_bush",
	"minecraft:red_flower":          "minecraft:poppy",
	"minecraft:yellow_flower":       "minecraft:dandelion",
	"minecraft:turtle_egg":          "minecraft:turtle_egg",
	"minecraft:monster_egg":         "minecraft:infested_stone",
	"minecraft:noteblock":           "minecraft:noteblock",
	"minecraft:note_block":          "minecraft:noteblock",
	"minecraft:lit_pumpkin":         "minecraft:lit_pumpkin",
	"minecraft:respawn_anchor":      "minecraft:respawn_anchor",
	"minecraft:warped_fungus":       "minecraft:warped_fungus",
	"minecraft:dirt_with_roots":     "minecraft:dirt_with_roots",
	"minecraft:pale_moss_block":     "minecraft:pale_moss_block",
	"minecraft:muddy_mangrove_root": "minecraft:muddy_mangrove_roots",
}

// NormaliseBlockName adds the minecraft namespace and maps legacy names.
func NormaliseBlockName(n string) string {
	n = strings.ToLower(strings.TrimSpace(n))
	if !strings.Contains(n, ":") {
		n = "minecraft:" + n
	}
	if a, ok := blockAliases[n]; ok {
		return a
	}
	return n
}
