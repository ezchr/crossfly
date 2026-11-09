//go:build crossfly_full

package misc_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/goal/misc"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// fangs returns the positions of the evoker fangs in the world.
func fangs(w *mobtest.World) []mgl64.Vec3 {
	var ps []mgl64.Vec3
	w.Do(func(tx *world.Tx) {
		for e := range tx.Entities() {
			if e.H().Type() == misc.FangType {
				ps = append(ps, e.Position())
			}
		}
	})
	return ps
}

// TestFangBite: evoker fangs bite two ticks after they appear, for 6 magic
// damage that armour does not lower (EvocationFang::normalTick /
// _dealDamageTo), and are gone after 22 ticks.
func TestFangBite(t *testing.T) {
	w := arena(t, 8)
	p := w.AddPlayer("p", mgl64.Vec3{0.5, 1, 0.5})
	p.SetMaxHealth(200)
	p.With(func(pl *player.Player) {
		pl.Armour().SetChestplate(item.NewStack(item.Chestplate{Tier: item.ArmourTierDiamond{}}, 1))
		pl.Armour().SetHelmet(item.NewStack(item.Helmet{Tier: item.ArmourTierDiamond{}}, 1))
	})
	e := w.Spawn("minecraft:evocation_illager", mgl64.Vec3{6.5, 1, 6.5})
	w.Do(func(tx *world.Tx) {
		m, _ := e.Mob(tx)
		misc.SpawnFang(tx, mgl64.Vec3{0, 1, 0}, m, 0)
	})
	// The player may regenerate 1 in a tick; diamond armour would take the
	// bite below 4.
	w.Tick(1)
	if h := p.Health(); h < 200 {
		t.Fatalf("health %v after one tick, want no bite yet", h)
	}
	w.Tick(1)
	if h := p.Health(); h < 194 || h > 195 {
		t.Fatalf("health %v after the bite, want 194 (6, armour ignored)", h)
	}
	if n := len(fangs(w)); n != 1 {
		t.Fatalf("%d fangs before their lifetime ends", n)
	}
	w.Tick(20)
	if n := len(fangs(w)); n != 0 {
		t.Errorf("%d fangs left after 22 ticks", n)
	}
}

// TestFangAllies: the fangs spare their evoker, its vexes and illagers
// (EvocationIllager::isAlliedTo) but bite other mobs and other vexes; fangs
// with no evoker bite illagers too.
func TestFangAllies(t *testing.T) {
	w := arena(t, 12)
	e := w.Spawn("minecraft:evocation_illager", mgl64.Vec3{10.5, 1, 10.5})
	own := w.Spawn("minecraft:vex", mgl64.Vec3{-6.5, 1, -6.5}, mob.SpawnOptions{Configure: func(m *mob.Mob) {
		m.UpdateMeta(func(meta *mob.Meta) { meta.Owner = e.H() })
	}})
	stray := w.Spawn("minecraft:vex", mgl64.Vec3{-6.5, 1, 6.5})
	vind := w.Spawn("minecraft:vindicator", mgl64.Vec3{6.5, 1, -6.5})
	zomb := w.Spawn("minecraft:zombie", mgl64.Vec3{0.5, 1, 6.5})
	vind2 := w.Spawn("minecraft:vindicator", mgl64.Vec3{0.5, 1, -6.5})
	refs := []*mobtest.Ref{e, own, stray, vind, zomb, vind2}
	health := func() []float64 {
		var hs []float64
		w.Do(func(tx *world.Tx) {
			for _, r := range refs {
				m, _ := r.Mob(tx)
				hs = append(hs, m.Health())
			}
		})
		return hs
	}
	w.Tick(1)
	before := health()
	w.Do(func(tx *world.Tx) {
		ev, _ := e.Mob(tx)
		for i, r := range refs {
			m, _ := r.Mob(tx)
			m.SetVelocity(mgl64.Vec3{})
			var owner world.Entity = ev
			if i == 5 {
				owner = nil
			}
			misc.SpawnFang(tx, m.Position(), owner, 0)
		}
	})
	w.Tick(2)
	after := health()
	want := []bool{false, false, true, false, true, true}
	names := []string{"evoker", "its vex", "other vex", "vindicator", "zombie", "vindicator (ownerless fangs)"}
	for i := range refs {
		if bit := after[i] < before[i]; bit != want[i] {
			t.Errorf("%v bitten %v (%v -> %v), want %v", names[i], bit, before[i], after[i], want[i])
		}
	}
}

// fangSpell is the evoker's close-range spell from its 1.26 entity file.
const fangSpell = `{"summon_choices": [{"cast_duration": 2, "particle_color": "#FF664D59", "cooldown_time": 5, "max_activation_range": 3, "min_activation_range": 0, "sequence": [{"base_delay": 1, "shape": "circle", "num_entities_spawned": 5, "delay_per_summon": 0, "entity_lifespan": 1.1, "entity_type": "minecraft:evocation_fang", "size": 1.5, "sound_event": "prepare.attack", "target": "self"}, {"base_delay": 0.15, "target": "self", "delay_per_summon": 0, "num_entities_spawned": 8, "shape": "circle", "entity_lifespan": 1.1, "entity_type": "minecraft:evocation_fang", "size": 2.5}], "start_sound_event": "cast.spell", "weight": 3}]}`

// TestSummonStages: the fang circles come one stage after the other
// (SummonActorGoal::tick): the 5 inner fangs a second into the cast, the 8
// outer ones 3 ticks after them; the cast shows the casting flag and the
// spell colour.
func TestSummonStages(t *testing.T) {
	w := arena(t, 12)
	p := w.AddPlayer("p", mgl64.Vec3{2.5, 1, 0.5})
	p.SetMaxHealth(200)
	e := w.Spawn("minecraft:evocation_illager", mgl64.Vec3{0.5, 1, 0.5})
	swap(w, e, "summon", goal(t, "minecraft:behavior.summon_entity", fangSpell), true)
	setTarget(w, e, p)
	var counts []int
	for range 30 {
		w.Tick(1)
		counts = append(counts, len(fangs(w)))
		if len(counts) == 2 {
			e.With(func(m *mob.Mob) {
				if !m.ActorFlag(protocol.EntityDataFlagCasting) {
					t.Error("not casting")
				}
				if v, _ := m.ActorData(protocol.EntityDataKeyDataSpellCastingColor); v != int32(-10072743) {
					t.Errorf("spell colour %v, want #FF664D59", v)
				}
			})
		}
	}
	first := -1
	for i, n := range counts {
		if n > 0 {
			first = i
			break
		}
	}
	if first < 0 {
		t.Fatalf("no fangs: %v", counts)
	}
	if first != 20 || counts[first] != 5 {
		t.Errorf("first fangs at tick %d (%d of them), want 5 at tick 20: %v", first, counts[first], counts)
	}
	if first+3 < len(counts) && (counts[first+2] != 5 || counts[first+3] != 13) {
		t.Errorf("outer ring not 3 ticks after the inner one: %v", counts)
	}
}

// lineSpell is the evoker's long-range spell.
const lineSpell = `{"summon_choices": [{"cast_duration": 2, "particle_color": "#FF664D59", "cooldown_time": 5, "min_activation_range": 3, "sequence": [{"base_delay": 1, "target": "self", "delay_per_summon": 0.05, "num_entities_spawned": 16, "shape": "line", "entity_lifespan": 1.1, "entity_type": "minecraft:evocation_fang", "size": 20}], "start_sound_event": "cast.spell", "weight": 3}]}`

// TestSummonLineAim: the fang line runs one fang a tick towards where the
// target stood when the cast began (SummonActorGoal::start keeps it), not
// where it went since.
func TestSummonLineAim(t *testing.T) {
	w := arena(t, 22)
	p := w.AddPlayer("p", mgl64.Vec3{8.5, 1, 0.5})
	p.SetMaxHealth(1000)
	e := w.Spawn("minecraft:evocation_illager", mgl64.Vec3{0.5, 1, 0.5})
	swap(w, e, "summon", goal(t, "minecraft:behavior.summon_entity", lineSpell), true)
	setTarget(w, e, p)
	w.Tick(2)
	p.Teleport(mgl64.Vec3{0.5, 1, 8.5})
	w.Tick(19)
	if n := len(fangs(w)); n != 1 {
		t.Errorf("%d fangs on the first tick of the line, want 1", n)
	}
	w.Tick(15)
	fs := fangs(w)
	if len(fs) != 16 {
		t.Fatalf("%d fangs, want 16", len(fs))
	}
	for _, f := range fs {
		if math.Abs(f[2]-0) > 1e-9 || f[0] < 1 {
			t.Errorf("fang at %v off the line towards +x", f)
		}
	}
}

// TestSummonSharedCooldown: two spells share the goal's cooldown: after a
// cast nothing is cast for the cooldown_time of the spell cast.
func TestSummonSharedCooldown(t *testing.T) {
	w := arena(t, 12)
	p := w.AddPlayer("p", mgl64.Vec3{2.5, 1, 0.5})
	p.SetMaxHealth(1000)
	e := w.Spawn("minecraft:evocation_illager", mgl64.Vec3{0.5, 1, 0.5})
	raw := `{"summon_choices": [
		{"cast_duration": 0.5, "cooldown_time": 3, "sequence": [{"base_delay": 0, "num_entities_spawned": 1, "entity_type": "minecraft:evocation_fang", "size": 1}]},
		{"cast_duration": 0.5, "cooldown_time": 3, "sequence": [{"base_delay": 0, "num_entities_spawned": 2, "entity_type": "minecraft:evocation_fang", "size": 1}]}]}`
	swap(w, e, "summon", goal(t, "minecraft:behavior.summon_entity", raw), true)
	setTarget(w, e, p)
	var starts []int
	casting := false
	for i := range 160 {
		w.Tick(1)
		e.With(func(m *mob.Mob) {
			now := m.ActorFlag(protocol.EntityDataFlagCasting)
			if now && !casting {
				starts = append(starts, i)
			}
			casting = now
		})
	}
	if len(starts) < 2 {
		t.Fatalf("casts started at %v, want at least 2", starts)
	}
	for i := 1; i < len(starts); i++ {
		if d := starts[i] - starts[i-1]; d < 10+60 {
			t.Errorf("casts %d ticks apart (%v), want the 10 tick cast + 60 tick cooldown", d, starts)
		}
	}
}

// TestSummonCap: a spell whose summon_cap is reached is not cast at all
// (SummonActorGoal::_selectBestSpell).
func TestSummonCap(t *testing.T) {
	w := arena(t, 12)
	p := w.AddPlayer("p", mgl64.Vec3{8.5, 1, 0.5})
	e := w.Spawn("minecraft:evocation_illager", mgl64.Vec3{0.5, 1, 0.5})
	for i := range 2 {
		w.Spawn("minecraft:vex", mgl64.Vec3{-3.5, 1, float64(i)*2 - 1})
	}
	raw := `{"summon_choices": [{"cast_duration": 1, "cooldown_time": 0, "sequence": [{"base_delay": 0.5, "entity_type": "minecraft:vex", "shape": "circle", "num_entities_spawned": 3, "summon_cap_radius": 16, "size": 1, "summon_cap": 2}]}]}`
	swap(w, e, "summon", goal(t, "minecraft:behavior.summon_entity", raw), true)
	setTarget(w, e, p)
	if _, ok := flagSeen(w, e, 40, protocol.EntityDataFlagCasting); ok {
		t.Errorf("cast with the summon cap reached")
	}
	if n := len(w.Mobs("minecraft:vex")); n != 2 {
		t.Errorf("%d vexes, want the 2 there were", n)
	}
}
