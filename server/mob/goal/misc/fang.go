//go:build crossfly_full

package misc

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/mob/goal/misc/kit"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// FangID is the identifier of the evoker fangs.
const FangID = "minecraft:evocation_fang"

// FangType is minecraft:evocation_fang (BDS EvocationFang): the fangs an
// evoker's spell raises from the ground. Bedrock clients draw it from the
// identifier, Java clients as evoker_fangs (javasession entity_types.go).
// Fangs live for 1.1 seconds and are not saved.
var FangType fangType

// FangTypes returns the entity types of this package, for a server's entity
// registry.
func FangTypes() []world.EntityType { return []world.EntityType{FangType} }

// The fangs join the server's entity registry through mob.Types.
func init() { mob.RegisterEntityType(FangType) }

type fangType struct{}

func (fangType) Open(tx *world.Tx, h *world.EntityHandle, data *world.EntityData) world.Entity {
	return entity.Open(tx, h, data)
}
func (fangType) EncodeEntity() string { return FangID }

// BBox is the 1 x 0.8 box EvocationFang sets in its constructor (no
// minecraft:collision_box overrides it: the fangs have no entity JSON).
func (fangType) BBox(world.Entity) cube.BBox { return cube.Box(-0.5, 0, -0.5, 0.5, 0.8, 0.5) }

// DecodeNBT drops saved fangs.
func (fangType) DecodeNBT(_ map[string]any, data *world.EntityData) { data.Data = &fang{} }
func (fangType) EncodeNBT(*world.EntityData) map[string]any         { return nil }

// fangLife is the lifetime of fangs summoned without entity_lifespan
// (EvocationFang::normalTick sets 22 ticks).
const fangLife = 22

// SpawnFang adds evoker fangs at pos, owned by owner (nil for none), that
// live for life ticks (fangLife if life <= 0).
func SpawnFang(tx *world.Tx, pos mgl64.Vec3, owner world.Entity, life int) *world.EntityHandle {
	if life <= 0 {
		life = fangLife
	}
	f := &fang{life: life}
	if owner != nil {
		f.owner = owner.H()
	}
	h := world.EntitySpawnOpts{Position: pos}.New(FangType, fangConfig{f})
	tx.AddEntity(h)
	return h
}

type fangConfig struct{ f *fang }

func (c fangConfig) Apply(data *world.EntityData) { data.Data = c.f }

// fang is the behaviour of evoker fangs: their limited lifetime (actor data
// LimitedLife) and their bite.
type fang struct {
	owner *world.EntityHandle
	life  int
}

// Owner is the evoker that summoned the fangs.
func (f *fang) Owner() *world.EntityHandle { return f.owner }

// ExtraActorData shows the lifetime left (LimitedLife, which the client's
// fangs count down).
func (f *fang) ExtraActorData() map[uint32]any {
	return map[uint32]any{protocol.EntityDataKeyDataLifetimeTicks: int32(f.life)}
}

// Tick is EvocationFang::normalTick on the server: the lifetime counts
// down; with 20 ticks left the fangs bite every mob and player in their box
// grown by 0.2 sideways (_dealDamageTo), show the attack (actor event 4,
// the jaws closing) and play their sound; at 0 they are gone.
func (f *fang) Tick(e *entity.Ent, tx *world.Tx) *entity.Movement {
	f.life--
	if f.life == 20 {
		f.bite(e, tx)
	}
	if f.life <= 0 {
		_ = e.Close()
	}
	return nil
}

func (f *fang) bite(e *entity.Ent, tx *world.Tx) {
	box := FangType.BBox(e).Translate(e.Position()).GrowVec3(mgl64.Vec3{0.2, 0, 0.2})
	var hit []world.Entity
	// EntitiesWithin goes by position: grown, it finds every entity whose
	// box can meet the fangs' (BlockSource::fetchEntities2).
	for t := range tx.EntitiesWithin(box.Grow(2)) {
		if _, ok := mob.Of(t); (ok || kit.IsPlayer(t)) && kit.Alive(t) && kit.BBox(t).IntersectsWith(box) {
			hit = append(hit, t)
		}
	}
	for _, t := range hit {
		f.dealDamageTo(e, tx, t)
	}
	for _, v := range tx.Viewers(e.Position()) {
		v.ViewEntityAction(e, kit.ActorEvent(packet.ActorEventStartAttacking))
	}
	tx.PlaySound(e.Position(), mob.Sound{Event: "fang", Identifier: FangID})
}

// dealDamageTo is EvocationFang::_dealDamageTo: 6 magic damage (armour does
// not lower it) by the owner, which becomes the victim's last attacker,
// unless the victim is the owner or its ally (EvocationIllager::isAlliedTo);
// fangs whose owner is gone bite everything, by themselves.
func (f *fang) dealDamageTo(e *entity.Ent, tx *world.Tx, t world.Entity) {
	if f.owner != nil && t.H() == f.owner {
		return
	}
	l, ok := t.(entity.Living)
	if !ok {
		return
	}
	var owner world.Entity
	if f.owner != nil {
		owner, _ = f.owner.Entity(tx)
	}
	if owner == nil {
		l.Hurt(6, FangDamageSource{Attacker: e})
		return
	}
	if om, ok := mob.Of(owner); ok && alliedTo(tx, om, t, 0) {
		return
	}
	l.Hurt(6, FangDamageSource{Attacker: owner})
	if tm, ok := mob.Of(t); ok {
		if om, ok := mob.Of(owner); ok {
			tm.SetLastHurtBy(om)
		}
	}
}

// alliedTo is Mob::isAlliedTo as the evoker overrides it
// (EvocationIllager::isAlliedTo): itself, its vexes (a vex is an ally of
// whoever its owner is an ally of) and the illager family. Other mobs have
// no allies.
func alliedTo(tx *world.Tx, m *mob.Mob, t world.Entity, depth int) bool {
	if t == nil || depth > 4 {
		return false
	}
	if t.H() == m.H() {
		return true
	}
	if m.Identifier() != "minecraft:evocation_illager" {
		return false
	}
	tm, ok := mob.Of(t)
	if !ok {
		return false
	}
	if tm.Identifier() == "minecraft:vex" {
		o := tm.Owner()
		if o == nil {
			return false
		}
		oe, ok := o.Entity(tx)
		return ok && alliedTo(tx, m, oe, depth+1)
	}
	return tm.HasFamily("illager")
}

// FangDamageSource is the damage of evoker fangs (ActorDamageCause magic):
// Attacker is the evoker, or the fangs if it is gone.
type FangDamageSource struct{ Attacker world.Entity }

// DamageCause is the Bedrock damage cause name of the bite.
func (FangDamageSource) DamageCause() string { return "magic" }

func (FangDamageSource) ReducedByArmour() bool     { return false }
func (FangDamageSource) ReducedByResistance() bool { return true }
func (FangDamageSource) Fire() bool                { return false }
func (FangDamageSource) IgnoreTotem() bool         { return false }
