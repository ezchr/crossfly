//go:build crossfly_full

package nav

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

// NodeType is the BDS NodeType enum (LeviLamina mc/enums/NodeType.h). Negative values and Blocked
// are not walkable.
type NodeType int32

const (
	NodeStairs     NodeType = -7
	NodeHoney      NodeType = -6
	NodeUnwalkable NodeType = -5
	NodeHot        NodeType = -4
	NodeFence      NodeType = -3
	NodeLava       NodeType = -2
	NodeWater      NodeType = -1
	NodeBlocked    NodeType = 0
	NodeBreakable  NodeType = 1
	NodeOpen       NodeType = 2
	NodeWalkable   NodeType = 3
	NodeClimbable  NodeType = 4
	NodeSwimmable  NodeType = 5
	NodeBreachable NodeType = 6
	NodeFlyable    NodeType = 7
	NodeDoor       NodeType = 8
	// nodeNone is OpenTypeCount, returned where BDS means "no answer".
	nodeNone NodeType = 9
)

func (t NodeType) String() string {
	switch t {
	case NodeStairs:
		return "stairs"
	case NodeHoney:
		return "honey"
	case NodeUnwalkable:
		return "unwalkable"
	case NodeHot:
		return "hot"
	case NodeFence:
		return "fence"
	case NodeLava:
		return "lava"
	case NodeWater:
		return "water"
	case NodeBlocked:
		return "blocked"
	case NodeBreakable:
		return "breakable"
	case NodeOpen:
		return "open"
	case NodeWalkable:
		return "walkable"
	case NodeClimbable:
		return "climbable"
	case NodeSwimmable:
		return "swimmable"
	case NodeBreachable:
		return "breachable"
	case NodeFlyable:
		return "flyable"
	case NodeDoor:
		return "door"
	}
	return "none"
}

// PathNode is one waypoint: the minimum corner block of the mob's footprint and its node type.
type PathNode struct {
	Pos  cube.Pos
	Type NodeType
}

// Path is a found path (BDS Path). Index is the next node to walk to.
type Path struct {
	nodes    []PathNode
	index    int
	complete bool
}

// NewPath builds a path from nodes, for tests and custom goals.
func NewPath(nodes []PathNode, complete bool) *Path {
	return &Path{nodes: append([]PathNode(nil), nodes...), complete: complete}
}

// Len returns the number of nodes.
func (p *Path) Len() int { return len(p.nodes) }

// Nodes returns the nodes. The slice must not be changed.
func (p *Path) Nodes() []PathNode { return p.nodes }

// Index returns the index of the node the mob is walking to.
func (p *Path) Index() int { return p.index }

// SetIndex sets the current node index.
func (p *Path) SetIndex(i int) { p.index = i }

// Next advances to the next node.
func (p *Path) Next() { p.index++ }

// Done reports whether all nodes were reached (Path::isDone).
func (p *Path) Done() bool { return p.index >= len(p.nodes) }

// Valid reports whether the path has any nodes (Path::isValid).
func (p *Path) Valid() bool { return len(p.nodes) != 0 }

// Complete reports whether the path ends at the goal (PathCompletionType 2) rather than at the
// node closest to it (partial path, type 1).
func (p *Path) Complete() bool { return p.complete }

func (p *Path) clamp(i int) int {
	if i >= len(p.nodes) {
		i = len(p.nodes) - 1
	}
	if i < 0 {
		i = 0
	}
	return i
}

// NodePos returns the block of node i (clamped, as Path::getNodePos).
func (p *Path) NodePos(i int) cube.Pos {
	if len(p.nodes) == 0 {
		return cube.Pos{}
	}
	return p.nodes[p.clamp(i)].Pos
}

// NodeType returns the type of node i (clamped).
func (p *Path) NodeType(i int) NodeType {
	if len(p.nodes) == 0 {
		return NodeBlocked
	}
	return p.nodes[p.clamp(i)].Type
}

// Pos returns the world position a mob of the given width walks to for node i (Path::getPos):
// the node corner plus int(width+1)/2 on x and z, plus 0.5 up for flyable nodes.
func (p *Path) Pos(width float64, i int) mgl64.Vec3 {
	if len(p.nodes) == 0 {
		return mgl64.Vec3{}
	}
	n := p.nodes[p.clamp(i)]
	off := float64(int(width+1)) * 0.5
	v := mgl64.Vec3{float64(n.Pos[0]) + off, float64(n.Pos[1]), float64(n.Pos[2]) + off}
	if n.Type == NodeFlyable {
		v[1] += 0.5
	}
	return v
}

// CurrentPos is Pos(width, Index()).
func (p *Path) CurrentPos(width float64) mgl64.Vec3 { return p.Pos(width, p.index) }

// EndPos returns the last node's corner as a vector (Path::getEndPos).
func (p *Path) EndPos() mgl64.Vec3 {
	if len(p.nodes) == 0 {
		return mgl64.Vec3{}
	}
	return p.nodes[len(p.nodes)-1].Pos.Vec3()
}

// LastPos returns the last node's block.
func (p *Path) LastPos() cube.Pos {
	if len(p.nodes) == 0 {
		return cube.Pos{}
	}
	return p.nodes[len(p.nodes)-1].Pos
}

// EndsInXZ reports whether the last node is in the same column as v.
func (p *Path) EndsInXZ(v mgl64.Vec3) bool {
	if len(p.nodes) == 0 {
		return false
	}
	b, l := cube.PosFromVec3(v), p.nodes[len(p.nodes)-1].Pos
	return b[0] == l[0] && b[2] == l[2]
}

// SameAs reports whether o has the same node positions (Path::sameAs).
func (p *Path) SameAs(o *Path) bool {
	if o == nil || len(o.nodes) != len(p.nodes) {
		return false
	}
	for i := range p.nodes {
		if p.nodes[i].Pos != o.nodes[i].Pos {
			return false
		}
	}
	return true
}

// SetSize truncates the path to n nodes (Path::setSize; growing is not supported).
func (p *Path) SetSize(n int) {
	if n < len(p.nodes) {
		p.nodes = p.nodes[:n]
	}
}

// Copy returns a deep copy.
func (p *Path) Copy() *Path {
	return &Path{nodes: append([]PathNode(nil), p.nodes...), index: p.index, complete: p.complete}
}
