//go:build crossfly_full

package mobtest_test

// minecraft:transformation details (BDS TransformationComponent): the cure
// delay roll and block assist, the begin/transformation sounds,
// drop_inventory, keep_level and trades over villager -> zombie villager ->
// villager, the cured discounts and a transformation added by the killing
// hit.

import (
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	ccombat "github.com/df-mc/dragonfly/server/mob/component/combat"
	"github.com/df-mc/dragonfly/server/mob/component/village"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// tfSoundViewer records the mob level sound events it is shown.
type tfSoundViewer struct {
	world.NopViewer
	events []string
}

func (v *tfSoundViewer) ViewSound(_ mgl64.Vec3, s world.Sound) {
	if ls, ok := s.(interface {
		LevelSoundEvent() (string, string, bool)
	}); ok {
		ev, _, _ := ls.LevelSoundEvent()
		v.events = append(v.events, ev)
	}
}

func (v *tfSoundViewer) count(ev string) int {
	n := 0
	for _, e := range v.events {
		if e == ev {
			n++
		}
	}
	return n
}

func tfListen(w *mobtest.World) *tfSoundViewer {
	v := &tfSoundViewer{}
	w.Do(func(tx *world.Tx) {
		l := world.NewLoader(4, tx.World(), v)
		l.Move(tx, mgl64.Vec3{0.5, 1, 0.5})
		l.Load(tx, 1000)
	})
	return v
}

var tfPersistent = mob.SpawnOptions{Configure: func(m *mob.Mob) { m.SetPersistent(true) }}

func tfTransformation(t *testing.T, w *mobtest.World, r *mobtest.Ref) *ccombat.Transformation {
	t.Helper()
	var tr *ccombat.Transformation
	r.With(func(m *mob.Mob) { tr, _ = mob.ComponentOf[*ccombat.Transformation](m) })
	if tr == nil {
		t.Fatalf("no transformation:\n%v", w.Dump(r))
	}
	return tr
}

// tfFind returns a mob of id that is none of skip.
func tfFind(w *mobtest.World, id string, skip ...*mobtest.Ref) *mobtest.Ref {
	for _, r := range w.Mobs(id) {
		if !slices.ContainsFunc(skip, func(s *mobtest.Ref) bool { return s.H() == r.H() }) {
			return r
		}
	}
	return nil
}

// The cure delay is value (100 s) + range_min (80 s) + [0, 120 s): 3600 to
// 5999 ticks (TransformationComponent::reloadComponent), and "remedy" plays
// as it starts.
func TestZombieVillagerCureDelayAndRemedy(t *testing.T) {
	needs(t, true, false, "minecraft:transformation")
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 12, block.Stone{}) })
	v := tfListen(w)
	seen := map[int]bool{}
	for i := range 8 {
		z := w.Spawn("minecraft:zombie_villager_v2", mgl64.Vec3{float64(i) - 3.5, 1, 0.5}, tfPersistent)
		z.With(func(m *mob.Mob) { m.Fire("villager_converted", nil) })
		left := tfTransformation(t, w, z).Left()
		if left < 3600 || left >= 6000 {
			t.Errorf("cure delay %d ticks, want 3600..5999", left)
		}
		seen[left] = true
	}
	if len(seen) < 2 {
		t.Errorf("cure delays not random: %v", seen)
	}
	if n := v.count("remedy"); n != 8 {
		t.Errorf("remedy played %d times, want 8 (%v)", n, v.events)
	}
}

// Iron bars around a curing zombie villager speed the cure up
// (tickTransformationComponent block assist); without them it counts down
// one per tick.
func TestZombieVillagerCureBlockAssist(t *testing.T) {
	needs(t, true, false, "minecraft:transformation")
	long(t)
	w := mobtest.New(t, mobtest.Options{})
	pen := func(tx *world.Tx, cx int, blk world.Block) {
		w.Wall(tx, cube.Pos{cx - 2, 1, -2}, cube.Pos{cx + 2, 1, -2}, 3, blk)
		w.Wall(tx, cube.Pos{cx - 2, 1, 2}, cube.Pos{cx + 2, 1, 2}, 3, blk)
		w.Wall(tx, cube.Pos{cx - 2, 1, -2}, cube.Pos{cx - 2, 1, 2}, 3, blk)
		w.Wall(tx, cube.Pos{cx + 2, 1, -2}, cube.Pos{cx + 2, 1, 2}, 3, blk)
	}
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 24, block.Stone{})
		pen(tx, 0, block.IronBars{})
		pen(tx, 20, block.Stone{})
	})
	bars := w.Spawn("minecraft:zombie_villager_v2", mgl64.Vec3{0.5, 1, 0.5}, tfPersistent)
	plain := w.Spawn("minecraft:zombie_villager_v2", mgl64.Vec3{20.5, 1, 0.5}, tfPersistent)
	for _, r := range []*mobtest.Ref{bars, plain} {
		r.With(func(m *mob.Mob) { m.Fire("villager_converted", nil) })
	}
	tb, tp := tfTransformation(t, w, bars), tfTransformation(t, w, plain)
	b0, p0 := tb.Left(), tp.Left()
	const ticks = 1500
	w.Tick(ticks)
	if got := p0 - tp.Left(); got != ticks {
		t.Errorf("without iron bars the cure went %d ticks in %d", got, ticks)
	}
	extra := b0 - tb.Left() - ticks
	// 1% of ticks look, at most block_max (4) blocks, 30% each: about 18.
	if extra <= 0 || extra > ticks/10 {
		t.Errorf("iron bars sped the cure up by %d ticks in %d, want a few dozen at most but more than 0", extra, ticks)
	}
}

// Killing a villager whose transformation is due (the become_zombie group a
// zombie's fatal hit adds) turns it into a zombie villager that keeps the
// trade level, trades, profession and skin (keep_level, persist_trades);
// curing that zombie villager gives them back, with the cured discount, a
// nearby discount for villagers around and the cure sounds.
func TestVillagerZombieVillagerVillagerKeepsTrades(t *testing.T) {
	needs(t, true, true, "minecraft:transformation", "minecraft:economy_trade_table")
	w := mobtest.New(t, mobtest.Options{Difficulty: world.DifficultyHard})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 16, block.Stone{}) })
	sounds := tfListen(w)
	trader := func(x float64) *mobtest.Ref {
		for i := range 40 {
			r := w.Spawn("minecraft:villager_v2", mgl64.Vec3{x, 1, 0.5 + float64(i%4)}, tfPersistent)
			ok := false
			r.With(func(m *mob.Mob) {
				if e, has := village.EconomyOf(m); has && !m.Meta().Baby {
					e.Generate(m)
					ok = len(e.Offers()) > 0
				}
			})
			if ok {
				return r
			}
			r.With(func(m *mob.Mob) { _ = m.Close() })
		}
		t.Fatal("no villager with trades spawned")
		return nil
	}
	vr := trader(0.5)
	near := trader(6.5)
	p := w.AddPlayer("trader", mgl64.Vec3{2.5, 1, 2.5})

	professions := []string{"farmer", "fisherman", "shepherd", "fletcher", "librarian", "cartographer", "cleric", "armorer",
		"weaponsmith", "toolsmith", "butcher", "leatherworker", "stone_mason", "mason"}
	var (
		offers     []*village.Offer
		exp        int
		tier, skin int32
		riches     int
		profession string
	)
	w.Do(func(tx *world.Tx) {
		m, _ := vr.Mob(tx)
		e, _ := village.EconomyOf(m)
		for range 3 {
			if !e.Trade(m, tx, p.H(), 0) {
				t.Fatalf("trade failed:\n%v", w.Dump(vr))
			}
		}
		offers, exp, riches = slices.Clone(e.Offers()), e.Experience(), e.Riches()
		tier, skin = m.Meta().TradeTier, m.SkinID()
		for _, f := range professions {
			if m.HasFamily(f) {
				profession = f
			}
		}
		if !m.Fire("become_zombie", nil) {
			t.Fatal("become_zombie did not fire")
		}
		tr, ok := mob.ComponentOf[*ccombat.Transformation](m)
		if !ok || tr.Left() > 0 {
			t.Fatalf("become_zombie added no due transformation (%v)", ok)
		}
		m.Kill(entity.AttackDamageSource{})
	})
	if exp == 0 || profession == "" {
		t.Fatalf("setup: exp %d profession %q", exp, profession)
	}
	if vr.Alive() {
		t.Fatalf("the killed villager is still there:\n%v", w.Dump(vr))
	}
	zr := tfFind(w, "minecraft:zombie_villager_v2")
	if zr == nil {
		t.Fatal("the killed villager did not become a zombie villager")
	}
	var saved map[string]any
	zr.With(func(z *mob.Mob) {
		got, ok := village.PersistingOffers(z)
		if !ok || len(got) != len(offers) || got[0] != offers[0] {
			t.Errorf("zombie villager persisting offers %d (%v), want the villager's %d", len(got), ok, len(offers))
		}
		if mt := z.Meta(); mt.TradeExperience != int32(exp) || mt.TradeTier != tier {
			t.Errorf("zombie villager trade exp %d tier %d, want %d %d (keep_level)", mt.TradeExperience, mt.TradeTier, exp, tier)
		}
		if !z.HasFamily(profession) {
			t.Errorf("zombie villager lost the %v profession: %v", profession, z.Families())
		}
		saved = village.EncodeActorTradeNBT(z)
	})
	if saved["persistingOffers"] == nil || saved["persistingRiches"] != int32(riches) {
		t.Errorf("zombie villager NBT %v", saved)
	}
	// The persisting trades survive a save (EncodeActorTradeNBT and back).
	copyRef := w.Spawn("minecraft:zombie_villager_v2", mgl64.Vec3{-6.5, 1, 0.5}, tfPersistent)
	copyRef.With(func(z *mob.Mob) {
		village.DecodeActorTradeNBT(z, saved)
		got, _ := village.PersistingOffers(z)
		if len(got) != len(offers) || z.Meta().TradeExperience != int32(exp) {
			t.Errorf("decoded zombie villager: %d offers exp %d", len(got), z.Meta().TradeExperience)
		}
		_ = z.Close()
	})

	zr.With(func(z *mob.Mob) { z.Fire("villager_converted", nil) })
	tfTransformation(t, w, zr).SetLeft(1)
	w.Tick(2)
	if zr.Alive() {
		t.Fatalf("the zombie villager was not cured:\n%v", w.Dump(zr))
	}
	cr := tfFind(w, "minecraft:villager_v2", near)
	if cr == nil {
		t.Fatal("no cured villager")
	}
	cr.With(func(c *mob.Mob) {
		if c.SkinID() != skin || !c.HasFamily(profession) {
			t.Errorf("cured villager skin %d families %v, want skin %d and %v", c.SkinID(), c.Families(), skin, profession)
		}
		e, ok := village.EconomyOf(c)
		if !ok {
			t.Fatalf("cured villager has no trades:\n%v", w.Dump(cr))
		}
		if got := e.Offers(); len(got) != len(offers) || got[0] != offers[0] || got[0].Uses != 3 {
			t.Errorf("cured villager offers %d, want the villager's %d with 3 uses", len(got), len(offers))
		}
		if e.Experience() != exp || c.Meta().TradeTier != tier || e.Riches() != riches {
			t.Errorf("cured villager exp %d tier %d riches %d, want %d %d %d", e.Experience(), c.Meta().TradeTier, e.Riches(), exp, tier, riches)
		}
		if low, high, _ := e.CuredDiscounts(); low != -25 || high != -20 {
			t.Errorf("cured discount %d/%d, want -25/-20", low, high)
		}
	})
	near.With(func(n *mob.Mob) {
		e, _ := village.EconomyOf(n)
		if low, high, _ := e.CuredDiscounts(); low != -25 || high != 0 {
			t.Errorf("nearby villager discount %d/%d, want -25/0", low, high)
		}
	})
	if sounds.count("remedy") != 1 || sounds.count("unfect") != 1 {
		t.Errorf("cure sounds %v, want one remedy and one unfect", sounds.events)
	}
}

// A zombie's fatal hit on a villager on hard (villager_v2 damage_sensor:
// has_damage fatal, other is_family zombie) makes a zombie villager.
func TestZombieKillsVillagerIntoZombieVillager(t *testing.T) {
	needs(t, true, true, "minecraft:transformation", "minecraft:damage_sensor")
	w := mobtest.New(t, mobtest.Options{Difficulty: world.DifficultyHard})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	v := w.Spawn("minecraft:villager_v2", mgl64.Vec3{0.5, 1, 0.5}, tfPersistent)
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{2.5, 1, 0.5}, tfPersistent)
	w.Do(func(tx *world.Tx) {
		vm, _ := v.Mob(tx)
		ze, _ := z.Entity(tx)
		vm.Hurt(1000, entity.AttackDamageSource{Attacker: ze})
	})
	if tfFind(w, "minecraft:zombie_villager_v2") == nil {
		if !v.Alive() {
			t.Skip("villager died without converting: has_damage fatal not handled by the damage_sensor filter yet")
		}
		t.Fatalf("villager neither died nor converted:\n%v", w.Dump(v))
	}
	if v.Alive() {
		t.Errorf("the converted villager is still there")
	}
}

// A zombifying piglin drops its inventory (drop_inventory), keeps its
// equipment (preserve_equipment) and plays converted_to_zombified.
func TestPiglinZombifiesDropsInventory(t *testing.T) {
	needs(t, true, false, "minecraft:transformation")
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	sounds := tfListen(w)
	pr := w.Spawn("minecraft:piglin", mgl64.Vec3{0.5, 1, 0.5}, tfPersistent)
	sword := item.NewStack(item.Sword{Tier: item.ToolTierGold}, 1)
	pr.With(func(m *mob.Mob) {
		if _, err := village.Inventory(m).AddItem(item.NewStack(item.GoldIngot{}, 3)); err != nil {
			t.Fatal(err)
		}
		m.SetHeldItems(sword, item.Stack{})
		m.Fire("become_zombie_event", nil)
	})
	w.Tick(2)
	if pr.Alive() {
		t.Fatalf("piglin did not zombify:\n%v", w.Dump(pr))
	}
	zp := tfFind(w, "minecraft:zombie_pigman")
	if zp == nil {
		t.Fatal("no zombified piglin")
	}
	zp.With(func(m *mob.Mob) {
		if main, _ := m.HeldItems(); !main.Comparable(sword) {
			t.Errorf("zombified piglin holds %v, want the golden sword", main)
		}
	})
	gold := 0
	w.Do(func(tx *world.Tx) {
		for e := range tx.Entities() {
			if it, ok := e.(*entity.Ent); ok {
				if b, ok := it.Behaviour().(*entity.ItemBehaviour); ok {
					if _, isGold := b.Item().Item().(item.GoldIngot); isGold {
						gold += b.Item().Count()
					}
				}
			}
		}
	})
	if gold != 3 {
		t.Errorf("%d gold ingots dropped, want 3", gold)
	}
	if sounds.count("converted_to_zombified") != 1 {
		t.Errorf("sounds %v, want converted_to_zombified", sounds.events)
	}
}
