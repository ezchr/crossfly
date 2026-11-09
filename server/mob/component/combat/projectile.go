//go:build crossfly_full

package combat

import (
	"bytes"
	"encoding/json"
	"math"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/component/combat/sense"
	"github.com/df-mc/dragonfly/server/mob/data"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

// Projectiles from their entity definition: the minecraft:projectile
// component of small_fireball.json, fireball.json, llama_spit.json,
// shulker_bullet.json, thrown_trident.json, ... drives how a mob's
// projectile flies and what it does where it lands (BDS ProjectileComponent,
// ProjectileFactory::shootProjectileFromDefinition and the OnHitSubcomponents).
// The projectiles Dragonfly has entity types of (arrow, snowball, egg,
// potions, ender pearl) and the boss projectiles keep their own code.
//
// A projectile is an entity.Ent with entity.ProjectileBehaviour (block and
// entity collision, the hit callback) wrapped in mobProjectile, which adds
// homing, reflecting when hit and the on_hit effects of the definition.

// projectileSpec is one definition's minecraft:projectile.
type projectileSpec struct {
	id            string
	width, height float64

	power, gravity, inertia float64
	uncBase, uncMult        float64
	// anchor is BDS ProjectileAnchor: 0 the shooter's feet, 1 its eyes
	// ("eye_height"), 2 its middle ("middle").
	anchor int
	offset mgl64.Vec3

	homing        bool
	reflectOnHurt bool
	destroyOnHurt bool
	// pickable: players and projectiles can hit it (BDS
	// ActorCollision::initializePickable; SmallFireball removes it again).
	pickable bool
	hitSound string

	impact     *impactDamage
	effects    []hitEffect
	explode    *hitExplosion
	blockFire  bool // catch_fire: sets the block hit on fire
	fireGrief  bool // catch_fire fire_affected_by_griefing
	stick      bool // stick_in_ground: stays in the block it hit
	particle   string
	partEntity bool
	partOther  bool
}

// impactDamage is on_hit.impact_damage.
type impactDamage struct {
	min, max  float64
	knockback bool
	catchFire bool
	// filter is the entity type the damage is limited to (snowballs hurt
	// only blazes), empty for all.
	filter string
}

// hitEffect is one on_hit.mob_effect effect.
type hitEffect struct {
	name      string
	amplifier int
	// ticks by difficulty: easy (and peaceful), normal, hard.
	ticks [3]int
}

// hitExplosion is the explosion a definition_event on_hit sets off (the
// ghast fireball adds a minecraft:explode group with fuse 0).
type hitExplosion struct {
	power                          float64
	fire, fireGrief, blocks, grief bool
}

// notPickable lists the projectiles BDS does not let players hit although
// their JSON reflects on hurt: SmallFireball's constructor removes the
// PickComponent its Fireball base adds.
var notPickable = map[string]bool{"minecraft:small_fireball": true}

// pickableClass lists the BDS projectile classes that call
// ActorCollision::initializePickable (Fireball, LlamaSpit, ShulkerBullet).
var pickableClass = map[string]bool{
	"minecraft:fireball":       true,
	"minecraft:small_fireball": true,
	"minecraft:llama_spit":     true,
	"minecraft:shulker_bullet": true,
}

var (
	specsOnce sync.Once
	specs     map[string]*projectileSpec
)

// projectileSpecOf returns the minecraft:projectile of definition id, or nil.
func projectileSpecOf(id string) *projectileSpec {
	specsOnce.Do(loadProjectileSpecs)
	return specs[id]
}

func loadProjectileSpecs() {
	specs = map[string]*projectileSpec{}
	for _, f := range data.Entities() {
		if s, ok := parseProjectileSpec(f.Data); ok {
			specs[s.id] = s
		}
	}
}

// parseProjectileSpec reads the projectile of one entity file.
func parseProjectileSpec(raw []byte) (*projectileSpec, bool) {
	var file struct {
		Entity struct {
			Description struct {
				Identifier string `json:"identifier"`
			} `json:"description"`
			Components      map[string]json.RawMessage            `json:"components"`
			ComponentGroups map[string]map[string]json.RawMessage `json:"component_groups"`
			Events          map[string]json.RawMessage            `json:"events"`
		} `json:"minecraft:entity"`
	}
	if json.Unmarshal(raw, &file) != nil {
		return nil, false
	}
	ent := file.Entity
	pr, ok := ent.Components["minecraft:projectile"]
	if !ok {
		return nil, false
	}
	var v struct {
		Power         *float64        `json:"power"`
		Gravity       *float64        `json:"gravity"`
		Inertia       *float64        `json:"inertia"`
		UncBase       float64         `json:"uncertainty_base"`
		UncMult       float64         `json:"uncertainty_multiplier"`
		Anchor        json.RawMessage `json:"anchor"`
		Offset        []float64       `json:"offset"`
		Homing        bool            `json:"homing"`
		ReflectOnHurt bool            `json:"reflect_on_hurt"`
		DestroyOnHurt bool            `json:"destroy_on_hurt"`
		HitSound      string          `json:"hit_sound"`
		OnHit         struct {
			ImpactDamage *struct {
				Damage    json.RawMessage `json:"damage"`
				Knockback *bool           `json:"knockback"`
				CatchFire bool            `json:"catch_fire"`
				Filter    string          `json:"filter"`
			} `json:"impact_damage"`
			MobEffect *struct {
				Effects []struct {
					Effect         string `json:"effect"`
					Amplifier      int    `json:"amplifier"`
					DurationEasy   int    `json:"durationeasy"`
					DurationNormal int    `json:"durationnormal"`
					DurationHard   int    `json:"durationhard"`
				} `json:"effects"`
			} `json:"mob_effect"`
			DefinitionEvent *struct {
				AffectProjectile bool `json:"affect_projectile"`
				EventTrigger     struct {
					Event string `json:"event"`
				} `json:"event_trigger"`
			} `json:"definition_event"`
			CatchFire *struct {
				FireAffectedByGriefing bool `json:"fire_affected_by_griefing"`
			} `json:"catch_fire"`
			StickInGround *json.RawMessage `json:"stick_in_ground"`
			ParticleOnHit *struct {
				ParticleType string `json:"particle_type"`
				OnEntityHit  bool   `json:"on_entity_hit"`
				OnOtherHit   bool   `json:"on_other_hit"`
			} `json:"particle_on_hit"`
		} `json:"on_hit"`
	}
	if json.Unmarshal(pr, &v) != nil {
		return nil, false
	}
	s := &projectileSpec{
		id:            ent.Description.Identifier,
		width:         0.25,
		height:        0.25,
		power:         or(v.Power, 1.3),
		gravity:       or(v.Gravity, 0.05),
		inertia:       or(v.Inertia, 0.99),
		uncBase:       v.UncBase,
		uncMult:       v.UncMult,
		anchor:        parseAnchor(v.Anchor),
		homing:        v.Homing,
		reflectOnHurt: v.ReflectOnHurt,
		destroyOnHurt: v.DestroyOnHurt,
		hitSound:      v.HitSound,
	}
	s.pickable = pickableClass[s.id] && !notPickable[s.id]
	if len(v.Offset) == 3 {
		s.offset = mgl64.Vec3{v.Offset[0], v.Offset[1], v.Offset[2]}
	}
	if cb, ok := ent.Components["minecraft:collision_box"]; ok {
		var box struct{ Width, Height float64 }
		if json.Unmarshal(cb, &box) == nil && box.Width > 0 && box.Height > 0 {
			s.width, s.height = box.Width, box.Height
		}
	}
	h := v.OnHit
	if d := h.ImpactDamage; d != nil {
		r, err := sense.ParseRange(d.Damage, sense.Range{})
		if err != nil {
			return nil, false
		}
		s.impact = &impactDamage{min: r.Min, max: math.Max(r.Min, r.Max), knockback: d.Knockback == nil || *d.Knockback, catchFire: d.CatchFire, filter: d.Filter}
	}
	if e := h.MobEffect; e != nil {
		for _, x := range e.Effects {
			s.effects = append(s.effects, hitEffect{name: x.Effect, amplifier: x.Amplifier, ticks: [3]int{x.DurationEasy, x.DurationNormal, x.DurationHard}})
		}
	}
	if de := h.DefinitionEvent; de != nil && de.AffectProjectile {
		s.explode = explosionOfEvent(ent.Events[de.EventTrigger.Event], ent.ComponentGroups)
	}
	if cf := h.CatchFire; cf != nil {
		s.blockFire, s.fireGrief = true, cf.FireAffectedByGriefing
	}
	s.stick = h.StickInGround != nil
	if p := h.ParticleOnHit; p != nil {
		s.particle, s.partEntity, s.partOther = p.ParticleType, p.OnEntityHit, p.OnOtherHit
	}
	return s, true
}

// parseAnchor reads anchor: the BDS integer, or the names the 1.26 files use.
func parseAnchor(raw json.RawMessage) int {
	raw = bytes.TrimSpace(raw)
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var name string
	_ = json.Unmarshal(raw, &name)
	switch name {
	case "eye_height":
		return 1
	case "middle":
		return 2
	}
	return 0
}

// explosionOfEvent returns the minecraft:explode an event adds, if any.
func explosionOfEvent(raw json.RawMessage, groups map[string]map[string]json.RawMessage) *hitExplosion {
	var ev struct {
		Add struct {
			ComponentGroups []string `json:"component_groups"`
		} `json:"add"`
	}
	if json.Unmarshal(raw, &ev) != nil {
		return nil
	}
	for _, g := range ev.Add.ComponentGroups {
		ex, ok := groups[g]["minecraft:explode"]
		if !ok {
			continue
		}
		var v struct {
			Power          *float64 `json:"power"`
			CausesFire     bool     `json:"causes_fire"`
			FireGrief      bool     `json:"fire_affected_by_griefing"`
			BreaksBlocks   *bool    `json:"breaks_blocks"`
			DestroyGriefed bool     `json:"destroy_affected_by_griefing"`
		}
		if json.Unmarshal(ex, &v) != nil {
			return nil
		}
		return &hitExplosion{power: or(v.Power, 3), fire: v.CausesFire, fireGrief: v.FireGrief,
			blocks: v.BreaksBlocks == nil || *v.BreaksBlocks, grief: v.DestroyGriefed}
	}
	return nil
}

func or[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

// definedProjectileType is the entity type of a projectile from its
// definition. Bedrock draws it from the identifier, the Java session maps
// the identifier (entity_types.go). Like the boss projectiles, these are
// not saved.
type definedProjectileType struct{ spec *projectileSpec }

func (t definedProjectileType) Open(tx *world.Tx, h *world.EntityHandle, data *world.EntityData) world.Entity {
	return entity.Open(tx, h, data)
}
func (t definedProjectileType) EncodeEntity() string { return t.spec.id }
func (t definedProjectileType) BBox(world.Entity) cube.BBox {
	w := t.spec.width / 2
	return cube.Box(-w, 0, -w, w, t.spec.height, w)
}

// DecodeNBT drops saved projectiles.
func (t definedProjectileType) DecodeNBT(_ map[string]any, data *world.EntityData) {
	data.Data = newMobProjectile(t.spec, nil, nil)
}
func (definedProjectileType) EncodeNBT(*world.EntityData) map[string]any { return nil }

// mobProjectile is the behaviour of a projectile from its definition.
type mobProjectile struct {
	*entity.ProjectileBehaviour
	spec *projectileSpec
	conf entity.ProjectileBehaviourConfig

	// lastVel is the velocity before this tick's move (the projectile
	// stops where it hits).
	lastVel mgl64.Vec3

	// Homing (ProjectileComponent::handleMovementHoming).
	target     *world.EntityHandle
	targetVel  mgl64.Vec3
	steps      int
	moveFace   cube.Face
	hasMove    bool
	homingInit bool
}

// pickableProjectile is a mobProjectile players and projectiles can hit.
type pickableProjectile struct{ *mobProjectile }

// newMobProjectile makes the behaviour of a projectile of spec owned by
// owner, homing in on target when the projectile is homing.
func newMobProjectile(spec *projectileSpec, owner, target *world.EntityHandle) entity.Behaviour {
	p := &mobProjectile{spec: spec, target: target}
	p.conf = entity.ProjectileBehaviourConfig{
		Owner:  owner,
		Damage: -1, // the on_hit effects hurt, not ProjectileBehaviour
		// ProjectileComponent::handleMovementGravity: velocity x inertia,
		// then gravity. Homing projectiles steer themselves.
		Gravity:               spec.gravity,
		Drag:                  1 - spec.inertia,
		SurviveBlockCollision: spec.stick,
		DisablePickup:         true,
		Hit:                   p.hit,
	}
	if spec.homing {
		p.conf.Gravity, p.conf.Drag = 0, 0
	}
	p.ProjectileBehaviour = p.conf.New()
	if spec.pickable {
		return pickableProjectile{p}
	}
	return p
}

// Tick ...
func (p *mobProjectile) Tick(e *entity.Ent, tx *world.Tx) *entity.Movement {
	if p.spec.homing {
		p.steer(e, tx)
	}
	p.lastVel = e.Velocity()
	m := p.ProjectileBehaviour.Tick(e, tx)
	if p.spec.homing {
		p.afterMove(e, tx)
	}
	return m
}

// Hurt is ProjectileComponent::hurt: a mob (or player) hitting the
// projectile, or a projectile hitting it, sends it where the attacker looks
// and makes the attacker its owner (reflect_on_hurt), then destroys it
// (destroy_on_hurt, or always for projectiles). Block explosions destroy
// it.
func (p pickableProjectile) Hurt(e *entity.Ent, _ float64, src world.DamageSource) (float64, bool) {
	var attacker world.Entity
	byProjectile := false
	switch s := src.(type) {
	case entity.AttackDamageSource:
		attacker = s.Attacker
	case entity.ProjectileDamageSource:
		attacker, byProjectile = s.Owner, true
		if attacker == nil {
			attacker = s.Projectile
		}
	case entity.ExplosionDamageSource:
		if _, ent := s.Source.(world.EntityExplosionSource); !ent {
			_ = e.Close()
		}
		return 0, false
	default:
		return 0, false
	}
	_, living := attacker.(entity.Living)
	if !living && !byProjectile {
		return 0, false
	}
	if p.spec.reflectOnHurt && attacker != nil {
		e.SetVelocity(attacker.Rotation().Vec3())
		p.conf.Owner = attacker.H()
		p.ProjectileBehaviour = p.conf.New()
		p.target = nil
	}
	if p.spec.destroyOnHurt || byProjectile {
		tx := e.Tx()
		tx.AddParticle(e.Position(), particle.Named{Bedrock: "minecraft:critical_hit_emitter", Java: "crit"})
		if p.spec.hitSound != "" {
			tx.PlaySound(e.Position(), mob.Sound{Event: "hurt", Identifier: p.spec.id})
		}
		_ = e.Close()
	}
	return 0, true
}

// owner returns the projectile's owner in tx, or nil.
func (p *mobProjectile) owner(tx *world.Tx) world.Entity {
	if h := p.Owner(); h != nil {
		if o, ok := h.Entity(tx); ok {
			return o
		}
	}
	return nil
}

// hit runs the on_hit effects (ImpactDamage, MobEffect, CatchFire,
// ActorDefinitionEvent explode, ParticleOnHit) where the projectile landed.
func (p *mobProjectile) hit(e *entity.Ent, tx *world.Tx, res trace.Result) {
	s := p.spec
	owner := p.owner(tx)
	pos := res.Position()
	switch r := res.(type) {
	case trace.EntityResult:
		victim := r.Entity()
		p.hurtEntity(e, tx, owner, victim)
		if s.particle != "" && s.partEntity {
			tx.AddParticle(pos, hitParticle(s.particle))
		}
	case trace.BlockResult:
		if s.blockFire && (!s.fireGrief || MobGriefing(tx)) {
			block.Fire{}.Start(tx, r.BlockPosition().Side(r.Face()))
		}
		if s.particle != "" && s.partOther {
			tx.AddParticle(pos, hitParticle(s.particle))
		}
	}
	if s.hitSound != "" {
		tx.PlaySound(pos, mob.Sound{Event: s.hitSound, Identifier: s.id})
	}
	if x := s.explode; x != nil {
		fire := x.fire && (!x.fireGrief || MobGriefing(tx))
		blocks := x.blocks && (!x.grief || MobGriefing(tx))
		if blocks {
			block.ExplosionConfig{SpawnFire: fire, Source: e}.Explode(tx, world.EntityExplosionSource{Entity: e, ExplosionSize: x.power})
		} else {
			ExplodeEntitiesOnly(tx, e, x.power)
		}
		_ = e.Close()
	}
}

// hurtEntity is ImpactDamageSubcomponent and MobEffectSubcomponent on the
// entity hit.
func (p *mobProjectile) hurtEntity(e *entity.Ent, tx *world.Tx, owner, victim world.Entity) {
	s := p.spec
	if imp := s.impact; imp != nil && (imp.filter == "" || victim.H().Type().EncodeEntity() == "minecraft:"+imp.filter) {
		dmg := imp.min
		if imp.max > imp.min {
			dmg += rand.Float64() * (imp.max - imp.min)
		}
		// Ghast::_hurt: a fireball a player sent back kills the ghast.
		if s.id == "minecraft:fireball" && sense.IsPlayer(owner) && victim.H().Type().EncodeEntity() == "minecraft:ghast" {
			dmg = 1000
		}
		src := entity.ProjectileDamageSource{Projectile: e, Owner: owner}
		if l, ok := victim.(entity.Living); ok {
			if _, res := l.Hurt(dmg, src); res.Accepted() {
				if imp.knockback {
					l.KnockBack(l.Position().Sub(p.lastVel), 0.45, 0.3608)
				}
				if f, ok := victim.(entity.Flammable); ok && imp.catchFire {
					f.SetOnFire(5 * time.Second)
				}
			}
		} else {
			entity.HurtEntity(victim, dmg, src)
		}
	}
	l, ok := victim.(interface{ AddEffect(effect.Effect) })
	if !ok || len(s.effects) == 0 {
		return
	}
	d := min(max(sense.DifficultyID(tx), 1), 3) - 1
	for _, x := range s.effects {
		t, ok := hitEffectTypes[strings.TrimPrefix(x.name, "minecraft:")]
		if !ok {
			continue
		}
		l.AddEffect(effect.New(t, x.amplifier+1, time.Duration(x.ticks[d])*time.Second/20))
	}
}

// hitEffectTypes are the lasting effects a projectile's mob_effect may give,
// by BDS name (package misc has the full list, but it imports this package).
var hitEffectTypes = map[string]effect.LastingType{
	"levitation": effect.Levitation, "poison": effect.Poison, "wither": effect.Wither,
	"slowness": effect.Slowness, "weakness": effect.Weakness, "hunger": effect.Hunger,
	"nausea": effect.Nausea, "blindness": effect.Blindness, "darkness": effect.Darkness,
	"mining_fatigue": effect.MiningFatigue,
}

// hitParticle is a particle_on_hit particle_type (BDS ParticleType names).
func hitParticle(name string) world.Particle {
	switch name {
	case "largeexplode":
		return particle.Named{Bedrock: "minecraft:large_explosion", Java: "explosion"}
	case "snowballpoof":
		return particle.SnowballPoof{}
	}
	return particle.Named{Bedrock: "minecraft:" + name}
}

// shootDefined is ProjectileFactory::shootProjectileFromDefinition for a
// mob: the projectile starts at the shooter's anchor plus offset, one block
// towards the target, aimed at the middle of the target (lobbed when it
// has gravity), with uncertainty base - multiplier x difficulty. power 0
// uses the projectile's own.
func shootDefined(m *mob.Mob, target world.Entity, s *projectileSpec, power float64) bool {
	if power <= 0 {
		power = s.power
	}
	tx := m.Tx()
	pos := m.Position()
	y0 := pos[1]
	switch s.anchor {
	case 1:
		y0 = m.EyePosition()[1]
	case 2:
		y0 = pos[1] + m.BBox().Height()/2
	}
	// The offset turns with the shooter (ProjectileComponent::getOffset
	// rotated by the yaw).
	yaw := mgl64.DegToRad(m.Yaw())
	sin, cos := math.Sincos(yaw)
	off := mgl64.Vec3{cos*s.offset[0] - sin*s.offset[2], s.offset[1], sin*s.offset[0] + cos*s.offset[2]}

	tb := sense.BBox(target)
	tp := target.Position()
	dx := tp[0] - pos[0] - off[0]
	dy := tb.Min()[1] + tb.Height()/2 - y0 - off[1]
	dz := tp[2] - pos[2] - off[2]
	h := math.Hypot(dx, dz)
	if h < 1e-7 {
		return false
	}
	start := mgl64.Vec3{pos[0] + dx/h + off[0], y0 + off[1], pos[2] + dz/h + off[2]}
	dir := mgl64.Vec3{dx, dy, dz}.Normalize()
	if g := s.gravity; g != 0 && !s.homing {
		// The lower of the two launch angles that reach the target, 45
		// degrees when none does.
		p2 := power * power
		disc := p2*p2 - g*(g*h*h+2*dy*p2)
		theta := math.Pi / 4
		if disc >= 0 {
			sq := math.Sqrt(disc)
			theta = math.Min(math.Atan2(p2-sq, g*h), math.Atan2(p2+sq, g*h))
		}
		c := math.Cos(theta)
		dir = mgl64.Vec3{dx / h * c, math.Sin(theta), dz / h * c}
	}
	unc := math.Max(0, s.uncBase-s.uncMult*float64(sense.DifficultyID(tx)))
	r := m.Rand()
	for i := range dir {
		dir[i] += r.NormFloat64() * 0.0075 * unc
	}
	vel := dir.Normalize().Mul(power)
	rot := cube.Rotation{mgl64.RadToDeg(math.Atan2(-vel[0], vel[2])), -mgl64.RadToDeg(math.Atan2(vel[1], math.Hypot(vel[0], vel[2])))}
	var th *world.EntityHandle
	if s.homing {
		th = target.H()
	}
	opts := world.EntitySpawnOpts{Position: start, Velocity: vel, Rotation: rot}
	tx.AddEntity(opts.New(definedProjectileType{spec: s}, behaviourConfig{newMobProjectile(s, m.H(), th)}))
	return true
}

// behaviourConfig sets an already built behaviour as the entity data.
type behaviourConfig struct{ b entity.Behaviour }

func (c behaviourConfig) Apply(data *world.EntityData) { data.Data = c.b }

// steer is the velocity half of ProjectileComponent::handleMovementHoming:
// towards the next waypoint while the target lives, else falling.
func (p *mobProjectile) steer(e *entity.Ent, tx *world.Tx) {
	t := p.liveTarget(tx)
	if !p.homingInit {
		p.homingInit = true
		if t != nil {
			p.selectMove(e, tx, t, -1)
		}
	}
	v := e.Velocity()
	if t != nil {
		for i := range p.targetVel {
			p.targetVel[i] = math.Max(-1, math.Min(1, p.targetVel[i]*1.025))
		}
		v = v.Add(p.targetVel.Sub(v).Mul(0.2))
	} else {
		v[1] -= 0.04
	}
	e.SetVelocity(v)
}

// afterMove picks a new waypoint when the flight steps ran out, the next
// block is solid or the target's block is reached on the current axis.
func (p *mobProjectile) afterMove(e *entity.Ent, tx *world.Tx) {
	t := p.liveTarget(tx)
	if t == nil {
		return
	}
	axis := -1
	if p.hasMove {
		axis = faceAxis(p.moveFace)
	}
	if p.steps > 0 {
		if p.steps--; p.steps == 0 {
			p.selectMove(e, tx, t, axis)
			return
		}
	}
	if !p.hasMove {
		return
	}
	cur := cube.PosFromVec3(e.Position())
	if next := cur.Side(p.moveFace); tx.Block(next).Model().FaceSolid(next, cube.FaceUp, tx) {
		p.selectMove(e, tx, t, axis)
		return
	}
	tb := cube.PosFromVec3(t.Position())
	if cur[axis] == tb[axis] {
		p.selectMove(e, tx, t, axis)
	}
}

// liveTarget returns the homing target while it is alive and not a
// spectator.
func (p *mobProjectile) liveTarget(tx *world.Tx) world.Entity {
	if p.target == nil {
		return nil
	}
	t, ok := p.target.Entity(tx)
	if !ok || !sense.Alive(t) {
		return nil
	}
	if g, ok := t.(interface{ GameMode() world.GameMode }); ok && !g.GameMode().HasCollision() {
		return nil
	}
	return t
}

// selectMove is ProjectileComponent::_selectNextMoveDirection: a step
// along one axis towards the target (not along avoid), through air.
func (p *mobProjectile) selectMove(e *entity.Ent, tx *world.Tx, t world.Entity, avoid int) {
	pos := e.Position()
	tb := sense.BBox(t)
	ty := math.Hypot(tb.Width(), tb.Height()) * 0.5
	target := cube.PosFromVec3(mgl64.Vec3{t.Position()[0], t.Position()[1] + ty, t.Position()[2]})
	goal := mgl64.Vec3{float64(target[0]) + 0.5, float64(target[1]) + ty, float64(target[2]) + 0.5}
	// Within 2 blocks it heads straight for the target, keeping a random
	// face only for its axis checks.
	face := cube.Face(rand.IntN(6))
	centre := mgl64.Vec3{float64(target[0]) + 0.5, float64(target[1]) + 0.5, float64(target[2]) + 0.5}
	if centre.Sub(pos).LenSqr() >= 4 {
		cur := cube.PosFromVec3(pos)
		var faces []cube.Face
		try := func(axis int, f cube.Face, ok bool) {
			if axis != avoid && ok && isAir(tx, cur.Side(f)) {
				faces = append(faces, f)
			}
		}
		try(0, cube.FaceEast, cur[0] < target[0])
		try(0, cube.FaceWest, cur[0] > target[0])
		try(1, cube.FaceUp, cur[1] < target[1])
		try(1, cube.FaceDown, cur[1] > target[1])
		try(2, cube.FaceSouth, cur[2] < target[2])
		try(2, cube.FaceNorth, cur[2] > target[2])
		if len(faces) == 0 {
			for i := 5; i > 0 && !isAir(tx, cur.Side(face)); i-- {
				face = cube.Face(rand.IntN(6))
			}
		} else {
			face = faces[rand.IntN(len(faces))]
		}
		goal = pos.Add(cube.Pos{}.Side(face).Vec3())
	}
	p.moveFace, p.hasMove = face, true
	d := goal.Sub(pos)
	if l := d.Len(); l == 0 {
		p.targetVel = mgl64.Vec3{}
	} else {
		p.targetVel = d.Mul(0.15 / l)
	}
	p.steps = 10 * (rand.IntN(5) + 1)
}

func isAir(tx *world.Tx, pos cube.Pos) bool {
	_, ok := tx.Block(pos).(block.Air)
	return ok
}

// faceAxis is the index into a position of the axis a face lies on.
func faceAxis(f cube.Face) int {
	switch f {
	case cube.FaceDown, cube.FaceUp:
		return 1
	case cube.FaceNorth, cube.FaceSouth:
		return 2
	}
	return 0
}
