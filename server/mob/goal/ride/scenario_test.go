//go:build crossfly_full

package ride_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	cride "github.com/df-mc/dragonfly/server/mob/component/ride"
	"github.com/df-mc/dragonfly/server/mob/component/tame"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	_ "github.com/df-mc/dragonfly/server/mob/vanilla"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

func newWorld(t *testing.T, opts mobtest.Options) *mobtest.World {
	t.Helper()
	if opts.Time == 0 {
		opts.Time = mobtest.Noon
	}
	w := mobtest.New(t, opts)
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 24, block.Grass{}) })
	return w
}

func mobOf(t *testing.T, w *mobtest.World, r *mobtest.Ref, f func(m *mob.Mob)) {
	t.Helper()
	r.With(f)
}

func riding(w *mobtest.World, p *mobtest.Player) *world.EntityHandle {
	var h *world.EntityHandle
	p.With(func(pl *player.Player) { h = pl.RidingEntityHandle() })
	return h
}

// A tamed, saddled horse: the player gets on by using it, steers it with
// its move input (input_ground_controlled, java riding speed), and gets off.
func TestHorseRideAndSteer(t *testing.T) {
	w := newWorld(t, mobtest.Options{})
	h := w.Spawn("minecraft:horse", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_tame_adult"})
	h.With(func(m *mob.Mob) { m.Fire("minecraft:horse_saddled", nil) })
	p := w.AddPlayer("rider", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(5)
	if _, ok := p.Interact(h, item.Stack{}); !ok {
		t.Fatal("using a saddled horse did not get on")
	}
	if riding(w, p) != h.H() {
		t.Fatal("player does not ride the horse")
	}
	h.With(func(m *mob.Mob) {
		if !m.ActorFlag(protocolKeyboardControlled) {
			t.Error("saddled horse lacks keyboard_controlled")
		}
		c, ok := m.ControllingPassenger()
		if !ok || c.H() != p.H() {
			t.Fatal("player does not control the horse")
		}
	})
	start := h.Pos()
	for i := 0; i < 40; i++ {
		w.Do(func(tx *world.Tx) {
			e, _ := h.Entity(tx)
			r, ok := entity.RideableOf(e)
			if !ok {
				t.Fatal("horse is no Rideable")
			}
			r.MoveInput(mgl64.Vec2{0, 1}, 0, 0) // yaw 0: +Z
		})
		w.Tick(1)
	}
	moved := h.Pos().Sub(start)
	if moved[2] < 3 || math.Abs(moved[0]) > 1 {
		t.Fatalf("horse moved %v in 40 ticks of forward input, want >3 along +Z", moved)
	}
	// The rider is kept on the seat (0, 1.1, -0.2).
	if d := p.Pos().Sub(h.Pos()); math.Abs(d[1]-1.1) > 0.05 {
		t.Errorf("rider %v above horse, want 1.1", d[1])
	}
	p.With(func(pl *player.Player) { pl.DismountEntity(pl.Tx()) })
	w.Tick(2)
	if riding(w, p) != nil {
		t.Fatal("player still rides after dismounting")
	}
	h.With(func(m *mob.Mob) {
		if m.RiderCount() != 0 {
			t.Fatalf("horse keeps %d riders", m.RiderCount())
		}
	})
}

// The power jump (can_power_jump): a full bar jumps by horse.jump_strength.
func TestHorsePowerJump(t *testing.T) {
	w := newWorld(t, mobtest.Options{})
	h := w.Spawn("minecraft:horse", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_tame_adult"})
	h.With(func(m *mob.Mob) { m.Fire("minecraft:horse_saddled", nil) })
	p := w.AddPlayer("rider", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(5)
	p.Interact(h, item.Stack{})
	w.Tick(2)
	var strength float64
	h.With(func(m *mob.Mob) { strength = m.Attr(mob.AttrJumpStrength) })
	w.Do(func(tx *world.Tx) {
		e, _ := h.Entity(tx)
		r, _ := entity.RideableOf(e)
		pe, _ := p.Entity(tx)
		r.(entity.VehicleJumper).RiderJump(pe, 1)
	})
	h.With(func(m *mob.Mob) {
		if math.Abs(m.Vel()[1]-strength) > 1e-9 {
			t.Fatalf("jump velocity %v, want jump_strength %v", m.Vel()[1], strength)
		}
	})
	maxY := 0.0
	for i := 0; i < 20; i++ {
		w.Tick(1)
		maxY = max(maxY, h.Pos()[1]-1)
	}
	if maxY < 0.8 {
		t.Fatalf("horse rose %v blocks, want a jump", maxY)
	}
}

// Taming a wild horse by riding it (tamemount): a full temper tames on the
// first roll; the rider becomes the owner.
func TestHorseTameByRiding(t *testing.T) {
	w := newWorld(t, mobtest.Options{Seed: 7})
	h := w.Spawn("minecraft:horse", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_adult"})
	p := w.AddPlayer("tamer", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(5)
	h.With(func(m *mob.Mob) {
		tm, ok := mob.ComponentOf[*tame.TameMount](m)
		if !ok {
			t.Fatal("wild horse has no tamemount")
		}
		if tm.Temper() != 0 {
			t.Errorf("start temper %d, want min_temper 0", tm.Temper())
		}
	})
	// Feeding raises the temper by temper_mod (wheat 3).
	if _, ok := p.Interact(h, item.NewStack(item.Wheat{}, 1)); !ok {
		t.Fatal("wild horse did not eat wheat")
	}
	h.With(func(m *mob.Mob) {
		tm, _ := mob.ComponentOf[*tame.TameMount](m)
		if tm.Temper() != 3 {
			t.Fatalf("temper %d after wheat, want 3", tm.Temper())
		}
	})
	if _, ok := p.Interact(h, item.Stack{}); !ok || riding(w, p) != h.H() {
		t.Fatal("could not get on the wild horse")
	}
	h.With(func(m *mob.Mob) {
		if _, ok := m.ControllingPassenger(); ok {
			t.Error("an untamed horse is steered by its rider")
		}
	})
	// Ten golden apples (temper_mod 10): temper 100 = max_temper, so the
	// first roll tames. Feeding is refused while ridden.
	p.With(func(pl *player.Player) { pl.DismountEntity(pl.Tx()) })
	for i := 0; i < 10; i++ {
		p.Interact(h, item.NewStack(item.GoldenApple{}, 1))
	}
	p.Interact(h, item.Stack{})
	if _, ok := w.TickUntil(2000, func(tx *world.Tx) bool {
		e, _ := h.Entity(tx)
		m, _ := mob.Of(e)
		return tame.Tamed(m)
	}); !ok {
		t.Fatal("horse with full temper was not tamed by riding")
	}
	h.With(func(m *mob.Mob) {
		if !tame.IsOwner(m, mustPlayer(t, w, p)) {
			t.Fatal("rider is not the owner")
		}
		if !m.Persistent() {
			t.Error("tamed horse is not persistent")
		}
	})
}

func mustPlayer(t *testing.T, w *mobtest.World, p *mobtest.Player) world.Entity {
	var e world.Entity
	w.Do(func(tx *world.Tx) { e, _ = p.Entity(tx) })
	if e == nil {
		t.Fatal("player gone")
	}
	return e
}

// Wolves: bones tame (0.33), the new pet sits, its owner makes it stand,
// it follows the owner and teleports when it cannot keep up.
func TestWolfTameSitFollow(t *testing.T) {
	w := newWorld(t, mobtest.Options{Seed: 3})
	wolf := w.Spawn("minecraft:wolf", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_wild_adult"})
	p := w.AddPlayer("owner", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(5)
	tries := 0
	for ; tries < 60; tries++ {
		p.Interact(wolf, item.NewStack(item.Bone{}, 1))
		tamed := false
		wolf.With(func(m *mob.Mob) { tamed = tame.Tamed(m) })
		if tamed {
			break
		}
	}
	if tries == 60 {
		t.Fatal("60 bones did not tame the wolf")
	}
	w.Tick(2)
	wolf.With(func(m *mob.Mob) {
		if !tame.IsOwner(m, mustPlayer(t, w, p)) {
			t.Fatal("tamer is not the owner")
		}
		if !m.Sitting() {
			t.Error("a freshly tamed wolf does not sit")
		}
		if m.Owner() == nil {
			t.Error("owner not shown (Meta.Owner)")
		}
	})
	// Sitting: it stays even when the owner walks away.
	p.Teleport(mgl64.Vec3{8.5, 1, 0.5})
	start := wolf.Pos()
	w.Tick(60)
	if wolf.Pos().Sub(start).Len() > 0.5 {
		t.Fatal("sitting wolf moved")
	}
	p.Teleport(mgl64.Vec3{2.5, 1, 0.5})
	if _, ok := p.Interact(wolf, item.Stack{}); !ok {
		t.Fatal("owner could not make the wolf stand")
	}
	wolf.With(func(m *mob.Mob) {
		if m.Sitting() {
			t.Fatal("wolf still sits")
		}
	})
	// Following: start_distance 10, stop_distance 2.
	p.Teleport(mgl64.Vec3{14.5, 1, 0.5})
	if _, ok := w.TickUntil(400, func(tx *world.Tx) bool { return w.Dist(wolf, p) < 4 }); !ok {
		t.Fatalf("wolf did not follow its owner: %v", w.Dist(wolf, p))
	}
}

// A stranger cannot make a tamed wolf sit; a bone does not re-tame it.
func TestWolfSitOwnerOnly(t *testing.T) {
	w := newWorld(t, mobtest.Options{})
	wolf := w.Spawn("minecraft:wolf", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_tame_adult"})
	owner := w.AddPlayer("owner", mgl64.Vec3{2.5, 1, 0.5})
	other := w.AddPlayer("other", mgl64.Vec3{-1.5, 1, 0.5})
	w.Tick(2)
	wolf.With(func(m *mob.Mob) { m.SetOwner(mustPlayer(t, w, owner)) })
	if _, ok := other.Interact(wolf, item.Stack{}); ok {
		t.Error("a stranger made the wolf sit")
	}
	if _, ok := owner.Interact(wolf, item.Stack{}); !ok {
		t.Fatal("owner could not make the wolf sit")
	}
	wolf.With(func(m *mob.Mob) {
		if !m.Sitting() {
			t.Fatal("wolf does not sit")
		}
	})
}

// Owners are saved by UUID and found again after a reload.
func TestOwnerSaved(t *testing.T) {
	w := newWorld(t, mobtest.Options{Persistent: true})
	wolf := w.Spawn("minecraft:wolf", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_tame_adult"})
	p := w.AddPlayer("owner", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(2)
	pe := mustPlayer(t, w, p)
	id := pe.H().UUID()
	wolf.With(func(m *mob.Mob) {
		m.SetOwner(pe)
		s, _ := tame.SittableOf(m)
		s.SetSitting(m, true)
	})
	w.Reload()
	w.Tick(2)
	wolf.With(func(m *mob.Mob) {
		got, ok := m.OwnerUUID()
		if !ok || got != id {
			t.Fatalf("owner after reload %v, want %v", got, id)
		}
		if !m.Sitting() {
			t.Error("sitting not saved")
		}
		if o, ok := m.OwnerEntity(); !ok || o.H().UUID() != id {
			t.Error("owner not found again")
		}
	})
}

// A spider jockey: addrider spawns the skeleton on the spider, which
// carries it (mount_pathing); the link is saved with the spider.
func TestSpiderJockey(t *testing.T) {
	w := newWorld(t, mobtest.Options{Persistent: true, Time: 18000})
	sp := w.Spawn("minecraft:spider", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:entity_spawned_with_default_jockey"})
	w.Tick(3)
	var rider *world.EntityHandle
	sp.With(func(m *mob.Mob) {
		if m.RiderCount() != 1 {
			t.Fatalf("spider jockey has %d riders", m.RiderCount())
		}
		rider = m.PassengerHandles()[0]
	})
	r := w.Track(rider)
	r.With(func(m *mob.Mob) {
		if m.Identifier() != "minecraft:skeleton" {
			t.Errorf("rider %v, want skeleton", m.Identifier())
		}
		if !m.Riding() {
			t.Error("skeleton does not know it rides")
		}
		if !m.ActorFlag(protocolRotationLocked) {
			t.Error("skeleton rider lacks rotation_locked_to_vehicle")
		}
	})
	w.Tick(20)
	if d := r.Pos().Sub(sp.Pos()); math.Abs(d[1]-0.54) > 0.05 && math.Abs(d[1]-0.325) > 0.05 {
		t.Errorf("rider %v above spider, want seat height", d[1])
	}
	w.Reload()
	w.Tick(5)
	sp.With(func(m *mob.Mob) {
		if m.RiderCount() != 1 {
			t.Fatalf("spider has %d riders after reload", m.RiderCount())
		}
	})
}

// Baby zombies look for a chicken (find_mount, baby_undead seat).
func TestBabyZombieFindsChicken(t *testing.T) {
	w := newWorld(t, mobtest.Options{Seed: 11, Time: 18000})
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:as_baby_jockey"})
	c := w.Spawn("minecraft:chicken", mgl64.Vec3{4.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_adult"})
	if _, ok := w.TickUntil(600, func(tx *world.Tx) bool {
		e, _ := z.Entity(tx)
		m, _ := mob.Of(e)
		return m != nil && m.VehicleHandle() == c.H()
	}); !ok {
		z.With(func(m *mob.Mob) { t.Fatalf("baby zombie did not mount the chicken: %v", w.Dump(z)) })
	}
}

// A saddled pig: the player gets on, and holding a carrot on a stick it
// steers (controlled_by_player); using the stick boosts it (boostable).
func TestPigCarrotOnAStick(t *testing.T) {
	w := newWorld(t, mobtest.Options{})
	pig := w.Spawn("minecraft:pig", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_adult"})
	p := w.AddPlayer("rider", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(2)
	if _, ok := p.Interact(pig, item.NewStack(item.Saddle{}, 1)); !ok {
		t.Fatal("pig did not take the saddle")
	}
	w.Tick(1)
	pig.With(func(m *mob.Mob) {
		if !m.Saddled() {
			t.Fatal("pig is not saddled")
		}
	})
	if _, ok := p.Interact(pig, item.Stack{}); !ok || riding(w, p) != pig.H() {
		t.Fatal("could not get on the saddled pig")
	}
	// Without the stick the pig does not go where the rider looks.
	p.With(func(pl *player.Player) { pl.Move(mgl64.Vec3{}, 90, 0) }) // yaw 90: -X
	start := pig.Pos()
	w.Tick(40)
	p.Hold(item.NewStack(item.CarrotOnAStick{}, 1))
	w.Tick(80)
	moved := pig.Pos().Sub(start)
	if moved[0] > -2 {
		t.Fatalf("pig moved %v, want along -X towards the rider's look", moved)
	}
	var used bool
	p.With(func(pl *player.Player) {
		held, _ := pl.HeldItems()
		ctx := &item.UseContext{}
		used = held.Item().(item.Usable).Use(pl.Tx(), pl, ctx)
		if used && ctx.Damage != 2 {
			t.Errorf("boost damage %d, want 2", ctx.Damage)
		}
	})
	if !used {
		t.Fatal("carrot on a stick did not boost")
	}
	pig.With(func(m *mob.Mob) {
		b, ok := mob.ComponentOf[*cride.Boostable](m)
		if !ok || !b.Boosting() || b.Multiplier(m) != 1.35 {
			t.Fatal("pig is not boosted by 1.35")
		}
	})
}

// Donkeys: a tamed donkey takes a saddle through its interact component
// (equip_item_slot 0, equippable), shows it saddled, and drops it again for
// shears (drop_item_slot).
func TestDonkeySaddleEquip(t *testing.T) {
	w := newWorld(t, mobtest.Options{})
	d := w.Spawn("minecraft:donkey", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_tame_adult"})
	p := w.AddPlayer("p", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(2)
	left, ok := p.Interact(d, item.NewStack(item.Saddle{}, 1))
	if !ok || !left.Empty() {
		t.Fatalf("saddle not used (%v, left %v)", ok, left)
	}
	w.Tick(1)
	d.With(func(m *mob.Mob) {
		e, _ := mob.ComponentOf[*cride.Equippable](m)
		if e == nil || e.Item(0).Empty() {
			t.Fatal("saddle not in equippable slot 0")
		}
		if !m.Saddled() {
			t.Fatal("donkey not saddled after on_equip")
		}
		if !m.HasComponent("minecraft:input_ground_controlled") {
			t.Error("saddled donkey cannot be steered")
		}
	})
	if _, ok := p.Interact(d, item.NewStack(item.Shears{}, 1)); !ok {
		t.Fatal("shears did not take the saddle off")
	}
	w.Tick(1)
	d.With(func(m *mob.Mob) {
		if m.Saddled() {
			t.Fatal("donkey still saddled")
		}
	})
	if n := w.Count(func(e world.Entity) bool { return e.H().Type() == entity.ItemType }); n == 0 {
		t.Error("removed saddle was not dropped")
	}
}

// Parrots on a cookie: healable with force_use applies fatal poison.
func TestParrotCookie(t *testing.T) {
	w := newWorld(t, mobtest.Options{})
	par := w.Spawn("minecraft:parrot", mgl64.Vec3{0.5, 1, 0.5})
	p := w.AddPlayer("p", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(2)
	if _, ok := p.Interact(par, item.NewStack(item.Cookie{}, 1)); !ok {
		t.Fatal("parrot did not take the cookie")
	}
	par.With(func(m *mob.Mob) {
		if len(m.Effects()) == 0 {
			t.Fatal("cookie did not poison the parrot")
		}
	})
}

// Wolves beg at a player holding a bone (interested flag).
func TestWolfBegs(t *testing.T) {
	w := newWorld(t, mobtest.Options{})
	wolf := w.Spawn("minecraft:wolf", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_wild_adult"})
	p := w.AddPlayer("p", mgl64.Vec3{3.5, 1, 0.5})
	p.Hold(item.NewStack(item.Bone{}, 1))
	if _, ok := w.TickUntil(200, func(tx *world.Tx) bool {
		e, _ := wolf.Entity(tx)
		m, _ := mob.Of(e)
		return m.ActorFlag(protocolInterested)
	}); !ok {
		t.Fatal("wolf did not beg for the bone")
	}
	p.Hold(item.Stack{})
	if _, ok := w.TickUntil(100, func(tx *world.Tx) bool {
		e, _ := wolf.Entity(tx)
		m, _ := mob.Of(e)
		return !m.ActorFlag(protocolInterested)
	}); !ok {
		t.Fatal("wolf kept begging without the bone")
	}
}

// Wolves attack what their owner hits (owner_hurt_target).
func TestWolfOwnerHurtTarget(t *testing.T) {
	w := newWorld(t, mobtest.Options{})
	wolf := w.Spawn("minecraft:wolf", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:spawn_tame_adult"})
	sheep := w.Spawn("minecraft:sheep", mgl64.Vec3{4.5, 1, 4.5}, mob.SpawnOptions{Event: "minecraft:spawn_adult"})
	p := w.AddPlayer("owner", mgl64.Vec3{2.5, 1, 2.5})
	w.Tick(2)
	wolf.With(func(m *mob.Mob) { m.SetOwner(mustPlayer(t, w, p)) })
	w.Do(func(tx *world.Tx) {
		e, _ := sheep.Entity(tx)
		pe, _ := p.Entity(tx)
		m, _ := mob.Of(e)
		m.Hurt(1, entity.AttackDamageSource{Attacker: pe})
	})
	if _, ok := w.TickUntil(60, func(tx *world.Tx) bool {
		e, _ := wolf.Entity(tx)
		m, _ := mob.Of(e)
		return m.TargetHandle() == sheep.H()
	}); !ok {
		t.Fatal("wolf did not target what its owner hit")
	}
}

const (
	protocolKeyboardControlled = protocol.EntityDataFlagKeyboardControlled
	protocolRotationLocked     = protocol.EntityDataFlagRotationLockedToVehicle
	protocolInterested         = protocol.EntityDataFlagInterested
)

// What the sessions read: entity.RideableOf/RiderOf give the mob's seat,
// riding flag data and controller.
func TestRidingAdapters(t *testing.T) {
	w := newWorld(t, mobtest.Options{Time: 18000})
	sp := w.Spawn("minecraft:spider", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:entity_spawned_with_default_jockey"})
	w.Tick(3)
	w.Do(func(tx *world.Tx) {
		e, _ := sp.Entity(tx)
		r, ok := entity.RideableOf(e)
		if !ok {
			t.Fatal("spider is no Rideable")
		}
		if r.ControllingRider() != nil || r.ControllingSeatIndex() != -1 {
			t.Error("a skeleton rider controls the spider")
		}
		seats := r.Riders()
		if len(seats) != 1 || seats[0].SeatIndex != 0 {
			t.Fatalf("riders %v", seats)
		}
		re, _ := seats[0].Handle.Entity(tx)
		rider, ok := entity.RiderOf(re)
		if !ok || rider.RidingEntityHandle() != e.H() || rider.SeatIndex() != 0 {
			t.Fatal("skeleton is no Rider of the spider")
		}
		if off, ok := rider.SeatOffset(); !ok || off[1] <= 0 {
			t.Errorf("seat offset %v %v", off, ok)
		}
		if f, ok := r.(entity.RiderFilter); !ok || f.AcceptsRider(re) {
			t.Error("a full spider accepts another rider")
		}
	})
	// A mob without rideable is no Rideable, but every mob is a Rider.
	c := w.Spawn("minecraft:creeper", mgl64.Vec3{4.5, 1, 0.5})
	w.Do(func(tx *world.Tx) {
		e, _ := c.Entity(tx)
		if _, ok := entity.RideableOf(e); ok {
			t.Error("creeper is a Rideable")
		}
		if r, ok := entity.RiderOf(e); !ok || r.SeatIndex() != -1 {
			t.Error("creeper is no idle Rider")
		}
	})
}
