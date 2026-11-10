//go:build crossfly_full

package mobtest_test

import (
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob/internal/mobtest"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestMobDamageDifficulty: Player::_hurt scales every damage a mob deals
// (melee, contact, a creeper's explosion) by difficulty, 0 on peaceful,
// but not projectiles. Every hit goes to a fresh player: no tick passes
// between the hits, so a player's hurt cooldown (10 ticks) would take the
// later ones down to what they deal over the last.
func TestMobDamageDifficulty(t *testing.T) {
	w := mobtest.New(t, mobtest.Options{})
	w.Do(func(tx *world.Tx) { w.Floor(tx, 0, 16, block.Stone{}) })
	z := w.Spawn("minecraft:zombie", mgl64.Vec3{12.5, 1, 0.5})
	n := 0
	hurt := func(d world.Difficulty, dmg float64, src func(m world.Entity) world.DamageSource) float64 {
		n++
		p := w.AddPlayer(fmt.Sprintf("target%d", n), mgl64.Vec3{float64(n%10) - 5, 1, float64(n/10) + 3})
		var took float64
		w.Do(func(tx *world.Tx) {
			tx.World().SetDifficulty(d)
			zm, _ := z.Mob(tx)
			pl, _ := p.H().Entity(tx)
			pp := pl.(*player.Player)
			before := pp.Health()
			pp.Hurt(dmg, src(zm))
			took = before - pp.Health()
		})
		return took
	}
	attack := func(m world.Entity) world.DamageSource { return entity.AttackDamageSource{Attacker: m} }
	explosion := func(m world.Entity) world.DamageSource {
		return entity.ExplosionDamageSource{Source: world.EntityExplosionSource{Entity: m}}
	}
	for _, c := range []struct {
		d       world.Difficulty
		in, out float64
	}{
		{world.DifficultyPeaceful, 6, 0},
		{world.DifficultyEasy, 6, 4},
		{world.DifficultyEasy, 2, 2},
		{world.DifficultyNormal, 6, 6},
		{world.DifficultyHard, 6, 9},
	} {
		for name, src := range map[string]func(world.Entity) world.DamageSource{"attack": attack, "explosion": explosion} {
			if got := hurt(c.d, c.in, src); got != c.out {
				t.Errorf("%v %s of %v: took %v, want %v", c.d, name, c.in, got, c.out)
			}
		}
	}
	projectile := func(m world.Entity) world.DamageSource { return entity.ProjectileDamageSource{Projectile: m, Owner: m} }
	if got := hurt(world.DifficultyHard, 4, projectile); got != 4 {
		t.Errorf("hard projectile of 4 took %v, want 4", got)
	}
}
