//go:build crossfly_full

package village_test

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	vc "github.com/df-mc/dragonfly/server/mob/component/village"
	gv "github.com/df-mc/dragonfly/server/mob/goal/village"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	_ "github.com/df-mc/dragonfly/server/mob/vanilla"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func needs(t *testing.T, keys ...string) {
	t.Helper()
	mobtest.RequireDefinitions(t)
	mobtest.RequireFilters(t)
	mobtest.Require(t, append([]string{"minecraft:navigation.walk", "minecraft:movement.basic"}, keys...)...)
}

// bed places a bed (head at p, foot to the south).
func bed(tx *world.Tx, p cube.Pos) {
	tx.SetBlock(p, block.Bed{Facing: cube.North, Head: true}, nil)
	tx.SetBlock(p.Side(cube.FaceSouth), block.Bed{Facing: cube.North}, nil)
}

// villager spawns an adult villager_v2 with event (spawn_farmer, ...).
func villager(w *mobtest.World, pos mgl64.Vec3, event string) *mobtest.Ref {
	return w.Spawn("minecraft:villager_v2", pos, mob.SpawnOptions{Event: event})
}

func villageOf(r *mobtest.Ref, tx *world.Tx) (*vc.Village, bool) {
	m, ok := r.Mob(tx)
	if !ok {
		return nil, false
	}
	return vc.VillageOf(m)
}

// TestVillageFormsAndJobSite: beds found a village (Village bounds +-32,
// +-12 around the first bed), the villager joins it and claims a bed, and a
// composter turns it into a farmer (minecraft:become_farmer, variant 1).
func TestVillageFormsAndJobSite(t *testing.T) {
	needs(t, "minecraft:dweller", "minecraft:scheduler")
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 24, block.Stone{})
		bed(tx, cube.Pos{4, 1, 4})
		bed(tx, cube.Pos{6, 1, 4})
		tx.SetBlock(cube.Pos{-4, 1, 0}, block.Composter{}, nil)
	})
	v := villager(w, mgl64.Vec3{0.5, 1, 0.5}, "minecraft:spawn_farmer")
	v.With(func(m *mob.Mob) { m.Fire("minecraft:become_unskilled", nil) })
	n, ok := w.TickUntil(600, func(tx *world.Tx) bool {
		vil, ok := villageOf(v, tx)
		if !ok {
			return false
		}
		m, _ := v.Mob(tx)
		_, hasBed := vil.OwnedPOI(m.H(), vc.POIBed)
		_, hasJob := vil.OwnedPOI(m.H(), vc.POIJob)
		return hasBed && hasJob && m.Meta().Variant == 1
	})
	if !ok {
		t.Fatalf("villager did not join a village, claim a bed and become a farmer in %d ticks:\n%v", n, w.Dump(v))
	}
	w.Do(func(tx *world.Tx) {
		vil, _ := villageOf(v, tx)
		if !vil.Within(mgl64.Vec3{4, 1, 4}, 0) || !vil.Within(mgl64.Vec3{-4, 1, 0}, 0) {
			t.Errorf("village bounds %v do not hold its POIs", vil.Bounds())
		}
		if got := vil.BedCount(); got != 2 {
			t.Errorf("bed count %v, want 2", got)
		}
		m, _ := v.Mob(tx)
		if d, _ := vc.DwellerOf(m); d.Preferred() != "farmer" {
			t.Errorf("preferred profession %q, want farmer", d.Preferred())
		}
	})
	t.Logf("farmer after %d ticks", n)
}

// TestJobSiteLost: breaking the job site of a villager that never traded
// fires minecraft:become_unskilled (Village::_updateClaimedPOIs).
func TestJobSiteLost(t *testing.T) {
	needs(t, "minecraft:dweller")
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	site := cube.Pos{-3, 1, 0}
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 24, block.Stone{})
		bed(tx, cube.Pos{4, 1, 4})
		tx.SetBlock(site, block.Lectern{}, nil)
	})
	v := villager(w, mgl64.Vec3{0.5, 1, 0.5}, "minecraft:spawn_farmer")
	v.With(func(m *mob.Mob) { m.Fire("minecraft:become_unskilled", nil) })
	if n, ok := w.TickUntil(600, func(tx *world.Tx) bool {
		m, ok := v.Mob(tx)
		return ok && m.Meta().Variant == 5
	}); !ok {
		t.Fatalf("villager did not become a librarian in %d ticks:\n%v", n, w.Dump(v))
	}
	w.Do(func(tx *world.Tx) { tx.SetBlock(site, nil, nil) })
	if n, ok := w.TickUntil(40, func(tx *world.Tx) bool {
		m, ok := v.Mob(tx)
		return ok && m.Meta().Variant == 0
	}); !ok {
		t.Fatalf("villager kept its profession %d ticks after its lectern broke:\n%v", n, w.Dump(v))
	}
}

// TestTrading: a farmer's offers cover the five tiers (only tier 0
// unlocked), a trade gives trader experience and levels it up at 10, uses
// count up, and a restock (minecraft:resupply_trades) resets them.
func TestTrading(t *testing.T) {
	needs(t, "minecraft:economy_trade_table", "minecraft:behavior.trade_with_player")
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 16, block.Stone{}) })
	v := villager(w, mgl64.Vec3{0.5, 1, 0.5}, "minecraft:spawn_farmer")
	p := w.AddPlayer("trader", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(2)
	if _, ok := p.Interact(v, item.Stack{}); !ok {
		t.Fatalf("interacting did not open trading:\n%v", w.Dump(v))
	}
	w.Do(func(tx *world.Tx) {
		m, _ := v.Mob(tx)
		e, ok := vc.EconomyOf(m)
		if !ok {
			t.Fatal("farmer without economy_trade_table")
		}
		tiers := map[int]int{}
		for _, o := range e.Offers() {
			tiers[o.Tier]++
		}
		for tier := range 5 {
			if tiers[tier] == 0 {
				t.Errorf("no offers of tier %v (%v)", tier, tiers)
			}
		}
		if got := e.TierExp(); len(got) != 5 || got[1] != 10 || got[4] != 250 {
			t.Errorf("tier experience %v", got)
		}
		if tp, ok := e.Trading(tx); !ok || tp.Name() != "trader" {
			t.Error("trading player not recorded")
		}
		pl, _ := p.H().Entity(tx)
		idx := -1
		for i, o := range e.Offers() {
			if o.Tier == 0 && o.TraderExp >= 2 {
				idx = i
				break
			}
		}
		if idx < 0 {
			t.Fatal("no novice offer")
		}
		for range 5 {
			if !e.Trade(m, tx, pl.H(), idx) {
				t.Fatal("trade refused")
			}
		}
		if e.Experience() < 10 || m.Meta().TradeTier != 1 {
			t.Errorf("after 5 trades: experience %v tier %v, want >= 10 / 1", e.Experience(), m.Meta().TradeTier)
		}
		if e.Offers()[idx].Uses != 5 {
			t.Errorf("uses %v, want 5", e.Offers()[idx].Uses)
		}
		e.Resupply()
		if o := e.Offers()[idx]; o.Uses != 0 {
			t.Errorf("uses after restock %v", o.Uses)
		}
	})
	if n, ok := w.TickUntil(5, func(tx *world.Tx) bool {
		m, _ := v.Mob(tx)
		return m.Goals().Running("minecraft:behavior.trade_with_player")
	}); !ok {
		t.Errorf("trade_with_player did not run while trading (%d ticks):\n%v", n, w.Dump(v))
	}
}

// TestVillagerSleeps: at night the bed schedule makes a villager walk to its
// bed and sleep in it.
func TestVillagerSleeps(t *testing.T) {
	needs(t, "minecraft:behavior.sleep", "minecraft:scheduler", "minecraft:dweller")
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Midnight})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 24, block.Stone{})
		bed(tx, cube.Pos{6, 1, 2})
	})
	v := villager(w, mgl64.Vec3{0.5, 1, 0.5}, "minecraft:spawn_farmer")
	n, ok := w.TickUntil(1200, func(tx *world.Tx) bool {
		m, ok := v.Mob(tx)
		return ok && vc.IsSleeping(m)
	})
	if !ok {
		t.Fatalf("villager not asleep after %d ticks:\n%v", n, w.Dump(v))
	}
	w.Do(func(tx *world.Tx) {
		m, _ := v.Mob(tx)
		if !m.ActorFlag(vc.FlagSleeping) {
			t.Error("sleeping actor flag not set")
		}
		if d := m.Position().Sub(mgl64.Vec3{6.5, 1, 2.5}).Len(); d > 1.5 {
			t.Errorf("sleeping %.1f blocks from the bed", d)
		}
	})
	t.Logf("asleep after %d ticks", n)
}

// TestFarmerHarvests: at work time a farmer reaps ripe wheat on farmland and
// replants it from its seeds.
func TestFarmerHarvests(t *testing.T) {
	needs(t, "minecraft:behavior.harvest_farm_block", "minecraft:scheduler")
	w := mobtest.New(t, mobtest.Options{Time: 1000})
	crop := cube.Pos{3, 1, 0}
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 16, block.Stone{})
		tx.SetBlock(crop.Side(cube.FaceDown), block.Farmland{Hydration: 7}, nil)
		wheat, _ := world.BlockByName("minecraft:wheat", map[string]any{"growth": int32(7)})
		tx.SetBlock(crop, wheat, nil)
	})
	v := villager(w, mgl64.Vec3{0.5, 1, 0.5}, "minecraft:spawn_farmer")
	v.With(func(m *mob.Mob) {
		m.Fire("minecraft:become_farmer", nil)
		_, _ = vc.Inventory(m).AddItem(item.NewStack(block.WheatSeeds{}, 4))
	})
	n, ok := w.TickUntil(1200, func(tx *world.Tx) bool {
		c, ok := tx.Block(crop).(block.Crop)
		return ok && c.GrowthStage() == 0
	})
	if !ok {
		t.Fatalf("wheat not harvested and replanted after %d ticks:\n%v", n, w.Dump(v))
	}
	t.Logf("harvested after %d ticks", n)
}

// TestRaid: bad omen in a village becomes raid omen; when it runs out the
// raid starts (300-tick warm-up), the first wave (4 pillagers) spawns and
// joins; once the waves are beaten the killers get hero of the village and
// the villagers celebrate.
func TestRaid(t *testing.T) {
	needs(t, "minecraft:dweller", "minecraft:can_join_raid")
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon, Difficulty: world.DifficultyNormal})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 40, block.Stone{})
		bed(tx, cube.Pos{2, 1, 2})
		bed(tx, cube.Pos{4, 1, 2})
	})
	v := villager(w, mgl64.Vec3{0.5, 1, 0.5}, "minecraft:spawn_farmer")
	p := w.AddPlayer("hero", mgl64.Vec3{1.5, 1, 4.5})
	p.SetMaxHealth(1000)
	if n, ok := w.TickUntil(400, func(tx *world.Tx) bool { _, ok := villageOf(v, tx); return ok }); !ok {
		t.Fatalf("no village after %d ticks", n)
	}
	p.With(func(pl *player.Player) { pl.AddEffect(effect.New(vc.BadOmen, 1, time.Minute)) })
	var raid *vc.Raid
	n, ok := w.TickUntil(1200, func(tx *world.Tx) bool {
		vil, _ := villageOf(v, tx)
		raid, _ = vil.Raid()
		return raid != nil
	})
	if !ok {
		t.Fatalf("no raid %d ticks after bad omen", n)
	}
	if n < 590 {
		t.Errorf("raid started after %d ticks, want the 30 s of raid omen first", n)
	}
	w.Do(func(tx *world.Tx) { raid.SetGroups(1) })
	if n, ok := w.TickUntil(800, func(tx *world.Tx) bool { return raid.State == vc.RaidInPlay }); !ok {
		t.Fatalf("first wave did not spawn in %d ticks (state %v)", n, raid.State)
	}
	if got := len(raid.Raiders()); got < 4 {
		t.Fatalf("first wave has %d raiders, want 4 pillagers", got)
	}
	w.Do(func(tx *world.Tx) {
		pl, _ := p.H().Entity(tx)
		for _, h := range raid.Raiders() {
			if m, ok := mob.OfHandle(tx, h); ok {
				m.Hurt(1000, entity.AttackDamageSource{Attacker: pl})
			}
		}
	})
	if n, ok := w.TickUntil(200, func(tx *world.Tx) bool { return raid.State >= vc.RaidFinished }); !ok {
		t.Fatalf("raid not won %d ticks after the wave died (state %v)", n, raid.State)
	}
	w.Do(func(tx *world.Tx) {
		pl, _ := p.H().Entity(tx)
		found := false
		for _, e := range pl.(*player.Player).Effects() {
			found = found || e.Type() == vc.HeroOfTheVillage
		}
		if !found {
			t.Error("killer did not get hero of the village")
		}
		m, _ := v.Mob(tx)
		if !m.HasComponent("minecraft:behavior.celebrate_survive") {
			t.Error("villager does not celebrate the victory")
		}
	})
}

// TestIronGolemDefends: an iron golem of the village targets an aggressor
// of its villagers (defend_village_target, attack_chance 0.05).
func TestIronGolemDefends(t *testing.T) {
	needs(t, "minecraft:behavior.defend_village_target", "minecraft:dweller")
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 24, block.Stone{})
		bed(tx, cube.Pos{2, 1, 2})
	})
	v := villager(w, mgl64.Vec3{0.5, 1, 0.5}, "minecraft:spawn_farmer")
	g := w.Spawn("minecraft:iron_golem", mgl64.Vec3{-3.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:from_village"})
	if n, ok := w.TickUntil(400, func(tx *world.Tx) bool { _, ok := villageOf(v, tx); return ok }); !ok {
		t.Fatalf("no village after %d ticks", n)
	}
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{6.5, 1, 6.5}, mob.SpawnOptions{Event: "minecraft:as_adult"})
	w.Do(func(tx *world.Tx) {
		vil, _ := villageOf(v, tx)
		gm, _ := g.Mob(tx)
		if d, ok := vc.DwellerOf(gm); ok && d.Village() == nil {
			d.SetVillage(gm, vil)
		}
		ze, _ := z.Entity(tx)
		vil.AddAggressor(ze)
	})
	if n, ok := w.TickUntil(300, func(tx *world.Tx) bool {
		gm, _ := g.Mob(tx)
		t, ok := gm.Target()
		return ok && t.H() == z.H()
	}); !ok {
		t.Fatalf("golem did not target the aggressor in %d ticks:\n%v", n, w.Dump(g))
	}
}

// TestPiglinBarter: a piglin that picks up gold admires it (off hand,
// admiring flag) for 8 s, then throws barter loot.
func TestPiglinBarter(t *testing.T) {
	needs(t, "minecraft:admire_item", "minecraft:barter", "minecraft:behavior.admire_item", "minecraft:behavior.barter")
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 16, block.Stone{}) })
	pg := w.Spawn("minecraft:piglin", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "spawn_adult"})
	w.Tick(2)
	pg.With(func(m *mob.Mob) {
		sh, ok := vc.ShareablesOf(m)
		if !ok {
			t.Fatal("piglin has no shareables")
		}
		if n := sh.PickedUp(m, item.NewStack(item.GoldIngot{}, 1), nil); n != 1 {
			t.Fatalf("piglin took %d gold ingots, want 1", n)
		}
	})
	if n, ok := w.TickUntil(10, func(tx *world.Tx) bool {
		m, _ := pg.Mob(tx)
		_, off := m.HeldItems()
		return m.ActorFlag(vc.FlagAdmiring) && !off.Empty()
	}); !ok {
		t.Fatalf("piglin not admiring after %d ticks:\n%v", n, w.Dump(pg))
	}
	if n, ok := w.TickUntil(300, func(tx *world.Tx) bool {
		for e := range tx.Entities() {
			if e.H().Type().EncodeEntity() == "minecraft:item" {
				return true
			}
		}
		return false
	}); !ok {
		t.Fatalf("no barter loot %d ticks later:\n%v", n, w.Dump(pg))
	}
}

// TestEndermanTakesBlock: an enderman on grass picks up a block
// (take_block, chance 0.05 a tick).
func TestEndermanTakesBlock(t *testing.T) {
	needs(t, "minecraft:behavior.take_block")
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 8, block.Dirt{})
		w.Fill(tx, cube.Pos{-3, 1, -3}, cube.Pos{3, 3, 3}, block.Dirt{})
		w.Clear(tx, cube.Pos{0, 1, 0}, cube.Pos{0, 3, 0})
	})
	e := w.Spawn("minecraft:enderman", mgl64.Vec3{0.5, 1, 0.5})
	if n, ok := w.TickUntil(600, func(tx *world.Tx) bool {
		m, _ := e.Mob(tx)
		_, carried := gv.Carried(m)
		return carried
	}); !ok {
		t.Fatalf("enderman carries nothing after %d ticks:\n%v", n, w.Dump(e))
	}
}

// TestCelebrateHunt: a piglin that kills a hoglin celebrates.
func TestCelebrateHunt(t *testing.T) {
	needs(t, "minecraft:celebrate_hunt")
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 16, block.Stone{}) })
	pg := w.Spawn("minecraft:piglin", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "spawn_adult"})
	h := w.Spawn("minecraft:hoglin", mgl64.Vec3{3.5, 1, 0.5})
	w.Tick(2)
	w.Do(func(tx *world.Tx) {
		pm, _ := pg.Mob(tx)
		hm, _ := h.Mob(tx)
		hm.Hurt(1000, entity.AttackDamageSource{Attacker: pm})
		if !pm.ActorFlag(vc.FlagCelebrating) && !pm.ActorFlag(vc.FlagCelebratingSpecial) {
			t.Error("piglin does not celebrate the hunt")
		}
	})
}

// TestFollowCaravan: a llama joins the caravan of a leashed llama.
func TestFollowCaravan(t *testing.T) {
	needs(t, "minecraft:behavior.follow_caravan")
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 16, block.Stone{}) })
	trader := w.Spawn("minecraft:wandering_trader", mgl64.Vec3{0.5, 1, 0.5})
	lead := w.Spawn("minecraft:trader_llama", mgl64.Vec3{2.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_adult"})
	tail := w.Spawn("minecraft:llama", mgl64.Vec3{6.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_adult"})
	w.Do(func(tx *world.Tx) {
		lm, _ := lead.Mob(tx)
		te, _ := trader.Entity(tx)
		vc.SetLeashHolder(lm, te)
	})
	if n, ok := w.TickUntil(200, func(tx *world.Tx) bool {
		m, _ := tail.Mob(tx)
		h, ok := vc.CaravanHead(m)
		return ok && h.H() == lead.H()
	}); !ok {
		t.Fatalf("llama did not join the caravan in %d ticks:\n%v", n, w.Dump(tail))
	}
}

// TestDrinkMilk: an invisible wandering trader at day drinks milk and loses
// its effects.
func TestDrinkMilk(t *testing.T) {
	needs(t, "minecraft:behavior.drink_milk")
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 16, block.Stone{}) })
	tr := w.Spawn("minecraft:wandering_trader", mgl64.Vec3{0.5, 1, 0.5})
	tr.With(func(m *mob.Mob) { m.AddEffect(effect.New(effect.Invisibility, 1, time.Minute)) })
	if n, ok := w.TickUntil(400, func(tx *world.Tx) bool {
		m, _ := tr.Mob(tx)
		return len(m.Effects()) == 0
	}); !ok {
		t.Fatalf("trader kept its effects %d ticks:\n%v", n, w.Dump(tr))
	}
}

// TestPiglinPicksUpGold: pickup_items hands a dropped gold ingot to the
// piglin's shareables (admire: true), which start admiring it.
func TestPiglinPicksUpGold(t *testing.T) {
	needs(t, "minecraft:behavior.pickup_items", "minecraft:shareables", "minecraft:admire_item")
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 16, block.Stone{})
		tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{3.5, 1, 0.5}}, item.NewStack(item.GoldIngot{}, 1)))
	})
	pg := w.Spawn("minecraft:piglin", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "spawn_adult"})
	if n, ok := w.TickUntil(300, func(tx *world.Tx) bool {
		m, _ := pg.Mob(tx)
		a, ok := vc.AdmireOf(m)
		return ok && a.Admiring
	}); !ok {
		t.Fatalf("piglin did not pick up and admire the gold in %d ticks:\n%v", n, w.Dump(pg))
	}
}

// TestScheduler: the villager schedule fires the work event in the morning
// and the bed event at night (minecraft:scheduler, hourly_clock_time).
func TestScheduler(t *testing.T) {
	needs(t, "minecraft:scheduler")
	w := mobtest.New(t, mobtest.Options{Time: 1000})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 16, block.Stone{}) })
	v := villager(w, mgl64.Vec3{0.5, 1, 0.5}, "minecraft:spawn_farmer")
	v.With(func(m *mob.Mob) { m.Fire("minecraft:become_farmer", nil) })
	w.Tick(3)
	v.With(func(m *mob.Mob) {
		if !m.HasComponent("minecraft:behavior.harvest_farm_block") {
			t.Errorf("no work schedule at 1000: %v", m.Components())
		}
	})
	w.W.SetTime(13000)
	w.Tick(3)
	v.With(func(m *mob.Mob) {
		if !m.HasComponent("minecraft:behavior.sleep") {
			t.Errorf("no bed schedule at 13000: %v", m.Components())
		}
	})
}
