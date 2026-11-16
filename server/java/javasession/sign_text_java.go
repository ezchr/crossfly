package javasession

import (
	"image/color"
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

// A Java client draws a sign's text from its block entity data only (SignBlockEntity.loadAdditional):
// without it every sign is blank. This is that data for a Bedrock sign, both sides.

// javaDyeNames are the Java DyeColor names in id order, the order of item.Colours().
var javaDyeNames = [16]string{"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray",
	"light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black"}

// signDyeName is the Java dye name of a sign side's text colour (black for any other colour).
func signDyeName(c color.RGBA) string {
	for i, col := range item.Colours() {
		if i < len(javaDyeNames) && col.SignRGBA() == c {
			return javaDyeNames[i]
		}
	}
	return "black"
}

// signSideNBT is one side of a sign as Java's SignText: four lines (plain text components; the
// client draws § codes in them), the dye colour and the glow.
func signSideNBT(t block.SignText) map[string]any {
	lines := strings.Split(strings.ReplaceAll(t.Text, "\r", ""), "\n")
	msgs := make([]string, 4)
	copy(msgs, lines) // Java signs have four lines: more are dropped
	glow := uint8(0)
	if t.Glowing {
		glow = 1
	}
	return map[string]any{"messages": msgs, "color": signDyeName(t.BaseColour), "has_glowing_text": glow}
}

// signBlockEntity returns the Java block entity type and network NBT of a sign, ok false for any
// other block.
func signBlockEntity(b world.Block) (typ string, data []byte, ok bool) {
	s, isSign := b.(block.Sign)
	if !isSign {
		return "", nil, false
	}
	waxed := uint8(0)
	if s.Waxed {
		waxed = 1
	}
	enc, err := nbt.MarshalEncoding(map[string]any{
		"is_waxed": waxed, "front_text": signSideNBT(s.Front), "back_text": signSideNBT(s.Back),
	}, nbt.BigEndian)
	if err != nil || len(enc) < 3 {
		return "", nil, false
	}
	// Network NBT: the compound tag type, then its payload without a name.
	return "minecraft:sign", append([]byte{enc[0]}, enc[3:]...), true
}
