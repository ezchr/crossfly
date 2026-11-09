package javasession

import (
	"context"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// glideWorld runs f with a test session whose player stands in a world wearing an elytra.
func glideWorld(t *testing.T, f func(tx *world.Tx, s *Session, p *player.Player)) {
	t.Helper()
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	_, _ = world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		pos := mgl64.Vec3{0.5, 10, 0.5}
		h := world.EntitySpawnOpts{Position: pos}.New(player.Type, player.Config{
			Name: "Glider", UUID: uuid.New(), GameMode: world.GameModeSurvival, Health: 20, MaxHealth: 20, Position: pos,
		})
		p := tx.AddEntity(h).(*player.Player)
		p.Armour().SetChestplate(item.NewStack(item.Elytra{}, 1))
		s := mobTestSession()
		s.ent = p.H()
		f(tx, s, p)
		return struct{}{}, nil
	})
}

// TestJavaGlideEndsOnGround: a Java client asks to start gliding and never says it stopped; its
// move packets' on_ground flag ends the glide, as vanilla's canGlide does. Without it the player
// slid along the ground gliding, and a jump there was boosted by the glide.
func TestJavaGlideEndsOnGround(t *testing.T) {
	glideWorld(t, func(tx *world.Tx, s *Session, p *player.Player) {
		s.landed(p, false)
		if s.startGliding(p); !p.Gliding() {
			t.Fatal("START_FALL_FLYING in the air did not start the glide")
		}
		s.landed(p, false)
		if !p.Gliding() {
			t.Fatal("an airborne move ended the glide")
		}
		s.landed(p, true)
		if p.Gliding() {
			t.Fatal("still gliding after a move on the ground")
		}
	})
}

// TestJavaGlideEndsLevitating: levitation ends a glide (canGlide).
func TestJavaGlideEndsLevitating(t *testing.T) {
	glideWorld(t, func(tx *world.Tx, s *Session, p *player.Player) {
		s.landed(p, false)
		s.startGliding(p)
		p.AddEffect(effect.New(effect.Levitation, 1, time.Second*10))
		if s.landed(p, false); p.Gliding() {
			t.Fatal("still gliding while levitating")
		}
	})
}

// TestJavaGlideRefusedOnGround: START_FALL_FLYING on the ground is refused, and the client, which
// glides already by its own prediction, is sent its flags without the glide bit.
func TestJavaGlideRefusedOnGround(t *testing.T) {
	glideWorld(t, func(tx *world.Tx, s *Session, p *player.Player) {
		s.landed(p, true)
		s.take()
		if s.startGliding(p); p.Gliding() {
			t.Fatal("gliding started on the ground")
		}
		b := findPacket(s.take(), v777.ClientboundPlaySetEntityData)
		if b == nil || b[0] != selfEntityID || b[1] != 0 || b[2] != dataTypeByte || b[3]&0x80 != 0 {
			t.Fatalf("refused start: own flags % x, want them sent without 0x80", b)
		}
	})
}
