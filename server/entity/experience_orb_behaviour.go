package entity

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// ExperienceOrbBehaviourConfig holds optional parameters for the creation of
// an ExperienceOrbBehaviour.
type ExperienceOrbBehaviourConfig struct {
	// Gravity is the amount of Y velocity subtracted every tick.
	Gravity float64
	// Drag is used to reduce all axes of the velocity every tick. Velocity is
	// multiplied with (1-Drag) every tick.
	Drag float64
	// ExistenceDuration specifies how long the experience orb should last. The
	// default is time.Minute * 5.
	ExistenceDuration time.Duration
	// Experience is the amount of experience held by the orb. Default is 1.
	Experience int
}

func (conf ExperienceOrbBehaviourConfig) Apply(data *world.EntityData) {
	data.Data = conf.New()
}

// New creates an ExperienceOrbBehaviour using the parameters in conf.
func (conf ExperienceOrbBehaviourConfig) New() *ExperienceOrbBehaviour {
	if conf.Experience == 0 {
		conf.Experience = 1
	}
	if conf.ExistenceDuration == 0 {
		conf.ExistenceDuration = time.Minute * 5
	}
	b := &ExperienceOrbBehaviour{conf: conf}
	b.passive = PassiveBehaviourConfig{
		Gravity:           conf.Gravity,
		Drag:              conf.Drag,
		ExistenceDuration: conf.ExistenceDuration,
		Tick:              b.tick,
	}.New()
	return b
}

// ExperienceOrbBehaviour implements Behaviour for an experience orb entity.
type ExperienceOrbBehaviour struct {
	conf    ExperienceOrbBehaviourConfig
	passive *PassiveBehaviour

	lastSearch time.Time
	target     *world.EntityHandle
	mergeTicks int
}

// PortalTravelComputer returns the interdimensional travel state for the behaviour.
func (exp *ExperienceOrbBehaviour) PortalTravelComputer() *PortalTravelComputer {
	return exp.passive.PortalTravelComputer()
}

// Experience returns the amount of experience the orb carries.
func (exp *ExperienceOrbBehaviour) Experience() int {
	return exp.conf.Experience
}

// Tick finds a target for the experience orb and moves the orb towards it.
func (exp *ExperienceOrbBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	return exp.passive.Tick(e, tx)
}

// followBox is the bounding box used to search for collectors to follow for experience orbs.
var followBox = cube.Box(-8, -8, -8, 8, 8, 8)

// OrbMergeRadius, when over 0, is how far apart (in blocks) experience orbs merge into one
// carrying their total, as Paper's merge-radius for experience (3 by default there). 0 keeps
// vanilla Bedrock behaviour here: orbs never merge. Set it before the server starts.
var OrbMergeRadius float64

// tryMerge merges e into another orb within OrbMergeRadius: the other orb takes e's experience,
// keeping its place, motion and target (Paper's ExperienceOrb merging), and e is closed. It reports
// whether e was merged. Replacing both with a new orb, as this did first, gave a resting pair a new
// random hop and a new orb's search delay at every merge, so a pile of orbs kept hopping and never
// flew to a player (2026-11-19).
func (exp *ExperienceOrbBehaviour) tryMerge(e *Ent, tx *world.Tx) bool {
	pos := e.Position()
	for other := range tx.EntitiesWithin(cube.Box(-OrbMergeRadius, -OrbMergeRadius, -OrbMergeRadius, OrbMergeRadius, OrbMergeRadius, OrbMergeRadius).Translate(pos)) {
		if other.H() == e.H() || other.H().Type() != ExperienceOrbType {
			continue
		}
		o, ok := other.(*Ent)
		if !ok {
			continue
		}
		ob, ok := o.Behaviour().(*ExperienceOrbBehaviour)
		if !ok || o.Position().Sub(pos).Len() > OrbMergeRadius {
			continue
		}
		ob.conf.Experience += exp.conf.Experience
		exp.conf.Experience = 0
		_ = e.Close()
		o.updateState() // its size shows its new value
		return true
	}
	return false
}

// tick finds a target for the experience orb and moves the orb towards it.
func (exp *ExperienceOrbBehaviour) tick(e *Ent, tx *world.Tx) {
	if OrbMergeRadius > 0 {
		exp.mergeTicks++
		if exp.mergeTicks%10 == 1 && exp.tryMerge(e, tx) {
			return
		}
	}
	targetEnt, ok := exp.target.Entity(tx)
	target, _ := targetEnt.(experienceCollector)

	pos := e.Position()
	hasTarget := ok && !target.Dead() && pos.Sub(target.Position()).Len() <= 8
	if !hasTarget && time.Since(exp.lastSearch) >= time.Second {
		exp.findTarget(tx, pos)
	} else if hasTarget {
		exp.moveToTarget(e, target)
	}
}

// findTarget attempts to find a target for an experience orb in w around pos.
func (exp *ExperienceOrbBehaviour) findTarget(tx *world.Tx, pos mgl64.Vec3) {
	exp.target = nil
	best := math.MaxFloat64
	for o := range tx.EntitiesWithin(followBox.Translate(pos)) {
		if ec, ok := o.(experienceCollector); ok && canFollow(ec) {
			if d := o.Position().Sub(pos).LenSqr(); d < best {
				exp.target, best = o.H(), d
			}
		}
	}
	exp.lastSearch = time.Now()
}

// moveToTarget applies velocity to the experience orb so that it moves towards
// its current target. If it intersects with the target, the orb is collected.
func (exp *ExperienceOrbBehaviour) moveToTarget(e *Ent, target experienceCollector) {
	pos, dst := e.Position(), target.Position()
	if o, ok := target.(Eyed); ok {
		dst[1] += o.EyeHeight() / 2
	}
	diff := dst.Sub(pos).Mul(0.125)
	if dist := diff.LenSqr(); dist < 1 && dist > 0 {
		e.SetVelocity(e.Velocity().Add(diff.Normalize().Mul(0.2 * math.Pow(1-math.Sqrt(dist), 2))))
	}

	if e.H().Type().BBox(e).Translate(pos).IntersectsWith(target.H().Type().BBox(target).Translate(target.Position())) && target.CollectExperience(exp.conf.Experience) {
		_ = e.Close()
	}
}

// canFollow reports whether an orb may fly to ec: alive and in a game mode that interacts (not a
// spectator), whatever the 100 ms between pickups.
func canFollow(ec experienceCollector) bool {
	if ec.Dead() {
		return false
	}
	if g, ok := ec.(interface{ GameMode() world.GameMode }); ok && !g.GameMode().AllowsInteraction() {
		return false
	}
	return true
}

// experienceCollector represents an entity that can collect experience orbs.
type experienceCollector interface {
	Living
	// CollectExperience makes the player collect the experience points passed,
	// adding it to the experience manager. A bool is returned indicating
	// whether the player was able to collect the experience, or not, due to the
	// 100ms delay between experience collection.
	CollectExperience(value int) bool
	// CanCollectExperience returns whether the player can collect experience or not.
	CanCollectExperience() bool
}
