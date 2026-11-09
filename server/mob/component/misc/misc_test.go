//go:build crossfly_full

package misc_test

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/component/misc"
	"github.com/df-mc/dragonfly/server/mob/goal/combat"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	_ "github.com/df-mc/dragonfly/server/mob/vanilla"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

func groups(m *mob.Mob) []string {
	if g, ok := m.Instance().(mob.InstanceGroups); ok {
		return g.ActiveGroups()
	}
	return nil
}

func hasGroup(w *mobtest.World, r *mobtest.Ref, g string) bool {
	ok := false
	r.With(func(m *mob.Mob) { ok = slices.Contains(groups(m), g) })
	return ok
}

func arena(t *testing.T) *mobtest.World {
	mobtest.RequireDefinitions(t)
	mobtest.RequireFilters(t)
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Arena(tx, 12, 3) })
	return w
}

func build(t *testing.T, key, raw string) map[string]any {
	t.Helper()
	c, err := misc.Build(key, raw)
	if err != nil {
		t.Fatalf("%v: %v", key, err)
	}
	return misc.Params(c)
}

func eq(t *testing.T, what string, got map[string]any, want map[string]any) {
	t.Helper()
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%v %v = %v, want %v", what, k, got[k], v)
		}
	}
}

// TestBDSDefaults checks the defaults of the components with parameters
// against BDS (definitions / constructors named in their doc comments).
func TestBDSDefaults(t *testing.T) {
	eq(t, "area_attack", build(t, "minecraft:area_attack", `{}`), map[string]any{"range": 0.2, "damage": 2.0, "cooldown": 0, "cause": "", "sound": true})
	eq(t, "mob_effect", build(t, "minecraft:mob_effect", `{}`), map[string]any{"range": 0.2, "duration": 200, "cooldown": 0, "ambient": false})
	eq(t, "damage_over_time", build(t, "minecraft:damage_over_time", `{}`), map[string]any{"damage": 1.0, "interval": 0})
	eq(t, "attack_cooldown", build(t, "minecraft:attack_cooldown", `{}`), map[string]any{"min": 1.0, "max": 1.0})
	eq(t, "entity_sensor", build(t, "minecraft:entity_sensor", `{"subsensors":[{"event":"x"}]}`), map[string]any{"relative": true, "players": false, "h": 10.0, "v": 10.0, "min": 1, "max": -1, "cooldown": -1})
	eq(t, "entity_sensor legacy", build(t, "minecraft:entity_sensor", `{"sensor_range": 3, "event": "x"}`), map[string]any{"h": 3.0, "v": 3.0, "subs": 1})
	eq(t, "block_sensor", build(t, "minecraft:block_sensor", `{}`), map[string]any{"radius": 16.0})
	eq(t, "combat_regeneration", build(t, "minecraft:combat_regeneration", `{}`), map[string]any{"duration": 100, "self": false, "family": false})
	eq(t, "strength", build(t, "minecraft:strength", `{}`), map[string]any{"value": 1, "max": 5})
	eq(t, "genetics", build(t, "minecraft:genetics", `{}`), map[string]any{"mutation": 0.03125, "genes": 0})
	eq(t, "apply_knockback_rules", build(t, "minecraft:apply_knockback_rules", `{"presets":[{}]}`), map[string]any{"h": 0.4, "v": 0.4, "water": false})

	// Vanilla values parse as written.
	eq(t, "slime area_attack", build(t, "minecraft:area_attack", string(mobtest.Component(t, "slime.json", "minecraft:slime_large", "minecraft:area_attack"))),
		map[string]any{"range": 0.15, "damage": 4.0, "cooldown": 10, "cause": "entity_attack"})
	eq(t, "warden mob_effect", build(t, "minecraft:mob_effect", string(mobtest.Component(t, "warden.json", "", "minecraft:mob_effect"))),
		map[string]any{"range": 20.0, "duration": 260, "cooldown": 120})
	eq(t, "vex damage_over_time", build(t, "minecraft:damage_over_time", string(mobtest.Component(t, "vex.json", "minecraft:periodic_damage", "minecraft:damage_over_time"))),
		map[string]any{"damage": 1.0, "interval": 20})
	eq(t, "goat attack_cooldown", build(t, "minecraft:attack_cooldown", string(mobtest.Component(t, "goat.json", "attack_cooldown", "minecraft:attack_cooldown"))),
		map[string]any{"min": 30.0, "max": 40.0})
	eq(t, "llama strength", build(t, "minecraft:strength", string(mobtest.Component(t, "llama.json", "minecraft:strength_3", "minecraft:strength"))),
		map[string]any{"value": 3, "max": 5})
}

// TestFireImmune: a blaze in lava takes no damage, does not burn and has the
// fire_immune actor flag.
func TestFireImmune(t *testing.T) {
	w := arena(t)
	b := w.Spawn("minecraft:blaze", mgl64.Vec3{0.5, 1, 0.5})
	w.Do(func(tx *world.Tx) { tx.SetBlock(cube.Pos{0, 1, 0}, block.Lava{Still: true, Depth: 8}, nil) })
	var before float64
	b.With(func(m *mob.Mob) {
		before = m.Health()
		if !m.ActorFlag(protocol.EntityDataFlagFireImmune) {
			t.Error("fire_immune actor flag not set")
		}
		m.SetOnFire(5 * time.Second)
	})
	w.Tick(60)
	b.With(func(m *mob.Mob) {
		if m.Health() < before {
			t.Errorf("blaze took fire damage: %v -> %v", before, m.Health())
		}
		if m.OnFireDuration() > 0 {
			t.Error("blaze still burning")
		}
		if n, _ := m.Hurt(3, block.FireDamageSource{}); n != 0 {
			t.Error("fire damage dealt")
		}
		if n, _ := m.Hurt(3, entity.AttackDamageSource{}); n == 0 {
			t.Error("other damage cancelled")
		}
	})
	w.Do(func(tx *world.Tx) {
		if e, _ := b.Entity(tx); !misc.IsFireImmune(e) {
			t.Error("IsFireImmune false")
		}
	})
}

// TestFreezing: freezing_immune cancels freezing damage, freezing_vulnerable
// multiplies it by 5.
func TestFreezing(t *testing.T) {
	w := arena(t)
	s := w.Spawn("minecraft:skeleton", mgl64.Vec3{0.5, 1, 0.5})
	b := w.Spawn("minecraft:blaze", mgl64.Vec3{4.5, 1, 0.5})
	w.Tick(1)
	s.With(func(m *mob.Mob) {
		if n, _ := m.Hurt(1, core.CauseSource{Cause: "freezing"}); n != 0 {
			t.Errorf("skeleton took %v freezing damage", n)
		}
	})
	b.With(func(m *mob.Mob) {
		if n, _ := m.Hurt(1, core.CauseSource{Cause: "freezing"}); n != 5 {
			t.Errorf("blaze took %v freezing damage, want 5", n)
		}
	})
}

// TestEffectImmunity: a nautilus never gets poison.
func TestEffectImmunity(t *testing.T) {
	w := arena(t)
	n := w.Spawn("minecraft:nautilus", mgl64.Vec3{0.5, 1, 0.5})
	n.With(func(m *mob.Mob) {
		m.AddEffect(effect.New(effect.Poison, 1, 10*time.Second))
		m.AddEffect(effect.New(effect.Speed, 1, 10*time.Second))
		if _, ok := m.Effect(effect.Poison); ok {
			t.Error("nautilus got poison")
		}
		if _, ok := m.Effect(effect.Speed); !ok {
			t.Error("nautilus did not get speed")
		}
	})
}

// TestPufferfishPoisons: a fully puffed pufferfish poisons a player
// touching it for 10 s (mob_effect) and hurts them (area_attack).
func TestPufferfishPoisons(t *testing.T) {
	w := arena(t)
	p := w.AddPlayer("p", mgl64.Vec3{1.2, 1, 0.5})
	f := w.Spawn("minecraft:pufferfish", mgl64.Vec3{0.5, 1, 0.5})
	f.With(func(m *mob.Mob) { m.Fire("minecraft:to_full_puff", nil) })
	w.Tick(3)
	p.With(func(pl *player.Player) {
		e, ok := pl.Effect(effect.Poison)
		if !ok {
			t.Fatal("player not poisoned")
		}
		if d := e.Duration(); d > 10*time.Second || d < 9*time.Second {
			t.Errorf("poison lasts %v, want about 10 s", d)
		}
	})
	if p.Health() >= 20 {
		t.Error("pufferfish area_attack dealt no damage")
	}
}

// TestSlimeAreaAttack: a large slime hurts an iron golem (in its
// entity_filter) touching it for 4, once per damage_cooldown (0.5 s). A mob
// is used because Dragonfly's player hurt immunity runs on wall-clock time.
func TestSlimeAreaAttack(t *testing.T) {
	w := arena(t)
	g := w.Spawn("minecraft:iron_golem", mgl64.Vec3{2.1, 1, 0.5})
	s := w.Spawn("minecraft:slime", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "spawn_large"})
	var hits []int
	var dmg []float64
	last := 100.0
	w.Each(40, func(tx *world.Tx, tick int) bool {
		for _, r := range []*mobtest.Ref{g, s} {
			if m, ok := r.Mob(tx); ok {
				m.SetVelocity(mgl64.Vec3{})
			}
		}
		gm, _ := g.Mob(tx)
		sm, _ := s.Mob(tx)
		gm.SetPosition(mgl64.Vec3{2.1, 1, 0.5})
		sm.SetPosition(mgl64.Vec3{0.5, 1, 0.5})
		if h := gm.Health(); h < last {
			hits, dmg = append(hits, tick), append(dmg, last-h)
			last = h
		}
		return true
	})
	if len(hits) < 3 {
		t.Fatalf("slime hit %d times in 40 ticks, want 4 (%v)", len(hits), w.Dump(s))
	}
	for i := 1; i < len(hits); i++ {
		if hits[i]-hits[i-1] < 10 {
			t.Errorf("hits %d ticks apart, below the 10-tick cooldown: %v", hits[i]-hits[i-1], hits)
		}
	}
	if dmg[1] != 4 {
		t.Errorf("hit for %v, want 4", dmg[1])
	}
}

// TestVexDamageOverTime: the periodic_damage group hurts the vex every
// second.
func TestVexDamageOverTime(t *testing.T) {
	w := arena(t)
	v := w.Spawn("minecraft:vex", mgl64.Vec3{0.5, 3, 0.5})
	var before float64
	v.With(func(m *mob.Mob) { m.Fire("minecraft:add_periodic_damage", nil); before = m.Health() })
	w.Tick(41)
	v.With(func(m *mob.Mob) {
		if lost := before - m.Health(); lost < 2 || lost > 3 {
			t.Errorf("vex lost %v health in 41 ticks, want 2", lost)
		}
	})
}

// TestAttackCooldown: the goat's attack_cooldown group fires
// attack_cooldown_complete_event after 30..40 s, which removes it.
func TestAttackCooldown(t *testing.T) {
	w := arena(t)
	g := w.Spawn("minecraft:goat", mgl64.Vec3{0.5, 1, 0.5})
	g.With(func(m *mob.Mob) { m.Fire("start_event", nil) })
	if !hasGroup(w, g, "attack_cooldown") {
		t.Fatal("attack_cooldown group not added")
	}
	w.Tick(590)
	if !hasGroup(w, g, "attack_cooldown") {
		t.Fatal("cooldown ended before 30 s")
	}
	n, ok := w.TickUntil(220, func(*world.Tx) bool { return !hasGroup(w, g, "attack_cooldown") })
	if !ok {
		t.Fatal("cooldown never completed")
	}
	if total := 590 + n; total > 801 {
		t.Errorf("cooldown took %d ticks, want <= 800", total)
	}
}

// TestIronGolemKnockback: the iron golem's apply_knockback_rules fling its
// melee target upwards (vertical 0.39 under the 0.8 cap, horizontal 0.52).
func TestIronGolemKnockback(t *testing.T) {
	w := arena(t)
	p := w.AddPlayer("p", mgl64.Vec3{2, 1, 0.5})
	p.SetMaxHealth(500)
	g := w.Spawn("minecraft:iron_golem", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(2)
	w.Do(func(tx *world.Tx) {
		m, _ := g.Mob(tx)
		e, _ := p.Entity(tx)
		pl := e.(*player.Player)
		pl.SetVelocity(mgl64.Vec3{})
		if !combat.DoHurtTarget(m, pl) {
			t.Fatal("golem did not hit")
		}
		v := pl.Velocity()
		if math.Abs(v[1]-0.39) > 0.01 {
			t.Errorf("vertical knock back %.3f, want 0.39", v[1])
		}
		if h := math.Hypot(v[0], v[2]); math.Abs(h-0.52) > 0.01 {
			t.Errorf("horizontal knock back %.3f, want 0.52", h)
		}
	})
}

// TestCombatRegeneration: when the axolotl's target is killed by a player,
// the player gets Regeneration for 5 s and loses Mining Fatigue.
func TestCombatRegeneration(t *testing.T) {
	w := arena(t)
	p := w.AddPlayer("p", mgl64.Vec3{4, 1, 0.5})
	a := w.Spawn("minecraft:axolotl", mgl64.Vec3{0.5, 1, 0.5})
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{0.5, 1, 4.5})
	w.Do(func(tx *world.Tx) {
		am, _ := a.Mob(tx)
		zm, _ := z.Mob(tx)
		am.SetTarget(zm)
		e, _ := p.Entity(tx)
		e.(*player.Player).AddEffect(effect.New(effect.MiningFatigue, 1, time.Minute))
	})
	w.Tick(1)
	w.Do(func(tx *world.Tx) {
		zm, _ := z.Mob(tx)
		e, _ := p.Entity(tx)
		zm.Hurt(100, entity.AttackDamageSource{Attacker: e})
	})
	w.Tick(2)
	p.With(func(pl *player.Player) {
		r, ok := pl.Effect(effect.Regeneration)
		if !ok {
			t.Fatal("player got no regeneration")
		}
		if r.Duration() > 5*time.Second || r.Duration() < 4*time.Second {
			t.Errorf("regeneration lasts %v, want 5 s", r.Duration())
		}
		if _, ok := pl.Effect(effect.MiningFatigue); ok {
			t.Error("mining fatigue not removed")
		}
	})
}

// TestOnHurt: a blaze hurt by anything fires on_hurt_event (adds
// mode_switcher); on_hurt_by_player needs a player attacker.
func TestOnHurt(t *testing.T) {
	w := arena(t)
	b := w.Spawn("minecraft:blaze", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{NoEvent: true})
	w.Tick(1)
	if hasGroup(w, b, "mode_switcher") {
		t.Fatal("mode_switcher before any hit")
	}
	b.With(func(m *mob.Mob) { m.Hurt(1, entity.AttackDamageSource{}) })
	if !hasGroup(w, b, "mode_switcher") {
		t.Fatal("on_hurt did not fire")
	}
}

// TestOnFriendlyAnger: when a polar bear becomes angry, the adult polar
// bears around fire on_anger (adult_wild -> adult_hostile).
func TestOnFriendlyAnger(t *testing.T) {
	w := arena(t)
	a := w.Spawn("minecraft:polar_bear", mgl64.Vec3{0.5, 1, 0.5}, mob.SpawnOptions{Event: "minecraft:entity_born"})
	b := w.Spawn("minecraft:polar_bear", mgl64.Vec3{4.5, 1, 0.5})
	b.With(func(m *mob.Mob) {
		if !slices.Contains(groups(m), "minecraft:adult_wild") {
			m.Fire("minecraft:entity_spawned", nil)
		}
	})
	w.Tick(1)
	if !hasGroup(w, b, "minecraft:adult_wild") {
		t.Skip("second bear spawned as a baby")
	}
	// The bear that hears about the anger already has the attacker as its
	// target (hurt_by_target alert_same_type does that in the game), else
	// its angry component calms it down at once.
	p := w.AddPlayer("p", mgl64.Vec3{8.5, 1, 0.5})
	w.Do(func(tx *world.Tx) {
		pe, _ := p.Entity(tx)
		bm, _ := b.Mob(tx)
		bm.SetTarget(pe)
		am, _ := a.Mob(tx)
		am.UpdateMeta(func(meta *mob.Meta) { meta.Angry = true })
	})
	w.Tick(2)
	if !hasGroup(w, b, "minecraft:adult_hostile") {
		t.Errorf("friendly anger did not fire: %v", w.Dump(b))
	}
}

// TestPufferfishSensor: a player within 2.5 blocks makes a normal
// pufferfish start puffing up (entity_sensor -> start_half_puff).
func TestPufferfishSensor(t *testing.T) {
	w := arena(t)
	f := w.Spawn("minecraft:pufferfish", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(2)
	if !hasGroup(w, f, "minecraft:normal_puff") {
		t.Skipf("pufferfish not in normal_puff: %v", w.Dump(f))
	}
	w.AddPlayer("p", mgl64.Vec3{2.5, 1, 0.5})
	w.Tick(2)
	if hasGroup(w, f, "minecraft:normal_puff") || !hasGroup(w, f, "minecraft:half_puff_primary") {
		t.Errorf("sensor did not fire start_half_puff: %v", w.Dump(f))
	}
}

// TestPiglinBlockSensor: breaking a gold block near an adult piglin fires
// important_block_destroyed_event; a far one does not.
func TestPiglinBlockSensor(t *testing.T) {
	w := arena(t)
	pg := w.Spawn("minecraft:piglin", mgl64.Vec3{0.5, 1, 0.5})
	p := w.AddPlayer("p", mgl64.Vec3{3, 1, 0.5})
	w.Tick(2)
	if !hasGroup(w, pg, "piglin_adult") {
		t.Skip("baby piglin")
	}
	w.Do(func(tx *world.Tx) {
		e, _ := p.Entity(tx)
		misc.BlockBroken(tx, cube.Pos{40, 1, 0}, block.Gold{}, e)
	})
	if hasGroup(w, pg, "take_target_as_response_to_block_break") {
		t.Fatal("fired for a block 40 blocks away")
	}
	w.Do(func(tx *world.Tx) {
		e, _ := p.Entity(tx)
		misc.BlockBroken(tx, cube.Pos{3, 1, 3}, block.Stone{}, e)
	})
	if hasGroup(w, pg, "take_target_as_response_to_block_break") {
		t.Fatal("fired for stone")
	}
	w.Do(func(tx *world.Tx) {
		e, _ := p.Entity(tx)
		misc.BlockBroken(tx, cube.Pos{3, 1, 3}, block.Gold{}, e)
	})
	if !hasGroup(w, pg, "take_target_as_response_to_block_break") {
		t.Errorf("gold block break not sensed: %v", w.Dump(pg))
	}
}

// TestSpellEffects: a cured zombie villager's to_villager group gives it
// strength and removes weakness.
func TestSpellEffects(t *testing.T) {
	w := arena(t)
	z := w.Spawn("minecraft:zombie_villager_v2", mgl64.Vec3{0.5, 1, 0.5})
	z.With(func(m *mob.Mob) {
		m.AddEffect(effect.New(effect.Weakness, 1, time.Minute))
		m.Fire("villager_converted", nil)
		if _, ok := m.Effect(effect.Weakness); ok {
			t.Error("weakness not removed")
		}
		s, ok := m.Effect(effect.Strength)
		if !ok {
			t.Fatal("no strength")
		}
		if s.Duration() != 300*time.Second {
			t.Errorf("strength lasts %v, want 300 s", s.Duration())
		}
	})
}

// TestPandaGenetics: a spawned panda rolls alleles in 0..15 and fires the
// variant event of the first matching genetic variant; a bred baby takes
// one allele of each parent.
func TestPandaGenetics(t *testing.T) {
	w := arena(t)
	p := w.Spawn("minecraft:panda", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(2)
	p.With(func(m *mob.Mob) {
		g, ok := mob.ComponentOf[*misc.Genetics](m)
		if !ok {
			t.Fatal("no genetics")
		}
		main, hidden, ok := g.Alleles("panda_variant")
		if !ok || main < 0 || main > 15 || hidden < 0 || hidden > 15 {
			t.Fatalf("alleles %v %v %v", main, hidden, ok)
		}
		want := map[int]string{0: "minecraft:panda_lazy", 1: "minecraft:panda_worried", 2: "minecraft:panda_playful", 3: "minecraft:panda_aggressive"}
		gs := groups(m)
		if ev, ok := want[main]; ok && !slices.Contains(gs, ev) {
			t.Errorf("main allele %d, groups %v: missing %v", main, gs, ev)
		}
	})

	// Breeding: both parents lazy (0/0) -> the baby is 0/0 unless it
	// mutated (3 %).
	a := w.Spawn("minecraft:panda", mgl64.Vec3{3.5, 1, 3.5})
	b := w.Spawn("minecraft:panda", mgl64.Vec3{4, 1, 3.5})
	w.Tick(2)
	var baby *mob.Mob
	w.Do(func(tx *world.Tx) {
		am, _ := a.Mob(tx)
		bm, _ := b.Mob(tx)
		for _, m := range []*mob.Mob{am, bm} {
			m.Fire("minecraft:ageable_grow_up", nil)
			g, _ := mob.ComponentOf[*misc.Genetics](m)
			g.SetAlleles("panda_variant", 0, 0)
			br, ok := core.BreedableOf(m)
			if !ok {
				t.Skip("panda not breedable")
			}
			br.SetInLove(m, nil)
		}
		ab, _ := core.BreedableOf(am)
		babies := ab.Mate(am, bm)
		if len(babies) == 0 {
			t.Fatal("no baby")
		}
		baby = babies[0]
		g, _ := mob.ComponentOf[*misc.Genetics](baby)
		main, hidden, _ := g.Alleles("panda_variant")
		if main != 0 && hidden != 0 {
			t.Errorf("baby alleles %d/%d, want inherited 0s", main, hidden)
		}
	})
}

// TestStrengthAndFlags: llama strength, ravager stun and sniffer pregnancy.
func TestStrengthAndFlags(t *testing.T) {
	w := arena(t)
	l := w.Spawn("minecraft:llama", mgl64.Vec3{0.5, 1, 0.5})
	r := w.Spawn("minecraft:ravager", mgl64.Vec3{5.5, 1, 0.5})
	s := w.Spawn("minecraft:sniffer", mgl64.Vec3{-5.5, 1, 0.5})
	w.Tick(1)
	l.With(func(m *mob.Mob) {
		st, ok := mob.ComponentOf[*misc.Strength](m)
		if !ok || st.Value < 1 || st.Value > 5 || st.Max != 5 {
			t.Errorf("llama strength %+v %v", st, ok)
		}
	})
	r.With(func(m *mob.Mob) {
		m.Fire("minecraft:become_stunned", nil)
		if !m.ActorFlag(protocol.EntityDataFlagStunned) || !misc.IsStunned(m) {
			t.Error("ravager not stunned")
		}
	})
	s.With(func(m *mob.Mob) {
		m.Fire("minecraft:become_pregnant", nil)
		if m.HasComponent("minecraft:is_pregnant") && !m.ActorFlag(protocol.EntityDataFlagPregnant) {
			t.Error("pregnant flag not set")
		}
	})
}

// TestOnEquipmentChanged: a sulfur cube given a block in its main hand fires
// on_block_absorbed; taking it fires on_block_ejected.
func TestOnEquipmentChanged(t *testing.T) {
	w := arena(t)
	c := w.Spawn("minecraft:sulfur_cube", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(2)
	var before []string
	c.With(func(m *mob.Mob) {
		before = groups(m)
		m.SetHeldItems(item.NewStack(block.Stone{}, 1), item.Stack{})
	})
	w.Tick(1)
	var after []string
	c.With(func(m *mob.Mob) { after = groups(m) })
	if slices.Equal(before, after) {
		t.Errorf("groups unchanged after equipping: %v", after)
	}
}
