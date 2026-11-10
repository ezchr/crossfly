//go:build crossfly_full

package hostile

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob/jbrain"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// fakeWarden is a WardenBody without a world.
type fakeWarden struct {
	pose      WardenPose
	onGround  bool
	sounds    []string
	anger     map[*world.EntityHandle]int
	attack    *world.EntityHandle
	angryAt   *world.EntityHandle
	discarded bool
	events    []int
}

func (w *fakeWarden) Tx() *world.Tx                              { return nil }
func (w *fakeWarden) Pos() mgl64.Vec3                            { return mgl64.Vec3{} }
func (w *fakeWarden) H() *world.EntityHandle                     { return nil }
func (w *fakeWarden) OnGround() bool                             { return w.onGround }
func (w *fakeWarden) InLiquid() bool                             { return false }
func (w *fakeWarden) Riding() bool                               { return false }
func (w *fakeWarden) StopRiding()                                {}
func (w *fakeWarden) SetWardenPose(p WardenPose)                 { w.pose = p }
func (w *fakeWarden) WardenPose() WardenPose                     { return w.pose }
func (w *fakeWarden) Sound(name string, _, _ float64)            { w.sounds = append(w.sounds, name) }
func (w *fakeWarden) EntityEvent(id int)                         { w.events = append(w.events, id) }
func (w *fakeWarden) CanTargetEntity(h *world.EntityHandle) bool { return h != nil }
func (w *fakeWarden) IncreaseAngerAt(h *world.EntityHandle, n int, _ bool) {
	if w.anger == nil {
		w.anger = map[*world.EntityHandle]int{}
	}
	w.anger[h] += n
}
func (w *fakeWarden) SetAttackTarget(h *world.EntityHandle)      { w.attack = h }
func (w *fakeWarden) EntityAngryAt() (*world.EntityHandle, bool) { return w.angryAt, w.angryAt != nil }
func (w *fakeWarden) LookAt(mgl64.Vec3)                          {}
func (w *fakeWarden) Discard()                                   { w.discarded = true }
func (w *fakeWarden) ChestPos() mgl64.Vec3                       { return mgl64.Vec3{0, 1.6, 0} }
func (w *fakeWarden) WithinWorldBorder(cube.Pos) bool            { return true }

// wardenCtx is a brain context whose memories know every memory the
// controls use.
func wardenCtx(cs ...jbrain.Control[*fakeWarden]) *jbrain.Ctx {
	c := &jbrain.Ctx{Rand: newRand(), Mem: &jbrain.Memories{}}
	for _, ctl := range cs {
		for _, id := range ctl.RequiredMemories() {
			c.Mem.Register(id)
		}
	}
	for _, m := range []interface{ ID() jbrain.MemoryID }{jbrain.MemDigCooldown, jbrain.MemSniffCooldown, jbrain.MemLookTarget,
		jbrain.MemDisturbanceLocation, jbrain.MemWalkTarget, jbrain.MemAttackTarget} {
		c.Mem.Register(m.ID())
	}
	return c
}

// run ticks a control n times (TryStart while stopped, TickOrStop while
// running), advancing the clock and expiring memories as a brain would.
func run(c *jbrain.Ctx, w *fakeWarden, ctl jbrain.Control[*fakeWarden], n int) {
	for i := 0; i < n; i++ {
		if ctl.State() == jbrain.Stopped {
			ctl.TryStart(c, w)
		} else {
			ctl.TickOrStop(c, w)
		}
		c.Time++
	}
}

// TestWardenRoar: the roar target gets +20 anger, the roar sound comes after
// 25 ticks, and after 84 ticks the warden attacks it.
func TestWardenRoar(t *testing.T) {
	w := &fakeWarden{}
	roar := newRoar[*fakeWarden]()
	c := wardenCtx(roar)
	target := &world.EntityHandle{}
	jbrain.Set(c.Mem, jbrain.MemRoarTarget, target)
	run(c, w, roar, 1)
	if w.pose != WardenRoaring || w.anger[target] != wardenRoarAnger {
		t.Fatalf("roar start: pose %v anger %v", w.pose, w.anger[target])
	}
	run(c, w, roar, WardenRoarDuration+1)
	if w.attack != target || w.pose != WardenStanding || c.Mem.Has(jbrain.MemRoarTarget.ID()) {
		t.Errorf("after the roar: attack %v pose %v", w.attack, w.pose)
	}
}

// TestWardenSniffAndDig: TryToSniff needs something attackable and no
// cooldown; digging on the ground ends with the warden gone.
func TestWardenSniffAndDig(t *testing.T) {
	w := &fakeWarden{}
	try := newTryToSniff[*fakeWarden]()
	c := wardenCtx(try)
	if try.TryStart(c, w) {
		t.Fatalf("sniffed with nothing attackable")
	}
	jbrain.Set(c.Mem, jbrain.MemNearestAttackable, &world.EntityHandle{})
	if !try.TryStart(c, w) || w.pose != WardenSniffing || !c.Mem.Has(jbrain.MemSniffCooldown.ID()) {
		t.Fatalf("TryToSniff did not start sniffing")
	}
	dig := newDigging[*fakeWarden]()
	w2 := &fakeWarden{onGround: true}
	c2 := wardenCtx(dig)
	run(c2, w2, dig, 1)
	if w2.pose != WardenDigging || w2.discarded {
		t.Fatalf("digging start: pose %v discarded %v", w2.pose, w2.discarded)
	}
	run(c2, w2, dig, wardenDiggingDuration+1)
	if !w2.discarded {
		t.Errorf("the warden did not dig away")
	}
	// In the air (not on the ground, not in liquid) it does not start.
	w3 := &fakeWarden{}
	if newDigging[*fakeWarden]().TryStart(wardenCtx(), w3) {
		t.Errorf("digging started in the air")
	}
}

// TestWardenDisturbance: setDisturbanceLocation only without anger or a
// target; it sets the disturbance, the look target and the sniff cooldown
// for 100 ticks and drops the walk target.
func TestWardenDisturbance(t *testing.T) {
	w := &fakeWarden{}
	c := wardenCtx()
	pos := cube.Pos{3, 4, 5}
	SetWardenDisturbance(w, c.Mem, pos)
	if got, ok := jbrain.Get(c.Mem, jbrain.MemDisturbanceLocation); !ok || got != pos {
		t.Fatalf("disturbance %v %v", got, ok)
	}
	if c.Mem.TimeUntilExpiry(jbrain.MemDisturbanceLocation.ID()) != wardenDisturbanceTTL || !c.Mem.Has(jbrain.MemSniffCooldown.ID()) {
		t.Errorf("expiry or sniff cooldown missing")
	}
	c2 := wardenCtx()
	jbrain.Set(c2.Mem, jbrain.MemAttackTarget, &world.EntityHandle{})
	SetWardenDisturbance(w, c2.Mem, pos)
	if c2.Mem.Has(jbrain.MemDisturbanceLocation.ID()) {
		t.Errorf("a fighting warden investigates a disturbance")
	}
}
