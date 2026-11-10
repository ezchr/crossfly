//go:build crossfly_full

package jpets

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/janimal"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// The glue between the shared animal layer (janimal, written against a
// Host) and the Java mob base: petHost is a Host for a pet, animalGoal turns a
// janimal goal into a javamob.Goal.

// Animalish is implemented by the behaviour of every pet with an Animal
// state: it gives the state and makes the baby (getBreedOffspring).
type Animalish interface {
	animal() *janimal.Animal
	// breed is getBreedOffspring + spawnChildFromBreeding (copying variants,
	// owner and collar onto the baby); false when there is no offspring.
	breed(m, partner *javamob.Mob) bool
	// panicking is PathfinderMob.isPanicking.
	panicking() bool
}

// petHost is a janimal.Host over a Java mob.
type petHost struct{ m *javamob.Mob }

func hostOf(m *javamob.Mob) petHost { return petHost{m: m} }

func (h petHost) b() Animalish { return h.m.Behaviour().(Animalish) }

func (h petHost) AnimalState() *janimal.Animal { return h.b().animal() }
func (h petHost) Self() world.Entity           { return h.m }
func (h petHost) Tx() *world.Tx                { return h.m.Tx() }
func (h petHost) Rand() *rand.Rand             { return h.m.Rand() }
func (h petHost) Position() mgl64.Vec3         { return h.m.Position() }
func (h petHost) Alive() bool                  { return !h.m.Dead() }
func (h petHost) OnFire() bool                 { return h.m.IsOnFire() }
func (h petHost) Panicking() bool              { return h.b().panicking() }
func (h petHost) LastDamageType() string       { return lastDamageType(h.m) }

func (h petHost) MoveToEntity(e world.Entity, speed float64) bool {
	return h.m.Navigation().MoveToEntity(h.m, e, speed)
}
func (h petHost) MoveTo(pos mgl64.Vec3, speed float64) bool {
	return h.m.Navigation().MoveTo(h.m, pos, 0, speed)
}
func (h petHost) StopNavigation()      { h.m.Navigation().Stop() }
func (h petHost) NavigationDone() bool { return h.m.Navigation().Done() }
func (h petHost) LookAt(e world.Entity, maxYaw, maxPitch float64) {
	h.m.LookControl().SetLookAtEntity(e, maxYaw, maxPitch)
}
func (h petHost) MaxHeadXRot() float64 { return maxHeadXRot(h.m) }
func (h petHost) MaxHeadYRot() float64 { return h.m.MaxHeadYRot() }
func (h petHost) DefaultRandomPos(xz, y int) (mgl64.Vec3, bool) {
	return h.m.DefaultRandomPos(xz, y)
}

func (h petHost) SameTypeInBox(dx, dy, dz float64) []petHost {
	box := h.m.BBox().GrowVec3(mgl64.Vec3{dx, dy, dz})
	id := entityID(h.m)
	var out []petHost
	for e := range h.m.Tx().EntitiesWithin(box) {
		if entityID(e) != id {
			continue
		}
		if o, ok := javamob.Of(e); ok && !o.Dead() {
			if _, ok := o.Behaviour().(Animalish); ok {
				out = append(out, petHost{m: o})
			}
		}
	}
	return out
}

func (h petHost) NearestPlayer(r float64, pred func(p world.Entity) bool) world.Entity {
	c := javamob.ForNonCombat(r)
	c.IgnoreLineOfSight = true
	if pred != nil {
		c.Selector = func(e world.Entity, _ *world.Tx) bool { return pred(e) }
	}
	p, ok := h.m.NearestPlayer(c)
	if !ok {
		return nil
	}
	return p
}

func (h petHost) TemptRange() float64 { return h.m.Attr(javamob.AttrTemptRange) }

func (h petHost) BroadcastEvent(id byte) { broadcastEvent(h.m, janimal.Event(id)) }
func (h petHost) MobGriefing() bool      { return true }
func (h petHost) MobDrops() bool         { return true }
func (h petHost) DropXP(n int) {
	for _, orb := range entity.NewExperienceOrbs(h.m.Position(), n) {
		h.m.Tx().AddEntity(orb)
	}
}
func (h petHost) BreedOffspring(partner petHost) bool { return h.b().breed(h.m, partner.m) }
func (h petHost) Ate()                                {}

// janimalGoal is the method set of the janimal goals.
type janimalGoal interface {
	CanUse(h petHost) bool
	Flags() janimal.GoalFlags
}

// animalGoal is a janimal goal run by the Java goal selector.
type animalGoal struct{ g janimalGoal }

func wrapGoal(g janimalGoal) javamob.Goal { return animalGoal{g: g} }

func (a animalGoal) CanUse(m *javamob.Mob) bool { return a.g.CanUse(hostOf(m)) }
func (a animalGoal) Flags() javamob.Flags       { return javamob.Flags(a.g.Flags()) }
func (a animalGoal) CanContinueToUse(m *javamob.Mob) bool {
	if c, ok := a.g.(interface{ CanContinueToUse(petHost) bool }); ok {
		return c.CanContinueToUse(hostOf(m))
	}
	return a.g.CanUse(hostOf(m))
}
func (a animalGoal) Start(m *javamob.Mob) {
	if s, ok := a.g.(interface{ Start(petHost) }); ok {
		s.Start(hostOf(m))
	}
}
func (a animalGoal) Stop(m *javamob.Mob) {
	if s, ok := a.g.(interface{ Stop(petHost) }); ok {
		s.Stop(hostOf(m))
	}
}
func (a animalGoal) Tick(m *javamob.Mob) {
	if t, ok := a.g.(interface{ Tick(petHost) }); ok {
		t.Tick(hostOf(m))
	}
}
func (a animalGoal) RequiresUpdateEveryTick() bool {
	if e, ok := a.g.(interface{ RequiresUpdateEveryTick() bool }); ok {
		return e.RequiresUpdateEveryTick()
	}
	return false
}

// animalFeed is Animal.mobInteract for a pet: its food makes an adult fall
// in love (hearts) or a baby grow; the item is used up (not in creative).
func animalFeed(m *javamob.Mob, a *janimal.Animal, p *player.Player, held item.Stack, isFood bool) (item.Stack, bool) {
	res, boundary := a.Feed(isFood, p.UUID())
	if !res.Used() {
		return held, false
	}
	if res == janimal.FeedLove {
		broadcastEvent(m, janimal.Event(janimal.EventLoveHearts))
	}
	if boundary {
		m.SetBaby(a.Baby())
	}
	m.PlaySound("eat")
	return consumeOne(p, held), true
}

// animalTick is the AgeableMob/Animal part of aiStep: age and love.
func animalTick(m *javamob.Mob, a *janimal.Animal) {
	a.BeforeAI()
	if t := a.Tick(!m.Dead()); t.Boundary {
		m.SetBaby(a.Baby())
	}
}

// foodNutrition is FoodProperties.nutrition of the foods pets eat.
func foodNutrition(name string, def float64) float64 {
	if n, ok := nutrition[name]; ok {
		return n
	}
	return def
}

var nutrition = map[string]float64{
	"minecraft:beef": 3, "minecraft:chicken": 2, "minecraft:cooked_beef": 8, "minecraft:cooked_chicken": 6,
	"minecraft:cooked_mutton": 6, "minecraft:cooked_porkchop": 8, "minecraft:cooked_rabbit": 5, "minecraft:mutton": 2,
	"minecraft:porkchop": 3, "minecraft:rabbit": 3, "minecraft:rotten_flesh": 4, "minecraft:cod": 2,
	"minecraft:cooked_cod": 5, "minecraft:salmon": 2, "minecraft:cooked_salmon": 6, "minecraft:tropical_fish": 1,
	"minecraft:pufferfish": 1, "minecraft:rabbit_stew": 10, "minecraft:sweet_berries": 2, "minecraft:glow_berries": 2,
}

// maxHeadXRot is Mob.getMaxHeadXRot: 40, or the behaviour's own.
func maxHeadXRot(m *javamob.Mob) float64 {
	if h, ok := m.Behaviour().(javamob.HeadLimiter); ok {
		return h.MaxHeadXRot()
	}
	return 40
}

// spawnCtx is variant.SpawnContext at a mob's block.
type spawnCtx struct {
	tx  *world.Tx
	pos cube.Pos
}

func spawnContextAt(m *javamob.Mob) SpawnContext {
	return spawnCtx{tx: m.Tx(), pos: cube.PosFromVec3(m.Position())}
}

func (c spawnCtx) Biome() string {
	return "minecraft:" + c.tx.Biome(c.pos).String()
}

// InStructure: Crossfly does not keep structure starts, so no position is in
// a structure (witch huts give no all black cats).
func (c spawnCtx) InStructure(string) bool { return false }

// MoonBrightness is DimensionType.MOON_BRIGHTNESS_PER_PHASE of the moon
// phase (day % 8).
func (c spawnCtx) MoonBrightness() float64 {
	day := c.tx.World().Time() / 24000
	return []float64{1, 0.75, 0.5, 0.25, 0, 0.25, 0.5, 0.75}[((day%8)+8)%8]
}

var _ mob.Component = mountComp{}
