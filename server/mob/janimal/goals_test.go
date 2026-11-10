//go:build crossfly_full

package janimal

import (
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// fakeWorld holds fake animals for the goal tests: navigation teleports a step towards the target.
type fakeWorld struct {
	animals []*fake
	players []world.Entity
	babies  int
	xp      int
	events  []byte
}

type fake struct {
	world.Entity
	w       *fakeWorld
	a       Animal
	pos     mgl64.Vec3
	r       *rand.Rand
	target  world.Entity
	navDone bool
	panic   bool
	damage  string
	alive   bool
}

func (w *fakeWorld) add(pos mgl64.Vec3) *fake {
	f := &fake{w: w, pos: pos, r: rand.New(rand.NewPCG(uint64(len(w.animals)), 9)), navDone: true, alive: true}
	w.animals = append(w.animals, f)
	return f
}

func (f *fake) AnimalState() *Animal   { return &f.a }
func (f *fake) Self() world.Entity     { return f }
func (f *fake) Tx() *world.Tx          { return nil }
func (f *fake) Rand() *rand.Rand       { return f.r }
func (f *fake) Position() mgl64.Vec3   { return f.pos }
func (f *fake) Alive() bool            { return f.alive }
func (f *fake) OnFire() bool           { return false }
func (f *fake) Panicking() bool        { return f.panic }
func (f *fake) LastDamageType() string { return f.damage }
func (f *fake) MoveToEntity(e world.Entity, _ float64) bool {
	f.target, f.navDone = e, false
	// One block per call towards the target.
	d := e.Position().Sub(f.pos)
	if d.Len() > 1 {
		d = d.Normalize()
	}
	f.pos = f.pos.Add(d)
	return true
}
func (f *fake) MoveTo(p mgl64.Vec3, _ float64) bool   { f.navDone = false; return true }
func (f *fake) StopNavigation()                       { f.target, f.navDone = nil, true }
func (f *fake) NavigationDone() bool                  { return f.navDone }
func (f *fake) LookAt(world.Entity, float64, float64) {}
func (f *fake) MaxHeadXRot() float64                  { return 40 }
func (f *fake) MaxHeadYRot() float64                  { return 75 }
func (f *fake) DefaultRandomPos(int, int) (mgl64.Vec3, bool) {
	return f.pos.Add(mgl64.Vec3{3, 0, 0}), true
}
func (f *fake) SameTypeInBox(dx, dy, dz float64) []*fake {
	var out []*fake
	for _, o := range f.w.animals {
		d := o.pos.Sub(f.pos)
		if abs(int(d[0])) <= int(dx)+1 && abs(int(d[1])) <= int(dy)+1 && abs(int(d[2])) <= int(dz)+1 {
			out = append(out, o)
		}
	}
	return out
}
func (f *fake) NearestPlayer(r float64, pred func(world.Entity) bool) world.Entity {
	for _, p := range f.w.players {
		if p.Position().Sub(f.pos).Len() <= r && pred(p) {
			return p
		}
	}
	return nil
}
func (f *fake) TemptRange() float64    { return 10 }
func (f *fake) BroadcastEvent(id byte) { f.w.events = append(f.w.events, id) }
func (f *fake) MobGriefing() bool      { return true }
func (f *fake) MobDrops() bool         { return true }
func (f *fake) DropXP(n int)           { f.w.xp += n }
func (f *fake) BreedOffspring(*fake) bool {
	f.w.babies++
	b := f.w.add(f.pos)
	b.a.SetBaby(true)
	return true
}
func (f *fake) Ate() {}

var _ Host[*fake] = (*fake)(nil)

// Two cows in love 5 blocks apart walk together and breed after 30 goal ticks (60 game ticks).
func TestBreedGoal(t *testing.T) {
	w := &fakeWorld{}
	a, b := w.add(mgl64.Vec3{0, 0, 0}), w.add(mgl64.Vec3{5, 0, 0})
	g := &BreedGoal[*fake]{Speed: 1}
	if g.CanUse(a) {
		t.Fatal("breeds without love")
	}
	a.a.Feed(true, uuid.New())
	if g.CanUse(a) {
		t.Fatal("partner not in love")
	}
	b.a.Feed(true, uuid.New())
	b.panic = true
	if g.CanUse(a) {
		t.Fatal("chose a panicking partner")
	}
	b.panic = false
	if !g.CanUse(a) {
		t.Fatal("no partner")
	}
	g.Start(a)
	ticks := 0
	for g.CanContinueToUse(a) && w.babies == 0 {
		g.Tick(a)
		ticks++
	}
	g.Stop(a)
	if w.babies != 1 || ticks != 30 {
		t.Fatalf("babies %d after %d goal ticks", w.babies, ticks)
	}
	if a.a.Age() != 6000 || b.a.Age() != 6000 || a.a.InLove() || b.a.InLove() {
		t.Fatal("parents not on cooldown")
	}
	if w.xp < 1 || w.xp > 7 || len(w.events) != 1 || w.events[0] != EventLoveHearts {
		t.Fatalf("xp %d events %v", w.xp, w.events)
	}
	if !w.animals[2].a.Baby() {
		t.Fatal("offspring not a baby")
	}
}

// A baby follows the nearest adult once it is 3 blocks away.
func TestFollowParentGoal(t *testing.T) {
	w := &fakeWorld{}
	baby := w.add(mgl64.Vec3{0, 0, 0})
	baby.a.SetBaby(true)
	adult := w.add(mgl64.Vec3{2, 0, 0})
	g := &FollowParentGoal[*fake]{Speed: 1.25}
	if g.CanUse(baby) {
		t.Fatal("follows a parent 2 blocks away")
	}
	adult.pos = mgl64.Vec3{6, 0, 0}
	if !g.CanUse(baby) || g.parent != adult {
		t.Fatal("does not follow")
	}
	if g.CanUse(adult) {
		t.Fatal("adult follows")
	}
	g.Start(baby)
	g.Tick(baby)
	if baby.target != adult {
		t.Fatal("not walking to the parent")
	}
	adult.pos = mgl64.Vec3{30, 0, 0}
	if g.CanContinueToUse(baby) {
		t.Fatal("follows past 16 blocks")
	}
}

type fakePlayer struct {
	world.Entity
	pos  mgl64.Vec3
	main item.Stack
}

func (p *fakePlayer) Position() mgl64.Vec3                { return p.pos }
func (p *fakePlayer) HeldItems() (item.Stack, item.Stack) { return p.main, item.Stack{} }

// Tempting follows a player with wheat and waits 50 goal ticks after stopping.
func TestTemptGoal(t *testing.T) {
	w := &fakeWorld{}
	cow := w.add(mgl64.Vec3{0, 0, 0})
	p := &fakePlayer{pos: mgl64.Vec3{8, 0, 0}, main: item.NewStack(item.Wheat{}, 1)}
	w.players = append(w.players, p)
	g := NewTemptGoal[*fake](1.25, StackIn(CowFood), false)
	if !g.CanUse(cow) {
		t.Fatal("not tempted by wheat")
	}
	g.Start(cow)
	for i := 0; i < 10; i++ {
		g.Tick(cow)
	}
	if d := cow.pos.Sub(p.pos).Len(); d > 3 {
		t.Fatalf("still %f blocks away", d)
	}
	p.main = item.NewStack(item.Stick{}, 1)
	if g.CanContinueToUse(cow) {
		t.Fatal("follows a player without food")
	}
	g.Stop(cow)
	p.main = item.NewStack(item.Wheat{}, 1)
	for i := 0; i < 50; i++ {
		if g.CanUse(cow) {
			t.Fatalf("tempted again after %d goal ticks", i)
		}
	}
	if !g.CanUse(cow) {
		t.Fatal("not tempted after calming down")
	}
	cow.pos, p.pos = mgl64.Vec3{}, mgl64.Vec3{11, 0, 0}
	if g.CanUse(cow) {
		t.Fatal("tempted from 11 blocks")
	}
}

func TestPanicGoal(t *testing.T) {
	w := &fakeWorld{}
	cow := w.add(mgl64.Vec3{0, 0, 0})
	g := &PanicGoal[*fake]{Speed: 2}
	if g.CanUse(cow) {
		t.Fatal("panics without damage")
	}
	cow.damage = "minecraft:fall"
	if g.CanUse(cow) {
		t.Fatal("fall damage does not cause panic")
	}
	cow.damage = "minecraft:player_attack"
	if !g.CanUse(cow) {
		t.Fatal("no panic after a player hit")
	}
	g.Start(cow)
	if !g.Running() || !g.CanContinueToUse(cow) {
		t.Fatal("not running")
	}
	cow.navDone = true
	if g.CanContinueToUse(cow) {
		t.Fatal("runs after the path is done")
	}
}
