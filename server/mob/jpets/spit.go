//go:build crossfly_full

package jpets

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Llama spit (projectile/LlamaSpit.java): gravity 0.06, drag 0.99, 1 damage
// on an entity, gone on any block or in water. Both clients draw it as
// minecraft:llama_spit.

type spitType struct{}

func (spitType) Open(tx *world.Tx, h *world.EntityHandle, data *world.EntityData) world.Entity {
	return entity.Open(tx, h, data)
}
func (spitType) EncodeEntity() string { return "minecraft:llama_spit" }
func (spitType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.125, 0, -0.125, 0.125, 0.25, 0.125)
}
func (spitType) DecodeNBT(map[string]any, *world.EntityData) {}
func (spitType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

// spit is Llama.spit: from in front of the llama's mouth towards the target's
// lower third, aimed slightly up by the distance, speed 1.5, inaccuracy 10.
func (h *Equine) spit(m *javamob.Mob, target world.Entity, _ float64) {
	w := m.Width()
	body := m.BodyYRot()
	from := mgl64.Vec3{
		m.Position()[0] - (w+1)*0.5*float64(sinDeg32(body)),
		m.EyeY() - float64(float32(0.1)),
		m.Position()[2] + (w+1)*0.5*float64(cosDeg32(body)),
	}
	t := target.Position()
	th := target.H().Type().BBox(target).Height()
	xd := t[0] - m.Position()[0]
	yd := t[1] + th/3 - from[1]
	zd := t[2] - m.Position()[2]
	yo := math.Sqrt(xd*xd+zd*zd) * float64(float32(0.2))
	vel := shootVelocity(m, mgl64.Vec3{xd, yd + yo, zd}, 1.5, 10)
	conf := entity.ProjectileBehaviourConfig{
		Owner:   m.H(),
		Gravity: 0.06,
		Drag:    0.01,
		Damage:  -1,
		Hit: func(e *entity.Ent, tx *world.Tx, res trace.Result) {
			r, ok := res.(trace.EntityResult)
			if !ok {
				return
			}
			if l, ok := r.Entity().(entity.Living); ok {
				owner, _ := m.H().Entity(tx)
				l.Hurt(1, entity.ProjectileDamageSource{Projectile: e, Owner: owner})
			}
		},
	}
	opts := world.EntitySpawnOpts{Position: from, Velocity: vel}
	m.Tx().AddEntity(opts.New(spitType{}, conf))
	m.PlaySound("spit")
	h.llama.didSpit = true
}

// shootVelocity is Projectile.shoot: the direction normalised, spread by a
// gaussian of 0.0075 x inaccuracy per axis, times the speed.
func shootVelocity(m *javamob.Mob, dir mgl64.Vec3, speed, inaccuracy float64) mgl64.Vec3 {
	if dir.Len() == 0 {
		return dir
	}
	r := m.Rand()
	n := dir.Normalize()
	gauss := func() float64 {
		// RandomSource.triangle(0, 0.0172275 * inaccuracy) in 26.x.
		return (r.Float64() - r.Float64()) * 0.0172275 * inaccuracy
	}
	return mgl64.Vec3{n[0] + gauss(), n[1] + gauss(), n[2] + gauss()}.Mul(speed)
}
