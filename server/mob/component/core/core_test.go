//go:build crossfly_full

package core_test

import (
	"os"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	. "github.com/df-mc/dragonfly/server/mob/component/core/coretest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Definitions trimmed from the vanilla 1.26.63 cow, sheep and chicken (the
// component JSON is copied verbatim).
var (
	cow = &Def{ID: "test:cow", Category: "creature",
		Base: []mob.RawComponent{
			C("minecraft:type_family", `{"family":["cow","mob"]}`),
			C("minecraft:collision_box", `{"width":0.9,"height":1.4}`),
			C("minecraft:health", `{"value":10,"max":10}`),
			C("minecraft:movement", `{"value":0.25}`),
			C("minecraft:navigation.walk", `{}`),
			C("minecraft:offspring", `{"offspring_pairs":{"test:cow":"test:cow"}}`),
		},
		Groups: map[string][]mob.RawComponent{
			"adult": {
				C("minecraft:experience_reward", `{"on_bred":"Math.Random(1,7)","on_death":"query.last_hit_by_player ? Math.Random(1,3) : 0"}`),
				C("minecraft:loot", `{"table":"loot_tables/entities/cow.json"}`),
				C("minecraft:breedable", `{"require_tame":false,"breeds_with":{"test:cow":{}},"breed_items":["wheat"]}`),
				C("minecraft:interact", `{"interactions":[{"on_interact":{"filters":{"all_of":[{"test":"is_family","subject":"other","value":"player"},{"test":"has_equipment","domain":"hand","subject":"other","value":"bucket:0"}]}},"use_item":true,"swing":true,"transform_to_item":"bucket:1","play_sounds":"milk","interact_text":"action.interact.milk"}]}`),
			},
			"baby": {
				C("minecraft:is_baby", `{}`),
				C("minecraft:scale", `{"value":0.5}`),
				C("minecraft:ageable", `{"duration":1,"feed_items":"wheat","grow_up":{"event":"minecraft:ageable_grow_up","target":"self"}}`),
			},
		},
		Events: map[string]Event{
			mob.EventSpawned: {Add: []string{"adult"}},
			mob.EventBorn:    {Add: []string{"baby"}},
			mob.EventGrowUp:  {Remove: []string{"baby"}, Add: []string{"adult"}},
		},
	}
	sheep = &Def{ID: "test:sheep", Category: "creature",
		Base: []mob.RawComponent{
			C("minecraft:type_family", `{"family":["sheep","mob"]}`),
			C("minecraft:health", `{"value":8,"max":8}`),
			C("minecraft:interact", `{"interactions":[{"cooldown":2.5,"use_item":false,"swing":true,"hurt_item":1,"spawn_items":{"table":"loot_tables/entities/sheep_shear.json"},"play_sounds":["shear"],"interact_text":"action.interact.shear","vibration":"shear","on_interact":{"filters":{"all_of":[{"test":"has_equipment","subject":"other","domain":"hand","value":"shears"},{"test":"is_family","subject":"other","value":"player"},{"test":"has_component","operator":"!=","value":"minecraft:is_baby"},{"test":"has_component","value":"minecraft:is_dyeable"}]},"event":"minecraft:on_sheared","target":"self"}}]}`),
		},
		Groups: map[string][]mob.RawComponent{
			"dyeable": {C("minecraft:is_dyeable", `{"interact_text":"action.interact.dye"}`)},
			"sheared": {C("minecraft:is_sheared", `{}`)},
			"red":     {C("minecraft:color", `{"value":14}`)},
		},
		Events: map[string]Event{
			mob.EventSpawned:       {Add: []string{"dyeable", "red"}},
			"minecraft:on_sheared": {Remove: []string{"dyeable"}, Add: []string{"sheared"}},
		},
	}
	chicken = &Def{ID: "test:chicken", Category: "creature",
		Props: []mob.PropertyDecl{{Name: "minecraft:climate_variant", Type: "enum", Values: []string{"temperate", "warm", "cold"}, Default: "temperate"}},
		Base: []mob.RawComponent{
			C("minecraft:health", `{"value":4,"max":4}`),
			C("minecraft:spawn_entity", `{"entities":[
				{"min_wait_time":1,"max_wait_time":1,"spawn_sound":"plop","spawn_item":"egg","filters":[{"test":"rider_count","subject":"self","operator":"==","value":0},{"test":"enum_property","subject":"self","domain":"minecraft:climate_variant","value":"temperate"}]},
				{"min_wait_time":1,"max_wait_time":1,"spawn_sound":"plop","spawn_item":"blue_egg","filters":[{"test":"rider_count","subject":"self","operator":"==","value":0},{"test":"enum_property","subject":"self","domain":"minecraft:climate_variant","value":"cold"}]}]}`),
		},
	}
	misc = &Def{ID: "test:misc", Category: "monster",
		Base: []mob.RawComponent{
			C("minecraft:health", `{"value":20,"max":20}`),
			C("minecraft:breathable", `{"total_supply":1,"suffocate_time":0}`),
			C("minecraft:timer", `{"looping":false,"time":0.5,"time_down_event":{"event":"test:rang"}}`),
			C("minecraft:attack", `{"damage":3,"effect_name":"poison","effect_duration":7}`),
			C("minecraft:follow_range", `{"value":32,"max":32}`),
		},
	}
)

func TestMain(m *testing.M) {
	InstallFilters()
	Register(cow, sheep, chicken, misc)
	os.Exit(m.Run())
}

func floor(tx *world.Tx) { Floor(tx, 9, 6, Stone) }

var at = mgl64.Vec3{0.5, 10, 0.5}

func TestAttributesAndShape(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx)
		m, _ := Spawn(t, tx, "test:cow", at, "")
		if m.Speed() != 0.25 || m.MaxHealth() != 10 || m.Health() != 10 {
			Fatalf(t, "speed %v health %v/%v", m.Speed(), m.Health(), m.MaxHealth())
		}
		if !m.HasFamily("cow") || m.BBox().Width() < 0.89 || m.BBox().Height() < 1.39 {
			Fatalf(t, "families %v box %v", m.Families(), m.BBox())
		}
		baby, _ := Spawn(t, tx, "test:cow", at, mob.EventBorn)
		if !baby.Baby() || baby.Scale() != 0.5 || baby.BBox().Height() > 0.71 {
			Fatalf(t, "baby %v scale %v box %v", baby.Baby(), baby.Scale(), baby.BBox())
		}
		x, _ := Spawn(t, tx, "test:misc", at, "")
		if x.Attr(mob.AttrFollowRange) != 32 {
			Fatalf(t, "follow range %v", x.Attr(mob.AttrFollowRange))
		}
		a := core.AttackOf(x)
		if a.Damage != 3 || a.Effect == nil {
			Fatalf(t, "attack %+v", a)
		}
	})
}

func TestMilkCow(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx)
		m, _ := Spawn(t, tx, "test:cow", at, "")
		p := Player(tx, at.Add(mgl64.Vec3{1, 0, 0}), item.NewStack(item.Bucket{}, 1))
		held, _ := p.HeldItems()
		left, ok := m.Interact(p, held, m.Position())
		if !ok {
			Fatal(t, "milking was refused")
		}
		if b, ok := left.Item().(item.Bucket); !ok || b.Empty() || left.Count() != 1 {
			Fatalf(t, "got %v, want a milk bucket", left)
		}
		// A stack of buckets: one is used, the milk goes to the inventory.
		p.SetHeldItems(item.NewStack(item.Bucket{}, 3), item.Stack{})
		left, ok = m.Interact(p, item.NewStack(item.Bucket{}, 3), m.Position())
		if !ok || left.Count() != 2 {
			Fatalf(t, "stack: %v %v", left, ok)
		}
		found := false
		for _, s := range p.Inventory().Items() {
			if b, ok := s.Item().(item.Bucket); ok && !b.Empty() {
				found = true
			}
		}
		if !found {
			Fatal(t, "milk bucket not added to the inventory")
		}
		// Anything else is not milking.
		p.SetHeldItems(item.NewStack(item.Stick{}, 1), item.Stack{})
		if _, ok := m.Interact(p, item.NewStack(item.Stick{}, 1), m.Position()); ok {
			Fatal(t, "stick milked the cow")
		}
	})
}

func TestShearSheep(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx)
		m, _ := Spawn(t, tx, "test:sheep", at, "")
		if m.Colour() != 14 {
			Fatalf(t, "colour %v, want red (14)", m.Colour())
		}
		shears := item.NewStack(item.Shears{}, 1)
		p := Player(tx, at.Add(mgl64.Vec3{1, 0, 0}), shears)
		left, ok := m.Interact(p, shears, m.Position())
		if !ok {
			Fatal(t, "shearing was refused")
		}
		if left.Durability() != shears.Durability()-1 {
			Fatalf(t, "shears durability %v -> %v", shears.Durability(), left.Durability())
		}
		if !m.Sheared() || !InstOf(m).HasGroup("sheared") {
			Fatal(t, "sheep is not sheared")
		}
		n := 0
		for _, s := range Items(tx) {
			if w, ok := s.Item().(block.Wool); ok {
				if w.Colour != item.ColourRed() {
					Fatalf(t, "wool colour %v", w.Colour)
				}
				n += s.Count()
			}
		}
		if n < 1 || n > 3 {
			Fatalf(t, "%v wool dropped, want 1-3", n)
		}
		TickAll(tx, 60, m)
		p.SetHeldItems(left, item.Stack{})
		if _, ok := m.Interact(p, left, m.Position()); ok {
			Fatal(t, "a sheared sheep was sheared again")
		}
	})
}

func TestChickenLaysEgg(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx)
		m, _ := Spawn(t, tx, "test:chicken", at, "")
		TickAll(tx, 25, m)
		eggs := 0
		for _, s := range Items(tx) {
			if _, ok := s.Item().(item.Egg); ok {
				eggs += s.Count()
			}
		}
		if eggs != 1 {
			Fatalf(t, "%v eggs after 25 ticks, want 1", eggs)
		}
		// A warm chicken matches no entry here.
		w, _ := Spawn(t, tx, "test:chicken", at.Add(mgl64.Vec3{3, 0, 0}), "")
		_ = w.SetProperty("minecraft:climate_variant", "warm")
		before := len(Items(tx))
		TickAll(tx, 25, w)
		if len(Items(tx)) != before {
			Fatal(t, "a warm chicken laid a temperate egg")
		}
	})
}

func TestLootOnDeath(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx)
		p := Player(tx, at.Add(mgl64.Vec3{1, 0, 0}), item.Stack{})
		m, _ := Spawn(t, tx, "test:cow", at, "")
		m.Hurt(100, entity.AttackDamageSource{Attacker: p})
		if !m.Dead() {
			Fatal(t, "cow did not die")
		}
		beef := 0
		for _, s := range Items(tx) {
			if _, ok := s.Item().(item.Beef); ok {
				beef += s.Count()
			}
		}
		if beef < 1 || beef > 3 {
			Fatalf(t, "%v beef, want 1-3 (items %v)", beef, Items(tx))
		}
		// The experience drops with the corpse, 20 ticks after death.
		if xp := Experience(tx); xp != 0 {
			Fatalf(t, "%v experience at death, want none until the corpse goes", xp)
		}
		for i := range 20 {
			m.Tick(tx, int64(i))
		}
		if xp := Experience(tx); xp < 1 || xp > 3 {
			Fatalf(t, "%v experience, want 1-3", xp)
		}

	})
}

// TestLootBurning: a burning cow drops cooked beef; without a player kill
// there is no experience.
func TestLootBurning(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx)
		m, _ := Spawn(t, tx, "test:cow", at, "")
		m.SetOnFire(5e9)
		m.Hurt(100, entity.VoidDamageSource{})
		cooked := false
		for _, s := range Items(tx) {
			if b, ok := s.Item().(item.Beef); ok && b.Cooked {
				cooked = true
			}
		}
		if !cooked {
			Fatalf(t, "no cooked beef from a burning cow: %v", Items(tx))
		}
		if Experience(tx) != 0 {
			Fatal(t, "experience without a player kill")
		}
	})
}

func TestBreedableMate(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx)
		a, _ := Spawn(t, tx, "test:cow", at, "")
		b, _ := Spawn(t, tx, "test:cow", at.Add(mgl64.Vec3{1, 0, 0}), "")
		p := Player(tx, at.Add(mgl64.Vec3{0, 0, 2}), item.Stack{})
		ba, _ := core.BreedableOf(a)
		if ba.CanMate(a, b) {
			Fatal(t, "could mate before love")
		}
		for _, m := range []*mob.Mob{a, b} {
			left, ok := m.Interact(p, item.NewStack(item.Wheat{}, 2), m.Position())
			if !ok || left.Count() != 1 || !core.InLove(m) {
				Fatalf(t, "feeding wheat: %v %v %v", left, ok, core.InLove(m))
			}
		}
		if _, ok := a.Interact(p, item.NewStack(item.Wheat{}, 1), a.Position()); ok {
			Fatal(t, "fed a cow already in love")
		}
		if !ba.CanMate(a, b) {
			Fatal(t, "cannot mate in love")
		}
		babies := ba.Mate(a, b)
		if len(babies) != 1 || !babies[0].Baby() {
			Fatalf(t, "babies %v", babies)
		}
		if core.InLove(a) || core.InLove(b) || ba.Cooldown() == 0 {
			Fatal(t, "parents still in love / no cooldown")
		}
		if xp := Experience(tx); xp < 1 || xp > 7 {
			Fatalf(t, "breeding experience %v, want 1-7", xp)
		}
	})
}

func TestAgeableGrowsUp(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx)
		baby, _ := Spawn(t, tx, "test:cow", at, mob.EventBorn)
		TickAll(tx, 10, baby)
		if !baby.Baby() {
			Fatal(t, "grew up too early")
		}
		TickAll(tx, 15, baby)
		if baby.Baby() || !InstOf(baby).HasGroup("adult") {
			Fatal(t, "did not grow up after its duration")
		}
		// Feeding: 10% of 20 ticks per wheat.
		b2, _ := Spawn(t, tx, "test:cow", at, mob.EventBorn)
		p := Player(tx, at, item.Stack{})
		for i := 0; i < 10; i++ {
			if _, ok := b2.Interact(p, item.NewStack(item.Wheat{}, 1), at); !ok {
				Fatal(t, "feeding refused")
			}
		}
		if b2.Baby() {
			Fatal(t, "ten feedings did not grow the baby up")
		}
	})
}

func TestTimerAndBreathable(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx)
		m, _ := Spawn(t, tx, "test:misc", at, "")
		TickAll(tx, 15, m)
		n := strings.Count(strings.Join(InstOf(m).Fired, ","), "test:rang")
		if n != 1 {
			Fatalf(t, "timer fired %v times, want 1", n)
		}
		TickAll(tx, 40, m)
		if n := strings.Count(strings.Join(InstOf(m).Fired, ","), "test:rang"); n != 1 {
			Fatalf(t, "non-looping timer fired %v times", n)
		}
		if m.Health() != m.MaxHealth() {
			Fatal(t, "hurt breathing air")
		}
		for y := 10; y <= 12; y++ {
			tx.SetBlock(cube.Pos{0, y, 0}, block.Water{Still: true, Depth: 8}, nil)
		}
		TickAll(tx, 30, m)
		if m.Health() >= m.MaxHealth() {
			Fatalf(t, "did not drown: health %v", m.Health())
		}
	})
}

func TestMolang(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx)
		m, _ := Spawn(t, tx, "test:cow", at, "")
		for src, want := range map[string]float64{
			"1 + 2 * 3":                        7,
			"query.is_baby ? 0.02 : 0.001":     0.001,
			"!query.is_baby && 2 > 1":          1,
			"Math.Min(3, 5) + math.max(1, 2)":  5,
			"query.last_hit_by_player ? 5 : 0": 0,
			"(1 + 1) * -2":                     -4,
		} {
			e, err := core.CompileMolang(src)
			if err != nil {
				Fatalf(t, "%q: %v", src, err)
			}
			if got := e.Eval(m); got != want {
				t.Errorf("%q = %v, want %v", src, got, want)
			}
		}
		e, _ := core.CompileMolang("Math.Random(1,3)")
		for i := 0; i < 50; i++ {
			if v := e.Eval(m); v < 1 || v > 3 {
				Fatalf(t, "Math.Random(1,3) = %v", v)
			}
		}
	})
}

func TestItemDescriptors(t *testing.T) {
	for desc, want := range map[string]string{
		"bucket:1":  "minecraft:milk_bucket",
		"bucket:0":  "minecraft:bucket",
		"muttonRaw": "minecraft:mutton",
		"wool:14":   "minecraft:red_wool",
		"wheat":     "minecraft:wheat",
		"dye:4":     "minecraft:lapis_lazuli",
		"totem":     "minecraft:totem_of_undying",
	} {
		s, ok := core.ParseItemDescriptor(desc).Stack(1)
		if !ok {
			t.Errorf("%v did not resolve", desc)
			continue
		}
		if name, _ := s.Item().EncodeItem(); name != want {
			t.Errorf("%v = %v, want %v", desc, name, want)
		}
		if !core.ParseItemDescriptor(desc).Matches(s) {
			t.Errorf("%v does not match its own stack", desc)
		}
	}
}

// TestEmbeddedLootTables parses every embedded table and lists item names
// Dragonfly lacks.
func TestEmbeddedLootTables(t *testing.T) {
	for _, path := range []string{"loot_tables/entities/cow.json", "loot_tables/entities/sheep.json",
		"loot_tables/entities/chicken.json", "loot_tables/entities/pig.json", "loot_tables/entities/zombie.json",
		"loot_tables/entities/skeleton.json", "loot_tables/entities/spider.json", "loot_tables/entities/creeper.json",
		"loot_tables/entities/sheep_shear.json", "loot_tables/entities/zombie_equipment.json"} {
		if _, err := core.LoadLootTable(path); err != nil {
			t.Errorf("%v: %v", path, err)
		}
	}
}

func TestColourPersists(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx)
		m, _ := Spawn(t, tx, "test:sheep", at, "")
		core.SetColor(m, 3)
		data := m.Type().EncodeNBT(&world.EntityData{Data: behaviourOf(m)})
		h := world.EntitySpawnOpts{Position: at}.New(m.Type(), nbtConfig{m.Type(), data})
		m2, _ := mob.Of(tx.AddEntity(h))
		m2.Tick(tx, 1)
		if m2.Colour() != 3 {
			Fatalf(t, "colour after reload %v, want 3", m2.Colour())
		}
	})
}

type nbtConfig struct {
	t *mob.Type
	m map[string]any
}

func (c nbtConfig) Apply(data *world.EntityData) { c.t.DecodeNBT(c.m, data) }

func behaviourOf(m *mob.Mob) any { return m.Behaviour() }
