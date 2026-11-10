//go:build crossfly_full

package social

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// Dancing at jukeboxes, shared by the allay and the parrot (BDS DanceComponent,
// DanceComponentListener and DanceSystem; the entity files have no component for it, Allay::
// reloadHardcoded and Parrot::reloadHardcodedClient add it).
//
// DanceComponentListener hears the record level sounds: a record sound (a disc starting, or the
// record.playing sound JukeboxBlockActor::tick plays every second) within the listen distance
// of the mob's position makes it dance and remembers where the sound came from; record.null (the
// disc taken out or the song over) within the distance stops it. DanceSystem stops a dancing mob
// once it is farther than the distance from that place. Both clients show the dancing from the
// dancing actor flag (Java's allay from Allay.DATA_DANCING, javasession; the Java client makes
// parrots dance by itself from the jukebox level event).
//
// The jukeboxes drive it through block.JukeboxListener (one entity query per jukebox and second);
// the per-tick check costs one flag test for mobs that do not dance.

func init() {
	mob.RegisterJukeboxHook(jukeboxPlaying)
	mob.RegisterSystem(mob.System{Name: "social/dance", Stage: mob.StageSensors, Tick: danceTick})
}

// allayDanceDistance is the listen distance Allay::reloadHardcoded gives its DanceComponent.
const allayDanceDistance = 10

// parrotDanceDistance is DanceComponentInternal::DEFAULT_DISTANCE (the float 3.4641, sqrt 12),
// the parrot's listen distance.
const parrotDanceDistance = 3.4640999

// danceDistance is the listen distance of the DanceComponent of the entity type id.
func danceDistance(id string) (float64, bool) {
	switch id {
	case "minecraft:allay":
		return allayDanceDistance, true
	case "minecraft:parrot":
		return parrotDanceDistance, true
	}
	return 0, false
}

// danceKey keys the position of the music a mob dances to (mob.Data, a mgl64.Vec3).
type danceKey struct{}

// Dancing reports whether m dances (Actor::isDancing).
func Dancing(m *mob.Mob) bool { return m.ActorFlag(protocol.EntityDataFlagDancing) }

// jukeboxPlaying is DanceComponentListener::_onLevelSoundEvent for the jukebox at pos, whose
// sounds come from the centre of the block.
func jukeboxPlaying(tx *world.Tx, pos cube.Pos, playing bool) {
	c := pos.Vec3Centre()
	const r = allayDanceDistance
	for e := range tx.EntitiesWithin(cube.Box(c[0]-r, c[1]-r, c[2]-r, c[0]+r, c[1]+r, c[2]+r)) {
		m, ok := mob.Of(e)
		if !ok || m.Dead() {
			continue
		}
		d, ok := danceDistance(m.Identifier())
		if !ok || m.Position().Sub(c).Len() > d {
			continue
		}
		if playing {
			m.SetData(danceKey{}, c)
			setDancing(m, true)
		} else {
			setDancing(m, false)
		}
	}
}

// danceTick is tickDanceComponent: a dancing mob stops once it is farther from the music than
// its listen distance (DanceComponent::inListenDistance).
func danceTick(m *mob.Mob) {
	if !Dancing(m) {
		return
	}
	d, ok := danceDistance(m.Identifier())
	v, heard := m.Data(danceKey{})
	if !ok || !heard || m.Position().Sub(v.(mgl64.Vec3)).Len() > d {
		setDancing(m, false)
	}
}

// setDancing is Actor::setDancing.
func setDancing(m *mob.Mob, v bool) {
	if Dancing(m) == v {
		return
	}
	m.SetActorFlag(protocol.EntityDataFlagDancing, v)
	if !v {
		m.SetData(danceKey{}, nil)
	}
}
