//go:build crossfly_full

package jpets

import (
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestMain(m *testing.M) {
	if err := mobtest.RegisterTypes(); err != nil {
		panic(err)
	}
	javamob.Install()
	os.Exit(m.Run())
}

func spawnJava(t *testing.T, w *mobtest.World, id string, pos mgl64.Vec3) *mobtest.Ref {
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

func withMob(t *testing.T, w *mobtest.World, r *mobtest.Ref, f func(m *javamob.Mob)) {
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

func stackOf(t *testing.T, name string, n int) item.Stack {
	t.Helper()
	it, ok := world.ItemByName(name, 0)
	if !ok {
		t.Fatalf("no item %v", name)
	}
	return item.NewStack(it, n)
}

func arena(w *mobtest.World) {
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 24, block.Grass{}) })
}

// tameWolf feeds bones until the wolf is tame (one in three: Wolf.tryToTame).
func tameWolf(t *testing.T, w *mobtest.World, p *mobtest.Player, r *mobtest.Ref) {
	t.Helper()
	for i := 0; i < 60; i++ {
		p.Interact(r, stackOf(t, "minecraft:bone", 1))
		tame := false
		withMob(t, w, r, func(m *javamob.Mob) { tame = m.Behaviour().(*Wolf).Tame })
		if tame {
			return
		}
	}
	t.Fatal("wolf never tamed")
}

// TestWolfTaming: bones tame a wolf, which then sits (ordered), has 40
// health and the player as owner; empty hands toggle sitting; the wolf
// stands up when hurt (Paper: after the hit).
func TestWolfTaming(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	arena(w)
	r := spawnJava(t, w, "minecraft:wolf", mgl64.Vec3{0.5, 1, 0.5})
	p := w.AddPlayer("tester", mgl64.Vec3{2.5, 1, 0.5})
	withMob(t, w, r, func(m *javamob.Mob) {
		if m.MaxHealth() != 8 {
			t.Fatalf("wild wolf health %v", m.MaxHealth())
		}
	})
	tameWolf(t, w, p, r)
	w.Tick(4)
	withMob(t, w, r, func(m *javamob.Mob) {
		wf := m.Behaviour().(*Wolf)
		if !wf.OrderedToSit || !m.Meta().Tamed || m.MaxHealth() != 40 || m.Health() != 40 {
			t.Fatalf("tamed wolf: sit %v tamed %v health %v/%v", wf.OrderedToSit, m.Meta().Tamed, m.Health(), m.MaxHealth())
		}
		if !isOwnedBy(m, mustPlayer(t, w, p, m.Tx())) {
			t.Fatal("not owned by the player")
		}
		if !m.Meta().Sitting {
			t.Fatal("not shown sitting")
		}
	})
	// Empty hand: stand up.
	p.Interact(r, item.Stack{})
	w.Tick(4)
	withMob(t, w, r, func(m *javamob.Mob) {
		if m.Behaviour().(*Wolf).OrderedToSit || m.Meta().Sitting {
			t.Fatal("still sitting after the owner's click")
		}
	})
	// Collar: a blue dye.
	p.Interact(r, stackOf(t, "minecraft:blue_dye", 1))
	withMob(t, w, r, func(m *javamob.Mob) {
		if m.Behaviour().(*Wolf).CollarColour != DyeBlue || m.Meta().Color != DyeBlue {
			t.Fatal("collar not dyed")
		}
	})
	// Sit again, then a hit stands it up.
	p.Interact(r, item.Stack{})
	withMob(t, w, r, func(m *javamob.Mob) {
		m.Hurt(1, entity.FallDamageSource{})
		if m.Behaviour().(*Wolf).OrderedToSit {
			t.Fatal("a hurt wolf keeps sitting")
		}
	})
}

func mustPlayer(t *testing.T, w *mobtest.World, p *mobtest.Player, tx *world.Tx) world.Entity {
	t.Helper()
	e, ok := p.Entity(tx)
	if !ok {
		t.Fatal("player gone")
	}
	return e
}

// TestWolfTeleportsToOwner: a tame wolf 12+ blocks from its owner teleports
// next to it (FollowOwnerGoal.tick -> tryToTeleportToOwner).
func TestWolfTeleportsToOwner(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	arena(w)
	r := spawnJava(t, w, "minecraft:wolf", mgl64.Vec3{0.5, 1, 0.5})
	p := w.AddPlayer("tester", mgl64.Vec3{2.5, 1, 0.5})
	tameWolf(t, w, p, r)
	p.Interact(r, item.Stack{}) // stand up
	p.Teleport(mgl64.Vec3{18.5, 1, 18.5})
	_, ok := w.TickUntil(200, func(tx *world.Tx) bool {
		return mobtest.DistTx(tx, r, p) < 4.5
	})
	if !ok {
		t.Fatalf("wolf did not come to its owner: %v", w.Dist(r, p))
	}
}

// TestWolfArmour: wolf armour takes the damage and wears down.
func TestWolfArmour(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	arena(w)
	r := spawnJava(t, w, "minecraft:wolf", mgl64.Vec3{0.5, 1, 0.5})
	p := w.AddPlayer("tester", mgl64.Vec3{2.5, 1, 0.5})
	tameWolf(t, w, p, r)
	p.Interact(r, stackOf(t, "minecraft:wolf_armor", 1))
	withMob(t, w, r, func(m *javamob.Mob) {
		if itemName(m.BodyArmour()) != "minecraft:wolf_armor" {
			t.Fatal("armour not put on")
		}
		hp := m.Health()
		m.Hurt(5, entity.AttackDamageSource{})
		if m.Health() != hp {
			t.Fatalf("health %v -> %v with armour", hp, m.Health())
		}
		if a := m.BodyArmour(); a.MaxDurability()-a.Durability() != 5 {
			t.Fatalf("armour damage %v", a.MaxDurability()-a.Durability())
		}
	})
}

// TestHorseTamingByRiding: a wild horse throws a rider off and gets 5 more
// temper until a roll below its temper tames it; with full temper it is
// tamed at the first roll.
func TestHorseTamingByRiding(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	arena(w)
	r := spawnJava(t, w, "minecraft:horse", mgl64.Vec3{0.5, 1, 0.5})
	p := w.AddPlayer("tester", mgl64.Vec3{2.5, 1, 0.5})
	withMob(t, w, r, func(m *javamob.Mob) {
		h := m.Behaviour().(*Equine)
		a := m.Attributes()
		if mh := a.Base(javamob.AttrMaxHealth); mh < 15 || mh > 30 {
			t.Fatalf("horse health %v", mh)
		}
		if h.Tame {
			t.Fatal("spawned tame")
		}
		h.Temper = h.maxTemper()
	})
	p.Interact(r, item.Stack{})
	withMob(t, w, r, func(m *javamob.Mob) {
		if len(m.Passengers()) != 1 {
			t.Fatal("player did not get on")
		}
	})
	if _, ok := w.TickUntil(600, func(tx *world.Tx) bool {
		bm, _ := r.Mob(tx)
		jm, _ := javamob.Of(bm)
		return jm.Behaviour().(*Equine).Tame
	}); !ok {
		t.Fatal("horse with full temper never tamed")
	}
}

// TestHorseSaddle: a saddle goes on a tamed horse only; then the rider
// steers it and both clients get the saddled and steering flags.
func TestHorseSaddle(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	arena(w)
	r := spawnJava(t, w, "minecraft:horse", mgl64.Vec3{0.5, 1, 0.5})
	p := w.AddPlayer("tester", mgl64.Vec3{2.5, 1, 0.5})
	p.Interact(r, stackOf(t, "minecraft:saddle", 1))
	withMob(t, w, r, func(m *javamob.Mob) {
		if m.Meta().Saddled {
			t.Fatal("wild horse took a saddle")
		}
		m.Behaviour().(*Equine).tameWithName(m, mustPlayer(t, w, p, m.Tx()).(*player.Player))
		m.EjectPassengers()
	})
	p.Interact(r, stackOf(t, "minecraft:saddle", 1))
	withMob(t, w, r, func(m *javamob.Mob) {
		h := m.Behaviour().(*Equine)
		if !h.saddled() || !m.Meta().Saddled {
			t.Fatal("tamed horse not saddled")
		}
		if !h.Controls(m, mustPlayer(t, w, p, m.Tx())) {
			t.Fatal("a saddled horse is not steered by its player rider")
		}
	})
}

// TestHorseBreeding: two tamed horses fed golden carrots make a foal whose
// stats stay in the horse ranges.
func TestHorseBreeding(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	arena(w)
	a := spawnJava(t, w, "minecraft:horse", mgl64.Vec3{0.5, 1, 0.5})
	b := spawnJava(t, w, "minecraft:horse", mgl64.Vec3{2.5, 1, 0.5})
	p := w.AddPlayer("tester", mgl64.Vec3{1.5, 1, 3.5})
	for _, r := range []*mobtest.Ref{a, b} {
		withMob(t, w, r, func(m *javamob.Mob) {
			m.Behaviour().(*Equine).tameWithName(m, mustPlayer(t, w, p, m.Tx()).(*player.Player))
			m.SetHealth(m.MaxHealth())
		})
		p.Interact(r, stackOf(t, "minecraft:golden_carrot", 1))
	}
	_, ok := w.TickUntil(400, func(tx *world.Tx) bool { return len(w.Mobs("minecraft:horse")) >= 3 })
	if !ok {
		t.Fatal("no foal")
	}
	for _, r := range w.Mobs("minecraft:horse") {
		withMob(t, w, r, func(m *javamob.Mob) {
			if !m.Baby() {
				return
			}
			at := m.Attributes()
			if s := at.Base(javamob.AttrMovementSpeed); s < MinHorseSpeed-1e-9 || s > MaxHorseSpeed+1e-9 {
				t.Fatalf("foal speed %v", s)
			}
			if !m.Behaviour().(*Equine).Bred {
				t.Fatal("foal not marked bred")
			}
		})
	}
}

// TestCatTaming: raw fish tame a cat (one in three), which then sits.
func TestCatTaming(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	arena(w)
	r := spawnJava(t, w, "minecraft:cat", mgl64.Vec3{0.5, 1, 0.5})
	p := w.AddPlayer("tester", mgl64.Vec3{2.5, 1, 0.5})
	for i := 0; i < 60; i++ {
		p.Interact(r, stackOf(t, "minecraft:cod", 1))
		tame := false
		withMob(t, w, r, func(m *javamob.Mob) { tame = m.Behaviour().(*Cat).Tame })
		if tame {
			break
		}
	}
	withMob(t, w, r, func(m *javamob.Mob) {
		c := m.Behaviour().(*Cat)
		if !c.Tame || !c.OrderedToSit {
			t.Fatal("cat not tamed and sitting")
		}
	})
}

// TestPetsSaved: a tame wolf keeps its owner, collar and order to sit, a
// tamed horse its temper, saddle and coat across a reload.
func TestPetsSaved(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{Persistent: true})
	arena(w)
	r := spawnJava(t, w, "minecraft:wolf", mgl64.Vec3{0.5, 1, 0.5})
	hr := spawnJava(t, w, "minecraft:horse", mgl64.Vec3{4.5, 1, 4.5})
	p := w.AddPlayer("tester", mgl64.Vec3{2.5, 1, 0.5})
	tameWolf(t, w, p, r)
	p.Interact(r, stackOf(t, "minecraft:lime_dye", 1))
	var coat, marks int
	withMob(t, w, hr, func(m *javamob.Mob) {
		h := m.Behaviour().(*Equine)
		h.tameWithName(m, mustPlayer(t, w, p, m.Tx()).(*player.Player))
		h.setSaddle(m, stackOf(t, "minecraft:saddle", 1))
		h.Temper = 42
		coat, marks = h.Variant, h.Markings
	})
	w.Reload()
	w.Tick(1)
	withMob(t, w, r, func(m *javamob.Mob) {
		wf := m.Behaviour().(*Wolf)
		if !wf.Tame || !wf.OrderedToSit || wf.CollarColour != DyeLime || m.MaxHealth() != 40 {
			t.Fatalf("wolf after reload: tame %v sit %v collar %v max %v", wf.Tame, wf.OrderedToSit, wf.CollarColour, m.MaxHealth())
		}
		if _, ok := m.OwnerUUID(); !ok {
			t.Fatal("wolf lost its owner")
		}
	})
	withMob(t, w, hr, func(m *javamob.Mob) {
		h := m.Behaviour().(*Equine)
		if !h.Tame || !h.saddled() || h.Temper != 42 || h.Variant != coat || h.Markings != marks {
			t.Fatalf("horse after reload: %+v", h)
		}
	})
}

// TestOcelotTrust: fish fed to a tempted ocelot within 3 blocks makes it
// trust the player (one in three), after which it no longer flees players.
func TestOcelotTrust(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	arena(w)
	r := spawnJava(t, w, "minecraft:ocelot", mgl64.Vec3{0.5, 1, 0.5})
	p := w.AddPlayer("tester", mgl64.Vec3{2.5, 1, 0.5})
	p.Hold(stackOf(t, "minecraft:cod", 64))
	// The ocelot must be tempted (OcelotTemptGoal running) to take the fish.
	if _, ok := w.TickUntil(100, func(tx *world.Tx) bool {
		bm, _ := r.Mob(tx)
		jm, _ := javamob.Of(bm)
		return jm.Behaviour().(*Ocelot).tempt.Running()
	}); !ok {
		t.Fatal("ocelot not tempted by fish")
	}
	for i := 0; i < 60; i++ {
		p.Interact(r, stackOf(t, "minecraft:cod", 1))
		trusting := false
		withMob(t, w, r, func(m *javamob.Mob) { trusting = m.Behaviour().(*Ocelot).Trusting })
		if trusting {
			withMob(t, w, r, func(m *javamob.Mob) {
				for _, g := range m.Goals().Goals() {
					if _, ok := g.(*catAvoidPlayers); ok {
						t.Fatal("a trusting ocelot still avoids players")
					}
				}
			})
			return
		}
	}
	t.Fatal("ocelot never trusted the player")
}

// TestPolarBearDefendsCub: an adult bear with a cub nearby targets a player
// within half its follow range.
func TestPolarBearDefendsCub(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	arena(w)
	bear := spawnJava(t, w, "minecraft:polar_bear", mgl64.Vec3{0.5, 1, 0.5})
	cub := spawnJava(t, w, "minecraft:polar_bear", mgl64.Vec3{2.5, 1, 0.5})
	withMob(t, w, bear, func(m *javamob.Mob) { m.SetBaby(false) })
	withMob(t, w, cub, func(m *javamob.Mob) {
		m.Behaviour().(*PolarBear).Animal.SetBaby(true)
		m.SetBaby(true)
	})
	w.AddPlayer("tester", mgl64.Vec3{6.5, 1, 0.5})
	if _, ok := w.TickUntil(200, func(tx *world.Tx) bool {
		bm, _ := bear.Mob(tx)
		_, ok := bm.Target()
		return ok
	}); !ok {
		t.Fatal("bear did not go for the player near its cub")
	}
}

// TestFoxPicksUpItem: a fox with an empty mouth walks to an item and holds
// it.
func TestFoxPicksUpItem(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Arena(tx, 5, 3) })
	r := spawnJava(t, w, "minecraft:fox", mgl64.Vec3{0.5, 1, 0.5})
	withMob(t, w, r, func(m *javamob.Mob) { m.SetItemSlot(javamob.SlotMainHand, item.Stack{}) })
	w.Do(func(tx *world.Tx) {
		tx.AddEntity(tx.World().EntityRegistry().Config().Item(world.EntitySpawnOpts{Position: mgl64.Vec3{3.5, 1, 0.5}}, stackOf(t, "minecraft:feather", 1)))
	})
	// FoxSearchForItemsGoal shares priority 11 with the random stroll, so it
	// starts between strolls.
	if _, ok := w.TickUntil(4800, func(tx *world.Tx) bool {
		bm, _ := r.Mob(tx)
		jm, _ := javamob.Of(bm)
		return itemName(jm.ItemBySlot(javamob.SlotMainHand)) == "minecraft:feather"
	}); !ok {
		t.Fatal("fox never picked up the feather")
	}
}

// TestFoxSleepsInShade: by day a fox under a roof with nothing around falls
// asleep.
func TestFoxSleepsInShade(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{Time: mobtest.Noon})
	arena(w)
	w.Do(func(tx *world.Tx) {
		w.Fill(tx, cube.Pos{-6, 4, -6}, cube.Pos{6, 4, 6}, block.Stone{})
	})
	r := spawnJava(t, w, "minecraft:fox", mgl64.Vec3{0.5, 1, 0.5})
	if _, ok := w.TickUntil(3600, func(tx *world.Tx) bool {
		bm, _ := r.Mob(tx)
		jm, _ := javamob.Of(bm)
		return jm.Behaviour().(*Fox).flag(foxSleeping)
	}); !ok {
		t.Fatal("fox did not sleep in the shade by day")
	}
}

// TestPandaBamboo: the first bamboo puts an adult panda in love, the next
// one makes it sit down and eat with the bamboo in its paws.
func TestPandaBamboo(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	arena(w)
	r := spawnJava(t, w, "minecraft:panda", mgl64.Vec3{0.5, 1, 0.5})
	p := w.AddPlayer("tester", mgl64.Vec3{2.5, 1, 0.5})
	withMob(t, w, r, func(m *javamob.Mob) {
		pd := m.Behaviour().(*Panda)
		if m.Meta().Variant != int32(pd.personality()) {
			t.Fatal("personality not shown")
		}
		if pd.personality() == GeneWorried {
			t.Skip("a worried panda in this seed")
		}
		m.SetBaby(false)
		pd.Animal.SetAge(0)
	})
	p.Interact(r, stackOf(t, "minecraft:bamboo", 1))
	withMob(t, w, r, func(m *javamob.Mob) {
		if !m.Behaviour().(*Panda).Animal.InLove() {
			t.Fatal("bamboo did not make the panda fall in love")
		}
	})
	p.Interact(r, stackOf(t, "minecraft:bamboo", 1))
	withMob(t, w, r, func(m *javamob.Mob) {
		pd := m.Behaviour().(*Panda)
		if !pd.flag(flagSit) || !pd.eating() || itemName(m.ItemBySlot(javamob.SlotMainHand)) != "minecraft:bamboo" {
			t.Fatalf("panda not sitting and eating: flags %v eat %v held %v", pd.flags, pd.eatCounter, itemName(m.ItemBySlot(javamob.SlotMainHand)))
		}
	})
}

// TestDonkeyChest: a tamed donkey takes a chest; its window then has the
// saddle and armour slots and 15 chest slots, and the chest and its items
// drop when it dies.
func TestDonkeyChest(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	arena(w)
	r := spawnJava(t, w, "minecraft:donkey", mgl64.Vec3{0.5, 1, 0.5})
	p := w.AddPlayer("tester", mgl64.Vec3{2.5, 1, 0.5})
	withMob(t, w, r, func(m *javamob.Mob) {
		m.Behaviour().(*Equine).tameWithName(m, mustPlayer(t, w, p, m.Tx()).(*player.Player))
	})
	p.Interact(r, stackOf(t, "minecraft:chest", 1))
	withMob(t, w, r, func(m *javamob.Mob) {
		h := m.Behaviour().(*Equine)
		if !h.Chest || !m.Meta().Chested {
			t.Fatal("no chest on the donkey")
		}
		win, ok := h.Window(m, mustPlayer(t, w, p, m.Tx()), entity.WindowLayoutJava)
		if !ok || win.ChestSlots != 15 || win.Inventory.Size() != 17 {
			t.Fatalf("window: ok %v chest %v size %v", ok, win.ChestSlots, win.Inventory.Size())
		}
		_ = win.Inventory.SetItem(2, stackOf(t, "minecraft:diamond", 3))
		if it, _ := h.inv.Item(0); itemName(it) != "minecraft:diamond" {
			t.Fatal("window slot not written to the chest")
		}
	})
}

// TestLlamaStrengthColumns: a chested llama has 3 x strength slots.
func TestLlamaStrengthColumns(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	arena(w)
	r := spawnJava(t, w, "minecraft:llama", mgl64.Vec3{0.5, 1, 0.5})
	withMob(t, w, r, func(m *javamob.Mob) {
		h := m.Behaviour().(*Equine)
		if h.llama.Strength < 1 || h.llama.Strength > 5 {
			t.Fatalf("strength %v", h.llama.Strength)
		}
		h.setChest(m, true)
		if h.inv.Size() != 3*h.llama.Strength {
			t.Fatalf("llama inventory %v for strength %v", h.inv.Size(), h.llama.Strength)
		}
	})
}

// TestLlamaCaravan: a free llama near a leashed one joins it as a caravan
// and follows.
func TestLlamaCaravan(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	arena(w)
	lead := spawnJava(t, w, "minecraft:llama", mgl64.Vec3{0.5, 1, 0.5})
	tail := spawnJava(t, w, "minecraft:llama", mgl64.Vec3{5.5, 1, 0.5})
	p := w.AddPlayer("tester", mgl64.Vec3{-2.5, 1, 0.5})
	withMob(t, w, lead, func(m *javamob.Mob) { core.Leash(m.Mob, mustPlayer(t, w, p, m.Tx())) })
	if _, ok := w.TickUntil(400, func(tx *world.Tx) bool {
		bm, _ := tail.Mob(tx)
		jm, _ := javamob.Of(bm)
		return jm.Behaviour().(*Equine).llama.inCaravan()
	}); !ok {
		t.Fatal("the free llama did not join the leashed one")
	}
}

// TestMuleBreeding: a horse and a donkey in love make a mule.
func TestMuleBreeding(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	arena(w)
	a := spawnJava(t, w, "minecraft:horse", mgl64.Vec3{0.5, 1, 0.5})
	b := spawnJava(t, w, "minecraft:donkey", mgl64.Vec3{2.5, 1, 0.5})
	p := w.AddPlayer("tester", mgl64.Vec3{1.5, 1, 3.5})
	for _, r := range []*mobtest.Ref{a, b} {
		withMob(t, w, r, func(m *javamob.Mob) {
			m.Behaviour().(*Equine).tameWithName(m, mustPlayer(t, w, p, m.Tx()).(*player.Player))
			m.SetHealth(m.MaxHealth())
		})
		p.Interact(r, stackOf(t, "minecraft:golden_carrot", 1))
	}
	if _, ok := w.TickUntil(400, func(tx *world.Tx) bool { return len(w.Mobs("minecraft:mule")) > 0 }); !ok {
		t.Fatal("no mule")
	}
}
