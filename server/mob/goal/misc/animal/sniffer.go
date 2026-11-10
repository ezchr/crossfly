//go:build crossfly_full

package animal

import (
	"encoding/json"
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// randomSearchAndDig is minecraft:behavior.random_search_and_dig (BDS
// RandomSearchAndDigGoal; control flags move|look): the sniffer picks a
// random target_blocks block with air above within search_range_xz /
// search_range_y (find_valid_position_retries tries), walks there at
// speed_multiplier (actor flag searching, on_searching_start), and within
// goal_radius digs for digging_duration_range seconds (actor flag digging,
// on_digging_start). spawn_item_after_seconds into the dig the item_table
// loot drops spawn_item_pos_offset blocks ahead (on_item_found); at the end
// on_success fires. A failed path fires on_fail_during_searching, losing the
// dig block (or water) on_fail_during_digging. Then cooldown_range.
type randomSearchAndDig struct {
	mob.GoalBase
	debugName
	speed, radius                                                float64
	retries, xz, y                                               int
	targets                                                      kit.BlockList
	cooldown, digTime                                            kit.Range
	table                                                        string
	itemAfter                                                    int
	itemOffset                                                   float64
	onSearch, onSearchFail, onDig, onFound, onDigFail, onSuccess kit.Trigger

	state, timer, digLen int
	target               cube.Pos
	next                 int64
	found                bool
}

const (
	searching = iota
	digging
	finished
)

func newRandomSearchAndDig(spec mob.GoalSpec) (mob.Goal, error) {
	var v struct {
		Speed      *float64        `json:"speed_multiplier"`
		Retries    *int            `json:"find_valid_position_retries"`
		Targets    json.RawMessage `json:"target_blocks"`
		Radius     *float64        `json:"goal_radius"`
		XZ         *int            `json:"search_range_xz"`
		Y          *int            `json:"search_range_y"`
		Cooldown   json.RawMessage `json:"cooldown_range"`
		DigTime    json.RawMessage `json:"digging_duration_range"`
		Table      string          `json:"item_table"`
		ItemAfter  float64         `json:"spawn_item_after_seconds"`
		ItemOffset float64         `json:"spawn_item_pos_offset"`
		OnSearch   json.RawMessage `json:"on_searching_start"`
		OnSFail    json.RawMessage `json:"on_fail_during_searching"`
		OnDig      json.RawMessage `json:"on_digging_start"`
		OnFound    json.RawMessage `json:"on_item_found"`
		OnDFail    json.RawMessage `json:"on_fail_during_digging"`
		OnSuccess  json.RawMessage `json:"on_success"`
	}
	if err := kit.Decode(spec, &v); err != nil {
		return nil, err
	}
	g := &randomSearchAndDig{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, debugName: "RandomSearchAndDigGoal",
		speed: kit.Or(v.Speed, 1), retries: kit.Or(v.Retries, 10), radius: kit.Or(v.Radius, 1.5), xz: kit.Or(v.XZ, 20), y: kit.Or(v.Y, 3),
		table: v.Table, itemAfter: kit.Ticks(v.ItemAfter), itemOffset: v.ItemOffset}
	var err error
	if g.targets, err = kit.ParseBlockList(v.Targets); err != nil {
		return nil, err
	}
	if g.cooldown, err = kit.ParseRange(v.Cooldown, kit.Range{}); err != nil {
		return nil, err
	}
	if g.digTime, err = kit.ParseRange(v.DigTime, kit.Range{}); err != nil {
		return nil, err
	}
	for _, t := range []struct {
		dst *kit.Trigger
		raw json.RawMessage
	}{{&g.onSearch, v.OnSearch}, {&g.onSearchFail, v.OnSFail}, {&g.onDig, v.OnDig}, {&g.onFound, v.OnFound}, {&g.onDigFail, v.OnDFail}, {&g.onSuccess, v.OnSuccess}} {
		if *t.dst, err = kit.ParseTrigger(t.raw); err != nil {
			return nil, err
		}
	}
	return g, nil
}

func (g *randomSearchAndDig) params() map[string]float64 {
	return map[string]float64{"speed_multiplier": g.speed, "retries": float64(g.retries), "goal_radius": g.radius, "search_range_xz": float64(g.xz),
		"search_range_y": float64(g.y), "dig_min": g.digTime.Min, "dig_max": g.digTime.Max, "item_after_ticks": float64(g.itemAfter), "item_offset": g.itemOffset}
}

func (g *randomSearchAndDig) validDig(tx *world.Tx, p cube.Pos) bool {
	return g.targets.Has(tx.Block(p)) && air(tx, p.Side(cube.FaceUp))
}

// CanUse ...
func (g *randomSearchAndDig) CanUse(m *mob.Mob) bool {
	if m.Ticks() < g.next || !m.OnGround() || m.Physics().InWater || m.Baby() {
		return false
	}
	tx := m.Tx()
	base := cube.PosFromVec3(m.Position())
	for i := 0; i < g.retries; i++ {
		p := base.Add(cube.Pos{m.Rand().IntN(2*g.xz+1) - g.xz, m.Rand().IntN(2*g.y+1) - g.y, m.Rand().IntN(2*g.xz+1) - g.xz})
		q, ok := kit.Ground(tx, p, g.y)
		if !ok {
			continue
		}
		if under := q.Side(cube.FaceDown); g.validDig(tx, under) {
			g.target = under
			return true
		}
	}
	g.next = m.Ticks() + 20
	return false
}

// CanContinueToUse ...
func (g *randomSearchAndDig) CanContinueToUse(*mob.Mob) bool { return g.state != finished }

// Start ...
func (g *randomSearchAndDig) Start(m *mob.Mob) {
	g.state, g.timer, g.found = searching, 0, false
	kit.Flag(m, protocol.EntityDataFlagSearching, true)
	g.onSearch.Fire(m, nil)
	if !kit.MoveToBlock(m, g.target.Side(cube.FaceUp), g.speed) {
		g.fail(m, &g.onSearchFail)
	}
}

func (g *randomSearchAndDig) fail(m *mob.Mob, t *kit.Trigger) {
	g.state = finished
	t.Fire(m, nil)
}

// Tick ...
func (g *randomSearchAndDig) Tick(m *mob.Mob) {
	tx := m.Tx()
	c := g.target.Side(cube.FaceUp).Vec3Centre()
	switch g.state {
	case searching:
		g.timer++
		if kit.HDistSq(m.Position(), c) <= g.radius*g.radius && math.Abs(m.Position()[1]-c[1]+0.5) <= 1.5 {
			kit.Stop(m)
			kit.Flag(m, protocol.EntityDataFlagSearching, false)
			kit.Flag(m, protocol.EntityDataFlagDigging, true)
			g.state, g.timer = digging, 0
			g.digLen = kit.RollTicks(m, g.digTime)
			g.onDig.Fire(m, nil)
			return
		}
		if m.Navigation().Done() || g.timer > 600 || !g.validDig(tx, g.target) {
			g.fail(m, &g.onSearchFail)
		}
	case digging:
		kit.Stop(m)
		kit.LookAt(m, g.target.Vec3Centre())
		if m.Physics().InWater || !g.targets.Has(tx.Block(blockAt(m.Position()).Side(cube.FaceDown))) && !g.targets.Has(tx.Block(g.target)) {
			g.fail(m, &g.onDigFail)
			return
		}
		g.timer++
		if !g.found && g.timer >= g.itemAfter {
			g.found = true
			// RandomSearchAndDigGoal::_digUpItems: at the sniffer's feet,
			// spawn_item_pos_offset ahead.
			pos := m.Position().Add(forward(m).Mul(g.itemOffset))
			dropAt(tx, m, pos, rollLoot(m, g.table))
			g.onFound.Fire(m, nil)
		}
		if g.timer >= g.digLen {
			g.state = finished
			g.onSuccess.Fire(m, nil)
		}
	}
}

// Stop ...
func (g *randomSearchAndDig) Stop(m *mob.Mob) {
	kit.Flag(m, protocol.EntityDataFlagSearching, false)
	kit.Flag(m, protocol.EntityDataFlagDigging, false)
	g.next = m.Ticks() + int64(kit.RollTicks(m, g.cooldown))
	g.state = searching
	kit.Stop(m)
}

// infested returns the infested version of a host block (BDS
// Block::tryGetInfested; source: java/paper InfestedBlock.infestedByHost).
func infested(b world.Block) (world.Block, bool) {
	switch b := b.(type) {
	case block.Cobblestone:
		if !b.Mossy {
			return block.InfestedCobblestone{}, true
		}
	case block.StoneBricks:
		return block.InfestedStoneBricks{Type: b.Type}, true
	case block.Deepslate:
		if b.Type == block.NormalDeepslate() {
			return block.InfestedDeepslate{Axis: b.Axis}, true
		}
	}
	if n, props := b.EncodeBlock(); n == "minecraft:stone" && len(props) == 0 {
		return block.InfestedStone{}, true
	}
	return nil, false
}

// uninfested returns the host block of an infested block
// (Block::tryGetUninfested).
func uninfested(b world.Block) (world.Block, bool) {
	switch b := b.(type) {
	case block.InfestedStone:
		s, ok := world.BlockByName("minecraft:stone", nil)
		return s, ok
	case block.InfestedCobblestone:
		return block.Cobblestone{}, true
	case block.InfestedStoneBricks:
		return block.StoneBricks{Type: b.Type}, true
	case block.InfestedDeepslate:
		return block.Deepslate{Type: block.NormalDeepslate(), Axis: b.Axis}, true
	}
	return nil, false
}

// mergeWithStone is minecraft:behavior.silverfish_merge_with_stone (BDS
// SilverfishMergeWithStoneGoal, a RandomStrollGoal with speed 1, xz 10,
// y 7, interval 120; control flags move|look): a silverfish without a
// target whose navigation is done, with mob griefing on and a 1 in 10
// chance, looks at the block in a random direction next to it (from
// floor(y + 0.5)): a host block (stone, cobblestone, stone bricks,
// deepslate) becomes infested and the silverfish goes into it (actor event
// silverfish merge animation, then removed). Otherwise it strolls like
// behavior.random_stroll.
type mergeWithStone struct {
	mob.GoalBase
	debugName
	face   cube.Face
	merge  bool
	wanted mgl64.Vec3
	done   bool
}

func newMergeWithStone(spec mob.GoalSpec) (mob.Goal, error) {
	return &mergeWithStone{GoalBase: mob.GoalBase{Control: spec.FlagsOr(mob.FlagMove | mob.FlagLook)}, debugName: "SilverfishMergeWithStoneGoal"}, nil
}

func (g *mergeWithStone) params() map[string]float64 { return map[string]float64{"chance": 10} }

// mergePos is the block the silverfish would hide in.
func (g *mergeWithStone) mergePos(m *mob.Mob) cube.Pos {
	return cube.PosFromVec3(m.Position().Add(mgl64.Vec3{0, 0.5, 0})).Side(g.face)
}

// CanUse ...
func (g *mergeWithStone) CanUse(m *mob.Mob) bool {
	if _, ok := m.Target(); ok || !m.HasNavigation() || !m.Navigation().Done() {
		return false
	}
	if combat.MobGriefing(m.Tx()) && m.Rand().IntN(10) == 0 {
		// Facing::getRandomFace: down, up, north, south, west, east.
		g.face = cube.Faces()[m.Rand().IntN(6)]
		if _, ok := infested(m.Tx().Block(g.mergePos(m))); ok {
			g.merge = true
			return true
		}
	}
	g.merge = false
	// RandomStrollGoal::canUse.
	if m.Rand().IntN(120) != 0 {
		return false
	}
	pos, ok := kit.RandomPos(m, 10, 7)
	if !ok {
		return false
	}
	g.wanted = pos
	return true
}

// CanContinueToUse ...
func (g *mergeWithStone) CanContinueToUse(*mob.Mob) bool { return !g.merge && !g.done }

// Start merges, or starts strolling.
func (g *mergeWithStone) Start(m *mob.Mob) {
	if !g.merge {
		if !m.Navigation().MoveTo(m, g.wanted.Add(mgl64.Vec3{0.5, 0, 0.5}), 1) {
			g.done = true
		}
		return
	}
	tx := m.Tx()
	pos := g.mergePos(m)
	b, ok := infested(tx.Block(pos))
	if !ok {
		return
	}
	tx.SetBlock(pos, b, nil)
	kit.Show(m, kit.ActorEvent(packet.ActorEventSilverfishMergeAnimation))
	_ = m.Close()
}

// Tick ...
func (g *mergeWithStone) Tick(m *mob.Mob) {
	if !g.merge {
		g.done = m.Navigation().Done()
	}
}

// Stop ...
func (g *mergeWithStone) Stop(m *mob.Mob) {
	if !g.merge {
		m.Navigation().Stop(m)
	}
	g.done = false
}

// wakeUpFriends is minecraft:behavior.silverfish_wake_up_friends (BDS
// SilverfishWakeUpFriendsGoal): when hurt by a mob or player (within the
// last 100 ticks) the silverfish waits 20 ticks, then scans 5 blocks up and
// down and 10 around (y outermost, each axis 0, 1, -1, 2, -2, ...) for
// infested blocks: with mob griefing each breaks like Level::destroyBlock
// (destroy particles, the silverfish of InfestedBlock::spawnAfterBreak),
// else it turns back into its host block; after each one the scan stops
// with a 50 % chance.
type wakeUpFriends struct {
	mob.GoalBase
	debugName
	counter  int
	lastHurt int64
}

func newWakeUpFriends(spec mob.GoalSpec) (mob.Goal, error) {
	return &wakeUpFriends{GoalBase: mob.GoalBase{Control: spec.FlagsOr(0)}, debugName: "SilverfishWakeUpFriendsGoal", lastHurt: -1}, nil
}

func (g *wakeUpFriends) params() map[string]float64 { return map[string]float64{"delay_ticks": 20} }

// CanUse ...
func (g *wakeUpFriends) CanUse(m *mob.Mob) bool {
	_, tick, ok := m.LastHurtBy()
	if ok && m.Ticks()-tick <= 100 && tick != g.lastHurt && g.counter <= 0 {
		g.counter, g.lastHurt = 20, tick
	}
	return g.counter > 0
}

// Tick ...
func (g *wakeUpFriends) Tick(m *mob.Mob) {
	if g.counter--; g.counter > 0 {
		return
	}
	tx := m.Tx()
	base := cube.PosFromVec3(m.Position())
	grief := combat.MobGriefing(tx)
	// BDS order of each axis: 0, 1, -1, 2, -2, ...
	alt := func(i int) int {
		if i%2 == 1 {
			return (i + 1) / 2
		}
		return -i / 2
	}
	for yi := 0; yi <= 10; yi++ {
		for xi := 0; xi <= 20; xi++ {
			for zi := 0; zi <= 20; zi++ {
				p := base.Add(cube.Pos{alt(xi), alt(yi), alt(zi)})
				b := tx.Block(p)
				host, ok := uninfested(b)
				if !ok {
					continue
				}
				if grief {
					// Level::destroyBlock with drops: break particles and
					// sound, then the block's spawnAfterBreak lets its
					// silverfish out (no silk touch: no user).
					tx.SetBlock(p, nil, nil)
					tx.AddParticle(p.Vec3Centre(), particle.BlockBreak{Block: b})
					if br, ok := b.(block.Breakable); ok {
						if h := br.BreakInfo().BreakHandler; h != nil {
							h(p, tx, nil)
						}
					}
				} else {
					tx.SetBlock(p, host, nil)
				}
				if m.Rand().IntN(2) == 0 {
					return
				}
			}
		}
	}
}
