//go:build crossfly_full

package janimal

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Behaviour is a javamob.Behaviour of a Java Animal class (it usually embeds Base).
type Behaviour interface {
	javamob.Behaviour
	AnimalState() *Animal
}

// Breeder is implemented by Behaviours whose class has offspring (getBreedOffspring): it returns the type
// of the baby and a function that copies what the baby takes from its parents (variants, owner...) onto
// it before it is finalized. Without it an animal breeds a baby of its own type with nothing copied.
type Breeder interface {
	BreedOffspring(self, partner *Body) (id string, configure func(baby *javamob.Mob), ok bool)
}

// Eater is Mob.ate (EatBlockGoal).
type Eater interface{ Ate(b *Body) }

// Body is an Animal opened in its transaction: the javamob.Mob and its Animal behaviour. It is the Host the
// janimal goals run on.
type Body struct {
	*javamob.Mob
	B Behaviour
}

// BodyOf returns the Body of e if it is a Java animal.
func BodyOf(e world.Entity) (*Body, bool) {
	m, ok := javamob.Of(e)
	if !ok {
		return nil, false
	}
	return Wrap(m)
}

// Wrap returns m as a Body if its behaviour is an Animal.
func Wrap(m *javamob.Mob) (*Body, bool) {
	b, ok := m.Behaviour().(Behaviour)
	if !ok {
		return nil, false
	}
	return &Body{Mob: m, B: b}, true
}

var _ Host[*Body] = (*Body)(nil)

func (b *Body) AnimalState() *Animal { return b.B.AnimalState() }
func (b *Body) Self() world.Entity   { return b.Mob.Mob }
func (b *Body) Rand() *rand.Rand     { return b.Mob.Rand() }
func (b *Body) Alive() bool          { return !b.Dead() }
func (b *Body) OnFire() bool         { return b.IsOnFire() }

// Panicking is PathfinderMob.isPanicking: a running janimal PanicGoal (or the brain's IS_PANICKING, which
// brain-driven animals report through Panicker).
func (b *Body) Panicking() bool {
	if p, ok := b.B.(interface{ Panicking(b *Body) bool }); ok {
		return p.Panicking(b)
	}
	for _, g := range b.Goals().Running() {
		if r, ok := g.(interface{ PanicRunning() bool }); ok && r.PanicRunning() {
			return true
		}
	}
	return false
}

func (b *Body) LastDamageType() string { return lastDamageType(b.Mob) }

func (b *Body) MoveToEntity(e world.Entity, speed float64) bool {
	return b.Navigation().MoveToEntity(b.Mob, e, speed)
}
func (b *Body) MoveTo(pos mgl64.Vec3, speed float64) bool {
	return b.Navigation().MoveTo(b.Mob, pos, 0, speed)
}
func (b *Body) StopNavigation()      { b.Navigation().Stop() }
func (b *Body) NavigationDone() bool { return b.Navigation().Done() }
func (b *Body) LookAt(e world.Entity, maxYaw, maxPitch float64) {
	b.LookControl().SetLookAtEntity(e, maxYaw, maxPitch)
}
func (b *Body) MaxHeadXRot() float64 {
	if h, ok := b.B.(javamob.HeadLimiter); ok {
		return h.MaxHeadXRot()
	}
	return 40
}

// SameTypeInBox is Level.getEntitiesOfClass(getClass(), box): Java animals of the same type.
func (b *Body) SameTypeInBox(dx, dy, dz float64) []*Body {
	box := b.BBox().GrowVec3(mgl64.Vec3{dx, dy, dz})
	id := b.JavaType().ID
	var out []*Body
	for e := range b.Tx().EntitiesWithin(box) {
		o, ok := BodyOf(e)
		if !ok || o.JavaType().ID != id || o.Dead() {
			continue
		}
		out = append(out, o)
	}
	return out
}

func (b *Body) NearestPlayer(r float64, pred func(world.Entity) bool) world.Entity {
	c := javamob.ForNonCombat(r)
	c.IgnoreLineOfSight = true
	c.Selector = func(e world.Entity, _ *world.Tx) bool { return pred(e) }
	p, ok := b.Mob.NearestPlayer(c)
	if !ok {
		return nil
	}
	return p
}

func (b *Body) TemptRange() float64 { return b.Attr(javamob.AttrTemptRange) }

func (b *Body) BroadcastEvent(id byte) {
	for _, v := range b.Tx().Viewers(b.Position()) {
		v.ViewEntityAction(b.Mob.Mob, Event(id))
	}
}

func (b *Body) MobGriefing() bool { return gameRuleMobGriefing(b.Tx()) }
func (b *Body) MobDrops() bool    { return gameRuleMobDrops(b.Tx()) }

func (b *Body) DropXP(n int) {
	for _, orb := range entity.NewExperienceOrbs(b.Position(), n) {
		b.Tx().AddEntity(orb)
	}
}

// BreedOffspring is getBreedOffspring + Animal.spawnChildFromBreeding: a baby at the parent, rotation 0, 0.
func (b *Body) BreedOffspring(partner *Body) bool {
	id, configure := b.JavaType().ID, func(*javamob.Mob) {}
	if br, ok := b.B.(Breeder); ok {
		var ok2 bool
		if id, configure, ok2 = br.BreedOffspring(b, partner); !ok2 {
			return false
		}
	}
	_, err := javamob.Spawn(b.Tx(), id, b.Position(), javamob.SpawnBreeding, javamob.SpawnOptions{Configure: func(m *javamob.Mob) {
		if baby, ok := Wrap(m); ok {
			baby.AnimalState().SetBaby(true)
			m.SetBaby(true)
		}
		configure(m)
	}})
	return err == nil
}

func (b *Body) Ate() {
	if e, ok := b.B.(Eater); ok {
		e.Ate(b)
	}
}

// Held is a player's item in the hand used (InteractionHand), for interaction handlers.
func Held(user item.User) item.Stack {
	main, _ := user.HeldItems()
	return main
}

// BlockPos is Entity.blockPosition.
func (b *Body) BlockPos() cube.Pos { return cube.PosFromVec3(b.Position()) }
