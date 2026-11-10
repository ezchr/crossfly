//go:build crossfly_full

package jnether

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The ghast (Ghast, Paper 26.3): it floats to random spots within 16
// blocks (RandomFloatAroundGoal, GhastMoveControl: a push of flying speed x
// 5/3 every 2-6 ticks towards the spot while the way is clear), faces its
// target, and charges a large fireball for 20 ticks (sound at 10) at a
// player within 64 it sees, then rests 40 ticks. A fireball sent back by a
// player kills it (Ghast.hurtServer: 1000 damage).

func init() {
	javamob.Register(javamob.Type{
		ID: "minecraft:ghast", Category: javamob.CategoryMonster,
		Width: 4, Height: 4, EyeHeight: 2.6,
		FireImmune: true, NotInPeaceful: true,
		Attributes: func() map[javamob.Attribute]float64 {
			a := javamob.LivingAttributes()
			a[javamob.AttrMaxHealth] = 10
			a[javamob.AttrFollowRange] = 100
			a[javamob.AttrFlyingSpeed] = 0.06
			return a
		},
		Families: []string{"ghast", "monster", "mob"},
		XPReward: 5,
		New:      func() javamob.Behaviour { return &Ghast{ExplosionPower: 1} },
	})
}

// Ghast is the Ghast class.
type Ghast struct {
	ExplosionPower int
	move           GhastMoveControl
}

// RegisterGoals is Ghast.registerGoals.
func (g *Ghast) RegisterGoals(m *javamob.Mob) {
	m.UpdateMeta(func(meta *mob.Meta) { meta.NoGravity = true })
	m.Goals().Add(5, &RandomFloatAround{GoalBase: javamob.GoalBase{Control: javamob.FlagMove}, Move: &g.move})
	m.Goals().Add(7, &ghastLook{GoalBase: javamob.GoalBase{Control: javamob.FlagLook}})
	m.Goals().Add(7, &ghastShoot{g: g})
	t := goal.NewNearestAttackableTarget(nil, true, false)
	t.Selector = func(e world.Entity, tx *world.Tx) bool {
		jm, ok := javamob.OfHandle(tx, m.H())
		return ok && math.Abs(e.Position()[1]-jm.Position()[1]) <= 4
	}
	m.Targets().Add(1, t)
}

// CustomServerAiStep runs GhastMoveControl.tick (MoveControl.tick follows
// customServerAiStep in Mob.serverAiStep).
func (g *Ghast) CustomServerAiStep(m *javamob.Mob) { g.move.Tick(m) }

// PostTick finishes Ghast.travel (travelFlying(input, 0.02)): the base
// moves the ghast without gravity (NoGravity) and slows it by 0.91
// sideways and 0.98 vertically as for any mob in the air; travelFlying
// slows all three by 0.91, so the vertical part is corrected here. The
// ghast's input is always zero, so moveRelative adds nothing.
func (g *Ghast) PostTick(m *javamob.Mob) {
	if m.InLiquid() || m.OnGround() {
		return
	}
	v := m.Velocity()
	m.SetVelocity(mgl64.Vec3{v[0], v[1] * 0.91 / 0.98, v[2]})
}

// FilterDamage is Ghast.hurtServer/isInvulnerableTo: a large fireball a
// player sent back deals 1000.
func (g *Ghast) FilterDamage(m *javamob.Mob, d *mob.Damage) {
	if s, ok := d.Source.(FireballDamageSource); ok && s.Large && javamob.IsPlayer(s.Owner) {
		d.Amount = 1000
		return
	}
	fireImmune(d)
}

// Persister: ExplosionPower.
func (g *Ghast) EncodeNBT(data map[string]any) { data["ExplosionPower"] = uint8(g.ExplosionPower) }
func (g *Ghast) DecodeNBT(_ *javamob.Mob, data map[string]any) {
	if v, ok := data["ExplosionPower"].(uint8); ok {
		g.ExplosionPower = int(v)
	}
}

// GhastMoveControl is Ghast.GhastMoveControl (careful false): every 2-6
// ticks a push of flying speed x 5/3 towards the wanted spot, if nothing
// collides on the way; otherwise it stops wanting to move. The happy ghast
// uses it too (careful).
type GhastMoveControl struct {
	Careful       bool
	wanted        mgl64.Vec3
	moving        bool
	floatDuration int
}

// SetWanted is setWantedPosition.
func (c *GhastMoveControl) SetWanted(p mgl64.Vec3) { c.wanted, c.moving = p, true }

// HasWanted is hasWanted.
func (c *GhastMoveControl) HasWanted() bool { return c.moving }

// Wanted is the wanted position.
func (c *GhastMoveControl) Wanted() mgl64.Vec3 { return c.wanted }

// Tick is GhastMoveControl.tick.
func (c *GhastMoveControl) Tick(m *javamob.Mob) {
	if !c.moving {
		return
	}
	c.floatDuration--
	if c.floatDuration+1 > 0 {
		return
	}
	c.floatDuration += m.Rand().IntN(5) + 2
	travel := c.wanted.Sub(m.Position())
	if c.canReach(m, travel) {
		m.SetVelocity(m.Velocity().Add(normalize(travel).Mul(m.Attr(javamob.AttrFlyingSpeed) * 5 / 3)))
	} else {
		c.moving = false
	}
}

// canReach: no block collision box crossing the box's path to the spot.
func (c *GhastMoveControl) canReach(m *javamob.Mob, travel mgl64.Vec3) bool {
	box := m.BBox()
	dest := box.Translate(travel)
	tx := m.Tx()
	steps := int(math.Ceil(travel.Len()))
	for i := 1; i <= steps; i++ {
		b := box.Translate(travel.Mul(float64(i) / float64(steps)))
		if collides(tx, b, box) {
			return false
		}
	}
	return !collides(tx, dest, box)
}

// collides reports whether a block collision box in b meets b (blocks the
// start box already touches are left out, as aabb.intersects(blockPos)).
func collides(tx *world.Tx, b, start cube.BBox) bool {
	mn, mx := b.Min(), b.Max()
	for x := int(math.Floor(mn[0])); x <= int(math.Floor(mx[0])); x++ {
		for y := int(math.Floor(mn[1])); y <= int(math.Floor(mx[1])); y++ {
			for z := int(math.Floor(mn[2])); z <= int(math.Floor(mx[2])); z++ {
				pos := cube.Pos{x, y, z}
				if start.IntersectsWith(cube.Box(float64(x), float64(y), float64(z), float64(x+1), float64(y+1), float64(z+1))) {
					continue
				}
				for _, bb := range tx.Block(pos).Model().BBox(pos, tx) {
					if bb.Translate(pos.Vec3()).IntersectsWith(b) {
						return true
					}
				}
			}
		}
	}
	return false
}

// RandomFloatAround is Ghast.RandomFloatAroundGoal: a new random spot within
// 16 blocks when there is none, it is reached (within 1) or too far (60).
type RandomFloatAround struct {
	javamob.GoalBase
	Move *GhastMoveControl
	// DistanceToBlocks is the happy ghast's wish to stay near blocks (0: none).
	DistanceToBlocks int
}

func (g *RandomFloatAround) CanContinueToUse(*javamob.Mob) bool { return false }
func (g *RandomFloatAround) CanUse(m *javamob.Mob) bool {
	if !g.Move.HasWanted() {
		return true
	}
	d := g.Move.Wanted().Sub(m.Position()).LenSqr()
	return d < 1 || d > 3600
}
func (g *RandomFloatAround) Start(m *javamob.Mob) {
	g.Move.SetWanted(SuitableFlyToPosition(m, g.DistanceToBlocks))
}

// SuitableFlyToPosition is getSuitableFlyToPosition: up to 64 tries for a
// spot within 16 on each axis (inside the home, near blocks if asked);
// above the terrain height the spot is mirrored down.
func SuitableFlyToPosition(m *javamob.Mob, distanceToBlocks int) mgl64.Vec3 {
	r := m.Rand()
	center := m.Position()
	pick := func() mgl64.Vec3 {
		return center.Add(mgl64.Vec3{(r.Float64()*2 - 1) * 16, (r.Float64()*2 - 1) * 16, (r.Float64()*2 - 1) * 16})
	}
	var res mgl64.Vec3
	found := false
	for range 64 {
		res = pick()
		if !m.WithinHome(cube.PosFromVec3(res)) {
			found = false
			continue
		}
		found = true
		if goodFlyTarget(m.Tx(), res, distanceToBlocks) {
			return res
		}
	}
	if !found {
		res = pick()
	}
	pos := cube.PosFromVec3(res)
	if h := heightNoLeaves(m.Tx(), pos[0], pos[2]); h < pos[1] && h > m.Tx().Range().Min() {
		res[1] = center[1] - math.Abs(center[1]-res[1])
	}
	return res
}

func goodFlyTarget(tx *world.Tx, t mgl64.Vec3, dist int) bool {
	if dist <= 0 {
		return true
	}
	pos := cube.PosFromVec3(t)
	if blockID(tx.Block(pos)) != "air" {
		return false
	}
	for _, d := range javaDirections {
		for i := 1; i < dist; i++ {
			if blockID(tx.Block(pos.Add(cube.Pos{d[0] * i, d[1] * i, d[2] * i}))) != "air" {
				return true
			}
		}
	}
	return false
}

// ghastLook is Ghast.GhastLookGoal: faceMovementDirection.
type ghastLook struct{ javamob.GoalBase }

func (g *ghastLook) CanUse(*javamob.Mob) bool      { return true }
func (g *ghastLook) RequiresUpdateEveryTick() bool { return true }
func (g *ghastLook) Tick(m *javamob.Mob)           { FaceMovementDirection(m) }

// FaceMovementDirection is Ghast.faceMovementDirection: face the way it
// moves, or its target within 64.
func FaceMovementDirection(m *javamob.Mob) {
	t, ok := m.Target()
	if !ok {
		v := m.Velocity()
		yaw := -math.Atan2(v[0], v[2]) * 180 / math.Pi
		m.SetYRot(yaw)
		m.SetBodyYRot(yaw)
		return
	}
	d := t.Position().Sub(m.Position())
	if d.LenSqr() < 4096 {
		yaw := -math.Atan2(d[0], d[2]) * 180 / math.Pi
		m.SetYRot(yaw)
		m.SetBodyYRot(yaw)
	}
}

// ghastShoot is Ghast.GhastShootFireballGoal.
type ghastShoot struct {
	javamob.GoalBase
	g      *Ghast
	charge int
}

func (s *ghastShoot) RequiresUpdateEveryTick() bool { return true }
func (s *ghastShoot) CanUse(m *javamob.Mob) bool    { _, ok := m.Target(); return ok }
func (s *ghastShoot) Start(*javamob.Mob)            { s.charge = 0 }
func (s *ghastShoot) Stop(m *javamob.Mob)           { setCharging(m, false) }

func (s *ghastShoot) Tick(m *javamob.Mob) {
	t, ok := m.Target()
	if !ok {
		return
	}
	pos := m.Position()
	if t.Position().Sub(pos).LenSqr() < 4096 && m.HasLineOfSight(t) {
		s.charge++
		if s.charge == 10 {
			m.Tx().PlaySound(pos, entitySound{event: "warn", id: "minecraft:ghast"})
		}
		if s.charge == 20 {
			view := m.Rotation().Vec3()
			tp := t.Position()
			mid := pos[1] + m.Height()*0.5
			dir := mgl64.Vec3{tp[0] - (pos[0] + view[0]*4), tp[1] + t.H().Type().BBox(t).Height()*0.5 - (0.5 + mid), tp[2] - (pos[2] + view[2]*4)}
			m.Tx().PlaySound(pos, entitySound{event: "shoot", id: "minecraft:ghast"})
			SpawnHurting(m.Tx(), LargeFireballType, HurtingOpts{
				Owner: m, Pos: mgl64.Vec3{pos[0] + view[0]*4, mid + 0.5, pos[2] + view[2]*4},
				Dir: normalize(dir), ExplosionPower: s.g.ExplosionPower, Incendiary: mobGriefing(m.Tx()),
			})
			s.charge = -40
		}
	} else if s.charge > 0 {
		s.charge--
	}
	setCharging(m, s.charge > 10)
}

// setCharging is Ghast.setCharging (the open mouth): Meta.Charged.
func setCharging(m *javamob.Mob, v bool) {
	if m.Meta().Charged != v {
		m.UpdateMeta(func(meta *mob.Meta) { meta.Charged = v })
	}
}
