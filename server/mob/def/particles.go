//go:build crossfly_full

package def

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// eventParticles are the particles of emit_particle event responses by
// their ParticleType name (ParticleTypeMap, the names the entity files
// use); the vanilla files only use creakingcrumble. Java has no
// block-free crumble particle (its creaking uses block_crumble with a
// block state), so Java clients see none.
var eventParticles = map[string]world.Particle{
	"creakingcrumble": particle.Legacy{Bedrock: packet.ParticleTypeCreakingCrumble},
}

// eventParticle returns the particle of an emit_particle response.
func eventParticle(name string) (world.Particle, bool) {
	p, ok := eventParticles[name]
	return p, ok
}
