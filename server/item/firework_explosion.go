package item

import "reflect"

// FireworkExplosion represents an explosion of a firework.
type FireworkExplosion struct {
	// Shape represents the shape of the explosion.
	Shape FireworkShape
	// Colour is the colour of the explosion.
	Colour Colour
	// Fade is the colour the explosion should fade into. Fades must be set to true in order for this to function.
	Fade Colour
	// Fades is true if the explosion should fade into the fade colour.
	Fades bool
	// Twinkle is true if the explosion should twinkle on explode.
	Twinkle bool
	// Trail is true if the explosion should have a trail.
	Trail bool
	// Colours holds every colour of an explosion with more than one, such as a firework star crafted
	// from several dyes. Colour is then Colours[0]. It is empty for an explosion of one colour.
	Colours []Colour
	// FadeColours holds every fade colour of an explosion that fades into more than one. Fade is then
	// FadeColours[0] and Fades is true. It is empty for an explosion with at most one fade colour.
	FadeColours []Colour
}

// AllColours returns every colour of the explosion: Colours, or just Colour if Colours is empty.
func (f FireworkExplosion) AllColours() []Colour {
	if len(f.Colours) > 0 {
		return f.Colours
	}
	return []Colour{f.Colour}
}

// AllFades returns every fade colour of the explosion: FadeColours, just Fade if FadeColours is empty
// and Fades is true, or nothing.
func (f FireworkExplosion) AllFades() []Colour {
	if len(f.FadeColours) > 0 {
		return f.FadeColours
	}
	if f.Fades {
		return []Colour{f.Fade}
	}
	return nil
}

// WithColours returns the explosion with the colours passed, setting Colour and Colours.
func (f FireworkExplosion) WithColours(c ...Colour) FireworkExplosion {
	f.Colours = nil
	if len(c) > 0 {
		f.Colour = c[0]
	}
	if len(c) > 1 {
		f.Colours = append([]Colour(nil), c...)
	}
	return f
}

// WithFades returns the explosion with the fade colours passed, setting Fade, Fades and FadeColours.
// No colours removes the fade.
func (f FireworkExplosion) WithFades(c ...Colour) FireworkExplosion {
	f.FadeColours, f.Fades, f.Fade = nil, len(c) > 0, Colour{}
	if len(c) > 0 {
		f.Fade = c[0]
	}
	if len(c) > 1 {
		f.FadeColours = append([]Colour(nil), c...)
	}
	return f
}

// EncodeNBT ...
func (f FireworkExplosion) EncodeNBT() map[string]any {
	return map[string]any{
		"FireworkType":    f.Shape.Uint8(),
		"FireworkColor":   colourByteArray(f.AllColours()),
		"FireworkFade":    colourByteArray(f.AllFades()),
		"FireworkFlicker": boolByte(f.Twinkle),
		"FireworkTrail":   boolByte(f.Trail),
	}
}

// DecodeNBT ...
func (f FireworkExplosion) DecodeNBT(data map[string]any) any {
	if t, ok := data["FireworkType"].(uint8); ok && int(t) < len(FireworkShapes()) {
		f.Shape = FireworkShapes()[t]
	}
	f.Twinkle = data["FireworkFlicker"] == uint8(1)
	f.Trail = data["FireworkTrail"] == uint8(1)

	if colours := decodeColourBytes(data["FireworkColor"]); len(colours) > 0 {
		f = f.WithColours(colours...)
	}
	f = f.WithFades(decodeColourBytes(data["FireworkFade"])...)
	return f
}

// colourByteArray encodes colours as an NBT byte array of inverted colour IDs. The NBT encoder writes
// fixed-size byte arrays as byte arrays, so the array type is made for the number of colours.
func colourByteArray(colours []Colour) any {
	switch len(colours) {
	case 0:
		return [0]uint8{}
	case 1:
		return [1]uint8{uint8(invertColour(colours[0]))}
	}
	arr := reflect.New(reflect.ArrayOf(len(colours), reflect.TypeFor[uint8]())).Elem()
	for i, c := range colours {
		arr.Index(i).SetUint(uint64(uint8(invertColour(c))))
	}
	return arr.Interface()
}

// decodeColourBytes decodes the colours of a FireworkColor or FireworkFade tag: a byte array as
// read from disk, or a list of bytes as sent over the network.
func decodeColourBytes(v any) []Colour {
	var ids []uint8
	switch v := v.(type) {
	case nil:
		return nil
	case []any:
		for _, b := range v {
			if b, ok := b.(uint8); ok {
				ids = append(ids, b)
			}
		}
	case []uint8:
		ids = v
	default:
		rv := reflect.ValueOf(v)
		if rv.Kind() != reflect.Array || rv.Type().Elem().Kind() != reflect.Uint8 {
			return nil
		}
		for i := range rv.Len() {
			ids = append(ids, uint8(rv.Index(i).Uint()))
		}
	}
	colours := make([]Colour, 0, len(ids))
	for _, id := range ids {
		colours = append(colours, invertColourID(int16(id)))
	}
	return colours
}
