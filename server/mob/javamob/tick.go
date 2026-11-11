//go:build crossfly_full

package javamob

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// DespawnCheck is Mob.checkDespawn. The natural spawning package sets it (SetDespawnCheck); without one no Java mob
// despawns except in peaceful.
type DespawnCheck func(m *Mob)

var despawnCheck DespawnCheck

// SetDespawnCheck sets the despawn check run at the start of every Java mob's tick, as ServerLevel.tick runs
// Entity.checkDespawn before Entity.tick. Call it from init only.
func SetDespawnCheck(f DespawnCheck) { despawnCheck = f }

// gone reports whether the mob was removed during this tick.
func (m *Mob) gone() bool { return m.H().Closed() || m.Tx() == nil }

// tick is one tick of the mob: Mob.tick (LivingEntity.tick, Entity.tick) after ServerLevel's checkDespawn.
func (m *Mob) tick() {
	s := m.s
	if !m.Dead() && despawnCheck != nil {
		despawnCheck(m)
		if m.gone() {
			return
		}
	}
	// Entity.commonTick: the old position (setOldPosAndRot) and the tick count.
	s.old = m.Position()
	s.tickCount++
	if t, ok := s.b.(Ticker); ok {
		t.Tick(m)
		if m.gone() {
			return
		}
	}
	m.baseTick()
	if m.gone() {
		return
	}
	m.aiStep()
	if m.gone() {
		return
	}
	m.tickHeadTurn()
	if t, ok := s.b.(PostTicker); ok {
		t.PostTick(m)
		if m.gone() {
			return
		}
	}
	p := m.Physics()
	p.OnGround, p.HorizontalCollision, p.VerticalCollision = s.onGround, s.horizontalCollision, s.verticalCollision
	p.InWater, p.InLava, p.Underwater, p.FallDistance = s.inWater, s.lavaHeight > 0, s.eyeInWater, s.fallDistance
	m.SendMovement()
}

// baseTick is Mob.baseTick, LivingEntity.baseTick and Entity.baseTick.
func (m *Mob) baseTick() {
	s := m.s
	tx := m.Tx()
	// Mob.baseTick: the ambient sound, once in a while.
	if !m.Dead() {
		if m.Rand().IntN(1000) < s.ambientSound {
			s.ambientSound = -80 // getAmbientSoundInterval
			m.PlaySound("ambient")
		} else {
			s.ambientSound++
		}
	}
	// Entity.baseTick.
	s.wasInPowderSnow, s.inPowderSnow = s.inPowderSnow, false
	s.wasEyeInWater = s.eyeInWater
	m.updateFluidInteraction()
	fireTicks := int(m.OnFireDuration() / (time.Second / 20))
	if fireTicks > 0 {
		if s.t.FireImmune {
			m.Extinguish()
		} else if fireTicks%20 == 0 && s.lavaHeight <= 0 {
			m.Hurt(1, block.FireDamageSource{})
			if m.gone() {
				return
			}
		}
	}
	if s.lavaHeight > 0 {
		s.fallDistance *= 0.5
	}
	s.firstTick = false
	// LivingEntity.baseTick.
	if !m.Dead() {
		if m.inWall(tx) {
			m.Hurt(1, entity.SuffocationDamageSource{})
			if m.gone() {
				return
			}
		}
		m.tickAir()
		if m.gone() {
			return
		}
	}
	if s.hurtTime > 0 {
		s.hurtTime--
	}
}

// inWall is Entity.isInWall: a suffocating block's collision box takes in the mob's eyes.
func (m *Mob) inWall(tx *world.Tx) bool {
	w := float64(float32(m.s.width) * 0.8)
	eye := m.EyePosition()
	bb := cube.Box(eye[0]-w/2, eye[1]-5e-7, eye[2]-w/2, eye[0]+w/2, eye[1]+5e-7, eye[2]+w/2)
	for pos := range cube.Range3D(cube.PosFromVec3(bb.Min()), cube.PosFromVec3(bb.Max())) {
		b := tx.Block(pos)
		if !suffocating(b) {
			continue
		}
		for _, box := range b.Model().BBox(pos, tx) {
			if overlaps(box.Translate(pos.Vec3()), bb) {
				return true
			}
		}
	}
	return false
}

// air is the mob's air supply (Entity.getAirSupply), in the mob's scratch data.
type airKey struct{}

// tickAir is the drowning part of LivingEntity.baseTick: 300 ticks of air, -1 a tick with the eyes in water, 2
// damage every time it reaches -20, +4 a tick out of water.
func (m *Mob) tickAir() {
	const maxAir = 300
	air := maxAir
	if v, ok := m.Data(airKey{}); ok {
		air = v.(int)
	}
	if m.s.eyeInWater && !m.CanBreatheUnderwater() {
		bonus := m.Attr(AttrOxygenBonus)
		if !(bonus > 0 && m.Rand().Float64() >= 1/(bonus+1)) {
			air--
		}
		if air <= -20 {
			air = 0
			m.Hurt(2, entity.DrowningDamageSource{})
		}
	} else if air < maxAir {
		air = min(air+4, maxAir)
	}
	if air != maxAir || m.hasData(airKey{}) {
		m.SetData(airKey{}, air)
	}
}

func (m *Mob) hasData(k any) bool { _, ok := m.Data(k); return ok }

// CanBreatheUnderwater is LivingEntity.canBreatheUnderwater (EntityTypeTags.CAN_BREATHE_UNDER_WATER: undead and
// aquatic mobs) or water breathing.
func (m *Mob) CanBreatheUnderwater() bool {
	if m.s.t.Undead || m.s.t.Aquatic {
		return true
	}
	_, ok := m.Effect(effect.WaterBreathing)
	return ok
}

// aiStep is Mob.aiStep and LivingEntity.aiStep.
func (m *Mob) aiStep() {
	s := m.s
	if a, ok := s.b.(AiStepper); ok {
		a.AiStep(m)
		if m.gone() {
			return
		}
	}
	if s.noJumpDelay > 0 {
		s.noJumpDelay--
	}
	v := m.Velocity()
	if math.Abs(v[0]) < 0.003 {
		v[0] = 0
	}
	if math.Abs(v[2]) < 0.003 {
		v[2] = 0
	}
	if math.Abs(v[1]) < 0.003 {
		v[1] = 0
	}
	m.SetVelocity(v)
	// applyInput.
	s.xxa *= float64(float32(0.98))
	s.zza *= float64(float32(0.98))
	if m.Dead() {
		s.jumping, s.xxa, s.zza = false, 0, 0
	} else if !s.noAI {
		m.serverAiStep()
		if m.gone() {
			return
		}
	}
	m.handleJump()
	if !s.noAI {
		m.travel(mgl64.Vec3{s.xxa, s.yya, s.zza})
		if m.gone() {
			return
		}
	}
	m.applyEffectsFromBlocks()
	if m.gone() {
		return
	}
	m.pushEntities()
	if m.gone() {
		return
	}
	if ws, ok := s.b.(WaterSensitive); ok && ws.SensitiveToWater(m) && m.InWaterOrRain() {
		m.Hurt(1, entity.DrowningDamageSource{})
		if m.gone() {
			return
		}
	}
	// Mob.aiStep.
	if s.t.BurnsInDaylight {
		m.burnUndead()
	}
}

// handleJump is the jump part of LivingEntity.aiStep.
func (m *Mob) handleJump() {
	s := m.s
	if !s.jumping {
		s.noJumpDelay = 0
		return
	}
	lava := m.InLava()
	height := s.waterHeight
	if lava {
		height = s.lavaHeight
	}
	inWater := s.inWater && height > 0
	threshold := m.FluidJumpThreshold()
	switch {
	case inWater && (!s.onGround || height > threshold):
		m.jumpInLiquidOf(false)
	case lava && (!s.onGround || s.lavaHeight > threshold):
		m.jumpInLiquidOf(true)
	case (s.onGround || inWater && height <= threshold) && s.noJumpDelay == 0:
		m.jumpFromGround()
		s.noJumpDelay = 10
	}
}

// jumpInLiquid is LivingEntity.jumpInLiquid: 0.04 up.
func (m *Mob) jumpInLiquid() {
	m.SetVelocity(m.Velocity().Add(mgl64.Vec3{0, float64(float32(0.04))}))
}

// jumpInLiquidOf runs the behaviour's Jumper.JumpInLiquid, else jumpInLiquid.
func (m *Mob) jumpInLiquidOf(lava bool) {
	if j, ok := m.s.b.(Jumper); ok && j.JumpInLiquid(m, lava) {
		return
	}
	m.jumpInLiquid()
}

// JumpPower is LivingEntity.getJumpPower.
func (m *Mob) JumpPower() float64 { return m.jumpPower() }

// jumpFromGround is LivingEntity.jumpFromGround (or the behaviour's Jumper).
func (m *Mob) jumpFromGround() {
	if j, ok := m.s.b.(Jumper); ok && j.JumpFromGround(m) {
		return
	}
	power := m.jumpPower()
	if power <= 1e-5 {
		return
	}
	v := m.Velocity()
	v[1] = math.Max(power, v[1])
	m.SetVelocity(v)
	m.SyncMovement()
}

// jumpPower is LivingEntity.getJumpPower: the jump strength attribute times the block's jump factor, plus jump
// boost.
func (m *Mob) jumpPower() float64 {
	p := float32(m.Attr(AttrJumpStrength)) * float32(m.blockJumpFactor())
	if e, ok := m.Effect(effect.JumpBoost); ok {
		p += 0.1 * float32(e.Level())
	}
	return float64(p)
}

// serverAiStep is Mob.serverAiStep.
func (m *Mob) serverAiStep() {
	s := m.s
	s.noActionTime++
	s.sensing.tick()
	if (s.tickCount+s.idParity)%2 != 0 && s.tickCount > 1 {
		s.targets.tickRunning(m, false)
		s.goals.tickRunning(m, false)
	} else {
		s.targets.tick(m)
		s.goals.tick(m)
	}
	if m.gone() {
		return
	}
	s.nav.Tick(m)
	if c, ok := s.b.(ServerAiStepper); ok {
		c.CustomServerAiStep(m)
		if m.gone() {
			return
		}
	}
	if s.brain != nil {
		s.brain.Tick(m)
		if m.gone() {
			return
		}
	}
	s.move.tick(m)
	s.look.tick(m)
	s.jump.tick(m)
}

// tickHeadTurn is Mob.tickHeadTurn: the body rotation control.
func (m *Mob) tickHeadTurn() { m.s.body.tick(m) }

// burnUndead is Mob.burnUndead: in daylight under the open sky, a mob without a helmet catches fire for 8 seconds
// and one with a helmet damages it instead.
func (m *Mob) burnUndead() {
	if m.Dead() || !m.sunBurnTick() {
		return
	}
	if ss, ok := m.s.b.(SunSensitive); ok && !ss.SunSensitive(m) {
		return
	}
	if helmet := m.Armour().Helmet(); !helmet.Empty() {
		m.damageHelmet(m.Rand().IntN(2))
		return
	}
	m.IgniteForSeconds(8)
}

// sunBurnTick is Mob.isSunBurnTick.
func (m *Mob) sunBurnTick() bool {
	tx := m.Tx()
	if tx.World().Dimension() != world.Overworld {
		return false
	}
	br := m.LightLevelDependentMagicValue()
	pos := m.Position()
	eye := cube.PosFromVec3(mgl64.Vec3{pos[0], m.EyeY(), pos[2]})
	if m.InWaterOrRain() || m.s.inPowderSnow || m.s.wasInPowderSnow {
		return false
	}
	return br > 0.5 && m.Rand().Float32()*30 < (br-0.4)*2 && canSeeSky(tx, eye)
}

// LightLevelDependentMagicValue is Entity.getLightLevelDependentMagicValue: the brightness at the mob's eyes (0..1).
func (m *Mob) LightLevelDependentMagicValue() float32 {
	pos := m.Position()
	p := cube.PosFromVec3(mgl64.Vec3{pos[0], m.EyeY(), pos[2]})
	return brightness(RawBrightness(m.Tx(), p))
}

// brightness is DimensionType's brightness curve with no ambient light.
func brightness(level int) float32 {
	f := float32(level) / 15
	return f / (4 - 3*f)
}

// RawBrightness is Level.getMaxLocalRawBrightness: the block light or the sky light darkened by the time of day and
// the weather, whichever is brighter.
func RawBrightness(tx *world.Tx, pos cube.Pos) int {
	sky := int(tx.SkyLight(pos)) - SkyDarken(tx)
	return max(sky, int(tx.BlockLight(pos)), 0)
}

// SkyDarken is Level.skyDarken: how much darker than 15 the sky light is now.
func SkyDarken(tx *world.Tx) int {
	w := tx.World()
	if w.Dimension() != world.Overworld {
		return 0
	}
	t := float64(w.Time()%24000) / 24000
	frac := t - 0.25
	frac -= math.Floor(frac)
	smooth := 0.5 - math.Cos(frac*math.Pi)/2
	tod := (frac*2 + smooth) / 3
	day := 0.5 + 2*math.Max(-0.25, math.Min(0.25, math.Cos(tod*math.Pi*2)))
	if tx.Raining() {
		day *= 1 - 5.0/16
	}
	if tx.Thundering() {
		day *= 1 - 5.0/16
	}
	return int((1 - day) * 11)
}

// IgniteForSeconds is Entity.igniteForSeconds: on fire for at least that long.
func (m *Mob) IgniteForSeconds(sec float64) {
	d := time.Duration(math.Floor(sec*20)) * time.Second / 20
	if m.OnFireDuration() < d {
		m.SetOnFire(d)
	}
}

// IsOnFire reports whether the mob burns.
func (m *Mob) IsOnFire() bool { return m.OnFireDuration() > 0 && !m.s.t.FireImmune }

func canSeeSky(tx *world.Tx, pos cube.Pos) bool {
	return pos[1] > tx.HighestLightBlocker(pos[0], pos[2])
}

func overlaps(a, b cube.BBox) bool {
	return a.Min()[0] < b.Max()[0] && a.Max()[0] > b.Min()[0] &&
		a.Min()[1] < b.Max()[1] && a.Max()[1] > b.Min()[1] &&
		a.Min()[2] < b.Max()[2] && a.Max()[2] > b.Min()[2]
}
