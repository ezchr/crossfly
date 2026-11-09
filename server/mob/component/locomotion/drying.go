//go:build crossfly_full

package locomotion

import (
	"encoding/json"
	"slices"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// dryingOutTimer is minecraft:drying_out_timer (BDS DryingOutTimerComponent,
// DryingOutTimerSystem; axolotl, dolphin): out of water and rain the mob
// dries out over total_time seconds and then fires dried_out_event; water or
// rain stops the timer for good (stopped_drying_out_event, the events usually
// swap the component). RefillWater (a water bottle) adds
// water_bottle_refill_time seconds and fires recover_after_dried_out_event
// when it was dried out. The timer and state are saved.
type dryingOutTimer struct {
	total, refill                int
	driedOut, stopped, recovered sense.Trigger

	timer int
	state int // 0 drying, 1 dried out, 2 stopped
}

func newDryingOutTimer(_ string, raw json.RawMessage) (mob.Component, error) {
	j := struct {
		Total     float64         `json:"total_time"`
		Refill    float64         `json:"water_bottle_refill_time"`
		DriedOut  json.RawMessage `json:"dried_out_event"`
		Stopped   json.RawMessage `json:"stopped_drying_out_event"`
		Recovered json.RawMessage `json:"recover_after_dried_out_event"`
	}{}
	if err := decode(raw, &j); err != nil {
		return nil, err
	}
	c := &dryingOutTimer{total: int(j.Total * 20), refill: int(j.Refill * 20)}
	var err error
	if c.driedOut, err = sense.ParseTrigger(j.DriedOut); err != nil {
		return nil, err
	}
	if c.stopped, err = sense.ParseTrigger(j.Stopped); err != nil {
		return nil, err
	}
	if c.recovered, err = sense.ParseTrigger(j.Recovered); err != nil {
		return nil, err
	}
	c.timer = c.total
	return c, nil
}

func (c *dryingOutTimer) Attach(*mob.Mob) {}
func (c *dryingOutTimer) Detach(*mob.Mob) {}

// Tick is DryingOutTimerSystem::_tickDryingOutTimerComponent.
func (c *dryingOutTimer) Tick(m *mob.Mob) {
	if m.Dead() || c.state == 2 {
		return
	}
	if sense.InWaterOrRain(m.Tx(), m) {
		c.state = 2
		c.stopped.Fire(m, nil)
		return
	}
	if entity.AffectedByWaterBottle(m) {
		c.RefillWater(m)
	}
	if c.timer > 0 {
		c.timer--
	}
	if c.timer <= 0 && c.state == 0 {
		c.state = 1
		c.driedOut.Fire(m, nil)
	}
}

// RefillWater is a water bottle splashing the mob
// (Actor::isAffectedByWaterBottle).
func (c *dryingOutTimer) RefillWater(m *mob.Mob) {
	if c.refill == 0 {
		return
	}
	c.timer = min(c.timer+c.refill, c.total)
	if c.state == 1 {
		c.state = 0
		c.recovered.Fire(m, nil)
	}
}

// Remaining returns the ticks left before the mob dries out.
func (c *dryingOutTimer) Remaining() int { return c.timer }

func (c *dryingOutTimer) EncodeNBT() map[string]any {
	return map[string]any{"DryingOutTimer": int32(c.timer), "DryingOutState": int32(c.state)}
}

func (c *dryingOutTimer) DecodeNBT(_ *mob.Mob, d map[string]any) {
	if v, ok := d["DryingOutTimer"].(int32); ok {
		c.timer = int(v)
	}
	if v, ok := d["DryingOutState"].(int32); ok {
		c.state = int(v)
	}
}

// bribeable is minecraft:bribeable (BDS BribeableComponent, dolphin): a player
// feeding one of bribe_items bribes the mob (the item is used up, hearts
// show) after bribe_cooldown seconds (2) since the last bribe;
// find_underwater_treasure runs while it is bribed.
type bribeable struct {
	items    []string
	cooldown int
	bribed   bool
	last     int64
}

func newBribeable(_ string, raw json.RawMessage) (mob.Component, error) {
	j := struct {
		Items    []string `json:"bribe_items"`
		Cooldown *float64 `json:"bribe_cooldown"`
	}{}
	if err := decode(raw, &j); err != nil {
		return nil, err
	}
	c := &bribeable{cooldown: 40, last: -1 << 40}
	if j.Cooldown != nil {
		c.cooldown = int(*j.Cooldown * 20)
	}
	for _, it := range j.Items {
		c.items = append(c.items, itemName(it))
	}
	return c, nil
}

// itemName maps legacy item names (fish, salmon) to identifiers.
func itemName(s string) string {
	switch s {
	case "fish":
		return "minecraft:cod"
	case "salmon":
		return "minecraft:salmon"
	}
	if len(s) > 0 && !containsColon(s) {
		return "minecraft:" + s
	}
	return s
}

func containsColon(s string) bool {
	for _, r := range s {
		if r == ':' {
			return true
		}
	}
	return false
}

func (c *bribeable) Attach(*mob.Mob) {}
func (c *bribeable) Detach(*mob.Mob) {}

func (c *bribeable) Bribed() bool { return c.bribed }
func (c *bribeable) ResetBribe()  { c.bribed = false }

// Interact implements mob.Interactor.
func (c *bribeable) Interact(m *mob.Mob, user item.User, held item.Stack, _ mgl64.Vec3) (item.Stack, bool) {
	if held.Empty() || c.bribed || m.Ticks()-c.last < int64(c.cooldown) {
		return held, false
	}
	name, _ := held.Item().EncodeItem()
	if !slices.Contains(c.items, name) {
		return held, false
	}
	c.bribed, c.last = true, m.Ticks()
	if g, ok := user.(interface{ GameMode() world.GameMode }); ok && g.GameMode().CreativeInventory() {
		return held, true
	}
	return held.Grow(-1), true
}
