//go:build crossfly_full

// Package kit holds the helpers the goals of server/mob/goal/misc and its
// sub-packages share: JSON values, triggers, entity and block searches,
// movement shortcuts, loot drops and client actions. It registers nothing.
package kit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/mob/component/core"
	"github.com/df-mc/dragonfly/server/mob/component/misc"
	goalcore "github.com/df-mc/dragonfly/server/mob/goal/core"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// MaxHeadXRot is Mob::getMaxHeadXRot, the pitch speed goals pass to the look
// control; HeadYawSpeed is the usual 30 degrees a tick.
const (
	MaxHeadXRot  = 40
	HeadYawSpeed = 30
)

// Ticks converts seconds to ticks (rounded).
func Ticks(seconds float64) int { return sense.Ticks(seconds) }

// Or returns *p, or def when p is nil.
func Or[T any](p *T, def T) T {
	if p != nil {
		return *p
	}
	return def
}

// Range is a min/max range (sense.Range).
type Range = sense.Range

// ParseRange reads a number, [min, max] or {min, max} (sense.ParseRange).
func ParseRange(raw json.RawMessage, def Range) (Range, error) { return sense.ParseRange(raw, def) }

// Trigger is a BDS ActorDefinitionTrigger (sense.Trigger).
type Trigger = sense.Trigger

// ParseTrigger reads one trigger (an object or an event name).
func ParseTrigger(raw json.RawMessage) (Trigger, error) { return sense.ParseTrigger(raw) }

// ParseTriggers reads one trigger or a list of them.
func ParseTriggers(raw json.RawMessage) ([]Trigger, error) { return sense.ParseTriggers(raw) }

// FireAll fires every trigger whose filters pass.
func FireAll(m *mob.Mob, ts []Trigger, ctx *mob.FilterContext) {
	for _, t := range ts {
		t.Run(m, ctx)
	}
}

// Decode unmarshals a goal's JSON into v, prefixing errors with the key.
func Decode(spec mob.GoalSpec, v any) error {
	if err := spec.Decode(v); err != nil {
		return fmt.Errorf("%v: %w", spec.Key, err)
	}
	return nil
}

// IsNull reports whether raw is absent or null.
func IsNull(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) == 0 || bytes.Equal(t, []byte("null"))
}

// Filter compiles filter JSON; absent filters pass.
func Filter(raw json.RawMessage) (mob.Filter, error) { return mob.CompileFilter(raw) }

// OtherCtx is m's filter context with e as "other" (and "player" when e is
// one, and m's target as "target").
func OtherCtx(m *mob.Mob, e world.Entity) *mob.FilterContext {
	ctx := m.Ctx()
	ctx.Other = e
	if t, ok := m.Target(); ok {
		ctx.Target = t
	}
	if _, ok := e.(*player.Player); ok {
		ctx.Player = e
	}
	return ctx
}

// Alive reports whether e is in the world and not dead.
func Alive(e world.Entity) bool { return sense.Alive(e) }

// IsPlayer reports whether e is a player.
func IsPlayer(e world.Entity) bool { return sense.IsPlayer(e) }

// VisiblePlayer reports whether e is a player in a game mode mobs notice
// (not spectator).
func VisiblePlayer(e world.Entity) bool {
	p, ok := e.(*player.Player)
	return ok && p.GameMode().Visible()
}

// Creative reports whether e is a player that takes no damage.
func Creative(e world.Entity) bool { return sense.Invulnerable(e) }

// DistSq is the squared distance between two positions.
func DistSq(a, b mgl64.Vec3) float64 { return a.Sub(b).LenSqr() }

// HDistSq is the squared horizontal distance.
func HDistSq(a, b mgl64.Vec3) float64 { return sense.HorizontalDistSq(a, b) }

// Width, BBox, EyePos, Center: entity shape helpers (sense).
func Width(e world.Entity) float64           { return sense.Width(e) }
func BBox(e world.Entity) cube.BBox          { return sense.BBox(e) }
func EyePos(e world.Entity) mgl64.Vec3       { return sense.EyePos(e) }
func Center(e world.Entity) mgl64.Vec3       { return sense.Center(e) }
func CanSee(m *mob.Mob, e world.Entity) bool { return sense.CanSee(m, e) }

// YawTowards is the yaw facing from a to b.
func YawTowards(a, b mgl64.Vec3) float64 { return sense.YawTowards(a, b) }

// Around returns the entities other than m in m's box grown by xz and y,
// nearest first (BDS fetchNearbyActorsSorted).
func Around(m *mob.Mob, xz, y float64) []world.Entity {
	b := m.BBox()
	box := cube.Box(b.Min()[0]-xz, b.Min()[1]-y, b.Min()[2]-xz, b.Max()[0]+xz, b.Max()[1]+y, b.Max()[2]+xz)
	pos := m.Position()
	// The distances are worked out once per entity, not per comparison.
	near := aroundBufs.Get().(*[]nearby)
	defer func() {
		clear(*near)
		*near = (*near)[:0]
		aroundBufs.Put(near)
	}()
	for e := range m.Tx().EntitiesWithin(box) {
		if e.H() != m.H() {
			*near = append(*near, nearby{e: e, dist: DistSq(e.Position(), pos), i: len(*near)})
		}
	}
	if len(*near) == 0 {
		return nil
	}
	slices.SortFunc(*near, func(a, b nearby) int {
		if c := cmpFloat(a.dist, b.dist); c != 0 {
			return c
		}
		return a.i - b.i
	})
	out := make([]world.Entity, len(*near))
	for i, n := range *near {
		out[i] = n.e
	}
	return out
}

// nearby is an entity Around found, with its squared distance and the order
// it was found in (ties keep it, as a stable sort).
type nearby struct {
	e    world.Entity
	dist float64
	i    int
}

var aroundBufs = sync.Pool{New: func() any { return new([]nearby) }}

// Nearest returns the nearest living entity within radius (a sphere,
// searched in a box radius x height) for which ok is true.
func Nearest(m *mob.Mob, radius, height float64, ok func(e world.Entity) bool) (world.Entity, bool) {
	r2 := radius * radius
	for _, e := range Around(m, radius, height) {
		if !Alive(e) || DistSq(e.Position(), m.Position()) > r2 {
			continue
		}
		if ok == nil || ok(e) {
			return e, true
		}
	}
	return nil, false
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// Mob opens e as a mob.
func Mob(e world.Entity) (*mob.Mob, bool) { return mob.Of(e) }

// MoveTo paths m to pos at speed (a multiplier of the movement attribute).
func MoveTo(m *mob.Mob, pos mgl64.Vec3, speed float64) bool {
	return m.Navigation().MoveTo(m, pos, speed)
}

// MoveToBlock paths m to the centre of the top of block p's space.
func MoveToBlock(m *mob.Mob, p cube.Pos, speed float64) bool {
	return MoveTo(m, mgl64.Vec3{float64(p[0]) + 0.5, float64(p[1]), float64(p[2]) + 0.5}, speed)
}

// LookAt turns m's head towards pos.
func LookAt(m *mob.Mob, pos mgl64.Vec3) { m.LookControl().SetLookAt(pos, HeadYawSpeed, MaxHeadXRot) }

// LookAtEntity turns m's head towards e's eyes.
func LookAtEntity(m *mob.Mob, e world.Entity) {
	m.LookControl().SetLookAtEntity(e, HeadYawSpeed, MaxHeadXRot)
}

// Stop stops m's path.
func Stop(m *mob.Mob) { m.Navigation().Stop(m) }

// RandomPos is goal/core.RandomPos (BDS RandomPos::generateRandomPos, 10
// tries, best walk target value): a random standable position within xz/y
// blocks.
func RandomPos(m *mob.Mob, xz, y int) (mgl64.Vec3, bool) {
	return goalcore.RandomPos(m, xz, y, 0, nil, 10)
}

// RandomPosAway is RandomPos pointing away from from (getPosAvoid).
func RandomPosAway(m *mob.Mob, xz, y int, from mgl64.Vec3) (mgl64.Vec3, bool) {
	away := m.Position().Sub(from)
	return goalcore.RandomPos(m, xz, y, 0, &away, 10)
}

// Ground finds the first free space at or above a solid block within dy of
// p (the position a walker stands in).
func Ground(tx *world.Tx, p cube.Pos, dy int) (cube.Pos, bool) {
	for i := 0; i <= 2*dy; i++ {
		q := p.Add(cube.Pos{0, dy - i, 0})
		if q[1] <= tx.Range()[0] || q[1] >= tx.Range()[1] {
			continue
		}
		if Solid(tx, q.Side(cube.FaceDown)) && !Solid(tx, q) && !Solid(tx, q.Side(cube.FaceUp)) {
			return q, true
		}
	}
	return cube.Pos{}, false
}

// Solid reports whether the block at p has a full solid top (mobs stand on
// it).
func Solid(tx *world.Tx, p cube.Pos) bool {
	b := tx.Block(p)
	return b.Model().FaceSolid(p, cube.FaceUp, tx)
}

// BlockName returns the normalised identifier of a block.
func BlockName(b world.Block) string {
	n, _ := b.EncodeBlock()
	return misc.NormaliseBlockName(n)
}

// BlockList is a list of block names from JSON: strings, or objects with a
// "name" (BDS BlockDescriptor).
type BlockList []string

// ParseBlockList reads a block list.
func ParseBlockList(raw json.RawMessage) (BlockList, error) {
	if IsNull(raw) {
		return nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		var one json.RawMessage = raw
		items = []json.RawMessage{one}
	}
	var out BlockList
	for _, it := range items {
		var s string
		if json.Unmarshal(it, &s) == nil {
			out = append(out, misc.NormaliseBlockName(s))
			continue
		}
		var o struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(it, &o); err != nil {
			return nil, err
		}
		if o.Name != "" {
			out = append(out, misc.NormaliseBlockName(o.Name))
		}
	}
	return out, nil
}

// Has reports whether the list names b.
func (l BlockList) Has(b world.Block) bool { return l.HasName(BlockName(b)) }

// HasName reports whether the list has the normalised name n.
func (l BlockList) HasName(n string) bool {
	for _, x := range l {
		if x == n || strings.TrimSuffix(x, "_block") == strings.TrimSuffix(n, "_block") {
			return true
		}
	}
	return false
}

// FindBlock searches the blocks within xz horizontally and y vertically of
// m's block (BDS BaseMoveToBlockGoal::findTargetBlock, rings outwards) for
// one ok accepts. method "random" picks a random match, anything else the
// nearest.
func FindBlock(m *mob.Mob, xz, y int, method string, ok func(p cube.Pos, b world.Block) bool) (cube.Pos, bool) {
	tx := m.Tx()
	base := cube.PosFromVec3(m.Position())
	var matches []cube.Pos
	bestD := math.MaxFloat64
	var best cube.Pos
	found := false
	for dy := -y; dy <= y; dy++ {
		for dx := -xz; dx <= xz; dx++ {
			for dz := -xz; dz <= xz; dz++ {
				p := base.Add(cube.Pos{dx, dy, dz})
				if p[1] < tx.Range()[0] || p[1] > tx.Range()[1] {
					continue
				}
				b := tx.Block(p)
				if !ok(p, b) {
					continue
				}
				if method == "random" {
					matches = append(matches, p)
					continue
				}
				d := float64(dx*dx + dy*dy + dz*dz)
				if d < bestD {
					bestD, best, found = d, p, true
				}
			}
		}
	}
	if method == "random" {
		if len(matches) == 0 {
			return cube.Pos{}, false
		}
		return matches[m.Rand().IntN(len(matches))], true
	}
	return best, found
}

// BlockCtx is a filter context with the block as "block".
func BlockCtx(m *mob.Mob, p cube.Pos, b world.Block) *mob.FilterContext {
	ctx := m.Ctx()
	ctx.Block, ctx.BlockPos, ctx.HasBlock = b, p, true
	return ctx
}

// Flag turns an extra Bedrock actor flag on or off (protocol.EntityDataFlag*).
func Flag(m *mob.Mob, flag uint8, v bool) { m.SetActorFlag(flag, v) }

// Sound plays a level sound event of m.
func Sound(m *mob.Mob, event string) { m.PlaySound(event) }

// ActorEvent is an entity action that is a Bedrock actor event
// (gophertunnel packet.ActorEvent*), shown to both clients.
type ActorEvent uint8

// EntityAction ...
func (ActorEvent) EntityAction() {}

// ActorEvent ...
func (a ActorEvent) ActorEvent() uint8 { return uint8(a) }

// Show shows an entity action of m to its viewers.
func Show(m *mob.Mob, a world.EntityAction) {
	for _, v := range m.Tx().Viewers(m.Position()) {
		v.ViewEntityAction(m, a)
	}
}

// Swing shows m swinging its arm.
func Swing(m *mob.Mob) { Show(m, entity.SwingArmAction{}) }

// DropLoot rolls a loot table (path as in entity JSON) and drops the items
// at m.
func DropLoot(m *mob.Mob, table string, yOffset float64) bool {
	if table == "" {
		return false
	}
	t, err := core.LoadLootTable(table)
	if err != nil || t == nil {
		return false
	}
	stacks := t.Roll(&core.LootContext{Mob: m})
	core.DropItems(m, stacks, yOffset)
	return len(stacks) > 0
}

// Hurt hurts e by m with an attack (goal/combat DoHurtTarget is used for
// real melee hits; this is a raw hit with a fixed amount).
func Hurt(m *mob.Mob, e world.Entity, dmg float64) bool {
	l, ok := e.(entity.Living)
	if !ok || l.Dead() {
		return false
	}
	n, res := l.Hurt(dmg, entity.AttackDamageSource{Attacker: m})
	return res.Accepted() || n > 0
}

// Difficulty is the world difficulty (0 peaceful .. 3 hard).
func Difficulty(tx *world.Tx) int { return sense.DifficultyID(tx) }

// IsDay reports whether it is day.
func IsDay(tx *world.Tx) bool { return sense.IsDay(tx) }

// Cooldown is a tick deadline helper.
type Cooldown struct{ until int64 }

// Ready reports whether the deadline passed.
func (c *Cooldown) Ready(m *mob.Mob) bool { return m.Ticks() >= c.until }

// Set starts a cooldown of n ticks.
func (c *Cooldown) Set(m *mob.Mob, n int) { c.until = m.Ticks() + int64(n) }

// RollTicks rolls a range of seconds into ticks.
func RollTicks(m *mob.Mob, r Range) int { return Ticks(r.Roll(m.Rand())) }
