package jraid

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// Attributes are the base attributes of a raid mob (createAttributes on top of Monster.createMonsterAttributes:
// attack damage 2, follow range 16, movement speed 0.7 by default).
type Attributes struct {
	MaxHealth, MovementSpeed, AttackDamage, FollowRange float64
	KnockbackResistance, AttackKnockback, StepHeight    float64
	// XP is the experience reward (Monster default 5).
	XP int
}

// MobAttributes are the attributes of the mobs of the raids row, by Java entity id.
var MobAttributes = map[string]Attributes{
	// Pillager.createAttributes.
	"minecraft:pillager": {MaxHealth: 24, MovementSpeed: 0.35, AttackDamage: 5, FollowRange: 32, StepHeight: 0.6, XP: 5},
	// Vindicator.createAttributes.
	"minecraft:vindicator": {MaxHealth: 24, MovementSpeed: 0.35, AttackDamage: 5, FollowRange: 12, StepHeight: 0.6, XP: 5},
	// Evoker.createAttributes, xpReward 10.
	"minecraft:evoker": {MaxHealth: 24, MovementSpeed: 0.5, AttackDamage: 2, FollowRange: 12, StepHeight: 0.6, XP: 10},
	// Illusioner.createAttributes, xpReward 5.
	"minecraft:illusioner": {MaxHealth: 32, MovementSpeed: 0.5, AttackDamage: 2, FollowRange: 18, StepHeight: 0.6, XP: 5},
	// Witch.createAttributes.
	"minecraft:witch": {MaxHealth: 26, MovementSpeed: 0.25, AttackDamage: 2, FollowRange: 16, StepHeight: 0.6, XP: 5},
	// Ravager.createAttributes, xpReward 20.
	"minecraft:ravager": {MaxHealth: 100, MovementSpeed: 0.3, AttackDamage: 12, FollowRange: 32, KnockbackResistance: 0.75,
		AttackKnockback: 1.5, StepHeight: 1, XP: 20},
	// Vex.createAttributes, xpReward 3.
	"minecraft:vex": {MaxHealth: 14, MovementSpeed: 0.7, AttackDamage: 4, FollowRange: 16, StepHeight: 0.6, XP: 3},
}

// RangedAttackUncertainty is RangedAttackMob.rangedAttackUncertainty: 14 - 4 * the difficulty id (the pillager's
// crossbow, the illusioner's bow). Witches override it with 8 (WitchRangedAttackUncertainty).
func RangedAttackUncertainty(d Difficulty) float32 { return float32(14 - int(d)*4) }

// WitchRangedAttackUncertainty is Witch.rangedAttackUncertainty.
const WitchRangedAttackUncertainty = 8

// ArmPose is AbstractIllager.IllagerArmPose.
type ArmPose int

const (
	PoseCrossed ArmPose = iota
	PoseAttacking
	PoseSpellcasting
	PoseBowAndArrow
	PoseCrossbowHold
	PoseCrossbowCharge
	PoseCelebrating
	PoseNeutral
)

// Spell is SpellcasterIllager.IllagerSpell, by its id (the DATA_SPELL_CASTING_ID byte the client reads).
type Spell int

const (
	SpellNone Spell = iota
	SpellSummonVex
	SpellFangs
	SpellWololo
	SpellDisappear
	SpellBlindness
)

// Colour is the spell's particle colour (IllagerSpell.spellColor), red, green and blue from 0 to 1.
func (s Spell) Colour() [3]float64 {
	return [...][3]float64{{0, 0, 0}, {0.7, 0.7, 0.8}, {0.4, 0.3, 0.35}, {0.7, 0.5, 0.2}, {0.3, 0.3, 0.8}, {0.1, 0.1, 0.2}}[s]
}

// SpellTiming is the timing of a SpellcasterUseSpellGoal: the warm-up until the spell is cast (getCastWarmupTime,
// adjusted for the goal's tick rate), the time the caster raises its arms (getCastingTime) and the interval before
// the goal may run again (getCastingInterval).
type SpellTiming struct{ Warmup, Casting, Interval int }

// SpellTimings are the timings of the evoker's and illusioner's spells (Evoker.EvokerAttackSpellGoal,
// EvokerSummonSpellGoal, EvokerWololoSpellGoal, Illusioner.IllusionerMirrorSpellGoal and
// IllusionerBlindnessSpellGoal).
var SpellTimings = map[Spell]SpellTiming{
	SpellFangs:     {Warmup: 20, Casting: 40, Interval: 100},
	SpellSummonVex: {Warmup: 20, Casting: 100, Interval: 340},
	SpellWololo:    {Warmup: 40, Casting: 60, Interval: 140},
	SpellDisappear: {Warmup: 20, Casting: 20, Interval: 340},
	SpellBlindness: {Warmup: 20, Casting: 20, Interval: 180},
}

// Spellcaster is the spell state of a SpellcasterIllager and its SpellcasterUseSpellGoal.
type Spellcaster struct {
	// CastingTicks is SpellcasterIllager.spellCastingTickCount ("SpellTicks").
	CastingTicks int
	// Current is the spell being cast (setIsCastingSpell), shown to clients.
	Current Spell
}

// IsCasting is SpellcasterIllager.isCastingSpell.
func (s *Spellcaster) IsCasting() bool { return s.CastingTicks > 0 }

// ServerAIStep is SpellcasterIllager.customServerAiStep: the casting time runs down.
func (s *Spellcaster) ServerAIStep() {
	if s.CastingTicks > 0 {
		s.CastingTicks--
	}
}

// ArmPose is SpellcasterIllager.getArmPose for evokers (Illusioner.getArmPose: an aggressive illusioner draws its
// bow, see IllusionerArmPose).
func (s *Spellcaster) ArmPose(celebrating bool) ArmPose {
	if s.IsCasting() {
		return PoseSpellcasting
	}
	if celebrating {
		return PoseCelebrating
	}
	return PoseCrossed
}

// IllusionerArmPose is Illusioner.getArmPose.
func IllusionerArmPose(casting, aggressive bool) ArmPose {
	if casting {
		return PoseSpellcasting
	}
	if aggressive {
		return PoseBowAndArrow
	}
	return PoseCrossed
}

// UseSpell is a running SpellcasterUseSpellGoal.
type UseSpell struct {
	Spell Spell
	// WarmupDelay is attackWarmupDelay: the ticks left until the spell is cast.
	WarmupDelay int
	// NextAttackTick is nextAttackTickCount: the caster's tickCount before which the goal cannot start again.
	NextAttackTick int
}

// CanUse is SpellcasterUseSpellGoal.canUse without the subclass checks: the caster has a living target, casts
// nothing and the interval passed.
func (g *UseSpell) CanUse(c *Spellcaster, hasLivingTarget bool, tickCount int) bool {
	return hasLivingTarget && !c.IsCasting() && tickCount >= g.NextAttackTick
}

// Start is SpellcasterUseSpellGoal.start (the caller plays the prepare sound): the warm-up is the goal's
// adjustedTickDelay of the spell's warm-up, which halves it for goals that tick every other tick.
func (g *UseSpell) Start(c *Spellcaster, tickCount int, adjustedTickDelay func(int) int) {
	t := SpellTimings[g.Spell]
	g.WarmupDelay = adjustedTickDelay(t.Warmup)
	c.CastingTicks = t.Casting
	g.NextAttackTick = tickCount + t.Interval
	c.Current = g.Spell
}

// Tick is SpellcasterUseSpellGoal.tick: it reports true on the tick the spell is cast (performSpellCasting and the
// casting sound).
func (g *UseSpell) Tick() bool {
	g.WarmupDelay--
	return g.WarmupDelay == 0
}

// Stop is SpellcasterCastingSpellGoal.stop: the caster shows no spell.
func (c *Spellcaster) Stop() { c.Current = SpellNone }

// Fang is an evoker fang to place: its position, yaw in radians and warm-up delay in ticks.
type Fang struct {
	X, Z  float64
	Angle float32
	Delay int
}

// FangLayout is Evoker.EvokerAttackSpellGoal.performSpellCasting: within 3 blocks of the target, a ring of 5 fangs
// at 1.5 blocks and 8 at 2.5 blocks (delay 3) around the evoker; farther away, a line of 16 fangs towards the
// target, 1.25 blocks apart, each a tick later than the last.
func FangLayout(ex, ez, tx, tz, distSqr float64) []Fang {
	angle := float32(math.Atan2(tz-ez, tx-ex))
	var fangs []Fang
	if distSqr < 9 {
		for i := 0; i < 5; i++ {
			a := angle + float32(i)*math.Pi*0.4
			fangs = append(fangs, Fang{X: ex + float64(cos32(a))*1.5, Z: ez + float64(sin32(a))*1.5, Angle: a})
		}
		for i := 0; i < 8; i++ {
			a := angle + float32(i)*math.Pi*2/8 + float32(math.Pi*2/5)
			fangs = append(fangs, Fang{X: ex + float64(cos32(a))*2.5, Z: ez + float64(sin32(a))*2.5, Angle: a, Delay: 3})
		}
		return fangs
	}
	for i := 0; i < 16; i++ {
		reach := 1.25 * float64(i+1)
		fangs = append(fangs, Fang{X: ex + float64(cos32(angle))*reach, Z: ez + float64(sin32(angle))*reach, Angle: angle, Delay: i})
	}
	return fangs
}

// FangGround is what EvokerAttackSpellGoal.createSpellEntity reads of the level.
type FangGround interface {
	// TopFaceSturdy is BlockState.isFaceSturdy(level, pos, UP).
	TopFaceSturdy(pos cube.Pos) bool
	// CollisionTop is the top of the block's collision shape at pos (VoxelShape.max(Y)), 0 for air or no shape.
	CollisionTop(pos cube.Pos) float64
}

// FangY is EvokerAttackSpellGoal.createSpellEntity's ground search: from maxY down to minY - 1, the first block
// standing on a sturdy top face; the fang sits on top of that block's collision shape. It reports false if there
// is no ground.
func FangY(g FangGround, x, z, minY, maxY float64) (float64, bool) {
	pos := cube.PosFromVec3([3]float64{x, maxY, z})
	for {
		below := pos.Side(cube.FaceDown)
		if g.TopFaceSturdy(below) {
			return float64(pos[1]) + g.CollisionTop(pos), true
		}
		pos = below
		if pos[1] < int(math.Floor(minY))-1 {
			return 0, false
		}
	}
}

// Fang timing (EvokerFangs): the fang bites 8 ticks after its warm-up ends (warmupDelayTicks reaching -8), dealing
// 6 magic damage (indirect, by the evoker) to living entities within its box grown by 0.2 horizontally that are
// not allied to the evoker, and is gone 22 ticks after the warm-up.
const (
	FangBiteTick = -8
	FangDamage   = 6
	FangLife     = 22
)

// FangState is the server side of EvokerFangs.
type FangState struct {
	Warmup    int
	life      int
	sentSpike bool
}

// NewFangState returns a fang with the warm-up delay.
func NewFangState(warmup int) *FangState { return &FangState{Warmup: warmup, life: FangLife} }

// Tick is EvokerFangs.tick on the server: it reports whether to send the attack event (entity event 4, the bite
// animation and sound), whether to bite and whether the fang is done.
func (f *FangState) Tick() (spike, bite, done bool) {
	f.Warmup--
	if f.Warmup >= 0 {
		return false, false, false
	}
	bite = f.Warmup == FangBiteTick
	if !f.sentSpike {
		f.sentSpike, spike = true, true
	}
	f.life--
	return spike, bite, f.life < 0
}

// VexSummons is the number of vexes an evoker summons (EvokerSummonSpellGoal.performSpellCasting).
const VexSummons = 3

// CanSummonVexes is the extra check of EvokerSummonSpellGoal.canUse: random(8) + 1 is above the vexes within 16
// blocks.
func CanSummonVexes(r *rand.Rand, vexesNearby int) bool { return r.IntN(8)+1 > vexesNearby }

// VexSpawn is where a summoned vex appears and how long it lives: 2 blocks around the evoker, one up, for 30 to
// 119 seconds (setLimitedLife).
func VexSpawn(r *rand.Rand, evoker cube.Pos) (pos cube.Pos, lifeTicks int) {
	pos = evoker.Add(cube.Pos{-2 + r.IntN(5), 1, -2 + r.IntN(5)})
	return pos, 20 * (30 + r.IntN(90))
}

// Potion is a potion a witch drinks or throws.
type Potion int

const (
	PotionNone Potion = iota
	PotionWaterBreathing
	PotionFireResistance
	PotionHealing
	PotionSwiftness
	PotionHarming
	PotionRegeneration
	PotionSlowness
	PotionPoison
	PotionWeakness
)

// WitchState is what Witch.aiStep reads of a witch to pick a potion to drink.
type WitchState struct {
	EyeInWater, WaterBreathing     bool
	OnFireOrHurtByFire, FireResist bool
	Health, MaxHealth              float32
	HasTarget, Speed               bool
	TargetDistSqr                  float64
}

// WitchDrink is the potion choice of Witch.aiStep for a witch not drinking: water breathing (15%) under water,
// fire resistance (15%) when burning, healing (5%) when hurt, swiftness (50%) when its target is over 11 blocks
// away. Each check draws its own random number, in order.
func WitchDrink(r *rand.Rand, s WitchState) Potion {
	if r.Float32() < 0.15 && s.EyeInWater && !s.WaterBreathing {
		return PotionWaterBreathing
	}
	if r.Float32() < 0.15 && s.OnFireOrHurtByFire && !s.FireResist {
		return PotionFireResistance
	}
	if r.Float32() < 0.05 && s.Health < s.MaxHealth {
		return PotionHealing
	}
	if r.Float32() < 0.5 && s.HasTarget && !s.Speed && s.TargetDistSqr > 121 {
		return PotionSwiftness
	}
	return PotionNone
}

// WitchTarget is what Witch.performRangedAttack reads of its target.
type WitchTarget struct {
	Raider                     bool
	Health                     float32
	Slowness, Poison, Weakness bool
}

// WitchThrow is Witch.performRangedAttack's potion choice: a raider gets healing (at 4 health or less) or
// regeneration (and the witch drops it as target); others get slowness from 8 blocks, poison at 8 health or more,
// weakness (25%) within 3 blocks, harming otherwise. dist is the horizontal distance to where the target will be.
func WitchThrow(r *rand.Rand, t WitchTarget, dist float64) (p Potion, untarget bool) {
	if t.Raider {
		if t.Health <= 4 {
			return PotionHealing, true
		}
		return PotionRegeneration, true
	}
	if dist >= 8 && !t.Slowness {
		return PotionSlowness, false
	}
	if t.Health >= 8 && !t.Poison {
		return PotionPoison, false
	}
	if dist <= 3 && !t.Weakness && r.Float32() < 0.25 {
		return PotionWeakness, false
	}
	return PotionHarming, false
}

// WitchThrowPower is the splash potion's speed: 0.45 within 2 blocks, 0.75 otherwise.
func WitchThrowPower(dist float64) float32 {
	if dist <= 2 {
		return 0.45
	}
	return 0.75
}

// Witch constants: the drinking slowdown (SPEED_MODIFIER_DRINKING, -0.25 added to the movement speed), the
// chance per tick of the witch particle event (entity event 15), the healing goal's cooldown
// (NearestHealableRaiderTargetGoal) and the damage witches take from magic (DamageTypeTags.WITCH_RESISTANT_TO).
const (
	WitchDrinkingSpeed     = -0.25
	WitchParticleChance    = 7.5e-4
	WitchHealCooldown      = 200
	WitchMagicDamageFactor = 0.15
	PotionDrinkTicks       = 32
)

// RavagerState is the attack, stun and roar state of a ravager (Ravager.attackTick, stunnedTick, roarTick).
type RavagerState struct {
	AttackTick, StunnedTick, RoarTick int
}

// Ravager constants.
const (
	RavagerAttackDuration = 10
	RavagerStunDuration   = 40
	RavagerRoarDuration   = 20
	RavagerRoarTick       = 10
	RavagerRoarDamage     = 6
	RavagerRoarRadius     = 4
	RavagerBaseSpeed      = 0.3
	RavagerAttackSpeed    = 0.35
)

// Immobile is the part of Ravager.isImmobile it adds: attacking, stunned or roaring.
func (r *RavagerState) Immobile() bool {
	return r.AttackTick > 0 || r.StunnedTick > 0 || r.RoarTick > 0
}

// CanSee is the part of Ravager.hasLineOfSight it adds: no sight while stunned or roaring.
func (r *RavagerState) CanSee() bool { return r.StunnedTick <= 0 && r.RoarTick <= 0 }

// Speed is the movement speed base value Ravager.aiStep moves towards: 0 while immobile, else lerp 10% towards
// 0.35 with a target or 0.3 without.
func (r *RavagerState) Speed(base float64, immobile, hasTarget bool) float64 {
	if immobile {
		return 0
	}
	target := RavagerBaseSpeed
	if hasTarget {
		target = RavagerAttackSpeed
	}
	return base + 0.1*(target-base)
}

// Tick is the timer part of Ravager.aiStep: it reports whether the ravager roars this tick (roarTick reaching 10)
// and whether its stun ended (the roar sound plays and the roar starts).
func (r *RavagerState) Tick() (roar, stunEnded bool) {
	if r.RoarTick > 0 {
		r.RoarTick--
		roar = r.RoarTick == RavagerRoarTick
	}
	if r.AttackTick > 0 {
		r.AttackTick--
	}
	if r.StunnedTick > 0 {
		r.StunnedTick--
		if r.StunnedTick == 0 {
			r.RoarTick = RavagerRoarDuration
			stunEnded = true
		}
	}
	return roar, stunEnded
}

// Attack is Ravager.doHurtTarget's timer: the ravager is busy for 10 ticks (entity event 4).
func (r *RavagerState) Attack() { r.AttackTick = RavagerAttackDuration }

// BlockedByShield is Ravager.blockedByItem: when not roaring, a shield block stuns it for 40 ticks half the time
// (entity event 39, the defender is pushed), otherwise knocks the defender back hard. It reports whether the
// ravager is stunned and whether to knock back.
func (r *RavagerState) BlockedByShield(rnd *rand.Rand) (stunned, knockback bool) {
	if r.RoarTick != 0 {
		return false, false
	}
	if rnd.Float64() < 0.5 {
		r.StunnedTick = RavagerStunDuration
		return true, false
	}
	return false, true
}

// StrongKnockback is Ravager.strongKnockback: the push from the ravager to an entity dx, dz away.
func StrongKnockback(dx, dz float64) (x, y, z float64) {
	dd := math.Max(dx*dx+dz*dz, 0.001)
	return dx / dd * 4, 0.2, dz / dd * 4
}

// CrossbowState is RangedCrossbowAttackGoal.CrossbowState.
type CrossbowState int

const (
	Uncharged CrossbowState = iota
	Charging
	Charged
	ReadyToAttack
)

// CrossbowGoal is the state of RangedCrossbowAttackGoal (pillagers: speed 1, radius 8).
type CrossbowGoal struct {
	State          CrossbowState
	SeeTime        int
	AttackDelay    int
	UpdatePathWait int
	AttackRadiusSq float64
}

// NewCrossbowGoal is RangedCrossbowAttackGoal(mob, speed, attackRadius).
func NewCrossbowGoal(attackRadius float64) *CrossbowGoal {
	return &CrossbowGoal{AttackRadiusSq: attackRadius * attackRadius}
}

// CrossbowMob is what RangedCrossbowAttackGoal.tick drives.
type CrossbowMob interface {
	// MoveTo is getNavigation().moveTo(target, speed): speed 1 while uncharged, 0.5 otherwise.
	MoveTo(speedFactor float64)
	StopNavigation()
	LookAtTarget()
	StartUsingCrossbow()
	IsUsingItem() bool
	TicksUsingItem() int
	ChargeDuration() int
	ReleaseUsingItem()
	SetChargingCrossbow(bool)
	PerformRangedAttack()
}

// Tick is RangedCrossbowAttackGoal.tick for a mob with a target: walk into range and sight, charge the crossbow,
// wait 1 to 2 seconds, then fire when the target is in sight.
func (g *CrossbowGoal) Tick(r *rand.Rand, m CrossbowMob, lineOfSight bool, distSqr float64) {
	if lineOfSight != (g.SeeTime > 0) {
		g.SeeTime = 0
	}
	if lineOfSight {
		g.SeeTime++
	} else {
		g.SeeTime--
	}
	needsToMove := (distSqr > g.AttackRadiusSq || g.SeeTime < 5) && g.AttackDelay == 0
	if needsToMove {
		g.UpdatePathWait--
		if g.UpdatePathWait <= 0 {
			if g.State == Uncharged {
				m.MoveTo(1)
			} else {
				m.MoveTo(0.5)
			}
			// PATHFINDING_DELAY_RANGE: TimeUtil.rangeOfSeconds(1, 2).
			g.UpdatePathWait = 20 + r.IntN(21)
		}
	} else {
		g.UpdatePathWait = 0
		m.StopNavigation()
	}
	m.LookAtTarget()
	switch g.State {
	case Uncharged:
		if !needsToMove {
			m.StartUsingCrossbow()
			g.State = Charging
			m.SetChargingCrossbow(true)
		}
	case Charging:
		if !m.IsUsingItem() {
			g.State = Uncharged
		}
		if m.TicksUsingItem() >= m.ChargeDuration() {
			m.ReleaseUsingItem()
			g.State = Charged
			g.AttackDelay = 20 + r.IntN(20)
			m.SetChargingCrossbow(false)
		}
	case Charged:
		g.AttackDelay--
		if g.AttackDelay == 0 {
			g.State = ReadyToAttack
		}
	case ReadyToAttack:
		if lineOfSight {
			m.PerformRangedAttack()
			g.State = Uncharged
		}
	}
}

// PillagerArmPose is Pillager.getArmPose.
func PillagerArmPose(charging, holdingCrossbow, aggressive bool) ArmPose {
	switch {
	case charging:
		return PoseCrossbowCharge
	case holdingCrossbow:
		return PoseCrossbowHold
	case aggressive:
		return PoseAttacking
	}
	return PoseNeutral
}

// VindicatorArmPose is Vindicator.getArmPose.
func VindicatorArmPose(aggressive, celebrating bool) ArmPose {
	switch {
	case aggressive:
		return PoseAttacking
	case celebrating:
		return PoseCelebrating
	}
	return PoseCrossed
}

// IsJohnny is Vindicator.setCustomName's check: a vindicator named Johnny attacks every living entity
// (VindicatorJohnnyAttackGoal) and keeps that once set ("Johnny" in its data).
func IsJohnny(name string) bool { return name == "Johnny" }

// VindicatorBreaksDoors is Vindicator.DOOR_BREAKING_PREDICATE (Paper's door-breaking-difficulty default for
// vindicators): normal and hard.
func VindicatorBreaksDoors(d Difficulty) bool { return d == Normal || d == Hard }

// Vex constants: a vex with limited life takes 1 starvation damage every second once its life ran out
// (Vex.tick), charges 1 in 7 ticks when over 2 blocks from its target (VexChargeAttackGoal) and picks a random
// air block within 7/5/7 of its bound origin to fly to (VexRandomMoveGoal).
const (
	VexChargeChance   = 7
	VexStarveInterval = 20
)
