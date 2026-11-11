//go:build crossfly_full

package hostile

import (
	"math"
	"slices"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

const phantomID = "minecraft:phantom"

// phantomAttackPhase is Phantom.AttackPhase.
type phantomAttackPhase uint8

const (
	phantomCircle phantomAttackPhase = iota
	phantomSwoop
)

// Phantom is the per-mob state of a phantom (Phantom).
type Phantom struct {
	size            int
	moveTargetPoint mgl64.Vec3
	anchor          *cube.Pos
	phase           phantomAttackPhase
	// move is the PhantomMoveControl's speed.
	moveSpeed float64
}

// phantomOf returns the phantom state of m.
func phantomOf(m *mob.Mob) *Phantom {
	return stateOf(m, func() *Phantom { return &Phantom{moveSpeed: 0.1} })
}

// Size is Phantom.getPhantomSize (0..64).
func (p *Phantom) Size() int { return p.size }

// SetPhantomSize is Phantom.setPhantomSize and updatePhantomSizeInfo: the
// box grows 15% per size step (0.9 x 0.5 at size 0) and the attack damage is
// 6 + size. Both clients get the size through the scale.
func SetPhantomSize(m *mob.Mob, size int) {
	size = max(0, min(64, size))
	phantomOf(m).size = size
	scale := 1 + 0.15*float64(size)
	m.UpdateMeta(func(meta *mob.Meta) {
		meta.Width, meta.Height = 0.9, 0.5
		meta.Scale = scale
	})
	m.Attributes().SetBase(mob.AttrAttackDamage, float64(6+size))
	if jm, ok := javamob.Of(m); ok {
		jm.SetDimensions(0.9*scale, 0.5*scale)
		jm.Attributes().Instance(javamob.AttrAttackDamage).SetBase(float64(6 + size))
	}
}

// PhantomFinalizeSpawn is Phantom.finalizeSpawn: the anchor is 5 blocks above
// where it spawned, size 0.
func PhantomFinalizeSpawn(m *mob.Mob) {
	p := phantomOf(m)
	a := blockPos(m.Pos()).Add(cube.Pos{0, 5, 0})
	p.anchor = &a
	SetPhantomSize(m, 0)
}

// EncodeNBT writes Phantom.addAdditionalSaveData (anchor_pos, size). Paper's
// Paper.SpawningEntity/ShouldBurnInDay are left out: their defaults are what
// a server without plugins has.
func (p *Phantom) EncodeNBT(data map[string]any) {
	if p.anchor != nil {
		data["anchor_pos"] = []int32{int32(p.anchor[0]), int32(p.anchor[1]), int32(p.anchor[2])}
	}
	data["size"] = int32(p.size)
}

// DecodeNBT reads Phantom.readAdditionalSaveData.
func (p *Phantom) DecodeNBT(m *mob.Mob, data map[string]any) {
	if a, ok := data["anchor_pos"].([]int32); ok && len(a) == 3 {
		pos := cube.Pos{int(a[0]), int(a[1]), int(a[2])}
		p.anchor = &pos
	}
	s, _ := nbtInt(data, "size")
	SetPhantomSize(m, s)
}

// phantomTargetGoal is Phantom.PhantomAttackPlayerTargetGoal (target
// selector priority 1): every 3 s (first after 1 s) the highest player in a
// box 16 wide and 64 tall around the phantom that may be attacked. Paper
// (phantoms-only-attack-insomniacs, default true): only players who have not
// slept for 72000 ticks.
type phantomTargetGoal struct {
	nextScanTick int
}

func newPhantomTargetGoal() *phantomTargetGoal {
	return &phantomTargetGoal{nextScanTick: reducedTickDelay(20)}
}

func (g *phantomTargetGoal) Flags() mob.ControlFlags { return 0 }

func (g *phantomTargetGoal) IsTargetGoal() bool { return true }

func (g *phantomTargetGoal) CanUse(m *mob.Mob) bool {
	if g.nextScanTick > 0 {
		g.nextScanTick--
		return false
	}
	g.nextScanTick = reducedTickDelay(60)
	box := m.BBox().GrowVec3(mgl64.Vec3{16, 64, 16})
	var players []*player.Player
	for e := range m.Tx().EntitiesWithin(box) {
		if pl, ok := e.(*player.Player); ok && (Targeting{Combat: true, Range: 64}).Test(m, pl) {
			players = append(players, pl)
		}
	}
	// Highest first; Java's sort is stable.
	slices.SortStableFunc(players, func(a, b *player.Player) int {
		ya, yb := a.Position()[1], b.Position()[1]
		switch {
		case ya > yb:
			return -1
		case ya < yb:
			return 1
		}
		return 0
	})
	for _, pl := range players {
		if (Targeting{Combat: true}).Test(m, pl) && insomniac(pl) {
			m.SetTarget(pl)
			return true
		}
	}
	return false
}

func (g *phantomTargetGoal) CanContinueToUse(m *mob.Mob) bool {
	t, ok := m.Target()
	return ok && (Targeting{Combat: true}).Test(m, t)
}

// insomniacStartTicks is Paper's player-insomnia-start-ticks (72000, as
// vanilla's PhantomSpawner).
const insomniacStartTicks = 72000

// insomniac is Paper's EntitySelector.IS_INSOMNIAC: the player has not slept
// (or died) for insomniacStartTicks.
func insomniac(e world.Entity) bool {
	r, ok := e.(interface{ TimeSinceRest() int64 })
	if !ok {
		return false
	}
	return max(r.TimeSinceRest(), 1) >= insomniacStartTicks
}

// phantomStrategyGoal is Phantom.PhantomAttackStrategyGoal (priority 1, no
// flags): with a target, circle above it, then swoop every 8..11 s.
type phantomStrategyGoal struct {
	nextSweepTick int
}

func (g *phantomStrategyGoal) Flags() mob.ControlFlags { return 0 }

func (g *phantomStrategyGoal) CanUse(m *mob.Mob) bool {
	t, ok := m.Target()
	return ok && (Targeting{Combat: true}).Test(m, t)
}

func (g *phantomStrategyGoal) Start(m *mob.Mob) {
	g.nextSweepTick = adjustedTickDelay(false, 10)
	phantomOf(m).phase = phantomCircle
	setAnchorAboveTarget(m)
}

// Stop moves the anchor 10..29 blocks above the ground under it.
func (g *phantomStrategyGoal) Stop(m *mob.Mob) {
	p := phantomOf(m)
	if p.anchor == nil {
		return
	}
	a := *p.anchor
	h := m.Tx().HighestBlock(a[0], a[2]) + 1 // getHeightmapPos(MOTION_BLOCKING)
	a[1] = h + 10 + nextInt(m.Rand(), 20)
	p.anchor = &a
}

func (g *phantomStrategyGoal) Tick(m *mob.Mob) {
	p := phantomOf(m)
	if p.phase != phantomCircle {
		return
	}
	g.nextSweepTick--
	if g.nextSweepTick > 0 {
		return
	}
	p.phase = phantomSwoop
	setAnchorAboveTarget(m)
	g.nextSweepTick = adjustedTickDelay(false, (8+nextInt(m.Rand(), 4))*20)
	playSound(m, "entity.phantom.swoop", 10, 0.95+nextFloat(m.Rand())*0.1)
}

// seaLevel is the overworld sea level (Crossfly's worlds use vanilla's 63).
var seaLevel = 63

// setAnchorAboveTarget is PhantomAttackStrategyGoal.setAnchorAboveTarget:
// 20..39 blocks above the target, not below sea level + 1.
func setAnchorAboveTarget(m *mob.Mob) {
	p := phantomOf(m)
	t, ok := m.Target()
	if p.anchor == nil || !ok {
		return
	}
	a := blockPos(t.Position()).Add(cube.Pos{0, 20 + nextInt(m.Rand(), 20), 0})
	if a[1] < seaLevel {
		a[1] = seaLevel + 1
	}
	p.anchor = &a
}

// phantomCircleGoal is Phantom.PhantomCircleAroundAnchorGoal (priority 3,
// MOVE): fly in circles 5..15 blocks around the anchor, changing height,
// radius and direction now and then, and around blocks in the way.
type phantomCircleGoal struct {
	angle, distance, height, clockwise float64
}

func (g *phantomCircleGoal) Flags() mob.ControlFlags { return mob.FlagMove }

func (g *phantomCircleGoal) CanUse(m *mob.Mob) bool {
	_, ok := m.Target()
	return !ok || phantomOf(m).phase == phantomCircle
}

func (g *phantomCircleGoal) Start(m *mob.Mob) {
	r := m.Rand()
	g.distance = 5 + nextFloat(r)*10
	g.height = -4 + nextFloat(r)*9
	g.clockwise = -1
	if nextBool(r) {
		g.clockwise = 1
	}
	g.selectNext(m)
}

func (g *phantomCircleGoal) Tick(m *mob.Mob) {
	r := m.Rand()
	p := phantomOf(m)
	if nextInt(r, adjustedTickDelay(false, 350)) == 0 {
		g.height = -4 + nextFloat(r)*9
	}
	if nextInt(r, adjustedTickDelay(false, 250)) == 0 {
		g.distance++
		if g.distance > 15 {
			g.distance = 5
			g.clockwise = -g.clockwise
		}
	}
	if nextInt(r, adjustedTickDelay(false, 450)) == 0 {
		g.angle = nextFloat(r) * 2 * math.Pi
		g.selectNext(m)
	}
	if touchingTarget(m) {
		g.selectNext(m)
	}
	pos := m.Pos()
	bp := blockPos(pos)
	tx := m.Tx()
	if p.moveTargetPoint[1] < pos[1] && !isAir(tx, bp.Side(cube.FaceDown)) {
		g.height = math.Max(1, g.height)
		g.selectNext(m)
	}
	if p.moveTargetPoint[1] > pos[1] && !isAir(tx, bp.Side(cube.FaceUp)) {
		g.height = math.Min(-1, g.height)
		g.selectNext(m)
	}
}

func (g *phantomCircleGoal) selectNext(m *mob.Mob) {
	p := phantomOf(m)
	if p.anchor == nil {
		a := blockPos(m.Pos())
		p.anchor = &a
	}
	g.angle += g.clockwise * 15 * math.Pi / 180
	a := p.anchor.Vec3()
	p.moveTargetPoint = a.Add(mgl64.Vec3{g.distance * math.Cos(g.angle), -4 + g.height, g.distance * math.Sin(g.angle)})
}

// touchingTarget is PhantomMoveTargetGoal.touchingTarget.
func touchingTarget(m *mob.Mob) bool {
	return phantomOf(m).moveTargetPoint.Sub(m.Pos()).LenSqr() < 4
}

// isAir is Level.isEmptyBlock: air with no liquid in it.
func isAir(tx *world.Tx, pos cube.Pos) bool {
	if _, ok := tx.Block(pos).(block.Air); !ok {
		return false
	}
	_, liquid := tx.Liquid(pos)
	return !liquid
}

// phantomSweepGoal is Phantom.PhantomSweepAttackGoal (priority 2, MOVE): dive
// at the target and bite it, then circle again; give up on a creative or
// spectator player, a dead target, when hurt or blocked, or when a cat is
// near (they hiss at it).
type phantomSweepGoal struct {
	scaredOfCat   bool
	catSearchTick int64
}

func (g *phantomSweepGoal) Flags() mob.ControlFlags { return mob.FlagMove }

func (g *phantomSweepGoal) CanUse(m *mob.Mob) bool {
	_, ok := m.Target()
	return ok && phantomOf(m).phase == phantomSwoop
}

func (g *phantomSweepGoal) CanContinueToUse(m *mob.Mob) bool {
	t, ok := m.Target()
	if !ok || !alive(t) || creativeOrSpectator(t) || !g.CanUse(m) {
		return false
	}
	if tickCount(m) > g.catSearchTick {
		g.catSearchTick = tickCount(m) + 20
		cats := 0
		for e := range m.Tx().EntitiesWithin(m.BBox().Grow(16)) {
			if entityID(e) == "minecraft:cat" && alive(e) {
				cats++
				CatHiss(e)
			}
		}
		g.scaredOfCat = cats > 0
	}
	return !g.scaredOfCat
}

// CatHiss is Cat.hiss (the pets row's cat); the base or that row sets it.
var CatHiss = func(cat world.Entity) {}

func (g *phantomSweepGoal) Stop(m *mob.Mob) {
	m.SetTarget(nil)
	phantomOf(m).phase = phantomCircle
}

func (g *phantomSweepGoal) Tick(m *mob.Mob) {
	t, ok := m.Target()
	if !ok {
		return
	}
	p := phantomOf(m)
	tp := t.Position()
	tb := targetBox(t)
	p.moveTargetPoint = mgl64.Vec3{tp[0], tp[1] + (tb.Max()[1]-tb.Min()[1])*0.5, tp[2]}
	if m.BBox().Grow(0.2).IntersectsWith(tb) {
		doHurtTarget(m, t)
		p.phase = phantomCircle
		// Level event 1039 (PHANTOM_BITE): the bite sound.
		PlayLevelEvent(m.Tx(), blockPos(m.Pos()), levelEventPhantomBite, 0)
	} else if horizontalCollision(m) || Hooks.HurtRecently(m) {
		p.phase = phantomCircle
	}
}

// levelEventPhantomBite is LevelEvent.SOUND_PHANTOM_BITE.
const levelEventPhantomBite = 1039

// PlayLevelEvent plays a Java level event at pos for both editions (the base
// sets it).
var PlayLevelEvent = func(tx *world.Tx, pos cube.Pos, event, data int) {}

// targetBox is the bounding box of an entity in the world.
func targetBox(e world.Entity) cube.BBox { return e.H().Type().BBox(e).Translate(e.Position()) }

// phantomMoveControl is Phantom.PhantomMoveControl: fly towards the move
// target point, turning at most 4° a tick; the speed builds up to 1.8 while
// flying straight and drops to 0.2 in turns; a wall makes it turn round.
type phantomMoveControl struct{}

func (phantomMoveControl) SetWantedPosition(mgl64.Vec3, float64) {}
func (phantomMoveControl) HasWanted() bool                       { return false }
func (phantomMoveControl) WantedPosition() mgl64.Vec3            { return mgl64.Vec3{} }
func (phantomMoveControl) Speed() float64                        { return 0 }
func (phantomMoveControl) Strafe(float64, float64)               {}
func (phantomMoveControl) Stop()                                 {}

func (phantomMoveControl) Tick(m *mob.Mob) {
	p := phantomOf(m)
	if horizontalCollision(m) {
		m.SetYaw(m.Yaw() + 180)
		p.moveSpeed = 0.1
	}
	pos := m.Pos()
	tdx := p.moveTargetPoint[0] - pos[0]
	tdy := p.moveTargetPoint[1] - pos[1]
	tdz := p.moveTargetPoint[2] - pos[2]
	sd := math.Sqrt(tdx*tdx + tdz*tdz)
	if math.Abs(sd) <= 1e-5 {
		return
	}
	yRelativeScale := 1 - math.Abs(tdy*0.7)/sd
	tdx *= yRelativeScale
	tdz *= yRelativeScale
	sd = math.Sqrt(tdx*tdx + tdz*tdz)
	sd2 := math.Sqrt(tdx*tdx + tdz*tdz + tdy*tdy)
	prev := m.Yaw()
	angle := math.Atan2(tdz, tdx)
	a := wrapDegrees(m.Yaw() + 90)
	b := wrapDegrees(angle * 180 / math.Pi)
	m.SetYaw(approachDegrees(a, b, 4) - 90)
	m.SetBodyYaw(m.Yaw())
	if degreesDifferenceAbs(prev, m.Yaw()) < 3 {
		p.moveSpeed = approach(p.moveSpeed, 1.8, 0.005*(1.8/p.moveSpeed))
	} else {
		p.moveSpeed = approach(p.moveSpeed, 0.2, 0.025)
	}
	xRotD := -(math.Atan2(-tdy, sd) * 180 / math.Pi)
	m.SetPitch(xRotD)
	moveAngle := (m.Yaw() + 90) * math.Pi / 180
	txd := p.moveSpeed * math.Cos(moveAngle) * math.Abs(tdx/sd2)
	tzd := p.moveSpeed * math.Sin(moveAngle) * math.Abs(tdz/sd2)
	tyd := p.moveSpeed * math.Sin(xRotD*math.Pi/180) * math.Abs(tdy/sd2)
	v := m.Vel()
	m.SetVelocity(v.Add(mgl64.Vec3{txd, tyd, tzd}.Sub(v).Mul(0.2)))
}

// phantomLookControl is Phantom.PhantomLookControl: it does nothing (the
// move control sets the rotation).
type phantomLookControl struct{}

func (phantomLookControl) Tick(*mob.Mob)                                  {}
func (phantomLookControl) SetLookAt(mgl64.Vec3, float64, float64)         {}
func (phantomLookControl) SetLookAtEntity(world.Entity, float64, float64) {}
func (phantomLookControl) HasWanted() bool                                { return false }

// phantomBodyControl is Phantom.PhantomBodyRotationControl: the head takes
// the body's last yaw and the body the facing.
type phantomBodyControl struct{}

func (phantomBodyControl) Tick(m *mob.Mob) {
	m.SetHeadYaw(m.BodyYaw())
	m.SetBodyYaw(m.Yaw())
}

// PhantomTravel is Phantom.travel: LivingEntity.travelFlying(input, 0.2):
// 0.02 of the input with 0.8 drag in water, 0.02 and 0.5 in lava, 0.2 and
// 0.91 in the air; no gravity.
func PhantomTravel(m *mob.Mob, input mgl64.Vec3) {
	speed, drag := 0.2, 0.91
	switch {
	case inWater(m):
		speed, drag = 0.02, 0.8
	case inLava(m):
		speed, drag = 0.02, 0.5
	}
	v := m.Vel().Add(inputVector(input, speed, m.Yaw()))
	m.SetVelocity(Hooks.Move(m, v).Mul(drag))
}

// PhantomXP is Phantom's xp reward.
const PhantomXP = 5
