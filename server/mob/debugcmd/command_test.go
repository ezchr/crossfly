//go:build crossfly_full

package debugcmd_test

import (
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/debugcmd"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	_ "github.com/df-mc/dragonfly/server/mob/vanilla" // whatever mob packages are linked
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// source captures command output for a session-less player.
type source struct {
	*player.Player
	out []string
}

func (s *source) SendCommandOutput(o *cmd.Output) {
	for _, m := range o.Messages() {
		s.out = append(s.out, m.String())
	}
	for _, e := range o.Errors() {
		s.out = append(s.out, "error: "+e.Error())
	}
}

func run(t *testing.T, w *mobtest.World, p *mobtest.Player, c cmd.Command, args string) string {
	t.Helper()
	var s *source
	w.Do(func(tx *world.Tx) {
		e, ok := p.Entity(tx)
		if !ok {
			t.Fatal("player gone")
		}
		s = &source{Player: e.(*player.Player)}
		c.Execute(args, s, tx)
	})
	return strings.Join(s.out, "\n")
}

func TestMobDebug(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 12, block.Stone{}) })
	// The player looks north (yaw 180) at the zombie; the cow is behind it.
	p := w.AddPlayer("admin", mgl64.Vec3{0.5, 1, 0.5})
	p.With(func(pl *player.Player) { pl.Move(mgl64.Vec3{}, 180, 0) })
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{0.5, 1, -6.5})
	w.Spawn("minecraft:cow", mgl64.Vec3{0.5, 1, 6.5})
	w.Tick(2)

	w.Do(func(tx *world.Tx) {
		e, _ := p.Entity(tx)
		pl := e.(*player.Player)
		m, ok := debugcmd.LookedAt(tx, pl.Position().Add(mgl64.Vec3{0, pl.EyeHeight()}), pl.Rotation().Vec3(), debugcmd.Reach)
		if !ok || m.H() != z.H() {
			t.Fatalf("LookedAt = %v %v, want the zombie (rotation %v)", m, ok, pl.Rotation())
		}
	})

	c := debugcmd.Command(nil)
	out := run(t, w, p, c, "")
	if !strings.Contains(out, "zombie at") || !strings.Contains(out, "goals (") || !strings.Contains(out, "components (") {
		t.Fatalf("summary:\n%v", out)
	}
	if out := run(t, w, p, c, "goals"); !strings.Contains(out, "zombie: goals (") {
		t.Fatalf("goals:\n%v", out)
	}
	if out := run(t, w, p, c, "groups"); !strings.Contains(out, "components (") {
		t.Fatalf("groups:\n%v", out)
	}
	if out := run(t, w, p, c, "path 0"); !strings.Contains(out, "path:") {
		t.Fatalf("path:\n%v", out)
	}
	out = run(t, w, p, c, "unsupported")
	if !strings.Contains(out, "unsupported components seen") {
		t.Fatalf("unsupported:\n%v", out)
	}
	if len(mob.RegisteredGoals()) == 0 && !strings.Contains(out, "behavior.") {
		t.Fatalf("with no goals linked the zombie's goals should be listed as unsupported:\n%v", out)
	}
	t.Logf("summary:\n%v", run(t, w, p, c, ""))

	denied := debugcmd.Command(func(cmd.Source) bool { return false })
	if out := run(t, w, p, denied, "goals"); !strings.Contains(out, "error") {
		t.Fatalf("denied source ran the command:\n%v", out)
	}
}

func TestDescribeGoals(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 4, block.Stone{}) })
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{0.5, 1, 0.5})
	w.Tick(1)
	z.With(func(m *mob.Mob) {
		i := debugcmd.Describe(m)
		if i.Identifier != "minecraft:zombie" || len(i.Components) == 0 {
			t.Fatalf("describe: %+v", i)
		}
		if len(i.Goals) != m.Goals().Len() {
			t.Fatalf("%d goals described, selector has %d", len(i.Goals), m.Goals().Len())
		}
		for n := 1; n < len(i.Goals); n++ {
			if i.Goals[n].Priority < i.Goals[n-1].Priority {
				t.Fatalf("goals not sorted by priority: %+v", i.Goals)
			}
		}
		for _, u := range i.Unsupported {
			if mob.Supported(u) {
				t.Fatalf("%v listed unsupported but has a factory", u)
			}
		}
	})
}
