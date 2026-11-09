//go:build crossfly_full

package coretest

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Controls are fake navigation, look and jump controls. The navigation moves
// the mob straight to its destination at Step blocks a tick (no
// pathfinding, no physics) and records what goals asked for.
type Controls struct {
	Nav  *Nav
	Look *Look
	Jump *Jump
}

// Install puts the fakes on m.
func (c *Controls) Install(m *mob.Mob) {
	c.Nav, c.Look, c.Jump = &Nav{Step: 0.25}, &Look{}, &Jump{}
	m.SetNavigation(c.Nav)
	m.SetLookControl(c.Look)
	m.SetJumpControl(c.Jump)
}

// Nav is a fake mob.Navigation.
type Nav struct {
	Step float64
	// Fail makes every MoveTo fail.
	Fail bool

	path     *Path
	target   *world.EntityHandle
	dest     mgl64.Vec3
	Speed    float64
	MoveTos  int
	LastDest mgl64.Vec3
	// LastEntity is the entity of the last MoveToEntity.
	LastEntity *world.EntityHandle
}

// Path is a fake straight path.
type Path struct {
	start, end cube.Pos
	done       bool
}

func (p *Path) Len() int { return 2 }
func (p *Path) Index() int {
	if p.done {
		return 2
	}
	return 1
}
func (p *Path) Node(i int) cube.Pos {
	if i == 0 {
		return p.start
	}
	return p.end
}
func (p *Path) End() cube.Pos  { return p.end }
func (p *Path) Complete() bool { return true }
func (p *Path) Done() bool     { return p.done }

func (n *Nav) Tick(m *mob.Mob) {
	if n.path == nil || n.path.done {
		return
	}
	dest := n.dest
	if n.target != nil {
		if e, ok := n.target.Entity(m.Tx()); ok {
			dest = e.Position()
		}
	}
	d := dest.Sub(m.Position())
	d[1] = 0
	if d.Len() <= n.Step {
		m.SetPosition(mgl64.Vec3{dest[0], m.Position()[1], dest[2]})
		n.path.done = true
		return
	}
	m.SetPosition(m.Position().Add(d.Normalize().Mul(n.Step)))
}

func (n *Nav) CreatePath(m *mob.Mob, pos mgl64.Vec3) mob.Path {
	return &Path{start: cube.PosFromVec3(m.Position()), end: cube.PosFromVec3(pos)}
}
func (n *Nav) CreatePathToEntity(m *mob.Mob, e world.Entity) mob.Path {
	return n.CreatePath(m, e.Position())
}
func (n *Nav) MoveTo(m *mob.Mob, pos mgl64.Vec3, speed float64) bool {
	n.MoveTos++
	n.LastDest = pos
	if n.Fail {
		return false
	}
	n.path, n.dest, n.target, n.Speed = n.CreatePath(m, pos).(*Path), pos, nil, speed
	return true
}
func (n *Nav) MoveToEntity(m *mob.Mob, e world.Entity, speed float64) bool {
	ok := n.MoveTo(m, e.Position(), speed)
	n.LastEntity = e.H()
	if ok {
		n.target = e.H()
	}
	return ok
}
func (n *Nav) MoveAlong(m *mob.Mob, p mob.Path, speed float64) bool {
	n.path, n.Speed = p.(*Path), speed
	return true
}
func (n *Nav) Path() mob.Path {
	if n.path == nil {
		return nil
	}
	return n.path
}
func (n *Nav) Stop(*mob.Mob)          { n.path, n.target = nil, nil }
func (n *Nav) Done() bool             { return n.path == nil || n.path.done }
func (n *Nav) SetSpeed(speed float64) { n.Speed = speed }

// Look is a fake mob.LookControl recording the last look target.
type Look struct {
	At     mgl64.Vec3
	Entity *world.EntityHandle
	Calls  int
}

func (l *Look) Tick(*mob.Mob) {}
func (l *Look) SetLookAt(pos mgl64.Vec3, _, _ float64) {
	l.At, l.Entity = pos, nil
	l.Calls++
}
func (l *Look) SetLookAtEntity(e world.Entity, _, _ float64) {
	l.At, l.Entity = e.Position(), e.H()
	l.Calls++
}
func (l *Look) HasWanted() bool { return l.Calls > 0 }

// Jump is a fake mob.JumpControl counting jumps.
type Jump struct{ Jumps int }

func (j *Jump) Tick(*mob.Mob) {}
func (j *Jump) Jump()         { j.Jumps++ }
func (j *Jump) Jumping() bool { return false }
