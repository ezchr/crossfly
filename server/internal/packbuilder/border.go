package packbuilder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"

	"github.com/df-mc/dragonfly/server/world"
)

// The Bedrock world border wall (session/border.go), in two entities. A piece of wall is a plane of
// bones, one per column, and its mark variant (first*128 + end) says which columns show. The ring
// is a whole square, a bone for each anchor, and its mark variant says which bone shows. Both show
// the texture, the registered square tile repeated, a tile every borderWallTile blocks. The variant
// picks the texture: Java's look (still, shrinking, growing), then the registered ones. The skin ID
// is where the scroll was when the entity appeared.

const (
	borderRingEntity = "dragonfly:world_border"
	borderCapEntity  = "dragonfly:world_border_cap"
	borderCell       = 32  // blocks in a piece, and its columns
	borderWallHeight = 384 // blocks
	borderWallTile   = 32  // blocks a texture tile covers, wide enough to read from afar
	borderTileW      = borderCell
	borderTileH      = 64
	// borderRingSpan is how much of the texture a cube of the ring shows each way.
	borderRingSpan = 96
	// The texture holds a tile more than a plane shows each way, for the scroll.
	borderTexW = 128
	borderTexH = 128
	// borderVisible is the largest box clients keep around an entity to see if it is in view.
	borderVisible = 50
)

// Java's border colours: still, shrinking, growing.
var borderColours = [3]color.NRGBA{{32, 160, 255, 255}, {255, 48, 48, 255}, {64, 255, 128, 255}}

// buildBorder writes the wall's files, if the server uses it. It returns how many it wrote.
func buildBorder(dir string) int {
	if !world.BorderWall() {
		return 0
	}
	mkdir := func(p string) {
		if err := os.MkdirAll(filepath.Join(dir, p), os.ModePerm); err != nil {
			panic(err)
		}
	}
	write := func(p string, data []byte) {
		if err := os.WriteFile(filepath.Join(dir, p), data, 0666); err != nil {
			panic(err)
		}
	}
	writeJSON := func(p string, v any) {
		data, err := json.Marshal(v)
		if err != nil {
			panic(err)
		}
		write(p, data)
	}
	mkdir("entity")
	mkdir("models/entity")
	mkdir("render_controllers")
	mkdir("textures/dragonfly/world_border")

	textures := map[string]string{}
	var skins, mats []string
	for i, c := range borderColours {
		name := fmt.Sprintf("java_%d", i)
		write("textures/dragonfly/world_border/"+name+".png", borderAtlas(borderStripes(c)))
		textures[name] = "textures/dragonfly/world_border/" + name
		skins = append(skins, "Texture."+name)
		mats = append(mats, "Material.glow")
	}
	// javaScroll is true for Java's looks, the first variants.
	javaScroll := fmt.Sprintf("query.variant < %d", len(borderColours))
	for i, t := range world.BorderTextures() {
		name := fmt.Sprintf("custom_%d", i)
		write("textures/dragonfly/world_border/"+name+".png", borderAtlas(t.PNG))
		textures[name] = "textures/dragonfly/world_border/" + name
		// Vibrant Visuals: the coloured pixels glow (emissive), the white and black ones don't.
		write("textures/dragonfly/world_border/"+name+"_mer.png", borderAtlas(borderGlowMER(t.PNG)))
		writeJSON("textures/dragonfly/world_border/"+name+".texture_set.json", map[string]any{
			"format_version": "1.21.30",
			"minecraft:texture_set": map[string]any{
				"color":                        name,
				"metalness_emissive_roughness": name + "_mer",
			},
		})
		skins = append(skins, "Texture."+name)
		mats = append(mats, "Material.default")
	}

	type wall struct {
		id, name string
		parts    []any
		model    func(id string) map[string]any
	}
	walls := []wall{{borderCapEntity, "world_border_cap", borderColumns(borderCell), func(id string) map[string]any {
		return borderGeometry(id, borderCell, 1)
	}}}
	if minX, minZ, maxX, maxZ, ok := world.BorderRing(); ok {
		walls = append(walls, wall{borderRingEntity, "world_border", borderRingParts(minX, minZ, maxX, maxZ), func(id string) map[string]any {
			return borderRingGeometry(id, minX, minZ, maxX, maxZ)
		}})
	}
	for _, e := range walls {
		geometry, controller := "geometry.dragonfly."+e.name, "controller.render.dragonfly."+e.name
		writeJSON("entity/"+e.name+".entity.json", map[string]any{
			"format_version": "1.10.0",
			"minecraft:client_entity": map[string]any{
				"description": map[string]any{
					"identifier":         e.id,
					"materials":          map[string]any{"default": "breeze_wind", "glow": "charged_creeper"},
					"textures":           textures,
					"geometry":           map[string]any{"default": geometry},
					"render_controllers": []string{controller},
				},
			},
		})
		writeJSON("render_controllers/"+e.name+".render_controllers.json", map[string]any{
			"format_version": "1.8.0",
			"render_controllers": map[string]any{
				controller: map[string]any{
					"arrays": map[string]any{
						"textures":  map[string]any{"Array.skins": skins},
						"materials": map[string]any{"Array.mats": mats},
					},
					"geometry":        "Geometry.default",
					"materials":       []any{map[string]any{"*": "Array.mats[query.variant]"}},
					"textures":        []string{"Array.skins[query.variant]"},
					"part_visibility": e.parts,
					"ignore_lighting": true,
					"uv_anim": map[string]any{
						// Java's looks scroll as on Java: one of its 2-block tiles every 3 seconds,
						// diagonally; registered textures up and to the right, a wall tile every 8
						// seconds. The skin ID is where the scroll was, in ticks, when the piece
						// appeared, which keeps pieces together.
						"offset": []any{
							javaScroll + " ? math.mod(query.life_time + query.skin_id / 20, 3) / 192 : 0.25 - math.mod(query.life_time + query.skin_id / 20, 8) / 32",
							javaScroll + " ? math.mod(query.life_time + query.skin_id / 20, 3) / 192 : math.mod(query.life_time + query.skin_id / 20, 8) / 32",
						},
						// Vanilla refuses a uv_anim without a scale.
						"scale": []any{1.0, 1.0},
					},
				},
			},
		})
		writeJSON("models/entity/"+e.name+".geo.json", e.model(geometry))
	}
	return 3*len(walls) + len(skins)
}

// borderGeometry is a plane along x, its middle at the entity, both sides textured: a root bone
// for the animation and one bone per column, width blocks wide.
func borderGeometry(id string, columns, width int) map[string]any {
	const unit = 16 // model units per block
	bones := []any{map[string]any{"name": "root", "pivot": []int{0, 0, 0}}}
	slice := float64(min(width, borderTileW))
	for c := range columns {
		x := (c - columns/2) * width * unit
		u := math.Mod(float64(c)*slice, borderTileW)
		var cubes []any
		for y := -borderWallHeight / 2 * unit; y < borderWallHeight/2*unit; y += borderTileH * unit {
			cubes = append(cubes, map[string]any{
				"origin": []int{x, y, 0},
				"size":   []int{width * unit, borderTileH * unit, 0},
				"uv": map[string]any{
					"north": map[string]any{"uv": []float64{u, 0}, "uv_size": []float64{slice, borderTileH}},
					"south": map[string]any{"uv": []float64{u + slice, 0}, "uv_size": []float64{-slice, borderTileH}},
				},
			})
		}
		bones = append(bones, map[string]any{"name": fmt.Sprintf("c%d", c), "parent": "root", "pivot": []int{0, 0, 0}, "cubes": cubes})
	}
	return borderModel(id, bones)
}

// borderModel is a model of the bones.
func borderModel(id string, bones []any) map[string]any {
	return map[string]any{
		"format_version": "1.12.0",
		"minecraft:geometry": []any{map[string]any{
			"description": map[string]any{
				"identifier":            id,
				"texture_width":         borderTexW,
				"texture_height":        borderTexH,
				"visible_bounds_width":  borderVisible,
				"visible_bounds_height": borderVisible,
				"visible_bounds_offset": []int{0, 0, 0},
			},
			"bones": bones,
		}},
	}
}

// borderRingGeometry is a whole square, once for each anchor (world.BorderRingAnchor), each at its
// place in the world as seen from that anchor. A model's z runs against the world's, so the walls
// are at minus their distance. The texture goes by the world's position, so every anchor's wall
// looks the same.
func borderRingGeometry(id string, minX, minZ, maxX, maxZ float64) map[string]any {
	bones := []any{map[string]any{"name": "root", "pivot": []int{0, 0, 0}}}
	nx, nz := world.BorderRingCells(minX, maxX), world.BorderRingCells(minZ, maxZ)
	for i := range nx {
		for j := range nz {
			ax, az := world.BorderRingAnchor(minX, i), world.BorderRingAnchor(minZ, j)
			var cubes []any
			for _, line := range []float64{minZ, maxZ} {
				eachBorderTile(minX, maxX, func(from, to, u float64) {
					cubes = append(cubes, borderRingCubes(
						[3]float64{from - ax, 0, az - line}, [3]float64{to - from, 0, 0}, "north", "south", u)...)
				})
			}
			for _, line := range []float64{minX, maxX} {
				eachBorderTile(minZ, maxZ, func(from, to, u float64) {
					cubes = append(cubes, borderRingCubes(
						[3]float64{line - ax, 0, az - to}, [3]float64{0, 0, to - from}, "east", "west", u)...)
				})
			}
			bones = append(bones, map[string]any{"name": fmt.Sprintf("a%d", i*nz+j), "parent": "root", "pivot": []int{0, 0, 0}, "cubes": cubes})
		}
	}
	return borderModel(id, bones)
}

// eachBorderTile cuts from..to into stretches the texture can show in one go, starting where it
// repeats. u is where in the texture a stretch starts.
func eachBorderTile(from, to float64, f func(from, to, u float64)) {
	for v := math.Floor(from/borderWallTile) * borderWallTile; v < to; v += borderRingSpan {
		a, b := max(v, from), min(v+borderRingSpan, to)
		f(a, b, a-v)
	}
}

// borderRingCubes is a stretch of wall from its corner, from bottom to top, in blocks. front and
// back are the faces it is seen by.
func borderRingCubes(at, size [3]float64, front, back string, u float64) []any {
	const unit = 16 // model units per block
	width := size[0] + size[2]
	var cubes []any
	for y := -borderWallHeight / 2; y < borderWallHeight/2; y += borderRingSpan {
		cubes = append(cubes, map[string]any{
			"origin": []float64{at[0] * unit, float64(y * unit), at[2] * unit},
			"size":   []float64{size[0] * unit, borderRingSpan * unit, size[2] * unit},
			"uv": map[string]any{
				front: map[string]any{"uv": []float64{u, 0}, "uv_size": []float64{width, borderRingSpan}},
				back:  map[string]any{"uv": []float64{u + width, 0}, "uv_size": []float64{-width, borderRingSpan}},
			},
		})
	}
	return cubes
}

// borderRingParts shows the bone of one anchor (the mark variant).
func borderRingParts(minX, minZ, maxX, maxZ float64) []any {
	parts := []any{map[string]any{"*": true}}
	for a := range world.BorderRingCells(minX, maxX) * world.BorderRingCells(minZ, maxZ) {
		parts = append(parts, map[string]any{fmt.Sprintf("a%d", a): fmt.Sprintf("query.mark_variant == %d", a)})
	}
	return parts
}

// borderColumns shows only the columns from first to end (the mark variant, first*128 + end).
func borderColumns(columns int) []any {
	parts := []any{map[string]any{"*": true}}
	for c := range columns {
		parts = append(parts, map[string]any{fmt.Sprintf("c%d", c): fmt.Sprintf(
			"math.mod(math.floor(query.mark_variant / 128), 128) <= %d && math.mod(query.mark_variant, 128) > %d", c, c)})
	}
	return parts
}

// borderAtlas repeats a square tile into the texture the planes show, so that one cube covers
// many tiles and no client has to wrap a texture.
func borderAtlas(tile []byte) []byte {
	img, err := png.Decode(bytes.NewReader(tile))
	if err != nil {
		panic(err)
	}
	size := img.Bounds().Dx()
	atlas := image.NewNRGBA(image.Rect(0, 0, size*borderTexW/borderWallTile, size*borderTexH/borderWallTile))
	for y := 0; y < atlas.Rect.Dy(); y += size {
		for x := 0; x < atlas.Rect.Dx(); x += size {
			draw.Draw(atlas, image.Rect(x, y, x+size, y+size), img, img.Bounds().Min, draw.Src)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, atlas); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// borderStripes draws a wall tile of Java's border in colour c, as a Java client draws it: its
// forcefield texture (16 pixels of thin diagonal stripes in four greys and alphas) once every 2
// blocks, multiplied by the colour, and glowing as on Java (added to what is behind). The pattern is drawn here, not copied from the game.
func borderStripes(c color.NRGBA) []byte {
	const javaTile = 16                                     // pixels of Java's texture, which covers 2 blocks
	const size = javaTile * borderWallTile / javaBorderTile // the wall tile, borderWallTile blocks
	// Grey and alpha of the four pixels of a stripe, from its leading edge.
	stripe := [4][2]uint32{{201, 171}, {174, 156}, {179, 126}, {153, 60}}
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			k := (x + y) % 8
			if k >= 4 {
				continue
			}
			g, a := stripe[k][0], stripe[k][1]
			// Drawn additively (Material.glow adds the colour, ignoring alpha, and drops pixels under
			// half alpha), so the colour is weighted by the alpha here, as Java weights it when it adds.
			f := g * a
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(uint32(c.R) * f / (255 * 255)), G: uint8(uint32(c.G) * f / (255 * 255)), B: uint8(uint32(c.B) * f / (255 * 255)), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// javaBorderTile is how many blocks a tile of Java's border texture covers, and javaBorderScroll
// how long it takes to scroll one tile (diagonally), as a Java client draws it (WorldBorderRenderer).
const (
	javaBorderTile   = 2
	javaBorderScroll = 3 // seconds
)

// borderGlowMER makes a Vibrant Visuals MER map (red metalness, green emissive, blue roughness) for
// a border tile: emissive by how colourful each pixel is, so the glitch colours glow and the white and
// black stripes stay as they are. Classic graphics ignore it.
func borderGlowMER(tile []byte) []byte {
	img, err := png.Decode(bytes.NewReader(tile))
	if err != nil {
		panic(err)
	}
	b := img.Bounds()
	mer := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			hi, lo := max(c.R, c.G, c.B), min(c.R, c.G, c.B)
			var emissive uint8
			if hi > 0 && c.A > 0 {
				sat := float64(hi-lo) / float64(hi)
				if sat > 0.25 { // grey, white and black stay dark
					emissive = uint8(min(255.0, sat*float64(hi)*1.4))
				}
			}
			mer.SetNRGBA(x-b.Min.X, y-b.Min.Y, color.NRGBA{R: 0, G: emissive, B: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, mer); err != nil {
		panic(err)
	}
	return buf.Bytes()
}
