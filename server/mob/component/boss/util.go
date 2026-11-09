//go:build crossfly_full

package boss

import (
	"bytes"
	"encoding/json"
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// ActorEvent is a Bedrock actor event (gophertunnel packet.ActorEvent*)
// shown on an entity, as a world.EntityAction both client sessions
// translate (server/session plays it as an ActorEvent packet,
// server/java/javasession as the matching Java entity or game event).
type ActorEvent uint8

// EntityAction ...
func (ActorEvent) EntityAction() {}

// ActorEvent ...
func (a ActorEvent) ActorEvent() uint8 { return uint8(a) }

// ShowActorEvent shows an actor event of e to the viewers near it.
func ShowActorEvent(tx *world.Tx, e world.Entity, ev uint8) {
	for _, v := range tx.Viewers(e.Position()) {
		v.ViewEntityAction(e, ActorEvent(ev))
	}
}

// Ticks converts seconds to ticks (rounded, as BDS definitions do).
func Ticks(seconds float64) int { return sense.Ticks(seconds) }

func decode(raw json.RawMessage, v any) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || bytes.Equal(raw, []byte("{}")) {
		return nil
	}
	return json.Unmarshal(raw, v)
}

func or[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

// AttackablePlayer reports whether e is a living player a mob may attack
// (not creative or spectator).
func AttackablePlayer(e world.Entity) bool {
	return sense.IsPlayer(e) && sense.Alive(e) && !sense.Invulnerable(e)
}

// NearestPlayer returns the nearest attackable player within r of pos
// (BDS Dimension::fetchNearestAttackablePlayer).
func NearestPlayer(tx *world.Tx, pos mgl64.Vec3, r float64) (world.Entity, bool) {
	var best world.Entity
	bestD := r * r
	for p := range tx.Players() {
		if !AttackablePlayer(p) {
			continue
		}
		if d := p.Position().Sub(pos).LenSqr(); d <= bestD {
			best, bestD = p, d
		}
	}
	return best, best != nil
}

// Living reports whether e is alive (players and mobs).
func Living(e world.Entity) bool { return sense.Alive(e) }

// IsLiving reports whether e is a player or a mob.
func IsLiving(e world.Entity) bool {
	if _, ok := mob.Of(e); ok {
		return true
	}
	_, ok := e.(entity.Living)
	return ok
}

// velocityEntity is an entity whose velocity may be set (players, mobs).
type velocityEntity interface {
	Velocity() mgl64.Vec3
	SetVelocity(v mgl64.Vec3)
}

// knockbackResistance is the knockback_resistance attribute of e (players
// 0, or with netherite armour, which Dragonfly folds into KnockBack).
func knockbackResistance(e world.Entity) float64 {
	if m, ok := mob.Of(e); ok {
		return m.Attr(mob.AttrKnockbackResistance)
	}
	return 0
}

// KnockBack is BDS Mob::knockback (non-legacy): the velocity halves, then
// h*0.4 horizontally away from src and v up, scaled by 1 - knockback
// resistance, with the Y velocity capped at cap.
func KnockBack(e world.Entity, src mgl64.Vec3, h, v, cap float64) {
	ve, ok := e.(velocityEntity)
	if !ok {
		return
	}
	res := knockbackResistance(e)
	if res >= 1 {
		return
	}
	scale := math.Max(1-res, 0)
	d := e.Position().Sub(src)
	d[1] = 0
	if d.Len() > 1e-4 {
		d = d.Normalize()
	}
	vel := ve.Velocity().Mul(0.5)
	vel = vel.Add(mgl64.Vec3{d[0] * h * scale * 0.4, v * scale, d[2] * h * scale * 0.4})
	vel[1] = math.Min(vel[1], cap)
	ve.SetVelocity(vel)
}

// Push adds an impulse to e's velocity (wing knockback, wind bursts).
func Push(e world.Entity, impulse mgl64.Vec3) {
	if ve, ok := e.(velocityEntity); ok {
		ve.SetVelocity(ve.Velocity().Add(impulse))
	}
}

// SonicBoomDamageSource is the warden's sonic boom (BDS cause 31,
// SonicBoom): it ignores armour and enchantments but not Resistance.
type SonicBoomDamageSource struct{ Attacker world.Entity }

func (SonicBoomDamageSource) ReducedByArmour() bool     { return false }
func (SonicBoomDamageSource) ReducedByResistance() bool { return true }
func (SonicBoomDamageSource) Fire() bool                { return false }
func (SonicBoomDamageSource) IgnoreTotem() bool         { return false }
func (SonicBoomDamageSource) AffectedByEnchantment(item.EnchantmentType) bool {
	return false
}

// MobGriefing reports whether mobs may break blocks (game rule
// mobGriefing, combat.MobGriefing).
func MobGriefing(tx *world.Tx) bool { return combat.MobGriefing(tx) }

// isAir reports whether the block at pos is air.
func isAir(tx *world.Tx, pos cube.Pos) bool {
	_, ok := tx.Block(pos).(block.Air)
	return ok
}

// blockName is the identifier of a block.
func blockName(b world.Block) string {
	n, _ := b.EncodeBlock()
	return n
}

// solidTop reports whether the block at pos has a full solid top face.
func solidTop(tx *world.Tx, pos cube.Pos) bool {
	b := tx.Block(pos)
	if _, ok := b.(block.Air); ok {
		return false
	}
	if _, ok := b.(world.Liquid); ok {
		return false
	}
	return b.Model().FaceSolid(pos, cube.FaceUp, tx)
}

// AboveTopSolid is BDS BlockSource::getAboveTopSolidBlock: the first y
// above the highest block with a solid top at or below startY in column
// (x, z), or the bottom of the world if there is none.
func AboveTopSolid(tx *world.Tx, x, startY, z int) int {
	r := tx.Range()
	y := min(startY, tx.HighestBlock(x, z))
	for ; y >= r[0]; y-- {
		if solidTop(tx, cube.Pos{x, y, z}) {
			return y + 1
		}
	}
	return r[0]
}

// boxBlocks calls f for every block position inside box (floored).
func boxBlocks(box cube.BBox, f func(p cube.Pos)) {
	mn, mx := box.Min(), box.Max()
	for x := int(math.Floor(mn[0])); x <= int(math.Floor(mx[0])); x++ {
		for y := int(math.Floor(mn[1])); y <= int(math.Floor(mx[1])); y++ {
			for z := int(math.Floor(mn[2])); z <= int(math.Floor(mx[2])); z++ {
				f(cube.Pos{x, y, z})
			}
		}
	}
}

// boxAt is a box of w x h x w centred on c.
func boxAt(c mgl64.Vec3, w, h float64) cube.BBox {
	return cube.Box(c[0]-w/2, c[1]-h/2, c[2]-w/2, c[0]+w/2, c[1]+h/2, c[2]+w/2)
}

// wrapDegrees wraps an angle to [-180, 180).
func wrapDegrees(a float64) float64 {
	a = math.Mod(a+180, 360)
	if a < 0 {
		a += 360
	}
	return a - 180
}

// clamp ...
func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

// rad/deg helpers.
func sinDeg(a float64) float64 { return math.Sin(mgl64.DegToRad(a)) }
func cosDeg(a float64) float64 { return math.Cos(mgl64.DegToRad(a)) }
