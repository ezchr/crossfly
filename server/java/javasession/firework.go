package javasession

import (
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	jitem "github.com/df-mc/dragonfly/server/java/protocol/item"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/java/protocol/wire"
	"github.com/df-mc/dragonfly/server/world"
)

// Firework rockets. A Java client boosts its own player itself: the rocket entity carries the id of
// the entity it is attached to (FireworkRocketEntity.DATA_ATTACHED_TO_TARGET), and while that
// entity glides the client pushes it every tick the rocket lives. The server pushes no one (nor does
// FireworkBehaviour, which only keeps the rocket at its owner). The rocket's item carries the
// explosions the client draws when it explodes (EntityEvent 17).

// Entity data of a firework_rocket (Entity's, then FireworkRocketEntity's).
const (
	dataSilent             = 4  // Entity.DATA_SILENT
	dataFireworkItem       = 8  // FireworkRocketEntity.DATA_ID_FIREWORKS_ITEM
	dataFireworkAttachedTo = 9  // FireworkRocketEntity.DATA_ATTACHED_TO_TARGET
	dataTypeOptUnsignedInt = 19 // OPTIONAL_UNSIGNED_INT: the value plus one, 0 for none

	entityEventFireworksExplode = 17
)

// viewFirework sends a firework rocket's data: its item, the entity it is attached to (the player
// itself, or another player this client sees) and that it is silent. Silent, as the client would
// otherwise play the blasts of its explosion again: the server plays them for every edition.
func (s *Session) viewFirework(fb *entity.FireworkBehaviour, id int32) {
	w := s.packet()
	w.VarInt(id)
	w.Byte(dataSilent)
	w.VarInt(dataTypeBoolean)
	w.Bool(true)
	w.Byte(dataFireworkItem)
	w.VarInt(dataTypeItemStack)
	s.writeStack(w, item.NewStack(fb.Firework(), 1))
	if attached, ok := s.fireworkTarget(fb); ok {
		w.Byte(dataFireworkAttachedTo)
		w.VarInt(dataTypeOptUnsignedInt)
		w.VarInt(attached + 1)
	}
	w.Byte(0xff)
	s.queue(v777.ClientboundPlaySetEntityData, w)
}

// fireworkTarget is the client's id of the entity a rocket is attached to, if it is attached and
// the client knows that entity.
func (s *Session) fireworkTarget(fb *entity.FireworkBehaviour) (int32, bool) {
	owner := fb.Owner()
	if !fb.Attached() || owner == nil {
		return 0, false
	}
	if owner == s.ent {
		return selfEntityID, true
	}
	s.entMu.Lock()
	defer s.entMu.Unlock()
	id, ok := s.entityIDs[owner]
	return id, ok
}

// viewFireworkExplosion makes the client draw a rocket's explosion, from the explosions of the item
// it was sent with (FireworkRocketEntity.handleEntityEvent).
func (s *Session) viewFireworkExplosion(e world.Entity, id int32) {
	if ent, ok := e.(*entity.Ent); !ok {
		return
	} else if _, ok := ent.Behaviour().(*entity.FireworkBehaviour); !ok {
		return
	}
	w := s.packet()
	w.Int32(id)
	w.Byte(entityEventFireworksExplode)
	s.queue(v777.ClientboundPlayEntityEvent, w)
}

// javaFireworkColours are the Java firework colours of the dye colours (DyeColor.fireworkColor), by
// item.Colour.Uint8, which counts the dyes in Java's order.
var javaFireworkColours = [16]int32{
	15790320, 15435844, 12801229, 6719955, 14602026, 4312372, 14188952, 4408131,
	11250603, 2651799, 8073150, 2437522, 5320730, 3887386, 11743532, 1973019,
}

// javaFlightDuration is the Java flight duration (Fireworks.flightDuration) of a firework: Bedrock's
// Flight byte, which item.Firework keeps as (Flight+1)*500ms.
func javaFlightDuration(f item.Firework) int32 {
	return max(0, int32(f.Duration/(500*time.Millisecond))-1)
}

// fireworksComponent sets the minecraft:fireworks component of a firework rocket: its flight duration
// and explosions. A rocket that is Java's default one (flight 1, no explosions) is sent without it.
func fireworksComponent(f item.Firework, js *jitem.Stack) {
	flight := javaFlightDuration(f)
	if flight == 1 && len(f.Explosions) == 0 {
		return
	}
	var w wire.Writer
	w.VarInt(flight)
	n := min(len(f.Explosions), 256)
	w.VarInt(int32(n))
	for _, ex := range f.Explosions[:n] {
		w.VarInt(int32(ex.Shape.Uint8())) // the shapes are numbered alike
		w.VarInt(1)
		w.Int32(javaFireworkColour(ex.Colour))
		if ex.Fades {
			w.VarInt(1)
			w.Int32(javaFireworkColour(ex.Fade))
		} else {
			w.VarInt(0)
		}
		w.Bool(ex.Trail)
		w.Bool(ex.Twinkle)
	}
	js.Raw = append(js.Raw, jitem.RawComponent{Type: jitem.CompFireworks, Data: w.B})
}

func javaFireworkColour(c item.Colour) int32 {
	return javaFireworkColours[c.Uint8()&15]
}
