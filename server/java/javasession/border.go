package javasession

import (
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// javaBorderTeleportBoundary is the portal teleport boundary Java servers send with the border.
const javaBorderTeleportBoundary = 29999984

// ViewBorder shows the world border to a Java player (world.BorderViewer) with Java's own
// border, sent again whenever it is changed: the client moves a moving border itself.
func (s *Session) ViewBorder(b world.Border, _ mgl64.Vec3) {
	if s.borderVersion.Swap(b.Version) == b.Version && s.borderSent.Load() {
		return
	}
	s.borderSent.Store(true)
	center, size, target := b.Center, b.Size, b.TargetSize
	if !b.Enabled() {
		center, size, target = mgl64.Vec2{}, world.BorderMaxSize, world.BorderMaxSize
	}
	w := s.packet()
	w.Float64(center[0])
	w.Float64(center[1])
	w.Float64(size)
	w.Float64(target)
	w.VarLong(b.Remaining.Milliseconds())
	w.VarInt(javaBorderTeleportBoundary)
	w.VarInt(int32(b.WarningDistance))
	w.VarInt(int32(b.WarningTime.Seconds()))
	s.queue(v777.ClientboundPlayInitializeBorder, w)
}
