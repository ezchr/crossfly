//go:build crossfly_full

package monster

import (
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/goal/village"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/mob/javamob/path"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func init() {
	javamob.Register(javamob.Type{
		ID: "minecraft:enderman", Category: javamob.CategoryMonster, Width: 0.6, Height: 2.9, EyeHeight: 2.55,
		NotInPeaceful: true, XPReward: 5,
		Families: []string{"enderman", "monster", "mob"},
		Attributes: func() map[javamob.Attribute]float64 {
			a := javamob.MonsterAttributes()
			a[javamob.AttrMaxHealth] = 40
			a[javamob.AttrMovementSpeed] = float64(float32(0.3))
			a[javamob.AttrAttackDamage] = 7
			a[javamob.AttrFollowRange] = 64
			a[javamob.AttrStepHeight] = 1
			return a
		},
		New: func() javamob.Behaviour {
			return &Enderman{NeutralMob: javamob.NeutralMob{AngerTime: [2]int{400, 780}}, lastStare: math.MinInt32}
		},
	})
}

// Enderman is Java's Enderman: neutral until stared at or hit, it teleports away from projectiles, water and
// daylight, and moves blocks around.
type Enderman struct {
	javamob.NeutralMob
	targetChangeTime int
	// carried mirrors the carried block (kept in the mob's data by package goal/village) for EncodeNBT.
	carried   world.Block
	lastStare int
	staredAt  bool
}

// RegisterGoals is Enderman.registerGoals (and the water malus of its constructor).
func (e *Enderman) RegisterGoals(m *javamob.Mob) {
	m.SetPathfindingMalus(path.Water, -1)
	m.Goals().Add(0, goal.NewFloat(m))
	m.Goals().Add(1, &freezeWhenLookedAt{GoalBase: javamob.GoalBase{Control: javamob.FlagJump | javamob.FlagMove}, e: e})
	m.Goals().Add(2, goal.NewMeleeAttack(1, false))
	m.Goals().Add(7, goal.NewWaterAvoidingRandomStroll(1, 0))
	m.Goals().Add(8, goal.NewLookAtPlayer(8))
	m.Goals().Add(8, goal.NewRandomLookAround())
	m.Goals().Add(10, &leaveBlock{})
	m.Goals().Add(11, &takeBlock{})
	m.Targets().Add(1, newLookForPlayer(e))
	m.Targets().Add(2, goal.NewHurtByTarget())
	m.Targets().Add(3, goal.NewNearestAttackableTarget(goal.IsType("minecraft:endermite"), true, false))
	// ResetUniversalAngerTargetGoal only acts with the universalAnger game rule, off by default.
}

// WalkTargetValue is Enderman.getWalkTargetValue: everywhere alike.
func (e *Enderman) WalkTargetValue(*javamob.Mob, cube.Pos) float64 { return 0 }

// OnTargetChanged is Enderman.setTarget: the scream (creepy) and +0.15 speed while it has a target.
func (e *Enderman) OnTargetChanged(m *javamob.Mob, _, target world.Entity) {
	speed := m.Attributes().Instance(javamob.AttrMovementSpeed)
	if target == nil {
		e.targetChangeTime = 0
		e.staredAt = false
		speed.RemoveModifier("minecraft:attacking")
		setCreepy(m, false)
	} else {
		e.targetChangeTime = m.TickCount()
		setCreepy(m, true)
		if !speed.HasModifier("minecraft:attacking") {
			speed.AddModifier(javamob.Modifier{ID: "minecraft:attacking", Amount: float64(float32(0.15)), Op: javamob.OpAddValue, Transient: true})
		}
	}
	m.SyncAttributes()
}

// setCreepy is DATA_CREEPY: the open-jawed scream, shown to clients as angry.
func setCreepy(m *javamob.Mob, v bool) {
	if m.Meta().Angry != v {
		m.UpdateMeta(func(meta *mob.Meta) { meta.Angry = v })
	}
}

// AiStep is Enderman.aiStep: no jumping, and the anger kept up while the target is around.
func (e *Enderman) AiStep(m *javamob.Mob) {
	m.SetJumping(false)
	e.UpdatePersistentAnger(m, true)
}

// SensitiveToWater is Enderman.isSensitiveToWater.
func (e *Enderman) SensitiveToWater(*javamob.Mob) bool { return true }

// CustomServerAiStep is Enderman.customServerAiStep: in daylight under the sky, half a minute after its last
// target change, it drops the target and teleports away.
func (e *Enderman) CustomServerAiStep(m *javamob.Mob) {
	if !goal.BrightOutside(m) || m.TickCount() < e.targetChangeTime+600 {
		return
	}
	br := m.LightLevelDependentMagicValue()
	if br > 0.5 && path.CanSeeSky(m.Tx(), cube.PosFromVec3(m.Position())) && m.Rand().Float32()*30 < (br-0.4)*2 {
		m.SetTarget(nil)
		e.Teleport(m)
	}
}

// FilterDamage is the start of Enderman.hurtServer: projectiles never hit (it teleports away instead).
func (e *Enderman) FilterDamage(m *javamob.Mob, d *mob.Damage) {
	if _, proj := d.Source.(entity.ProjectileDamageSource); proj && !m.Riding() {
		d.Cancel = true
		e.repeatedlyTryToTeleport(m)
	}
}

// OnHurt is the end of Enderman.hurtServer: hurt by something that is no living entity, it usually teleports.
func (e *Enderman) OnHurt(m *javamob.Mob, _ float64, src world.DamageSource) {
	if m.Dead() {
		return
	}
	if _, living := javamob.SourceEntity(src).(entity.Living); !living && m.Rand().IntN(10) != 0 {
		e.Teleport(m)
	}
}

func (e *Enderman) repeatedlyTryToTeleport(m *javamob.Mob) {
	if m.Riding() {
		return
	}
	for i := 0; i < 64; i++ {
		if e.Teleport(m) {
			return
		}
	}
}

// Teleport is Enderman.teleport: somewhere random within 32 blocks.
func (e *Enderman) Teleport(m *javamob.Mob) bool {
	if m.Dead() {
		return false
	}
	r := m.Rand()
	p := m.Position()
	return e.teleportTo(m, mgl64.Vec3{p[0] + (r.Float64()-0.5)*64, p[1] + float64(r.IntN(64)-32), p[2] + (r.Float64()-0.5)*64})
}

// teleportTowards is Enderman.teleportTowards: closer to the entity.
func (e *Enderman) teleportTowards(m *javamob.Mob, t world.Entity) bool {
	p := m.Position()
	eye := entity.EyePosition(t)
	dir := mgl64.Vec3{p[0] - t.Position()[0], p[1] + m.Height()/2 - eye[1], p[2] - t.Position()[2]}.Normalize()
	r := m.Rand()
	return e.teleportTo(m, mgl64.Vec3{
		p[0] + (r.Float64()-0.5)*8 - dir[0]*16,
		p[1] + float64(r.IntN(16)-8) - dir[1]*16,
		p[2] + (r.Float64()-0.5)*8 - dir[2]*16,
	})
}

// teleportTo is LivingEntity.randomTeleport: down to the ground under to, onto a spot without collisions or
// liquids and not over water (canRandomlyTeleportTo).
func (e *Enderman) teleportTo(m *javamob.Mob, to mgl64.Vec3) bool {
	if m.Riding() {
		return false
	}
	tx := m.Tx()
	pos := cube.PosFromVec3(to)
	if _, loaded := tx.BlockLoaded(pos); !loaded || pos.OutOfBounds(tx.Range()) {
		return false
	}
	y := to[1]
	ground := false
	for !ground && pos[1] > tx.Range()[0] {
		below := pos.Side(cube.FaceDown)
		if path.IsSolid(tx, below) {
			ground = true
		} else {
			y--
			pos = below
		}
	}
	if !ground {
		return false
	}
	if l, ok := tx.Liquid(pos.Side(cube.FaceDown)); ok && l.LiquidType() == "water" {
		return false
	}
	at := mgl64.Vec3{to[0], y, to[2]}
	w, h := m.Width()/2, m.Height()
	box := cube.Box(at[0]-w, at[1], at[2]-w, at[0]+w, at[1]+h, at[2]+w)
	if path.Collides(tx, box) || liquidIn(tx, box) {
		return false
	}
	m.Teleport(at)
	m.Navigation().Stop()
	m.PlaySound("teleport")
	return true
}

// RemoveWhenFarAway is Enderman.requiresCustomPersistence: an enderman holding a block does not despawn.
func (e *Enderman) RemoveWhenFarAway(m *javamob.Mob, _ float64) bool {
	_, carrying := village.Carried(m.Mob)
	return !carrying
}

// beingStaredBy is Enderman.isBeingStaredBy: the player looks right at its head (and wears no carved pumpkin).
func beingStaredBy(m *javamob.Mob, p *player.Player) bool {
	if helmet := p.Armour().Helmet(); !helmet.Empty() {
		if name, _ := helmet.Item().EncodeItem(); name == "minecraft:carved_pumpkin" {
			return false
		}
	}
	look := p.Rotation().Vec3().Normalize()
	pe := entity.EyePosition(p)
	pos := m.Position()
	dir := mgl64.Vec3{pos[0] - pe[0], m.EyeY() - pe[1], pos[2] - pe[2]}
	dist := dir.Len()
	if dist == 0 {
		return false
	}
	dir = dir.Mul(1 / dist)
	if look.Dot(dir) <= 1-0.025/dist {
		return false
	}
	return javamob.Clear(m.Tx(), pe, mgl64.Vec3{pos[0], m.EyeY(), pos[2]})
}

// EncodeNBT is Enderman.addAdditionalSaveData: the carried block and the anger.
func (e *Enderman) EncodeNBT(data map[string]any) {
	e.EncodeAnger(data)
	if e.carried != nil {
		name, props := e.carried.EncodeBlock()
		data["carriedBlockState"] = map[string]any{"name": name, "states": props}
	}
}

// DecodeNBT is Enderman.readAdditionalSaveData.
func (e *Enderman) DecodeNBT(m *javamob.Mob, data map[string]any) {
	e.DecodeAnger(m, data)
	if b, ok := data["carriedBlockState"].(map[string]any); ok {
		name, _ := b["name"].(string)
		props, _ := b["states"].(map[string]any)
		if bl, ok := world.BlockByName(name, props); ok {
			village.SetCarried(m.Mob, bl)
		}
	}
}

// PostTick notes the carried block for EncodeNBT, which runs outside any transaction.
func (e *Enderman) PostTick(m *javamob.Mob) {
	e.carried, _ = village.Carried(m.Mob)
}

// freezeWhenLookedAt is EndermanFreezeWhenLookedAt: it stands still and stares back at a player looking at it
// within 16 blocks.
type freezeWhenLookedAt struct {
	javamob.GoalBase
	e      *Enderman
	target *world.EntityHandle
}

func (g *freezeWhenLookedAt) CanUse(m *javamob.Mob) bool {
	t, ok := m.Target()
	p, isPlayer := t.(*player.Player)
	if !ok || !isPlayer {
		return false
	}
	g.target = t.H()
	if t.Position().Sub(m.Position()).LenSqr() > 256 {
		return false
	}
	return beingStaredBy(m, p)
}
func (g *freezeWhenLookedAt) Start(m *javamob.Mob) { m.Navigation().Stop() }
func (g *freezeWhenLookedAt) Tick(m *javamob.Mob) {
	if t, ok := g.target.Entity(m.Tx()); ok {
		m.LookControl().SetLookAt(m, entity.EyePosition(t))
	}
}

// lookForPlayer is EndermanLookForPlayerGoal: a player that stares at it (or that it is angry at) becomes its
// target a quarter second later; it teleports when stared at up close or when the target gets far away.
type lookForPlayer struct {
	*goal.NearestAttackableTarget
	e           *Enderman
	pending     *world.EntityHandle
	aggroTime   int
	teleportTim int
}

func newLookForPlayer(e *Enderman) *lookForPlayer {
	g := &lookForPlayer{NearestAttackableTarget: goal.NewNearestAttackableTarget(nil, false, false), e: e}
	return g
}

func (g *lookForPlayer) angerInducing(m *javamob.Mob) func(world.Entity, *world.Tx) bool {
	return func(t world.Entity, _ *world.Tx) bool {
		p, ok := t.(*player.Player)
		if !ok {
			return false
		}
		if m.HasPassenger(p.H()) {
			return false
		}
		return beingStaredBy(m, p) || g.e.IsAngryAt(m, p)
	}
}

func (g *lookForPlayer) CanUse(m *javamob.Mob) bool {
	c := javamob.ForCombat(m.FollowRange())
	c.Selector = g.angerInducing(m)
	p, ok := m.NearestPlayer(c)
	if !ok {
		g.pending = nil
		return false
	}
	g.pending = p.H()
	return true
}

func (g *lookForPlayer) Start(m *javamob.Mob) {
	g.aggroTime = javamob.AdjustedTickDelay(g, 5)
	g.teleportTim = 0
	g.e.staredAt = true
}

func (g *lookForPlayer) Stop(m *javamob.Mob) {
	g.pending = nil
	g.NearestAttackableTarget.Stop(m)
}

func (g *lookForPlayer) CanContinueToUse(m *javamob.Mob) bool {
	if g.pending != nil {
		p, ok := g.pending.Entity(m.Tx())
		if !ok || !g.angerInducing(m)(p, m.Tx()) {
			return false
		}
		m.LookAtEntity(p, 10, 10)
		return true
	}
	if t, ok := m.Target(); ok {
		c := javamob.ForCombat(0)
		c.IgnoreLineOfSight = true
		if c.Test(m, t) {
			return true
		}
	}
	return g.NearestAttackableTarget.CanContinueToUse(m)
}

func (g *lookForPlayer) RequiresUpdateEveryTick() bool { return false }

func (g *lookForPlayer) Tick(m *javamob.Mob) {
	if g.pending != nil {
		if g.aggroTime--; g.aggroTime <= 0 {
			if p, ok := g.pending.Entity(m.Tx()); ok {
				m.SetTarget(p)
			}
			g.pending = nil
			g.NearestAttackableTarget.Target.Start(m)
		}
		return
	}
	t, ok := m.Target()
	if !ok || m.Riding() {
		return
	}
	p, isPlayer := t.(*player.Player)
	d := t.Position().Sub(m.Position()).LenSqr()
	if isPlayer && beingStaredBy(m, p) {
		if d < 16 {
			g.e.Teleport(m)
		}
		g.teleportTim = 0
	} else if d > 256 {
		if g.teleportTim++; g.teleportTim >= javamob.AdjustedTickDelay(g, 30) && g.e.teleportTowards(m, t) {
			g.teleportTim = 0
		}
	}
}

// holdable is BlockTags.ENDERMAN_HOLDABLE, by Bedrock name.
func holdable(b world.Block) bool {
	name, _ := b.EncodeBlock()
	name = strings.TrimPrefix(name, "minecraft:")
	switch name {
	case "grass_block", "dirt", "coarse_dirt", "podzol", "mycelium", "rooted_dirt", "moss_block", "pale_moss_block",
		"mud", "muddy_mangrove_roots", "sand", "red_sand", "gravel", "brown_mushroom", "red_mushroom", "tnt",
		"cactus", "clay", "pumpkin", "carved_pumpkin", "melon_block", "crimson_fungus", "crimson_nylium",
		"crimson_roots", "warped_fungus", "warped_nylium", "warped_roots", "dandelion", "poppy", "blue_orchid",
		"allium", "azure_bluet", "red_tulip", "orange_tulip", "white_tulip", "pink_tulip", "oxeye_daisy",
		"cornflower", "lily_of_the_valley", "wither_rose", "torchflower", "open_eyeblossom", "closed_eyeblossom":
		return true
	}
	return false
}

// takeBlock is EndermanTakeBlockGoal: now and then it picks up a holdable block it can reach (mob griefing).
type takeBlock struct{ javamob.GoalBase }

func (g *takeBlock) CanUse(m *javamob.Mob) bool {
	if _, carrying := village.Carried(m.Mob); carrying {
		return false
	}
	return m.Rand().IntN(javamob.ReducedTickDelay(20)) == 0
}

func (g *takeBlock) Tick(m *javamob.Mob) {
	r := m.Rand()
	p := m.Position()
	pos := cube.Pos{int(math.Floor(p[0] - 2 + r.Float64()*4)), int(math.Floor(p[1] + r.Float64()*3)), int(math.Floor(p[2] - 2 + r.Float64()*4))}
	tx := m.Tx()
	b := tx.Block(pos)
	if !holdable(b) {
		return
	}
	bp := cube.PosFromVec3(p)
	from := mgl64.Vec3{float64(bp[0]) + 0.5, float64(pos[1]) + 0.5, float64(bp[2]) + 0.5}
	if hit, ok := firstBlockOnLine(tx, from, pos.Vec3Centre()); !ok || hit != pos {
		return
	}
	tx.SetBlock(pos, nil, nil)
	village.SetCarried(m.Mob, b)
}

// firstBlockOnLine is the block a Block.OUTLINE clip from from to to hits first.
func firstBlockOnLine(tx *world.Tx, from, to mgl64.Vec3) (cube.Pos, bool) {
	start := cube.PosFromVec3(from)
	end := cube.PosFromVec3(to)
	steps := int(math.Ceil(to.Sub(from).Len()*4)) + 1
	for i := 0; i <= steps; i++ {
		v := from.Add(to.Sub(from).Mul(float64(i) / float64(steps)))
		pos := cube.PosFromVec3(v)
		if pos == start && pos != end {
			continue
		}
		if _, air := tx.Block(pos).(block.Air); !air {
			return pos, true
		}
	}
	return cube.Pos{}, false
}

// leaveBlock is EndermanLeaveBlockGoal: now and then it puts the block down next to it on a full block.
type leaveBlock struct{ javamob.GoalBase }

func (g *leaveBlock) CanUse(m *javamob.Mob) bool {
	if _, carrying := village.Carried(m.Mob); !carrying {
		return false
	}
	return m.Rand().IntN(javamob.ReducedTickDelay(2000)) == 0
}

func (g *leaveBlock) Tick(m *javamob.Mob) {
	carried, ok := village.Carried(m.Mob)
	if !ok {
		return
	}
	r := m.Rand()
	p := m.Position()
	pos := cube.Pos{int(math.Floor(p[0] - 1 + r.Float64()*2)), int(math.Floor(p[1] + r.Float64()*2)), int(math.Floor(p[2] - 1 + r.Float64()*2))}
	tx := m.Tx()
	if _, air := tx.Block(pos).(block.Air); !air {
		return
	}
	below := tx.Block(pos.Side(cube.FaceDown))
	if _, air := below.(block.Air); air {
		return
	}
	if _, bedrock := below.(block.Bedrock); bedrock || !path.IsSuffocating(below) {
		return
	}
	for e := range tx.EntitiesWithin(cube.Box(float64(pos[0]), float64(pos[1]), float64(pos[2]), float64(pos[0]+1), float64(pos[1]+1), float64(pos[2]+1))) {
		if e.H() != m.H() {
			return
		}
	}
	tx.SetBlock(pos, carried, nil)
	village.SetCarried(m.Mob, nil)
}
