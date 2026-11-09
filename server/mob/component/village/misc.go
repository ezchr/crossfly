//go:build crossfly_full

package village

import (
	"encoding/json"
	"fmt"
	"image/color"
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/filter"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// Effects Dragonfly lacks: bad omen (28), hero of the village (29), raid
// omen (36). They do nothing by themselves; the raid code reads them.
var (
	BadOmen          = omenEffect{c: color.RGBA{R: 0x0b, G: 0x61, B: 0x38, A: 0xff}}
	HeroOfTheVillage = omenEffect{c: color.RGBA{R: 0x44, G: 0xff, B: 0x44, A: 0xff}, hero: true}
	RaidOmen         = omenEffect{c: color.RGBA{R: 0xde, G: 0x40, B: 0x58, A: 0xff}, raid: true}
)

type omenEffect struct {
	c          color.RGBA
	hero, raid bool
}

func (o omenEffect) RGBA() color.RGBA                { return o.c }
func (omenEffect) Apply(world.Entity, effect.Effect) {}
func (omenEffect) Start(world.Entity, int)           {}
func (omenEffect) End(world.Entity, int)             {}

// Actor flags used by these goals (gophertunnel protocol.EntityDataFlag*).
const (
	FlagSleeping           = protocol.EntityDataFlagSleeping
	FlagCelebrating        = protocol.EntityDataFlagCelebrating
	FlagCelebratingSpecial = protocol.EntityDataFlagCelebratingSpecial
	FlagAdmiring           = protocol.EntityDataFlagAdmiring
	FlagTradeInterest      = protocol.EntityDataFlagTradeInterest
	FlagBribed             = protocol.EntityDataFlagBribed
)

type sleepKey struct{}

// sleepState is a mob asleep in a bed (SleepGoal).
type sleepState struct{ bed cube.Pos }

// SetSleeping marks m asleep in the bed at bed (or awake) and shows it.
func SetSleeping(m *mob.Mob, v bool, bed cube.Pos) {
	if v {
		m.SetData(sleepKey{}, &sleepState{bed: bed})
	} else {
		m.SetData(sleepKey{}, nil)
	}
	m.SetActorFlag(FlagSleeping, v)
}

// IsSleeping reports whether m sleeps.
func IsSleeping(m *mob.Mob) bool {
	_, ok := m.Data(sleepKey{})
	return ok
}

// SleepingBed returns the bed m sleeps in.
func SleepingBed(m *mob.Mob) (cube.Pos, bool) {
	v, ok := m.Data(sleepKey{})
	if !ok {
		return cube.Pos{}, false
	}
	return v.(*sleepState).bed, true
}

func init() {
	filter.RegisterFlagSource(func(m *mob.Mob, f filter.Flag) (bool, bool) {
		switch f {
		case filter.FlagSleeping:
			return IsSleeping(m), true
		case filter.FlagTradeSupply:
			if e, ok := EconomyOf(m); ok {
				return e.HasSupply(), true
			}
		case filter.FlagInVillage:
			if m.Tx() != nil {
				return ManagerOf(m.Tx()).VillageAt(m.Position()) != nil, true
			}
		case filter.FlagInCaravan:
			_, ok := CaravanHead(m)
			return ok || HasCaravanTail(m), true
		case filter.FlagRaider:
			if v, ok := VillageOf(m); ok && v.raid != nil && v.HasDweller(RoleHostile, m.H()) {
				return true, true
			}
		}
		return false, false
	})
}

// Inventory returns the mob's inventory: the one of a component that has
// one (minecraft:inventory), else a private one sized from the
// minecraft:inventory JSON (inventory_size, default 5). The private one is
// not saved.
func Inventory(m *mob.Mob) *inventory.Inventory {
	if c, ok := mob.ComponentOf[interface{ Inventory() *inventory.Inventory }](m); ok {
		if inv := c.Inventory(); inv != nil {
			return inv
		}
	}
	if v, ok := m.Data(invKey{}); ok {
		return v.(*inventory.Inventory)
	}
	size := 5
	if raw, ok := m.ComponentJSON("minecraft:inventory"); ok {
		var j struct {
			Size int `json:"inventory_size"`
		}
		if json.Unmarshal(raw, &j) == nil && j.Size > 0 {
			size = j.Size
		}
	}
	inv := inventory.New(size, nil)
	m.SetData(invKey{}, inv)
	return inv
}

type invKey struct{}

// CountItems counts the items matching name in the mob's inventory.
func CountItems(m *mob.Mob, d core.ItemDescriptor) int {
	n := 0
	for _, s := range Inventory(m).Slots() {
		if !s.Empty() && d.Matches(s) {
			n += s.Count()
		}
	}
	return n
}

// home is minecraft:home (BDS HomeComponent): the home position is set to
// where the mob first appears; every 100 ticks a home whose block is not in
// home_block_list is forgotten.
type home struct {
	radius  int
	blocks  []string
	typ     string
	counter int
}

func newHome(_ string, raw json.RawMessage) (mob.Component, error) {
	j := struct {
		Radius *int            `json:"restriction_radius"`
		Blocks json.RawMessage `json:"home_block_list"`
		Type   string          `json:"restriction_type"`
	}{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &j); err != nil {
			return nil, fmt.Errorf("home: %w", err)
		}
	}
	h := &home{radius: -1, typ: j.Type}
	if j.Radius != nil {
		h.radius = *j.Radius
	}
	if len(j.Blocks) > 0 {
		var one string
		if json.Unmarshal(j.Blocks, &one) == nil {
			h.blocks = []string{one}
		} else {
			_ = json.Unmarshal(j.Blocks, &h.blocks)
		}
		for i, b := range h.blocks {
			h.blocks[i] = core.ParseItemDescriptor(b).Name
		}
	}
	return h, nil
}

func (h *home) Attach(m *mob.Mob) {
	if _, ok := m.Home(); !ok {
		m.SetHome(cube.PosFromVec3(m.Position()))
	}
}
func (h *home) Detach(*mob.Mob) {}

func (h *home) Tick(m *mob.Mob) {
	if h.counter++; h.counter < 100 {
		return
	}
	h.counter = 0
	p, ok := m.Home()
	if !ok || len(h.blocks) == 0 {
		return
	}
	b, loaded := m.Tx().BlockLoaded(p)
	if !loaded {
		return
	}
	name, _ := b.EncodeBlock()
	for _, n := range h.blocks {
		if n == name {
			return
		}
	}
	m.ClearHome()
}

// HomeRestriction returns the home position and radius of m's
// minecraft:home (radius -1: none).
func HomeRestriction(m *mob.Mob) (cube.Pos, int, bool) {
	h, ok := mob.ComponentOf[*home](m)
	if !ok {
		return cube.Pos{}, 0, false
	}
	p, ok := m.Home()
	return p, h.radius, ok
}

// WithinHomeRestriction is HomeComponent::isWithinRestriction.
func WithinHomeRestriction(m *mob.Mob, pos mgl64.Vec3) bool {
	p, r, ok := HomeRestriction(m)
	if !ok || r < 0 {
		return false
	}
	return p.Vec3Centre().Sub(pos).LenSqr() < float64(r*r)
}

// marker is a component with no behaviour of its own whose presence other
// code checks with HasComponent (is_illager_captain, can_join_raid,
// raid_trigger, hide, annotation.open_door's flag for nav).
type marker struct{}

func newMarker(string, json.RawMessage) (mob.Component, error) { return marker{}, nil }
func (marker) Attach(*mob.Mob)                                 {}
func (marker) Detach(*mob.Mob)                                 {}

// HideState is minecraft:hide (BDS HideComponent): whether the villager
// hides because of a raid or a bell.
type HideState struct {
	InRaid, ReactToBell bool
}

func newHide(string, json.RawMessage) (mob.Component, error) { return &HideState{}, nil }
func (*HideState) Attach(*mob.Mob)                           {}
func (*HideState) Detach(*mob.Mob)                           {}
func (h *HideState) EncodeNBT() map[string]any {
	return map[string]any{"IsInRaid": boolByte(h.InRaid), "ReactToBell": boolByte(h.ReactToBell)}
}
func (h *HideState) DecodeNBT(_ *mob.Mob, d map[string]any) {
	a, _ := d["IsInRaid"].(uint8)
	b, _ := d["ReactToBell"].(uint8)
	h.InRaid, h.ReactToBell = a != 0, b != 0
}

// openDoor is minecraft:annotation.open_door (BDS OpenDoorAnnotation
// System -> checkPath): wooden doors on the next nodes of the mob's path are
// opened when it is within 1.5 blocks and closed again once it is 2 blocks
// past them. Package nav already treats the mob as a door opener while the
// component is active.
type openDoor struct {
	opened map[cube.Pos]bool
}

func newOpenDoor(string, json.RawMessage) (mob.Component, error) {
	return &openDoor{opened: map[cube.Pos]bool{}}, nil
}
func (d *openDoor) Attach(*mob.Mob) {}
func (d *openDoor) Detach(m *mob.Mob) {
	for p := range d.opened {
		SetDoor(m.Tx(), p, false)
	}
}

func (d *openDoor) TickStage() mob.Stage { return mob.StageNavigation }

func (d *openDoor) Tick(m *mob.Mob) {
	pos := m.Position()
	if path := m.Navigation().Path(); path != nil && !m.Navigation().Done() {
		for i := path.Index(); i < min(path.Len(), path.Index()+2); i++ {
			n := path.Node(i)
			for _, p := range []cube.Pos{n, n.Side(cube.FaceUp)} {
				if !IsWoodDoor(m.Tx(), p) || d.opened[p] {
					continue
				}
				if p.Vec3Centre().Sub(pos).Len() < 1.5+0.5 {
					if SetDoor(m.Tx(), p, true) {
						d.opened[p] = true
					}
				}
			}
		}
	}
	for p := range d.opened {
		if c := p.Vec3Centre(); math.Hypot(c[0]-pos[0], c[2]-pos[2]) > 2 {
			SetDoor(m.Tx(), p, false)
			delete(d.opened, p)
		}
	}
}

// IsWoodDoor reports whether pos holds a wooden door.
func IsWoodDoor(tx *world.Tx, pos cube.Pos) bool {
	b, ok := tx.BlockLoaded(pos)
	if !ok {
		return false
	}
	_, door := b.(block.WoodDoor)
	return door
}

// SetDoor opens or closes the wooden door at pos (both halves) and plays
// the sound. It reports whether the door changed.
func SetDoor(tx *world.Tx, pos cube.Pos, open bool) bool {
	b, ok := tx.BlockLoaded(pos)
	if !ok {
		return false
	}
	d, ok := b.(block.WoodDoor)
	if !ok || d.Open == open {
		return false
	}
	d.Open = open
	tx.SetBlock(pos, d, nil)
	other := pos.Side(cube.FaceUp)
	if d.Top {
		other = pos.Side(cube.FaceDown)
	}
	if o, ok := tx.Block(other).(block.WoodDoor); ok {
		o.Open = open
		tx.SetBlock(other, o, nil)
	}
	if open {
		tx.PlaySound(pos.Vec3Centre(), sound.DoorOpen{Block: d})
	} else {
		tx.PlaySound(pos.Vec3Centre(), sound.DoorClose{Block: d})
	}
	return true
}

// managedTrader is minecraft:managed_wandering_trader: a trader the
// scheduler does not manage any more (another one spawned) is removed.
type managedTrader struct{}

func newManagedTrader(string, json.RawMessage) (mob.Component, error) { return &managedTrader{}, nil }
func (*managedTrader) Attach(*mob.Mob)                                {}
func (*managedTrader) Detach(*mob.Mob)                                {}
func (*managedTrader) Tick(m *mob.Mob) {
	mgr := ManagerOf(m.Tx())
	if !mgr.trader.manages(m.H()) {
		_ = m.Close()
	}
}

// growsCrop is minecraft:grows_crop (bees, BDS GrowCropSystem): a mob in a
// crop (or above one) grows it by one stage with chance per tick, using a
// charge each time, never the same crop twice in a row.
type growsCrop struct {
	charges int
	chance  float64
	last    cube.Pos
	hasLast bool
}

func newGrowsCrop(_ string, raw json.RawMessage) (mob.Component, error) {
	j := struct {
		Charges *int     `json:"charges"`
		Chance  *float64 `json:"chance"`
	}{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &j); err != nil {
			return nil, fmt.Errorf("grows_crop: %w", err)
		}
	}
	g := &growsCrop{charges: 10}
	if j.Charges != nil {
		g.charges = *j.Charges
	}
	if j.Chance != nil {
		g.chance = *j.Chance
	}
	return g, nil
}

func (*growsCrop) Attach(*mob.Mob) {}
func (*growsCrop) Detach(*mob.Mob) {}

func (g *growsCrop) Tick(m *mob.Mob) {
	if g.charges < 1 {
		return
	}
	tx := m.Tx()
	pos := cube.PosFromVec3(m.Position())
	cand := pos
	if !g.cropAt(tx, pos) {
		if cand = pos.Side(cube.FaceDown); !g.cropAt(tx, cand) {
			return
		}
	}
	if m.Rand().Float64() > g.chance {
		return
	}
	if GrowCrop(tx, cand) {
		g.charges--
		g.last, g.hasLast = cand, true
	}
}

func (g *growsCrop) cropAt(tx *world.Tx, pos cube.Pos) bool {
	if g.hasLast && pos == g.last {
		return false
	}
	b, ok := tx.BlockLoaded(pos)
	if !ok {
		return false
	}
	_, crop := b.(block.Crop)
	return crop
}

func (g *growsCrop) EncodeNBT() map[string]any {
	return map[string]any{"grows_crop.charges": int32(g.charges)}
}
func (g *growsCrop) DecodeNBT(_ *mob.Mob, d map[string]any) {
	if v, ok := d["grows_crop.charges"].(int32); ok {
		g.charges = int(v)
	}
}

// GrowCrop grows the crop at pos by one stage (max 7) and shows bone meal
// particles. It reports whether it grew.
func GrowCrop(tx *world.Tx, pos cube.Pos) bool {
	b := tx.Block(pos)
	c, ok := b.(block.Crop)
	if !ok || c.GrowthStage() >= 7 {
		return false
	}
	nb, ok := withGrowth(b, c.GrowthStage()+1)
	if !ok {
		return false
	}
	tx.SetBlock(pos, nb, nil)
	return true
}

// withGrowth returns crop b at another growth stage.
func withGrowth(b world.Block, g int) (world.Block, bool) {
	name, props := b.EncodeBlock()
	np := map[string]any{}
	for k, v := range props {
		np[k] = v
	}
	if _, ok := np["growth"]; !ok {
		return nil, false
	}
	np["growth"] = int32(g)
	nb, ok := world.BlockByName(name, np)
	return nb, ok
}

// insomnia is minecraft:insomnia (players, BDS InsomniaSystem): the time
// since the player last slept counts up and resets while sleeping or on
// respawn. Players do not run definitions in this server, so the manager
// counts it for every player; InsomniaTicks reads it (phantom spawning).
type insomnia struct{}

func newInsomnia(string, json.RawMessage) (mob.Component, error) { return insomnia{}, nil }
func (insomnia) Attach(*mob.Mob)                                 {}
func (insomnia) Detach(*mob.Mob)                                 {}

// InsomniaTicks returns how many ticks ago p last rested (slept or
// respawned), as BDS InsomniaComponent::timeSinceRest.
func InsomniaTicks(tx *world.Tx, p *player.Player) int64 {
	mgr := ManagerOf(tx)
	t, ok := mgr.rest[p.H()]
	if !ok {
		return 1
	}
	return mgr.now - t + 1
}

// DaysUntilInsomnia is the BDS default (3 days = 72000 ticks).
const DaysUntilInsomnia = 3

// effect helpers.
func hasEffect(l interface{ Effects() []effect.Effect }, t effect.Type) (effect.Effect, bool) {
	for _, e := range l.Effects() {
		if e.Type() == t {
			return e, true
		}
	}
	return effect.Effect{}, false
}

var _ = time.Second
