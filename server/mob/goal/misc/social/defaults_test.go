//go:build crossfly_full

package social

import (
	"encoding/json"
	"testing"

	"github.com/df-mc/dragonfly/server/mob"
	"github.com/go-gl/mathgl/mgl64"
)

func build(t *testing.T, f mob.GoalFactory, key, raw string) mob.Goal {
	t.Helper()
	spec, err := mob.ParseGoalSpec(key, json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	g, err := f(spec)
	if err != nil {
		t.Fatalf("%v: %v", key, err)
	}
	return g
}

// TestDefaults checks the BDS/doc defaults each goal falls back to when its
// JSON leaves a field out, and the default control flags.
func TestDefaults(t *testing.T) {
	gi := build(t, newGiveItems(false), "minecraft:behavior.go_and_give_items_to_owner", `{}`).(*giveItems)
	if gi.speed != 1 || gi.reach != 3 || gi.listen != 600 || gi.Flags() != mob.FlagMove|mob.FlagLook {
		t.Errorf("give items defaults: %+v", gi)
	}
	sn := build(t, newStayNearNoteblock, "minecraft:behavior.stay_near_noteblock", `{}`).(*stayNearNoteblock)
	if sn.speed != 1 || sn.start != 16 || sn.stop != 4 || sn.Flags() != mob.FlagMove {
		t.Errorf("stay near noteblock defaults: %+v", sn)
	}
	d := build(t, newDropItemFor, "minecraft:behavior.drop_item_for", `{}`).(*dropItemFor)
	if d.cooldown != 4 || d.chance != 1 || d.offer != 1 || d.teleportMin != 2 || d.lookHeight != 10 || d.teleportOff != (mgl64.Vec3{0, 1, 0}) || d.todMin != 0 || d.todMx != 1 {
		t.Errorf("drop_item_for defaults: %+v", d)
	}
	p := build(t, newPlay, "minecraft:behavior.play", `{}`).(*play)
	if p.speed != 1 || p.chance != 0.005 || p.duration != 1000 || p.area != (mgl64.Vec3{6, 3, 6}) || p.follow != 2 || p.posRange != 16 || p.posH != 3 || p.Flags() != mob.FlagMove {
		t.Errorf("play defaults: %+v", p)
	}
	h := build(t, newHide, "minecraft:behavior.hide", `{}`).(*hide)
	if h.duration != 20 || h.speed != 1 {
		t.Errorf("hide defaults: %+v", h)
	}
	s := build(t, newSniff, "minecraft:behavior.sniff", `{}`).(*sniff)
	if s.duration != 20 || s.radius != 5 || s.suspH != 3 || s.suspV != 3 || s.cooldown.Min != 5 || s.cooldown.Max != 10 || s.Flags() != mob.FlagMove|mob.FlagLook {
		t.Errorf("sniff defaults: %+v", s)
	}
	j := build(t, newJumpAroundTarget, "minecraft:behavior.jump_around_target", `{}`).(*jumpAroundTarget)
	if j.cooldown != 10 || j.hurtCooldown != 2 || j.lastHurt != 40 || j.prepare != 10 || j.maxVel != 1.4 || j.spread != 90 || j.vertSpace != 4 || j.snap != 10 || len(j.angles) != 5 || j.valid.Min != 4 || j.valid.Max != 20 {
		t.Errorf("jump_around_target defaults: %+v", j)
	}
	ma := build(t, newMoveAroundTarget, "minecraft:behavior.move_around_target", `{}`).(*moveAroundTarget)
	if ma.spread != 90 || ma.rng.Min != 4 || ma.rng.Max != 8 || ma.speed != 1 {
		t.Errorf("move_around_target defaults: %+v", ma)
	}
	f := build(t, newFireAtTarget, "minecraft:behavior.fire_at_target", `{}`).(*fireAtTarget)
	if f.fov != 90 || f.maxX != 30 || f.maxY != 30 || f.rng.Max != 16 || f.Flags() != mob.FlagMove|mob.FlagLook {
		t.Errorf("fire_at_target defaults: %+v", f)
	}
	ml := build(t, newMakeLove, "minecraft:behavior.make_love", `{}`)
	rl := build(t, newReceiveLove, "minecraft:behavior.receive_love", `{}`)
	if ml.Flags() != mob.FlagMove|mob.FlagLook || rl.Flags() != mob.FlagMove|mob.FlagLook {
		t.Errorf("love flags: %v %v", ml.Flags(), rl.Flags())
	}
}

// TestJumpVector checks the launch velocity lands where it should under
// gravity 0.08 (no drag).
func TestJumpVector(t *testing.T) {
	from, to := mgl64.Vec3{0, 0, 0}, mgl64.Vec3{6, 0, 0}
	v, ok := jumpVector(from, to, 55, 1.4)
	if !ok {
		t.Fatal("no jump vector")
	}
	pos := from
	for i := 0; i < 200; i++ {
		pos = pos.Add(v)
		v[1] -= 0.08
		if pos[1] < 0 {
			break
		}
	}
	if pos[0] < 5 || pos[0] > 7.5 {
		t.Errorf("lands at x=%.2f, want ~6", pos[0])
	}
	if _, ok := jumpVector(from, mgl64.Vec3{60, 0, 0}, 55, 1.4); ok {
		t.Errorf("60 blocks should exceed max_jump_velocity 1.4")
	}
}
