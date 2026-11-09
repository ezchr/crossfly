//go:build crossfly_full

package mob

import (
	"bytes"
	"encoding/binary"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// state is everything a mob keeps between ticks. It is the entity.Behaviour
// stored in world.EntityData.Data.
type state struct {
	entity.BaseBehaviour

	t    *Type
	data *world.EntityData

	initialised bool
	inst        Instance
	pendingNBT  map[string]any
	spawnEvent  string
	removed     bool

	rnd   *rand.Rand
	ticks int64

	health  *entity.HealthManager
	effects *entity.EffectManager
	attrs   *Attributes

	families []string
	meta     Meta
	physics  Physics
	input    Input
	mc       *entity.MovementComputer

	headYaw, bodyYaw float64

	mainHand, offHand item.Stack
	bodyArmour        item.Stack
	armour            *inventory.Armour

	target             *world.EntityHandle
	lastHurtBy         *world.EntityHandle
	lastHurtTick       int64
	lastPlayerHurtTick int64
	lastHurtMob        *world.EntityHandle

	nav  Navigation
	move MoveControl
	look LookControl
	jump JumpControl
	body BodyControl

	goals   *GoalSelector
	comps   []attachedComponent
	tickers [stageCount][]Ticker
	frozen  []FrozenTicker
	// compOf caches ComponentOf by type (nil: none) until comps change.
	compOf []compOfEntry

	immune     int
	lastDamage float64
	dying      int

	home       *cube.Pos
	persistent bool

	// noAction is BDS Mob::mNoActionTime (inactivity timer, ticks).
	noAction int
	// ignoreWebs: minecraft:block_movement_slowdown_immunity lists
	// minecraft:web, so the environment step skips cobwebs.
	ignoreWebs bool

	sent struct {
		pos, vel         mgl64.Vec3
		rot              cube.Rotation
		headYaw, bodyYaw float64
		onGround         bool
	}

	scratch map[any]any

	// appliedGen is the instance generation (InstanceGeneration) the
	// components were last applied for, if genKnown.
	appliedGen uint64
	genKnown   bool

	// cur is the *Mob last opened (Of, Tick, Armour). Callbacks stored
	// beyond one transaction (the armour inventory's) use it, never a *Mob
	// they captured, and skip the world when its transaction is over.
	cur *Mob
}

// newArmour makes the armour inventory; its change callback refreshes the
// viewers through the current mob.
func (s *state) newArmour() *inventory.Armour {
	return inventory.NewArmour(func(int, item.Stack, item.Stack) {
		if m := s.cur; m != nil {
			m.RefreshState()
			m.viewEquipment(world.Viewer.ViewEntityArmour)
		}
	})
}

func newState(t *Type) *state {
	s := &state{
		BaseBehaviour:      entity.NewBaseBehaviour(),
		t:                  t,
		health:             entity.NewHealthManager(10, 10),
		effects:            entity.NewEffectManager(),
		attrs:              newAttributes(),
		goals:              NewGoalSelector(),
		lastHurtTick:       -1 << 40,
		lastPlayerHurtTick: -1 << 40,
		meta:               Meta{Scale: 1, Width: 0.6, Height: 1.8},
	}
	s.resetControls()
	return s
}

func (s *state) resetControls() {
	s.nav, s.move, s.look, s.jump, s.body = nopNavigation{}, nopMove{}, nopLook{}, nopJump{}, nopBody{}
}

// ensureInit builds the mob's definition instance and attaches its
// components the first time it is needed (Spawn, or the first tick of a
// mob loaded from disk).
func (m *Mob) ensureInit() {
	s := m.s
	if s.initialised {
		return
	}
	s.initialised = true
	if s.rnd == nil {
		id := m.H().UUID()
		s.rnd = rand.New(rand.NewPCG(binary.LittleEndian.Uint64(id[8:]), binary.LittleEndian.Uint64(id[:8])^0x9e3779b97f4a7c15))
	}
	s.bodyYaw, s.headYaw = m.data.Rot[0], m.data.Rot[0]
	s.inst = s.t.def.NewInstance()
	s.cur = m
	if s.armour == nil {
		s.armour = s.newArmour()
	}
	nbt := s.pendingNBT
	s.pendingNBT = nil
	if nbt != nil {
		if d, ok := nbt["mob:definition"].(map[string]any); ok {
			s.inst.DecodeNBT(d)
		}
	}
	for _, h := range initHooks {
		h.Init(m)
	}
	m.applyComponents()
	if nbt != nil {
		m.decodeNBT(nbt)
	}
	if ev := s.spawnEvent; ev != "" {
		s.spawnEvent = ""
		m.Fire(ev, nil)
	}
}

// applyComponents brings the attached components in line with the
// instance's active set: components that went away or whose JSON changed
// are detached (last first), then new or changed ones are built and
// attached in definition order. Unchanged components keep their state.
func (m *Mob) applyComponents() {
	s := m.s
	active := s.inst.Components()
	next := make(map[string][]byte, len(active))
	for _, c := range active {
		next[c.Key] = c.Raw
	}
	var reinit map[string]bool
	if r, ok := s.inst.(InstanceReinit); ok {
		for _, k := range r.TakeReinit() {
			if reinit == nil {
				reinit = map[string]bool{}
			}
			reinit[k] = true
		}
	}
	kept := make(map[string]attachedComponent, len(s.comps))
	for i := len(s.comps) - 1; i >= 0; i-- {
		c := s.comps[i]
		if raw, ok := next[c.key]; ok && bytes.Equal(raw, c.raw) && !reinit[c.key] {
			kept[c.key] = c
			continue
		}
		m.detach(c)
	}
	comps := make([]attachedComponent, 0, len(active))
	for _, c := range active {
		if k, ok := kept[c.Key]; ok {
			comps = append(comps, k)
			continue
		}
		comps = append(comps, m.attach(c))
	}
	s.comps = comps
	clear(s.compOf)
	s.compOf = s.compOf[:0]
	s.rebuildTickers()
	if g, ok := s.inst.(InstanceGeneration); ok {
		s.appliedGen, s.genKnown = g.Generation(), true
	}
}

// ComponentsApplied reports whether the attached components are the
// instance's active set: no event changed it since they were attached. It
// is false if the instance cannot tell.
func (m *Mob) ComponentsApplied() bool {
	s := m.s
	if !s.genKnown {
		return false
	}
	g, ok := s.inst.(InstanceGeneration)
	return ok && g.Generation() == s.appliedGen
}

func (m *Mob) detach(c attachedComponent) {
	if c.goal {
		m.s.goals.Remove(c.key)
		return
	}
	if c.comp != nil {
		c.comp.Detach(m)
	}
}

func (m *Mob) attach(c RawComponent) attachedComponent {
	a := attachedComponent{key: c.Key, raw: c.Raw}
	if f, ok := goalFactories[c.Key]; ok {
		spec, err := ParseGoalSpec(c.Key, c.Raw)
		if err == nil {
			var g Goal
			if g, err = f(spec); err == nil && g != nil {
				m.s.goals.Add(c.Key, spec.Priority, g)
				a.goal = true
				return a
			}
		}
		if err != nil {
			recordFactoryError(m.Identifier(), c.Key, err)
		}
		return a
	}
	if f, ok := componentFactories[c.Key]; ok {
		comp, err := f(c.Key, c.Raw)
		if err != nil {
			recordFactoryError(m.Identifier(), c.Key, err)
			return a
		}
		if comp != nil {
			a.comp = comp
			comp.Attach(m)
		}
		return a
	}
	recordUnsupported(c.Key)
	return a
}

// rebuildTickers builds new ticker lists (never reusing the old backing
// arrays): a component change made while runStage ranges over a list (an
// event fired by a ticker) must not change the list being iterated.
func (s *state) rebuildTickers() {
	for i := range s.tickers {
		s.tickers[i] = nil
	}
	s.frozen = nil
	for _, c := range s.comps {
		if f, ok := c.comp.(FrozenTicker); ok {
			s.frozen = append(s.frozen, f)
		}
		t, ok := c.comp.(Ticker)
		if !ok {
			continue
		}
		st := StageSensors
		if staged, ok := t.(StagedTicker); ok {
			st = staged.TickStage()
		}
		if st < stageCount {
			s.tickers[st] = append(s.tickers[st], t)
		}
	}
}
