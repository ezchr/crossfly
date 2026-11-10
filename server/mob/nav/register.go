//go:build crossfly_full

package nav

import (
	"encoding/json"
	"fmt"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/physics"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// This file adapts the package to the mob framework: the minecraft:navigation.* components attach
// a *Navigation as the mob's mob.Navigation, minecraft:preferred_path feeds Traits.Preferred, and
// *mob.Mob is wrapped as an Agent with run-time Traits (TraitSource).

func init() {
	for _, k := range []string{
		"minecraft:navigation.walk", "minecraft:navigation.climb", "minecraft:navigation.generic",
		"minecraft:navigation.float", "minecraft:navigation.fly", "minecraft:navigation.hover",
		"minecraft:navigation.swim",
	} {
		mob.RegisterComponent(k, newNavComponent)
	}
	mob.RegisterComponent("minecraft:preferred_path", newPreferredPath)
}

// navComponent is one minecraft:navigation.* component. Attaching it makes its navigation the
// mob's; a mob has at most one navigation component active (BDS NavigationComponent).
type navComponent struct{ n *MobNavigation }

func newNavComponent(key string, raw json.RawMessage) (mob.Component, error) {
	n, ok, err := FromComponent(key, raw)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("nav: unknown navigation component %v", key)
	}
	return &navComponent{n: &MobNavigation{N: n}}, nil
}

func (c *navComponent) Attach(m *mob.Mob) { m.SetNavigation(c.n) }
func (c *navComponent) Detach(m *mob.Mob) {
	if m.Navigation() == mob.Navigation(c.n) {
		c.n.Stop(m)
		m.SetNavigation(nil)
	}
}

// preferredPath is minecraft:preferred_path, read by agent.PathTraits.
type preferredPath struct{ pp PreferredPath }

func newPreferredPath(_ string, raw json.RawMessage) (mob.Component, error) {
	pp, err := ParsePreferredPath(raw)
	if err != nil {
		return nil, err
	}
	return &preferredPath{pp: pp}, nil
}

func (*preferredPath) Attach(*mob.Mob) {}
func (*preferredPath) Detach(*mob.Mob) {}

// MobNavigation is a *Navigation driven by a *mob.Mob: it implements mob.Navigation.
type MobNavigation struct{ N *Navigation }

var _ mob.Navigation = (*MobNavigation)(nil)

// NavigationOf returns the nav package navigation of m, if it has one.
func NavigationOf(m *mob.Mob) (*Navigation, bool) {
	if n, ok := m.Navigation().(*MobNavigation); ok {
		return n.N, true
	}
	return nil, false
}

func (n *MobNavigation) Tick(m *mob.Mob) { n.N.Tick(m.Tx(), agent{m}) }
func (n *MobNavigation) CreatePath(m *mob.Mob, pos mgl64.Vec3) mob.Path {
	return wrapPath(n.N.CreatePath(m.Tx(), agent{m}, pos))
}
func (n *MobNavigation) CreatePathToEntity(m *mob.Mob, e world.Entity) mob.Path {
	return wrapPath(n.N.CreatePathToEntity(m.Tx(), agent{m}, e))
}
func (n *MobNavigation) MoveTo(m *mob.Mob, pos mgl64.Vec3, speed float64) bool {
	return n.N.MoveTo(m.Tx(), agent{m}, pos, speed)
}
func (n *MobNavigation) MoveToEntity(m *mob.Mob, e world.Entity, speed float64) bool {
	return n.N.MoveToEntity(m.Tx(), agent{m}, e, speed)
}
func (n *MobNavigation) MoveAlong(m *mob.Mob, p mob.Path, speed float64) bool {
	pa, ok := p.(pathAdapter)
	if !ok || pa.p == nil {
		return false
	}
	return n.N.MoveAlong(m.Tx(), agent{m}, pa.p, speed)
}
func (n *MobNavigation) Path() mob.Path         { return wrapPath(n.N.Path()) }
func (n *MobNavigation) Stop(m *mob.Mob)        { n.N.Stop(agent{m}) }
func (n *MobNavigation) Done() bool             { return n.N.Done() }
func (n *MobNavigation) SetSpeed(speed float64) { n.N.SetSpeed(speed) }

// SetAvoidWater changes avoid_water at run time (panic with prefer_water, BDS
// NavigationComponent::setAvoidWater).
func (n *MobNavigation) SetAvoidWater(v bool) { n.N.Options.AvoidWater = v }

// AvoidWater reports the current avoid_water setting.
func (n *MobNavigation) AvoidWater() bool { return n.N.Options.AvoidWater }

// SetEndPathRadius makes the last node count as reached within r blocks.
func (n *MobNavigation) SetEndPathRadius(r float64) { n.N.SetEndPathRadius(r) }

// Pending reports whether a request waits for path budget.
func (n *MobNavigation) Pending() bool { return n.N.Pending() }

// Kind is the navigation kind.
func (n *MobNavigation) Kind() Kind { return n.N.Kind }

// pathAdapter is a *Path as a mob.Path.
type pathAdapter struct{ p *Path }

func wrapPath(p *Path) mob.Path {
	if p == nil {
		return nil
	}
	return pathAdapter{p}
}

func (p pathAdapter) Len() int            { return p.p.Len() }
func (p pathAdapter) Index() int          { return p.p.Index() }
func (p pathAdapter) Node(i int) cube.Pos { return p.p.NodePos(i) }
func (p pathAdapter) End() cube.Pos       { return p.p.LastPos() }
func (p pathAdapter) Complete() bool      { return p.p.Complete() }
func (p pathAdapter) Done() bool          { return p.p.Done() }

// Unwrap returns the nav path behind a mob.Path made by this package.
func Unwrap(p mob.Path) (*Path, bool) {
	pa, ok := p.(pathAdapter)
	return pa.p, ok && pa.p != nil
}

// agent is a *mob.Mob as an Agent and TraitSource.
type agent struct{ m *mob.Mob }

func (a agent) Position() mgl64.Vec3 { return a.m.Position() }
func (a agent) Size() (float64, float64) {
	meta := a.m.Meta()
	return meta.Width * meta.Scale, meta.Height * meta.Scale
}
func (a agent) OnGround() bool            { return a.m.OnGround() }
func (a agent) InWater() bool             { return a.m.Physics().InWater }
func (a agent) InLava() bool              { return a.m.Physics().InLava }
func (a agent) HorizontalCollision() bool { return a.m.Physics().HorizontalCollision }
func (a agent) SetWantedPosition(pos mgl64.Vec3, speed float64) {
	a.m.MoveControl().SetWantedPosition(pos, speed)
}
func (a agent) ClearWantedPosition() { a.m.MoveControl().Stop() }

// PathTraits are the per-mob pathing facts (ActorPathingData).
func (a agent) PathTraits() Traits {
	m := a.m
	fireImmune := m.HasComponent("minecraft:fire_immune")
	if _, ok := m.Effect(effect.FireResistance); ok {
		fireImmune = true
	}
	tr := Traits{
		StepHeight:  physics.MaxAutoStep(m),
		FollowRange: m.Attr(mob.AttrFollowRange),
		FireImmune:  fireImmune,
		DoorOpener:  m.HasComponent("minecraft:annotation.open_door"),
		DoorBreaker: doorBreaker(m),
		// can_stand_on_powder_snow (BDS CanStandOnSnowFlag) or the
		// lightweight family (HasLightweightFamilyFlag): powder snow holds.
		ClimbPowderSnow: m.HasComponent("minecraft:can_stand_on_powder_snow") || m.HasFamily("lightweight"),
	}
	_, hasTarget := m.Target()
	tr.PassFire = fireImmune || (hasTarget && m.HasFamily("zombie") && m.HasFamily("monster"))
	if pp, ok := mob.ComponentOf[*preferredPath](m); ok {
		tr.Preferred = &pp.pp
	}
	if c, ok := m.Component("minecraft:damage_sensor"); ok {
		if s, ok := c.(interface {
			CancelsDamage(*mob.Mob, world.DamageSource) bool
		}); ok {
			tr.BerryBushSafe = s.CancelsDamage(m, block.DamageSource{Block: block.SweetBerryBush{Age: 1}})
		}
	}
	return tr
}

// doorBreaker is Actor::isDoorBreaker: annotation.break_door is active and the difficulty is at
// least its min_difficulty (hard by default).
func doorBreaker(m *mob.Mob) bool {
	raw, ok := m.ComponentJSON("minecraft:annotation.break_door")
	if !ok {
		return false
	}
	var v struct {
		MinDifficulty string `json:"min_difficulty"`
	}
	_ = json.Unmarshal(raw, &v)
	min := 3
	if d, ok := map[string]int{"peaceful": 0, "easy": 1, "normal": 2, "hard": 3}[v.MinDifficulty]; ok {
		min = d
	}
	tx := m.Tx()
	if tx == nil {
		return false
	}
	id, ok := world.DifficultyID(tx.World().Difficulty())
	if !ok {
		id = 2
	}
	return id >= min
}
