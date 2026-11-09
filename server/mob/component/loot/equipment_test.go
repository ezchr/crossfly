//go:build crossfly_full

package loot_test

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/component/loot"
	"github.com/df-mc/dragonfly/server/mob/debugcmd"
	"github.com/df-mc/dragonfly/server/mob/goal/combat"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	_ "github.com/df-mc/dragonfly/server/mob/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// gear is what one mob had after its first ticks.
type gear struct {
	main, off               item.Stack
	head, chest, legs, feet item.Stack
	baby                    bool
}

func name(s item.Stack) string {
	if s.Empty() {
		return ""
	}
	n, _ := s.Item().EncodeItem()
	return strings.TrimPrefix(n, "minecraft:")
}

func gearOf(m *mob.Mob) gear {
	a := m.Armour()
	main, off := m.HeldItems()
	return gear{main: main, off: off, head: a.Helmet(), chest: a.Chestplate(), legs: a.Leggings(), feet: a.Boots(),
		baby: m.Meta().Baby}
}

// spawnGear spawns n mobs of id in batches, ticks each batch twice so the
// equipment rolls, records the gear and removes the batch.
func spawnGear(t *testing.T, opts mobtest.Options, id string, n int) []gear {
	t.Helper()
	w := mobtest.New(t, opts)
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 12, block.Stone{}) })
	var out []gear
	for done := 0; done < n; {
		batch := min(100, n-done)
		var refs []*mobtest.Ref
		w.Do(func(tx *world.Tx) {
			for i := range batch {
				pos := mgl64.Vec3{float64(i%10) - 4.5, 1, float64(i/10) - 4.5}
				refs = append(refs, w.SpawnTx(tx, id, pos, mob.SpawnOptions{}))
			}
		})
		w.Tick(2)
		w.Do(func(tx *world.Tx) {
			for _, r := range refs {
				if m, ok := r.Mob(tx); ok {
					out = append(out, gearOf(m))
					_ = m.Close()
				}
			}
		})
		done += batch
	}
	return out
}

func count(gs []gear, f func(g gear) bool) int {
	n := 0
	for _, g := range gs {
		if f(g) {
			n++
		}
	}
	return n
}

func summary(gs []gear) string {
	c := map[string]int{}
	for _, g := range gs {
		for slot, s := range map[string]item.Stack{"main": g.main, "off": g.off, "head": g.head, "chest": g.chest, "legs": g.legs, "feet": g.feet} {
			if !s.Empty() {
				c[slot+"="+name(s)]++
			}
		}
	}
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%v:%d ", k, c[k])
	}
	return b.String()
}

// within checks that got out of n is within 4 standard deviations of the
// expected rate p.
func within(t *testing.T, what string, got, n int, p float64) {
	t.Helper()
	sd := math.Sqrt(float64(n) * p * (1 - p))
	exp := float64(n) * p
	if math.Abs(float64(got)-exp) > 4*sd+1 {
		t.Errorf("%v: %d of %d, want about %.1f (rate %.4f)", what, got, n, exp, p)
	}
}

// TestFixedGear: mobs whose table always gives the same weapon.
func TestFixedGear(t *testing.T) {
	cases := []struct {
		id, main, off string
	}{
		{"minecraft:skeleton", "bow", ""},
		{"minecraft:stray", "bow", ""},
		{"minecraft:wither_skeleton", "stone_sword", ""},
		{"minecraft:pillager", "crossbow", ""},
		{"minecraft:vindicator", "iron_axe", ""},
		{"minecraft:vex", "iron_sword", ""},
	}
	for _, c := range cases {
		gs := spawnGear(t, mobtest.Options{}, c.id, 40)
		if n := count(gs, func(g gear) bool { return name(g.main) == c.main }); n != len(gs) {
			t.Errorf("%v: %d of %d hold %v: %v", c.id, n, len(gs), c.main, summary(gs))
		}
		t.Logf("%v: %v", c.id, summary(gs))
	}
}

// TestZombieGearHard: zombie_equipment.json on Hard at night of day 0, special
// multiplier 0.875: a weapon 5% x 0.875, armour (at least a helmet) 15% x
// 0.875; the weapon is a spear, sword or shovel 1:2:3.
func TestZombieGearHard(t *testing.T) {
	const n = 1000
	mult := loot.SpecialMultiplierAt(3, mobtest.Midnight)
	if math.Abs(mult-0.875) > 1e-9 {
		t.Fatalf("special multiplier %v, want 0.875", mult)
	}
	gs := spawnGear(t, mobtest.Options{Difficulty: world.DifficultyHard}, "minecraft:zombie", n)
	t.Logf("zombie (hard): %v", summary(gs))
	within(t, "zombie weapon", count(gs, func(g gear) bool { return !g.main.Empty() }), n, 0.05*mult)
	within(t, "zombie armour", count(gs, func(g gear) bool { return !g.head.Empty() }), n, 0.15*mult)
	if n := count(gs, func(g gear) bool { return g.head.Empty() && !g.chest.Empty() }); n != 0 {
		t.Errorf("%d zombies with a chestplate but no helmet", n)
	}
	// A full set needs all three 0.6 rolls on Hard.
	helm := count(gs, func(g gear) bool { return !g.head.Empty() })
	full := count(gs, func(g gear) bool { return !g.feet.Empty() })
	if helm > 0 && float64(full)/float64(helm) > 0.6 {
		t.Errorf("%d full sets of %d armoured zombies: too many", full, helm)
	}
}

// TestZombieGearEasy: Easy has special multiplier 0 and the armour pool's
// difficulty chance is 0: no gear at all.
func TestZombieGearEasy(t *testing.T) {
	gs := spawnGear(t, mobtest.Options{Difficulty: world.DifficultyEasy}, "minecraft:zombie", 300)
	if s := summary(gs); s != "" {
		t.Errorf("easy zombies with gear: %v", s)
	}
}

// TestSplitGear: piglins, drowned, zombified piglins and foxes.
func TestSplitGear(t *testing.T) {
	const n = 400
	pig := spawnGear(t, mobtest.Options{}, "minecraft:piglin", n)
	t.Logf("piglin: %v", summary(pig))
	adults := count(pig, func(g gear) bool { return !g.baby })
	within(t, "piglin adults", adults, n, 0.95)
	cross := count(pig, func(g gear) bool { return name(g.main) == "crossbow" })
	gold := count(pig, func(g gear) bool { return name(g.main) == "golden_sword" || name(g.main) == "golden_spear" })
	if cross+gold != adults {
		t.Errorf("piglin: %d crossbows + %d golden weapons != %d adults", cross, gold, adults)
	}
	within(t, "piglin crossbow", cross, adults, 0.5)
	if n := count(pig, func(g gear) bool { return g.baby && !g.main.Empty() }); n != 0 {
		t.Errorf("%d baby piglins hold a weapon", n)
	}

	dr := spawnGear(t, mobtest.Options{}, "minecraft:drowned", n)
	t.Logf("drowned: %v", summary(dr))
	within(t, "drowned trident", count(dr, func(g gear) bool { return name(g.main) == "trident" }), n, 25.0/400)
	within(t, "drowned nautilus shell", count(dr, func(g gear) bool { return name(g.off) == "nautilus_shell" }), n, 0.08)
	within(t, "drowned fishing rod", count(dr, func(g gear) bool { return name(g.main) == "fishing_rod" }), n, 375.0/400*0.0375)

	zp := spawnGear(t, mobtest.Options{}, "minecraft:zombie_pigman", 200)
	t.Logf("zombie_pigman: %v", summary(zp))
	if c := count(zp, func(g gear) bool { return name(g.main) == "golden_sword" || name(g.main) == "golden_spear" }); c != len(zp) {
		t.Errorf("zombified piglin: %d of %d with a golden weapon", c, len(zp))
	}

	fox := spawnGear(t, mobtest.Options{}, "minecraft:fox", n)
	t.Logf("fox: %v", summary(fox))
	within(t, "fox with an item in its mouth", count(fox, func(g gear) bool { return !g.main.Empty() }), n, 0.95*0.2)
}

// TestEnchantRandomGear: enchant_random_gear on vindicator axes, chance 0.25
// x the special multiplier (Hard, night of day 0: 0.875).
func TestEnchantRandomGear(t *testing.T) {
	const n = 400
	gs := spawnGear(t, mobtest.Options{Difficulty: world.DifficultyHard}, "minecraft:vindicator", n)
	ench := count(gs, func(g gear) bool { return len(g.main.Enchantments()) > 0 })
	within(t, "enchanted vindicator axes", ench, n, 0.25*0.875)
	for _, g := range gs {
		for _, e := range g.main.Enchantments() {
			if !e.Type().CompatibleWithItem(g.main.Item()) {
				t.Errorf("axe got %v", e.Type().Name())
			}
		}
	}
}

func TestSpecialMultiplier(t *testing.T) {
	for _, c := range []struct {
		d, time int
		want    float64
	}{
		{0, 0, 0}, {1, 0, 0}, {2, 0, 0.125}, {3, 0, 0.875},
		// Day 63 (time 1.5M), full moon: time part 0.25, moon 0.25.
		{2, 24000 * 64, (2*(0.375+0.25+0.25+0.75) - 2) / 2},
		{3, 24000 * 64, 1},
	} {
		if got := loot.SpecialMultiplierAt(c.d, c.time); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("SpecialMultiplierAt(%d, %d) = %v, want %v", c.d, c.time, got, c.want)
		}
	}
}

// itemsAround returns the item entities in the world and removes them.
func takeItems(tx *world.Tx) []item.Stack {
	var out []item.Stack
	for e := range tx.Entities() {
		if e.H().Type() != entity.ItemType {
			continue
		}
		if ent, ok := e.(*entity.Ent); ok {
			if b, ok := ent.Behaviour().(*entity.ItemBehaviour); ok {
				out = append(out, b.Item())
			}
		}
		_ = e.Close()
	}
	return out
}

func countItems(ss []item.Stack, n string) int {
	c := 0
	for _, s := range ss {
		if name(s) == n {
			c += s.Count()
		}
	}
	return c
}

func playerEntity(t *testing.T, tx *world.Tx, p *mobtest.Player) world.Entity {
	e, ok := p.Entity(tx)
	if !ok {
		t.Fatal("player gone")
	}
	return e
}

// TestEquipmentDrops: 8.5% per slot when a player kills (+1% per Looting
// level), nothing for other deaths, drop chance 1 always, picked-up items
// always; the slots are empty after death.
func TestEquipmentDrops(t *testing.T) {
	const n = 600
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 12, block.Stone{}) })
	p := w.AddPlayer("killer", mgl64.Vec3{8.5, 1, 8.5})
	helmet := item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 1)
	sword := item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1)
	looting := item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1).WithEnchantments(item.NewEnchantment(loot.Looting, 3))

	run := func(held item.Stack, kill func(tx *world.Tx, m *mob.Mob), prep func(m *mob.Mob)) (helmets, swords int, drops []item.Stack) {
		p.Hold(held)
		for done := 0; done < n; done += 100 {
			var refs []*mobtest.Ref
			w.Do(func(tx *world.Tx) {
				for i := range 100 {
					refs = append(refs, w.SpawnTx(tx, "minecraft:zombie", mgl64.Vec3{float64(i%10) - 4.5, 1, float64(i/10) - 4.5}, mob.SpawnOptions{Event: "minecraft:as_adult"}))
				}
			})
			w.Tick(2)
			w.Do(func(tx *world.Tx) {
				takeItems(tx)
				for _, r := range refs {
					m, ok := r.Mob(tx)
					if !ok {
						continue
					}
					m.Armour().SetHelmet(helmet)
					m.SetHeldItems(sword, item.Stack{})
					if prep != nil {
						prep(m)
					}
					kill(tx, m)
					if !core.EquipmentSlot(m, core.SlotHead).Empty() || !core.EquipmentSlot(m, core.SlotMainHand).Empty() {
						t.Fatalf("a dead zombie still wears its gear")
					}
				}
				ss := takeItems(tx)
				helmets += countItems(ss, "iron_helmet")
				swords += countItems(ss, "iron_sword")
				drops = append(drops, ss...)
			})
			w.Do(func(tx *world.Tx) {
				for _, r := range refs {
					if m, ok := r.Mob(tx); ok {
						_ = m.Close()
					}
				}
			})
		}
		return
	}
	byPlayer := func(tx *world.Tx, m *mob.Mob) {
		// A small lethal hit: worn armour loses max(damage/4, 1) durability
		// (Mob::hurtArmorSlots), so a hit of 1000 would break the helmet.
		m.SetHealth(1)
		m.Hurt(5, entity.AttackDamageSource{Attacker: playerEntity(t, tx, p)})
	}
	byVoid := func(tx *world.Tx, m *mob.Mob) { m.Hurt(1000, entity.VoidDamageSource{}) }

	h, s, drops := run(item.Stack{}, byPlayer, nil)
	within(t, "helmets (player kill)", h, n, 0.085)
	within(t, "swords (player kill)", s, n, 0.085)
	for _, d := range drops {
		if (name(d) == "iron_helmet" || name(d) == "iron_sword") && d.Durability() == d.MaxDurability() {
			t.Errorf("a dropped %v was not worn down", name(d))
		}
	}
	h, s, _ = run(looting, byPlayer, nil)
	within(t, "helmets (Looting III)", h, n, 0.115)
	within(t, "swords (Looting III)", s, n, 0.115)
	h, s, _ = run(item.Stack{}, byVoid, nil)
	if h+s != 0 {
		t.Errorf("void deaths dropped %d helmets and %d swords", h, s)
	}
	h, s, drops = run(item.Stack{}, byVoid, func(m *mob.Mob) {
		main, off := m.HeldItems()
		m.SetHeldItems(loot.MarkPickedUp(main), off)
		core.SetDropChance(m, core.SlotHead, 1)
	})
	if s != n || h != n {
		t.Errorf("picked-up swords %d and chance-1 helmets %d of %d dropped", s, h, n)
	}
	for _, d := range drops {
		if loot.WasPickedUp(d) {
			t.Fatal("a dropped item still has the picked-up mark")
		}
		if d.Durability() != d.MaxDurability() {
			t.Fatalf("a guaranteed drop was damaged: %v/%v", d.Durability(), d.MaxDurability())
		}
	}
}

// TestDrownedOffhandDrop: drowned list slot.weapon.offhand with chance 1:
// their nautilus shell always drops, even without a player.
func TestDrownedOffhandDrop(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 6, block.Stone{}) })
	d := w.Spawn("minecraft:drowned", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:as_adult"})
	w.Tick(2)
	w.Do(func(tx *world.Tx) {
		m, _ := d.Mob(tx)
		if c := core.DropChance(m, core.SlotOffHand); c != 1 {
			t.Fatalf("drowned off-hand drop chance %v, want 1", c)
		}
		takeItems(tx)
		m.SetHeldItems(item.Stack{}, item.NewStack(item.NautilusShell{}, 1))
		m.Hurt(1000, entity.VoidDamageSource{})
		if got := countItems(takeItems(tx), "nautilus_shell"); got != 1 {
			t.Fatalf("drowned dropped %d nautilus shells, want 1", got)
		}
	})
}

// TestLootingLoot: zombie rotten flesh is 0-2 plus round(U(0,1) x Looting).
func TestLootingLoot(t *testing.T) {
	const n = 300
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 12, block.Stone{}) })
	p := w.AddPlayer("killer", mgl64.Vec3{8.5, 1, 8.5})
	avg := func(held item.Stack) float64 {
		p.Hold(held)
		total := 0
		for done := 0; done < n; done += 100 {
			var refs []*mobtest.Ref
			w.Do(func(tx *world.Tx) {
				for i := range 100 {
					refs = append(refs, w.SpawnTx(tx, "minecraft:zombie", mgl64.Vec3{float64(i%10) - 4.5, 1, float64(i/10) - 4.5}, mob.SpawnOptions{Event: "minecraft:as_adult"}))
				}
			})
			w.Tick(2)
			w.Do(func(tx *world.Tx) {
				takeItems(tx)
				for _, r := range refs {
					if m, ok := r.Mob(tx); ok {
						m.Hurt(1000, entity.AttackDamageSource{Attacker: playerEntity(t, tx, p)})
					}
				}
				total += countItems(takeItems(tx), "rotten_flesh")
				for _, r := range refs {
					if m, ok := r.Mob(tx); ok {
						_ = m.Close()
					}
				}
			})
		}
		return float64(total) / n
	}
	a0 := avg(item.Stack{})
	a3 := avg(item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1).WithEnchantments(item.NewEnchantment(loot.Looting, 3)))
	t.Logf("rotten flesh per zombie: %.2f without Looting, %.2f with Looting III", a0, a3)
	if math.Abs(a0-1) > 0.2 || math.Abs(a3-a0-1.5) > 0.3 {
		t.Errorf("rotten flesh averages %.2f / %.2f, want about 1 / 2.5", a0, a3)
	}
}

// TestDeathExperience: the experience drops with the corpse 20 ticks after
// a player kill (5 for an adult zombie, 12 for a baby; the armour already
// dropped, so equipment_count is 0), nothing for other deaths.
func TestDeathExperience(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 6, block.Stone{}) })
	p := w.AddPlayer("killer", mgl64.Vec3{4.5, 1, 4.5})
	orbs := func() int {
		n := 0
		w.Do(func(tx *world.Tx) {
			for e := range tx.Entities() {
				if ent, ok := e.(*entity.Ent); ok {
					if b, ok := ent.Behaviour().(*entity.ExperienceOrbBehaviour); ok {
						n += b.Experience()
						_ = e.Close()
					}
				}
			}
		})
		return n
	}
	for _, c := range []struct {
		event  string
		player bool
		want   int
	}{{"minecraft:as_adult", true, 5}, {"minecraft:as_baby", true, 12}, {"minecraft:as_adult", false, 0}} {
		z := w.Spawn("minecraft:zombie", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: c.event})
		w.Tick(2)
		w.Do(func(tx *world.Tx) {
			m, _ := z.Mob(tx)
			m.Armour().SetHelmet(item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 1))
			if c.player {
				m.Hurt(1000, entity.AttackDamageSource{Attacker: playerEntity(t, tx, p)})
			} else {
				m.Hurt(1000, entity.VoidDamageSource{})
			}
		})
		if got := orbs(); got != 0 {
			t.Errorf("%v: %d experience at death, want it with the corpse", c.event, got)
		}
		w.Tick(25)
		if got := orbs(); got != c.want {
			t.Errorf("%v (player kill %v): %d experience, want %d", c.event, c.player, got, c.want)
		}
		w.Do(func(tx *world.Tx) { takeItems(tx) })
	}
}

// TestWeaponDamage: a mob's melee hit adds its weapon (Sharpness included).
func TestWeaponDamage(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	sharp := item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1).WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 5))
	for i, held := range []item.Stack{{}, item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1), sharp} {
		p := w.AddPlayer(fmt.Sprintf("target%d", i), mgl64.Vec3{float64(i*3) + 0.5, 1, 3.5})
		z := w.Spawn("minecraft:zombie", mgl64.Vec3{float64(i*3) + 0.5, 1, 2.5}, mob.SpawnOptions{Event: "minecraft:as_adult"})
		w.Tick(2)
		before := p.Health()
		var base float64
		w.Do(func(tx *world.Tx) {
			m, _ := z.Mob(tx)
			m.SetHeldItems(held, item.Stack{})
			base = m.Attr(mob.AttrAttackDamage)
			combat.DoHurtTarget(m, playerEntity(t, tx, p))
		})
		want := base + loot.WeaponDamage(held)
		if got := before - p.Health(); math.Abs(got-want) > 1e-6 {
			t.Errorf("hit with %v: %v damage, want %v", name(held), got, want)
		}
	}
}

// TestChargedCreeperHead: a charged creeper's explosion makes a zombie drop
// its head.
func TestChargedCreeperHead(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 6, block.Stone{}) })
	c := w.Spawn("minecraft:creeper", mgl64.Vec3{2.5, 1, 0.5})
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:as_adult"})
	w.Tick(1)
	w.Do(func(tx *world.Tx) {
		cm, _ := c.Mob(tx)
		zm, _ := z.Mob(tx)
		cm.UpdateMeta(func(m *mob.Meta) { m.Charged = true })
		takeItems(tx)
		zm.Hurt(1000, entity.ExplosionDamageSource{Source: world.EntityExplosionSource{Entity: cm, ExplosionSize: 3}})
		if n := countItems(takeItems(tx), "zombie_head"); n != 1 {
			t.Fatalf("%d zombie heads, want 1", n)
		}
		// Mob::die matches the actor type exactly: a husk drops no head.
		h := w.SpawnTx(tx, "minecraft:husk", mgl64.Vec3{0.5, 1, 2.5}, mob.SpawnOptions{Event: "minecraft:as_adult"})
		hm, _ := h.Mob(tx)
		hm.Hurt(1000, entity.ExplosionDamageSource{Source: world.EntityExplosionSource{Entity: cm, ExplosionSize: 3}})
		if n := countItems(takeItems(tx), "zombie_head"); n != 0 {
			t.Fatalf("a husk dropped %d zombie heads, want 0", n)
		}
	})
}

// TestDebugGear spawns each mob /mobdebug gear spawns, rolls its gear at
// once and prints it the way the command does.
func TestDebugGear(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{Difficulty: world.DifficultyHard})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 12, block.Stone{}) })
	w.Do(func(tx *world.Tx) {
		for i, id := range debugcmd.GearMobs {
			r := w.SpawnTx(tx, id, mgl64.Vec3{float64(i) - 6.5, 1, 0.5}, mob.SpawnOptions{})
			m, _ := r.Mob(tx)
			if !core.RollEquipment(m) && id != "minecraft:fox" && id != "minecraft:piglin" && id != "minecraft:piglin_brute" {
				t.Errorf("%v has no minecraft:equipment", id)
			}
			s := debugcmd.EquipmentString(m)
			if !strings.Contains(s, "mainhand") {
				t.Errorf("%v: %q", id, s)
			}
			t.Log(strings.ReplaceAll(strings.TrimSpace(s), "\n", ";"))
		}
	})
}
