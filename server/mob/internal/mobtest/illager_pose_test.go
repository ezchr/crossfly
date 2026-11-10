//go:build crossfly_full

package mobtest_test

import (
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// swingViewer records, for every arm swing a viewer is shown, the pose state
// the mob had at that moment.
type swingViewer struct {
	world.NopViewer
	mu     sync.Mutex
	swings []swingPose
}

type swingPose struct{ angry, aggressive, target bool }

func (v *swingViewer) ViewEntityAction(e world.Entity, a world.EntityAction) {
	if _, ok := a.(entity.SwingArmAction); !ok {
		return
	}
	m, ok := e.(*mob.Mob)
	if !ok {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.swings = append(v.swings, swingPose{angry: m.Angry(), aggressive: m.Aggressive(), target: m.TargetEntity() != nil})
}

func (v *swingViewer) first() (swingPose, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.swings) == 0 {
		return swingPose{}, false
	}
	return v.swings[0], true
}

// TestIllagerAttackPose: a vindicator or piglin brute swinging at a player
// has its target (Bedrock actor data TARGET: variable.has_target in the
// vindicator and piglin attack controllers) and is angry from
// on_target_acquired (query.is_angry shows the vindicator arms and axe) and
// so aggressive for Java (Mob flag 0x04: IllagerArmPose.ATTACKING,
// PiglinArmPose.ATTACKING_WITH_MELEE_WEAPON). A pillager fights in melee
// only underwater (melee_mode), through the same melee_box_attack goal.
func TestIllagerAttackPose(t *testing.T) {
	needs(t, true, true, "minecraft:behavior.melee_box_attack")
	for _, c := range []struct {
		id   string
		held item.Stack
	}{
		{"minecraft:vindicator", item.NewStack(item.Axe{Tier: item.ToolTierIron}, 1)},
		{"minecraft:piglin_brute", item.NewStack(item.Axe{Tier: item.ToolTierGold}, 1)},
	} {
		t.Run(c.id, func(t *testing.T) {
			w := mobtest.New(t, mobtest.Options{})
			w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 12, block.Stone{}) })
			r := w.Spawn(c.id, mgl64.Vec3{0.5, 1, 0.5})
			r.With(func(m *mob.Mob) {
				_, off := m.HeldItems()
				m.SetHeldItems(c.held, off)
			})
			p := w.AddPlayer("Steve", mgl64.Vec3{3.5, 1, 0.5})
			p.SetMaxHealth(1000)
			v := &swingViewer{}
			var l *world.Loader
			w.Do(func(tx *world.Tx) {
				l = world.NewLoader(2, tx.World(), v)
				l.Move(tx, mgl64.Vec3{0.5, 1, 0.5})
				l.Load(tx, 25)
			})
			defer w.Do(func(tx *world.Tx) { l.Close(tx) })
			if n, ok := w.TickUntil(400, func(*world.Tx) bool { _, ok := v.first(); return ok }); !ok {
				t.Fatalf("no swing in %d ticks:\n%v", n, w.Dump(r))
			}
			s, _ := v.first()
			if !s.target {
				t.Errorf("swung without a target (no Bedrock has_target)")
			}
			if !s.aggressive && !s.angry {
				t.Errorf("swung neither aggressive nor angry (Java arms not raised)")
			}
			if !s.angry {
				t.Errorf("swung while not angry")
			}
		})
	}
}
