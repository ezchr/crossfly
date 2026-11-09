//go:build crossfly_full

package hostile

import (
	"encoding/json"
	"fmt"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

// Teleport is minecraft:teleport (enderman, BDS TeleportComponent +
// tickTeleportComponent, defaults from TeleportDescription):
//
//   - in light above 0.5 under open sky it teleports randomly with
//     light_teleport_chance (0.01) per tick, in the dark (brightness <= 0.5)
//     with dark_teleport_chance (0.01);
//   - otherwise, with a target further than target_distance (16), it
//     teleports towards it with target_teleport_chance (1) per tick
//     (TeleportComponent::teleportTowards: 16 blocks along the line to the
//     target, +-4 spread);
//   - otherwise, with random_teleports (true), every
//     min_random_teleport_time..max_random_teleport_time (0..20 s) it
//     teleports within random_teleport_cube (32 x 32 x 32).
//
// TeleportComponent::teleport drops the destination to the first block with
// a solid top and refuses spots in liquid or inside blocks. With
// teleports_on_projectile_hit (1.26) a projectile hit is dodged by a
// teleport (up to 64 tries, damage cancelled; BDS EnderMan::_hurt); other
// damage without a living attacker (water, fire) teleports it 9 times in
// 10 afterwards (EnderMan::hurtEffects; the 9/10 is java/paper).

type Teleport struct {
	random           bool
	minTime, maxTime int
	cube             mgl64.Vec3
	targetDist       float64
	targetChance     float64
	lightChance      float64
	darkChance       float64
	onProjectile     bool
	timer            int
}

func newTeleport(key string, raw json.RawMessage) (mob.Component, error) {
	var v struct {
		Random       *bool     `json:"random_teleports"`
		MinTime      *float64  `json:"min_random_teleport_time"`
		MaxTime      *float64  `json:"max_random_teleport_time"`
		Cube         []float64 `json:"random_teleport_cube"`
		TargetDist   *float64  `json:"target_distance"`
		TargetChance *float64  `json:"target_teleport_chance"`
		Light        *float64  `json:"light_teleport_chance"`
		Dark         *float64  `json:"dark_teleport_chance"`
		OnProjectile bool      `json:"teleports_on_projectile_hit"`
	}
	if !kit.IsNull(raw) {
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("%v: %w", key, err)
		}
	}
	t := &Teleport{
		random: kit.Or(v.Random, true), minTime: int(float32(kit.Or(v.MinTime, 0)) * 20), maxTime: int(float32(kit.Or(v.MaxTime, 20)) * 20),
		cube: mgl64.Vec3{32, 32, 32}, targetDist: kit.Or(v.TargetDist, 16), targetChance: kit.Or(v.TargetChance, 1),
		lightChance: kit.Or(v.Light, 0.01), darkChance: kit.Or(v.Dark, 0.01), onProjectile: v.OnProjectile,
	}
	if len(v.Cube) == 3 {
		t.cube = mgl64.Vec3{v.Cube[0], v.Cube[1], v.Cube[2]}
	}
	return t, nil
}

// Attach ...
func (t *Teleport) Attach(m *mob.Mob) {
	t.resetTimer(m)
}

// Detach ...
func (*Teleport) Detach(*mob.Mob) {}

func (t *Teleport) resetTimer(m *mob.Mob) {
	t.timer = t.minTime
	if t.maxTime > t.minTime {
		t.timer += m.Rand().IntN(t.maxTime - t.minTime)
	}
}

// tick is tickTeleportComponent.
func (t *Teleport) tick(m *mob.Mob) {
	if m.Dead() {
		return
	}
	tx := m.Tx()
	if t.lightChance > 0 || t.darkChance > 0 {
		pos := cube.PosFromVec3(m.Position())
		bright := sense.Brightness(tx, pos)
		chance, ok := t.darkChance, true
		if bright > 0.5 {
			chance, ok = t.lightChance, sense.SkyVisible(tx, pos)
		}
		if ok && chance > 0 && m.Rand().Float64() < chance {
			t.RandomTeleport(m)
			return
		}
	}
	if target, ok := m.Target(); ok {
		if kit.DistSq(target.Position(), m.Position()) > t.targetDist*t.targetDist && m.Rand().Float64() < t.targetChance {
			t.TeleportTowards(m, target)
		}
		return
	}
	if !t.random {
		return
	}
	if t.timer--; t.timer <= 0 {
		t.resetTimer(m)
		t.RandomTeleport(m)
	}
}

// RandomTeleport is TeleportComponent::randomTeleport.
func (t *Teleport) RandomTeleport(m *mob.Mob) bool {
	r := m.Rand()
	p := m.Position()
	dest := mgl64.Vec3{p[0] + (r.Float64()-0.5)*t.cube[0], p[1] + (r.Float64()-0.5)*t.cube[1], p[2] + (r.Float64()-0.5)*t.cube[2]}
	return TeleportTo(m, dest)
}

// TeleportTowards is TeleportComponent::teleportTowards.
func (t *Teleport) TeleportTowards(m *mob.Mob, target world.Entity) bool {
	d := m.Position().Sub(sense.EyePos(target))
	if d.Len() >= 0.0001 {
		d = d.Normalize()
	} else {
		d = mgl64.Vec3{}
	}
	r := m.Rand()
	p := m.Position()
	dest := mgl64.Vec3{
		p[0] + (r.Float64()-0.5)*8 - d[0]*16,
		p[1] + (r.Float64()-0.5)*8 - d[1]*16,
		p[2] + (r.Float64()-0.5)*8 - d[2]*16,
	}
	return TeleportTo(m, dest)
}

// TeleportTo is TeleportComponent::teleport: drop to ground, check the
// spot, move, effects. It reports whether the mob moved.
func TeleportTo(m *mob.Mob, dest mgl64.Vec3) bool {
	tx := m.Tx()
	bp := cube.PosFromVec3(dest)
	minY := tx.Range()[0]
	for bp[1] > minY && !kit.Solid(tx, bp.Side(cube.FaceDown)) {
		bp[1]--
	}
	if bp[1] <= minY || !kit.Solid(tx, bp.Side(cube.FaceDown)) {
		return false
	}
	pos := mgl64.Vec3{dest[0], float64(bp[1]), dest[2]}
	box := m.H().Type().BBox(m).Translate(pos)
	low, high := cube.PosFromVec3(box.Min()), cube.PosFromVec3(box.Max().Sub(mgl64.Vec3{1e-4, 1e-4, 1e-4}))
	for q := range cube.Range3D(low, high) {
		if _, liquid := tx.Liquid(q); liquid {
			return false
		}
		for _, bb := range tx.Block(q).Model().BBox(q, tx) {
			if bb.Translate(q.Vec3()).IntersectsWith(box) {
				return false
			}
		}
	}
	from := m.Position()
	tx.AddParticle(from, particle.EndermanTeleport{})
	m.Teleport(pos)
	m.Navigation().Stop(m)
	tx.AddParticle(pos, particle.EndermanTeleport{})
	m.PlaySound("teleport")
	return true
}

// ModifyDamage dodges projectiles (teleports_on_projectile_hit).
func (t *Teleport) ModifyDamage(m *mob.Mob, d *mob.Damage) {
	if !t.onProjectile {
		return
	}
	if _, ok := d.Source.(entity.ProjectileDamageSource); !ok {
		return
	}
	for i := 0; i < 64; i++ {
		if t.RandomTeleport(m) {
			d.Cancel = true
			return
		}
	}
}

// OnHurt teleports after damage with no living attacker (EnderMan::hurtEffects).
func (t *Teleport) OnHurt(m *mob.Mob, _ float64, src world.DamageSource) {
	switch s := src.(type) {
	case entity.AttackDamageSource:
		if _, living := s.Attacker.(entity.Living); living {
			return
		}
	case entity.ProjectileDamageSource:
		return
	}
	if m.Dead() || m.Rand().IntN(10) == 0 {
		return
	}
	for i := 0; i < 64; i++ {
		if t.RandomTeleport(m) {
			return
		}
	}
}
