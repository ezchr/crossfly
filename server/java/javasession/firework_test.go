package javasession

import (
	"bytes"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	jitem "github.com/df-mc/dragonfly/server/java/protocol/item"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/java/protocol/wire"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// TestJavaFireworkAttached: a rocket launched while gliding carries the id of the player it is
// attached to (the player's own id for themselves), so the Java client boosts itself, and its item.
func TestJavaFireworkAttached(t *testing.T) {
	glideWorld(t, func(tx *world.Tx, s *Session, p *player.Player) {
		fw := item.Firework{Duration: time.Second, Explosions: []item.FireworkExplosion{
			{Shape: item.FireworkShapeStar(), Colour: item.ColourRed()},
		}}
		r := tx.AddEntity(entity.NewFireworkAttached(world.EntitySpawnOpts{Position: p.Position()}, fw, p))
		s.viewOtherEntity(r)
		out := s.take()
		id, ok := s.entityID(r)
		if !ok || findPacket(out, v777.ClientboundPlayAddEntity) == nil {
			t.Fatal("the rocket was not spawned")
		}
		b := findPacket(out, v777.ClientboundPlaySetEntityData)
		var head wire.Writer
		head.VarInt(id)
		head.B = append(head.B, dataSilent, dataTypeBoolean, 1, dataFireworkItem, dataTypeItemStack)
		tail := []byte{dataFireworkAttachedTo, dataTypeOptUnsignedInt, selfEntityID + 1, 0xff}
		if !bytes.HasPrefix(b, head.B) || !bytes.HasSuffix(b, tail) {
			t.Fatalf("rocket data % x: want silent, its item, then attached to %d", b, selfEntityID)
		}
		comp := fireworksBytes(fw)
		if !bytes.Contains(b, comp) {
			t.Fatalf("rocket data % x lacks its fireworks component % x", b, comp)
		}

		s.ViewEntityAction(r, entity.FireworkExplosionAction{})
		var ev wire.Writer
		ev.Int32(id)
		ev.Byte(entityEventFireworksExplode)
		if got := findPacket(s.take(), v777.ClientboundPlayEntityEvent); !bytes.Equal(got, ev.B) {
			t.Fatalf("explosion event % x, want % x", got, ev.B)
		}

		// Not attached (set off from a block): no target.
		r2 := tx.AddEntity(entity.NewFirework(world.EntitySpawnOpts{Position: p.Position()}, fw))
		s.viewOtherEntity(r2)
		if b := findPacket(s.take(), v777.ClientboundPlaySetEntityData); bytes.Contains(b, []byte{dataFireworkAttachedTo, dataTypeOptUnsignedInt}) {
			t.Fatalf("a free rocket was sent attached: % x", b)
		}
	})
}

func fireworksBytes(f item.Firework) []byte {
	var w wire.Writer
	w.VarInt(javaFlightDuration(f))
	w.VarInt(int32(len(f.Explosions)))
	for _, ex := range f.Explosions {
		w.VarInt(int32(ex.Shape.Uint8()))
		w.VarInt(1)
		w.Int32(javaFireworkColour(ex.Colour))
		w.VarInt(0)
		w.Bool(ex.Trail)
		w.Bool(ex.Twinkle)
	}
	return w.B
}

// TestJavaFireworksComponent: the minecraft:fireworks component as Fireworks.STREAM_CODEC writes it,
// in Java's dye colours; a plain rocket (flight 1, no explosions) is Java's default and sent bare.
func TestJavaFireworksComponent(t *testing.T) {
	fw := item.Firework{Duration: 1500 * time.Millisecond, Explosions: []item.FireworkExplosion{{
		Shape: item.FireworkShapeCreeperHead(), Colour: item.ColourRed(), Fade: item.ColourBlue(), Fades: true, Trail: true,
	}}}
	js := jitemStack(fw)
	if len(js) != 1 {
		t.Fatalf("components: %d, want 1", len(js))
	}
	var w wire.Writer
	w.VarInt(2) // flight
	w.VarInt(1)
	w.VarInt(3) // creeper
	w.VarInt(1)
	w.Int32(11743532) // DyeColor.RED fireworkColor
	w.VarInt(1)
	w.Int32(2437522) // DyeColor.BLUE
	w.Bool(true)
	w.Bool(false)
	if !bytes.Equal(js[0], w.B) {
		t.Fatalf("component % x, want % x", js[0], w.B)
	}
	if got := jitemStack(item.Firework{Duration: time.Second}); len(got) != 0 {
		t.Fatalf("a plain rocket got a fireworks component: % x", got)
	}
	for i, c := range item.Colours() {
		if int(c.Uint8()) != i {
			t.Fatalf("colour %v is %d, want %d (Java's dye order)", c, c.Uint8(), i)
		}
	}
}

// jitemStack converts a firework and returns its minecraft:fireworks components.
func jitemStack(f item.Firework) [][]byte {
	var js jitem.Stack
	javaStack(item.NewStack(f, 1), &js)
	var out [][]byte
	for _, r := range js.Raw {
		if r.Type == jitem.CompFireworks {
			out = append(out, r.Data)
		}
	}
	return out
}
