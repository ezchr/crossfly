package player

import (
	"math"
	"runtime"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type testExplosion struct{ pos mgl64.Vec3 }

func (e testExplosion) Position() mgl64.Vec3 { return e.pos }
func (e testExplosion) Size() float64        { return 4 }

func finitePlayer(t *testing.T, p *Player) {
	t.Helper()
	if !finiteVec3(p.Position()) || !finiteVec3(p.Velocity()) || !finite(p.Health()) || !finite(p.MaxHealth()) ||
		!finite(p.Absorption()) || !finite(p.Scale()) || !finite(p.Rotation()[0]) || !finite(p.Rotation()[1]) {
		t.Fatalf("non-finite state: pos %v vel %v health %v/%v absorption %v scale %v rot %v",
			p.Position(), p.Velocity(), p.Health(), p.MaxHealth(), p.Absorption(), p.Scale(), p.Rotation())
	}
}

func TestPlayerRefusesNonFinite(t *testing.T) {
	nan, inf := math.NaN(), math.Inf(1)
	spearWorld(t, world.GameModeSurvival, func(tx *world.Tx, p *Player) {
		p.Teleport(mgl64.Vec3{nan, 0, 0})
		p.Move(mgl64.Vec3{0, inf, 0}, 0, 0)
		p.Move(mgl64.Vec3{}, nan, 0)
		p.SetVelocity(mgl64.Vec3{0, 0, inf})
		p.KnockBack(mgl64.Vec3{nan, 0, 0}, 0.4, 0.4)
		p.KnockBack(mgl64.Vec3{}, inf, 0.4)
		p.SetScale(nan)
		p.SetSpeed(nan)
		p.SetFlightSpeed(inf)
		p.Hurt(nan, entity.VoidDamageSource{})
		p.Heal(nan, entity.FoodHealingSource{})
		p.SetAbsorption(nan)
		p.SetMaxHealth(nan)
		finitePlayer(t, p)
		if !finite(p.Speed()) || !finite(p.FlightSpeed()) {
			t.Fatalf("speed %v, flight speed %v", p.Speed(), p.FlightSpeed())
		}
		// An explosion right at the player's position.
		p.Explode(testExplosion{p.Position()}, 1)
		finitePlayer(t, p)
	})
}

func TestPlayerLongMoveIsQuick(t *testing.T) {
	spearWorld(t, world.GameModeSurvival, func(tx *world.Tx, p *Player) {
		// The ground check of a long move looks only at its end: checking the whole
		// box of a 2000-block move would load about 16000 chunks.
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		start := time.Now()
		p.Move(mgl64.Vec3{2000, 0, 2000}, 0, 0)
		runtime.ReadMemStats(&after)
		if d := time.Since(start); d > 2*time.Second {
			t.Fatalf("a long move took %v", d)
		}
		if grew := int64(after.TotalAlloc - before.TotalAlloc); grew > 64<<20 {
			t.Fatalf("a long move allocated %d MB", grew>>20)
		}
	})
}

func TestExhaustHugeValues(t *testing.T) {
	for _, v := range []float64{math.Inf(1), math.NaN(), 1e300} {
		h := newHungerManager()
		done := make(chan struct{})
		go func() { defer close(done); h.exhaust(v) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("exhaust(%v) hung", v)
		}
		if math.IsNaN(h.exhaustionLevel) || h.exhaustionLevel >= 4 {
			t.Fatalf("exhaust(%v) left exhaustion %v", v, h.exhaustionLevel)
		}
	}
}
