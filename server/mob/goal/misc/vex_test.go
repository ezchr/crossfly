//go:build crossfly_full

package misc_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestVexHardcodedGoals: every vex gets vex_random_move (8) and
// vex_copy_owner_target (2) though its entity file has neither
// (Vex::initializeComponents).
func TestVexHardcodedGoals(t *testing.T) {
	w := arena(t, 8)
	v := w.Spawn("minecraft:vex", mgl64.Vec3{0.5, 3, 0.5})
	w.Tick(1)
	v.With(func(m *mob.Mob) {
		for key, prio := range map[string]int{"minecraft:behavior.vex_random_move": 8, "minecraft:behavior.vex_copy_owner_target": 2} {
			if _, ok := m.Goals().Goal(key); !ok {
				t.Errorf("no %v", key)
			} else if p, _ := m.Goals().Priority(key); p != prio {
				t.Errorf("%v priority %v, want %v", key, p, prio)
			}
		}
	})
}

// TestVexRandomMove: an idle vex flies about (VexRandomMoveGoal).
func TestVexRandomMove(t *testing.T) {
	w := arena(t, 10)
	v := w.Spawn("minecraft:vex", mgl64.Vec3{0.5, 4, 0.5})
	var start mgl64.Vec3
	v.With(func(m *mob.Mob) {
		for _, k := range m.Goals().Keys() {
			if k != "minecraft:behavior.vex_random_move" {
				m.Goals().Remove(k)
			}
		}
		start = m.Position()
	})
	moved := false
	w.Each(200, func(tx *world.Tx, _ int) bool {
		m, _ := v.Mob(tx)
		moved = m.Position().Sub(start).Len() > 1.5
		return !moved
	})
	if !moved {
		t.Errorf("idle vex never flew about: %v", w.Dump(v))
	}
}

// TestVexCopiesOwnerTarget: a vex takes its evoker's target
// (VexCopyOwnerTargetGoal), even one it cannot see.
func TestVexCopiesOwnerTarget(t *testing.T) {
	w := arena(t, 12)
	e := w.Spawn("minecraft:evocation_illager", mgl64.Vec3{8.5, 1, 8.5})
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{-8.5, 1, 8.5})
	v := w.Spawn("minecraft:vex", mgl64.Vec3{0.5, 2, 0.5}, mob.SpawnOptions{Configure: func(m *mob.Mob) {
		m.UpdateMeta(func(meta *mob.Meta) { meta.Owner = e.H() })
	}})
	swap(w, e, "idle", goal(t, "minecraft:behavior.look_at_target", `{}`), true)
	setTarget(w, e, z)
	if _, ok := w.TickUntil(5, func(tx *world.Tx) bool {
		m, _ := v.Mob(tx)
		return m.TargetHandle() == z.H()
	}); !ok {
		t.Errorf("vex did not copy its owner's target: %v", w.Dump(v))
	}
}

// TestVexKeepsSword: a vex a player kills never drops its sword (its hand
// drop chances are 0, Vex::reloadHardcoded).
func TestVexKeepsSword(t *testing.T) {
	w := arena(t, 12)
	p := w.AddPlayer("p", mgl64.Vec3{0.5, 1, 0.5})
	var vexes []*mobtest.Ref
	for i := range 40 {
		vexes = append(vexes, w.Spawn("minecraft:vex", mgl64.Vec3{float64(i%8) - 3.5, 3, float64(i/8) - 2.5}))
	}
	w.Tick(2)
	armed := 0
	w.Do(func(tx *world.Tx) {
		pl, _ := p.Entity(tx)
		for _, r := range vexes {
			m, ok := r.Mob(tx)
			if !ok {
				continue
			}
			if main, _ := m.HeldItems(); !main.Empty() {
				armed++
			}
			m.Hurt(1000, entity.AttackDamageSource{Attacker: pl})
		}
	})
	if armed == 0 {
		t.Fatal("no vex held a sword")
	}
	w.Tick(2)
	w.Do(func(tx *world.Tx) {
		for e := range tx.Entities() {
			if it, ok := e.(*entity.Ent); ok {
				if b, ok := it.Behaviour().(*entity.ItemBehaviour); ok {
					if _, ok := b.Item().Item().(item.Sword); ok {
						t.Errorf("a vex dropped its sword (%d armed vexes killed)", armed)
						return
					}
				}
			}
		}
	})
}
