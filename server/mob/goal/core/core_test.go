//go:build crossfly_full

package core_test

import (
	"os"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	ccore "github.com/df-mc/dragonfly/server/mob/component/core"
	. "github.com/df-mc/dragonfly/server/mob/component/core/coretest"
	_ "github.com/df-mc/dragonfly/server/mob/goal/core"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

const (
	keyStroll  = "minecraft:behavior.random_stroll"
	keyLook    = "minecraft:behavior.look_at_player"
	keyAround  = "minecraft:behavior.random_look_around"
	keyFloat   = "minecraft:behavior.float"
	keyPanic   = "minecraft:behavior.panic"
	keyTempt   = "minecraft:behavior.tempt"
	keyBreed   = "minecraft:behavior.breed"
	keyFollow  = "minecraft:behavior.follow_parent"
	keyEat     = "minecraft:behavior.eat_block"
	keyMount   = "minecraft:behavior.mount_pathing"
	navigation = "minecraft:navigation.walk"
)

// A cow trimmed from vanilla 1.26.63 (goal JSON verbatim, apart from the
// probabilities the tests force).
var (
	cow = &Def{ID: "test:cow", Category: "creature",
		Base: []mob.RawComponent{
			C("minecraft:type_family", `{"family":["cow","mob"]}`),
			C("minecraft:collision_box", `{"width":0.9,"height":1.4}`),
			C("minecraft:health", `{"value":10,"max":10}`),
			C("minecraft:movement", `{"value":0.25}`),
			C(navigation, `{"can_path_over_water":true,"avoid_water":true,"avoid_damage_blocks":true}`),
			C("minecraft:offspring", `{"offspring_pairs":{"test:cow":"test:cow"}}`),
			C(keyFloat, `{"priority":0}`),
			C(keyPanic, `{"priority":1,"speed_multiplier":1.25}`),
			C(keyMount, `{"priority":2,"speed_multiplier":1.5,"target_dist":0.0,"track_target":true}`),
			C(keyTempt, `{"priority":4,"speed_multiplier":1.25,"items":["wheat"]}`),
		},
		Groups: map[string][]mob.RawComponent{
			"adult": {
				C("minecraft:breedable", `{"require_tame":false,"breeds_with":{"test:cow":{}},"breed_items":["wheat"]}`),
				C(keyBreed, `{"priority":3,"speed_multiplier":1.0}`),
			},
			"baby": {
				C("minecraft:is_baby", `{}`),
				C("minecraft:scale", `{"value":0.5}`),
				C(keyFollow, `{"priority":5,"speed_multiplier":1.1}`),
			},
		},
		Events: map[string]Event{
			mob.EventSpawned: {Add: []string{"adult"}},
			mob.EventBorn:    {Add: []string{"baby"}},
		},
	}
	// idle has the idle goals with forced probabilities.
	idle = &Def{ID: "test:idle", Category: "creature",
		Base: []mob.RawComponent{
			C("minecraft:health", `{"value":10,"max":10}`),
			C(navigation, `{}`),
			C(keyStroll, `{"priority":7,"speed_multiplier":0.8,"interval":0}`),
			C(keyLook, `{"priority":8,"look_distance":6.0,"probability":1}`),
			C(keyAround, `{"priority":9}`),
		},
	}
	sheep = &Def{ID: "test:sheep", Category: "creature",
		Base: []mob.RawComponent{
			C("minecraft:health", `{"value":8,"max":8}`),
			C(navigation, `{}`),
			C(keyEat, `{"priority":6,"success_chance":"query.is_baby ? 0.02 : 0.001","time_until_eat":1.8,"eat_and_replace_block_pairs":[{"eat_block":"grass","replace_block":"dirt"},{"eat_block":"tallgrass","replace_block":"air"},{"eat_block":"short_dry_grass","replace_block":"air"},{"eat_block":"tall_dry_grass","replace_block":"air"}],"on_eat":{"event":"minecraft:on_eat_block","target":"self"}}`),
		},
		Groups: map[string][]mob.RawComponent{
			"baby":    {C("minecraft:is_baby", `{}`)},
			"sheared": {C("minecraft:is_sheared", `{}`)},
		},
		Events: map[string]Event{
			mob.EventSpawned:         {Add: []string{"baby", "sheared"}},
			"minecraft:on_eat_block": {Remove: []string{"sheared"}},
		},
	}
)

func TestMain(m *testing.M) {
	InstallFilters()
	Register(cow, idle, sheep)
	os.Exit(m.Run())
}

var at = mgl64.Vec3{0.5, 10, 0.5}

func floor(tx *world.Tx, b world.Block) { Floor(tx, 9, 12, b) }

func running(m *mob.Mob, key string) bool { return slices.Contains(m.Goals().RunningGoals(), key) }

func TestRandomStroll(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx, Stone)
		m, c := Spawn(t, tx, "test:idle", at, "")
		TickAll(tx, 1, m)
		if !running(m, keyStroll) || c.Nav.MoveTos != 1 || c.Nav.Speed != 0.8 {
			Fatalf(t, "stroll: running %v moveTos %v speed %v", m.Goals().RunningGoals(), c.Nav.MoveTos, c.Nav.Speed)
		}
		d := c.Nav.LastDest
		if dx, dz := d[0]-at[0], d[2]-at[2]; dx*dx+dz*dz > 11*11*2 || d[1] != 10 {
			Fatalf(t, "destination %v is not a ground spot within 10 blocks", d)
		}
		// It stops once the navigation is done, and picks a new spot.
		TickAll(tx, 200, m)
		if c.Nav.MoveTos < 2 {
			Fatalf(t, "only %v strolls in 200 ticks", c.Nav.MoveTos)
		}
		// Without a path the goal ends at once.
		c.Nav.Fail = true
		c.Nav.Stop(m)
		n := c.Nav.MoveTos
		TickAll(tx, 3, m)
		if c.Nav.MoveTos == n {
			Fatal(t, "no new stroll tried")
		}
	})
}

func TestLookAtPlayer(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx, Stone)
		m, c := Spawn(t, tx, "test:idle", at, "")
		// The stroll (move|look, more important) would block the look.
		m.Goals().Remove(keyStroll)
		TickAll(tx, 1, m)
		if running(m, keyLook) {
			Fatal(t, "looked at nobody")
		}
		p := Player(tx, at.Add(mgl64.Vec3{3, 0, 0}), item.Stack{})
		TickAll(tx, 1, m)
		if !running(m, keyLook) || c.Look.Entity != nil && c.Look.Entity != p.H() {
			Fatalf(t, "look_at_player: running %v", m.Goals().RunningGoals())
		}
		if c.Look.At.Sub(entity.EyePosition(p)).Len() > 0.01 {
			Fatalf(t, "looks at %v, want the player's eyes %v", c.Look.At, entity.EyePosition(p))
		}
		// look_time is 2-4 s.
		TickAll(tx, 85, m)
		if running(m, keyLook) && c.Look.Calls < 40 {
			Fatal(t, "look did not last")
		}
		// Out of range: it stops.
		p.Teleport(at.Add(mgl64.Vec3{10, 0, 0}))
		TickAll(tx, 2, m)
		if running(m, keyLook) {
			Fatal(t, "still looking at a player 10 blocks away")
		}
	})
}

func TestRandomLookAround(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx, Stone)
		m, c := Spawn(t, tx, "test:idle", at, "")
		// Keep the stroll from blocking it (both use move|look).
		m.Goals().Remove(keyStroll)
		started := false
		for i := 0; i < 400 && !started; i++ {
			TickAll(tx, 1, m)
			started = running(m, keyAround)
		}
		if !started {
			Fatal(t, "random_look_around never started")
		}
		calls := c.Look.Calls
		stopped := false
		for i := 0; i < 5 && !stopped; i++ {
			TickAll(tx, 1, m)
			stopped = !running(m, keyAround)
		}
		if !stopped {
			Fatal(t, "random look lasted more than 5 ticks")
		}
		if c.Look.Calls == calls {
			Fatal(t, "no look target set")
		}
	})
}

func TestFloat(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx, Stone)
		m, c := Spawn(t, tx, "test:cow", at, "")
		TickAll(tx, 1, m)
		if running(m, keyFloat) {
			Fatal(t, "floating on land")
		}
		m.Physics().InWater = true
		TickAll(tx, 20, m)
		if !running(m, keyFloat) || c.Jump.Jumps < 10 {
			Fatalf(t, "float: running %v jumps %v", m.Goals().RunningGoals(), c.Jump.Jumps)
		}
	})
}

func TestPanic(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx, Stone)
		m, c := Spawn(t, tx, "test:cow", at, "")
		TickAll(tx, 5, m)
		if running(m, keyPanic) {
			Fatal(t, "panicking unhurt")
		}
		p := Player(tx, at.Add(mgl64.Vec3{-1, 0, 0}), item.Stack{})
		m.Hurt(1, entity.AttackDamageSource{Attacker: p})
		TickAll(tx, 1, m)
		if !running(m, keyPanic) || c.Nav.Speed != 1.25 {
			Fatalf(t, "panic: running %v speed %v", m.Goals().RunningGoals(), c.Nav.Speed)
		}
		if c.Nav.LastDest[0] < at[0]-0.5 {
			Fatalf(t, "ran towards the attacker: %v", c.Nav.LastDest)
		}
		// It ends when the navigation is done.
		TickAll(tx, 100, m)
		if running(m, keyPanic) {
			Fatal(t, "still panicking after reaching its spot")
		}
		// Long after the hit it does not start again.
		TickAll(tx, 50, m)
		if running(m, keyPanic) {
			Fatal(t, "panic restarted 150 ticks after the hit")
		}
	})
}

func TestTempt(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx, Stone)
		m, c := Spawn(t, tx, "test:cow", at, "")
		p := Player(tx, at.Add(mgl64.Vec3{6, 0, 0}), item.NewStack(item.Stick{}, 1))
		TickAll(tx, 2, m)
		if running(m, keyTempt) {
			Fatal(t, "tempted by a stick")
		}
		p.SetHeldItems(item.NewStack(item.Wheat{}, 1), item.Stack{})
		TickAll(tx, 1, m)
		if !running(m, keyTempt) {
			Fatalf(t, "not tempted by wheat: %v", m.Goals().RunningGoals())
		}
		if c.Nav.LastEntity != p.H() || c.Nav.Speed != 1.25 || c.Look.Entity != p.H() {
			Fatal(t, "does not walk to and look at the player")
		}
		if !m.Persistent() {
			Fatal(t, "a tempted mob must become persistent")
		}
		// It walks up to the player and stops close by.
		TickAll(tx, 40, m)
		if d := m.Position().Sub(p.Position()); d.Len() > 3 {
			Fatalf(t, "still %v blocks from the player", d.Len())
		}
		// Putting the wheat away ends it; then 100 ticks of cooldown.
		p.SetHeldItems(item.Stack{}, item.Stack{})
		TickAll(tx, 1, m)
		if running(m, keyTempt) {
			Fatal(t, "still tempted with an empty hand")
		}
		p.SetHeldItems(item.NewStack(item.Wheat{}, 1), item.Stack{})
		TickAll(tx, 50, m)
		if running(m, keyTempt) {
			Fatal(t, "tempted again during the cooldown")
		}
		TickAll(tx, 60, m)
		if !running(m, keyTempt) {
			Fatal(t, "not tempted after the cooldown")
		}
	})
}

func TestBreedingMakesBaby(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx, Stone)
		a, _ := Spawn(t, tx, "test:cow", at, "")
		b, _ := Spawn(t, tx, "test:cow", at.Add(mgl64.Vec3{4, 0, 0}), "")
		p := Player(tx, at.Add(mgl64.Vec3{0, 0, 3}), item.Stack{})
		for _, m := range []*mob.Mob{a, b} {
			if _, ok := m.Interact(p, item.NewStack(item.Wheat{}, 1), m.Position()); !ok {
				Fatal(t, "wheat not accepted")
			}
		}
		// The player has no wheat in hand: tempt stays off.
		TickAll(tx, 2, a, b)
		if !running(a, keyBreed) && !running(b, keyBreed) {
			Fatalf(t, "breed goal not running: %v / %v", a.Goals().RunningGoals(), b.Goals().RunningGoals())
		}
		TickAll(tx, 70, a, b)
		var babies []*mob.Mob
		for _, m := range Mobs(tx, "test:cow") {
			if m.Baby() {
				babies = append(babies, m)
			}
		}
		if len(babies) != 1 {
			Fatalf(t, "%v babies, want 1", len(babies))
		}
		if ccore.InLove(a) || ccore.InLove(b) {
			Fatal(t, "parents still in love")
		}
		// The baby follows its parents once they are 3+ blocks away.
		baby := babies[0]
		bc := &Controls{}
		bc.Install(baby)
		baby.SetPosition(a.Position().Add(mgl64.Vec3{0, 0, 6}))
		TickAll(tx, 1, baby)
		if !running(baby, keyFollow) {
			Fatalf(t, "baby does not follow: %v", baby.Goals().RunningGoals())
		}
		TickAll(tx, 40, baby)
		if running(baby, keyFollow) {
			Fatal(t, "still following within 3 blocks")
		}
	})
}

func TestSheepEatsGrass(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx, block.Grass{})
		m, c := Spawn(t, tx, "test:sheep", at, "")
		below := cube.Pos{0, 9, 0}
		ate := false
		for i := 0; i < 3000 && !ate; i++ {
			TickAll(tx, 1, m)
			_, ate = tx.Block(below).(block.Dirt)
		}
		if !ate {
			Fatal(t, "the sheep never ate the grass")
		}
		if m.Sheared() || InstOf(m).HasGroup("sheared") {
			Fatal(t, "on_eat did not fire (wool did not grow back)")
		}
		_ = c
		// Standing on dirt there is nothing to eat.
		before := len(InstOf(m).Fired)
		TickAll(tx, 400, m)
		if len(InstOf(m).Fired) != before {
			Fatal(t, "ate dirt")
		}
	})
}

func TestMountPathingStub(t *testing.T) {
	run := World(t)
	run(func(tx *world.Tx) {
		floor(tx, Stone)
		m, _ := Spawn(t, tx, "test:cow", at, "")
		if _, ok := m.Goals().Goal(keyMount); !ok {
			Fatal(t, "mount_pathing not registered")
		}
		TickAll(tx, 5, m)
		if running(m, keyMount) {
			Fatal(t, "mount_pathing ran without a rider")
		}
	})
}
