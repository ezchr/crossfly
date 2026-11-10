//go:build crossfly_full

package block

import (
	"math"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/gameevent"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// The sculk sensor and shrieker only matter with the mobs (the shrieker summons wardens): they register in
// Crossfly Full with the mob blocks, with the game event listener that lets them hear vibrations and the
// tick of the players' warden warning levels (WardenSpawnTrackerSystem).
func init() {
	mobBlocks = append(mobBlocks, func() {
		for phase := 0; phase <= 2; phase++ {
			world.RegisterBlock(SculkSensor{Phase: phase})
			for _, d := range cube.Directions() {
				world.RegisterBlock(CalibratedSculkSensor{Facing: d, Phase: phase})
			}
		}
		for _, active := range []bool{false, true} {
			world.RegisterBlock(SculkShrieker{Active: active})
			world.RegisterBlock(SculkShrieker{Active: active, CanSummon: true})
		}
		world.RegisterItem(SculkSensor{})
		world.RegisterItem(CalibratedSculkSensor{})
		world.RegisterItem(SculkShrieker{})
		gameevent.Register(sculkHear)
		world.RegisterTickHook(wardenThreatTick)
	})
}

var hashSculkSensor, hashSculkShrieker, hashCalibratedSculkSensor = NextHash(), NextHash(), NextHash()

// Sculk sensor values (SculkSensorBlock, SculkSensorVibrationConfig). The calibrated sensor is a
// SculkSensorBlock built with an active time of 10 (CalibratedSculkSensorBlock::CalibratedSculkSensorBlock)
// whose block actor listens 16 blocks far (std::make_shared<CalibratedSculkSensorBlockActor> passes 16 to
// SculkSensorBlockActor, where the plain one passes 8).
const (
	sculkSensorRange                 = 8
	calibratedSculkSensorRange       = 16
	sculkSensorActiveTicks           = 30
	calibratedSculkSensorActiveTicks = 10
	sculkSensorCooldown              = 10
	sculkShriekerRange               = 8
	sculkShriekerTicks               = 90
)

// SculkSensor is a sculk sensor (SculkSensorBlock): it hears the vibrations within 8 blocks, and one that
// reaches it makes it active for 30 ticks, giving a redstone signal that is the stronger the closer the
// vibration was, then 10 ticks of cooldown. Each activation clicks its tendrils
// (sculk_sensor_tendrils_clicking), which sculk shriekers within 8 blocks hear.
type SculkSensor struct {
	transparent
	sourceWaterDisplacer

	// Phase is sculk_sensor_phase: 0 inactive, 1 active, 2 cooldown.
	Phase int
	// Power is the redstone signal while active.
	Power int
	// Frequency is the frequency of the last vibration received (the comparator output).
	Frequency int
}

// SculkShrieker is a sculk shrieker (SculkShriekerBlock, SculkShriekerBlockActor): a player stepping on it
// or a sculk sensor clicking within 8 blocks of it because of a player makes it shriek. One that can summon
// (can_summon, as generated in ancient cities) raises the warning level of the players near it first,
// and at level 4 a warden emerges near it. A shriek darkens the screens of the players within 40 blocks.
type SculkShrieker struct {
	transparent
	sourceWaterDisplacer

	// Active is set while it shrieks (90 ticks).
	Active bool
	// CanSummon is can_summon.
	CanSummon bool
	// WarningLevel is the warning level its last shriek reached, 0 for none: it answers with it when the
	// shriek ends.
	WarningLevel int
}

// EncodeBlock ...
func (s SculkSensor) EncodeBlock() (string, map[string]any) {
	return "minecraft:sculk_sensor", map[string]any{"sculk_sensor_phase": int32(s.Phase)}
}

// EncodeItem ...
func (SculkSensor) EncodeItem() (string, int16) { return "minecraft:sculk_sensor", 0 }

// Hash ...
func (s SculkSensor) Hash() (uint64, uint64) { return hashSculkSensor, uint64(s.Phase) }

// Model ...
func (SculkSensor) Model() world.BlockModel { return model.Slab{} }

// BreakInfo ...
func (s SculkSensor) BreakInfo() BreakInfo {
	return newBreakInfo(1.5, alwaysHarvestable, hoeEffective, oneOf(SculkSensor{})).withXPDropRange(5, 5)
}

// LightEmissionLevel ...
func (SculkSensor) LightEmissionLevel() uint8 { return 1 }

// EncodeNBT ...
func (s SculkSensor) EncodeNBT() map[string]any {
	return map[string]any{"id": "SculkSensor", "power": int32(s.Power), "last_vibration_frequency": int32(s.Frequency)}
}

// DecodeNBT ...
func (s SculkSensor) DecodeNBT(data map[string]any) any {
	if v, ok := data["power"].(int32); ok {
		s.Power = int(v)
	}
	if v, ok := data["last_vibration_frequency"].(int32); ok {
		s.Frequency = int(v)
	}
	return s
}

// RedstonePower is the signal while active.
func (s SculkSensor) RedstonePower(cube.Pos, *world.Tx, cube.Face) int {
	return s.sensor().signal()
}

// RedstoneStrongPower strongly powers the block below while active (Java fallback: no Bedrock data. Java's
// SculkSensorBlock.getDirectSignal gives its signal upwards only, so to the block beneath).
func (s SculkSensor) RedstoneStrongPower(_ cube.Pos, _ *world.Tx, face cube.Face) int {
	if face != cube.FaceDown {
		return 0
	}
	return s.sensor().signal()
}

// Tick runs the sensor's vibration listener (SculkSensorBlockActor::tick).
func (s SculkSensor) Tick(currentTick int64, pos cube.Pos, tx *world.Tx) {
	sculkListenerTick(tx, pos, currentTick, sculkKindSensor)
}

// EntityStepOn posts sculk_touch for an entity standing on the sensor (SculkSensorBlock::onStandOn).
func (SculkSensor) EntityStepOn(pos cube.Pos, tx *world.Tx, e world.Entity) { sculkTouch(tx, pos, e) }

// SculkStepper ...
func (SculkSensor) SculkStepper() {}

// ScheduledTick ends the active phase and then the cooldown (SculkSensorBlock::tick).
func (s SculkSensor) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	sculkSensorScheduledTick(s, pos, tx)
}

func (s SculkSensor) sensor() sculkSensorState {
	return sculkSensorState{s.Phase, s.Power, s.Frequency}
}

func (s SculkSensor) withSensor(st sculkSensorState) sculkSensorBlock {
	s.Phase, s.Power, s.Frequency = st.Phase, st.Power, st.Frequency
	return s
}

func (SculkSensor) sensorRange() float64 { return sculkSensorRange }
func (SculkSensor) activeTicks() int     { return sculkSensorActiveTicks }

// CalibratedSculkSensor is a calibrated sculk sensor (CalibratedSculkSensorBlock): a sculk sensor that hears
// vibrations within 16 blocks and stays active for 10 ticks. A redstone signal into its back makes it hear
// only the vibrations whose frequency equals that signal (CalibratedSculkSensorVibrationConfig::shouldListen).
// It gives its signal to every side but its back.
type CalibratedSculkSensor struct {
	transparent
	sourceWaterDisplacer

	// Facing is minecraft:cardinal_direction, the direction the player who placed it was looking in. Its back,
	// where it takes its input from, is on the opposite side.
	Facing cube.Direction
	// Phase is sculk_sensor_phase: 0 inactive, 1 active, 2 cooldown.
	Phase int
	// Power is the redstone signal while active.
	Power int
	// Frequency is the frequency of the last vibration received (the comparator output).
	Frequency int
}

// EncodeBlock ...
func (s CalibratedSculkSensor) EncodeBlock() (string, map[string]any) {
	return "minecraft:calibrated_sculk_sensor", map[string]any{
		"minecraft:cardinal_direction": s.Facing.String(),
		"sculk_sensor_phase":           int32(s.Phase),
	}
}

// EncodeItem ...
func (CalibratedSculkSensor) EncodeItem() (string, int16) {
	return "minecraft:calibrated_sculk_sensor", 0
}

// Hash ...
func (s CalibratedSculkSensor) Hash() (uint64, uint64) {
	return hashCalibratedSculkSensor, uint64(s.Phase) | uint64(s.Facing)<<2
}

// Model ...
func (CalibratedSculkSensor) Model() world.BlockModel { return model.Slab{} }

// BreakInfo ...
func (s CalibratedSculkSensor) BreakInfo() BreakInfo {
	return newBreakInfo(1.5, alwaysHarvestable, hoeEffective, oneOf(CalibratedSculkSensor{})).withXPDropRange(5, 5)
}

// LightEmissionLevel ...
func (CalibratedSculkSensor) LightEmissionLevel() uint8 { return 1 }

// UseOnBlock places the sensor facing the way the player looks (Java fallback: no Bedrock data, the BDS
// dump has no placement code for it; Java's CalibratedSculkSensorBlock.getStateForPlacement takes the
// player's horizontal direction).
func (s CalibratedSculkSensor) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, s)
	if !used {
		return false
	}
	s = CalibratedSculkSensor{Facing: user.Rotation().Direction()}
	place(tx, pos, s, user, ctx)
	return placed(ctx)
}

// EncodeNBT ...
func (s CalibratedSculkSensor) EncodeNBT() map[string]any {
	return map[string]any{"id": "CalibratedSculkSensor", "power": int32(s.Power), "last_vibration_frequency": int32(s.Frequency)}
}

// DecodeNBT ...
func (s CalibratedSculkSensor) DecodeNBT(data map[string]any) any {
	if v, ok := data["power"].(int32); ok {
		s.Power = int(v)
	}
	if v, ok := data["last_vibration_frequency"].(int32); ok {
		s.Frequency = int(v)
	}
	return s
}

// back is the face it takes its input from.
func (s CalibratedSculkSensor) back() cube.Face { return s.Facing.Opposite().Face() }

// RedstonePower is the signal while active, to every side but the back (CalibratedSculkSensorBlock::
// setupRedstoneComponent leaves the back out of its allowed connections).
func (s CalibratedSculkSensor) RedstonePower(_ cube.Pos, _ *world.Tx, face cube.Face) int {
	if face == s.back() {
		return 0
	}
	return s.sensor().signal()
}

// RedstoneStrongPower strongly powers the block below while active (Java fallback, as SculkSensor).
func (s CalibratedSculkSensor) RedstoneStrongPower(_ cube.Pos, _ *world.Tx, face cube.Face) int {
	if face != cube.FaceDown {
		return 0
	}
	return s.sensor().signal()
}

// InputStrength is the redstone signal into the sensor's back (CalibratedSculkSensorBlock::getInputStrength),
// read as a repeater reads its input.
func (s CalibratedSculkSensor) InputStrength(pos cube.Pos, tx *world.Tx) int {
	return RedstoneRepeater{Facing: s.Facing.Opposite()}.input(pos, tx)
}

// Tick runs the sensor's vibration listener (SculkSensorBlockActor::tick).
func (s CalibratedSculkSensor) Tick(currentTick int64, pos cube.Pos, tx *world.Tx) {
	sculkListenerTick(tx, pos, currentTick, sculkKindCalibrated)
}

// EntityStepOn posts sculk_touch for an entity standing on the sensor (SculkSensorBlock::onStandOn).
func (CalibratedSculkSensor) EntityStepOn(pos cube.Pos, tx *world.Tx, e world.Entity) {
	sculkTouch(tx, pos, e)
}

// SculkStepper ...
func (CalibratedSculkSensor) SculkStepper() {}

// ScheduledTick ends the active phase and then the cooldown (SculkSensorBlock::tick).
func (s CalibratedSculkSensor) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	sculkSensorScheduledTick(s, pos, tx)
}

func (s CalibratedSculkSensor) sensor() sculkSensorState {
	return sculkSensorState{s.Phase, s.Power, s.Frequency}
}

func (s CalibratedSculkSensor) withSensor(st sculkSensorState) sculkSensorBlock {
	s.Phase, s.Power, s.Frequency = st.Phase, st.Power, st.Frequency
	return s
}

func (CalibratedSculkSensor) sensorRange() float64 { return calibratedSculkSensorRange }
func (CalibratedSculkSensor) activeTicks() int     { return calibratedSculkSensorActiveTicks }

// SculkStepper is a sculk block that reacts to the entities standing on it: sculk sensors post sculk_touch
// (SculkSensorBlock::onStandOn) and shriekers shriek for the player behind the entity
// (SculkShriekerBlock::onStandOn). Mobs call it, besides players.
type SculkStepper interface {
	EntityStepper
	SculkStepper()
}

// sculkSensorState is what plain and calibrated sculk sensors share.
type sculkSensorState struct{ Phase, Power, Frequency int }

// signal is the sensor's redstone signal: its power while active.
func (s sculkSensorState) signal() int {
	if s.Phase != 1 {
		return 0
	}
	return s.Power
}

// sculkSensorBlock is a sculk sensor or a calibrated sculk sensor.
type sculkSensorBlock interface {
	world.Block
	sensor() sculkSensorState
	withSensor(sculkSensorState) sculkSensorBlock
	sensorRange() float64
	activeTicks() int
}

// sculkSensorComparatorOutput is a sensor's comparator reading: the frequency of the vibration it received
// while it is active, 0 otherwise (SculkSensorBlock::getComparatorSignal).
func sculkSensorComparatorOutput(b sculkSensorBlock) int {
	if st := b.sensor(); st.Phase == 1 {
		return st.Frequency
	}
	return 0
}

// sculkTouch is SculkSensorBlock::onStandOn: the entity standing on a sensor posts sculk_touch there, unless
// it is a warden or a projectile.
func sculkTouch(tx *world.Tx, pos cube.Pos, e world.Entity) {
	if !gameevent.Active(tx) || isWarden(e) || gameevent.IsProjectile(tx, e) {
		return
	}
	gameevent.Post(tx, gameevent.SculkTouch, pos.Vec3Centre(), e)
}

// sculkSensorScheduledTick ends the active phase and then the cooldown (SculkSensorBlock::tick).
func sculkSensorScheduledTick(b sculkSensorBlock, pos cube.Pos, tx *world.Tx) {
	st := b.sensor()
	switch st.Phase {
	case 1:
		st.Phase, st.Power = 2, 0
		nb := b.withSensor(st)
		tx.SetBlock(pos, nb, nil)
		tx.ScheduleBlockUpdate(pos, nb, sculkSensorCooldown*time.Second/20)
	case 2:
		if !waterlogged(tx, pos) {
			tx.PlaySound(pos.Vec3(), sculkSound{event: "power.off.sculk_sensor"})
		}
		st.Phase = 0
		tx.SetBlock(pos, b.withSensor(st), nil)
	}
}

// sculkSensorActivate is SculkSensorBlock::setActivePhase: active with power, a click, the amethyst blocks
// next to it resonating, and the tendrils event.
func sculkSensorActivate(b sculkSensorBlock, tx *world.Tx, pos cube.Pos, power, frequency int, src, owner world.Entity) {
	nb := b.withSensor(sculkSensorState{Phase: 1, Power: power, Frequency: frequency})
	tx.SetBlock(pos, nb, nil)
	if !waterlogged(tx, pos) {
		tx.PlaySound(pos.Vec3(), sculkSound{event: "power.on.sculk_sensor"})
	}
	resonate(tx, pos, frequency, src)
	gameevent.PostContext(tx, gameevent.SculkSensorTendrilsClicking, pos.Vec3Centre(), gameevent.Context{Source: src, Owner: owner})
	tx.ScheduleBlockUpdate(pos, nb, time.Duration(b.activeTicks())*time.Second/20)
}

// resonate is the part of SculkSensorBlock::setActivePhase that calls AmethystBlockUtils::resonateVibration
// for each amethyst block next to the sensor: the block re-emits the vibration as resonate_n at its own
// position, n being the frequency the sensor received, so that sensors further away hear it. BDS has no
// body for resonateVibration in the dump: the event and the resonate sound, pitched by the frequency, follow
// Java's SculkSensorBlock.tryResonateVibration (Java fallback: no Bedrock data).
func resonate(tx *world.Tx, pos cube.Pos, frequency int, src world.Entity) {
	ev, ok := gameevent.Resonate(frequency)
	if !ok {
		return
	}
	for _, f := range cube.Faces() {
		at := pos.Side(f)
		if _, ok := tx.Block(at).(Amethyst); !ok {
			continue
		}
		gameevent.Post(tx, ev, at.Vec3Centre(), src)
		tx.PlaySound(at.Vec3Centre(), sound.Custom{Name: "resonate.amethyst_block", Volume: 1, Pitch: resonancePitch[frequency]})
	}
}

// resonancePitch is Java's SculkSensorBlock.RESONANCE_PITCH_BEND: the note block pitch of a tone for each
// frequency.
var resonancePitch = func() (p [16]float64) {
	tones := [16]int{0, 0, 2, 4, 6, 7, 9, 10, 12, 14, 15, 18, 19, 21, 22, 24}
	for i, n := range tones {
		p[i] = math.Pow(2, float64(n-12)/12)
	}
	return p
}()

// EncodeBlock ...
func (s SculkShrieker) EncodeBlock() (string, map[string]any) {
	return "minecraft:sculk_shrieker", map[string]any{"active": s.Active, "can_summon": s.CanSummon}
}

// EncodeItem ...
func (SculkShrieker) EncodeItem() (string, int16) { return "minecraft:sculk_shrieker", 0 }

// Hash ...
func (s SculkShrieker) Hash() (uint64, uint64) {
	return hashSculkShrieker, uint64(boolByte(s.Active)) | uint64(boolByte(s.CanSummon))<<1
}

// Model ...
func (SculkShrieker) Model() world.BlockModel { return model.Slab{} }

// BreakInfo ...
func (s SculkShrieker) BreakInfo() BreakInfo {
	return newBreakInfo(3, alwaysHarvestable, hoeEffective, oneOf(SculkShrieker{})).withXPDropRange(5, 5)
}

// EncodeNBT ...
func (s SculkShrieker) EncodeNBT() map[string]any {
	m := map[string]any{"id": "SculkShrieker"}
	if s.WarningLevel > 0 {
		m["warning_level"] = int32(s.WarningLevel)
	}
	return m
}

// DecodeNBT ...
func (s SculkShrieker) DecodeNBT(data map[string]any) any {
	if v, ok := data["warning_level"].(int32); ok {
		s.WarningLevel = int(v)
	}
	return s
}

// Tick runs the shrieker's vibration listener.
func (s SculkShrieker) Tick(currentTick int64, pos cube.Pos, tx *world.Tx) {
	sculkListenerTick(tx, pos, currentTick, sculkKindShrieker)
}

// EntityStepOn makes the shrieker shriek for the player behind an entity standing on it: the player itself,
// or the player riding the mob on it (SculkShriekerBlock::onStandOn with SculkShriekerBlockActor::
// tryGetPlayerInHierarchy). Projectiles do nothing.
func (s SculkShrieker) EntityStepOn(pos cube.Pos, tx *world.Tx, e world.Entity) {
	if gameevent.IsProjectile(tx, e) {
		return
	}
	if p := playerInHierarchy(tx, e); p != nil {
		s.tryShriek(tx, pos, p)
	}
}

// SculkStepper ...
func (SculkShrieker) SculkStepper() {}

// ScheduledTick ends the shriek and answers it (SculkShriekerBlock::tick, SculkShriekerBlockActor::
// tryRespond): a warning level of 4 summons a warden, a lower one plays the warden's warning sound for
// it, and the players near it are darkened.
func (s SculkShrieker) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if !s.Active {
		return
	}
	level := s.WarningLevel
	s.Active, s.WarningLevel = false, 0
	tx.SetBlock(pos, s, nil)
	if !s.CanSummon || !wardenSpawnsAllowed(tx) || level <= 0 {
		return
	}
	if level < 4 || wardenSummoner == nil || !wardenSummoner(tx, pos) {
		playWardenReply(tx, pos, level)
	}
	darkenAround(tx, pos)
}

// tryShriek is SculkShriekerBlockActor::tryShriek: an idle shrieker shrieks for p. One that can summon
// warns the players near it first and stays silent if that fails (a player's warning is cooling down,
// or a warden is near).
func (s SculkShrieker) tryShriek(tx *world.Tx, pos cube.Pos, p wardenThreatened) {
	if s.Active || spectator(p) {
		return
	}
	s.WarningLevel = 0
	if s.CanSummon && wardenSpawnsAllowed(tx) {
		level, ok := tryWarn(tx, pos, p)
		if !ok {
			return
		}
		s.WarningLevel = level
	}
	s.shriek(tx, pos, p)
}

// shriek is SculkShriekerBlockActorInternal::_shriek.
func (s SculkShrieker) shriek(tx *world.Tx, pos cube.Pos, p world.Entity) {
	s.Active = true
	tx.SetBlock(pos, s, nil)
	tx.ScheduleBlockUpdate(pos, s, sculkShriekerTicks*time.Second/20)
	tx.AddParticle(pos.Vec3(), particle.Shriek{})
	if !waterlogged(tx, pos) {
		tx.PlaySound(pos.Vec3(), sculkSound{event: "block.sculk_shrieker.shriek"})
	}
	gameevent.Post(tx, gameevent.Shriek, pos.Vec3Centre(), p)
}

// sculkSound is a level sound event of the sculk blocks or of the warden.
type sculkSound struct{ event, entity string }

// Play ...
func (sculkSound) Play(*world.World, mgl64.Vec3) {}

// LevelSoundEvent ...
func (s sculkSound) LevelSoundEvent() (event, entityType string, baby bool) {
	return s.event, s.entity, false
}

// waterlogged reports whether pos holds water too (the shriek and clicks are silent then).
func waterlogged(tx *world.Tx, pos cube.Pos) bool {
	_, ok := tx.Liquid(pos)
	return ok
}

func isWarden(e world.Entity) bool {
	return e != nil && e.H().Type().EncodeEntity() == "minecraft:warden"
}

// spectator reports whether e is a player in spectator mode.
func spectator(e world.Entity) bool {
	g, ok := e.(interface{ GameMode() world.GameMode })
	return ok && !g.GameMode().Visible()
}

// --- warden warnings (WardenSpawnTrackerComponent) ---

// wardenThreatened is a player: it has a warden warning level (WardenSpawnTrackerComponent).
type wardenThreatened interface {
	world.Entity
	WardenThreat() (level, cooldown, decreaseTimer int)
	SetWardenThreat(level, cooldown, decreaseTimer int)
	GameMode() world.GameMode
}

// The warning level values (WardenSpawnTrackerComponent).
const (
	wardenMaxThreat     = 4
	wardenThreatCool    = 200
	wardenThreatDecline = 12000
)

// wardenSummoner summons a warden near a shrieker (server/mob/component/boss).
var wardenSummoner func(tx *world.Tx, pos cube.Pos) bool

// SetWardenSummoner sets the function sculk shriekers summon wardens with: it tries to spawn one that
// emerges near pos and reports whether it did (SculkShriekerBlockActorInternal::_trySummonWarden).
func SetWardenSummoner(f func(tx *world.Tx, pos cube.Pos) bool) { wardenSummoner = f }

// wardenThreatTick is WardenSpawnTrackerSystem: each player's warning level drops by one every 12000
// ticks without a warning, and the cooldown between warnings counts down.
func wardenThreatTick(tx *world.Tx) {
	for e := range tx.Players() {
		p, ok := e.(wardenThreatened)
		if !ok {
			continue
		}
		level, cool, decline := p.WardenThreat()
		if level == 0 && cool == 0 {
			continue
		}
		if level > 0 {
			if decline--; decline <= 0 {
				level, decline = max(level-1, 0), wardenThreatDecline
			}
		}
		if cool > 0 {
			cool--
		}
		p.SetWardenThreat(level, cool, decline)
	}
}

// wardenSpawnsAllowed is SpawnChecks::canSpawnWarden: not on peaceful.
func wardenSpawnsAllowed(tx *world.Tx) bool {
	id, ok := world.DifficultyID(tx.World().Difficulty())
	return !ok || id != 0
}

// hasNearbyWarden is WardenSpawnTrackerComponent::hasNearbyWarden: a warden in the 48 block box around
// pos.
func hasNearbyWarden(tx *world.Tx, pos cube.Pos) bool {
	c := pos.Vec3Centre()
	box := cube.Box(c[0]-24, c[1]-24, c[2]-24, c[0]+24, c[1]+24, c[2]+24)
	for e := range tx.EntitiesWithin(box) {
		if isWarden(e) {
			return true
		}
	}
	return false
}

// nearbyPlayers is SculkShriekerBlockActorInternal::_getNearbyPlayers: the players with a warning level
// within radius of the centre of pos, but spectators.
func nearbyPlayers(tx *world.Tx, pos cube.Pos, radius float64) []wardenThreatened {
	c := pos.Vec3Centre()
	var out []wardenThreatened
	for e := range tx.Players() {
		p, ok := e.(wardenThreatened)
		if !ok || spectator(p) || p.Position().Sub(c).LenSqr() >= radius*radius {
			continue
		}
		out = append(out, p)
	}
	return out
}

// tryWarn is SculkShriekerBlockActorInternal::_tryWarn: unless a warden is near, the players within 16
// blocks and the one who set the shrieker off share the highest warning level among them, raised by one
// (to at most 4), with a 200 tick cooldown and 12000 ticks until it declines. It fails while any of them
// is cooling down.
func tryWarn(tx *world.Tx, pos cube.Pos, p wardenThreatened) (int, bool) {
	if hasNearbyWarden(tx, pos) {
		return 0, false
	}
	players := []wardenThreatened{}
	for _, o := range nearbyPlayers(tx, pos, 16) {
		if o.H() != p.H() {
			players = append(players, o)
		}
	}
	players = append(players, p)
	level := 0
	for _, o := range players {
		l, cool, _ := o.WardenThreat()
		if cool > 0 {
			return 0, false
		}
		level = max(level, l)
	}
	level = min(level+1, wardenMaxThreat)
	for _, o := range players {
		o.SetWardenThreat(level, wardenThreatCool, wardenThreatDecline)
	}
	return level, true
}

// playWardenReply plays the warning sound of a level at a random spot within 10 blocks of pos
// (SculkShriekerBlockActorInternal::_playWardenReplySound with WardenSpawnTrackerComponent::
// getSoundEventForThreatLevel: nearby_close, nearby_closer, nearby_closest, then agitated).
func playWardenReply(tx *world.Tx, pos cube.Pos, level int) {
	events := [...]string{"nearby_close", "nearby_closer", "nearby_closest", "agitated"}
	if level < 1 || level > len(events) {
		return
	}
	at := mgl64.Vec3{
		float64(pos[0] + rand.IntN(21) - 10),
		float64(pos[1] + rand.IntN(21) - 10),
		float64(pos[2] + rand.IntN(21) - 10),
	}
	tx.PlaySound(at, sculkSound{event: events[level-1], entity: "minecraft:warden"})
}

// darkenAround is SculkShriekerBlockActorInternal::_addDarknessEffect: darkness for 260 ticks for the
// players within 40 blocks that can be hurt. The radius is Java's SculkShriekerBlockEntity.DARKNESS_RADIUS
// (Java fallback: the BDS decompile loses the argument).
func darkenAround(tx *world.Tx, pos cube.Pos) {
	for _, p := range nearbyPlayers(tx, pos, 40) {
		if !p.GameMode().AllowsTakingDamage() {
			continue
		}
		if l, ok := p.(interface{ AddEffect(effect.Effect) }); ok {
			l.AddEffect(effect.New(effect.Darkness, 1, 260*time.Second/20))
		}
	}
}

// --- the sculk vibration listeners (VibrationListener with SculkSensorVibrationConfig and
// SculkShriekerVibrationConfig) ---

// sculkVibration is a vibration a sculk block selected or has in flight.
type sculkVibration struct {
	ev         gameevent.Event
	pos        mgl64.Vec3
	src, owner *world.EntityHandle
	dist       float64
	tick       int64
}

// sculkKind is the kind of sculk block a listener belongs to.
type sculkKind uint8

const (
	sculkKindSensor sculkKind = iota
	sculkKindCalibrated
	sculkKindShrieker
)

// listenRange is how far the kind of sculk block hears.
func (k sculkKind) listenRange() float64 {
	switch k {
	case sculkKindCalibrated:
		return calibratedSculkSensorRange
	case sculkKindShrieker:
		return sculkShriekerRange
	}
	return sculkSensorRange
}

// sculkListener is the vibration listener state of one sculk block.
type sculkListener struct {
	kind      sculkKind
	seen      int64
	candidate *sculkVibration
	flying    *sculkVibration
	left      int
}

// sculkListeners holds the listening sculk blocks of each world, added by their block entity tick (so
// also after a chunk loads) and dropped once they stop ticking (unloaded) or are gone. Only the world's
// own transactions touch its map.
var sculkListeners sync.Map // *world.World -> map[cube.Pos]*sculkListener

func sculkListenersOf(w *world.World, create bool) map[cube.Pos]*sculkListener {
	if v, ok := sculkListeners.Load(w); ok {
		return v.(map[cube.Pos]*sculkListener)
	}
	if !create {
		return nil
	}
	m := map[cube.Pos]*sculkListener{}
	sculkListeners.Store(w, m)
	return m
}

// sculkListenerTick is VibrationListener::tick for a sculk block: the vibration selected in an earlier
// tick starts flying (with its particle) and the one in flight arrives.
func sculkListenerTick(tx *world.Tx, pos cube.Pos, now int64, kind sculkKind) {
	reg := sculkListenersOf(tx.World(), true)
	l, ok := reg[pos]
	if !ok || l.kind != kind {
		if !ok {
			gameevent.Track(tx.World(), 1)
		}
		l = &sculkListener{kind: kind}
		reg[pos] = l
	}
	l.seen = now
	if l.flying == nil && l.candidate != nil && l.candidate.tick < now {
		v := l.candidate
		l.candidate, l.flying = nil, v
		l.left = int(v.dist)
		tx.AddParticle(cube.PosFromVec3(v.pos).Vec3Centre(), particle.Vibration{Pos: pos.Vec3Centre(), Ticks: l.left})
	}
	if l.flying == nil {
		return
	}
	if l.left--; l.left > 0 {
		return
	}
	v := l.flying
	l.flying = nil
	sculkReceive(tx, pos, v)
}

// sculkHear offers a game event to the sculk blocks in range (VibrationListener::handleGameEvent).
func sculkHear(tx *world.Tx, ev gameevent.Event, pos mgl64.Vec3, ctx gameevent.Context) {
	reg := sculkListenersOf(tx.World(), false)
	if len(reg) == 0 {
		return
	}
	now := tx.CurrentTick()
	at := cube.PosFromVec3(pos)
	for lp, l := range reg {
		if now-l.seen > 2 {
			// Its chunk is no longer ticking: it registers again once it ticks.
			delete(reg, lp)
			gameevent.Track(tx.World(), -1)
			continue
		}
		if l.flying != nil {
			continue
		}
		dist := lp.Vec3().Sub(at.Vec3()).Len()
		if dist > l.kind.listenRange() {
			continue
		}
		if !sculkListens(tx, lp, l, ev, at, ctx) || gameevent.Sneaking(ev, ctx) || ctx.Damped {
			continue
		}
		if vibrationOccluded(tx, pos, lp.Vec3Centre()) {
			continue
		}
		// VibrationSelector::addCandidate: a candidate of an earlier tick stays until it flies (so that an
		// event posted every tick, as sculk_touch, does not keep replacing it); within a tick the closest
		// wins, then the higher frequency.
		if c := l.candidate; c != nil {
			if c.tick != now {
				continue
			}
			if math.Abs(c.dist-dist) <= 0.005 {
				if c.ev.Frequency >= ev.Frequency {
					continue
				}
			} else if dist > c.dist {
				continue
			}
		}
		v := &sculkVibration{ev: ev, pos: pos, dist: dist, tick: now}
		if ctx.Source != nil {
			v.src = ctx.Source.H()
		}
		if ctx.Owner != nil {
			v.owner = ctx.Owner.H()
		}
		l.candidate = v
	}
}

// sculkListens is the config's isValidVibration and shouldListen for the block at lp.
func sculkListens(tx *world.Tx, lp cube.Pos, l *sculkListener, ev gameevent.Event, at cube.Pos, ctx gameevent.Context) bool {
	if l.kind == sculkKindShrieker {
		s, ok := tx.Block(lp).(SculkShrieker)
		return ok && ev == gameevent.SculkSensorTendrilsClicking && !s.Active && playerOf(tx, ctx) != nil
	}
	b, ok := tx.Block(lp).(sculkSensorBlock)
	if !ok || ev.Frequency == 0 || b.sensor().Phase != 0 {
		return false
	}
	if at == lp && (ev == gameevent.BlockDestroy || ev == gameevent.BlockPlace) {
		return false
	}
	if ev == gameevent.SculkTouch && at != lp {
		return false
	}
	// CalibratedSculkSensorVibrationConfig::shouldListen: with a signal into its back, only the frequency
	// equal to it.
	if c, ok := b.(CalibratedSculkSensor); ok {
		if in := c.InputStrength(lp, tx); in != 0 && in != ev.Frequency {
			return false
		}
	}
	return true
}

// playerOf is SculkShriekerBlockActor::tryGetPlayerInHierarchy for a vibration: the player behind its
// owner (a shooter) or else behind its source.
func playerOf(tx *world.Tx, ctx gameevent.Context) wardenThreatened {
	if ctx.Owner != nil {
		if p := playerInHierarchy(tx, ctx.Owner); p != nil {
			return p
		}
	}
	return playerInHierarchy(tx, ctx.Source)
}

// playerInHierarchy is SculkShriekerBlockActor::tryGetPlayerInHierarchy: e itself if a player, else the
// player controlling it (riding it in the controlling seat, the first), else the player who shot it if a
// projectile. BDS also takes the player who threw an item entity, which item entities do not keep here.
func playerInHierarchy(tx *world.Tx, e world.Entity) wardenThreatened {
	if e == nil {
		return nil
	}
	if p, ok := e.(wardenThreatened); ok {
		return p
	}
	if r, ok := e.(interface{ Passengers() []world.Entity }); ok {
		if ps := r.Passengers(); len(ps) > 0 {
			if p, ok := ps[0].(wardenThreatened); ok {
				return p
			}
		}
	}
	if o, ok := gameevent.ProjectileOwner(tx, e); ok {
		if p, ok := o.(wardenThreatened); ok {
			return p
		}
	}
	return nil
}

// vibrationOccluded is VibrationListener::_isVibrationOccluded: wool or carpet on the line between the
// vibration and the listener blocks it.
func vibrationOccluded(tx *world.Tx, from, to mgl64.Vec3) bool {
	d := to.Sub(from)
	n := int(d.Len() * 2)
	for i := 1; i < n; i++ {
		p := cube.PosFromVec3(from.Add(d.Mul(float64(i) / float64(n))))
		switch tx.Block(p).(type) {
		case Wool, Carpet:
			return true
		}
	}
	return false
}

// sculkReceive is the config's onSignalReceive.
func sculkReceive(tx *world.Tx, lp cube.Pos, v *sculkVibration) {
	var src, owner world.Entity
	if v.src != nil {
		src, _ = v.src.Entity(tx)
	}
	if v.owner != nil {
		owner, _ = v.owner.Entity(tx)
	}
	switch b := tx.Block(lp).(type) {
	case sculkSensorBlock:
		if b.sensor().Phase != 0 {
			return
		}
		// SculkSensorVibrationConfig::onSignalReceive: 15 - floor(distance / range * 15), at least 1.
		power := max(1, 15-int(math.Floor(v.dist/b.sensorRange()*15)))
		sculkSensorActivate(b, tx, lp, power, v.ev.Frequency, src, owner)
	case SculkShrieker:
		if p := playerOf(tx, gameevent.Context{Source: src, Owner: owner}); p != nil {
			b.tryShriek(tx, lp, p)
		}
	}
}
