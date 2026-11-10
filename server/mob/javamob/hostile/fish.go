//go:build crossfly_full

package hostile

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

const (
	codID          = "minecraft:cod"
	salmonID       = "minecraft:salmon"
	pufferfishID   = "minecraft:pufferfish"
	tropicalFishID = "minecraft:tropical_fish"
)

// Fish is the per-mob state of a fish (AbstractFish, AbstractSchoolingFish,
// Salmon, Pufferfish, TropicalFish).
type Fish struct {
	id string
	// fromBucket is AbstractFish.FROM_BUCKET: such fish never despawn.
	fromBucket bool
	// air is the air supply (WaterAnimal.handleAirSupply).
	air int

	// AbstractSchoolingFish.
	leader     *world.EntityHandle
	schoolSize int
	// isSchool is TropicalFish.isSchool: false for a fish of a random
	// (uncommon) variant, which then never takes followers at spawn.
	isSchool bool

	// Salmon.
	salmonSize SalmonSize

	// Pufferfish.
	puffState      int
	inflateCounter int
	deflateTimer   int

	// TropicalFish.
	variant TropicalVariant
}

// fishOf returns the fish state of m.
func fishOf(m *mob.Mob) *Fish {
	return stateOf(m, func() *Fish {
		return &Fish{id: m.Identifier(), air: 300, schoolSize: 1, isSchool: true, salmonSize: SalmonMedium, variant: defaultTropicalVariant}
	})
}

// FishMaxHealth is AbstractFish.createAttributes: 3 health (every fish).
const FishMaxHealth = 3

// FromBucket is AbstractFish.fromBucket.
func (f *Fish) FromBucket() bool { return f.fromBucket }

// SetFromBucket is AbstractFish.setFromBucket (Bucketable): a fish let out
// of a bucket stays.
func (f *Fish) SetFromBucket(v bool) { f.fromBucket = v }

// RemoveWhenFarAway is AbstractFish.removeWhenFarAway.
func (f *Fish) RemoveWhenFarAway(named bool) bool { return !f.fromBucket && !named }

// MaxSpawnClusterSize is AbstractFish.getMaxSpawnClusterSize (8) and
// AbstractSchoolingFish's max school size (Salmon 5).
func (f *Fish) MaxSpawnClusterSize() int {
	if f.id == salmonID {
		return 5
	}
	return 8
}

// schooling reports whether the fish is an AbstractSchoolingFish (cod,
// salmon, tropical fish; not the pufferfish).
func (f *Fish) schooling() bool { return f.id != pufferfishID }

// waterAnimalAir is WaterAnimal.handleAirSupply: out of water the air runs
// out by one each tick and, from -20, the fish takes 2 drowning damage (Paper:
// cause DRYOUT) and the count starts at 0 again; in water it is full (300).
// It reports whether the fish takes the damage now.
func waterAnimalAir(air *int, inWater bool) (hurt bool) {
	if inWater {
		*air = 300
		return false
	}
	*air--
	if *air <= -20 {
		*air = 0
		return true
	}
	return false
}

// FishBaseTick is the air part of WaterAnimal.baseTick: it hurts a fish that
// has been out of water too long.
func FishBaseTick(m *mob.Mob, drown func(m *mob.Mob, dmg float64)) {
	if m.Dead() {
		return
	}
	if waterAnimalAir(&fishOf(m).air, m.Physics().InWater) {
		drown(m, 2)
	}
}

// FishAIStep is the flop of AbstractFish.aiStep: a fish on dry ground hops
// randomly (0.4 up) and plays its flop sound.
func FishAIStep(m *mob.Mob) {
	p := m.Physics()
	if p.InWater || !m.OnGround() || !p.VerticalCollision {
		return
	}
	r := m.Rand()
	m.SetVelocity(m.Vel().Add(mgl64.Vec3{(nextFloat(r)*2 - 1) * 0.05, 0.4, (nextFloat(r)*2 - 1) * 0.05}))
	p.OnGround = false
	m.SyncMovement()
	playSound(m, fishFlopSound(m.Identifier()), 1, voicePitch(r))
}

// voicePitch is LivingEntity.getVoicePitch for an adult.
func voicePitch(r *rand.Rand) float64 { return (nextFloat(r)-nextFloat(r))*0.2 + 1 }

// fishFlopSound is AbstractFish.getFlopSound of each fish.
func fishFlopSound(id string) string {
	switch id {
	case codID:
		return "entity.cod.flop"
	case salmonID:
		return "entity.salmon.flop"
	case pufferfishID:
		return "entity.puffer_fish.flop"
	}
	return "entity.tropical_fish.flop"
}

// FishTravelInWater is AbstractFish.travelInWater: 0.01 of the input, 0.9
// drag, sinking slowly without a target.
func FishTravelInWater(m *mob.Mob, input mgl64.Vec3) {
	v := m.Vel().Add(inputVector(input, 0.01, m.Yaw()))
	v = Hooks.Move(m, v).Mul(0.9)
	if _, ok := m.Target(); !ok {
		v[1] -= 0.005
	}
	m.SetVelocity(v)
}

// fishMoveControl is AbstractFish.FishMoveControl: buoyant when the eyes are
// in water; swims straight at the wanted position, the speed easing in.
type fishMoveControl struct {
	wanted   mgl64.Vec3
	speedMod float64
	moveTo   bool
}

func (c *fishMoveControl) SetWantedPosition(pos mgl64.Vec3, speed float64) {
	c.wanted, c.speedMod, c.moveTo = pos, speed, true
}
func (c *fishMoveControl) HasWanted() bool            { return c.moveTo }
func (c *fishMoveControl) WantedPosition() mgl64.Vec3 { return c.wanted }
func (c *fishMoveControl) Speed() float64             { return c.speedMod }
func (c *fishMoveControl) Strafe(float64, float64)    {}
func (c *fishMoveControl) Stop()                      { c.moveTo = false }

func (c *fishMoveControl) Tick(m *mob.Mob) {
	if m.Physics().Underwater {
		m.SetVelocity(m.Vel().Add(mgl64.Vec3{0, 0.005}))
	}
	in := m.Input()
	if !c.moveTo || m.Navigation().Done() {
		in.Speed = 0
		return
	}
	target := c.speedMod * m.Attr(mob.AttrMovement)
	in.Speed = lerp(0.125, in.Speed, target)
	d := c.wanted.Sub(m.Pos())
	if d[1] != 0 {
		dd := d.Len()
		m.SetVelocity(m.Vel().Add(mgl64.Vec3{0, in.Speed * (d[1] / dd) * 0.1}))
	}
	if d[0] != 0 || d[2] != 0 {
		yRotD := math.Atan2(d[2], d[0])*180/math.Pi - 90
		m.SetYaw(rotlerp(m.Yaw(), yRotD, 90))
		m.SetBodyYaw(m.Yaw())
	}
}

// canRandomSwim is AbstractFish.canRandomSwim (FishSwimGoal's condition): a
// schooling fish that follows a leader does not swim on its own.
func canRandomSwim(m *mob.Mob) bool {
	f := fishOf(m)
	if !f.schooling() {
		return true
	}
	return !isFollower(m)
}

// Schooling (AbstractSchoolingFish).

// leaderOf returns the living leader of m.
func leaderOf(m *mob.Mob) (*mob.Mob, bool) {
	f := fishOf(m)
	if f.leader == nil {
		return nil, false
	}
	e, ok := f.leader.Entity(m.Tx())
	if !ok {
		return nil, false
	}
	l, ok := mob.Of(e)
	if !ok || l.Dead() {
		return nil, false
	}
	return l, true
}

// isFollower is AbstractSchoolingFish.isFollower.
func isFollower(m *mob.Mob) bool {
	_, ok := leaderOf(m)
	return ok
}

// startFollowing is AbstractSchoolingFish.startFollowing.
func startFollowing(m, leader *mob.Mob) {
	fishOf(m).leader = leader.H()
	fishOf(leader).schoolSize++
}

// stopFollowing is AbstractSchoolingFish.stopFollowing.
func stopFollowing(m *mob.Mob) {
	f := fishOf(m)
	if f.leader == nil {
		return
	}
	if e, ok := f.leader.Entity(m.Tx()); ok {
		if l, ok := mob.Of(e); ok {
			fishOf(l).schoolSize--
		}
	}
	f.leader = nil
}

// hasFollowers is AbstractSchoolingFish.hasFollowers.
func (f *Fish) hasFollowers() bool { return f.schoolSize > 1 }

// canBeFollowed is AbstractSchoolingFish.canBeFollowed.
func (f *Fish) canBeFollowed() bool {
	return f.hasFollowers() && f.schoolSize < f.MaxSpawnClusterSize()
}

// addFollowers is AbstractSchoolingFish.addFollowers: up to the free places
// of the school (counted before skipping the leader itself) start following.
func addFollowers(leader *mob.Mob, fish []*mob.Mob) {
	lf := fishOf(leader)
	free := lf.MaxSpawnClusterSize() - lf.schoolSize
	for i, other := range fish {
		if i >= free {
			break
		}
		if other.H() == leader.H() {
			continue
		}
		startFollowing(other, leader)
	}
}

// sameFishAround returns the fish of m's type within its box grown by r.
func sameFishAround(m *mob.Mob, r float64, filter func(*mob.Mob) bool) []*mob.Mob {
	var out []*mob.Mob
	for e := range m.Tx().EntitiesWithin(m.BBox().Grow(r)) {
		o, ok := mob.Of(e)
		if !ok || o.Identifier() != m.Identifier() || o.Dead() {
			continue
		}
		if filter == nil || filter(o) {
			out = append(out, o)
		}
	}
	return out
}

// SchoolingFishTick is AbstractSchoolingFish.tick: a leader alone (no fish
// of its kind within 8 blocks) now and then (1 in 200 ticks) forgets its
// school.
func SchoolingFishTick(m *mob.Mob) {
	f := fishOf(m)
	if !f.schooling() || !f.hasFollowers() || m.Rand().IntN(200) != 1 {
		return
	}
	if len(sameFishAround(m, 8, nil)) <= 1 {
		f.schoolSize = 1
	}
}

// followFlockLeaderGoal is FollowFlockLeaderGoal (priority 5, no flags):
// every 10 s or so a fish without a leader looks for a school within 8
// blocks with room (or makes one, leading the loners), then follows its
// leader while within 11 blocks.
type followFlockLeaderGoal struct {
	timeToRecalcPath int
	nextStartTick    int
}

func newFollowFlockLeaderGoal(r *rand.Rand) *followFlockLeaderGoal {
	return &followFlockLeaderGoal{nextStartTick: flockNextStartTick(r)}
}

// flockNextStartTick is FollowFlockLeaderGoal.nextStartTick: (200 + n%20)
// halved, n < 200 (so 100..110).
func flockNextStartTick(r *rand.Rand) int { return reducedTickDelay(200 + nextInt(r, 200)%20) }

func (g *followFlockLeaderGoal) Flags() mob.ControlFlags { return 0 }

func (g *followFlockLeaderGoal) CanUse(m *mob.Mob) bool {
	f := fishOf(m)
	switch {
	case f.hasFollowers():
		return false
	case isFollower(m):
		return true
	case g.nextStartTick > 0:
		g.nextStartTick--
		return false
	}
	g.nextStartTick = flockNextStartTick(m.Rand())
	candidates := sameFishAround(m, 8, func(o *mob.Mob) bool { return fishOf(o).canBeFollowed() || !isFollower(o) })
	leader := m
	for _, c := range candidates {
		if fishOf(c).canBeFollowed() {
			leader = c
			break
		}
	}
	var loners []*mob.Mob
	for _, c := range candidates {
		if !isFollower(c) {
			loners = append(loners, c)
		}
	}
	addFollowers(leader, loners)
	return isFollower(m)
}

func (g *followFlockLeaderGoal) CanContinueToUse(m *mob.Mob) bool {
	l, ok := leaderOf(m)
	return ok && distanceSqr(m, l) <= 121
}

func (g *followFlockLeaderGoal) Start(*mob.Mob) { g.timeToRecalcPath = 0 }

func (g *followFlockLeaderGoal) Stop(m *mob.Mob) { stopFollowing(m) }

func (g *followFlockLeaderGoal) Tick(m *mob.Mob) {
	g.timeToRecalcPath--
	if g.timeToRecalcPath > 0 {
		return
	}
	g.timeToRecalcPath = adjustedTickDelay(false, 10)
	if l, ok := leaderOf(m); ok {
		m.Navigation().MoveToEntity(m, l, 1)
	}
}

// SchoolSpawn is AbstractSchoolingFish.finalizeSpawn's group data: the first
// fish of a spawn group leads, the others follow it. leader is nil for the
// first fish; the returned handle is passed to the next ones.
func SchoolSpawn(m *mob.Mob, leader *mob.Mob) *mob.Mob {
	if leader == nil {
		return m
	}
	startFollowing(m, leader)
	return leader
}

// Salmon sizes (Salmon.Variant).

// SalmonSize is Salmon.Variant.
type SalmonSize uint8

const (
	SalmonSmall SalmonSize = iota
	SalmonMedium
	SalmonLarge
)

// Name is the serialised name.
func (s SalmonSize) Name() string { return [...]string{"small", "medium", "large"}[min(int(s), 2)] }

// Scale is Salmon.Variant.boundingBoxScale (0.5, 1, 1.5).
func (s SalmonSize) Scale() float64 { return [...]float64{0.5, 1, 1.5}[min(int(s), 2)] }

// salmonSizeByName is the codec of Salmon.Variant (medium when unknown).
func salmonSizeByName(n string) SalmonSize {
	switch n {
	case "small":
		return SalmonSmall
	case "large":
		return SalmonLarge
	}
	return SalmonMedium
}

// randomSalmonSize is Salmon.finalizeSpawn: small 30, medium 50, large 15.
func randomSalmonSize(r *rand.Rand) SalmonSize {
	n := nextInt(r, 95)
	switch {
	case n < 30:
		return SalmonSmall
	case n < 80:
		return SalmonMedium
	}
	return SalmonLarge
}

// SetSalmonSize sets a salmon's size: Java sees it as the salmon type
// (DATA_TYPE), Bedrock as the scale (javasession maps scale 0.5/1/1.5 to the
// type too), and the box scales with it.
func SetSalmonSize(m *mob.Mob, s SalmonSize) {
	fishOf(m).salmonSize = s
	m.UpdateMeta(func(meta *mob.Meta) { meta.Scale = s.Scale() })
}

// Pufferfish (Pufferfish).

// Puff states.
const (
	PuffSmall = 0
	PuffMid   = 1
	PuffFull  = 2
)

// pufferScale is Pufferfish.getScale.
func pufferScale(state int) float64 {
	switch state {
	case 0:
		return 0.5
	case 1:
		return 0.7
	}
	return 1
}

// SetPuffState is Pufferfish.setPuffState: both clients show it as the
// variant (javasession: PUFF_STATE = Variant) and the box scales.
func SetPuffState(m *mob.Mob, state int) {
	f := fishOf(m)
	f.puffState = state
	m.UpdateMeta(func(meta *mob.Meta) {
		meta.Variant = int32(state)
		s := pufferScale(state)
		meta.Width, meta.Height = 0.7*s, 0.7*s
	})
}

// PuffState is Pufferfish.getPuffState.
func (f *Fish) PuffState() int { return f.puffState }

// pufferStep is the puff part of Pufferfish.tick: while scared
// (inflateCounter > 0) it puffs to mid at once and to full after 40 ticks;
// then it deflates to mid after 60 ticks and to small after 100. It returns
// the new state.
func (f *Fish) pufferStep() int {
	s := f.puffState
	if f.inflateCounter > 0 {
		if s == PuffSmall {
			s = PuffMid
		} else if f.inflateCounter > 40 && s == PuffMid {
			s = PuffFull
		}
		f.inflateCounter++
	} else if s != PuffSmall {
		if f.deflateTimer > 60 && s == PuffFull {
			s = PuffMid
		} else if f.deflateTimer > 100 && s == PuffMid {
			s = PuffSmall
		}
		f.deflateTimer++
	}
	return s
}

// PufferfishTick is the server part of Pufferfish.tick (before the base
// tick).
func PufferfishTick(m *mob.Mob) {
	if m.Dead() {
		return
	}
	f := fishOf(m)
	if s := f.pufferStep(); s != f.puffState {
		SetPuffState(m, s)
		playSound(m, map[bool]string{true: "entity.puffer_fish.blow_up", false: "entity.puffer_fish.blow_out"}[s > f.puffState], 1, voicePitch(m.Rand()))
	}
}

// scaresPufferfish is Pufferfish.SCARY_MOB with the targeting conditions
// (non-combat, no line of sight or invisibility test): any living entity
// but creative players and the types of #not_scary_for_pufferfish.
func scaresPufferfish(e world.Entity) bool {
	if !alive(e) || spectator(e) {
		return false
	}
	if g, ok := e.(gameModer); ok && g.GameMode() == world.GameModeCreative {
		return false
	}
	return !notScaryForPufferfish[entityID(e)]
}

// notScaryForPufferfish is the entity type tag minecraft:not_scary_for_
// pufferfish (26.3).
var notScaryForPufferfish = map[string]bool{
	"minecraft:turtle": true, "minecraft:guardian": true, "minecraft:elder_guardian": true,
	"minecraft:cod": true, "minecraft:pufferfish": true, "minecraft:salmon": true,
	"minecraft:tropical_fish": true, "minecraft:dolphin": true, "minecraft:squid": true,
	"minecraft:glow_squid": true, "minecraft:tadpole": true, "minecraft:nautilus": true,
	"minecraft:zombie_nautilus": true, "minecraft:sulfur_cube": true,
}

// pufferfishPuffGoal is Pufferfish.PufferfishPuffGoal (priority 1, no
// flags): something scary within 2 blocks makes the fish puff up.
type pufferfishPuffGoal struct{}

func (pufferfishPuffGoal) Flags() mob.ControlFlags { return 0 }

func (pufferfishPuffGoal) CanUse(m *mob.Mob) bool {
	for e := range m.Tx().EntitiesWithin(m.BBox().Grow(2)) {
		if e.H() != m.H() && scaresPufferfish(e) {
			if _, living := e.(interface{ Health() float64 }); living {
				return true
			}
		}
	}
	return false
}

func (pufferfishPuffGoal) Start(m *mob.Mob) {
	f := fishOf(m)
	f.inflateCounter = 1
	f.deflateTimer = 0
}

func (pufferfishPuffGoal) Stop(m *mob.Mob) { fishOf(m).inflateCounter = 0 }

// PufferfishTouch is the sting of Pufferfish.aiStep (mobs within 0.3
// blocks) and Pufferfish.playerTouch (players touching it): while puffed, 1
// + state melee damage and Poison for 3 s per state. sting is called for
// players stung (Java shows them ClientboundGameEventPacket.PUFFER_FISH_
// STING).
func PufferfishTouch(m *mob.Mob, target world.Entity, poison func(target world.Entity, ticks int), sting func(world.Entity)) {
	f := fishOf(m)
	if m.Dead() || f.puffState == 0 || !scaresPufferfish(target) {
		return
	}
	if !mobAttack(m, target, float64(1+f.puffState)) {
		return
	}
	if isPlayer(target) && sting != nil {
		sting(target)
	}
	poison(target, 60*f.puffState)
}

// PufferfishTouchTargets are the mobs Pufferfish.aiStep stings: within its
// box grown by 0.3. Players are stung by playerTouch, which the base runs
// for players touching the fish.
func PufferfishTouchTargets(m *mob.Mob) []world.Entity {
	var out []world.Entity
	for e := range m.Tx().EntitiesWithin(m.BBox().Grow(0.3)) {
		if e.H() == m.H() || isPlayer(e) {
			continue
		}
		if _, ok := mob.Of(e); ok {
			out = append(out, e)
		}
	}
	return out
}

// Tropical fish variants (TropicalFish.Variant, Pattern).

// TropicalPattern is TropicalFish.Pattern: the base (0 small, 1 large) and
// the pattern index within it.
type TropicalPattern struct{ Base, Index uint8 }

// PackedID is Pattern.getPackedId.
func (p TropicalPattern) PackedID() int { return int(p.Base) | int(p.Index)<<8 }

// tropicalPatternNames are the patterns in TropicalFish.Pattern order.
var tropicalPatternNames = [12]string{"kob", "sunstreak", "snooper", "dasher", "brinely", "spotty",
	"flopper", "stripey", "glitter", "blockfish", "betty", "clayfish"}

// tropicalPattern returns pattern i of TropicalFish.Pattern.values().
func tropicalPattern(i int) TropicalPattern {
	return TropicalPattern{Base: uint8(i / 6), Index: uint8(i % 6)}
}

// tropicalPatternByName returns the pattern called name (kob if unknown).
func tropicalPatternByName(name string) TropicalPattern {
	for i, n := range tropicalPatternNames {
		if n == name {
			return tropicalPattern(i)
		}
	}
	return tropicalPattern(0)
}

// Name is the serialised name of the pattern.
func (p TropicalPattern) Name() string {
	i := int(p.Base)*6 + int(p.Index)
	if p.Base > 1 || p.Index > 5 {
		return "kob"
	}
	return tropicalPatternNames[i]
}

// TropicalVariant is TropicalFish.Variant: a pattern and two dye colours
// (DyeColor ids, white 0 .. black 15).
type TropicalVariant struct {
	Pattern               TropicalPattern
	BaseColour, PatColour uint8
}

// Packed is Variant.getPackedId (TropicalFish.packVariant).
func (v TropicalVariant) Packed() int32 {
	return int32(v.Pattern.PackedID()&0xffff | int(v.BaseColour)<<16 | int(v.PatColour)<<24)
}

// unpackTropicalVariant is Variant(int packedId).
func unpackTropicalVariant(p int32) TropicalVariant {
	u := uint32(p)
	pat := TropicalPattern{Base: uint8(u & 0xff), Index: uint8(u >> 8 & 0xff)}
	if pat.Base > 1 || pat.Index > 5 {
		pat = TropicalPattern{} // Pattern.byId: KOB when unknown
	}
	return TropicalVariant{Pattern: pat, BaseColour: uint8(u >> 16 & 0xff), PatColour: uint8(u >> 24 & 0xff)}
}

// Java DyeColor ids used by the common variants.
const (
	dyeWhite     = 0
	dyeOrange    = 1
	dyeLightBlue = 3
	dyeYellow    = 4
	dyeLime      = 5
	dyePink      = 6
	dyeGray      = 7
	dyeCyan      = 9
	dyePurple    = 10
	dyeBlue      = 11
	dyeRed       = 14
)

// defaultTropicalVariant is TropicalFish.DEFAULT_VARIANT.
var defaultTropicalVariant = TropicalVariant{Pattern: tropicalPatternByName("kob"), BaseColour: dyeWhite, PatColour: dyeWhite}

func tv(pattern string, base, pat uint8) TropicalVariant {
	return TropicalVariant{Pattern: tropicalPatternByName(pattern), BaseColour: base, PatColour: pat}
}

// CommonTropicalVariants is TropicalFish.COMMON_VARIANTS (the 22 named
// fish), in order.
var CommonTropicalVariants = []TropicalVariant{
	tv("stripey", dyeOrange, dyeGray), tv("flopper", dyeGray, dyeGray), tv("flopper", dyeGray, dyeBlue),
	tv("clayfish", dyeWhite, dyeGray), tv("sunstreak", dyeBlue, dyeGray), tv("kob", dyeOrange, dyeWhite),
	tv("spotty", dyePink, dyeLightBlue), tv("blockfish", dyePurple, dyeYellow), tv("clayfish", dyeWhite, dyeRed),
	tv("spotty", dyeWhite, dyeYellow), tv("glitter", dyeWhite, dyeGray), tv("clayfish", dyeWhite, dyeOrange),
	tv("dasher", dyeCyan, dyePink), tv("brinely", dyeLime, dyeLightBlue), tv("betty", dyeRed, dyeWhite),
	tv("snooper", dyeGray, dyeRed), tv("blockfish", dyeRed, dyeWhite), tv("flopper", dyeWhite, dyeYellow),
	tv("kob", dyeRed, dyeWhite), tv("sunstreak", dyeGray, dyeWhite), tv("dasher", dyeCyan, dyeYellow),
	tv("flopper", dyeYellow, dyeYellow),
}

// randomTropicalVariant is TropicalFish.finalizeSpawn without group data:
// 90% one of the common variants (the group shares it), else a random
// pattern and colours, and the fish swims alone (isSchool false).
func randomTropicalVariant(r *rand.Rand) (v TropicalVariant, school bool) {
	if nextFloat(r) < 0.9 {
		return CommonTropicalVariants[nextInt(r, len(CommonTropicalVariants))], true
	}
	return TropicalVariant{
		Pattern:    tropicalPattern(nextInt(r, 12)),
		BaseColour: uint8(nextInt(r, 16)),
		PatColour:  uint8(nextInt(r, 16)),
	}, false
}

// SetTropicalVariant sets a tropical fish's variant. Both clients show it:
// Bedrock as variant (base), mark variant (pattern), color and color2
// (javasession packs the same four into DATA_ID_TYPE_VARIANT).
func SetTropicalVariant(m *mob.Mob, v TropicalVariant) {
	fishOf(m).variant = v
	m.UpdateMeta(func(meta *mob.Meta) {
		meta.Variant = int32(v.Pattern.Base)
		meta.MarkVariant = int32(v.Pattern.Index)
		meta.Color = v.BaseColour
		meta.Color2 = v.PatColour
	})
}

// TropicalVariant returns the fish's variant.
func (f *Fish) TropicalVariant() TropicalVariant { return f.variant }

// EncodeNBT writes the fish's saved data (AbstractFish FromBucket, Salmon
// type, Pufferfish PuffState, TropicalFish Variant).
func (f *Fish) EncodeNBT(data map[string]any) {
	data["FromBucket"] = boolByte(f.fromBucket)
	switch f.id {
	case salmonID:
		data["type"] = f.salmonSize.Name()
	case pufferfishID:
		data["PuffState"] = int32(f.puffState)
	case tropicalFishID:
		data["Variant"] = f.variant.Packed()
	}
}

// DecodeNBT reads what EncodeNBT wrote.
func (f *Fish) DecodeNBT(m *mob.Mob, data map[string]any) {
	f.fromBucket = nbtBool(data, "FromBucket")
	switch f.id {
	case salmonID:
		n, _ := data["type"].(string)
		SetSalmonSize(m, salmonSizeByName(n))
	case pufferfishID:
		s, _ := nbtInt(data, "PuffState")
		SetPuffState(m, min(s, 2))
	case tropicalFishID:
		v := defaultTropicalVariant
		if p, ok := nbtInt(data, "Variant"); ok {
			v = unpackTropicalVariant(int32(p))
		}
		SetTropicalVariant(m, v)
	}
}

// surfaceWaterAnimalSpawn is WaterAnimal.checkSurfaceWaterAnimalSpawnRules
// (Paper's wateranimal-spawn-height is "default": sea level - 13 .. sea
// level): water below, a water block above.
func surfaceWaterAnimalSpawn(pos cube.Pos, seaLevel int, waterBelow, waterAbove bool) bool {
	return pos[1] >= seaLevel-13 && pos[1] <= seaLevel && waterBelow && waterAbove
}
