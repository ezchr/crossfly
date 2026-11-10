//go:build crossfly_full

package jpets

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The goals of Fox (its inner classes).

// foxFloat is Fox.FoxFloatGoal: swim up in water deeper than 0.25 or in
// lava, forgetting every state.
type foxFloat struct {
	*goal.Float
	f *Fox
}

func (g *foxFloat) CanUse(m *javamob.Mob) bool {
	return m.FluidHeight(false) > 0.25 || m.InLava()
}
func (g *foxFloat) Start(m *javamob.Mob) { g.f.clearStates(m) }

// foxFaceplant is Fox.FaceplantGoal (LOOK, JUMP, MOVE): stuck head first in
// snow for 40 ticks.
type foxFaceplant struct {
	f         *Fox
	countdown int
}

func (g *foxFaceplant) Flags() javamob.Flags {
	return javamob.FlagLook | javamob.FlagJump | javamob.FlagMove
}
func (g *foxFaceplant) CanUse(*javamob.Mob) bool { return g.f.flag(foxFaceplanted) }
func (g *foxFaceplant) CanContinueToUse(m *javamob.Mob) bool {
	return g.CanUse(m) && g.countdown > 0
}
func (g *foxFaceplant) Start(*javamob.Mob)  { g.countdown = javamob.ReducedTickDelay(40) }
func (g *foxFaceplant) Stop(m *javamob.Mob) { g.f.setFlag(m, foxFaceplanted, false) }
func (g *foxFaceplant) Tick(*javamob.Mob)   { g.countdown-- }

// foxBreed is Fox.FoxBreedGoal: both foxes stop what they do first.
type foxBreed struct {
	animalGoal
	f *Fox
}

func (g *foxBreed) Start(m *javamob.Mob) {
	g.f.clearStates(m)
	g.animalGoal.Start(m)
}

// foxFollowParent is Fox.FoxFollowParentGoal: not while defending.
type foxFollowParent struct {
	animalGoal
	f *Fox
}

func (g *foxFollowParent) CanUse(m *javamob.Mob) bool {
	return !g.f.flag(foxDefending) && g.animalGoal.CanUse(m)
}
func (g *foxFollowParent) CanContinueToUse(m *javamob.Mob) bool {
	return !g.f.flag(foxDefending) && g.animalGoal.CanContinueToUse(m)
}
func (g *foxFollowParent) Start(m *javamob.Mob) {
	g.f.clearStates(m)
	g.animalGoal.Start(m)
}

// foxMelee is Fox.FoxMeleeAttackGoal: not while sitting, asleep, crouching
// or stuck in snow.
type foxMelee struct {
	*goal.MeleeAttack
	f *Fox
}

func (g *foxMelee) CanUse(m *javamob.Mob) bool {
	f := g.f
	return !f.flag(foxSitting) && !f.flag(foxSleeping) && !f.flag(foxCrouching) && !f.flag(foxFaceplanted) && g.MeleeAttack.CanUse(m)
}

// foxLookAtPlayer is Fox.FoxLookAtPlayerGoal: not stuck in snow or
// interested in prey.
type foxLookAtPlayer struct {
	*goal.LookAt
	f *Fox
}

func (g *foxLookAtPlayer) CanUse(m *javamob.Mob) bool {
	return g.LookAt.CanUse(m) && !g.f.flag(foxFaceplanted) && !g.f.flag(foxInterested)
}
func (g *foxLookAtPlayer) CanContinueToUse(m *javamob.Mob) bool {
	return g.LookAt.CanContinueToUse(m) && !g.f.flag(foxFaceplanted) && !g.f.flag(foxInterested)
}

// hasShelter is FoxBehaviorGoal.hasShelter: no sky above the fox's head and a
// walk target value of 0 or more.
func (f *Fox) hasShelter(m *javamob.Mob) bool {
	p := m.Position()
	pos := cube.PosFromVec3(mgl64.Vec3{p[0], m.BBox().Max()[1], p[2]})
	return !path.CanSeeSky(m.Tx(), pos) && m.WalkTargetValue(pos) >= 0
}

// alertable is FoxBehaviorGoal.alertable: something within 12 x 6 x 12 the
// fox watches out for (FoxAlertableEntitiesSelector).
func (f *Fox) alertable(m *javamob.Mob) bool {
	c := javamob.ForCombat(12)
	c.IgnoreLineOfSight = true
	c.Selector = func(e world.Entity, _ *world.Tx) bool { return f.alertableEntity(e) }
	_, ok := m.NearestEntity(m.BBox().GrowVec3(mgl64.Vec3{12, 6, 12}), func(e world.Entity) bool {
		_, ok := e.(entity.Living)
		return ok
	}, c)
	return ok
}

// alertableEntity is FoxAlertableEntitiesSelector.test.
func (f *Fox) alertableEntity(e world.Entity) bool {
	switch entityID(e) {
	case "minecraft:fox":
		return false
	case "minecraft:chicken", "minecraft:rabbit":
		return true
	}
	if jm, ok := javamob.Of(e); ok {
		if jm.HasFamily("monster") {
			return true
		}
		if t, ok := jm.Behaviour().(interface{ IsTame() bool }); ok {
			return !t.IsTame()
		}
	}
	if p, ok := e.(*player.Player); ok {
		if p.GameMode() == world.GameModeSpectator || p.GameMode().CreativeInventory() {
			return false
		}
		_, asleep := p.Sleeping()
		return !f.trusts(e) && !asleep && !p.Sneaking()
	}
	return !f.trusts(e)
}

// foxSleep is Fox.SleepGoal (MOVE, LOOK, JUMP): a still fox sleeps by day
// in the shade when nothing is about.
type foxSleep struct {
	f         *Fox
	countdown int
}

func (g *foxSleep) Flags() javamob.Flags {
	return javamob.FlagMove | javamob.FlagLook | javamob.FlagJump
}

func (g *foxSleep) canSleep(m *javamob.Mob) bool {
	if g.countdown > 0 {
		g.countdown--
		return false
	}
	return brightOutside(m.Tx()) && g.f.hasShelter(m) && !g.f.alertable(m)
}

func (g *foxSleep) CanUse(m *javamob.Mob) bool {
	if m.Zza() != 0 || m.MoveSpeed() != 0 && !m.Navigation().Done() {
		return false
	}
	return g.canSleep(m) || g.f.flag(foxSleeping)
}
func (g *foxSleep) CanContinueToUse(m *javamob.Mob) bool { return g.canSleep(m) }

func (g *foxSleep) Stop(m *javamob.Mob) {
	g.countdown = m.Rand().IntN(javamob.ReducedTickDelay(140))
	g.f.clearStates(m)
}

func (g *foxSleep) Start(m *javamob.Mob) {
	f := g.f
	f.setFlag(m, foxSitting, false)
	f.setFlag(m, foxCrouching, false)
	f.setFlag(m, foxInterested, false)
	m.SetJumping(false)
	f.setFlag(m, foxSleeping, true)
	m.Navigation().Stop()
	p := m.Position()
	m.MoveControl().SetWantedPosition(p[0], p[1], p[2], 0)
}

// brightOutside is Level.isBrightOutside: daytime (sky darkness below 4) in
// a dimension with a day cycle.
func brightOutside(tx *world.Tx) bool {
	t := tx.World().Time() % 24000
	if t < 0 {
		t += 24000
	}
	// Day from 23460 (sunrise) to 12540 (sunset) in vanilla's sky darkness.
	return (t >= 23460 || t < 12540) && !tx.Thundering()
}

// foxSeekShelter is Fox.SeekShelterGoal (FleeSunGoal): out of the sun by
// day (every 100 ticks), or into cover in a thunderstorm.
type foxSeekShelter struct {
	f        *Fox
	speed    float64
	interval int
	wanted   mgl64.Vec3
}

func (g *foxSeekShelter) Flags() javamob.Flags { return javamob.FlagMove }

func (g *foxSeekShelter) CanUse(m *javamob.Mob) bool {
	if _, ok := m.Target(); ok || g.f.flag(foxSleeping) {
		return false
	}
	pos := cube.PosFromVec3(m.Position())
	if m.Tx().Thundering() && path.CanSeeSky(m.Tx(), pos) {
		return g.setWantedPos(m)
	}
	if g.interval > 0 {
		g.interval--
		return false
	}
	g.interval = 100
	return brightOutside(m.Tx()) && path.CanSeeSky(m.Tx(), pos) && g.setWantedPos(m)
}

// setWantedPos is FleeSunGoal.setWantedPos/getHidePos: ten tries within
// 10 x 3 x 10 for a block without sky and a negative walk target value.
func (g *foxSeekShelter) setWantedPos(m *javamob.Mob) bool {
	r := m.Rand()
	base := cube.PosFromVec3(m.Position())
	for range 10 {
		p := base.Add(cube.Pos{r.IntN(20) - 10, r.IntN(6) - 3, r.IntN(20) - 10})
		if !path.CanSeeSky(m.Tx(), p) && m.WalkTargetValue(p) < 0 {
			g.wanted = mgl64.Vec3{float64(p[0]) + 0.5, float64(p[1]), float64(p[2]) + 0.5}
			return true
		}
	}
	return false
}

func (g *foxSeekShelter) CanContinueToUse(m *javamob.Mob) bool { return !m.Navigation().Done() }
func (g *foxSeekShelter) Start(m *javamob.Mob) {
	g.f.clearStates(m)
	m.Navigation().MoveTo(m, g.wanted, 0, g.speed)
}

// foxPerch is Fox.PerchAndSearchGoal (MOVE, LOOK): now and then a calm fox
// sits and looks around 2 to 4 times.
type foxPerch struct {
	f                        *Fox
	relX, relZ               float64
	lookTime, looksRemaining int
}

func (g *foxPerch) Flags() javamob.Flags { return javamob.FlagMove | javamob.FlagLook }

func (g *foxPerch) CanUse(m *javamob.Mob) bool {
	_, hurt := m.LastHurtByMob()
	_, target := m.Target()
	f := g.f
	return !hurt && m.Rand().Float32() < 0.02 && !f.flag(foxSleeping) && !target && m.Navigation().Done() &&
		!f.alertable(m) && !f.flag(foxPouncing) && !f.flag(foxCrouching)
}
func (g *foxPerch) CanContinueToUse(*javamob.Mob) bool { return g.looksRemaining > 0 }

func (g *foxPerch) Start(m *javamob.Mob) {
	g.resetLook(m)
	g.looksRemaining = 2 + m.Rand().IntN(3)
	g.f.setFlag(m, foxSitting, true)
	m.Navigation().Stop()
}
func (g *foxPerch) Stop(m *javamob.Mob) { g.f.setFlag(m, foxSitting, false) }

func (g *foxPerch) Tick(m *javamob.Mob) {
	g.lookTime--
	if g.lookTime <= 0 {
		g.looksRemaining--
		g.resetLook(m)
	}
	p := m.Position()
	m.LookControl().SetLookAtWith(mgl64.Vec3{p[0] + g.relX, m.EyeY(), p[2] + g.relZ}, m.MaxHeadYRot(), maxHeadXRot(m))
}

func (g *foxPerch) resetLook(m *javamob.Mob) {
	a := math.Pi * 2 * m.Rand().Float64()
	g.relX, g.relZ = math.Cos(a), math.Sin(a)
	g.lookTime = javamob.ReducedTickDelay(80 + m.Rand().IntN(20))
}

// foxStalk is Fox.StalkPreyGoal (MOVE, LOOK): creep towards a chicken or
// rabbit until 6 blocks off, then crouch, interested.
type foxStalk struct{ f *Fox }

func (g *foxStalk) Flags() javamob.Flags { return javamob.FlagMove | javamob.FlagLook }

func (g *foxStalk) CanUse(m *javamob.Mob) bool {
	f := g.f
	if f.flag(foxSleeping) {
		return false
	}
	t, ok := m.Target()
	if !ok || dead(t) {
		return false
	}
	id := entityID(t)
	return (id == "minecraft:chicken" || id == "minecraft:rabbit") && distSq(m.Position(), t.Position()) > 36 &&
		!f.flag(foxCrouching) && !f.flag(foxInterested) && !m.Jumping()
}

func (g *foxStalk) Start(m *javamob.Mob) {
	g.f.setFlag(m, foxSitting, false)
	g.f.setFlag(m, foxFaceplanted, false)
}

func (g *foxStalk) Stop(m *javamob.Mob) {
	t, ok := m.Target()
	if ok && isPathClear(m, t) {
		g.f.setFlag(m, foxInterested, true)
		g.f.setFlag(m, foxCrouching, true)
		m.Navigation().Stop()
		m.LookControl().SetLookAtEntity(t, m.MaxHeadYRot(), maxHeadXRot(m))
		return
	}
	g.f.setFlag(m, foxInterested, false)
	g.f.setFlag(m, foxCrouching, false)
}

func (g *foxStalk) Tick(m *javamob.Mob) {
	t, ok := m.Target()
	if !ok {
		return
	}
	m.LookControl().SetLookAtEntity(t, m.MaxHeadYRot(), maxHeadXRot(m))
	if distSq(m.Position(), t.Position()) <= 36 {
		g.f.setFlag(m, foxInterested, true)
		g.f.setFlag(m, foxCrouching, true)
		m.Navigation().Stop()
		return
	}
	m.Navigation().MoveToEntity(m, t, 1.5)
}

// foxPounce is Fox.FoxPounceGoal (a JumpGoal, not interruptible): a fully
// crouched fox leaps at its prey (0.8 horizontal, 0.9 up) and bites it
// within 2 blocks; landing head first in snow it gets stuck.
type foxPounce struct{ f *Fox }

func (g *foxPounce) Flags() javamob.Flags  { return javamob.FlagJump | javamob.FlagMove }
func (g *foxPounce) IsInterruptable() bool { return false }

func (g *foxPounce) CanUse(m *javamob.Mob) bool {
	if g.f.crouchAmount != 5 {
		return false
	}
	t, ok := m.Target()
	if !ok || dead(t) {
		return false
	}
	if moving(t) {
		return false // the target moves off its facing (getMotionDirection != getDirection)
	}
	if !isPathClear(m, t) {
		m.Navigation().CreatePathToEntity(m, t, 0)
		g.f.setFlag(m, foxCrouching, false)
		g.f.setFlag(m, foxInterested, false)
		return false
	}
	return true
}

func (g *foxPounce) CanContinueToUse(m *javamob.Mob) bool {
	t, ok := m.Target()
	if !ok || dead(t) {
		return false
	}
	vy := m.Velocity()[1]
	return (vy*vy >= float64(float32(0.05)) || math.Abs(m.XRot()) >= 15 || !m.OnGround()) && !g.f.flag(foxFaceplanted)
}

func (g *foxPounce) Start(m *javamob.Mob) {
	m.SetJumping(true)
	g.f.setFlag(m, foxPouncing, true)
	g.f.setFlag(m, foxInterested, false)
	if t, ok := m.Target(); ok {
		m.LookControl().SetLookAtEntity(t, 60, 30)
		d := t.Position().Sub(m.Position())
		if d.Len() > 0 {
			d = d.Normalize()
		}
		m.SetVelocity(m.Velocity().Add(mgl64.Vec3{d[0] * 0.8, 0.9, d[2] * 0.8}))
	}
	m.Navigation().Stop()
}

func (g *foxPounce) Stop(m *javamob.Mob) {
	g.f.setFlag(m, foxCrouching, false)
	g.f.crouchAmount = 0
	g.f.setFlag(m, foxInterested, false)
	g.f.setFlag(m, foxPouncing, false)
}

func (g *foxPounce) Tick(m *javamob.Mob) {
	t, ok := m.Target()
	if ok {
		m.LookControl().SetLookAtEntity(t, 60, 30)
	}
	if !g.f.flag(foxFaceplanted) {
		v := m.Velocity()
		if v[1]*v[1] < float64(float32(0.03)) && m.XRot() != 0 {
			m.SetXRot(rotLerp(0.2, m.XRot(), 0))
		} else {
			h := math.Hypot(v[0], v[2])
			bias := 1.0
			if m.Jumping() && v[1] > 0 {
				bias = 6.5
			}
			by := v[1] * bias
			if l := math.Hypot(h, by); l > 1e-5 {
				sign := 1.0
				if -by < 0 {
					sign = -1
				}
				m.SetXRot(sign * math.Acos(h/l) * 180 / math.Pi)
			}
		}
	}
	if ok && t.Position().Sub(m.Position()).Len() <= 2 {
		m.DoHurtTarget(t)
		return
	}
	if m.XRot() > 0 && m.OnGround() && m.Velocity()[1] != 0 {
		if _, snow := m.Tx().Block(cube.PosFromVec3(m.Position())).(block.Snow); snow {
			m.SetXRot(60)
			m.SetTarget(nil)
			g.f.setFlag(m, foxFaceplanted, true)
		}
	}
}

// moving reports whether e walks (its motion direction differs from its
// facing as far as the pounce check can tell: a moving target).
func moving(e world.Entity) bool {
	if v, ok := e.(interface{ Velocity() mgl64.Vec3 }); ok {
		h := v.Velocity()
		return h[0]*h[0]+h[2]*h[2] > 1e-4
	}
	return false
}

func rotLerp(a, from, to float64) float64 {
	return from + a*wrap180(to-from)
}

func wrap180(a float64) float64 {
	a = math.Mod(a, 360)
	if a >= 180 {
		a -= 360
	}
	if a < -180 {
		a += 360
	}
	return a
}

// foxSearchItems is Fox.FoxSearchForItemsGoal (MOVE): an empty-mouthed calm
// fox walks to items within 8 blocks.
type foxSearchItems struct{ f *Fox }

func (g *foxSearchItems) Flags() javamob.Flags { return javamob.FlagMove }

func (g *foxSearchItems) items(m *javamob.Mob) []world.Entity {
	var out []world.Entity
	for _, e := range entitiesTouching(m.Tx(), m.BBox().Grow(8)) {
		if ent, ok := e.(*entity.Ent); ok {
			if ib, ok := ent.Behaviour().(*entity.ItemBehaviour); ok && ib.PickupReady() {
				out = append(out, e)
			}
		}
	}
	return out
}

func (g *foxSearchItems) CanUse(m *javamob.Mob) bool {
	if !m.ItemBySlot(javamob.SlotMainHand).Empty() {
		return false
	}
	_, t := m.Target()
	_, h := m.LastHurtByMob()
	if t || h || !g.f.canMove() || m.Rand().IntN(javamob.ReducedTickDelay(10)) != 0 {
		return false
	}
	return len(g.items(m)) > 0
}

func (g *foxSearchItems) Start(m *javamob.Mob) { g.Tick(m) }
func (g *foxSearchItems) Tick(m *javamob.Mob) {
	if !m.ItemBySlot(javamob.SlotMainHand).Empty() {
		return
	}
	if items := g.items(m); len(items) > 0 {
		m.Navigation().MoveToEntity(m, items[0], float64(float32(1.2)))
	}
}

// foxBerries is FoxEatBerriesGoal.isValidTarget: a ripe sweet berry bush
// (age 2 or more) or cave vines with glow berries.
func foxBerries(tx *world.Tx, pos cube.Pos) bool {
	switch b := tx.Block(pos).(type) {
	case block.SweetBerryBush:
		return b.Age >= 2
	case block.CaveVines:
		return b.Berries
	}
	return false
}

// foxEatBerriesHooks are FoxEatBerriesGoal's overrides: wait 40 ticks next
// to the bush, then pick: sweet berries (1-2, +1 when fully grown), one into
// the mouth if empty; glow berries as a player picks them.
func foxEatBerriesHooks(f *Fox) moveToBlockHooks {
	var waited int
	var target cube.Pos
	return moveToBlockHooks{
		canUse: func(*javamob.Mob) bool { return !f.flag(foxSleeping) },
		start: func(m *javamob.Mob) {
			waited = 0
			f.setFlag(m, foxSitting, false)
		},
		tickWithPos: func(m *javamob.Mob, reached bool, pos cube.Pos) {
			target = pos
			if reached {
				if waited >= 40 {
					foxPickBerries(m, f, target)
				} else {
					waited++
				}
			} else if m.Rand().Float32() < 0.05 {
				m.PlaySound("sniff")
			}
		},
	}
}

func foxPickBerries(m *javamob.Mob, f *Fox, pos cube.Pos) {
	tx := m.Tx()
	switch b := tx.Block(pos).(type) {
	case block.SweetBerryBush:
		if b.Age < 2 {
			return
		}
		count := 1 + m.Rand().IntN(2)
		if b.Age == 3 {
			count++
		}
		berries, _ := world.ItemByName("minecraft:sweet_berries", 0)
		if m.ItemBySlot(javamob.SlotMainHand).Empty() {
			m.SetItemSlot(javamob.SlotMainHand, item.NewStack(berries, 1))
			count--
		}
		if count > 0 {
			tx.AddEntity(tx.World().EntityRegistry().Config().Item(world.EntitySpawnOpts{Position: posVec(pos).Add(mgl64.Vec3{0.5, 0.5, 0.5})}, item.NewStack(berries, count)))
		}
		b.Age = 1
		tx.SetBlock(pos, b, nil)
		m.PlaySound("pick_berries")
	case block.CaveVines:
		if !b.Berries {
			return
		}
		b.Berries, b.Body = false, false
		tx.SetBlock(pos, b, nil)
		glow, _ := world.ItemByName("minecraft:glow_berries", 0)
		tx.AddEntity(tx.World().EntityRegistry().Config().Item(world.EntitySpawnOpts{Position: posVec(pos).Add(mgl64.Vec3{0.5, 0.5, 0.5})}, item.NewStack(glow, 1)))
	}
}

// foxDefendTrusted is Fox.DefendTrustedTargetGoal: whoever last hurt a
// player the fox trusts becomes its target; it growls, wakes up and
// defends.
type foxDefendTrusted struct {
	goal.Target
	f         *Fox
	candidate *world.EntityHandle
	timestamp int64
}

func (g *foxDefendTrusted) CanUse(m *javamob.Mob) bool {
	if m.Rand().IntN(javamob.ReducedTickDelay(10)) != 0 {
		return false
	}
	for p := range m.Tx().Players() {
		if !g.f.trusts(p) {
			continue
		}
		by, ts, ok := lastHurtByMobOf(p)
		if !ok || ts == g.timestamp {
			return false
		}
		t, ok := by.Entity(m.Tx())
		if !ok || g.f.trusts(t) || !javamob.ForCombat(0).Test(m, t) {
			return false
		}
		g.candidate = by
		return true
	}
	return false
}

func (g *foxDefendTrusted) Start(m *javamob.Mob) {
	if t, ok := g.candidate.Entity(m.Tx()); ok {
		m.SetTarget(t)
	}
	for p := range m.Tx().Players() {
		if g.f.trusts(p) {
			_, g.timestamp, _ = lastHurtByMobOf(p)
		}
	}
	m.PlaySound("mad")
	g.f.setFlag(m, foxDefending, true)
	g.f.setFlag(m, foxSleeping, false)
	g.Target.Start(m)
}

func (g *foxDefendTrusted) Stop(m *javamob.Mob) {
	g.f.setFlag(m, foxDefending, false) // Fox.setTarget(null)
	g.Target.Stop(m)
}
