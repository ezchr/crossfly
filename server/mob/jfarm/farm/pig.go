//go:build crossfly_full

package farm

import (
	"math"
	"math/rand/v2"
)

// Steering is ItemBasedSteering: the carrot on a stick (pig) and warped fungus on a stick (strider)
// boost. The boost time is synced to Java clients as Pig.DATA_BOOST_TIME.
type Steering struct {
	boosting  bool
	boostTime int32
	// total: DATA_BOOST_TIME, 140 to 980 ticks.
	total int32
}

// Boost is ItemBasedSteering.boost: false while a boost runs, else a new boost of 140 to 980 ticks
// (random.nextInt(841) + 140). The caller damages the stick (FoodOnAStickItem: 7 for a pig) and sends
// the new boost time.
func (s *Steering) Boost(r *rand.Rand) bool {
	if s.boosting {
		return false
	}
	s.boosting = true
	s.boostTime = 0
	s.total = int32(r.IntN(841) + 140)
	return true
}

// BoostTime is the DATA_BOOST_TIME value.
func (s *Steering) BoostTime() int32 { return s.total }

// Boosting reports whether a boost runs.
func (s *Steering) Boosting() bool { return s.boosting }

// Tick is ItemBasedSteering.tickBoost, run while the mob is ridden by its controller (Pig.tickRidden).
func (s *Steering) Tick() {
	if s.boosting {
		t := s.boostTime
		s.boostTime++
		if t > s.total {
			s.boosting = false
		}
	}
}

// Factor is ItemBasedSteering.boostFactor: 1 + 1.15 * sin(t / total * pi) while boosting.
func (s *Steering) Factor() float64 {
	if !s.boosting || s.total == 0 {
		return 1
	}
	return float64(1 + 1.15*float32(math.Sin(float64(float32(s.boostTime)/float32(s.total)*math.Pi))))
}

// PigRiddenSpeed is Pig.getRiddenSpeed: movement speed (0.25) * 0.225 * the boost factor; the ridden
// input is always straight ahead (Pig.getRiddenInput) and the pig turns to the controller's yaw, pitch
// halved (Pig.tickRidden).
func PigRiddenSpeed(movementSpeed float64, s *Steering) float64 {
	return float64(float32(movementSpeed * 0.225 * s.Factor()))
}

// CarrotOnAStickDamage is the durability a boost costs (Items.CARROT_ON_A_STICK: FoodOnAStickItem(7)).
const CarrotOnAStickDamage = 7
