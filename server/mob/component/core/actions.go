//go:build crossfly_full

package core

import (
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// Entity actions the passive mobs show. Each is a world.EntityAction with
// the Bedrock actor event (ActorEvent, gophertunnel packet.ActorEvent*) the
// clients translate.
type (
	// EatGrassAction: a sheep lowering its head to eat (eat_block start).
	EatGrassAction struct{}
	// LoveHeartsAction: hearts around a mob entering love mode.
	LoveHeartsAction struct{}
	// FeedAction: crumbs of the item a mob was fed (ageable feed items).
	FeedAction struct{}
)

func (EatGrassAction) EntityAction()     {}
func (EatGrassAction) ActorEvent() uint8 { return packet.ActorEventEatGrass }

func (LoveHeartsAction) EntityAction()     {}
func (LoveHeartsAction) ActorEvent() uint8 { return packet.ActorEventLoveHearts }

func (FeedAction) EntityAction()     {}
func (FeedAction) ActorEvent() uint8 { return packet.ActorEventFeed }

// ShowAction shows an entity action of m to its viewers.
func ShowAction(m *mob.Mob, a world.EntityAction) {
	for _, v := range m.Tx().Viewers(m.Position()) {
		v.ViewEntityAction(m, a)
	}
}

// MobSound is a Bedrock level sound event played by a mob ("ambient",
// "plop", "milk", "shear", "eat", ...), resolved by the client from the
// entity identifier, as BDS sends LevelSoundEvent with the actor type. Both
// client sessions translate it (mob.Sound.LevelSoundEvent).
type MobSound = mob.Sound

// PlayMobSound plays a sound event of m at its position.
func PlayMobSound(m *mob.Mob, event string) {
	if event == "" {
		return
	}
	m.Tx().PlaySound(m.Position(), MobSound{Event: event, Identifier: m.Identifier(), Baby: IsBaby(m)})
}
