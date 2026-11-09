package world

import (
	"bytes"
	"cmp"
	"image/png"
	"math"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-gl/mathgl/mgl64"
)

// A world border, as on Java Edition: a square around a centre that players can't walk out of, and
// outside of which they are hurt. It can grow or shrink over time. Java clients draw it
// themselves. Bedrock clients have no world border, so with UseBorderWall a wall is drawn for them
// by the server's resource pack (session/border.go).

// BorderMaxSize is the size of a world without a border: the largest Java has.
const BorderMaxSize = 59999968

// BorderConfig sets a world's border.
type BorderConfig struct {
	// Center is the centre of the square, as (x, z).
	Center mgl64.Vec2
	// Size is the length of the square's side in blocks. 0 or more than BorderMaxSize: no border.
	Size float64
	// DamagePerBlock is the damage a player takes for each block they are past SafeZone outside
	// the border, at least 1. Java's default: 0.2.
	DamagePerBlock float64
	// SafeZone is how far outside the border a player can be without being hurt. Java's default: 5.
	SafeZone float64
	// WarningDistance (blocks) and WarningTime (how long before a moving border reaches the player)
	// tint Java players' screen red. Java's defaults: 5 blocks, 15 seconds.
	WarningDistance int
	WarningTime     time.Duration
	// Texture is a texture registered with RegisterBorderTexture for the Bedrock wall; empty: Java's
	// look (blue, red while shrinking, green while growing).
	Texture string
	// Region makes the border hold only the players inside it, and those who walked (or were
	// pushed) out of it: a player who arrives outside it any other way (a teleport, a respawn,
	// another world) is left alone and doesn't see it. Without Region it holds everyone in the
	// world, as on Java.
	Region bool
}

// DefaultBorderConfig is a border of size around center with Java's default damage and warnings.
func DefaultBorderConfig(center mgl64.Vec2, size float64) BorderConfig {
	return BorderConfig{Center: center, Size: size, DamagePerBlock: 0.2, SafeZone: 5, WarningDistance: 5, WarningTime: 15 * time.Second}
}

// Border is a world's border at one moment.
type Border struct {
	BorderConfig
	// TargetSize is the size a moving border is heading for, reached after Remaining. Remaining 0:
	// the border is not moving.
	TargetSize float64
	Remaining  time.Duration
	// Version changes with every change made to the border (not with its movement).
	Version uint64
}

// Enabled reports whether the world has a border.
func (b Border) Enabled() bool { return b.Size > 0 && b.Size < BorderMaxSize }

// Shrinking and Growing report whether the border is moving, and which way.
func (b Border) Shrinking() bool { return b.Remaining > 0 && b.TargetSize < b.Size }
func (b Border) Growing() bool   { return b.Remaining > 0 && b.TargetSize > b.Size }

// Bounds returns the border's square as its smallest and largest x and z.
func (b Border) Bounds() (minX, minZ, maxX, maxZ float64) {
	h := b.Size / 2
	return b.Center[0] - h, b.Center[1] - h, b.Center[0] + h, b.Center[1] + h
}

// Contains reports whether pos is inside the border. A world without one contains everything.
func (b Border) Contains(pos mgl64.Vec3) bool {
	if !b.Enabled() {
		return true
	}
	minX, minZ, maxX, maxZ := b.Bounds()
	return pos[0] >= minX && pos[0] <= maxX && pos[2] >= minZ && pos[2] <= maxZ
}

// DistanceOutside is how far pos is outside the border, or 0 if it is inside.
func (b Border) DistanceOutside(pos mgl64.Vec3) float64 {
	if b.Contains(pos) {
		return 0
	}
	minX, minZ, maxX, maxZ := b.Bounds()
	dx := max(minX-pos[0], 0, pos[0]-maxX)
	dz := max(minZ-pos[2], 0, pos[2]-maxZ)
	return math.Hypot(dx, dz)
}

// Clamp moves pos to inside the border, margin blocks from its edge.
func (b Border) Clamp(pos mgl64.Vec3, margin float64) mgl64.Vec3 {
	if !b.Enabled() {
		return pos
	}
	minX, minZ, maxX, maxZ := b.Bounds()
	pos[0] = min(max(pos[0], minX+margin), maxX-margin)
	pos[2] = min(max(pos[2], minZ+margin), maxZ-margin)
	return pos
}

// BorderViewer is a player's session that shows the border. ViewBorder is called every tick with
// the player's world's border and the player's position.
type BorderViewer interface {
	ViewBorder(b Border, pos mgl64.Vec3)
}

// borderState is a world's border: the set config, and its movement.
type borderState struct {
	mu        sync.Mutex
	cfg       BorderConfig
	from      float64
	start     time.Time
	duration  time.Duration
	version   uint64
	hasBorder bool
}

var (
	borders        sync.Map // *World -> *borderState
	borderVersions atomic.Uint64
	// bordersSet is set once any world got a border (SetBorder): until then no world has one, and the border costs
	// players nothing each tick.
	bordersSet atomic.Bool
)

func (w *World) borderState() *borderState {
	if s, ok := borders.Load(w); ok {
		return s.(*borderState)
	}
	s, _ := borders.LoadOrStore(w, &borderState{})
	return s.(*borderState)
}

// BordersSet reports whether a border was ever set in any world (SetBorder).
func BordersSet() bool { return bordersSet.Load() }

// Border returns the world's border now.
func (w *World) Border() Border {
	if !bordersSet.Load() {
		return Border{BorderConfig: BorderConfig{Size: BorderMaxSize}, TargetSize: BorderMaxSize}
	}
	s := w.borderState()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasBorder {
		return Border{BorderConfig: BorderConfig{Size: BorderMaxSize}, TargetSize: BorderMaxSize}
	}
	b := Border{BorderConfig: s.cfg, TargetSize: s.cfg.Size, Version: s.version}
	if s.duration > 0 {
		elapsed := time.Since(s.start)
		if elapsed < s.duration {
			b.Size = s.from + (s.cfg.Size-s.from)*float64(elapsed)/float64(s.duration)
			b.Remaining = s.duration - elapsed
		}
	}
	return b
}

// SetBorder sets the world's border at once; a moving border stops. A Size of 0 removes it.
func (w *World) SetBorder(c BorderConfig) {
	bordersSet.Store(true)
	s := w.borderState()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hasBorder = c.Size > 0 && c.Size < BorderMaxSize
	s.cfg, s.duration = c, 0
	s.version = borderVersions.Add(1)
}

// MoveBorder grows or shrinks the world's border from its size now to size over d. It does
// nothing in a world without a border.
func (w *World) MoveBorder(size float64, d time.Duration) {
	now := w.Border()
	if !now.Enabled() {
		return
	}
	s := w.borderState()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.from, s.cfg.Size = now.Size, size
	s.start, s.duration = time.Now(), d
	s.version = borderVersions.Add(1)
}

// BorderTexture is a texture for the Bedrock border wall.
type BorderTexture struct {
	Name string
	// PNG is the texture, square, tiled every 4 blocks along the wall.
	PNG []byte
}

var (
	borderWall     atomic.Bool
	borderTexMu    sync.Mutex
	borderTextures []BorderTexture
)

// UseBorderWall draws the border for Bedrock players, as a wall in the server's resource pack.
// Call it before the server is started; without it Bedrock players don't see the border (it still
// hurts them and keeps them in).
func UseBorderWall() { borderWall.Store(true) }

// BorderWall reports whether UseBorderWall was called.
func BorderWall() bool { return borderWall.Load() }

// BorderRingCell is the distance in blocks between the anchors of the border ring. Clients draw an
// entity only while a box of at most 50 blocks around it is in view, and that box always is while
// the camera is inside it, so a player is always well inside the box of the nearest anchor.
const BorderRingCell = 40

var borderRing atomic.Pointer[[4]float64]

// SetBorderRing builds the whole wall of a square into the resource pack, so that Bedrock players
// see it from afar while the world's border is exactly that square. Any other border is drawn only
// near the player. Call it before the server is started.
func SetBorderRing(minX, minZ, maxX, maxZ float64) {
	borderRing.Store(&[4]float64{minX, minZ, maxX, maxZ})
}

// BorderRing returns the square of SetBorderRing.
func BorderRing() (minX, minZ, maxX, maxZ float64, ok bool) {
	r := borderRing.Load()
	if r == nil {
		return 0, 0, 0, 0, false
	}
	return r[0], r[1], r[2], r[3], true
}

// BorderRingCells is how many anchors the ring has between from and to.
func BorderRingCells(from, to float64) int {
	return int(math.Ceil((to - from) / BorderRingCell))
}

// BorderRingAnchor is the position of anchor i of the ring, counted from the side at from.
func BorderRingAnchor(from float64, i int) float64 {
	return from + BorderRingCell/2 + float64(i*BorderRingCell)
}

// RegisterBorderTexture adds a texture the Bedrock border wall can be drawn with (BorderConfig's
// Texture), from a square PNG. It turns on the wall too. Call it before the server is started.
func RegisterBorderTexture(name string, pngData []byte) error {
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return err
	}
	if b := img.Bounds(); b.Dx() != b.Dy() {
		return errNotSquare
	}
	UseBorderWall()
	borderTexMu.Lock()
	defer borderTexMu.Unlock()
	borderTextures = slices.DeleteFunc(borderTextures, func(t BorderTexture) bool { return t.Name == name })
	borderTextures = append(borderTextures, BorderTexture{Name: name, PNG: pngData})
	slices.SortFunc(borderTextures, func(a, b BorderTexture) int { return cmp.Compare(a.Name, b.Name) })
	return nil
}

// BorderTextures returns the registered border textures, sorted by name.
func BorderTextures() []BorderTexture {
	borderTexMu.Lock()
	defer borderTexMu.Unlock()
	return slices.Clone(borderTextures)
}

type borderError string

func (e borderError) Error() string { return string(e) }

const errNotSquare = borderError("world: border texture must be square")
