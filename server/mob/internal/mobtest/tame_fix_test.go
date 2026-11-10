//go:build crossfly_full

package mobtest_test

import (
	"fmt"
	"math"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/component/tame"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// petJSON is an add-on style pet as add-ons write them (the Gubby): the
// camelCase keys BDS reads besides the snake_case ones, a tamed group without
// minecraft:is_tamed, and inherit_tamed only on minecraft:breedable.
const petJSON = `{
  "format_version": "1.21.0",
  "minecraft:entity": {
    "description": {"identifier": "%s", "is_spawnable": false, "is_summonable": true},
    "component_groups": {
      "wild": {
        "minecraft:tameable": {"probability": 1, "tameItems": ["bone"], "tame_event": {"event": "tamed", "target": "self"}}
      },
      "tame": {
        "minecraft:sittable": {},
        "minecraft:behavior.stay_while_sitting": {"priority": 1},
        "minecraft:behavior.follow_owner": {"priority": 2, "speed_multiplier": 1, "start_distance": 10, "stop_distance": 2}
      },
      "adult": {
        "minecraft:breedable": {"requireTame": true, "inheritTamed": %s, "breedItems": ["wheat"],
          "breedsWith": {"mateType": "%[1]s", "babyType": "%[1]s", "breed_event": {"event": "minecraft:entity_born", "target": "baby"}}}
      },
      "baby": {"minecraft:is_baby": {}}
    },
    "components": {
      "minecraft:physics": {},
      "minecraft:collision_box": {"width": 0.6, "height": 0.8},
      "minecraft:health": {"value": 20, "max": 20},
      "minecraft:movement": {"value": 0.3},
      "minecraft:movement.basic": {},
      "minecraft:jump.static": {},
      "minecraft:navigation.walk": {"can_walk": true},
      "minecraft:nameable": {"alwaysShow": true, "allowNameTagRenaming": false},
      "minecraft:leashable": {},
      "minecraft:behavior.float": {"priority": 0}
    },
    "events": {
      "minecraft:entity_spawned": {"add": {"component_groups": ["wild", "adult"]}},
      "minecraft:entity_born": {"add": {"component_groups": ["wild", "baby"]}},
      "tamed": {"remove": {"component_groups": ["wild"]}, "add": {"component_groups": ["tame"]}}
    }
  }
}`

const (
	petID          = "test:pet"
	petNoInheritID = "test:pet_no_inherit"
)

var petOnce sync.Once

func registerPets(t *testing.T) {
	t.Helper()
	mobtest.RequireDefinitions(t)
	var err error
	petOnce.Do(func() {
		for _, p := range []struct{ id, inherit string }{{petID, "true"}, {petNoInheritID, "false"}} {
			d, e := mob.ParseDefinition([]byte(fmt.Sprintf(petJSON, p.id, p.inherit)))
			if e != nil {
				err = e
				return
			}
			mob.RegisterType(d)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
}

func playerOf(t *testing.T, tx *world.Tx, p *mobtest.Player) *player.Player {
	t.Helper()
	for pl := range tx.Players() {
		if pl.H() == p.H() {
			return pl.(*player.Player)
		}
	}
	t.Fatal("player not in the world")
	return nil
}

// tamePet tames r by feeding it a bone (probability 1) and makes it stand.
func tamePet(t *testing.T, w *mobtest.World, r *mobtest.Ref, p *mobtest.Player) {
	t.Helper()
	if _, ok := p.Interact(r, item.NewStack(item.Bone{}, 1)); !ok {
		t.Fatal("the bone was not taken")
	}
	r.With(func(m *mob.Mob) {
		if s, ok := tame.SittableOf(m); ok {
			s.SetSitting(m, false)
		}
	})
}

// TestTamingSetsTamedFlag: minecraft:tameable sets the TAMED flag itself
// (TameableComponent::_becomeTame, setStatusFlag(28)), without
// minecraft:is_tamed, and viewers (Bedrock and Java) are shown it. The tame
// item is read from tameItems.
func TestTamingSetsTamedFlag(t *testing.T) {
	registerPets(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 6, block.Stone{}) })
	r := w.Spawn(petID, mgl64.Vec3{0.5, 1, 2.5})
	p := w.AddPlayer("Steve", mgl64.Vec3{0.5, 1, 0.5})
	v := &stateViewer{}
	var l *world.Loader
	w.Do(func(tx *world.Tx) {
		l = world.NewLoader(2, tx.World(), v)
		l.Move(tx, mgl64.Vec3{0.5, 1, 0.5})
		l.Load(tx, 25)
	})
	defer w.Do(func(tx *world.Tx) { l.Close(tx) })
	w.Tick(2)
	r.With(func(m *mob.Mob) {
		if m.Tamed() || m.HasComponent("minecraft:is_tamed") {
			t.Fatal("the pet starts tamed")
		}
	})
	if _, ok := p.Interact(r, item.NewStack(item.Bone{}, 1)); !ok {
		t.Fatal("tameItems bone did not tame")
	}
	w.Tick(1)
	w.Do(func(tx *world.Tx) {
		m, _ := r.Mob(tx)
		if !m.Tamed() || !tame.Tamed(m) {
			t.Fatal("tamed pet has no TAMED flag")
		}
		if !tame.IsOwner(m, playerOf(t, tx, p)) || !m.Persistent() {
			t.Error("tamer is not the owner, or the pet is not persistent")
		}
	})
	if got := v.last(); got != "tamed+sitting" {
		t.Fatalf("viewers last saw %q (all %v)", got, v.seen)
	}
	// A tamed pet cannot be tamed again, and only its owner makes it sit or
	// stand (SitComponent::getInteraction).
	other := w.AddPlayer("Alex", mgl64.Vec3{-1.5, 1, 0.5})
	if _, ok := other.Interact(r, item.NewStack(item.Bone{}, 1)); ok {
		t.Error("another player used a bone on the tamed pet")
	}
	if left, ok := p.Interact(r, item.NewStack(item.Bone{}, 1)); !ok || left.Count() != 1 {
		t.Error("the owner could not make the pet stand, or the bone was used")
	}
	w.Do(func(tx *world.Tx) {
		m, _ := r.Mob(tx)
		if m.Sitting() || !tame.IsOwner(m, playerOf(t, tx, p)) {
			t.Error("the pet still sits or changed owner")
		}
	})
}

// TestNameableCamelCaseKeys: alwaysShow and allowNameTagRenaming
// (NameableDefinition "always_show|alwaysShow",
// "allowNameTagRenaming|allow_name_tag_renaming").
func TestNameableCamelCaseKeys(t *testing.T) {
	registerPets(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 6, block.Stone{}) })
	r := w.Spawn(petID, mgl64.Vec3{0.5, 1, 2.5})
	p := w.AddPlayer("Alex", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(2)
	r.With(func(m *mob.Mob) {
		if !m.AlwaysShowNameTag() {
			t.Error("alwaysShow true did not show the name")
		}
	})
	if _, ok := p.Interact(r, item.NewStack(item.NameTag{}, 1).WithCustomName("Bob")); ok {
		t.Error("allowNameTagRenaming false: a name tag was used")
	}
	r.With(func(m *mob.Mob) {
		if m.NameTag() == "Bob" {
			t.Error("allowNameTagRenaming false: renamed")
		}
	})
}

// breedPets tames two pets to p, mates them and returns the baby's state.
func breedPets(t *testing.T, w *mobtest.World, id string, p *mobtest.Player) *mobtest.Ref {
	t.Helper()
	a := w.Spawn(id, mgl64.Vec3{-1.5, 1, 2.5})
	b := w.Spawn(id, mgl64.Vec3{1.5, 1, 2.5})
	w.Tick(2)
	tamePet(t, w, a, p)
	tamePet(t, w, b, p)
	var baby *mobtest.Ref
	w.Do(func(tx *world.Tx) {
		ma, _ := a.Mob(tx)
		mb, _ := b.Mob(tx)
		ba, ok := core.BreedableOf(ma)
		if !ok {
			t.Fatal("no breedable (breedItems/breedsWith)")
		}
		bb, _ := core.BreedableOf(mb)
		// requireTame: only tamed pets may breed; the flag comes from taming.
		ba.SetInLove(ma, nil)
		bb.SetInLove(mb, nil)
		if !ba.CanMate(ma, mb) {
			t.Fatal("two tamed pets in love cannot mate")
		}
		babies := ba.Mate(ma, mb)
		if len(babies) != 1 {
			t.Fatalf("%d babies", len(babies))
		}
		baby = w.Track(babies[0].H())
	})
	return baby
}

// TestBredBabyInheritsTamed: inherit_tamed (BDS reads it from
// minecraft:breedable, "inheritTamed|inherit_tamed", default true): the baby
// of a tamed parent gets the TAMED flag and the owner; every bred baby is
// persistent (BreedableComponent::_handleMate).
func TestBredBabyInheritsTamed(t *testing.T) {
	registerPets(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	p := w.AddPlayer("Steve", mgl64.Vec3{0.5, 1, -3.5})
	baby := breedPets(t, w, petID, p)
	w.Do(func(tx *world.Tx) {
		m, _ := baby.Mob(tx)
		if !m.Baby() {
			t.Error("the baby is no baby")
		}
		if !m.Tamed() {
			t.Error("inherit_tamed: the baby is not tamed")
		}
		if id, ok := m.OwnerUUID(); !ok || id != playerOf(t, tx, p).UUID() {
			t.Error("inherit_tamed: the baby has not the parent's owner")
		}
		if o, ok := m.OwnerEntity(); !ok || o.H() != p.H() {
			t.Error("the baby does not find its owner")
		}
		if !m.Persistent() {
			t.Error("bred baby not persistent")
		}
	})

	w2 := mobtest.New(t, mobtest.Options{})
	w2.Do(func(tx *world.Tx) { w2.Floor(tx, 0, 8, block.Stone{}) })
	p2 := w2.AddPlayer("Alex", mgl64.Vec3{0.5, 1, -3.5})
	baby2 := breedPets(t, w2, petNoInheritID, p2)
	baby2.With(func(m *mob.Mob) {
		if m.Tamed() {
			t.Error("inheritTamed false: the baby is tamed")
		}
		if _, ok := m.OwnerUUID(); ok {
			t.Error("inheritTamed false: the baby has an owner")
		}
		if !m.Persistent() {
			t.Error("bred baby not persistent")
		}
	})
}

// TestTamedFlagSaved: the TAMED flag is saved with the mob (IsTamed) when no
// minecraft:is_tamed sets it, for a tamed pet and its bred baby, and the
// baby stays persistent.
func TestTamedFlagSaved(t *testing.T) {
	registerPets(t)
	w := mobtest.New(t, mobtest.Options{Persistent: true})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 8, block.Stone{}) })
	p := w.AddPlayer("Steve", mgl64.Vec3{0.5, 1, -3.5})
	baby := breedPets(t, w, petID, p)
	parents := w.Mobs(petID)
	w.Reload()
	w.Tick(2)
	for _, r := range parents {
		r.With(func(m *mob.Mob) {
			if !m.Tamed() {
				t.Errorf("tamed %v (baby %v) lost its TAMED flag", m.Identifier(), m.Baby())
			}
			if !m.Persistent() {
				t.Errorf("baby %v not persistent after reload", m.Baby())
			}
		})
	}
	baby.With(func(m *mob.Mob) {
		if !m.Tamed() || !m.Persistent() {
			t.Error("bred baby lost its TAMED flag or persistence")
		}
	})
}

// followWorld is a floor at the origin and an island at x 40..48 that no
// path connects. The owner stands on the island; a second player keeps the
// origin loaded.
func followWorld(t *testing.T) (*mobtest.World, *mobtest.Player, *mobtest.Player) {
	t.Helper()
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	w.Do(func(tx *world.Tx) {
		w.Floor(tx, 0, 6, block.Stone{})
		w.Fill(tx, cube.Pos{38, 0, -5}, cube.Pos{48, 0, 5}, block.Stone{})
	})
	owner := w.AddPlayer("Steve", mgl64.Vec3{43.5, 1, 0.5})
	anchor := w.AddPlayer("Alex", mgl64.Vec3{2.5, 60, 0.5})
	return w, owner, anchor
}

func tameWolf(t *testing.T, w *mobtest.World, owner *mobtest.Player, pos mgl64.Vec3) *mobtest.Ref {
	t.Helper()
	wolf := w.Spawn("minecraft:wolf", pos, mob.SpawnOptions{Event: "minecraft:spawn_tame_adult"})
	w.Do(func(tx *world.Tx) {
		m, _ := wolf.Mob(tx)
		m.SetOwner(playerOf(t, tx, owner))
	})
	return wolf
}

// TestPetTeleportsToOwner: follow_owner teleports a pet that cannot path to
// its owner (falling here) and is 12+ blocks away onto the ring of blocks 2
// from the owner's block, at the owner's feet (BDS 1.21.3
// FollowOwnerGoal::tick / _tryToGetTeleportPosition), only where the head
// block is free.
func TestPetTeleportsToOwner(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w, owner, _ := followWorld(t)
	// Roof the ring at head height except (45, 2, 2): the only spot left.
	w.Do(func(tx *world.Tx) {
		for x := 41; x <= 45; x++ {
			for z := -2; z <= 2; z++ {
				if (x == 45 && z == 2) || (x >= 42 && x <= 44 && z >= -1 && z <= 1) {
					continue
				}
				tx.SetBlock(cube.Pos{x, 2, z}, block.Stone{}, nil)
			}
		}
	})
	wolf := tameWolf(t, w, owner, mgl64.Vec3{0.5, 50, 0.5})
	if _, ok := w.TickUntil(40, func(tx *world.Tx) bool { return mobtest.DistTx(tx, wolf, owner) < 4 }); !ok {
		t.Fatalf("the wolf did not teleport to its owner: at %v", wolf.Pos())
	}
	pos := wolf.Pos()
	if math.Abs(pos[0]-45.5) > 0.06 || math.Abs(pos[2]-2.5) > 0.06 || math.Abs(pos[1]-1) > 0.01 {
		t.Errorf("teleported to %v, want the free spot (45.5, 1, 2.5)", pos)
	}
}

// TestLeashedPetDoesNotTeleport: a pet on a lead never teleports to its owner
// (FollowOwnerGoal::tick checks !isLeashed()), from follow_owner or
// teleport_to_owner.
func TestLeashedPetDoesNotTeleport(t *testing.T) {
	mobtest.RequireDefinitions(t)
	w, owner, anchor := followWorld(t)
	wolf := tameWolf(t, w, owner, mgl64.Vec3{0.5, 60, 0.5})
	w.Do(func(tx *world.Tx) {
		m, _ := wolf.Mob(tx)
		core.Leash(m, playerOf(t, tx, anchor))
	})
	for i := 0; i < 15; i++ {
		w.Tick(1)
		if d := w.Dist(wolf, owner); d < 20 {
			t.Fatalf("leashed wolf teleported to its owner after %d ticks (%.1f blocks)", i+1, d)
		}
	}
	wolf.With(func(m *mob.Mob) {
		if !core.Leashed(m) {
			t.Fatal("the lead broke: the test proves nothing")
		}
	})
	// Off the lead, the same falling wolf teleports.
	wolf.With(func(m *mob.Mob) { core.Unleash(m) })
	if _, ok := w.TickUntil(30, func(tx *world.Tx) bool { return mobtest.DistTx(tx, wolf, owner) < 4 }); !ok {
		t.Fatalf("unleashed wolf did not teleport: at %v", wolf.Pos())
	}
}
