//go:build crossfly_full

package javamob

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/go-gl/mathgl/mgl64"
)

// componentKey is the one component a Java mob's definition has: its state.
const componentKey = "crossfly:java_mob"

func init() {
	mob.RegisterComponent(componentKey, func(_ string, raw json.RawMessage) (mob.Component, error) {
		var v struct{ ID string }
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		t, ok := TypeOf(v.ID)
		if !ok {
			return nil, fmt.Errorf("javamob: no type %q", v.ID)
		}
		return newState(t), nil
	})
}

// definition is the mob.Definition of a Java type: the description of the Bedrock definition of the same mob (so
// its actor properties still reach Bedrock clients) and one component, the Java state.
type definition struct {
	desc  mob.Description
	comps []mob.RawComponent
}

func (d *definition) Identifier() string           { return d.desc.Identifier }
func (d *definition) Description() mob.Description { return d.desc }
func (d *definition) ComponentKeys() []string {
	keys := make([]string, len(d.comps))
	for i, c := range d.comps {
		keys[i] = c.Key
	}
	return keys
}
func (d *definition) NewInstance() mob.Instance {
	inst := &instance{d: d, props: map[string]any{}}
	for _, p := range d.desc.Properties {
		if p.Default != nil {
			inst.props[p.Name] = p.Default
		}
	}
	return inst
}

// instance is the live definition of a Java mob: no component groups, the Bedrock properties at their defaults.
// The spawn events the engine fires reach finalizeSpawn through Fire.
type instance struct {
	d     *definition
	props map[string]any
}

func (i *instance) Components() []mob.RawComponent { return i.d.comps }

func (i *instance) Fire(bm *mob.Mob, event string, _ *mob.FilterContext) bool {
	m, ok := wrap(bm)
	if !ok {
		return false
	}
	switch event {
	case mob.EventSpawned:
		// /summon, spawn eggs and mob.Spawn callers: finalize as a spawn item use unless Spawn set a reason.
		m.finalizeSpawn(m.s.reason)
	case mob.EventBorn:
		m.finalizeSpawn(SpawnBreeding)
	case mob.EventTransformed:
		m.finalizeSpawn(SpawnConversion)
	}
	return false
}

func (i *instance) Property(name string) (any, bool) { v, ok := i.props[name]; return v, ok }
func (i *instance) SetProperty(name string, v any) error {
	i.props[name] = v
	return nil
}
func (i *instance) Properties() []mob.PropertyValue {
	out := make([]mob.PropertyValue, 0, len(i.d.desc.Properties))
	for _, p := range i.d.desc.Properties {
		if v, ok := i.props[p.Name]; ok {
			out = append(out, mob.PropertyValue{Decl: p, Value: v})
		}
	}
	return out
}
func (i *instance) EncodeNBT() map[string]any { return nil }
func (i *instance) DecodeNBT(map[string]any)  {}

var (
	installOnce sync.Once
	installed   bool
)

// Install turns Java mob mode on: every registered Java type replaces the mob type of the same identifier in
// package mob (a Bedrock-engine mob type registered before keeps its description for the actor properties), and
// item entities move as in Java Edition. Call it once, after the Bedrock mobs are registered (vanilla.Register)
// and before the server is configured. Without Install nothing of this package runs.
func Install() {
	installOnce.Do(func() {
		installed = true
		entity.SetJavaMobs(true)
		for _, t := range Types() {
			desc := mob.Description{Identifier: t.ID}
			if old, ok := mob.TypeFor(t.ID); ok {
				desc = old.Definition().Description()
			}
			raw, _ := json.Marshal(map[string]string{"id": t.ID})
			comps := []mob.RawComponent{{Key: componentKey, Raw: raw}}
			for _, k := range t.Components {
				comps = append(comps, mob.RawComponent{Key: k, Raw: raw})
			}
			mob.RegisterTypeWithHooks(&definition{desc: desc, comps: comps}, mob.TypeHooks{Tick: tickMob, KnockBack: knockBackHook})
		}
	})
}

// Installed reports whether Java mob mode is on.
func Installed() bool { return installed }

func tickMob(bm *mob.Mob) {
	if m, ok := wrap(bm); ok {
		m.tick()
	}
}

func knockBackHook(bm *mob.Mob, src mgl64.Vec3, force, height float64) {
	if m, ok := wrap(bm); ok {
		m.knockBackFrom(src, force, height)
	}
}
