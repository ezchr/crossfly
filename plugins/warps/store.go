// Package warps implements walk-in warp pads and a private glass area editor.
// Hosts supply their own permissions, ranks, tags and named world registry.
package warps

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

const maxEditVolume = 50_000

type Destination struct {
	World    string        `json:"world"`
	Position mgl64.Vec3    `json:"position"`
	Rotation cube.Rotation `json:"rotation"`
}

type Warp struct {
	Name                         string `json:"name"`
	World                        string `json:"world"`
	Min, Max                     cube.Pos
	Destination                  *Destination `json:"destination,omitempty"`
	Enabled                      bool         `json:"enabled"`
	Fade                         bool         `json:"fade,omitempty"`
	Require, Deny, Rank, Message string
	// nil is the whole cuboid. A non-nil slice stores a hand-shaped area.
	Cells []cube.Pos `json:"cells,omitempty"`
	cells map[cube.Pos]bool
}

func (w *Warp) has(pos cube.Pos) bool {
	for axis := range 3 {
		if pos[axis] < w.Min[axis] || pos[axis] > w.Max[axis] {
			return false
		}
	}
	return w.cells == nil || w.cells[pos]
}

func (w *Warp) volume() int64 {
	v := int64(1)
	for a := range 3 {
		n := int64(w.Max[a]) - int64(w.Min[a]) + 1
		if n <= 0 || v > math.MaxInt64/n {
			return math.MaxInt64
		}
		v *= n
	}
	return v
}

func (w *Warp) shapeable() bool {
	if w.cells != nil {
		return true
	}
	if w.volume() > maxEditVolume {
		return false
	}
	w.cells = make(map[cube.Pos]bool, w.volume())
	for x := w.Min[0]; x <= w.Max[0]; x++ {
		for y := w.Min[1]; y <= w.Max[1]; y++ {
			for z := w.Min[2]; z <= w.Max[2]; z++ {
				w.cells[cube.Pos{x, y, z}] = true
			}
		}
	}
	return true
}

func (w *Warp) fit() {
	if len(w.cells) == 0 {
		return
	}
	first := true
	for p := range w.cells {
		if first {
			w.Min, w.Max, first = p, p, false
		}
		for a := range 3 {
			w.Min[a] = min(w.Min[a], p[a])
			w.Max[a] = max(w.Max[a], p[a])
		}
	}
	if w.volume() == int64(len(w.cells)) {
		w.cells = nil
	}
}

// Config adapts the plugin to a host without depending on its other plugins.
type Config struct {
	File        string
	Worlds      map[string]*world.World
	Admin       func(*player.Player) bool
	HasTag      func(*player.Player, string) bool
	RankAtLeast func(*player.Player, string) bool
	RankExists  func(string) bool
	// CanWarp may enforce host rules such as combat or login restrictions.
	CanWarp func(*player.Player) bool
}

type edit struct {
	handle   *world.EntityHandle
	picked   string
	world    *world.World
	at       mgl64.Vec3
	revision uint64
	// shown is the glass last sent to the editor; guard holds area blocks just broken
	// off over a real block, so the rest of that click cannot break the real block.
	shown map[cube.Pos]world.Block
	guard map[cube.Pos]time.Time
}

type Manager struct {
	mu         sync.Mutex
	config     Config
	warps      []*Warp
	editing    map[uuid.UUID]*edit
	cooldown   map[uuid.UUID]time.Time
	pending    map[uuid.UUID]uint64
	revision   uint64
	nextTicket uint64
	names      atomic.Pointer[[]string]
}

var validName = regexp.MustCompile(`^[a-z0-9_-]{1,64}$`)

// Open loads saved warps. Invalid data returns an error without overwriting the file.
func Open(c Config) (*Manager, error) {
	if c.File == "" {
		c.File = "warps.json"
	}
	if len(c.Worlds) == 0 || c.Admin == nil {
		return nil, fmt.Errorf("warps: worlds and Admin callback are required")
	}
	c.Worlds = mapsClone(c.Worlds)
	m := &Manager{config: c, editing: map[uuid.UUID]*edit{}, cooldown: map[uuid.UUID]time.Time{}, pending: map[uuid.UUID]uint64{}}
	raw, err := os.ReadFile(c.File)
	if os.IsNotExist(err) {
		m.publishNames()
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	var data struct {
		Warps []*Warp `json:"warps"`
	}
	if err = json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, w := range data.Warps {
		if w == nil || !validName.MatchString(w.Name) || seen[w.Name] {
			return nil, fmt.Errorf("warps: invalid or duplicate name")
		}
		seen[w.Name] = true
		if !validBox(w.Min, w.Max) || w.World == "" {
			return nil, fmt.Errorf("warps: invalid area for %s", w.Name)
		}
		if w.Destination != nil && (!finite(w.Destination.Position) || !finiteRotation(w.Destination.Rotation) || w.Destination.World == "") {
			return nil, fmt.Errorf("warps: invalid destination for %s", w.Name)
		}
		if w.Cells != nil {
			if len(w.Cells) == 0 || len(w.Cells) > maxEditVolume {
				return nil, fmt.Errorf("warps: invalid shaped area for %s", w.Name)
			}
			w.cells = map[cube.Pos]bool{}
			for _, p := range w.Cells {
				if !insideBox(p, w.Min, w.Max) {
					return nil, fmt.Errorf("warps: cell outside %s", w.Name)
				}
				w.cells[p] = true
			}
		}
		m.warps = append(m.warps, w)
	}
	m.publishNames()
	return m, nil
}

func mapsClone[K comparable, V any](m map[K]V) map[K]V {
	n := make(map[K]V, len(m))
	for k, v := range m {
		n[k] = v
	}
	return n
}
func insideBox(p, lo, hi cube.Pos) bool {
	for a := range 3 {
		if p[a] < lo[a] || p[a] > hi[a] {
			return false
		}
	}
	return true
}
func validBox(lo, hi cube.Pos) bool {
	for a := range 3 {
		if lo[a] > hi[a] || lo[a] < -30_000_000 || hi[a] > 30_000_000 {
			return false
		}
	}
	return lo[1] >= -2048 && hi[1] <= 2047
}
func finite(p mgl64.Vec3) bool {
	for _, v := range p {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 30_000_000 {
			return false
		}
	}
	return p[1] >= -2048 && p[1] <= 2047
}
func finiteRotation(r cube.Rotation) bool {
	return !math.IsNaN(r[0]) && !math.IsNaN(r[1]) && !math.IsInf(r[0], 0) && !math.IsInf(r[1], 0) && r[1] >= -90 && r[1] <= 90
}

func (m *Manager) get(name string) *Warp {
	for _, w := range m.warps {
		if w.Name == strings.ToLower(name) {
			return w
		}
	}
	return nil
}
func (m *Manager) worldName(w *world.World) string {
	for n, v := range m.config.Worlds {
		if v == w {
			return n
		}
	}
	return ""
}

// saveLocked uses an atomic rename, so a failed write never truncates the previous configuration.
func (m *Manager) saveLocked() error {
	for _, w := range m.warps {
		w.Cells = nil
		if w.cells != nil {
			for p := range w.cells {
				w.Cells = append(w.Cells, p)
			}
			slices.SortFunc(w.Cells, func(a, b cube.Pos) int {
				for i := range 3 {
					if a[i] != b[i] {
						return a[i] - b[i]
					}
				}
				return 0
			})
		}
	}
	raw, err := json.MarshalIndent(struct {
		Warps []*Warp `json:"warps"`
	}{m.warps}, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(m.config.File), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(m.config.File), ".warps-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(raw, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), m.config.File)
}
