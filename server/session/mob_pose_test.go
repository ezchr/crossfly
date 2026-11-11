package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// attackBehaviour is the behaviour of an *entity.Ent mob chasing a target
// (a plugin's own vindicator).
type attackBehaviour struct {
	angry  bool
	target *world.EntityHandle
}

func (attackBehaviour) Tick(*entity.Ent, *world.Tx) *entity.Movement { return nil }
func (b attackBehaviour) Angry() bool                                { return b.angry }
func (b attackBehaviour) TargetEntity() *world.EntityHandle          { return b.target }

// TestBehaviourAttackPose: a mob whose behaviour is angry and has a target
// gets the angry flag and the target actor data. The Bedrock vindicator
// shows its arms and axe only when angry (query.is_angry) and raises the axe
// and swings it only with a target (variable.has_target in
// controller.animation.vindicator.base); the piglin brute needs the target
// for its swing too.
func TestBehaviourAttackPose(t *testing.T) {
	s := newTestSession()
	m := &fakeMob{h: world.EntitySpawnOpts{}.New(&fakeMobType{id: "minecraft:vindicator"}, fakeMobConfig{})}
	target := world.EntitySpawnOpts{}.New(newFakeMobType(t), fakeMobConfig{})
	s.et.ids[target], s.et.entities[42] = 42, target
	e := struct {
		world.Entity // only the world.Entity methods of the fake: the behaviour has the rest
		behaviour2
	}{m, behaviour2{attackBehaviour{angry: true, target: target}}}

	md := s.parseEntityMetadata(e)
	if !md.Flag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagAngry) {
		t.Error("angry flag of the behaviour not set")
	}
	if got := md[protocol.EntityDataKeyTarget]; got != int64(42) {
		t.Errorf("target %#v, want 42", got)
	}

	e.behaviour2 = behaviour2{attackBehaviour{}}
	md = s.parseEntityMetadata(e)
	if md.Flag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagAngry) {
		t.Error("calm mob sent angry")
	}
	if got := md[protocol.EntityDataKeyTarget]; got != int64(0) {
		t.Errorf("calm mob target %#v, want 0", got)
	}
}

type behaviour2 struct{ b attackBehaviour }

func (b behaviour2) Behaviour() entity.Behaviour { return b.b }
