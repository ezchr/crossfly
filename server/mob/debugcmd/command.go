//go:build crossfly_full

package debugcmd

import (
	"image/color"
	"math"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

// Reach is how far /mobdebug looks for the mob the player looks at. With
// none in sight it takes the nearest mob within Nearby blocks.
const (
	Reach  = 48.0
	Nearby = 8.0
)

// Command returns /mobdebug. allow decides who may run it (nil: anyone);
// servers should pass their admin check.
//
//	/mobdebug                 summary of the looked-at mob
//	/mobdebug goals           its goals with priorities, running ones marked
//	/mobdebug path [seconds]  its path as particles (default 10 s)
//	/mobdebug groups          component groups, components, properties
//	/mobdebug unsupported     its unimplemented components + server-wide list
//	/mobdebug equipment       its equipment slots with drop chances
//	/mobdebug gear [count]    spawns each mob that has spawn equipment, prints its gear
func Command(allow func(src cmd.Source) bool) cmd.Command {
	g := gate{allow: allow}
	return cmd.New("mobdebug", "Shows the AI state of the mob you look at", nil,
		summary{gate: g}, goals{gate: g}, path{gate: g}, groups{gate: g}, unsupported{gate: g},
		equipment{gate: g}, gear{gate: g})
}

type gate struct{ allow func(src cmd.Source) bool }

// Allow ...
func (g gate) Allow(src cmd.Source) bool { return g.allow == nil || g.allow(src) }

type (
	summary struct{ gate }
	goals   struct {
		gate
		Goals cmd.SubCommand `cmd:"goals"`
	}
	path struct {
		gate
		Path    cmd.SubCommand    `cmd:"path"`
		Seconds cmd.Optional[int] `cmd:"seconds"`
	}
	groups struct {
		gate
		Groups cmd.SubCommand `cmd:"groups"`
	}
	unsupported struct {
		gate
		Unsupported cmd.SubCommand `cmd:"unsupported"`
	}
)

// Run ...
func (summary) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	if m, ok := target(src, o, tx); ok {
		printLines(o, Describe(m).String())
	}
}

// Run ...
func (goals) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	if m, ok := target(src, o, tx); ok {
		i := Describe(m)
		printLines(o, entityName(i.Identifier)+": "+i.GoalsString())
	}
}

// Run ...
func (c path) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	m, ok := target(src, o, tx)
	if !ok {
		return
	}
	i := Describe(m)
	printLines(o, i.PathString())
	secs := min(max(c.Seconds.LoadOr(10), 0), 120)
	ShowPath(tx, m.H(), time.Duration(secs)*time.Second)
}

// Run ...
func (groups) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	if m, ok := target(src, o, tx); ok {
		i := Describe(m)
		printLines(o, entityName(i.Identifier)+": "+i.GroupsString())
	}
}

// Run ...
func (unsupported) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	if tx != nil {
		if m, ok := lookedAt(src, tx); ok {
			i := Describe(m)
			o.Printf("%v: %d unsupported: %v", entityName(i.Identifier), len(i.Unsupported), strings.Join(i.Unsupported, ", "))
		}
	}
	printLines(o, UnsupportedReport(15))
}

func printLines(o *cmd.Output, s string) {
	for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		o.Print(l)
	}
}

// viewer is what /mobdebug needs from the source: a position and a look
// direction (players).
type viewer interface {
	Position() mgl64.Vec3
	Rotation() cube.Rotation
	EyeHeight() float64
}

func target(src cmd.Source, o *cmd.Output, tx *world.Tx) (*mob.Mob, bool) {
	if tx == nil {
		o.Error("/mobdebug must be run in a world")
		return nil, false
	}
	if _, ok := src.(viewer); !ok {
		o.Error("/mobdebug must be run by a player")
		return nil, false
	}
	m, ok := lookedAt(src, tx)
	if !ok {
		o.Errorf("no mob within %.0f blocks of your view", Reach)
	}
	return m, ok
}

func lookedAt(src cmd.Source, tx *world.Tx) (*mob.Mob, bool) {
	v, ok := src.(viewer)
	if !ok {
		return nil, false
	}
	eye := v.Position().Add(mgl64.Vec3{0, v.EyeHeight()})
	return LookedAt(tx, eye, v.Rotation().Vec3(), Reach)
}

// LookedAt returns the mob whose box the ray from eye along dir hits first
// within dist, or else the mob nearest to eye within Nearby blocks. Blocks
// do not stop the ray (it is a debug tool: look through walls).
func LookedAt(tx *world.Tx, eye, dir mgl64.Vec3, dist float64) (*mob.Mob, bool) {
	end := eye.Add(dir.Normalize().Mul(dist))
	search := cube.Box(eye[0], eye[1], eye[2], end[0], end[1], end[2]).Grow(2)
	var (
		best     *mob.Mob
		bestDist = math.MaxFloat64
	)
	for e := range tx.EntitiesWithin(search) {
		m, ok := mob.Of(e)
		if !ok {
			continue
		}
		if r, ok := trace.BBoxIntercept(m.BBox().Grow(0.25), eye, end); ok {
			if d := r.Position().Sub(eye).Len(); d < bestDist {
				best, bestDist = m, d
			}
		}
	}
	if best != nil {
		return best, true
	}
	bestDist = Nearby
	for e := range tx.EntitiesWithin(cube.Box(eye[0], eye[1], eye[2], eye[0], eye[1], eye[2]).Grow(Nearby)) {
		m, ok := mob.Of(e)
		if !ok {
			continue
		}
		if d := m.Pos().Sub(eye).Len(); d < bestDist {
			best, bestDist = m, d
		}
	}
	return best, best != nil
}

var (
	colourDone = color.RGBA{R: 120, G: 120, B: 120, A: 255}
	colourNext = color.RGBA{G: 255, A: 255}
	colourTodo = color.RGBA{R: 40, G: 120, B: 255, A: 255}
	colourEnd  = color.RGBA{R: 255, A: 255}
)

// DrawPath shows m's current path once with dust particles: reached nodes
// grey, the next node green, later ones blue, the end red (orange-red when
// the path is partial), plus a flame at the move control's wanted position.
func DrawPath(tx *world.Tx, m *mob.Mob) {
	p := m.Navigation().Path()
	if p != nil {
		for n := range p.Len() {
			c := colourTodo
			switch {
			case n < p.Index():
				c = colourDone
			case n == p.Index():
				c = colourNext
			case n == p.Len()-1:
				c = colourEnd
				if !p.Complete() {
					c.G = 120
				}
			}
			node := p.Node(n)
			tx.AddParticle(mgl64.Vec3{float64(node[0]) + 0.5, float64(node[1]) + 0.15, float64(node[2]) + 0.5}, particle.Dust{Colour: c})
		}
	}
	if mc := m.MoveControl(); mc.HasWanted() {
		tx.AddParticle(mc.WantedPosition().Add(mgl64.Vec3{0, 0.2}), particle.Flame{})
	}
}

// ShowPath draws the path of the mob behind h now and then every half
// second for d, following path changes, until the mob is gone.
func ShowPath(tx *world.Tx, h *world.EntityHandle, d time.Duration) {
	if m, ok := mob.OfHandle(tx, h); ok {
		DrawPath(tx, m)
	}
	if d <= 0 {
		return
	}
	deadline := time.Now().Add(d)
	var redraw func(tx *world.Tx)
	redraw = func(tx *world.Tx) {
		m, ok := mob.OfHandle(tx, h)
		if !ok || time.Now().After(deadline) {
			return
		}
		DrawPath(tx, m)
		tx.World().DoAfter(500*time.Millisecond, redraw)
	}
	tx.World().DoAfter(500*time.Millisecond, redraw)
}
