//go:build crossfly_full

package boss

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func init() {
	mob.RegisterDeathHook(mob.DeathHook{Name: "boss/wither_rose", Die: func(m *mob.Mob, src world.DamageSource) {
		if plantsWitherRose(src) {
			tryPlantWitherRose(m.Tx(), m.Position())
		}
	}})
}

// plantsWitherRose is the check of Actor::die: an actor dies leaving a
// wither rose when the entity that dealt the damage (the attacker, the
// projectile, the cause of an explosion) is a wither or one of its skulls.
func plantsWitherRose(src world.DamageSource) bool {
	var e world.Entity
	switch s := src.(type) {
	case entity.AttackDamageSource:
		e = s.Attacker
	case entity.ProjectileDamageSource:
		e = s.Projectile
	default:
		e = explosionCauser(src)
	}
	return e != nil && isWitherOrSkull(e)
}

func isWitherOrSkull(e world.Entity) bool {
	t := e.H().Type()
	return t == WitherSkullType || t == BlueWitherSkullType || t.EncodeEntity() == WitherID
}

// tryPlantWitherRose is Actor::_tryPlantWitherRose: a wither rose grows in
// the block at pos if it is air, the rose can stand there and mobGriefing
// is on; otherwise a wither rose item drops at pos.
func tryPlantWitherRose(tx *world.Tx, pos mgl64.Vec3) {
	rose := block.Flower{Type: block.WitherRose()}
	bp := cube.PosFromVec3(pos)
	if bp.OutOfBounds(tx.Range()) {
		return
	}
	if soil, ok := tx.Block(bp.Side(cube.FaceDown)).(block.Soil); ok && isAir(tx, bp) && soil.SoilFor(rose) && MobGriefing(tx) {
		tx.SetBlock(bp, rose, nil)
		return
	}
	tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: pos}, item.NewStack(rose, 1)))
}

// livePlayersNear returns the players alive in box, so plantForKilledPlayers can
// tell which of them the damage that follows killed. Mobs plant their rose
// through the death hook; players have none.
func livePlayersNear(tx *world.Tx, box cube.BBox) []world.Entity {
	var ps []world.Entity
	for e := range tx.EntitiesWithin(box) {
		if sense.IsPlayer(e) && sense.Alive(e) {
			ps = append(ps, e)
		}
	}
	return ps
}

// plantForKilledPlayers plants the wither roses of the players in es that
// died since they were listed alive (mobs in es are left to the death hook).
func plantForKilledPlayers(tx *world.Tx, es []world.Entity) {
	for _, p := range es {
		if d, ok := p.(interface{ Dead() bool }); ok && sense.IsPlayer(p) && d.Dead() {
			tryPlantWitherRose(tx, p.Position())
		}
	}
}
