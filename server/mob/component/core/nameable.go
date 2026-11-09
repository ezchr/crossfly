//go:build crossfly_full

package core

import (
	"encoding/json"
	"fmt"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/go-gl/mathgl/mgl64"
)

func init() {
	// The core reads the key for NameTaggable; this component handles the
	// name tag itself.
	mob.RegisterComponent("minecraft:nameable", newNameable)
}

// nameable is minecraft:nameable (BDS NameableComponent/NameableDefinition):
// with allow_name_tag_renaming (default true) a player's named name tag
// renames the mob (NameableComponent::getInteraction). nameEntity then makes
// the mob persistent and fires the on_named trigger of the first
// name_actions entry one of whose name_filter names equals the new name
// exactly, else default_trigger (the vindicator's "Johnny").
type nameable struct {
	allowRenaming bool
	actions       []nameAction
	defaultTrig   Trigger
}

type nameAction struct {
	names   []string
	onNamed Trigger
}

func newNameable(key string, raw json.RawMessage) (mob.Component, error) {
	var j struct {
		AllowRenaming *bool   `json:"allow_name_tag_renaming"`
		Default       Trigger `json:"default_trigger"`
		Actions       []struct {
			NameFilter json.RawMessage `json:"name_filter"`
			OnNamed    Trigger         `json:"on_named"`
		} `json:"name_actions"`
	}
	if err := decode(raw, &j); err != nil {
		return nil, fmt.Errorf("%v: %w", key, err)
	}
	c := &nameable{allowRenaming: j.AllowRenaming == nil || *j.AllowRenaming, defaultTrig: j.Default}
	for _, a := range j.Actions {
		names, err := stringList(a.NameFilter)
		if err != nil {
			return nil, fmt.Errorf("%v: name_filter: %w", key, err)
		}
		c.actions = append(c.actions, nameAction{names: names, onNamed: a.OnNamed})
	}
	return c, nil
}

func (*nameable) Attach(*mob.Mob) {}
func (*nameable) Detach(*mob.Mob) {}

// Interact is NameableComponent::getInteraction: a name tag with a custom
// name (other than the mob's name) names the mob and is used up.
func (c *nameable) Interact(m *mob.Mob, user item.User, held item.Stack, _ mgl64.Vec3) (item.Stack, bool) {
	if !c.allowRenaming || held.Empty() {
		return held, false
	}
	if _, ok := held.Item().(item.NameTag); !ok {
		return held, false
	}
	name := held.CustomName()
	if name == "" || name == m.NameTag() {
		return held, false
	}
	c.Name(m, name)
	return ConsumeOne(user, held), true
}

// Name is NameableComponent::nameEntity: m gets the name, shown when looked
// at, becomes persistent and runs the matching name action.
func (c *nameable) Name(m *mob.Mob, name string) {
	m.SetNameTag(name)
	m.SetAlwaysShowNameTag(false)
	m.SetPersistent(true)
	for _, a := range c.actions {
		for _, n := range a.names {
			if n == name {
				a.onNamed.Fire(m, nil)
				return
			}
		}
	}
	c.defaultTrig.Fire(m, nil)
}

// NameMob names m as a name tag would (NameableComponent::nameEntity), for
// commands and spawn eggs with a custom name. Without minecraft:nameable it
// only sets the name tag.
func NameMob(m *mob.Mob, name string) {
	if c, ok := mob.ComponentOf[*nameable](m); ok {
		c.Name(m, name)
		return
	}
	m.SetNameTag(name)
}
