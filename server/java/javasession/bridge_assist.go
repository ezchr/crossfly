package javasession

import (
	"math"
	"os"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Bridge assist: the block placement help of Bedrock Edition, for Java players (Config.BridgeAssist).
//
// A Bedrock client whose crosshair hits nothing while it looks down targets the block below the player
// instead, on the side the player looks towards, so a block used there lands in front of the block the
// player stands on. The client does that itself and sends an ordinary click on that block and face. A Java
// client has no such help: a click that hits nothing is sent as use_item. With the assist on, the session
// works out the target the Bedrock client would have picked and uses the item on it as if the client had
// clicked there. BRIDGE_ASSIST.md has the sources of the rules below and what is a guess.

const (
	// bridgeAssistViewY is how far down the player must look: the vertical part of the look direction is
	// below this, which is a pitch steeper than 44.4 degrees.
	bridgeAssistViewY = -0.7
	// bridgeAssistDepth is how far below the eyes the block under the player is looked for.
	bridgeAssistDepth = 2.0
	// bridgeAssistUseOnGap is how long after a use_item_on a use_item is not assisted: a Java client
	// sends both for one click when the block it hit passed the click on.
	bridgeAssistUseOnGap = 60 * time.Millisecond
)

// bridgeAssistEnv reads the environment variable CF_JAVA_BRIDGE_ASSIST, which overrides
// Config.BridgeAssist when set: 1, true or on turn the assist on, 0, false or off turn it off.
func bridgeAssistEnv(conf bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CF_JAVA_BRIDGE_ASSIST"))) {
	case "1", "true", "on", "yes":
		return true
	case "0", "false", "off", "no":
		return false
	}
	return conf
}

// bridgeAssistTarget returns the block and face a Bedrock client would use an item on when the crosshair
// of a player with its eyes at eye, looking along rot, hits nothing: the first block straight below the
// eyes, at most bridgeAssistDepth down, and the horizontal face the player looks towards. click is where
// the line down from the eyes enters the block, within the block. ok is false if the player does not look
// down far enough or has no block below them.
func bridgeAssistTarget(src world.BlockSource, eye mgl64.Vec3, rot cube.Rotation) (pos cube.Pos, face cube.Face, click mgl64.Vec3, ok bool) {
	yaw, pitch := mgl64.DegToRad(rot.Yaw()), mgl64.DegToRad(rot.Pitch())
	for _, v := range [...]float64{eye[0], eye[1], eye[2], yaw, pitch} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return pos, face, click, false
		}
	}
	if !(-math.Sin(pitch) < bridgeAssistViewY) {
		return pos, face, click, false
	}
	// The look direction is (-sin yaw, ., cos yaw) scaled by cos pitch; the face is its larger horizontal
	// part. Taking it from the yaw keeps it defined when looking straight down.
	vx, vz := -math.Sin(yaw), math.Cos(yaw)
	switch {
	case math.Abs(vx) <= math.Abs(vz) && vz >= 0:
		face = cube.FaceSouth
	case math.Abs(vx) <= math.Abs(vz):
		face = cube.FaceNorth
	case vx >= 0:
		face = cube.FaceEast
	default:
		face = cube.FaceWest
	}

	top, bottom := eye[1], eye[1]-bridgeAssistDepth
	x, z := int(math.Floor(eye[0])), int(math.Floor(eye[2]))
	for y := int(math.Floor(top)); y >= int(math.Floor(bottom)); y-- {
		pos = cube.Pos{x, y, z}
		b := src.Block(pos)
		if _, air := b.(block.Air); air {
			continue
		}
		if _, liquid := b.(world.Liquid); liquid {
			continue
		}
		boxes := b.Model().BBox(pos, src)
		if len(boxes) == 0 {
			// Blocks without a collision box that a crosshair still hits (plants, torches, snow
			// layers): the whole block stands in for their outline.
			boxes = []cube.BBox{cube.Box(0, 0, 0, 1, 1, 1)}
		}
		rx, rz := eye[0]-float64(x), eye[2]-float64(z)
		hitY, hit := 0.0, false
		for _, box := range boxes {
			min, max := box.Min(), box.Max()
			if rx < min[0] || rx > max[0] || rz < min[2] || rz > max[2] {
				continue
			}
			// The line enters the box through its top, which is at or below the eyes.
			if boxTop := float64(y) + max[1]; boxTop <= top && boxTop >= bottom && (!hit || boxTop > hitY) {
				hitY, hit = boxTop, true
			}
		}
		if hit {
			click = mgl64.Vec3{rx, mgl64.Clamp(hitY-float64(y), 0, 1), rz}
			return pos, face, click, true
		}
	}
	return pos, face, click, false
}

// bridgeAssistUse handles a use_item of a client that holds a block, with the rotation the packet came
// with. If Bedrock's placement help has a target for the player, the held item is used on it as a click of
// the client would be (Player.UseItemOnBlock: the same handlers, reach, entity and build checks, sound and
// item count), and the result reports true if that did something. On false the caller handles the
// use_item as usual.
func (s *Session) bridgeAssistUse(tx *world.Tx, c session.Controllable, yaw, pitch float32) bool {
	held, _ := c.HeldItems()
	if _, placeable := held.Item().(world.Block); !placeable || held.Empty() {
		return false
	}
	rot := cube.Rotation{float64(yaw), float64(pitch)}
	if yaw != yaw || pitch != pitch || math.IsInf(float64(yaw), 0) || pitch < -90 || pitch > 90 {
		rot = c.Rotation()
	}
	pos, face, click, ok := bridgeAssistTarget(tx, entity.EyePosition(c), rot)
	if !ok {
		return false
	}
	// The client did not predict this click: it neither swung its arm nor played the sound of the block.
	s.fxForgetUseOn()
	swings := s.selfSwingCount().Load()
	c.UseItemOnBlock(pos, face, click)
	if s.selfSwingCount().Load() == swings {
		// Nothing was placed or activated (no room, out of reach, cancelled).
		return false
	}
	s.resendHeldItem()
	s.swingSelf()
	if s.log != nil {
		s.log.Debug("bridge assist", "block", pos, "face", face.String(), "eye", entity.EyePosition(c), "yaw", yaw, "pitch", pitch)
	}
	return true
}

// fxForgetUseOn forgets the client's last use_item_on, so the sound of a block the server places for the
// client without a click of its own (bridge assist) is not taken for one the client played itself.
func (s *Session) fxForgetUseOn() {
	if st := s.fx(); st != nil {
		st.mu.Lock()
		st.usedOnAt = time.Time{}
		st.mu.Unlock()
	}
}
