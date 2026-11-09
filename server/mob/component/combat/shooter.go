//go:build crossfly_full

package combat

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// projectileDef is one projectile a shooter can fire.
type projectileDef struct {
	id         string
	auxVal     int
	chance     float64
	filter     mob.Filter
	loseTarget bool
}

// Shooter is minecraft:shooter (BDS ShooterComponent): the ranged attack
// goal calls Shoot. The first "projectiles" entry whose chance roll and
// filters pass is fired, else "def".
type Shooter struct {
	def         projectileDef
	projectiles []projectileDef
	power       float64
	sound       string
}

func newShooter(_ string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		Def         string  `json:"def"`
		AuxVal      int     `json:"aux_val"`
		Power       float64 `json:"power"`
		Sound       string  `json:"sound"`
		Projectiles []struct {
			Def        string          `json:"def"`
			AuxVal     int             `json:"aux_val"`
			Chance     *float64        `json:"chance"`
			Filters    json.RawMessage `json:"filters"`
			LoseTarget bool            `json:"lose_target"`
		} `json:"projectiles"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("minecraft:shooter: %w", err)
	}
	s := &Shooter{def: projectileDef{id: v.Def, auxVal: v.AuxVal, chance: 1, filter: mob.AlwaysTrue}, power: v.Power, sound: v.Sound}
	for _, p := range v.Projectiles {
		f, err := mob.CompileFilter(p.Filters)
		if err != nil {
			return nil, fmt.Errorf("minecraft:shooter: %w", err)
		}
		c := 1.0
		if p.Chance != nil {
			c = *p.Chance
		}
		s.projectiles = append(s.projectiles, projectileDef{id: p.Def, auxVal: p.AuxVal, chance: c, filter: f, loseTarget: p.LoseTarget})
	}
	return s, nil
}

// Attach ...
func (*Shooter) Attach(*mob.Mob) {}

// Detach ...
func (*Shooter) Detach(*mob.Mob) {}

// Shoot fires at target (ShooterComponent::onShoot).
func (s *Shooter) Shoot(m *mob.Mob, target world.Entity) {
	ctx := m.Ctx()
	ctx.Target, ctx.Other = target, target
	p := s.def
	for _, d := range s.projectiles {
		if m.Rand().Float64() < d.chance && d.filter.Test(ctx) {
			p = d
			break
		}
	}
	if !ShootProjectile(m, target, p.id, p.auxVal, s.power) {
		return
	}
	if s.sound == "bow" || (s.sound == "" && strings.HasSuffix(p.id, "arrow")) {
		m.Tx().PlaySound(m.Position(), sound.BowShoot{})
	}
	if p.loseTarget {
		m.ResetTarget()
	}
}

// projectileStats are the minecraft:projectile values of the mob variants
// of the vanilla projectiles (arrow.json minecraft:mob_arrow, ...).
var projectileStats = map[string]struct{ power, uncBase, uncMult float64 }{
	"minecraft:arrow":            {1.6, 16, 4},
	"minecraft:snowball":         {1.5, 1, 0},
	"minecraft:egg":              {1.5, 1, 0},
	"minecraft:splash_potion":    {0.5, 0, 0},
	"minecraft:lingering_potion": {0.5, 0, 0},
	"minecraft:ender_pearl":      {1.5, 1, 0},
}

var unsupportedProjectiles sync.Map

// ProjectileSpawner fires a projectile this file has no entity for, from m
// at target (power 0: the projectile's own).
type ProjectileSpawner func(m *mob.Mob, target world.Entity, auxVal int, power float64) bool

// extraProjectiles are the projectiles other mob packages registered.
var extraProjectiles = map[string]ProjectileSpawner{}

// RegisterProjectile lets ShootProjectile fire projectile id through f
// (package mob/component/boss: wither skulls, dragon fireballs, breeze wind
// charges). Call it from init only.
func RegisterProjectile(id string, f ProjectileSpawner) { extraProjectiles[id] = f }

// ShootProjectile fires projectile id from m's eyes at target (BDS
// Level::shootProjectileFromDefinition + ProjectileComponent::shoot):
// aimed at a third of the target's height, raised by 0.2 per block of
// horizontal distance for arrows, with the projectile's uncertainty
// (uncertainty_base - difficulty*uncertainty_multiplier). power 0 uses the
// projectile's own. It reports whether a projectile was spawned.
// source: java/paper AbstractSkeleton.performRangedAttack for the aim.
func ShootProjectile(m *mob.Mob, target world.Entity, id string, auxVal int, power float64) bool {
	if f, ok := extraProjectiles[id]; ok {
		return f(m, target, auxVal, power)
	}
	stats, ok := projectileStats[id]
	if !ok {
		if _, seen := unsupportedProjectiles.LoadOrStore(id, true); !seen {
			slog.Default().Warn("mob: shooter projectile not supported", "projectile", id, "entity", m.Identifier())
		}
		return false
	}
	if power <= 0 {
		power = stats.power
	}
	tx := m.Tx()
	start := m.EyePosition().Add(mgl64.Vec3{0, -0.1, 0})
	tb := sense.BBox(target)
	aim := mgl64.Vec3{target.Position()[0], tb.Min()[1] + tb.Height()/3, target.Position()[2]}
	d := aim.Sub(start)
	if id == "minecraft:arrow" {
		d[1] += math.Sqrt(d[0]*d[0]+d[2]*d[2]) * 0.2
	}
	if d.Len() < 1e-4 {
		return false
	}
	d = d.Normalize()
	unc := math.Max(0, stats.uncBase-float64(sense.DifficultyID(tx))*stats.uncMult)
	r := m.Rand()
	for i := range d {
		d[i] += r.NormFloat64() * 0.0075 * unc
	}
	vel := d.Normalize().Mul(power)
	yaw := mgl64.RadToDeg(math.Atan2(-vel[0], vel[2]))
	pitch := -mgl64.RadToDeg(math.Atan2(vel[1], math.Sqrt(vel[0]*vel[0]+vel[2]*vel[2])))
	opts := world.EntitySpawnOpts{Position: start, Velocity: vel, Rotation: cube.Rotation{yaw, pitch}}
	var h *world.EntityHandle
	switch id {
	case "minecraft:arrow":
		conf := entity.ProjectileBehaviourConfig{
			Owner: m.H(), Gravity: 0.05, Drag: 0.01, Damage: 2, Sound: sound.ArrowHit{},
			SurviveBlockCollision: true, DisablePickup: true,
		}
		if auxVal > 0 {
			conf.Potion = potion.From(int32(auxVal - 1))
		}
		h = opts.New(entity.ArrowType, conf)
	case "minecraft:snowball":
		h = entity.NewSnowball(opts, m)
	case "minecraft:egg":
		h = entity.NewEgg(opts, m)
	case "minecraft:splash_potion":
		h = entity.NewSplashPotion(opts, potion.From(int32(auxVal)), m)
	case "minecraft:lingering_potion":
		h = entity.NewLingeringPotion(opts, potion.From(int32(auxVal)), m)
	case "minecraft:ender_pearl":
		h = entity.NewEnderPearl(opts, m)
	}
	if h == nil {
		return false
	}
	tx.AddEntity(h)
	return true
}
