//go:build crossfly_full

package jnether

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// The shulker (Shulker, Paper 26.3): it never moves, sitting in its block
// attached to a face; it opens (peek 30) now and then, opens fully (100)
// and fires a shulker bullet every 20-110 ticks at a player within 20 it
// targets, and is armoured (+20) while closed, when arrows bounce off. Hurt
// below half health it teleports with a 1 in 4 chance; hit by a shulker
// bullet while open it teleports and, unless (n-1)/5 of the time with n the
// shulkers within 8 of where it was, leaves a new shulker of its colour
// behind (shulker farms). It teleports when its block goes or its face can
// no longer hold it.

// ShulkerID is the shulker's identifier.
const ShulkerID = "minecraft:shulker"

// shulkerNoColour is DATA_COLOR_ID 16: the default purple shulker.
const shulkerNoColour = 16

func init() {
	javamob.Register(javamob.Type{
		ID: ShulkerID, Category: javamob.CategoryMonster,
		Width: 1, Height: 1, EyeHeight: 0.5,
		FireImmune: true,
		Attributes: func() map[javamob.Attribute]float64 {
			a := javamob.LivingAttributes()
			a[javamob.AttrMaxHealth] = 30
			return a
		},
		Families: []string{"shulker", "monster", "mob"},
		XPReward: 5,
		New:      func() javamob.Behaviour { return &Shulker{face: cube.FaceDown, colour: shulkerNoColour} },
	})
}

// Shulker is the Shulker class.
type Shulker struct {
	face        cube.Face
	peek        int // DATA_PEEK_ID, 0..100
	current     float64
	colour      int // DATA_COLOR_ID, 16 none
	lastBlock   cube.Pos
	initialised bool
}

// RegisterGoals is Shulker.registerGoals.
func (s *Shulker) RegisterGoals(m *javamob.Mob) {
	look := goal.NewLookAtPlayer(8)
	look.Probability, look.OnlyHorizontal = 0.02, true
	m.Goals().Add(1, look)
	m.Goals().Add(4, &shulkerAttack{GoalBase: javamob.GoalBase{Control: javamob.FlagMove | javamob.FlagLook}, s: s})
	m.Goals().Add(7, &shulkerPeek{s: s})
	m.Goals().Add(8, goal.NewRandomLookAround())
	m.Targets().Add(1, goal.NewHurtByTarget(ShulkerID).SetAlertOthers())
	m.Targets().Add(2, &shulkerNearestAttack{GoalBase: javamob.GoalBase{Control: javamob.FlagTarget}, s: s})
	m.UpdateMeta(func(meta *mob.Meta) { meta.NoGravity = true })
	s.setRawPeek(m, s.peek, false)
}

// searchArea is ShulkerNearestAttackGoal.getTargetSearchArea: the follow
// range around, but only 4 along the attach face's axis.
func (s *Shulker) searchArea(m *javamob.Mob, d float64) cube.BBox {
	b := m.BBox()
	switch s.face.Axis() {
	case cube.X:
		return b.GrowVec3(mgl64.Vec3{4, d, d})
	case cube.Z:
		return b.GrowVec3(mgl64.Vec3{d, d, 4})
	}
	return b.GrowVec3(mgl64.Vec3{d, 4, d})
}

// AiStep: a shulker never moves (getDeltaMovement is zero and
// setDeltaMovement does nothing), so whatever pushed it is dropped before
// the base moves it.
func (s *Shulker) AiStep(m *javamob.Mob) {
	m.SetVelocity(mgl64.Vec3{})
}

// FinalizeSpawn is Shulker.finalizeSpawn: facing south, snapped to its
// block.
func (s *Shulker) FinalizeSpawn(m *javamob.Mob, _ javamob.SpawnReason) {
	m.SetYRot(0)
	m.SetHeadYRot(0)
	s.snap(m)
}

// snap is Shulker.setPos: the middle of the block (y rounded).
func (s *Shulker) snap(m *javamob.Mob) {
	p := m.Position()
	m.SetPosition(mgl64.Vec3{math.Floor(p[0]) + 0.5, math.Floor(p[1] + 0.5), math.Floor(p[2]) + 0.5})
}

// Tick is Shulker.tick after super.tick: a new face, or a teleport, when
// it cannot stay; the peek eases towards its target (0.05 a tick).
func (s *Shulker) PostTick(m *javamob.Mob) {
	m.SetVelocity(mgl64.Vec3{})
	s.snap(m)
	bp := cube.PosFromVec3(m.Position())
	if s.initialised && bp != s.lastBlock {
		s.setRawPeek(m, 0, false)
	}
	s.lastBlock, s.initialised = bp, true
	if !m.Riding() && !s.canStayAt(m, bp, s.face) {
		if f, ok := s.attachableSurface(m, bp); ok {
			s.setFace(m, f)
		} else {
			s.teleport(m)
		}
	}
	target := float64(s.peek) * 0.01
	if s.current != target {
		if s.current > target {
			s.current = clamp(s.current-0.05, target, 1)
		} else {
			s.current = clamp(s.current+0.05, 0, target)
		}
		if s.face == cube.FaceDown {
			// getDefaultDimensions: taller by the peek on the floor.
			m.SetDimensions(1, 1+s.current)
		}
	}
}

// canStayAt is Shulker.canStayAt: its block is free, the block on face
// has a full face towards it, and the fully opened shell would not hit a
// block.
func (s *Shulker) canStayAt(m *javamob.Mob, pos cube.Pos, face cube.Face) bool {
	tx := m.Tx()
	if b := tx.Block(pos); blockID(b) != "air" && !(blockID(b) == "moving_block" && pos == cube.PosFromVec3(m.Position())) {
		return false
	}
	on := pos.Side(face)
	if !tx.Block(on).Model().FaceSolid(on, face.Opposite(), tx) {
		return false
	}
	open := progressBox(face.Opposite(), 1, pos.Vec3Centre().Sub(mgl64.Vec3{0, 0.5, 0})).Grow(-1e-6)
	return !collides(tx, open, cube.BBox{}) && !shulkerAt(tx, open, m.H())
}

// shulkerAt reports whether another shulker's box meets b (shulkers are
// solid to each other: canBeCollidedWith).
func shulkerAt(tx *world.Tx, b cube.BBox, self *world.EntityHandle) bool {
	for e := range tx.EntitiesWithin(b.Grow(2)) {
		if e.H() != self && e.H().Type().EncodeEntity() == ShulkerID &&
			e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(b) {
			return true
		}
	}
	return false
}

// progressBox is Shulker.getProgressAabb(1, direction, progress, pos).
func progressBox(dir cube.Face, progress float64, pos mgl64.Vec3) cube.BBox {
	b := cube.Box(-0.5, 0, -0.5, 0.5, 1, 0.5)
	step := cube.Pos{}.Side(dir).Vec3()
	b = b.Extend(step.Mul(progress))
	return b.Translate(pos)
}

// attachableSurface is findAttachableSurface: the first face (Java
// Direction order) it can stay on.
func (s *Shulker) attachableSurface(m *javamob.Mob, pos cube.Pos) (cube.Face, bool) {
	for _, f := range []cube.Face{cube.FaceDown, cube.FaceUp, cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast} {
		if s.canStayAt(m, pos, f) {
			return f, true
		}
	}
	return 0, false
}

func (s *Shulker) setFace(m *javamob.Mob, f cube.Face) {
	s.face = f
	m.SetActorData(protocol.EntityDataKeyAttachFace, byte(f))
}

// teleport is Shulker.teleportSomewhere: 5 tries at a spot within 8 on
// each axis that is air, free and has a face to attach to.
func (s *Shulker) teleport(m *javamob.Mob) bool {
	if m.NoAI() || m.Dead() {
		return false
	}
	tx := m.Tx()
	old := cube.PosFromVec3(m.Position())
	r := m.Rand()
	for range 5 {
		t := old.Add(cube.Pos{r.IntN(17) - 8, r.IntN(17) - 8, r.IntN(17) - 8})
		if t[1] <= tx.Range().Min() || t.OutOfBounds(tx.Range()) {
			continue
		}
		if _, ok := tx.Block(t).(block.Air); !ok {
			continue
		}
		box := cube.Box(float64(t[0]), float64(t[1]), float64(t[2]), float64(t[0]+1), float64(t[1]+1), float64(t[2]+1)).Grow(-1e-6)
		if collides(tx, box, cube.BBox{}) || shulkerAt(tx, box, m.H()) {
			continue
		}
		f, ok := s.attachableSurface(m, t)
		if !ok {
			continue
		}
		s.setFace(m, f)
		m.PlaySound("teleport")
		m.Teleport(mgl64.Vec3{float64(t[0]) + 0.5, float64(t[1]), float64(t[2]) + 0.5})
		s.setRawPeek(m, 0, false)
		m.SetTarget(nil)
		return true
	}
	return false
}

// closed is isClosed.
func (s *Shulker) closed() bool { return s.peek == 0 }

// setRawPeek is setRawPeekAmount: +20 armour when closed; the open/close
// sound.
func (s *Shulker) setRawPeek(m *javamob.Mob, n int, sound bool) {
	armour := m.Attributes().Instance(javamob.AttrArmor)
	armour.RemoveModifier("minecraft:covered")
	if n == 0 {
		armour.AddModifier(javamob.Modifier{ID: "minecraft:covered", Amount: 20, Op: javamob.OpAddValue})
	}
	m.SyncAttributes()
	if sound {
		if n == 0 {
			m.PlaySound("shulker.close")
		} else {
			m.PlaySound("shulker.open")
		}
	}
	s.peek = n
	m.SetActorData(protocol.EntityDataKeyPeekID, int32(n))
}

// FilterDamage: arrows bounce off a closed shulker.
func (s *Shulker) FilterDamage(m *javamob.Mob, d *mob.Damage) {
	if fireImmune(d); d.Cancel {
		return
	}
	if !s.closed() {
		return
	}
	if p, ok := d.Source.(entity.ProjectileDamageSource); ok && p.Projectile != nil {
		switch p.Projectile.H().Type().EncodeEntity() {
		case "minecraft:arrow", "minecraft:thrown_trident":
			d.Cancel = true
		}
	}
}

// OnHurt is the rest of Shulker.hurtServer: below half health a 1 in 4
// teleport, else a shulker bullet's hit may duplicate it.
func (s *Shulker) OnHurt(m *javamob.Mob, _ float64, src world.DamageSource) {
	if m.Health() < m.MaxHealth()*0.5 && m.Rand().IntN(4) == 0 {
		s.teleport(m)
		return
	}
	if p, ok := src.(entity.ProjectileDamageSource); ok && p.Projectile != nil && p.Projectile.H().Type() == ShulkerBulletType {
		s.hitByShulkerBullet(m)
	}
}

// hitByShulkerBullet is Shulker.hitByShulkerBullet.
func (s *Shulker) hitByShulkerBullet(m *javamob.Mob) {
	oldPos, oldBox := m.Position(), m.BBox()
	if s.closed() || !s.teleport(m) {
		return
	}
	tx := m.Tx()
	n := 0
	box := oldBox.Grow(8)
	for e := range tx.EntitiesWithin(box.Grow(1)) {
		if e.H().Type().EncodeEntity() != ShulkerID {
			continue
		}
		if l, ok := e.(entity.Living); ok && l.Dead() {
			continue
		}
		if e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
			n++
		}
	}
	if !ShulkerDuplicates(n, m.Rand().Float32()) {
		return
	}
	colour := s.colour
	_, _ = javamob.Spawn(tx, ShulkerID, oldPos, javamob.SpawnBreeding, javamob.SpawnOptions{
		NoFinalize: true,
		Configure: func(baby *javamob.Mob) {
			if b, ok := baby.Behaviour().(*Shulker); ok {
				b.SetColour(baby, colour)
			}
		},
	})
}

// SetColour sets the colour (DATA_COLOR_ID: 0 white .. 15 black, 16 none).
func (s *Shulker) SetColour(m *javamob.Mob, c int) {
	s.colour = c
	v := int32(16)
	if c >= 0 && c < 16 {
		v = int32(15 - c)
	}
	m.UpdateMeta(func(meta *mob.Meta) { meta.Variant = v })
}

// Persister: AttachFace, Peek, Color.
func (s *Shulker) EncodeNBT(data map[string]any) {
	data["AttachFace"] = byte(s.face)
	data["Peek"] = byte(s.peek)
	data["Color"] = byte(s.colour)
}

func (s *Shulker) DecodeNBT(m *javamob.Mob, data map[string]any) {
	if v, ok := data["AttachFace"].(byte); ok && v < 6 {
		s.setFace(m, cube.Face(v))
	}
	if v, ok := data["Peek"].(byte); ok {
		s.setRawPeek(m, int(v), false)
	}
	if v, ok := data["Color"].(byte); ok {
		s.SetColour(m, int(v))
	}
}

// shulkerAttack is Shulker.ShulkerAttackGoal.
type shulkerAttack struct {
	javamob.GoalBase
	s          *Shulker
	attackTime int
}

func (g *shulkerAttack) RequiresUpdateEveryTick() bool { return true }
func (g *shulkerAttack) CanUse(m *javamob.Mob) bool {
	t, ok := m.Target()
	return ok && alive(t) && m.DifficultyID() != 0
}
func (g *shulkerAttack) Start(m *javamob.Mob) { g.attackTime = 20; g.s.setRawPeek(m, 100, true) }
func (g *shulkerAttack) Stop(m *javamob.Mob)  { g.s.setRawPeek(m, 0, true) }
func (g *shulkerAttack) Tick(m *javamob.Mob) {
	if m.DifficultyID() == 0 {
		return
	}
	g.attackTime--
	t, ok := m.Target()
	if !ok {
		return
	}
	m.LookControl().SetLookAtEntity(t, 180, 180)
	if t.Position().Sub(m.Position()).LenSqr() >= 400 {
		m.SetTarget(nil)
		return
	}
	if g.attackTime <= 0 {
		r := m.Rand()
		g.attackTime = 20 + r.IntN(10)*20/2
		SpawnShulkerBullet(m.Tx(), m, t, g.s.face.Axis())
		m.PlaySound("shoot")
	}
}

// shulkerPeek is Shulker.ShulkerPeekGoal: without a target, 1 in 40
// (reducedTickDelay 20) a tick, open to 30 for 20-60 ticks.
type shulkerPeek struct {
	javamob.GoalBase
	s    *Shulker
	time int
}

func (g *shulkerPeek) CanUse(m *javamob.Mob) bool {
	_, has := m.Target()
	return !has && m.Rand().IntN(javamob.ReducedTickDelay(40)) == 0 &&
		g.s.canStayAt(m, cube.PosFromVec3(m.Position()), g.s.face)
}
func (g *shulkerPeek) CanContinueToUse(m *javamob.Mob) bool {
	_, has := m.Target()
	return !has && g.time > 0
}
func (g *shulkerPeek) Start(m *javamob.Mob) {
	g.time = javamob.AdjustedTickDelay(g, 20*(1+m.Rand().IntN(3)))
	g.s.setRawPeek(m, 30, true)
}
func (g *shulkerPeek) Stop(m *javamob.Mob) {
	if _, has := m.Target(); !has {
		g.s.setRawPeek(m, 0, true)
	}
}
func (g *shulkerPeek) Tick(*javamob.Mob) { g.time-- }

// shulkerNearestAttack is Shulker.ShulkerNearestAttackGoal: a player
// within the follow range (16), only 4 along the attach face's axis, in
// sight; not on peaceful.
type shulkerNearestAttack struct {
	javamob.GoalBase
	s      *Shulker
	target *world.EntityHandle
}

func (g *shulkerNearestAttack) CanUse(m *javamob.Mob) bool {
	if m.DifficultyID() == 0 || m.Rand().IntN(javamob.ReducedTickDelay(10)) != 0 {
		return false
	}
	box := g.s.searchArea(m, m.FollowRange())
	e, ok := m.NearestEntity(box, goal.IsPlayer, javamob.ForCombat(m.FollowRange()))
	if !ok {
		return false
	}
	g.target = e.H()
	return true
}

func (g *shulkerNearestAttack) Start(m *javamob.Mob) {
	if e, ok := g.target.Entity(m.Tx()); ok {
		m.SetTarget(e)
	}
}

func (g *shulkerNearestAttack) CanContinueToUse(m *javamob.Mob) bool {
	t, ok := m.Target()
	if !ok || !alive(t) || !targetable(t) {
		return false
	}
	return t.Position().Sub(m.Position()).LenSqr() <= m.FollowRange()*m.FollowRange()
}

func (g *shulkerNearestAttack) Stop(m *javamob.Mob) { m.SetTarget(nil); g.target = nil }
