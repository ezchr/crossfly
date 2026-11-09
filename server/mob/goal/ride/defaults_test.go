//go:build crossfly_full

package ride

import (
	"encoding/json"
	"testing"

	"github.com/df-mc/dragonfly/server/mob"
)

func goal[T any](t *testing.T, f mob.GoalFactory, raw string) T {
	t.Helper()
	spec, err := mob.ParseGoalSpec("test", json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	g, err := f(spec)
	if err != nil {
		t.Fatal(err)
	}
	v, ok := g.(T)
	if !ok {
		t.Fatalf("factory built %T", g)
	}
	return v
}

// The BDS defaults of the goals (LegacyGoalDefinition::parse and the goal
// definitions' constructors).
func TestGoalDefaults(t *testing.T) {
	f := goal[*followOwner](t, newFollowOwner, `{}`)
	if f.speed != 1 || f.start != 10 || f.stop != 2 || !f.teleport || f.Flags() != mob.FlagMove|mob.FlagLook {
		t.Fatalf("follow_owner %+v", f)
	}
	c := goal[*controlledByPlayer](t, newControlledByPlayer, `{}`)
	if c.fraction != 0.5 || c.limit != 5 || c.mult != 1 {
		t.Fatalf("controlled_by_player %+v", c)
	}
	c = goal[*controlledByPlayer](t, newControlledByPlayer, `{"mount_speed_multiplier":1.45}`)
	if c.mult != 1.45 {
		t.Fatalf("strider multiplier %v", c.mult)
	}
	fm := goal[*findMount](t, newFindMount, `{}`)
	if fm.avoidWater || fm.startDelay != 0 || fm.targetNeeded || fm.mountDist != -1 || fm.maxFailed != 20 {
		t.Fatalf("find_mount %+v", fm)
	}
	b := goal[*beg](t, newBeg, `{}`)
	if b.dist != 8 || b.minT != 2 || b.maxT != 4 || b.Flags() != mob.FlagLook {
		t.Fatalf("beg %+v", b)
	}
	tr := goal[*skeletonHorseTrap](t, newSkeletonHorseTrap, `{"duration":900.0,"within_radius":10.0}`)
	if tr.left != 18000 || tr.radius != 10 {
		t.Fatalf("skeleton_horse_trap %+v", tr)
	}
	tp := goal[*teleportToOwner](t, newTeleportToOwner, `{}`)
	if tp.cooldown != 20 {
		t.Fatalf("teleport_to_owner cooldown %d", tp.cooldown)
	}
	ps := goal[*petSleepWithOwner](t, newPetSleepWithOwner, `{"goal_radius":1.0,"search_height":10,"speed_multiplier":1.2}`)
	if ps.speed != 1.2 || ps.height != 10 || ps.radius != 10 {
		t.Fatalf("pet_sleep_with_owner %+v", ps)
	}
	if g := goal[*ownerHurtBy](t, newOwnerHurtBy(true), `{}`); !g.IsTargetGoal() || !g.hurt {
		t.Fatal("owner_hurt_target is no target goal")
	}
	if g := goal[*playerRideTamed](t, newPlayerRideTamed, `{}`); g.Flags() != mob.FlagMove|mob.FlagLook {
		t.Fatal("player_ride_tamed flags")
	}
}

func TestWrapDegrees(t *testing.T) {
	for in, want := range map[float64]float64{190: -170, -190: 170, 360: 0, 45: 45} {
		if got := wrapDegrees(in); got != want {
			t.Errorf("wrapDegrees(%v) = %v, want %v", in, got, want)
		}
	}
}
