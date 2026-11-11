//go:build crossfly_full

package jraider

import (
	"os"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob/component/village"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/jpoi"
	"github.com/df-mc/dragonfly/server/mob/jraid"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestMain(m *testing.M) {
	if err := mobtest.RegisterTypes(); err != nil {
		panic(err)
	}
	javamob.Install()
	village.Enable()
	Install()
	os.Exit(m.Run())
}

func spawnJ(t *testing.T, w *mobtest.World, id string, pos mgl64.Vec3, reason javamob.SpawnReason) *mobtest.Ref {
	t.Helper()
	var ref *mobtest.Ref
	w.Do(func(tx *world.Tx) {
		m, err := javamob.Spawn(tx, id, pos, reason, javamob.SpawnOptions{})
		if err != nil {
			t.Fatal(err)
		}
		ref = w.Track(m.H())
	})
	return ref
}

func withJ(t *testing.T, w *mobtest.World, r *mobtest.Ref, f func(m *javamob.Mob)) {
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
		f(m)
	})
}

func playerHealth(t *testing.T, w *mobtest.World, p *mobtest.Player) (h float64) {
	t.Helper()
	w.Do(func(tx *world.Tx) {
		e, ok := p.Entity(tx)
		if !ok {
			t.Fatal("player gone")
		}
		h = e.(*player.Player).Health()
	})
	return h
}

func arena(w *mobtest.World) {
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 40, block.Stone{}) })
}

// TestJavaTypes: every raider is a Java type with Java's attributes (Pillager/Vindicator/Evoker/Witch/Ravager/
// Vex.createAttributes).
func TestJavaTypes(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	arena(w)
	cases := map[string]float64{PillagerID: 24, VindicatorID: 24, EvokerID: 24, WitchID: 26, RavagerID: 100, VexID: 14}
	for id, hp := range cases {
		r := spawnJ(t, w, id, mgl64.Vec3{0.5, 1, 0.5}, javamob.SpawnCommand)
		withJ(t, w, r, func(m *javamob.Mob) {
			if m.MaxHealth() != hp {
				t.Errorf("%s: max health %v, want %v", id, m.MaxHealth(), hp)
			}
		})
	}
}

// TestPillagerShoots: a pillager with a crossbow charges it, waits and shoots a player in range.
func TestPillagerShoots(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	arena(w)
	p := w.AddPlayer("tester", mgl64.Vec3{0.5, 1, 0.5})
	pl := spawnJ(t, w, PillagerID, mgl64.Vec3{6.5, 1, 0.5}, javamob.SpawnCommand)
	withJ(t, w, pl, func(m *javamob.Mob) {
		main, _ := m.HeldItems()
		if main.Empty() {
			t.Fatal("pillager without a crossbow")
		}
	})
	before := playerHealth(t, w, p)
	w.Tick(200)
	if after := playerHealth(t, w, p); after >= before {
		t.Fatalf("player not hurt by the pillager: %v -> %v", before, after)
	}
}

// TestVindicatorAndRavagerMelee: both walk up to the player and hit.
func TestVindicatorAndRavagerMelee(t *testing.T) {
	for _, id := range []string{VindicatorID, RavagerID} {
		w := mobtest.New(t, mobtest.Options{})
		arena(w)
		p := w.AddPlayer("tester", mgl64.Vec3{0.5, 1, 0.5})
		r := spawnJ(t, w, id, mgl64.Vec3{5.5, 1, 0.5}, javamob.SpawnCommand)
		withJ(t, w, r, func(m *javamob.Mob) {
			e, _ := p.Entity(m.Tx())
			m.SetTarget(e)
		})
		before := playerHealth(t, w, p)
		w.Tick(120)
		if after := playerHealth(t, w, p); after >= before {
			t.Errorf("%s did not hurt the player: %v -> %v", id, before, after)
		}
		w.Close()
	}
}

// TestEvokerSummonsVexes: an evoker with a target casts summon vex (or fangs) within its first spells; vexes
// appear with an owner and a limited life.
func TestEvokerSummonsVexes(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	arena(w)
	p := w.AddPlayer("tester", mgl64.Vec3{0.5, 1, 0.5})
	ev := spawnJ(t, w, EvokerID, mgl64.Vec3{8.5, 1, 0.5}, javamob.SpawnCommand)
	withJ(t, w, ev, func(m *javamob.Mob) {
		e, _ := p.Entity(m.Tx())
		m.SetTarget(e)
	})
	_, ok := w.TickUntil(400, func(tx *world.Tx) bool {
		for e := range tx.EntitiesWithin(cube.Box(-30, -5, -30, 30, 30, 30)) {
			if e.H().Type().EncodeEntity() == VexID {
				return true
			}
		}
		return false
	})
	if !ok {
		t.Fatal("no vex within 400 ticks")
	}
	w.Do(func(tx *world.Tx) {
		for e := range tx.EntitiesWithin(cube.Box(-30, -5, -30, 30, 30, 30)) {
			if e.H().Type().EncodeEntity() != VexID {
				continue
			}
			m, _ := javamob.Of(e)
			v := m.Behaviour().(*Vex)
			if v.owner != ev.H() || !v.limited || v.life <= 0 {
				t.Errorf("vex owner %v limited %v life %d", v.owner == ev.H(), v.limited, v.life)
			}
		}
	})
}

// TestWitchThrowsPotions: a witch throws harmful splash potions at a player.
func TestWitchThrowsPotions(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	arena(w)
	p := w.AddPlayer("tester", mgl64.Vec3{0.5, 1, 0.5})
	spawnJ(t, w, WitchID, mgl64.Vec3{6.5, 1, 0.5}, javamob.SpawnCommand)
	_, ok := w.TickUntil(300, func(tx *world.Tx) bool {
		e, _ := p.Entity(tx)
		pl := e.(*player.Player)
		for _, ef := range pl.Effects() {
			if ef.Type() == effect.Slowness || ef.Type() == effect.Poison {
				return true
			}
		}
		return pl.Health() < pl.MaxHealth()
	})
	if !ok {
		t.Fatal("the witch did not hit the player with a potion")
	}
}

// village makes an occupied village point of interest (a farmer's composter) at pos.
func makeVillage(t *testing.T, w *mobtest.World, pos cube.Pos) {
	t.Helper()
	w.Do(func(tx *world.Tx) {
		tx.SetBlock(pos, block.Composter{}, nil)
		m := jpoi.For(tx)
		m.EnsureLoadedAndValid(tx, pos, 16)
		if !m.Claim(tx, pos, jpoi.Is(jpoi.Farmer)) {
			t.Fatal("cannot claim the composter")
		}
	})
}

// TestBadOmenStartsRaid: bad omen in a village turns into raid omen; when raid omen runs out the raid starts, the
// first wave spawns as Java raiders that are in the raid, and the player sees the raid bar.
func TestBadOmenStartsRaid(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 64, block.Stone{}) })
	makeVillage(t, w, cube.Pos{0, 1, 0})
	p := w.AddPlayer("tester", mgl64.Vec3{2.5, 1, 0.5})
	w.Do(func(tx *world.Tx) {
		e, _ := p.Entity(tx)
		e.(*player.Player).AddEffect(effect.New(village.BadOmen, 1, time.Hour))
	})
	w.Tick(2)
	w.Do(func(tx *world.Tx) {
		e, _ := p.Entity(tx)
		pl := e.(*player.Player)
		l := levelOf(tx)
		t.Logf("sections %d diff %v effects %v", l.SectionsToVillage(cube.Pos{2, 1, 0}), l.Difficulty(), pl.Effects())
		if _, ok := omen(pl, village.BadOmen); ok {
			t.Fatal("bad omen stayed in a village")
		}
		if _, ok := omen(pl, village.RaidOmen); !ok {
			t.Fatal("no raid omen")
		}
		// Shorten the 30 second raid omen.
		pl.RemoveEffect(village.RaidOmen)
		pl.AddEffect(effect.New(village.RaidOmen, 1, time.Second/2))
	})
	w.Tick(15)
	var raid *jraid.Raid
	w.Do(func(tx *world.Tx) { raid = Raids(tx.World()).RaidAt(cube.Pos{0, 1, 0}) })
	if raid == nil {
		t.Fatal("no raid after raid omen ran out")
	}
	_, ok := w.TickUntil(600, func(*world.Tx) bool { return raid.GroupsSpawned() > 0 })
	if !ok {
		t.Fatalf("no wave within 600 ticks (status %v)", raid.Status())
	}
	if raid.TotalRaidersAlive() == 0 {
		t.Fatal("the first wave has no raiders")
	}
	w.Do(func(tx *world.Tx) {
		for _, r := range raid.Raiders() {
			rr := r.(*Raider)
			if rr.h == nil {
				t.Fatal("a raider was never spawned")
			}
			e, ok := rr.h.Entity(tx)
			if !ok {
				t.Fatal("raider not in the world")
			}
			m, ok := javamob.Of(e)
			if !ok {
				t.Fatalf("%v is not a Java mob", e.H().Type().EncodeEntity())
			}
			if m.Behaviour().(raiderBehaviour).raider() != rr || rr.Raid() != raid {
				t.Error("raider not bound to its raid")
			}
		}
	})
	if !raid.Bar().Visible {
		t.Error("raid bar not visible")
	}
}

// TestRaiderDeathMakesHero: killing the last raider of the last wave is a victory and the killer gets hero of the
// village (Raid.tick victory); here only that a killed raider leaves the raid.
func TestRaiderDeathLeavesRaid(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 64, block.Stone{}) })
	makeVillage(t, w, cube.Pos{0, 1, 0})
	p := w.AddPlayer("tester", mgl64.Vec3{2.5, 1, 0.5})
	pl := spawnJ(t, w, PillagerID, mgl64.Vec3{4.5, 1, 0.5}, javamob.SpawnCommand)
	var raid *jraid.Raid
	w.Do(func(tx *world.Tx) {
		l := levelOf(tx)
		e, _ := p.Entity(tx)
		raid = l.st.raids.CreateOrExtendRaid(l, playerRef{h: e.H(), st: l.st}, cube.Pos{0, 1, 0})
	})
	if raid == nil {
		t.Fatal("no raid")
	}
	w.Tick(40) // Raider.aiStep joins the nearby raid once a second.
	var r *Raider
	withJ(t, w, pl, func(m *javamob.Mob) { r = m.Behaviour().(raiderBehaviour).raider() })
	if r.Raid() != raid {
		t.Fatal("pillager did not join the raid it stands in")
	}
	withJ(t, w, pl, func(m *javamob.Mob) {
		e, _ := p.Entity(m.Tx())
		m.Hurt(1000, entity.AttackDamageSource{Attacker: e})
	})
	w.Tick(2)
	if r.Raid() != nil || raid.TotalRaidersAlive() != 0 {
		t.Fatal("dead raider still in the raid")
	}
	if len(raid.Heroes()) != 1 {
		t.Fatalf("heroes %d, want 1", len(raid.Heroes()))
	}
}

// TestPatrolCaptain: a pillager spawned as patrol leader carries the ominous banner and saves its patrol state.
func TestPatrolCaptain(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	arena(w)
	w.Do(func(tx *world.Tx) {
		if !spawnPatrolMember(levelOf(tx), cube.Pos{0, 1, 0}, true) {
			t.Fatal("no patrol member")
		}
	})
	w.Do(func(tx *world.Tx) {
		for e := range tx.EntitiesWithin(cube.Box(-3, -3, -3, 3, 5, 3)) {
			m, ok := javamob.Of(e)
			if !ok {
				continue
			}
			r := m.Behaviour().(raiderBehaviour).raider()
			if !r.PatrolLeader || !r.Patrolling || r.PatrolTarget == nil {
				t.Errorf("leader %v patrolling %v target %v", r.PatrolLeader, r.Patrolling, r.PatrolTarget)
			}
			if !wearsOminousBanner(r) {
				t.Error("captain without the ominous banner")
			}
			data := map[string]any{}
			m.Behaviour().(*Pillager).EncodeNBT(data)
			if data["PatrolLeader"] != uint8(1) || data["Patrolling"] != uint8(1) {
				t.Errorf("saved %v", data)
			}
			return
		}
		t.Fatal("pillager not found")
	})
}
