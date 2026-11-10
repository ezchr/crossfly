//go:build crossfly_full

package hostile

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/player"
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
	// size, AbstractCubeMob.getDefaultDimensions), EyeHeight its eye height.
	Width, Height, EyeHeight float64
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
	OnSetSize func(m *javamob.Mob, size int)
	// Sounds: Jump/Squish for size >= 2 and their tiny variants (Java sound
	// event names without the minecraft: prefix).
	JumpSound, JumpSoundSmall     string
	SquishSound, SquishSoundSmall string
}

// slimeKind is Slime.
var slimeKind = &CubeKind{
	ID: "minecraft:slime", Width: 0.52, Height: 0.52, EyeHeight: 0.325,
	// Slime.setSize: the attack damage is the size, so is the xp.
	SizeIsDamageAndXP: true,
	JumpSound:         "entity.slime.jump", JumpSoundSmall: "entity.slime.jump_small",
	SquishSound: "entity.slime.squish", SquishSoundSmall: "entity.slime.squish_small",
}

func init() {
	javamob.Register(javamob.Type{
		ID: slimeKind.ID, Category: javamob.CategoryMonster,
		Width: slimeKind.Width, Height: slimeKind.Height, EyeHeight: slimeKind.EyeHeight,
		NotInPeaceful: true,
		Families:      []string{"slime", "monster", "mob"},
		Attributes:    javamob.MonsterAttributes,
		New:           func() javamob.Behaviour { return NewCubeMob(slimeKind) },
	})
}

// Cube is the state of a cube mob (AbstractCubeMob's fields and its
// CubeMobMoveControl).
type Cube struct {
	kind        *CubeKind
	size        int
	wasOnGround bool
	xpReward    int
	// targetSquish is only the client animation; kept because landing makes
	// it observable in tests.
	targetSquish float64
	// deathTicks counts the ticks since death (the body is removed after
	// 20, when it splits).
	deathTicks int
	split      bool
	// presized: the size was set before finalizeSpawn (a split child:
	// Java's conversion does not run finalizeSpawn, so it keeps its size).
	presized bool

	move cubeMoveControl
}

// CubeMob is the Behaviour of a cube mob: AbstractCubeMob with Slime's goals
// (the magma cube is the same with its own CubeKind).
type CubeMob struct {
	Cube
	// BehaviourGoals, if set, replaces addBehaviourGoals (the attack goal);
	// TargetingGoals replaces addTargetingGoals.
	BehaviourGoals func(c *CubeMob, m *javamob.Mob)
	TargetingGoals func(c *CubeMob, m *javamob.Mob)
}

// NewCubeMob returns the behaviour of a cube mob of the kind, size 1.
func NewCubeMob(kind *CubeKind) *CubeMob { return &CubeMob{Cube: Cube{kind: kind, size: 1}} }

// cubeOf returns the cube state of a cube mob.
func cubeOf(m *javamob.Mob) (*Cube, bool) {
	c, ok := m.Behaviour().(interface{ cubeState() *Cube })
	if !ok {
		return nil, false
	}
	return c.cubeState(), true
}

func (c *Cube) cubeState() *Cube { return c }

// RegisterGoals is AbstractCubeMob.registerGoals, the constructor's move
// control and Slime's addBehaviourGoals/addTargetingGoals.
func (c *CubeMob) RegisterGoals(m *javamob.Mob) {
	c.move.yRot = 180 * m.Yaw() / math.Pi // the Java constructor's (odd) radians conversion
	m.MoveControl().Custom = func(m *javamob.Mob, _ *javamob.MoveControl) { c.move.tick(m, &c.Cube) }
	c.SetSize(m, c.size, true)
	m.Goals().Add(1, &cubeFloatGoal{javamob.GoalBase{Control: javamob.FlagJump | javamob.FlagMove}, c})
	m.Goals().Add(4, &cubeRandomDirectionGoal{GoalBase: javamob.GoalBase{Control: javamob.FlagLook}, c: c})
	m.Goals().Add(5, &cubeKeepOnJumpingGoal{javamob.GoalBase{Control: javamob.FlagJump | javamob.FlagMove}, c})
	if c.BehaviourGoals != nil {
		c.BehaviourGoals(c, m)
	} else {
		m.Goals().Add(2, &cubeAttackGoal{GoalBase: javamob.GoalBase{Control: javamob.FlagLook}, c: c})
	}
	if c.TargetingGoals != nil {
		c.TargetingGoals(c, m)
		return
	}
	// Slime.addTargetingGoals: players no more than 4 blocks above or below,
	// then iron golems.
	players := goal.NewNearestAttackableTarget(goal.IsPlayer, true, false)
	players.Selector = func(e world.Entity, _ *world.Tx) bool { return math.Abs(e.Position()[1]-m.Position()[1]) <= 4 }
	m.Targets().Add(1, players)
	m.Targets().Add(3, goal.NewNearestAttackableTarget(goal.IsType("minecraft:iron_golem"), true, false))
}

// Size is AbstractCubeMob.getSize.
func (c *Cube) Size() int { return c.size }

// Tiny is AbstractCubeMob.isTiny.
func (c *Cube) Tiny() bool { return c.size <= 1 }

// SetSize is AbstractCubeMob.setSize: clamped to 1..127, the box scales with
// it, max health is size² (setCubeMobHealth), the movement speed
// 0.2 + 0.1·size, and the kind's own attributes follow. Both clients show the
// size: Bedrock as minecraft:variant with the scaled box, Java as ID_SIZE
// (javasession: slime/magma cube size = Variant).
func (c *Cube) SetSize(m *javamob.Mob, size int, updateHealth bool) {
	size = max(CubeMinSize, min(CubeMaxSize, size))
	c.size = size
	k := c.kind
	m.SetDimensions(k.Width*float64(size), k.Height*float64(size))
	m.UpdateMeta(func(meta *mob.Meta) { meta.Variant = int32(size) })
	attrs := m.Attributes()
	attrs.Instance(javamob.AttrMaxHealth).SetBase(float64(size * size))
	attrs.Instance(javamob.AttrMovementSpeed).SetBase(float64(float32(0.2 + 0.1*float64(size))))
	if k.SizeIsDamageAndXP {
		attrs.Instance(javamob.AttrAttackDamage).SetBase(float64(size))
		c.xpReward = size
	}
	if k.OnSetSize != nil {
		k.OnSetSize(m, size)
	}
	m.SyncAttributes()
	if updateHealth {
		m.SetHealth(m.MaxHealth())
	}
}

// XPReward is the cube's xp (Slime/MagmaCube: the size).
func (c *Cube) XPReward(_ *javamob.Mob, base int) int {
	if c.kind.SizeIsDamageAndXP {
		return c.xpReward
	}
	return base
}

// FinalizeSpawn is AbstractCubeMob.finalizeSpawn: setSpawnSize.
func (c *Cube) FinalizeSpawn(m *javamob.Mob, _ javamob.SpawnReason) {
	if c.presized {
		return
	}
	c.SetSize(m, cubeSpawnSize(m.Rand(), m.Difficulty().Special), true)
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

// Tick is the start of AbstractCubeMob.tick, and the split of remove(): a
// dead cube of size > 1 splits when its body is removed. Package mob closes
// a dead mob after its 20th death tick without running it again, so the
// split runs on the last tick the body is ticked (one tick before Java's,
// until the base has a remove hook). The children are converted mobs
// (ConversionType.SPLIT_ON_DEATH: the name and persistence carry over, no
// equipment).
func (c *Cube) Tick(m *javamob.Mob) {
	if !m.Dead() {
		return
	}
	c.deathTicks++
	if c.deathTicks < 19 || c.split || c.size <= 1 {
		return
	}
	c.split = true
	pos := m.Position()
	for _, s := range cubeSplits(m.Rand(), c.size, m.Width(), cubeSplitCount(m.Rand())) {
		size, yaw := s.size, s.yaw
		child, err := javamob.Spawn(m.Tx(), c.kind.ID, pos.Add(s.offset), javamob.SpawnTriggered, javamob.SpawnOptions{
			Configure: func(n *javamob.Mob) {
				if nc, ok := cubeOf(n); ok {
					nc.SetSize(n, size, true)
					nc.presized = true
				}
				n.SetYaw(yaw)
				if name := m.NameTag(); name != "" {
					n.SetNameTag(name)
				}
				if m.PersistenceRequired() {
					n.SetPersistenceRequired()
				}
			},
		})
		_, _ = child, err
	}
}

// PostTick is the rest of AbstractCubeMob.tick (the landing squish) and the
// contact damage of players touching the cube (Player.aiStep touches every
// entity in its box grown by 1, 0.5, 1; the cube's playerTouch hits them).
func (c *Cube) PostTick(m *javamob.Mob) {
	if m.Dead() {
		return
	}
	c.tickLanding(m)
	if !c.canDealDamage() {
		return
	}
	box := m.BBox()
	for e := range m.Tx().EntitiesWithin(box.GrowVec3(mgl64.Vec3{1, 0.5, 1})) {
		p, ok := e.(*player.Player)
		if !ok || !targetBox(p).GrowVec3(mgl64.Vec3{1, 0.5, 1}).IntersectsWith(box) {
			continue
		}
		c.dealDamage(m, p)
	}
}

// tickLanding is the landing part of AbstractCubeMob.tick: landing plays the
// squish sound (the particles are the client's); leaving the ground starts
// the stretch.
func (c *Cube) tickLanding(m *javamob.Mob) {
	onGround := m.OnGround()
	switch {
	case onGround && !c.wasOnGround:
		r := m.Rand()
		snd := c.kind.SquishSound
		if c.Tiny() && c.kind.SquishSoundSmall != "" {
			snd = c.kind.SquishSoundSmall
		}
		playSound(m.Mob, snd, c.soundVolume(), ((nextFloat(r)-nextFloat(r))*0.2+1)/0.8)
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

// dealDamage is AbstractCubeMob.dealDamage: a melee hit for the attack
// damage if the cube is alive, in melee range of the target and sees it.
func (c *Cube) dealDamage(m *javamob.Mob, target world.Entity) bool {
	if !m.IsWithinMeleeAttackRange(target) || !m.HasLineOfSight(target) {
		return false
	}
	if !mobAttack(m.Mob, target, c.attackDamage(m)) {
		return false
	}
	r := m.Rand()
	playSound(m.Mob, "entity.slime.attack", 1, (nextFloat(r)-nextFloat(r))*0.2+1)
	return true
}

// canDealDamage is AbstractCubeMob.canDealDamage.
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
func (c *Cube) attackDamage(m *javamob.Mob) float64 {
	return m.Attr(javamob.AttrAttackDamage) + c.kind.AttackDamageBonus
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

// EncodeNBT writes AbstractCubeMob.addAdditionalSaveData.
func (c *Cube) EncodeNBT(data map[string]any) {
	data["Size"] = int32(c.size - 1)
	data["wasOnGround"] = boolByte(c.wasOnGround)
}

// DecodeNBT reads AbstractCubeMob.readAdditionalSaveData.
func (c *Cube) DecodeNBT(m *javamob.Mob, data map[string]any) {
	size, _ := nbtInt(data, "Size")
	c.SetSize(m, size+1, false)
	c.wasOnGround = nbtBool(data, "wasOnGround")
}

// cubeMoveControl is AbstractCubeMob.CubeMobMoveControl: the cube turns to
// a direction and hops that way, with a random delay between hops (a third
// of it while attacking).
type cubeMoveControl struct {
	yRot          float64
	jumpDelay     int
	aggressive    bool
	moveTo        bool
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
	c.moveTo = true
}

// tick is CubeMobMoveControl.tick.
func (c *cubeMoveControl) tick(m *javamob.Mob, cube *Cube) {
	yaw := rotlerp(m.Yaw(), c.yRot, 90)
	m.SetYaw(yaw)
	m.SetHeadYaw(yaw)
	m.SetBodyYaw(yaw)
	if !c.moveTo {
		m.SetZza(0)
		return
	}
	c.moveTo = false
	speed := c.speedModifier * m.Attr(javamob.AttrMovementSpeed)
	if !m.OnGround() {
		m.SetMoveSpeed(speed)
		return
	}
	m.SetMoveSpeed(speed)
	c.jumpDelay--
	if c.jumpDelay+1 <= 0 { // this.jumpDelay-- <= 0
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
			playSound(m.Mob, snd, cube.soundVolume(), cube.soundPitch(m.Rand()))
		}
		return
	}
	m.SetXxa(0)
	m.SetZza(0)
	m.SetMoveSpeed(0)
}

// cubeAttackGoal is AbstractCubeMob.CubeMobAttackGoal (priority 2, LOOK):
// face the target and hop at it, for at most 300 ticks at a time. Paper: the
// goal also ends when the target died, and stopping clears the target, so
// the cube looks for a new one.
type cubeAttackGoal struct {
	javamob.GoalBase
	c              *CubeMob
	growTiredTimer int
}

func (g *cubeAttackGoal) RequiresUpdateEveryTick() bool { return true }

func (g *cubeAttackGoal) CanUse(m *javamob.Mob) bool {
	t, ok := m.Target()
	return ok && alive(t) && canAttack(m.Mob, t)
}

func (g *cubeAttackGoal) Start(*javamob.Mob) { g.growTiredTimer = reducedTickDelay(300) }

func (g *cubeAttackGoal) CanContinueToUse(m *javamob.Mob) bool {
	t, ok := m.Target()
	if !ok || !alive(t) || !canAttack(m.Mob, t) {
		return false
	}
	g.growTiredTimer--
	return g.growTiredTimer > 0
}

// Stop is Paper's addition: the timer resets and the target is dropped.
func (g *cubeAttackGoal) Stop(m *javamob.Mob) {
	g.growTiredTimer = 0
	m.SetTarget(nil)
}

func (g *cubeAttackGoal) Tick(m *javamob.Mob) {
	if t, ok := m.Target(); ok {
		m.LookAtEntity(t, 10, 10)
	}
	g.c.move.setDirection(m.Yaw(), g.c.canDealDamage())
}

// cubeFloatGoal is AbstractCubeMob.CubeMobFloatGoal (priority 1, JUMP|MOVE):
// in water or lava the cube keeps jumping (80% of ticks) and moves faster.
type cubeFloatGoal struct {
	javamob.GoalBase
	c *CubeMob
}

func (cubeFloatGoal) RequiresUpdateEveryTick() bool { return true }

func (g *cubeFloatGoal) CanUse(m *javamob.Mob) bool { return m.InLiquid() }

func (g *cubeFloatGoal) Tick(m *javamob.Mob) {
	if nextFloat(m.Rand()) < 0.8 {
		m.JumpControl().Jump()
	}
	g.c.move.setWantedMovement(1.2)
}

// cubeKeepOnJumpingGoal is AbstractCubeMob.CubeMobKeepOnJumpingGoal
// (priority 5, JUMP|MOVE): always hop forward unless riding.
type cubeKeepOnJumpingGoal struct {
	javamob.GoalBase
	c *CubeMob
}

func (g *cubeKeepOnJumpingGoal) CanUse(m *javamob.Mob) bool { return !m.Riding() }

func (g *cubeKeepOnJumpingGoal) Tick(*javamob.Mob) { g.c.move.setWantedMovement(1) }

// cubeRandomDirectionGoal is AbstractCubeMob.CubeMobRandomDirectionGoal
// (priority 4, LOOK): without a target, pick a new direction every 40..99
// ticks (halved: the goal does not tick every tick).
type cubeRandomDirectionGoal struct {
	javamob.GoalBase
	c                 *CubeMob
	chosenDegrees     float64
	nextRandomizeTime int
}

func (g *cubeRandomDirectionGoal) CanUse(m *javamob.Mob) bool {
	if _, ok := m.Target(); ok {
		return false
	}
	_, levitating := m.Effect(effect.Levitation)
	return m.OnGround() || m.InLiquid() || levitating
}

func (g *cubeRandomDirectionGoal) Tick(m *javamob.Mob) {
	g.nextRandomizeTime--
	if g.nextRandomizeTime <= 0 {
		g.nextRandomizeTime = adjustedTickDelay(false, 40+nextInt(m.Rand(), 60))
		g.chosenDegrees = float64(nextInt(m.Rand(), 360))
	}
	g.c.move.setDirection(g.chosenDegrees, false)
}
