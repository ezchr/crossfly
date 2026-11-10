//go:build crossfly_full

package janimal

import (
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob/jbrain"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type brainFake struct {
	a        Animal
	mem      *jbrain.Memories
	navDone  bool
	stopped  int
	panicPos mgl64.Vec3
}

func newBrainFake() *brainFake {
	m := &jbrain.Memories{}
	for _, id := range []jbrain.MemoryID{jbrain.MemIsPanicking.ID(), jbrain.MemHurtBy.ID(), jbrain.MemWalkTarget.ID(),
		jbrain.MemLookTarget.ID(), jbrain.MemTemptationCooldownTicks.ID(), jbrain.MemTemptingPlayer.ID(), jbrain.MemBreedTarget.ID()} {
		m.Register(id)
	}
	return &brainFake{mem: m, navDone: true, panicPos: mgl64.Vec3{5, 0, 5}}
}

func (f *brainFake) AnimalState() *Animal                                   { return &f.a }
func (f *brainFake) H() *world.EntityHandle                                 { return nil }
func (f *brainFake) Mem() *jbrain.Memories                                  { return f.mem }
func (f *brainFake) Open(*world.Tx, *world.EntityHandle) (*brainFake, bool) { return nil, false }
func (f *brainFake) TypeID() string                                         { return "minecraft:goat" }
func (f *brainFake) Position() mgl64.Vec3                                   { return mgl64.Vec3{} }
func (f *brainFake) Width() float64                                         { return 0.9 }
func (f *brainFake) Baby() bool                                             { return f.a.Baby() }
func (f *brainFake) OnFire() bool                                           { return false }
func (f *brainFake) CanMate(p *brainFake) bool                              { return f.a.CanMate(&p.a) }
func (f *brainFake) StopNavigation()                                        { f.stopped++ }
func (f *brainFake) NavigationDone() bool                                   { return f.navDone }
func (f *brainFake) PanicPos() (mgl64.Vec3, bool)                           { return f.panicPos, true }
func (f *brainFake) BreedWith(*brainFake)                                   {}
func (f *brainFake) Alive() bool                                            { return true }
func (f *brainFake) TemptingPlayer() (*world.EntityHandle, bool)            { return nil, false }

var _ BrainAnimal[*brainFake] = (*brainFake)(nil)

func ctx(f *brainFake, t int64) *jbrain.Ctx {
	return &jbrain.Ctx{Time: t, Rand: rand.New(rand.NewPCG(3, 4)), Mem: f.mem}
}

func TestCountDownCooldownTicks(t *testing.T) {
	f := newBrainFake()
	b := CountDownCooldownTicks[*brainFake](jbrain.MemTemptationCooldownTicks)
	jbrain.Set(f.mem, jbrain.MemTemptationCooldownTicks, 100)
	if !b.TryStart(ctx(f, 0), f) {
		t.Fatal("did not start")
	}
	ticks := 0
	for i := int64(1); f.mem.Has(jbrain.MemTemptationCooldownTicks.ID()); i++ {
		b.TickOrStop(ctx(f, i), f)
		ticks++
		if ticks > 200 {
			t.Fatal("never ran out")
		}
	}
	if ticks != 101 {
		t.Fatalf("cooldown gone after %d ticks, want 101 (100 counts, then the stop)", ticks)
	}
}

func TestAnimalPanicBrain(t *testing.T) {
	f := newBrainFake()
	b := AnimalPanic[*brainFake](2, func(_ *brainFake, src world.DamageSource) bool {
		_, ok := src.(entity.AttackDamageSource)
		return ok
	})
	if b.TryStart(ctx(f, 0), f) {
		t.Fatal("panicked without damage")
	}
	jbrain.Set(f.mem, jbrain.MemHurtBy, world.DamageSource(entity.AttackDamageSource{}))
	if !b.TryStart(ctx(f, 0), f) || f.stopped != 1 {
		t.Fatal("no panic after an attack")
	}
	if v, _ := jbrain.Get(f.mem, jbrain.MemIsPanicking); !v {
		t.Fatal("IS_PANICKING not set")
	}
	n := int64(0)
	for b.State() == jbrain.Running {
		n++
		b.TickOrStop(ctx(f, n), f)
	}
	if n < 100 || n > 121 {
		t.Fatalf("panicked %d ticks, want 100-120", n)
	}
	if f.mem.Has(jbrain.MemIsPanicking.ID()) {
		t.Fatal("IS_PANICKING kept after the panic")
	}
	if !f.mem.Has(jbrain.MemWalkTarget.ID()) {
		t.Fatal("no walk target to the panic position")
	}
}
