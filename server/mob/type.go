//go:build crossfly_full

package mob

import (
	"fmt"
	"slices"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Type is the world.EntityType of one definition.
type Type struct {
	id  string
	def Definition
	// removeInPeaceful: the mob is removed while the difficulty is peaceful
	// (removeInPeacefulTypes).
	removeInPeaceful bool
}

var (
	typesMu sync.RWMutex
	types   = map[string]*Type{}
	order   []string
)

// RegisterType registers an entity type for a definition. A later
// registration of the same identifier replaces the earlier one (a custom
// pack overriding a vanilla mob). Add Types() to the server's entity
// registry.
func RegisterType(def Definition) *Type {
	t := &Type{id: def.Identifier(), def: def, removeInPeaceful: removeInPeacefulTypes[def.Identifier()]}
	typesMu.Lock()
	defer typesMu.Unlock()
	if _, ok := types[t.id]; !ok {
		order = append(order, t.id)
	}
	types[t.id] = t
	return t
}

// TypeFor returns the registered type of an identifier.
func TypeFor(id string) (*Type, bool) {
	typesMu.RLock()
	defer typesMu.RUnlock()
	t, ok := types[id]
	return t, ok
}

// extraTypes are entity types that come with the mobs but are no mobs (the
// leash knot of package mob/component/core), added with RegisterEntityType.
var extraTypes []world.EntityType

// RegisterEntityType adds an entity type that is not a mob but comes with
// them to Types, after the mob types (only once a mob type is registered, so
// the mobs off still register nothing). Call it from init only.
func RegisterEntityType(t world.EntityType) {
	typesMu.Lock()
	defer typesMu.Unlock()
	extraTypes = append(extraTypes, t)
}

// Types returns every registered mob type, in registration order, followed
// by the types added with RegisterEntityType.
func Types() []world.EntityType {
	typesMu.RLock()
	defer typesMu.RUnlock()
	out := make([]world.EntityType, 0, len(order)+len(extraTypes))
	for _, id := range order {
		out = append(out, types[id])
	}
	if len(order) > 0 {
		out = append(out, extraTypes...)
	}
	return out
}

// Identifiers returns the registered identifiers, sorted.
func Identifiers() []string {
	typesMu.RLock()
	defer typesMu.RUnlock()
	return slices.Sorted(slices.Values(order))
}

// Definition returns the type's definition.
func (t *Type) Definition() Definition { return t.def }

// Open ...
func (t *Type) Open(tx *world.Tx, h *world.EntityHandle, data *world.EntityData) world.Entity {
	s, ok := data.Data.(*state)
	if !ok {
		s = newState(t)
		data.Data = s
	}
	s.data = data
	// A mob is opened for every lookup (each entity a target search passes
	// over): within one transaction the same *Mob is handed out again.
	// Transactions are never reused, so a *Mob of an earlier one never
	// matches.
	if c := s.cur; c != nil && c.data == data && c.Ent.Tx() == tx && c.Ent.H() == h {
		return c
	}
	m := &Mob{Ent: entity.Open(tx, h, data), data: data, s: s}
	s.cur = m
	return m
}

// EncodeEntity ...
func (t *Type) EncodeEntity() string { return t.id }

// BBox is the collision box (minecraft:collision_box) times the scale.
func (t *Type) BBox(e world.Entity) cube.BBox {
	w, h := 0.6, 1.8
	if m, ok := Of(e); ok {
		w, h = m.s.meta.Width*m.s.meta.Scale, m.s.meta.Height*m.s.meta.Scale
	}
	return cube.Box(-w/2, 0, -w/2, w/2, h, w/2)
}

// DecodeNBT ...
func (t *Type) DecodeNBT(m map[string]any, data *world.EntityData) {
	s := newState(t)
	s.pendingNBT = m
	data.Data = s
}

// EncodeNBT ...
func (t *Type) EncodeNBT(data *world.EntityData) map[string]any {
	s, ok := data.Data.(*state)
	if !ok {
		return nil
	}
	if !s.initialised {
		// Never ticked since it loaded: save what was loaded.
		return s.pendingNBT
	}
	return s.encodeNBT()
}

// SpawnOptions are the options of Spawn.
type SpawnOptions struct {
	Rotation cube.Rotation
	Velocity mgl64.Vec3
	NameTag  string
	// Event is fired once the mob is in the world, before its first tick.
	// Empty means EventSpawned; set NoEvent to fire none.
	Event   string
	NoEvent bool
	// Configure, if set, runs after the components attached and before the
	// event fires.
	Configure func(m *Mob)
}

// Built-in events the engine fires (BDS fires them itself; definitions only
// respond to them).
const (
	EventSpawned        = "minecraft:entity_spawned"
	EventBorn           = "minecraft:entity_born"
	EventTransformed    = "minecraft:entity_transformed"
	EventGrowUp         = "minecraft:ageable_grow_up"
	EventOnTame         = "minecraft:on_tame"
	EventOnPrime        = "minecraft:on_prime"
	EventStartExploding = "minecraft:start_exploding"
)

// Spawn adds a new mob of the identifier at pos, initialises it and fires
// its spawn event (entity_spawned unless opts say otherwise).
func Spawn(tx *world.Tx, id string, pos mgl64.Vec3, opts SpawnOptions) (*Mob, error) {
	t, ok := TypeFor(id)
	if !ok {
		return nil, fmt.Errorf("mob: no mob type %q", id)
	}
	h := world.EntitySpawnOpts{Position: pos, Rotation: opts.Rotation, Velocity: opts.Velocity, NameTag: opts.NameTag}.New(t, spawnConfig{t: t})
	m, ok := Of(tx.AddEntity(h))
	if !ok {
		return nil, fmt.Errorf("mob: %v did not open as a mob", id)
	}
	m.ensureInit()
	if opts.Configure != nil {
		opts.Configure(m)
	}
	if !opts.NoEvent {
		ev := opts.Event
		if ev == "" {
			ev = EventSpawned
		}
		m.Fire(ev, nil)
	}
	return m, nil
}

// NewHandle creates a handle of a new mob for callers that add it
// themselves (tx.AddEntity). The mob initialises on its first tick and then
// fires event (none if empty).
func NewHandle(id string, opts world.EntitySpawnOpts, event string) (*world.EntityHandle, error) {
	t, ok := TypeFor(id)
	if !ok {
		return nil, fmt.Errorf("mob: no mob type %q", id)
	}
	return opts.New(t, spawnConfig{t: t, event: event}), nil
}

type spawnConfig struct {
	t     *Type
	event string
}

func (c spawnConfig) Apply(data *world.EntityData) {
	s := newState(c.t)
	s.spawnEvent = c.event
	data.Data = s
}

// EntityProperties lists the definition's description.properties for the
// clients (server/session sends the client-synced ones at join).
func (t *Type) EntityProperties() []world.EntityProperty {
	decls := t.def.Description().Properties
	if len(decls) == 0 {
		return nil
	}
	out := make([]world.EntityProperty, 0, len(decls))
	for _, d := range decls {
		p := world.EntityProperty{Name: d.Name, Min: d.Range[0], Max: d.Range[1], ClientSync: d.ClientSync}
		switch d.Type {
		case "int":
			p.Type = world.EntityPropertyInt
			if f, ok := d.Default.(float64); ok {
				p.Default = int(f)
			}
		case "float":
			p.Type = world.EntityPropertyFloat
			if f, ok := d.Default.(float64); ok {
				p.Default = f
			}
		case "bool":
			p.Type = world.EntityPropertyBool
			if b, ok := d.Default.(bool); ok {
				p.Default = b
			}
		case "enum":
			p.Type = world.EntityPropertyEnum
			p.Enum = d.Values
			if s, ok := d.Default.(string); ok && slices.Contains(d.Values, s) {
				p.Default = s
			}
		}
		out = append(out, p)
	}
	return out
}
