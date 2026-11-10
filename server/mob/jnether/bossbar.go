//go:build crossfly_full

package jnether

import (
	"github.com/df-mc/dragonfly/server/mob/component/boss"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// bossBar is a ServerBossEvent shown through the sessions' mob boss bars
// (boss.BossBarViewer, one bar per entity): the ender dragon's (owned by the
// End fight in Java) and the wither's.
type bossBar struct {
	colour uint8
	darken bool
	shown  map[boss.BossBarViewer]float64
}

// update shows the bar of e at progress to the viewers whose player near
// says so (checked every 20 ticks, or when refresh), updates the progress
// for those that see it and hides it from the others.
func (b *bossBar) update(tx *world.Tx, e world.Entity, progress float64, refresh bool, near func(p world.Entity) bool) {
	if refresh || b.shown == nil {
		in := map[boss.BossBarViewer]bool{}
		for _, v := range tx.Viewers(e.Position()) {
			bv, ok := v.(boss.BossBarViewer)
			if !ok {
				continue
			}
			pe, ok := bv.MobBossBarViewer().Entity(tx)
			if !ok || !near(pe) {
				continue
			}
			in[bv] = true
		}
		if b.shown == nil {
			b.shown = map[boss.BossBarViewer]float64{}
		}
		for v := range b.shown {
			if !in[v] {
				v.HideMobBossBar(e)
				delete(b.shown, v)
			}
		}
		for v := range in {
			if _, ok := b.shown[v]; !ok {
				v.ShowMobBossBar(e, "", progress, b.colour, b.darken)
				b.shown[v] = progress
			}
		}
	}
	for v, last := range b.shown {
		if last != progress {
			v.UpdateMobBossBar(e, progress)
			b.shown[v] = progress
		}
	}
}

// hide hides the bar from everyone.
func (b *bossBar) hide(e world.Entity) {
	for v := range b.shown {
		v.HideMobBossBar(e)
	}
	b.shown = nil
}

// withinDragonFight is the fight's validPlayer: alive and within 192 of
// 0 128 0 (EnderDragonFight.updatePlayers).
func withinDragonFight(p world.Entity) bool {
	return p.Position().Sub(mgl64.Vec3{0, 128, 0}).LenSqr() <= 192*192
}
