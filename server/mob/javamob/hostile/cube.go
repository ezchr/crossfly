//go:build crossfly_full

package hostile

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Cube mob sizes (AbstractCubeMob).
const (
	CubeMinSize        = 1
	CubeMaxSize        = 127
	CubeMaxNaturalSize = 4
)

// CubeKind is what differs between the cube mobs built on AbstractCubeMob:
// slime (here), magma cube (the nether row builds it on this) and sulfur
// cube. The zero values are the AbstractCubeMob defaults.
type CubeKind struct {
	// ID is the entity identifier.
	ID string
	// Width and Height are the EntityType size at size 1 (scaled by the
	// size, AbstractCubeMob.getDefaultDimensions).
	Width, Height float64
	// JumpDelayFactor multiplies getJumpDelay (MagmaCube: 4). 0 is 1.
	JumpDelayFactor int
	// SquishDecay is decreaseSquish's factor (0.6; MagmaCube 0.9). It only
	// matters to the client animation, kept for completeness.
	SquishDecay float64
	// AlwaysDealsDamage: canDealDamage is true even at size 1 (MagmaCube).
	AlwaysDealsDamage bool
	// NeverDealsDamage: canDealDamage is false (SulfurCube).
	NeverDealsDamage bool
	// AttackDamageBonus is added to the attack damage attribute
	// (getAttackDamage; MagmaCube 2).
	AttackDamageBonus float64
	// SizeIsDamageAndXP: setSize also sets the attack damage and the xp
	// reward to the size (Slime, MagmaCube).
	SizeIsDamageAndXP bool
	// OnSetSize runs after AbstractCubeMob.setSize with the clamped size
	// (MagmaCube sets its armour there).
	OnSetSize func(m *mob.Mob, size int)
	// Sounds: Jump/Squish for size >= 2 and their tiny variants (Java sound
	// event names without the minecraft: prefix).
	JumpSound, JumpSoundSmall     string
	SquishSound, SquishSoundSmall string
}

// slimeKind is Slime.
var slimeKind = &CubeKind{
	ID: "minecraft:slime", Width: 0.52, Height: 0.52,
	// Slime.setSize: the attack damage is the size, so is the xp.
	SizeIsDamageAndXP: true,
	JumpSound:         "entity.slime.jump", JumpSoundSmall: "entity.slime.jump_small",
	SquishSound: "entity.slime.squish", SquishSoundSmall: "entity.slime.squish_small",
}

// Cube is the per-mob state of a cube mob (AbstractCubeMob fields and its
// CubeMobMoveControl).
type Cube struct {
	kind        *CubeKind
	size        int
	wasOnGround bool
	xpReward    int
	// targetSquish/squish are only the client animation; the server keeps
	// targetSquish because it is cheap and makes landing observable in tests.
	targetSquish float64

	// CubeMobMoveControl.
	move cubeMoveControl
}

// cubeOf returns the cube state of m (size 1 until set).
func cubeOf(m *mob.Mob) *Cube {
	return stateOf(m, func() *Cube { return &Cube{kind: slimeKind, size: 1} })
}

// NewCube attaches the cube state of kind to m: size 1, the cube move control
// (AbstractCubeMob constructor).
func NewCube(m *mob.Mob, kind *CubeKind) *Cube {
	c := cubeOf(m)
	c.kind = kind
	c.move.yRot = 180 * m.Yaw() / math.Pi // CubeMobMoveControl: the Java constructor's (odd) radians conversion
	m.SetMoveControl(&c.move)
	SetCubeSize(m, c.size, true)
	return c
}

// Size is AbstractCubeMob.getSize.
func (c *Cube) Size() int { return c.size }

// Tiny is AbstractCubeMob.isTiny.
func (c *Cube) Tiny() bool { return c.size <= 1 }

// XPReward is the xp the cube drops (Slime: its size).
func (c *Cube) XPReward() int { return c.xpReward }

// SetCubeSize is AbstractCubeMob.setSize: clamped to 1..127, the box scales
// with it, max health is size² (setCubeMobHealth), the movement speed
// 0.2 + 0.1·size, and the kind's own attributes follow. Both clients show
// the size: Bedrock as minecraft:variant (with the scaled collision box),
// Java as ID_SIZE (javasession mobdata: slime/magma cube size = Variant).
func SetCubeSize(m *mob.Mob, size int, updateHealth bool) {
	c := cubeOf(m)
	size = max(CubeMinSize, min(CubeMaxSize, size))
	c.size = size
	k := c.kind
	m.UpdateMeta(func(meta *mob.Meta) {
		meta.Variant = int32(size)
		meta.Width = k.Width * float64(size)
		meta.Height = k.Height * float64(size)
	})
	m.SetMaxHealth(float64(size * size))
	m.Attributes().SetBase(mob.AttrMovement, 0.2+0.1*float64(size))
	if updateHealth {
		m.SetHealth(m.MaxHealth())
	}
	if k.SizeIsDamageAndXP {
		m.Attributes().SetBase(mob.AttrAttackDamage, float64(size))
		c.xpReward = size
	}
	if k.OnSetSize != nil {
		k.OnSetSize(m, size)
	}
}

// cubeSpawnSize is AbstractCubeMob.setSpawnSize: 1, 2 or 4 with equal odds,
// the two smaller ones bumped up one step with a chance of half the local
// difficulty's special multiplier.
func cubeSpawnSize(r *rand.Rand, specialMultiplier float64) int {
	scale := nextInt(r, 3)
	if scale < 2 && nextFloat(r) < 0.5*specialMultiplier {
		scale++
	}
	return 1 << scale
}

// cubeSplit is one child of AbstractCubeMob.remove: its size and offset
// from the parent.
type cubeSplit struct {
	size   int
	offset mgl64.Vec3
	yaw    float64
}

// cubeSplits is AbstractCubeMob.remove for a dead cube of size > 1: 2 to 4
// (getSplitCount) children of half the size, placed on the corners of a
// square half the parent's width wide, 0.5 higher, each facing a random way.
func cubeSplits(r *rand.Rand, size int, width float64, count int) []cubeSplit {
	if size <= 1 {
		return nil
	}
	half := size / 2
	off := width / 2
	out := make([]cubeSplit, 0, count)
	for i := 0; i < count; i++ {
		xd := (float64(i%2) - 0.5) * off
		zd := (float64(i/2) - 0.5) * off
		out = append(out, cubeSplit{size: half, offset: mgl64.Vec3{xd, 0.5, zd}, yaw: nextFloat(r) * 360})
	}
	return out
}

// cubeSplitCount is AbstractCubeMob.getSplitCount (SulfurCube: always 2).
func cubeSplitCount(r *rand.Rand) int { return 2 + nextInt(r, 3) }

// SplitCube spawns the children of a dead cube when its body is removed
// (AbstractCubeMob.remove with isDeadOrDying): the base calls it as the dead
// mob is closed. Paper adds every child only after all are made (the
// EntityTransformEvent can cancel them all); without plugins that is the
// same.
func SplitCube(m *mob.Mob, spawn func(tx *world.Tx, id string, pos mgl64.Vec3, yaw float64, size int)) {
	c, ok := existingState[Cube](m)
	if !ok || c.size <= 1 {
		return
	}
	pos := m.Pos()
	for _, s := range cubeSplits(m.Rand(), c.size, c.kind.Width*float64(c.size), cubeSplitCount(m.Rand())) {
		spawn(m.Tx(), c.kind.ID, pos.Add(s.offset), s.yaw, s.size)
	}
}

// canDealDamage is AbstractCubeMob.canDealDamage (isEffectiveAi is always
// true on the server for a mob that is not ridden by a player).
func (c *Cube) canDealDamage() bool {
	switch {
	case c.kind.NeverDealsDamage:
		return false
	case c.kind.AlwaysDealsDamage:
		return true
	}
	return !c.Tiny()
}

// attackDamage is AbstractCubeMob.getAttackDamage.
func (c *Cube) attackDamage(m *mob.Mob) float64 {
	return m.Attr(mob.AttrAttackDamage) + c.kind.AttackDamageBonus
}

// jumpDelay is AbstractCubeMob.getJumpDelay: 10..29 ticks (MagmaCube x4).
func (c *Cube) jumpDelay(r *rand.Rand) int {
	d := nextInt(r, 20) + 10
	if f := c.kind.JumpDelayFactor; f > 0 {
		d *= f
	}
	return d
}

// soundVolume is AbstractCubeMob.getSoundVolume.
func (c *Cube) soundVolume() float64 { return 0.4 * float64(c.size) }

// soundPitch is AbstractCubeMob.getSoundPitch.
func (c *Cube) soundPitch(r *rand.Rand) float64 {
	adjust := 0.8
	if c.Tiny() {
		adjust = 1.4
	}
	return ((nextFloat(r)-nextFloat(r))*0.2 + 1) * adjust
}

// TickLanding is the landing part of AbstractCubeMob.tick, run after the
// movement of the tick: landing plays the squish sound (the particles are
// the client's); leaving the ground starts the stretch.
func (c *Cube) TickLanding(m *mob.Mob, play SoundPlayer) {
	onGround := m.OnGround()
	switch {
	case onGround && !c.wasOnGround:
		r := m.Rand()
		snd := c.kind.SquishSound
		if c.Tiny() && c.kind.SquishSoundSmall != "" {
			snd = c.kind.SquishSoundSmall
		}
		play(m, snd, c.soundVolume(), ((nextFloat(r)-nextFloat(r))*0.2+1)/0.8)
		c.targetSquish = -0.5
	case !onGround && c.wasOnGround:
		c.targetSquish = 1
	}
	c.wasOnGround = onGround
	decay := c.kind.SquishDecay
	if decay == 0 {
		decay = 0.6
	}
	c.targetSquish *= decay
}

// EncodeNBT writes AbstractCubeMob.addAdditionalSaveData.
func (c *Cube) EncodeNBT(data map[string]any) {
	data["Size"] = int32(c.size - 1)
	data["wasOnGround"] = boolByte(c.wasOnGround)
}

// DecodeNBT reads AbstractCubeMob.readAdditionalSaveData (setSize(Size+1,
// false) before the health is read back by the base).
func (c *Cube) DecodeNBT(m *mob.Mob, data map[string]any) {
	size := 0
	if v, ok := nbtInt(data, "Size"); ok {
		size = v
	}
	SetCubeSize(m, size+1, false)
	c.wasOnGround = nbtBool(data, "wasOnGround")
}

// cubeMoveOp is MoveControl.Operation as the cube move control uses it.
type cubeMoveOp uint8

const (
	cubeWait cubeMoveOp = iota
	cubeMoveTo
)

// cubeMoveControl is AbstractCubeMob.CubeMobMoveControl: the cube turns to
// a direction and hops that way, with a random delay between hops (a third
// of it while attacking).
type cubeMoveControl struct {
	yRot          float64
	jumpDelay     int
	aggressive    bool
	op            cubeMoveOp
	speedModifier float64
}

// setDirection is CubeMobMoveControl.setDirection.
func (c *cubeMoveControl) setDirection(yRot float64, aggressive bool) {
	c.yRot = yRot
	c.aggressive = aggressive
}

// setWantedMovement is CubeMobMoveControl.setWantedMovement.
func (c *cubeMoveControl) setWantedMovement(speed float64) {
	c.speedModifier = speed
	c.op = cubeMoveTo
}

// Tick is CubeMobMoveControl.tick.
func (c *cubeMoveControl) Tick(m *mob.Mob) {
	yaw := rotlerp(m.Yaw(), c.yRot, 90)
	m.SetYaw(yaw)
	m.SetHeadYaw(yaw)
	m.SetBodyYaw(yaw)
	in := m.Input()
	if c.op != cubeMoveTo {
		in.Forward = 0
		return
	}
	c.op = cubeWait
	if !m.OnGround() {
		in.Speed = c.speedModifier * m.Attr(mob.AttrMovement)
		return
	}
	in.Speed = c.speedModifier * m.Attr(mob.AttrMovement)
	c.jumpDelay--
	if c.jumpDelay+1 <= 0 { // this.jumpDelay-- <= 0
		cube := cubeOf(m)
		c.jumpDelay = cube.jumpDelay(m.Rand())
		if c.aggressive {
			c.jumpDelay /= 3
		}
		m.JumpControl().Jump()
		if cube.size > 0 { // doPlayJumpSound
			snd := cube.kind.JumpSound
			if cube.Tiny() && cube.kind.JumpSoundSmall != "" {
				snd = cube.kind.JumpSoundSmall
			}
			playSound(m, snd, cube.soundVolume(), cube.soundPitch(m.Rand()))
		}
		return
	}
	in.Strafe, in.Forward, in.Speed = 0, 0, 0
}

// SetWantedPosition ... (mob.MoveControl; cubes do not path, a wanted
// position only makes them hop forward).
func (c *cubeMoveControl) SetWantedPosition(_ mgl64.Vec3, speed float64) { c.setWantedMovement(speed) }

// HasWanted ...
func (c *cubeMoveControl) HasWanted() bool { return c.op == cubeMoveTo }

// WantedPosition ...
func (c *cubeMoveControl) WantedPosition() mgl64.Vec3 { return mgl64.Vec3{} }

// Speed ...
func (c *cubeMoveControl) Speed() float64 { return c.speedModifier }

// Strafe ...
func (c *cubeMoveControl) Strafe(float64, float64) {}

// Stop ...
func (c *cubeMoveControl) Stop() { c.op = cubeWait }

// cubeMove returns m's cube move control, if it still has it.
func cubeMove(m *mob.Mob) (*cubeMoveControl, bool) {
	c, ok := m.MoveControl().(*cubeMoveControl)
	return c, ok
}

// cubeAttackGoal is AbstractCubeMob.CubeMobAttackGoal (priority 2, LOOK):
// face the target and hop at it, for at most 300 ticks at a time. Paper: the
// goal also ends when the target died, and stopping clears the target, so
// the cube looks for a new one.
type cubeAttackGoal struct {
	growTiredTimer int
}

func (g *cubeAttackGoal) Flags() mob.ControlFlags { return mob.FlagLook }

func (g *cubeAttackGoal) RequiresUpdateEveryTick() bool { return true }

func (g *cubeAttackGoal) CanUse(m *mob.Mob) bool {
	t, ok := m.Target()
	if !ok || !alive(t) || !canAttack(m, t) {
		return false
	}
	_, ok = cubeMove(m)
	return ok
}

func (g *cubeAttackGoal) Start(*mob.Mob) { g.growTiredTimer = reducedTickDelay(300) }

func (g *cubeAttackGoal) CanContinueToUse(m *mob.Mob) bool {
	t, ok := m.Target()
	if !ok || !alive(t) || !canAttack(m, t) {
		return false
	}
	g.growTiredTimer--
	return g.growTiredTimer > 0
}

// Stop is Paper's addition: the timer resets and the target is dropped.
func (g *cubeAttackGoal) Stop(m *mob.Mob) {
	g.growTiredTimer = 0
	m.SetTarget(nil)
}

func (g *cubeAttackGoal) Tick(m *mob.Mob) {
	if t, ok := m.Target(); ok {
		lookAtEntity(m, t, 10, 10)
	}
	if mc, ok := cubeMove(m); ok {
		mc.setDirection(m.Yaw(), cubeOf(m).canDealDamage())
	}
}

// cubeFloatGoal is AbstractCubeMob.CubeMobFloatGoal (priority 1, JUMP|MOVE):
// in water or lava the cube keeps jumping (80% of ticks) and moves faster.
type cubeFloatGoal struct{}

func (cubeFloatGoal) Flags() mob.ControlFlags { return mob.FlagJump | mob.FlagMove }

func (cubeFloatGoal) RequiresUpdateEveryTick() bool { return true }

func (cubeFloatGoal) CanUse(m *mob.Mob) bool {
	p := m.Physics()
	if !p.InWater && !p.InLava {
		return false
	}
	_, ok := cubeMove(m)
	return ok
}

func (cubeFloatGoal) Tick(m *mob.Mob) {
	if nextFloat(m.Rand()) < 0.8 {
		m.JumpControl().Jump()
	}
	if mc, ok := cubeMove(m); ok {
		mc.setWantedMovement(1.2)
	}
}

// cubeKeepOnJumpingGoal is AbstractCubeMob.CubeMobKeepOnJumpingGoal
// (priority 5, JUMP|MOVE): always hop forward unless riding.
type cubeKeepOnJumpingGoal struct{}

func (cubeKeepOnJumpingGoal) Flags() mob.ControlFlags { return mob.FlagJump | mob.FlagMove }

func (cubeKeepOnJumpingGoal) CanUse(m *mob.Mob) bool { return !m.Riding() }

func (cubeKeepOnJumpingGoal) Tick(m *mob.Mob) {
	if mc, ok := cubeMove(m); ok {
		mc.setWantedMovement(1)
	}
}

// cubeRandomDirectionGoal is AbstractCubeMob.CubeMobRandomDirectionGoal
// (priority 4, LOOK): without a target, pick a new direction every 40..99
// ticks (halved: the goal does not tick every tick).
type cubeRandomDirectionGoal struct {
	chosenDegrees     float64
	nextRandomizeTime int
}

func (g *cubeRandomDirectionGoal) Flags() mob.ControlFlags { return mob.FlagLook }

func (g *cubeRandomDirectionGoal) CanUse(m *mob.Mob) bool {
	if _, ok := m.Target(); ok {
		return false
	}
	p := m.Physics()
	if !m.OnGround() && !p.InWater && !p.InLava && !hasLevitation(m) {
		return false
	}
	_, ok := cubeMove(m)
	return ok
}

func (g *cubeRandomDirectionGoal) Tick(m *mob.Mob) {
	g.nextRandomizeTime--
	if g.nextRandomizeTime <= 0 {
		g.nextRandomizeTime = adjustedTickDelay(false, 40+nextInt(m.Rand(), 60))
		g.chosenDegrees = float64(nextInt(m.Rand(), 360))
	}
	if mc, ok := cubeMove(m); ok {
		mc.setDirection(g.chosenDegrees, false)
	}
}

// cubeContactDamage is AbstractCubeMob.dealDamage, run when a player touches
// the cube (playerTouch) or the cube pushes an iron golem (push): a melee hit
// for the attack damage if the cube may deal damage, is alive, in melee range
// of the target and sees it. It reports whether the hit landed (the attack
// sound then plays).
func cubeContactDamage(m *mob.Mob, target world.Entity) bool {
	c := cubeOf(m)
	if !c.canDealDamage() || m.Dead() || !withinMeleeAttackRange(m, target) || !hasLineOfSight(m, target) {
		return false
	}
	if !mobAttack(m, target, c.attackDamage(m)) {
		return false
	}
	r := m.Rand()
	playSound(m, "entity.slime.attack", 1, (nextFloat(r)-nextFloat(r))*0.2+1)
	return true
}
