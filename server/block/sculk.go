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
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/gameevent"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

// The sculk sensor and shrieker only matter with the mobs (the shrieker summons wardens): they register in
// Crossfly Full with the mob blocks, with the game event listener that lets them hear vibrations and the
// tick of the players' warden warning levels (WardenSpawnTrackerSystem).
func init() {
	mobBlocks = append(mobBlocks, func() {
		for phase := 0; phase <= 2; phase++ {
			world.RegisterBlock(SculkSensor{Phase: phase})
		}
		for _, active := range []bool{false, true} {
			world.RegisterBlock(SculkShrieker{Active: active})
			world.RegisterBlock(SculkShrieker{Active: active, CanSummon: true})
		}
		world.RegisterItem(SculkSensor{})
		world.RegisterItem(SculkShrieker{})
		gameevent.Register(sculkHear)
		world.RegisterTickHook(wardenThreatTick)
	})
}

var hashSculkSensor, hashSculkShrieker = NextHash(), NextHash()

// Sculk sensor values (SculkSensorBlock, SculkSensorVibrationConfig).
const (
	sculkSensorRange       = 8
	sculkSensorActiveTicks = 30 // the constructor's active time (10 for the calibrated sensor)
	sculkSensorCooldown    = 10
	sculkShriekerRange     = 8
	sculkShriekerTicks     = 90
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
	if s.Phase != 1 {
		return 0
	}
	return s.Power
}

// Tick runs the sensor's vibration listener (SculkSensorBlockActor::tick).
func (s SculkSensor) Tick(currentTick int64, pos cube.Pos, tx *world.Tx) {
	sculkListenerTick(tx, pos, currentTick, false)
}

// EntityStepOn posts sculk_touch for an entity standing on the sensor (SculkSensorBlock::onStandOn), but a
// warden's.
func (SculkSensor) EntityStepOn(pos cube.Pos, tx *world.Tx, e world.Entity) {
	if gameevent.Active(tx) && !isWarden(e) {
		gameevent.Post(tx, gameevent.SculkTouch, pos.Vec3Centre(), e)
	}
}

// ScheduledTick ends the active phase and then the cooldown (SculkSensorBlock::tick).
func (s SculkSensor) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	switch s.Phase {
	case 1:
		s.Phase, s.Power = 2, 0
		tx.SetBlock(pos, s, nil)
		tx.ScheduleBlockUpdate(pos, s, sculkSensorCooldown*time.Second/20)
	case 2:
		if !waterlogged(tx, pos) {
			tx.PlaySound(pos.Vec3(), sculkSound{event: "power.off.sculk_sensor"})
		}
		s.Phase = 0
		tx.SetBlock(pos, s, nil)
	}
}

// activate is SculkSensorBlock::setActivePhase: active with power, a click and the tendrils event.
func (s SculkSensor) activate(tx *world.Tx, pos cube.Pos, power, frequency int, src, owner world.Entity) {
	s.Phase, s.Power, s.Frequency = 1, power, frequency
	tx.SetBlock(pos, s, nil)
	if !waterlogged(tx, pos) {
		tx.PlaySound(pos.Vec3(), sculkSound{event: "power.on.sculk_sensor"})
	}
	gameevent.PostContext(tx, gameevent.SculkSensorTendrilsClicking, pos.Vec3Centre(), gameevent.Context{Source: src, Owner: owner})
	tx.ScheduleBlockUpdate(pos, s, sculkSensorActiveTicks*time.Second/20)
}

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
	sculkListenerTick(tx, pos, currentTick, true)
}

// EntityStepOn makes the shrieker shriek for a player standing on it (SculkShriekerBlock::onStandOn).
func (s SculkShrieker) EntityStepOn(pos cube.Pos, tx *world.Tx, e world.Entity) {
	if p, ok := e.(wardenThreatened); ok {
		s.tryShriek(tx, pos, p)
	}
}

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

// sculkListener is the vibration listener state of one sculk block.
type sculkListener struct {
	shrieker  bool
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
func sculkListenerTick(tx *world.Tx, pos cube.Pos, now int64, shrieker bool) {
	reg := sculkListenersOf(tx.World(), true)
	l, ok := reg[pos]
	if !ok || l.shrieker != shrieker {
		if !ok {
			gameevent.Track(tx.World(), 1)
		}
		l = &sculkListener{shrieker: shrieker}
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
		if dist > sculkSensorRange {
			continue
		}
		if !sculkListens(tx, lp, l, ev, at, ctx) || gameevent.Sneaking(ev, ctx) || ctx.Damped {
			continue
		}
		if vibrationOccluded(tx, pos, lp.Vec3Centre()) {
			continue
		}
		// VibrationSelector::addCandidate: the closest of a tick wins, then the higher frequency.
		if c := l.candidate; c != nil && c.tick == now {
			if c.dist < dist || (c.dist == dist && c.ev.Frequency >= ev.Frequency) {
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
	if l.shrieker {
		s, ok := tx.Block(lp).(SculkShrieker)
		return ok && ev == gameevent.SculkSensorTendrilsClicking && !s.Active && playerOf(ctx) != nil
	}
	s, ok := tx.Block(lp).(SculkSensor)
	if !ok || ev.Frequency == 0 || s.Phase != 0 {
		return false
	}
	if at == lp && (ev == gameevent.BlockDestroy || ev == gameevent.BlockPlace) {
		return false
	}
	return ev != gameevent.SculkTouch || at == lp
}

// playerOf is SculkShriekerBlockActor::tryGetPlayerInHierarchy for a vibration: the owner (a shooter)
// or the source, if a player.
func playerOf(ctx gameevent.Context) wardenThreatened {
	e := ctx.Source
	if ctx.Owner != nil {
		e = ctx.Owner
	}
	if p, ok := e.(wardenThreatened); ok {
		return p
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
	case SculkSensor:
		if b.Phase != 0 {
			return
		}
		power := max(1, 15-int(math.Floor(v.dist/sculkSensorRange*15)))
		b.activate(tx, lp, power, v.ev.Frequency, src, owner)
	case SculkShrieker:
		if p := playerOf(gameevent.Context{Source: src, Owner: owner}); p != nil {
			b.tryShriek(tx, lp, p)
		}
	}
}
