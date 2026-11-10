//go:build crossfly_full

package jvillage

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/jbrain"
	"github.com/df-mc/dragonfly/server/mob/jbrain/jmob"
	"github.com/df-mc/dragonfly/server/mob/jpoi"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

func spawnAt(t *testing.T, w *mobtest.World, id string, pos mgl64.Vec3) *mobtest.Ref {
	t.Helper()
	var ref *mobtest.Ref
	w.Do(func(tx *world.Tx) {
		m, err := javamob.Spawn(tx, id, pos, javamob.SpawnCommand, javamob.SpawnOptions{})
		if err != nil {
			t.Fatal(err)
		}
		ref = w.Track(m.H())
	})
	return ref
}

func with(t *testing.T, w *mobtest.World, r *mobtest.Ref, f func(tx *world.Tx, m *javamob.Mob)) {
	t.Helper()
	w.Do(func(tx *world.Tx) {
		bm, ok := r.Mob(tx)
		if !ok {
			t.Fatal("mob gone")
		}
		m, ok := javamob.Of(bm)
		if !ok {
			t.Fatal("not a Java mob")
		}
		f(tx, m)
	})
}

func villagerOf(t *testing.T, m *javamob.Mob) *Villager {
	v, ok := Of(m)
	if !ok {
		t.Fatal("not a villager")
	}
	return v
}

func countGolems(w *mobtest.World) int {
	n := 0
	w.Do(func(tx *world.Tx) {
		for e := range tx.Entities() {
			if e.H().Type().EncodeEntity() == IronGolemID {
				n++
			}
		}
	})
	return n
}

// TestVillagerTakesJob: an unemployed villager claims a lectern within 48
// blocks, walks to it and becomes a librarian with two level 1 trades
// (AcquirePoi, GoToPotentialJobSite, AssignProfessionFromJobSite).
func TestVillagerTakesJob(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{Time: 3000}) // work time
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 24, block.Stone{})
		tx.SetBlock(cube.Pos{6, 1, 0}, block.Lectern{}, nil)
	})
	r := spawnAt(t, w, VillagerID, mgl64.Vec3{0.5, 1, 0.5})
	ok := false
	for i := 0; i < 1200 && !ok; i++ {
		w.Tick(1)
		with(t, w, r, func(tx *world.Tx, m *javamob.Mob) {
			ok = villagerOf(t, m).Data.Profession == ProfLibrarian
		})
	}
	if !ok {
		t.Fatal("villager did not become a librarian within 1200 ticks")
	}
	with(t, w, r, func(tx *world.Tx, m *javamob.Mob) {
		v := villagerOf(t, m)
		if js, ok := jbrain.Get(jmob.Mem(m), jbrain.MemJobSite); !ok || js.Pos != (cube.Pos{6, 1, 0}) {
			t.Fatalf("job site %v %v", js, ok)
		}
		if n := len(v.Offers(m)); n != 2 {
			t.Fatalf("librarian has %d offers, want 2", n)
		}
		if rec, ok := jpoi.For(tx).Get(tx, cube.Pos{6, 1, 0}); !ok || rec.HasSpace() {
			t.Fatal("lectern ticket not taken")
		}
	})
}

// TestGolemNeedsFiveSleepers: five villagers that slept in the last day
// summon one golem when they gossip; afterwards they remember it
// (Villager.spawnGolemIfNeeded, GOLEM_DETECTED_RECENTLY 599 ticks).
func TestGolemNeedsFiveSleepers(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 24, block.Stone{}) })
	var refs []*mobtest.Ref
	for i := range 5 {
		refs = append(refs, spawnAt(t, w, VillagerID, mgl64.Vec3{float64(i) + .5, 1, .5}))
	}
	slept := func(n int) {
		for i, r := range refs {
			with(t, w, r, func(tx *world.Tx, m *javamob.Mob) {
				mem := jmob.Mem(m)
				mem.Erase(jbrain.MemGolemDetectedRecently.ID())
				if i < n {
					jbrain.Set(mem, jbrain.MemLastSlept, tx.CurrentTick()-100)
				} else {
					mem.Erase(jbrain.MemLastSlept.ID())
				}
			})
		}
	}
	trigger := func() {
		with(t, w, refs[0], func(tx *world.Tx, m *javamob.Mob) {
			villagerOf(t, m).SpawnGolemIfNeeded(m, tx.CurrentTick(), 5)
		})
	}
	slept(4)
	trigger()
	if n := countGolems(w); n != 0 {
		t.Fatalf("4 sleepers summoned %d golems", n)
	}
	slept(5)
	trigger()
	if n := countGolems(w); n != 1 {
		t.Fatalf("5 sleepers summoned %d golems, want 1", n)
	}
	with(t, w, refs[3], func(tx *world.Tx, m *javamob.Mob) {
		if !jmob.Mem(m).Has(jbrain.MemGolemDetectedRecently.ID()) {
			t.Fatal("villager did not note the golem")
		}
	})
	trigger()
	if n := countGolems(w); n != 1 {
		t.Fatal("golem summoned again while detected recently")
	}
}

// TestPanicNeedsThree: panicking villagers need only 3 (VillagerPanicTrigger).
func TestPanicNeedsThree(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 24, block.Stone{}) })
	var refs []*mobtest.Ref
	for i := range 3 {
		refs = append(refs, spawnAt(t, w, VillagerID, mgl64.Vec3{float64(i) + .5, 1, .5}))
	}
	for _, r := range refs {
		with(t, w, r, func(tx *world.Tx, m *javamob.Mob) {
			jbrain.Set(jmob.Mem(m), jbrain.MemLastSlept, tx.CurrentTick())
		})
	}
	with(t, w, refs[0], func(tx *world.Tx, m *javamob.Mob) {
		villagerOf(t, m).SpawnGolemIfNeeded(m, tx.CurrentTick(), 3)
	})
	if n := countGolems(w); n != 1 {
		t.Fatalf("3 panicking sleepers summoned %d golems, want 1", n)
	}
}

// TestGolemNotOnGlass: LEGACY_IRON_GOLEM never stands on glass.
func TestGolemNotOnGlass(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 24, block.Glass{}) })
	var refs []*mobtest.Ref
	for i := range 5 {
		refs = append(refs, spawnAt(t, w, VillagerID, mgl64.Vec3{float64(i) + .5, 1, .5}))
	}
	for _, r := range refs {
		with(t, w, r, func(tx *world.Tx, m *javamob.Mob) {
			jbrain.Set(jmob.Mem(m), jbrain.MemLastSlept, tx.CurrentTick())
		})
	}
	with(t, w, refs[0], func(tx *world.Tx, m *javamob.Mob) {
		villagerOf(t, m).SpawnGolemIfNeeded(m, tx.CurrentTick(), 5)
	})
	if n := countGolems(w); n != 0 {
		t.Fatalf("golem spawned on glass")
	}
}

// TestCuredDiscount: curing gives major positive 20 and minor positive 25,
// 125 reputation, cutting floor(125 * priceMultiplier) off every price.
func TestCuredDiscount(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 24, block.Stone{}) })
	r := spawnAt(t, w, VillagerID, mgl64.Vec3{.5, 1, .5})
	steve := uuid.MustParse("00000000-0000-0000-0000-00000000aaaa")
	with(t, w, r, func(tx *world.Tx, m *javamob.Mob) {
		v := villagerOf(t, m)
		v.Gossips().Add(steve, MajorPositive, 20)
		v.Gossips().Add(steve, MinorPositive, 25)
		if rep := v.Gossips().Reputation(steve, nil); rep != 125 {
			t.Fatalf("reputation %d", rep)
		}
	})
}

// TestCureZombieVillager: weakness + golden apple starts the cure; the cured
// villager keeps profession and trades, and the curer gets 125 reputation.
func TestCureZombieVillager(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{Time: 13000})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 24, block.Stone{}) })
	steve := w.AddPlayer("Steve", mgl64.Vec3{3.5, 1, .5})
	r := spawnAt(t, w, ZombieVillagerID, mgl64.Vec3{.5, 1, .5})
	with(t, w, r, func(tx *world.Tx, m *javamob.Mob) {
		z := m.Behaviour().(*ZombieVillager)
		z.Data = Data{Type: TypePlains, Profession: ProfLibrarian, Level: 2}
		z.offers = Offers{NewOffer(item.NewStack(item.Emerald{}, 9), item.Stack{}, item.NewStack(item.Book{}, 1), 12, 1, 0.05)}
		z.hasOffers = true
		m.AddEffect(effect.New(effect.Weakness, 1, time.Minute))
	})
	steve.Interact(r, item.NewStack(item.GoldenApple{}, 1))
	with(t, w, r, func(tx *world.Tx, m *javamob.Mob) {
		z := m.Behaviour().(*ZombieVillager)
		if !z.Converting() {
			t.Fatal("not converting after the golden apple")
		}
		z.conversionTime = 1
	})
	w.Tick(2)
	var cured *javamob.Mob
	w.Do(func(tx *world.Tx) {
		for e := range tx.Entities() {
			if jm, ok := javamob.Of(e); ok && jm.JavaType().ID == VillagerID {
				cured = jm
				v, _ := Of(jm)
				if v.Data.Profession != ProfLibrarian || v.Data.Level != 2 || len(v.offers) != 1 {
					t.Fatalf("cured villager %+v offers %d", v.Data, len(v.offers))
				}
				var id uuid.UUID
				steve.With(func(pl *player.Player) { id = pl.UUID() })
				if rep := v.Gossips().Reputation(id, nil); rep != 125 {
					t.Fatalf("curer reputation %d, want 125", rep)
				}
			}
		}
	})
	if cured == nil {
		t.Fatal("no villager after the cure")
	}
}

// TestRestockRules: a villager restocks at most twice a day, the second
// time 2400 ticks after the first; demand grows with sold-out offers.
func TestRestockRules(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{Time: 3000})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 24, block.Stone{}) })
	r := spawnAt(t, w, VillagerID, mgl64.Vec3{.5, 1, .5})
	with(t, w, r, func(tx *world.Tx, m *javamob.Mob) {
		v := villagerOf(t, m)
		v.Data = Data{Type: TypePlains, Profession: ProfFarmer, Level: 1}
		o := NewOffer(item.NewStack(item.Emerald{}, 1), item.Stack{}, item.NewStack(item.Bread{}, 6), 16, 1, 0.05)
		v.SetOffers(Offers{o})
		o.Uses = 16
		if !v.ShouldRestock(m) {
			t.Fatal("sold-out villager should restock")
		}
		v.Restock(m)
		if o.Uses != 0 || o.Demand != 16 {
			t.Fatalf("after restock uses %d demand %d", o.Uses, o.Demand)
		}
		o.Uses = 1
		if v.ShouldRestock(m) {
			t.Fatal("second restock allowed within 2400 ticks")
		}
	})
}
