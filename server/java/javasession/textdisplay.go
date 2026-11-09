package javasession

import (
	"math"
	"strings"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/java/protocol/text"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/world"
)

// Dragonfly's floating text entity (entity.NewText: "dragonfly:text", a name tag with no body) is
// shown to Java players as a text_display. Indexes are the 26.3 entity data ids (Entity 0-7,
// Display 8-22, TextDisplay 23-27).
const (
	dataDisplayBillboard = 15

	dataTextText      = 23
	dataTextLineWidth = 24

	billboardCenter = 3 // always faces the viewer, like a Bedrock name tag

	dataTypeInt       = 1
	dataTypeComponent = 5
)

func isTextEntity(e world.Entity) bool {
	return e.H().Type() == entity.TextType
}

// viewTextDisplay sends a text display's text (on spawn and whenever its name tag changes).
func (s *Session) viewTextDisplay(e world.Entity, id int32, full bool) {
	ent, ok := e.(*entity.Ent)
	if !ok {
		return
	}
	c := text.Legacy(ent.NameTag(), text.Bedrock)
	w := s.packet()
	w.VarInt(id)
	if full {
		w.Byte(dataDisplayBillboard)
		w.VarInt(dataTypeByte)
		w.Byte(billboardCenter)
		w.Byte(dataTextLineWidth)
		w.VarInt(dataTypeInt)
		w.VarInt(1000) // Bedrock does not wrap name tags; Java wraps at 200 pixels by default
		// A Bedrock name tag shows through blocks and at full light whatever is around it.
		w.Byte(dataTextFlags)
		w.VarInt(dataTypeByte)
		w.Byte(textFlagSeeThrough)
		w.Byte(dataDisplayBrightness)
		w.VarInt(dataTypeInt)
		w.VarInt(fullBrightness)
	}
	// With every text, as the shift depends on its number of lines.
	w.Byte(dataDisplayTranslation)
	w.VarInt(dataTypeVector3)
	w.Float32(0)
	w.Float32(textDisplayShift(ent.NameTag()))
	w.Float32(0)
	w.Byte(dataTextText)
	w.VarInt(dataTypeComponent)
	c.Write(w)
	w.Byte(0xff)
	s.queue(v777.ClientboundPlaySetEntityData, w)
}

// textDisplayBase is the shift of a text display with no lines, in thousandths of a block: see
// textDisplayShift and SetTextDisplayBase.
var textDisplayBase atomic.Int32

func init() { textDisplayBase.Store(350) }

// SetTextDisplayBase sets how far up a Java text display is drawn before the 0.1414 a line come
// off (0.35 unless set). It is the number to change if Java players see floating text higher or
// lower than Bedrock players do; text already shown keeps its place until it is sent again.
func SetTextDisplayBase(blocks float64) { textDisplayBase.Store(int32(math.Round(blocks * 1000))) }

// TextDisplayBase returns the value SetTextDisplayBase set.
func TextDisplayBase() float64 { return float64(textDisplayBase.Load()) / 1000 }

// textDisplayShift is how far up (down, if negative) a text display is drawn from its entity's
// position so that the text sits where a Bedrock player sees the name tag. Bedrock draws a name
// tag lower than Java draws the same text at the same place, by more the more lines it has: an
// 11-line leaderboard was almost a block higher for Java players. The 0.1414 a line is Geyser's,
// which moves text displays the other way for Bedrock players; the base is measured in game.
func textDisplayShift(nameTag string) float32 {
	if nameTag == "" {
		return 0
	}
	lines := strings.Count(nameTag, "\n") + 1
	return float32(TextDisplayBase()) - 0.1414*float32(lines)
}
