//go:build crossfly_full

package javasession

import (
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"

	jserver "github.com/df-mc/dragonfly/server/java/protocol/server"
	"github.com/df-mc/dragonfly/server/player/skin"
)

// testdata/cape_mojang.png is a real cape texture from Mojang's texture server (64x32).
func loadCapeFixture(t *testing.T) *image.NRGBA {
	t.Helper()
	f, err := os.Open("testdata/cape_mojang.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	n := image.NewNRGBA(img.Bounds())
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			n.Set(x, y, img.At(x, y))
		}
	}
	return n
}

func capeAt(pix []uint8, x, y int) color.NRGBA {
	i := (y*capeWidth + x) * 4
	return color.NRGBA{R: pix[i], G: pix[i+1], B: pix[i+2], A: pix[i+3]}
}

func TestBedrockCapeMojang(t *testing.T) {
	src := loadCapeFixture(t)
	if b := src.Bounds(); b.Dx() != 64 || b.Dy() != 32 {
		t.Fatalf("fixture is %v, want 64x32", b)
	}
	c := bedrockCape(src)
	if b := c.Bounds(); b.Dx() != capeWidth || b.Dy() != capeHeight || len(c.Pix) != capeWidth*capeHeight*4 {
		t.Fatalf("cape is %v with %d bytes", b, len(c.Pix))
	}
	opaque := 0
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			want, got := src.NRGBAAt(x, y), capeAt(c.Pix, x, y)
			if (image.Point{X: x, Y: y}).In(capeArea) {
				want.A = 0xff
			}
			if got != want {
				t.Fatalf("pixel %d,%d: got %v, want %v", x, y, got, want)
			}
			if got.A == 0xff {
				opaque++
			}
		}
	}
	if opaque < capeArea.Dx()*capeArea.Dy() {
		t.Fatalf("only %d opaque pixels", opaque)
	}
}

// HD capes are the 64x32 layout scaled up: scaling one down gives the 64x32 cape back.
func TestBedrockCapeHD(t *testing.T) {
	src := loadCapeFixture(t)
	want := bedrockCape(src)
	for _, k := range []int{2, 4, 16} {
		hd := image.NewNRGBA(image.Rect(0, 0, 64*k, 32*k))
		for y := 0; y < 32*k; y++ {
			for x := 0; x < 64*k; x++ {
				hd.SetNRGBA(x, y, src.NRGBAAt(x/k, y/k))
			}
		}
		got := bedrockCape(hd)
		for y := 0; y < 32; y++ {
			for x := 0; x < 64; x++ {
				g, w := capeAt(got.Pix, x, y), capeAt(want.Pix, x, y)
				if w.A == 0 && g.A == 0 {
					continue // the colour of a transparent pixel is not kept
				}
				if g != w {
					t.Fatalf("x%d: pixel %d,%d: got %v, want %v", k, x, y, g, w)
				}
			}
		}
	}
}

// A texture smaller than 64x32 (the old 22x17 cape) goes to the top left, the rest transparent.
func TestBedrockCapeSmall(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 22, 17))
	for y := 0; y < 17; y++ {
		for x := 0; x < 22; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 10), G: uint8(y * 10), B: 7, A: 0xff})
		}
	}
	c := bedrockCape(src)
	if got := capeAt(c.Pix, 21, 16); got != (color.NRGBA{R: 210, G: 160, B: 7, A: 0xff}) {
		t.Fatalf("pixel 21,16: %v", got)
	}
	if got := capeAt(c.Pix, 40, 20); got.A != 0 {
		t.Fatalf("pixel 40,20 should be transparent: %v", got)
	}
}

func texturesProp(json string) []jserver.Property {
	return []jserver.Property{{Name: "textures", Value: base64.StdEncoding.EncodeToString([]byte(json))}}
}

func TestCapeTextureURL(t *testing.T) {
	props := texturesProp(`{"textures":{"SKIN":{"url":"http://textures.minecraft.net/texture/aa"},"CAPE":{"url":"http://textures.minecraft.net/texture/bb"}}}`)
	if got := capeTextureURL(props); got != "http://textures.minecraft.net/texture/bb" {
		t.Fatalf("cape url %q", got)
	}
	if got := capeTextureURL(texturesProp(`{"textures":{"SKIN":{"url":"http://textures.minecraft.net/texture/aa"}}}`)); got != "" {
		t.Fatalf("no cape, got %q", got)
	}
	if got := capeTextureURL(nil); got != "" {
		t.Fatalf("no textures, got %q", got)
	}
	if got := capeID("http://textures.minecraft.net/texture/bb"); got != "java_cape_bb" {
		t.Fatalf("cape id %q", got)
	}
}

// javaSkin puts the profile's cape on the skin unless the player turned it off (all from the
// caches, so no download).
func TestJavaSkinCape(t *testing.T) {
	const skinURL, capeURL = "http://textures.minecraft.net/texture/test-skin", "http://textures.minecraft.net/texture/test-cape"
	base := bedrockSkin(image.NewNRGBA(image.Rect(0, 0, 64, 64)), false)
	skinCache.Store(skinURL, base)
	cape := bedrockCape(loadCapeFixture(t))
	capeCache.Store(capeURL, cape)
	defer skinCache.Delete(skinURL)
	defer capeCache.Delete(capeURL)
	props := texturesProp(`{"textures":{"SKIN":{"url":"` + skinURL + `"},"CAPE":{"url":"` + capeURL + `"}}}`)

	s := javaSkin(props, true)
	if b := s.Cape.Bounds(); b.Dx() != 64 || b.Dy() != 32 || &s.Cape.Pix[0] != &cape.Pix[0] {
		t.Fatalf("cape not on the skin: %v", b)
	}
	if s.CapeID != "java_cape_test-cape" || s.FullID == base.FullID || s.SkinID != base.SkinID {
		t.Fatalf("ids: skin %q cape %q full %q", s.SkinID, s.CapeID, s.FullID)
	}
	if len(s.Pix) != 64*64*4 {
		t.Fatalf("skin pixels lost: %d", len(s.Pix))
	}
	if again := javaSkin(props, true); again.FullID != s.FullID {
		t.Fatalf("full id not stable: %q, %q", again.FullID, s.FullID)
	}
	// The cached skin itself stays without a cape, for players sharing the skin without one.
	if v, _ := skinCache.Load(skinURL); v.(skin.Skin).Cape.Bounds().Dx() != 0 {
		t.Fatal("cape put on the cached skin")
	}

	off := javaSkin(props, false)
	if off.Cape.Bounds().Dx() != 0 || off.CapeID != "" || off.FullID != base.FullID {
		t.Fatalf("cape shown though turned off: %v %q", off.Cape.Bounds(), off.CapeID)
	}
}
