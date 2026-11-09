//go:build crossfly_full

package locomotion

import (
	"encoding/json"
	"math"
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/physics"
	"github.com/go-gl/mathgl/mgl64"
)

// moveMods are the movement modifiers of a mob, applied around the travel
// step of package physics: a system at the end of StageControls records the
// velocity and ground state before travel, one right after the built-in
// travel step changes the result.
type moveMods struct {
	friction, airDrag float64 // 1 = unchanged
	uniformDrag       bool
	bounce            float64
	buoyancy          *buoyant
	climber           bool

	preVel    mgl64.Vec3
	preGround bool
	set       uint8 // components using this
}

type modsKey struct{}

func modsOf(m *mob.Mob) *moveMods {
	if v, ok := m.Data(modsKey{}); ok {
		return v.(*moveMods)
	}
	s := &moveMods{friction: 1, airDrag: 1}
	m.SetData(modsKey{}, s)
	return s
}

// hasMods returns the mob's modifiers if any component set them.
func hasMods(m *mob.Mob) (*moveMods, bool) {
	v, ok := m.Data(modsKey{})
	if !ok {
		return nil, false
	}
	s := v.(*moveMods)
	return s, s.set > 0
}

func beforeTravel(m *mob.Mob) {
	if s, ok := hasMods(m); ok {
		s.preVel, s.preGround = m.Vel(), m.OnGround()
	}
}

func afterTravel(m *mob.Mob) {
	s, ok := hasMods(m)
	if !ok {
		return
	}
	p := m.Physics()
	liquid := p.InWater || p.InLava
	v := m.Vel()
	if !liquid && s.friction != 1 {
		// travel multiplied the horizontal velocity by f (block friction x
		// 0.91 on the ground, 0.91 in the air); BDS uses f x value. The
		// result is kept below 1 so a value above 1 cannot speed the mob up
		// without end.
		f := 0.91
		if s.preGround {
			f = blockFriction(m)
		}
		k := math.Min(f*s.friction, 0.98) / f
		v[0] *= k
		v[2] *= k
	}
	if !liquid && !p.OnGround && s.airDrag != 1 {
		keep := mgl64.Clamp(1-(1-0.91)*s.airDrag, 0, 1)
		v[0] *= keep / 0.91
		v[2] *= keep / 0.91
		if s.uniformDrag {
			v[1] *= keep / 0.98
		}
	} else if !liquid && !p.OnGround && s.uniformDrag {
		v[1] *= 0.91 / 0.98
	}
	if s.bounce > 0 && p.OnGround && !s.preGround && s.preVel[1] < -0.1 && !liquid {
		v[1] = -s.preVel[1] * s.bounce
		p.OnGround = false
	}
	if s.buoyancy != nil {
		v = s.buoyancy.apply(m, v)
	}
	if s.climber && climbableAt(m) {
		m.ResetFallDistance()
	}
	m.SetVelocity(v)
}

// valueComponent sets one modifier while active.
type valueComponent struct {
	set func(s *moveMods, v float64)
	v   float64
	def float64
}

func (c valueComponent) Attach(m *mob.Mob) {
	s := modsOf(m)
	c.set(s, c.v)
	s.set++
}

func (c valueComponent) Detach(m *mob.Mob) {
	s := modsOf(m)
	c.set(s, c.def)
	s.set--
}

func newValue(set func(s *moveMods, v float64), def float64) func(string, json.RawMessage) (mob.Component, error) {
	return func(_ string, raw json.RawMessage) (mob.Component, error) {
		v := struct {
			Value *float64 `json:"value"`
		}{}
		if err := decode(raw, &v); err != nil {
			return nil, err
		}
		val := def
		if v.Value != nil {
			val = *v.Value
		}
		return valueComponent{set: set, v: val, def: def}, nil
	}
}

// newFrictionModifier is minecraft:friction_modifier {value} (default 1,
// BDS FrictionModifierComponent): the travel friction is multiplied by it.
var newFrictionModifier = newValue(func(s *moveMods, v float64) { s.friction = math.Max(v, 0) }, 1)

// newAirDragModifier is minecraft:air_drag_modifier {value} (1.26, sulfur
// cube; not in BDS 1.21.3): the horizontal air drag (1 - 0.91) is scaled by
// value.
var newAirDragModifier = newValue(func(s *moveMods, v float64) { s.airDrag = math.Max(v, 0) }, 1)

// newBounciness is minecraft:bounciness {value} (1.26, sulfur cube): landing
// faster than 0.1 a tick bounces the mob back up at value x the landing
// speed.
var newBounciness = newValue(func(s *moveMods, v float64) { s.bounce = math.Max(v, 0) }, 0)

// newUniformAirDrag is minecraft:uses_uniform_air_drag (1.26, sulfur cube):
// the air drag applies to the vertical speed too, instead of 0.98.
func newUniformAirDrag(string, json.RawMessage) (mob.Component, error) {
	return valueComponent{set: func(s *moveMods, v float64) { s.uniformDrag = v != 0 }, v: 1, def: 0}, nil
}

// newBlockClimber is minecraft:block_climber (BDS BlockClimberSystem): a mob
// in or over powder snow it can stand on (can_stand_on_powder_snow or the
// lightweight family) takes no fall damage. Scaffolding is climbable for
// players only; Dragonfly has neither block, so only worlds with powder snow
// by name are affected. The navigation reads it for powder snow paths.
func newBlockClimber(string, json.RawMessage) (mob.Component, error) {
	return valueComponent{set: func(s *moveMods, v float64) { s.climber = v != 0 }, v: 1, def: 0}, nil
}

// climbableAt is BlockClimberSystem's climbable test at the feet and below.
func climbableAt(m *mob.Mob) bool {
	if !m.HasComponent("minecraft:can_stand_on_powder_snow") && !m.HasFamily("lightweight") {
		return false
	}
	tx := m.Tx()
	box := m.BBox()
	y := int(math.Floor(box.Min()[1]))
	for x := int(math.Floor(box.Min()[0])); x <= int(math.Floor(box.Max()[0])); x++ {
		for z := int(math.Floor(box.Min()[2])); z <= int(math.Floor(box.Max()[2])); z++ {
			for _, dy := range [2]int{0, -1} {
				p := cube.Pos{x, y + dy, z}
				if !inRange(tx, p) {
					continue
				}
				if name, _ := tx.Block(p).EncodeBlock(); name == "minecraft:powder_snow" {
					return true
				}
			}
		}
	}
	return false
}

// buoyant is minecraft:buoyant (BDS BuoyancyComponent and the buoyancy
// systems): in one of liquid_blocks the mob floats at the surface, bobbing
// with waves.
//
//	base_buoyancy 1, apply_gravity true, simulate_waves true,
//	big_wave_probability 0.03, big_wave_speed 10,
//	drag_down_on_buoyancy_removed 0, liquid_blocks. 1.26 movement_type
//	"none" turns the waves off.
//
// A wave timer grows by (horizontal speed x 30 + 1) x 0.05 a tick (x
// big_wave_speed with big_wave_probability). With its feet under the surface
// of a liquid block that has no liquid above, vy = min((b - w - 0.1) x 0.15,
// vy x 0.7 + 0.05) with b = clamp((base - depth into the block) x 0.9 + 0.1)
// and w = (sin(timer) + 1) x 0.035; deeper down it rises at up to 0.135.
// apply_gravity adds BDS's buoyancy gravity ((vy - 0.04) x 0.98) only for
// mobs travel does not already pull down. Removing the component drags the
// mob down by drag_down_on_buoyancy_removed once.
type buoyant struct {
	base        float64
	gravity     bool
	waves       bool
	bigProb     float64
	bigSpeed    float64
	dragDown    float64
	water, lava bool
	timer       float64
}

func newBuoyant(_ string, raw json.RawMessage) (mob.Component, error) {
	j := struct {
		Base     *float64 `json:"base_buoyancy"`
		Gravity  *bool    `json:"apply_gravity"`
		Waves    *bool    `json:"simulate_waves"`
		BigProb  *float64 `json:"big_wave_probability"`
		BigSpeed *float64 `json:"big_wave_speed"`
		DragDown float64  `json:"drag_down_on_buoyancy_removed"`
		Liquids  []string `json:"liquid_blocks"`
		Movement string   `json:"movement_type"`
	}{}
	if err := decode(raw, &j); err != nil {
		return nil, err
	}
	b := &buoyant{base: 1, gravity: true, waves: true, bigProb: 0.03, bigSpeed: 10, dragDown: j.DragDown}
	if j.Base != nil {
		b.base = *j.Base
	}
	if j.Gravity != nil {
		b.gravity = *j.Gravity
	}
	if j.Waves != nil {
		b.waves = *j.Waves
	}
	if strings.EqualFold(j.Movement, "none") {
		b.waves = false
	}
	if j.BigProb != nil {
		b.bigProb = *j.BigProb
	}
	if j.BigSpeed != nil {
		b.bigSpeed = *j.BigSpeed
	}
	for _, l := range j.Liquids {
		switch strings.TrimPrefix(l, "minecraft:") {
		case "water", "flowing_water":
			b.water = true
		case "lava", "flowing_lava":
			b.lava = true
		}
	}
	if len(j.Liquids) == 0 {
		b.water = true
	}
	return b, nil
}

func (b *buoyant) Attach(m *mob.Mob) {
	s := modsOf(m)
	s.buoyancy = b
	s.set++
}

func (b *buoyant) Detach(m *mob.Mob) {
	s := modsOf(m)
	if s.buoyancy == b {
		s.buoyancy = nil
	}
	s.set--
	if b.dragDown > 0 {
		v := m.Vel()
		v[1] -= b.dragDown
		m.SetVelocity(v)
	}
}

func (b *buoyant) liquid(m *mob.Mob, pos cube.Pos) (float64, bool) {
	tx := m.Tx()
	if !inRange(tx, pos) {
		return 0, false
	}
	l, ok := tx.Liquid(pos)
	if !ok {
		return 0, false
	}
	switch l.(type) {
	case block.Water:
		if !b.water {
			return 0, false
		}
	case block.Lava:
		if !b.lava {
			return 0, false
		}
	default:
		return 0, false
	}
	// The liquid's top: a source fills 8/9 of the block.
	d := 8 - l.LiquidDepth()
	h := float64(d+1) / 9
	if l.LiquidFalling() || d < 0 || d >= 8 {
		h = 1.0 / 9
	}
	return float64(pos[1]) + 1 - h, true
}

func (b *buoyant) apply(m *mob.Mob, v mgl64.Vec3) mgl64.Vec3 {
	r := m.Rand()
	speed := math.Hypot(v[0], v[2])*30 + 1
	if r.Float64() <= b.bigProb {
		speed *= b.bigSpeed
	}
	b.timer += speed * 0.05
	pos := m.Position()
	bp := cube.PosFromVec3(pos)
	top, here := b.liquid(m, bp)
	_, above := b.liquid(m, bp.Side(cube.FaceUp))
	canFloat := here && top > pos[1] && !above
	resurface := here && above
	if !canFloat && !resurface {
		return v
	}
	if b.gravity && !physics.HasGravity(m) {
		v[1] = (v[1] - 0.04) * 0.98
	}
	if canFloat {
		k := mgl64.Clamp((b.base-(pos[1]-float64(bp[1])))*0.9+0.1, 0, 1)
		w := 0.0
		if b.waves {
			w = (math.Sin(b.timer) + 1) * 0.035
		}
		if k != 0 {
			v[1] = math.Min((k-w-0.1)*0.15, v[1]*0.7+0.05)
		}
	} else {
		v[1] = math.Min(0.135, v[1]*0.7+0.05)
	}
	return v
}

// flagComponent is a marker component that sets an actor flag while active
// (gophertunnel EntityDataFlag*, 0 for none).
type flagComponent struct{ flag uint8 }

func (c flagComponent) Attach(m *mob.Mob) {
	if c.flag != 0 {
		m.SetActorFlag(c.flag, true)
	}
}

func (c flagComponent) Detach(m *mob.Mob) {
	if c.flag != 0 {
		m.SetActorFlag(c.flag, false)
	}
}

func newFlag(flag uint8) func(string, json.RawMessage) (mob.Component, error) {
	return func(string, json.RawMessage) (mob.Component, error) { return flagComponent{flag: flag}, nil }
}

var _ = slices.Contains[[]string]
