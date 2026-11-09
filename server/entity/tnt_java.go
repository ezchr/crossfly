//go:build crossfly_full

package entity

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// javaTick ticks primed TNT as Java's PrimedTnt.tick: gravity, the move, air drag (0.98) and ground friction, then
// its fuse goes down a tick; on the tick the fuse runs out it is removed and explodes, otherwise water and lava
// currents push it (EntityFluidInteraction). The passive expiry alone fired two ticks later and left the entity
// for another tick, and the passive movement applied drag before the move.
func (b *tntBehaviour) javaTick(e *Ent, tx *world.Tx) *Movement {
	p := b.PassiveBehaviour
	if p.close || b.exploded {
		_ = e.Close()
		return nil
	}
	pos, vel := e.data.Pos, e.data.Vel
	viewers := tx.Viewers(pos)
	velBefore := vel
	vel[1] -= 0.04
	dPos, vel := p.mc.CheckCollision(tx, e, pos, vel)
	vel = vel.Mul(0.98)
	if p.mc.OnGround() {
		vel = mgl64.Vec3{vel[0] * 0.7, vel[1] * -0.5, vel[2] * 0.7}
	}
	m := &Movement{v: viewers, e: e, pos: pos.Add(dPos), vel: vel, dpos: dPos, dvel: vel.Sub(velBefore), rot: e.data.Rot, onGround: p.mc.OnGround()}
	e.data.Pos, e.data.Vel = m.pos, m.vel
	p.fallDistance = math.Max(p.fallDistance-dPos[1], 0)

	p.fuse = p.conf.ExistenceDuration - e.Age()
	if p.Fuse()%(time.Second/4) == 0 {
		for _, v := range tx.Viewers(m.pos) {
			v.ViewEntityState(e)
		}
	}
	if p.fuse-time.Second/20 <= 0 {
		b.exploded = true
		_ = e.Close()
		explodeTNT(e, tx, b)
		return nil
	}
	applyFluidCurrents(e, tx)
	return m
}
