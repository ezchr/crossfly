package item

import "image/color"

// FireworkStar is an item used to determine the color, effect, and shape of firework rockets.
type FireworkStar struct {
	FireworkExplosion
}

// EncodeNBT ...
func (f FireworkStar) EncodeNBT() map[string]any {
	return map[string]any{
		"FireworksItem": f.FireworkExplosion.EncodeNBT(),
		"customColor":   int32FromRGBA(averageColour(f.AllColours())),
	}
}

// averageColour returns the average of the RGB values of the colours passed, which is the colour a
// firework star of several colours is drawn in.
func averageColour(colours []Colour) color.RGBA {
	if len(colours) == 1 {
		return colours[0].RGBA()
	}
	var r, g, b int
	for _, c := range colours {
		rgba := c.RGBA()
		r, g, b = r+int(rgba.R), g+int(rgba.G), b+int(rgba.B)
	}
	n := max(len(colours), 1)
	return color.RGBA{R: uint8(r / n), G: uint8(g / n), B: uint8(b / n), A: 0xff}
}

// DecodeNBT ...
func (f FireworkStar) DecodeNBT(data map[string]any) any {
	if i, ok := data["FireworksItem"].(map[string]any); ok {
		f.FireworkExplosion = f.FireworkExplosion.DecodeNBT(i).(FireworkExplosion)
	}
	return f
}

// EncodeItem ...
func (f FireworkStar) EncodeItem() (name string, meta int16) {
	return "minecraft:firework_star", invertColour(f.Colour)
}
