//go:build crossfly_full

package monster

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/javamob"
	"github.com/df-mc/dragonfly/server/mob/javamob/goal"
	"github.com/df-mc/dragonfly/server/world"
)

func init() {
	javamob.Register(javamob.Type{
		ID: "minecraft:creeper", Category: javamob.CategoryMonster, Width: 0.6, Height: 1.7,
		NotInPeaceful: true, XPReward: 5,
		Families: []string{"creeper", "monster", "mob"},
		Attributes: func() map[javamob.Attribute]float64 {
			a := javamob.MonsterAttributes()
			a[javamob.AttrMovementSpeed] = 0.25
			return a
		},
		New: func() javamob.Behaviour { return &Creeper{maxSwell: 30, radius: 3, swellDir: -1} },
	})
}

// Creeper is Java's Creeper: it swells for 1.5 seconds within 3 blocks of its target (and keeps swelling up to 7
// blocks away while it sees it), then explodes.
type Creeper struct {
	swell, maxSwell, radius int
	swellDir                int
	ignited, powered        bool
}

// RegisterGoals is Creeper.registerGoals.
func (c *Creeper) RegisterGoals(m *javamob.Mob) {
	m.Goals().Add(1, goal.NewFloat(m))
	m.Goals().Add(2, &swellGoal{c: c, GoalBase: javamob.GoalBase{Control: javamob.FlagMove}})
	m.Goals().Add(3, goal.NewAvoidEntity(goal.IsType("minecraft:ocelot"), 6, 1, 1.2))
	m.Goals().Add(3, goal.NewAvoidEntity(goal.IsType("minecraft:cat"), 6, 1, 1.2))
	m.Goals().Add(4, goal.NewMeleeAttack(1, false))
	m.Goals().Add(5, goal.NewWaterAvoidingRandomStroll(0.8, 0.001))
	m.Goals().Add(6, goal.NewLookAtPlayer(8))
	m.Goals().Add(6, goal.NewRandomLookAround())
	m.Targets().Add(1, goal.NewNearestAttackableTarget(nil, true, false))
	m.Targets().Add(2, goal.NewHurtByTarget())
}

// Swell is the swell (0..30): how far the fuse is.
func (c *Creeper) Swell() int { return c.swell }

// SwellDir is getSwellDir: 1 swelling, -1 shrinking.
func (c *Creeper) SwellDir() int { return c.swellDir }

// SetSwellDir is setSwellDir, shown to clients as the ignited (swelling) flag.
func (c *Creeper) SetSwellDir(m *javamob.Mob, d int) {
	c.swellDir = d
	ign := d > 0
	if m.Meta().Ignited != ign {
		m.UpdateMeta(func(meta *mob.Meta) {
			meta.Ignited = ign
			meta.Swell = int32(d)
			meta.FuseTicks = int32(c.maxSwell)
		})
	}
}

// Ignite is ignite: a creeper lit with flint and steel swells to the end.
func (c *Creeper) Ignite(m *javamob.Mob) {
	c.ignited = true
	c.SetSwellDir(m, 1)
}

// SetPowered is a lightning strike (thunderHit): a charged creeper explodes twice as big.
func (c *Creeper) SetPowered(m *javamob.Mob, v bool) {
	c.powered = v
	m.UpdateMeta(func(meta *mob.Meta) { meta.Charged = v })
}

// Tick is Creeper.tick before super.tick: the swell, and the explosion when it is full.
func (c *Creeper) Tick(m *javamob.Mob) {
	if m.Dead() {
		return
	}
	if c.ignited {
		c.SetSwellDir(m, 1)
	}
	c.swell += c.swellDir
	if c.swell < 0 {
		c.swell = 0
	}
	if c.swell >= c.maxSwell {
		c.swell = c.maxSwell
		c.explode(m)
	}
}

// explode is explodeCreeper: an explosion of radius 3 (6 charged) that breaks blocks (mob griefing), and the
// creeper is gone.
func (c *Creeper) explode(m *javamob.Mob) {
	size := float64(c.radius)
	if c.powered {
		size *= 2
	}
	// The creeper is at the centre: the explosion passes over it (ServerExplosion leaves out its source).
	block.ExplosionConfig{Source: m}.Explode(m.Tx(), world.EntityExplosionSource{Entity: m, ExplosionSize: size})
	_ = m.Close()
}

// MaxFallDistance is Creeper.getMaxFallDistance: with a target it risks all but one health.
func (c *Creeper) MaxFallDistance(m *javamob.Mob) int {
	if _, ok := m.Target(); !ok {
		return m.ComfortableFallDistance(0)
	}
	return m.ComfortableFallDistance(m.Health() - 1)
}

// OnFall is Creeper.causeFallDamage: a fall swells the creeper by 1.5 per block, short of exploding.
func (c *Creeper) OnFall(_ *javamob.Mob, dist float64) {
	c.swell += int(dist * 1.5)
	if c.swell > c.maxSwell-5 {
		c.swell = c.maxSwell - 5
	}
}

// DoHurtTarget is Creeper.doHurtTarget: a creeper does not hit.
func (c *Creeper) DoHurtTarget(*javamob.Mob, world.Entity) bool { return true }

// CanAttack is Creeper.setTarget: never a goat.
func (c *Creeper) CanAttack(_ *javamob.Mob, e world.Entity) bool {
	return e.H().Type().EncodeEntity() != "minecraft:goat"
}

// MobInteract is Creeper.mobInteract: flint and steel or a fire charge lights the creeper.
func (c *Creeper) MobInteract(m *javamob.Mob, _ item.User, held item.Stack) (item.Stack, bool) {
	switch held.Item().(type) {
	case item.FlintAndSteel:
		m.PlaySound("ignite")
		c.Ignite(m)
		return held.Damage(1), true
	case item.FireCharge:
		m.PlaySound("ignite")
		c.Ignite(m)
		return held.Grow(-1), true
	}
	return held, false
}

// WalkTargetValue is Monster.getWalkTargetValue.
func (c *Creeper) WalkTargetValue(m *javamob.Mob, pos cube.Pos) float64 {
	return javamob.MonsterWalkTargetValue(m, pos)
}

// EncodeNBT is Creeper.addAdditionalSaveData.
func (c *Creeper) EncodeNBT(data map[string]any) {
	data["powered"] = c.powered
	data["Fuse"] = int16(c.maxSwell)
	data["ExplosionRadius"] = uint8(c.radius)
	data["ignited"] = c.ignited
}

// DecodeNBT is Creeper.readAdditionalSaveData.
func (c *Creeper) DecodeNBT(m *javamob.Mob, data map[string]any) {
	if v, ok := data["powered"].(bool); ok && v {
		c.SetPowered(m, true)
	}
	if v, ok := data["Fuse"].(int16); ok {
		c.maxSwell = int(v)
	}
	if v, ok := data["ExplosionRadius"].(uint8); ok {
		c.radius = int(v)
	}
	if v, ok := data["ignited"].(bool); ok && v {
		c.Ignite(m)
	}
}

// swellGoal is SwellGoal: stop and swell while the target is within 3 blocks, keep swelling while it is within 7
// and seen.
type swellGoal struct {
	javamob.GoalBase
	c      *Creeper
	target *world.EntityHandle
}

func (g *swellGoal) CanUse(m *javamob.Mob) bool {
	if g.c.swellDir > 0 {
		return true
	}
	t, ok := m.Target()
	return ok && !dead(t) && t.Position().Sub(m.Position()).LenSqr() < 9
}

func (g *swellGoal) Start(m *javamob.Mob) {
	m.Navigation().Stop()
	g.target = m.TargetHandle()
}
func (g *swellGoal) Stop(*javamob.Mob)             { g.target = nil }
func (g *swellGoal) RequiresUpdateEveryTick() bool { return true }

func (g *swellGoal) Tick(m *javamob.Mob) {
	var t world.Entity
	ok := false
	if g.target != nil {
		t, ok = g.target.Entity(m.Tx())
	}
	switch {
	case !ok || dead(t):
		g.c.SetSwellDir(m, -1)
	case t.Position().Sub(m.Position()).LenSqr() > 49:
		g.c.SetSwellDir(m, -1)
	case !m.Sensing().HasLineOfSight(m, t):
		g.c.SetSwellDir(m, -1)
	default:
		g.c.SetSwellDir(m, 1)
	}
}

func dead(e world.Entity) bool {
	l, ok := e.(entity.Living)
	return ok && l.Dead()
}
