//go:build crossfly_full

package def

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/def/molang"
	"github.com/df-mc/dragonfly/server/mob/event"
	"github.com/df-mc/dragonfly/server/mob/filter"
	"github.com/df-mc/dragonfly/server/world"
)

func init() {
	mob.SetDefinitionParser(ParseMob)
}

// ParseMob is the mob.DefinitionParser. Partial errors (one bad event or
// property) are logged and the rest of the definition is used.
func ParseMob(raw []byte) (mob.Definition, error) {
	d, err := Parse(raw)
	if d == nil {
		return nil, err
	}
	if err != nil {
		slog.Default().Warn("mob def: definition loaded with errors", "entity", d.ID, "err", err)
	}
	return MobDefinition{d}, nil
}

// MobDefinition adapts a *Definition to mob.Definition.
type MobDefinition struct{ *Definition }

// Identifier ...
func (d MobDefinition) Identifier() string { return d.ID }

// ComponentKeys ...
func (d MobDefinition) ComponentKeys() []string { return d.ComponentNames() }

// Description ...
func (d MobDefinition) Description() mob.Description {
	desc := mob.Description{
		Identifier: d.ID, RuntimeIdentifier: d.RuntimeID, SpawnCategory: d.SpawnCategory,
		Spawnable: d.Spawnable, Summonable: d.Summonable, Experimental: d.Experimental,
	}
	for _, p := range d.Properties {
		desc.Properties = append(desc.Properties, decl(p))
	}
	return desc
}

func decl(p *PropertyDef) mob.PropertyDecl {
	return mob.PropertyDecl{
		Name: p.Name, Type: p.Type.String(), Values: p.Values,
		Range: [2]float64{p.Min, p.Max}, Default: p.RawDefault, ClientSync: p.ClientSync,
	}
}

// NewInstance ...
func (d MobDefinition) NewInstance() mob.Instance {
	return &MobInstance{Instance: d.Definition.NewInstance()}
}

// MobInstance adapts an *Instance to mob.Instance.
type MobInstance struct {
	*Instance
	changed, propsChanged bool
	reinit                []string
}

// TakeReinit returns (and forgets) the components that stay active but were
// re-supplied by a newer stack entry since the last call (Diff.Reinit): the
// mob rebuilds them, as BDS re-initialises the components of a re-added
// group (mob.InstanceReinit).
func (i *MobInstance) TakeReinit() []string {
	r := i.reinit
	i.reinit = nil
	return r
}

// Components ...
func (i *MobInstance) Components() []mob.RawComponent {
	cs := i.Instance.Components()
	out := make([]mob.RawComponent, len(cs))
	for k, c := range cs {
		out[k] = mob.RawComponent{Key: c.Name(), Raw: c.Raw()}
	}
	return out
}

// ComponentRaw returns the JSON of an active component, including group
// changes of the event being run (filter's live view).
func (i *MobInstance) ComponentRaw(name string) (json.RawMessage, bool) {
	c, ok := i.Component(name)
	if !ok {
		return nil, false
	}
	return c.Raw(), true
}

// Property returns int, float64, bool or string.
func (i *MobInstance) Property(name string) (any, bool) { return i.PropertyValue(name) }

// SetProperty ...
func (i *MobInstance) SetProperty(name string, v any) error { return i.SetPropertyNow(name, v) }

// Properties ...
func (i *MobInstance) Properties() []mob.PropertyValue {
	out := make([]mob.PropertyValue, len(i.def.Properties))
	for k, p := range i.def.Properties {
		out[k] = mob.PropertyValue{Decl: decl(p), Value: i.props[p.Index]}
	}
	return out
}

// EncodeNBT ...
func (i *MobInstance) EncodeNBT() map[string]any { return i.Save() }

// DecodeNBT ...
func (i *MobInstance) DecodeNBT(m map[string]any) {
	if err := i.Load(m); err != nil {
		slog.Default().Warn("mob def: loading saved definition state", "err", err)
	}
}

// Fire runs an event on m (mob.Instance). Other mobs reached by triggers
// re-read their components after the chain.
func (i *MobInstance) Fire(m *mob.Mob, name string, ctx *mob.FilterContext) bool {
	run := &fireRun{}
	self := run.entity(m, i)
	fctx := filter.FromMob(ctx)
	fctx.Self = self
	fctx.Other = run.wrap(fctx.Other)
	fctx.Target = run.wrap(fctx.Target)
	fctx.Baby = run.wrap(fctx.Baby)
	fctx.Parent = run.wrap(fctx.Parent)
	fctx.Player = run.wrap(fctx.Player)
	fctx.Damager = run.wrap(fctx.Damager)
	event.Fire(self, name, fctx)
	run.finish(m)
	changed := i.changed
	i.changed = false
	return changed
}

// fireRun tracks the mobs one Fire touched.
type fireRun struct {
	ents map[*world.EntityHandle]*MobEntity
}

func (r *fireRun) entity(m *mob.Mob, inst *MobInstance) *MobEntity {
	if r.ents == nil {
		r.ents = map[*world.EntityHandle]*MobEntity{}
	}
	if e, ok := r.ents[m.H()]; ok {
		return e
	}
	e := &MobEntity{EntityActor: filter.EntityActor{Tx: m.Tx(), E: m}, M: m, Inst: inst, run: r}
	inst.Instance.Fallback = mobQueries{m: m, tx: m.Tx()}
	inst.Instance.OnChange = func(d Diff) {
		inst.changed = true
		for _, c := range d.Reinit {
			inst.reinit = append(inst.reinit, c.Name())
		}
	}
	inst.Instance.OnPropertyChange = func(*PropertyDef, any) { inst.propsChanged = true }
	r.ents[m.H()] = e
	return e
}

// wrap turns a filter actor of a data-driven mob into its MobEntity, so
// triggers can run events on it.
func (r *fireRun) wrap(a filter.Actor) filter.Actor {
	ea, ok := a.(filter.EntityActor)
	if !ok {
		return a
	}
	m, ok := ea.E.(*mob.Mob)
	if !ok || !m.Initialised() {
		return a
	}
	inst, ok := m.Instance().(*MobInstance)
	if !ok {
		return a
	}
	return r.entity(m, inst)
}

func (r *fireRun) finish(self *mob.Mob) {
	for h, e := range r.ents {
		if e.Inst.propsChanged {
			e.Inst.propsChanged = false
			e.M.RefreshState()
		}
		if h != self.H() && e.Inst.changed {
			e.Inst.changed = false
			e.M.ReapplyComponents()
		}
	}
}

// MobEntity is a mob as seen by the event executor: the filter actor plus
// its definition state and the event action hooks.
type MobEntity struct {
	filter.EntityActor
	M    *mob.Mob
	Inst *MobInstance
	run  *fireRun
}

// EventState ...
func (e *MobEntity) EventState() event.State { return e.Inst.Instance }

// NameTag ...
func (e *MobEntity) NameTag() string { return e.M.NameTag() }

// HasComponent is the live component set (group changes of the running
// event included).
func (e *MobEntity) HasComponent(name string) bool { return e.Inst.HasComponent(name) }

// HandleAction implements the mob-side event responses.
func (e *MobEntity) HandleAction(a event.Action, ctx *filter.Context) bool {
	m := e.M
	switch a := a.(type) {
	case event.ResetTarget:
		m.ResetTarget()
	case event.StopMovement:
		if a.Horizontal {
			m.StopMovement()
		}
		if a.Vertical {
			v := m.Vel()
			v[1] = 0
			m.SetVelocity(v)
		}
	case event.SetHomePosition:
		m.SetHome(cube.PosFromVec3(m.Pos()))
	case event.DropItem:
		e.dropItem(a.Slot)
	default:
		// play_sound, emit_vibration, emit_particle, queue_command,
		// execute_event_on_home_block: State.HandleAction (log / no-op).
		return false
	}
	return true
}

func (e *MobEntity) dropItem(slot string) {
	m := e.M
	main, off := m.HeldItems()
	var st item.Stack
	switch slot {
	case "slot.weapon.mainhand", "":
		st, main = main, item.Stack{}
	case "slot.weapon.offhand":
		st, off = off, item.Stack{}
	default:
		return
	}
	if st.Empty() || m.Tx() == nil {
		return
	}
	m.SetHeldItems(main, off)
	opts := world.EntitySpawnOpts{Position: m.Pos().Add(m.Vel()), Velocity: m.Vel()}
	m.Tx().AddEntity(entity.NewItemPickupDelay(opts, st, time.Second))
}

// mobQueries answers the Molang queries vanilla entity files use in events
// and properties, for a mob.
type mobQueries struct {
	m  *mob.Mob
	tx *world.Tx
}

// Query ...
func (q mobQueries) Query(name string, args []molang.Value) (molang.Value, bool) {
	m := q.m
	a := filter.EntityActor{Tx: q.tx, E: m}
	switch name {
	case "is_baby":
		return molang.Bool(a.HasComponent("minecraft:is_baby")), true
	case "variant":
		v, _ := a.Number(filter.NumberVariant)
		return molang.Num(v), true
	case "mark_variant":
		v, _ := a.Number(filter.NumberMarkVariant)
		return molang.Num(v), true
	case "skin_id":
		v, _ := a.Number(filter.NumberSkinID)
		return molang.Num(v), true
	case "is_on_ground":
		return molang.Bool(m.OnGround()), true
	case "is_in_water":
		return molang.Bool(m.Physics().InWater), true
	case "is_in_lava":
		return molang.Bool(m.Physics().InLava), true
	case "head_is_in_water", "is_underwater":
		return molang.Bool(m.Physics().Underwater), true
	case "is_on_fire":
		return molang.Bool(m.OnFireDuration() > 0), true
	case "is_using_item":
		return molang.Bool(m.UsingItem()), true
	case "is_sneaking":
		return molang.Bool(m.Sneaking()), true
	case "is_sitting":
		return molang.Bool(m.Sitting()), true
	case "is_tamed":
		return molang.Bool(m.Tamed()), true
	case "is_angry":
		return molang.Bool(m.Angry()), true
	case "health":
		return molang.Num(m.Health()), true
	case "max_health":
		return molang.Num(m.MaxHealth()), true
	case "has_target":
		_, ok := m.Target()
		return molang.Bool(ok), true
	case "last_hit_by_player":
		// Experience drops when a player hit the mob in the last 5 seconds.
		t := m.LastHurtByPlayerTick()
		return molang.Bool(t > 0 && m.Ticks()-t <= 100), true
	case "equipment_count":
		return molang.Num(float64(len(a.Equipment(filter.DomainAny)))), true
	case "has_any_family":
		for _, f := range args {
			if m.HasFamily(f.Str) {
				return molang.Num(1), true
			}
		}
		return molang.Num(0), true
	case "anger_level":
		// minecraft:anger_level (package mob/component/boss).
		if a, ok := mob.ComponentOf[interface{ AngerLevel() int }](m); ok {
			return molang.Num(float64(a.AngerLevel())), true
		}
		return molang.Num(0), true
	case "player_level":
		return molang.Num(0), true
	}
	return molang.Value{}, false
}
