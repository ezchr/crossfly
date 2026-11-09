//go:build crossfly_full

package javasession

import (
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"net/url"
	"path"
	"sync"

	jserver "github.com/df-mc/dragonfly/server/java/protocol/server"
	"github.com/df-mc/dragonfly/server/player/skin"
)

// Bedrock players see a Java player's cape too: the CAPE texture of the signed profile (premium
// players who own one), downloaded from Mojang's texture server like the skin. A Java cape texture
// has Bedrock's layout (64x32: the cape at 0,0 and the elytra at 22,0), so it is copied over as
// is; larger (HD) textures keep that layout scaled up and are scaled down to it.

const capeWidth, capeHeight = 64, 32

// capes is set when Bedrock players are shown the capes of Java players ([Capes] in config.toml).
var capes bool

// SetCapes turns showing the capes of Java players to Bedrock players on or off (Crossfly Full).
func SetCapes(on bool) { capes = on }

// capesOn reports whether Bedrock players are shown the capes of Java players.
func capesOn() bool { return capes }

// capeArea is the part of a cape texture the cape itself is drawn from (front, back and sides of
// a 10x16x1 box). Java draws it opaque whatever its alpha (and the elytra, at 22,0, with alpha);
// Bedrock honours the alpha, so it is made opaque to look the same.
var capeArea = image.Rect(0, 0, 22, 17)

var capeCache sync.Map // texture URL -> skin.Cape

// capeTextureURL reads the cape URL from a signed "textures" property, or "" if there is none.
func capeTextureURL(props []jserver.Property) string {
	for _, p := range props {
		if p.Name != "textures" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(p.Value)
		if err != nil {
			return ""
		}
		var t struct {
			Textures struct {
				Cape struct {
					URL string `json:"url"`
				} `json:"CAPE"`
			} `json:"textures"`
		}
		if json.Unmarshal(raw, &t) != nil {
			return ""
		}
		return t.Textures.Cape.URL
	}
	return ""
}

// fetchCape starts getting the Bedrock cape of the texture at raw ("" for none) and returns a
// function, to be called once, that waits for it: the cape, its id and whether there is one. The
// download runs alongside the skin's, so a cape does not slow a login down.
func fetchCape(raw string) func() (skin.Cape, string, bool) {
	if raw == "" {
		return func() (skin.Cape, string, bool) { return skin.Cape{}, "", false }
	}
	id := capeID(raw)
	if v, ok := capeCache.Load(raw); ok {
		return func() (skin.Cape, string, bool) { return v.(skin.Cape), id, true }
	}
	ch := make(chan skin.Cape, 1) // closed without a value if the download fails
	go func() {
		img, err := fetchTexture(raw)
		if err != nil {
			close(ch)
			return
		}
		c := bedrockCape(img)
		capeCache.Store(raw, c)
		ch <- c
	}()
	return func() (skin.Cape, string, bool) {
		c, ok := <-ch
		return c, id, ok
	}
}

// capeID is the cape id Bedrock clients cache a cape under: the texture's hash, the last part of
// its URL, so the same cape always has the same id.
func capeID(raw string) string {
	if u, err := url.Parse(raw); err == nil && path.Base(u.Path) != "." && path.Base(u.Path) != "/" {
		return "java_cape_" + path.Base(u.Path)
	}
	return "java_cape_" + raw
}

// withCape is s with cape c (id id) on it. The full skin id changes with it: clients cache the
// whole skin under it, and players sharing a skin texture need not share a cape.
func withCape(s skin.Skin, c skin.Cape, id string) skin.Skin {
	s.Cape = c
	s.CapeID = id
	s.CapeOnClassic = false // as Geyser sends Java capes
	s.FullID = s.SkinID + "_" + id
	return s
}

// bedrockCape turns a Java cape texture into a 64x32 Bedrock cape. Textures at most 64 wide (64x32,
// or the old 22x17 one) are copied pixel for pixel from the top left; wider ones are the same
// layout scaled up by width/64, and are scaled down by averaging each block of pixels.
func bedrockCape(img image.Image) skin.Cape {
	c := skin.NewCape(capeWidth, capeHeight)
	b := img.Bounds()
	w := b.Dx()
	for y := 0; y < capeHeight; y++ {
		for x := 0; x < capeWidth; x++ {
			var px color.NRGBA
			solid := (image.Point{X: x, Y: y}).In(capeArea)
			if w <= capeWidth {
				if x < b.Dx() && y < b.Dy() {
					px = color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
				}
			} else {
				px = averagePixel(img, x*w/capeWidth, (x+1)*w/capeWidth, y*w/capeWidth, (y+1)*w/capeWidth, solid)
			}
			if solid {
				px.A = 0xff
			}
			i := (y*capeWidth + x) * 4
			c.Pix[i], c.Pix[i+1], c.Pix[i+2], c.Pix[i+3] = px.R, px.G, px.B, px.A
		}
	}
	return c
}

// averagePixel is the average colour of img's pixels [x0,x1)x[y0,y1), relative to its top left
// corner. Pixels outside img count as transparent. With solid, the colours are averaged ignoring
// alpha, as Java draws them in the cape area (a transparent pixel shows its colour there).
func averagePixel(img image.Image, x0, x1, y0, y1 int, solid bool) color.NRGBA {
	b := img.Bounds()
	if x1 <= x0 {
		x1 = x0 + 1
	}
	if y1 <= y0 {
		y1 = y0 + 1
	}
	var r, g, bl, a, n uint64
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			n++
			if x >= b.Dx() || y >= b.Dy() {
				continue
			}
			c := img.At(b.Min.X+x, b.Min.Y+y)
			if solid {
				p := color.NRGBAModel.Convert(c).(color.NRGBA) // an NRGBA colour as is
				r, g, bl = r+uint64(p.R), g+uint64(p.G), bl+uint64(p.B)
				continue
			}
			pr, pg, pb, pa := c.RGBA() // alpha-premultiplied
			r, g, bl, a = r+uint64(pr), g+uint64(pg), bl+uint64(pb), a+uint64(pa)
		}
	}
	if solid {
		return color.NRGBA{R: uint8(r / n), G: uint8(g / n), B: uint8(bl / n), A: 0xff}
	}
	avg := color.RGBA64{R: uint16(r / n), G: uint16(g / n), B: uint16(bl / n), A: uint16(a / n)}
	return color.NRGBAModel.Convert(avg).(color.NRGBA)
}
