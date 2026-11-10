//go:build crossfly_full

package jpets

import (
	"math"

	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Llamas (animal/equine/Llama.java): strength, caravans and spitting. The
// rest of a llama is an Equine of kindLlama.

// llamaState is Llama's own fields.
type llamaState struct {
	// Strength is DATA_STRENGTH_ID (1..5): inventory columns, how much
	// wolves fear it.
	Strength int
	// didSpit ends LlamaHurtByTargetGoal after a spit.
	didSpit bool
	// head is the llama this one follows (caravanHead), tail the one
	// following it (caravanTail).
	head, tail *world.EntityHandle
}

func (s *llamaState) inCaravan() bool      { return s.head != nil }
func (s *llamaState) hasCaravanTail() bool { return s.tail != nil }

// llamaOf returns the llama state of e, if e is a llama of this package.
func llamaOf(tx *world.Tx, h *world.EntityHandle) (*javamob.Mob, *Equine, bool) {
	jm, ok := javamob.OfHandle(tx, h)
	if !ok {
		return nil, nil, false
	}
	b, ok := jm.Behaviour().(*Equine)
	if !ok || b.kind != kindLlama {
		return nil, nil, false
	}
	return jm, b, true
}

// joinCaravan is Llama.joinCaravan: follow head.
func (h *Equine) joinCaravan(m *javamob.Mob, head *javamob.Mob, hb *Equine) {
	h.llama.head = head.H()
	hb.llama.tail = m.H()
}

// leaveCaravan is Llama.leaveCaravan.
func (h *Equine) leaveCaravan(m *javamob.Mob) {
	if h.llama.head != nil {
		if _, hb, ok := llamaOf(m.Tx(), h.llama.head); ok && hb.llama.tail == m.H() {
			hb.llama.tail = nil
		}
	}
	h.llama.head = nil
}

// llamaFollowCaravan is LlamaFollowCaravanGoal (flags MOVE): a free llama
// joins the end of a caravan within 9 x 4 x 9, or the back of a leashed
// llama, whose first llama is on a leash, and walks 2 blocks behind it,
// faster (x1.2 up to 3) when more than 26 blocks behind.
type llamaFollowCaravan struct {
	h                *Equine
	speed            float64
	distCheckCounter int
}

func (g *llamaFollowCaravan) Flags() javamob.Flags { return javamob.FlagMove }

func (g *llamaFollowCaravan) CanUse(m *javamob.Mob) bool {
	if leashed(m) || g.h.llama.inCaravan() {
		return false
	}
	box := m.BBox().GrowVec3(mgl64.Vec3{9, 4, 9})
	var (
		closest   *javamob.Mob
		closestB  *Equine
		closestSq = math.MaxFloat64
	)
	var llamas []*javamob.Mob
	for e := range m.Tx().EntitiesWithin(box) {
		if e.H() == m.H() {
			continue
		}
		if id := entityID(e); id != "minecraft:llama" && id != "minecraft:trader_llama" {
			continue
		}
		if jm, ok := javamob.Of(e); ok {
			llamas = append(llamas, jm)
		}
	}
	pick := func(ok func(*javamob.Mob, *Equine) bool) {
		for _, c := range llamas {
			cb, isEquine := c.Behaviour().(*Equine)
			if !isEquine || !ok(c, cb) || cb.llama.hasCaravanTail() {
				continue
			}
			d := distSq(m.Position(), c.Position())
			if d <= closestSq {
				closestSq, closest, closestB = d, c, cb
			}
		}
	}
	pick(func(_ *javamob.Mob, cb *Equine) bool { return cb.llama.inCaravan() })
	if closest == nil {
		pick(func(c *javamob.Mob, _ *Equine) bool { return leashed(c) })
	}
	if closest == nil || closestSq < 4 {
		return false
	}
	if !leashed(closest) && !firstIsLeashed(m.Tx(), closestB, 1) {
		return false
	}
	g.h.joinCaravan(m, closest, closestB)
	return true
}

// firstIsLeashed is LlamaFollowCaravanGoal.firstIsLeashed: the caravan,
// followed forward at most 8 llamas, starts with a leashed llama.
func firstIsLeashed(tx *world.Tx, b *Equine, counter int) bool {
	for counter <= 8 && b.llama.inCaravan() {
		head, hb, ok := llamaOf(tx, b.llama.head)
		if !ok {
			return false
		}
		if leashed(head) {
			return true
		}
		b = hb
		counter++
	}
	return false
}

func (g *llamaFollowCaravan) CanContinueToUse(m *javamob.Mob) bool {
	if !g.h.llama.inCaravan() {
		return false
	}
	head, _, ok := llamaOf(m.Tx(), g.h.llama.head)
	if !ok || head.Dead() || !firstIsLeashed(m.Tx(), g.h, 0) {
		return false
	}
	if distSq(m.Position(), head.Position()) > 676 {
		if g.speed <= 3 {
			g.speed *= 1.2
			g.distCheckCounter = javamob.ReducedTickDelay(40)
			return true
		}
		if g.distCheckCounter == 0 {
			return false
		}
	}
	if g.distCheckCounter > 0 {
		g.distCheckCounter--
	}
	return true
}

func (g *llamaFollowCaravan) Stop(m *javamob.Mob) {
	g.h.leaveCaravan(m)
	g.speed = 2.1
}

func (g *llamaFollowCaravan) Tick(m *javamob.Mob) {
	if !g.h.llama.inCaravan() || leashedToFence(m) {
		return
	}
	head, _, ok := llamaOf(m.Tx(), g.h.llama.head)
	if !ok {
		return
	}
	d := head.Position().Sub(m.Position())
	dist := d.Len()
	if dist == 0 {
		return
	}
	delta := d.Mul(1 / dist).Mul(math.Max(dist-2, 0))
	m.Navigation().MoveTo(m, m.Position().Add(delta), 0, g.speed)
}

// leashedToFence reports whether the mob's leash is tied to a fence post
// (LeashFenceKnotEntity).
func leashedToFence(m *javamob.Mob) bool {
	h, ok := core.LeashHolder(m.Mob)
	return ok && entityID(h) == "minecraft:leash_knot"
}
