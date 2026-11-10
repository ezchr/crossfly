package javasession

import (
	"github.com/df-mc/dragonfly/server/world"
)

// dragonID is the ender dragon's Bedrock identifier.
const dragonID = "minecraft:ender_dragon"

// dragonParts is the number of EnderDragonPart entities a Java client gives the dragon (head,
// neck, body, three tail segments, two wings), numbered right after the dragon's own id.
const dragonParts = 8

// dragonPartHit resolves an attack on a dragon part id: the Java client only picks the parts,
// never the dragon itself. The dragon is told which part was hit (head and neck take full
// damage) through SetHitPart, if it keeps parts.
func (s *Session) dragonPartHit(tx *world.Tx, id int32) (world.Entity, bool) {
	for part := range dragonParts {
		e, ok := s.entityByID(tx, id-int32(part)-1)
		if !ok {
			continue
		}
		if e.H().Type().EncodeEntity() != dragonID {
			return nil, false
		}
		if p, ok := e.(interface{ SetHitPart(int) }); ok {
			p.SetHitPart(part)
		}
		return e, true
	}
	return nil, false
}
