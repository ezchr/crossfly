//go:build crossfly_full

package social

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// noteKey keys an allay's remembered note block (mob.Data).
type noteKey struct{}

type noteMemory struct {
	pos  cube.Pos
	tick int64 // the allay's Ticks when it heard the note block
}

// noteHearingRange is the allay's vibration_listener range (BDS
// VibrationListener default 16 blocks).
const noteHearingRange = 16

// noteLoyalTicks is how long an allay ignores other note blocks after it
// heard one: AllayVibrationConfig::onSignalReceive takes a new note block
// only when it heard none yet, the same one again, or the last more than
// 600 ticks ago.
const noteLoyalTicks = 600

// Note blocks call NoteBlockPlayed through block.NoteListener
// (mob.BlockHost), whether a player or redstone plays them.
func init() { mob.RegisterNoteBlockHook(NoteBlockPlayed) }

// NoteBlockPlayed tells the allays within 16 blocks that the note block at
// pos played (BDS: the note_block_play game event reaches the allay's
// vibration listener, AllayVibrationConfig::onSignalReceive, which stores it
// in VibrationDataComponent). Note.playNote calls it for every note.
func NoteBlockPlayed(tx *world.Tx, pos cube.Pos) {
	c := pos.Vec3Centre()
	r := float64(noteHearingRange)
	for e := range tx.EntitiesWithin(cube.Box(c[0]-r, c[1]-r, c[2]-r, c[0]+r, c[1]+r, c[2]+r)) {
		m, ok := mob.Of(e)
		if !ok || m.Dead() || e.Position().Sub(c).Len() > r {
			continue
		}
		if !m.HasComponent("minecraft:behavior.go_and_give_items_to_noteblock") && !m.HasComponent("minecraft:behavior.stay_near_noteblock") {
			continue
		}
		if v, ok := m.Data(noteKey{}); ok {
			if n := v.(noteMemory); n.pos != pos && m.Ticks()-n.tick <= noteLoyalTicks {
				continue
			}
		}
		m.SetData(noteKey{}, noteMemory{pos: pos, tick: m.Ticks()})
	}
}

// LikedNoteBlock returns the note block m heard within listen ticks.
func LikedNoteBlock(m *mob.Mob, listen int) (cube.Pos, bool) {
	v, ok := m.Data(noteKey{})
	if !ok {
		return cube.Pos{}, false
	}
	n := v.(noteMemory)
	if m.Ticks()-n.tick > int64(listen) {
		m.SetData(noteKey{}, nil)
		return cube.Pos{}, false
	}
	if _, ok := m.Tx().Block(n.pos).(block.Note); !ok {
		m.SetData(noteKey{}, nil)
		return cube.Pos{}, false
	}
	return n.pos, true
}

// SetLiked makes e the allay's owner, the player it brings items to (BDS
// sets the owner when a player gives the allay an item through
// minecraft:interact give_item).
func SetLiked(m *mob.Mob, e world.Entity) {
	var h *world.EntityHandle
	if e != nil {
		h = e.H()
	}
	m.UpdateMeta(func(meta *mob.Meta) { meta.Owner = h })
}

// giveItems is the shared part of go_and_give_items_to_owner and
// go_and_give_items_to_noteblock (BDS GoAndGiveItemsToOwnerGoal,
// GoAndGiveItemsToNoteblockGoal): walk to the destination with the held
// item and throw one item at it from within reach.
//
//	run_speed      1.0
//	throw_sound    level sound played on throwing ("item_thrown")
//	on_item_throw  triggers fired on throwing
//	reach          3 blocks (BDS +116, not in JSON)
//	listen_time    30 s a heard note block is remembered (noteblock goal)
//
// Control flags move|look (BDS SetRotation(3)). The allay's one-slot
// inventory is its main hand here.
type giveItems struct {
	mob.GoalBase
	noteblock bool
	speed     float64
	reach     float64
	listen    int
	sound     string
	onThrow   []kit.Trigger

	thrown  bool
	repath  int
	pathTo  mgl64.Vec3
	dest    mgl64.Vec3
	destEnt *world.EntityHandle
}

func newGiveItems(noteblock bool) mob.GoalFactory {
	return func(spec mob.GoalSpec) (mob.Goal, error) {
		var v struct {
			RunSpeed   *float64        `json:"run_speed"`
			ThrowSound string          `json:"throw_sound"`
			OnThrow    json.RawMessage `json:"on_item_throw"`
			ListenTime *float64        `json:"listen_time"`
			Reach      *float64        `json:"reach_mob_distance"`
		}
		if err := kit.Decode(spec, &v); err != nil {
			return nil, err
		}
		g := &giveItems{
			GoalBase:  mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)},
			noteblock: noteblock, speed: kit.Or(v.RunSpeed, 1), reach: kit.Or(v.Reach, 3),
			listen: kit.Ticks(kit.Or(v.ListenTime, 30)), sound: v.ThrowSound,
		}
		var err error
		if g.onThrow, err = kit.ParseTriggers(v.OnThrow); err != nil {
			return nil, err
		}
		return g, nil
	}
}

// within reports whether m is within reach of pos. BDS compares the
// positions; the owner's body centre is accepted too, since a hovering allay
// may stay a few blocks above the feet.
func (g *giveItems) within(m *mob.Mob, pos mgl64.Vec3) bool {
	d := m.Position().Sub(pos).Len()
	if !g.noteblock {
		d = min(d, m.Position().Sub(pos.Add(mgl64.Vec3{0, 0.9, 0})).Len())
	}
	return d < g.reach
}

// target finds where to bring the item.
func (g *giveItems) target(m *mob.Mob) (mgl64.Vec3, world.Entity, bool) {
	if g.noteblock {
		p, ok := LikedNoteBlock(m, g.listen)
		if !ok {
			return mgl64.Vec3{}, nil, false
		}
		return blockCentre(p).Add(mgl64.Vec3{0, 1, 0}), nil, true
	}
	o, ok := entityOf(m, m.Owner())
	if !ok || !kit.Alive(o) {
		return mgl64.Vec3{}, nil, false
	}
	return o.Position(), o, true
}

func (g *giveItems) holding(m *mob.Mob) bool {
	main, _ := m.HeldItems()
	return !main.Empty()
}

// CanUse ...
func (g *giveItems) CanUse(m *mob.Mob) bool {
	if !g.holding(m) {
		return false
	}
	pos, e, ok := g.target(m)
	if !ok {
		return false
	}
	g.dest = pos
	g.destEnt = nil
	if e != nil {
		g.destEnt = e.H()
	}
	if g.within(m, pos) {
		return true
	}
	return m.Navigation().CreatePath(m, pos) != nil
}

// CanContinueToUse ...
func (g *giveItems) CanContinueToUse(m *mob.Mob) bool {
	if g.thrown || !g.holding(m) {
		return false
	}
	pos, _, ok := g.target(m)
	if !ok {
		return false
	}
	g.dest = pos
	return !m.Navigation().Done() || g.within(m, pos)
}

// Start ...
func (g *giveItems) Start(m *mob.Mob) {
	g.thrown, g.repath, g.pathTo = false, 20, g.dest
	kit.MoveTo(m, g.dest, g.speed)
}

// Stop ...
func (g *giveItems) Stop(m *mob.Mob) {
	kit.Stop(m)
	g.thrown = false
}

// Tick ...
func (g *giveItems) Tick(m *mob.Mob) {
	kit.LookAt(m, g.dest)
	if g.within(m, g.dest) {
		g.give(m)
		return
	}
	// Repath when the path ran out, or every 20 ticks once the destination
	// moved more than a block from where the path was made for.
	g.repath--
	if m.Navigation().Done() || (g.repath <= 0 && g.dest.Sub(g.pathTo).Len() > 1) {
		g.repath = 20
		g.pathTo = g.dest
		kit.MoveTo(m, g.dest, g.speed)
	}
}

// give throws one item at the destination (one block above an owner).
func (g *giveItems) give(m *mob.Mob) {
	main, off := m.HeldItems()
	if main.Empty() {
		return
	}
	to := g.dest
	if !g.noteblock {
		to = to.Add(mgl64.Vec3{0, 1, 0})
	}
	throwItem(m, main.Grow(-main.Count()+1), to, time.Second)
	m.SetHeldItems(main.Grow(-1), off)
	g.thrown = true
	kit.Sound(m, g.sound)
	kit.FireAll(m, g.onThrow, nil)
}

// AppendDebugInfo ...
func (g *giveItems) AppendDebugInfo(b *strings.Builder) {
	if g.noteblock {
		b.WriteString("GoAndGiveItemsToNoteblock")
		return
	}
	b.WriteString("GoAndGiveItemsToOwner")
}

// stayNearNoteblock is minecraft:behavior.stay_near_noteblock (BDS
// StayNearNoteblockGoal; Java StayCloseToTarget on the liked note block):
// when the remembered note block is farther than start_distance, the allay
// flies back until it is within stop_distance.
//
//	speed           1.0
//	start_distance  16
//	stop_distance   4
//	listen_time     30 s
//
// Control flag move.
type stayNearNoteblock struct {
	mob.GoalBase
	speed, start, stop float64
	listen             int
	pos                cube.Pos
	repath             int
}

func newStayNearNoteblock(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Speed      *float64 `json:"speed"`
		Start      *float64 `json:"start_distance"`
		Stop       *float64 `json:"stop_distance"`
		ListenTime *float64 `json:"listen_time"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	return &stayNearNoteblock{
		GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove)},
		speed:    kit.Or(v.Speed, 1), start: kit.Or(v.Start, 16), stop: kit.Or(v.Stop, 4),
		listen: kit.Ticks(kit.Or(v.ListenTime, 30)),
	}, nil
}

func (g *stayNearNoteblock) dist(m *mob.Mob) float64 {
	return m.Position().Sub(g.pos.Vec3Centre()).Len()
}

// CanUse ...
func (g *stayNearNoteblock) CanUse(m *mob.Mob) bool {
	p, ok := LikedNoteBlock(m, g.listen)
	if !ok {
		return false
	}
	g.pos = p
	return g.dist(m) > g.start
}

// CanContinueToUse ...
func (g *stayNearNoteblock) CanContinueToUse(m *mob.Mob) bool {
	if _, ok := LikedNoteBlock(m, g.listen); !ok {
		return false
	}
	return g.dist(m) > g.stop
}

// Start ...
func (g *stayNearNoteblock) Start(m *mob.Mob) {
	g.repath = 0
}

// Tick ...
func (g *stayNearNoteblock) Tick(m *mob.Mob) {
	if g.repath--; g.repath <= 0 || m.Navigation().Done() {
		g.repath = 20
		kit.MoveTo(m, g.pos.Vec3Centre().Add(mgl64.Vec3{0, 1, 0}), g.speed)
	}
}

// Stop ...
func (g *stayNearNoteblock) Stop(m *mob.Mob) { kit.Stop(m) }

// AppendDebugInfo ...
func (g *stayNearNoteblock) AppendDebugInfo(b *strings.Builder) { b.WriteString("StayNearNoteblock") }
