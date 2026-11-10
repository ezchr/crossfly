//go:build crossfly_full

package hostile

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/df-mc/dragonfly/server/mob/goal/village"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/gameevent"
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
// a solid top and refuses spots in liquid or inside blocks.
//
// Hits (BDS EnderMan::_hurt and EnderMan::hurtEffects, enderman only unless
// teleports_on_projectile_hit is set): a projectile that is not a thrown
// potion is dodged, up to 64 random teleports, and its damage is cancelled
// even when every teleport fails; any other damage (melee from anyone,
// fire, water) first teleports it once 9 times in 10 when the hit does not
// kill it, and once more after the damage.
//
// It also saves the block the mob carries (EnderMan::addAdditionalSaveData
// carriedBlock; goal/village.Carried), the enderman part this server has.

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

	enderman bool
	carried  *village.CarriedBlock
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
	t.enderman = m.Identifier() == "minecraft:enderman"
	t.carried = village.CarriedHolder(m)
}

// EncodeNBT saves the carried block.
func (t *Teleport) EncodeNBT() map[string]any {
	if t.carried == nil || t.carried.Block == nil {
		return nil
	}
	return map[string]any{"carriedBlock": nbtconv.WriteBlock(t.carried.Block)}
}

// DecodeNBT loads the carried block.
func (t *Teleport) DecodeNBT(m *mob.Mob, d map[string]any) {
	if b := nbtconv.Block(d, "carriedBlock"); b != nil {
		village.SetCarried(m, b)
	}
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
	// TeleportComponent::teleport: teleport where it left, made by the mob.
	if gameevent.Active(tx) {
		gameevent.Post(tx, gameevent.Teleport, from, m)
	}
	m.Teleport(pos)
	m.Navigation().Stop(m)
	tx.AddParticle(pos, particle.EndermanTeleport{})
	m.PlaySound("teleport")
	return true
}

// ModifyDamage is the teleport part of EnderMan::_hurt, before the damage
// applies.
func (t *Teleport) ModifyDamage(m *mob.Mob, d *mob.Damage) {
	if dodgedProjectile(d.Source) {
		if !t.enderman && !t.onProjectile {
			return
		}
		for i := 0; i < 64 && !t.RandomTeleport(m); i++ {
		}
		d.Cancel = true
		return
	}
	if !t.enderman {
		return
	}
	if m.Rand().IntN(10) != 0 && math.Trunc(m.Health())-d.Amount > 0 {
		t.RandomTeleport(m)
	}
}

// OnHurt is EnderMan::hurtEffects: damage that did not kill teleports it once
// more.
func (t *Teleport) OnHurt(m *mob.Mob, dmg float64, _ world.DamageSource) {
	if t.enderman && dmg > 0 && !m.Dead() {
		t.RandomTeleport(m)
	}
}

// dodgedProjectile reports whether src is projectile damage an enderman
// dodges: every projectile but splash and lingering potions.
func dodgedProjectile(src world.DamageSource) bool {
	s, ok := src.(entity.ProjectileDamageSource)
	if !ok {
		return false
	}
	if s.Projectile != nil {
		switch s.Projectile.H().Type().EncodeEntity() {
		case "minecraft:splash_potion", "minecraft:lingering_potion":
			return false
		}
	}
	return true
}
