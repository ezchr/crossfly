package warps

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type Action string

func (Action) Type() string { return "WarpAction" }
func (Action) Options(cmd.Source) []string {
	return []string{"create", "set", "dest", "area", "edit", "require", "deny", "rank", "message", "fade", "on", "off", "rename", "delete", "remove", "list", "info", "tp"}
}

type command struct {
	Action    Action                    `cmd:"action"`
	Arguments cmd.Optional[cmd.Varargs] `cmd:"arguments"`
	m         *Manager
}
type helpCommand struct{ m *Manager }

func (c command) Allow(s cmd.Source) bool {
	p, ok := s.(*player.Player)
	return ok && c.m.config.Admin(p)
}
func (c helpCommand) Allow(s cmd.Source) bool {
	p, ok := s.(*player.Player)
	return ok && c.m.config.Admin(p)
}
func (c helpCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) { help(o) }
func (c command) Run(s cmd.Source, o *cmd.Output, _ *world.Tx) {
	p, ok := s.(*player.Player)
	if !ok || !c.m.config.Admin(p) {
		o.Error("Only administrators may manage warps.")
		return
	}
	args := strings.Fields(string(c.Arguments.LoadOr("")))
	if err := c.m.run(p, string(c.Action), args, o); err != nil {
		o.Error(err)
	}
}

// Register adds the administrator-only /warp command. Call once at startup, after Open.
func (m *Manager) Register() {
	cmd.Register(cmd.New("warp", "Walk-in warp pads and glass area editor", nil, command{m: m}, helpCommand{m: m}))
}

func help(o *cmd.Output) {
	o.Print("§b/warp create <name> [x1 y1 z1 x2 y2 z2] — trigger area, defaults to your feet")
	o.Print("§b/warp dest <name> here|<x y z> [yaw pitch] — destination")
	o.Print("§b/warp edit [name] — toggle editor; break glass to shrink, place glass to expand")
	o.Print("§b/warp area <name> [x1 y1 z1 x2 y2 z2]; /warp list|info|tp|delete|on|off <name>")
	o.Print("§b/warp require|deny|rank <name> <value|none>; message <name> <text|none>; fade <name> on|off; rename <name> <new>")
}
func colours(s string) string { return strings.ReplaceAll(s, "&", "§") }

func (m *Manager) run(p *player.Player, sub string, a []string, o *cmd.Output) error {
	m.mu.Lock()
	revision := m.revision
	var after func()
	defer func() {
		changed := m.revision != revision
		m.mu.Unlock()
		if changed {
			m.refreshEditors(p.H())
		}
		if after != nil {
			after()
		}
	}()
	worldName := m.worldName(p.Tx().World())
	if sub == "list" {
		if len(m.warps) == 0 {
			o.Print("§eNo warps. /warp create <name>")
		}
		for _, w := range m.warps {
			o.Print(describe(w))
		}
		return nil
	}
	if worldName == "" {
		return fmt.Errorf("This world has no registered warp name")
	}
	if sub == "edit" {
		if len(a) > 1 {
			return fmt.Errorf("Usage: /warp edit [name]")
		}
		ed := m.editing[p.UUID()]
		if len(a) == 0 && ed != nil {
			delete(m.editing, p.UUID())
			p.SetBlockPreviews(nil)
			o.Print("§aWarp editor off; areas saved.")
			return nil
		}
		if ed == nil {
			ed = &edit{handle: p.H()}
			m.editing[p.UUID()] = ed
		}
		if len(a) == 1 {
			w := m.get(a[0])
			if w == nil {
				if ed.world == nil {
					delete(m.editing, p.UUID())
				}
				return fmt.Errorf("No warp %s", a[0])
			}
			ed.picked = w.Name
		}
		delete(m.pending, p.UUID())
		handGlass(p)
		n := m.drawLocked(p, ed, p.Position())
		o.Printf("§aWarp editor on: %d nearby area(s). Break glass to shrink; place glass to expand. /warp edit ends it.", n)
		if ed.picked != "" {
			o.Printf("§bSelected %s.", ed.picked)
		}
		return nil
	}
	if len(a) < 1 {
		help(o)
		return nil
	}
	name := strings.ToLower(a[0])
	if !validName.MatchString(name) {
		return fmt.Errorf("Warp names use 1–64 letters, numbers, _ or -")
	}
	w := m.get(name)
	if sub == "create" || sub == "set" && w == nil {
		if w != nil {
			return fmt.Errorf("Warp %s already exists", name)
		}
		lo, hi, err := parseArea(p, a[1:])
		if err != nil {
			return err
		}
		w = &Warp{Name: name, World: worldName, Min: lo, Max: hi, Enabled: true}
		if sub == "set" {
			w.Destination = &Destination{worldName, p.Position(), p.Rotation()}
		}
		m.warps = append(m.warps, w)
		m.changedLocked(p)
		o.Print("§aCreated " + describe(w))
		if w.Destination == nil {
			o.Print("§eNow stand at the destination: /warp dest " + name + " here")
		}
		return nil
	}
	if w == nil {
		return fmt.Errorf("No warp %s", name)
	}
	switch sub {
	case "info":
		o.Print(describe(w))
		if w.Message != "" {
			o.Print("§7Message: " + colours(w.Message))
		}
		return nil
	case "tp":
		if len(a) != 1 {
			return fmt.Errorf("Usage: /warp tp <name>")
		}
		if w.Destination == nil {
			return fmt.Errorf("Warp has no destination")
		}
		d := *w.Destination
		after = func() { m.teleport(p, d) }
		return nil
	case "dest", "set":
		var d Destination
		if sub == "set" && len(a) == 1 || len(a) == 2 && strings.EqualFold(a[1], "here") {
			d = Destination{worldName, p.Position(), p.Rotation()}
		} else {
			if len(a) != 4 && len(a) != 6 {
				return fmt.Errorf("Usage: /warp dest <name> here|<x y z> [yaw pitch]")
			}
			pos, err := parsePosition(a[1:4], p.Position())
			if err != nil {
				return err
			}
			d = Destination{World: worldName, Position: pos}
			if len(a) == 6 {
				for i := range 2 {
					v, err := strconv.ParseFloat(a[4+i], 64)
					if err != nil {
						return fmt.Errorf("Yaw and pitch must be numbers")
					}
					d.Rotation[i] = v
				}
				if !finiteRotation(d.Rotation) {
					return fmt.Errorf("Invalid yaw or pitch")
				}
			}
		}
		w.Destination = &d
	case "area":
		lo, hi, err := parseArea(p, a[1:])
		if err != nil {
			return err
		}
		w.Min, w.Max, w.World, w.cells = lo, hi, worldName, nil
	case "require", "deny", "rank":
		if len(a) != 2 {
			return fmt.Errorf("Usage: /warp %s <name> <value|none>", sub)
		}
		v := a[1]
		if strings.EqualFold(v, "none") {
			v = ""
		}
		if sub == "rank" {
			v = strings.ToLower(v)
			if v != "" && m.config.RankExists != nil && !m.config.RankExists(v) {
				return fmt.Errorf("Unknown rank %s", v)
			}
			if v != "" && m.config.RankAtLeast == nil {
				return fmt.Errorf("This host has no ranks configured")
			}
			w.Rank = v
		} else {
			if v != "" && m.config.HasTag == nil {
				return fmt.Errorf("This host has no tags configured")
			}
			if sub == "require" {
				w.Require = v
			} else {
				w.Deny = v
			}
		}
	case "message":
		if len(a) < 2 {
			return fmt.Errorf("Usage: /warp message <name> <text|none>")
		}
		w.Message = strings.Join(a[1:], " ")
		if strings.EqualFold(w.Message, "none") {
			w.Message = ""
		}
	case "fade":
		if len(a) != 2 || a[1] != "on" && a[1] != "off" {
			return fmt.Errorf("Usage: /warp fade <name> on|off")
		}
		w.Fade = a[1] == "on"
	case "on", "off":
		if len(a) != 1 {
			return fmt.Errorf("Usage: /warp %s <name>", sub)
		}
		w.Enabled = sub == "on"
	case "rename":
		if len(a) != 2 || !validName.MatchString(strings.ToLower(a[1])) {
			return fmt.Errorf("Usage: /warp rename <name> <new-name>")
		}
		n := strings.ToLower(a[1])
		if m.get(n) != nil {
			return fmt.Errorf("Name %s is already taken", n)
		}
		w.Name = n
		for _, ed := range m.editing {
			if ed.picked == name {
				ed.picked = n
			}
		}
	case "delete", "remove":
		if len(a) != 1 {
			return fmt.Errorf("Usage: /warp delete <name>")
		}
		for i, v := range m.warps {
			if v == w {
				m.warps = append(m.warps[:i], m.warps[i+1:]...)
				break
			}
		}
		for _, ed := range m.editing {
			if ed.picked == name {
				ed.picked = ""
			}
		}
		m.changedLocked(p)
		o.Print("§eDeleted " + name)
		return nil
	default:
		help(o)
		return nil
	}
	m.changedLocked(p)
	o.Print("§aUpdated " + describe(w))
	return nil
}

func parsePosition(a []string, base mgl64.Vec3) (mgl64.Vec3, error) {
	var pos mgl64.Vec3
	for i, t := range a {
		v := t
		relative := strings.HasPrefix(t, "~")
		if relative {
			v = strings.TrimPrefix(t, "~")
			if v == "" {
				v = "0"
			}
		}
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return pos, fmt.Errorf("Coordinates must be numbers or ~ offsets")
		}
		if relative {
			n += base[i]
		}
		pos[i] = n
	}
	if !finite(pos) {
		return pos, fmt.Errorf("Coordinates are outside the world limits")
	}
	return pos, nil
}
func parseArea(p *player.Player, a []string) (cube.Pos, cube.Pos, error) {
	if len(a) == 0 {
		pos := cube.PosFromVec3(p.Position())
		return pos, pos, nil
	}
	if len(a) != 6 {
		return cube.Pos{}, cube.Pos{}, fmt.Errorf("Give both corners: x1 y1 z1 x2 y2 z2, or leave them out for your feet")
	}
	one, err := parsePosition(a[:3], p.Position())
	if err != nil {
		return cube.Pos{}, cube.Pos{}, err
	}
	two, err := parsePosition(a[3:], p.Position())
	if err != nil {
		return cube.Pos{}, cube.Pos{}, err
	}
	lo, hi := cube.PosFromVec3(one), cube.PosFromVec3(two)
	for i := range 3 {
		lo[i], hi[i] = min(lo[i], hi[i]), max(lo[i], hi[i])
	}
	if lo[1] < p.Tx().Range()[0] || hi[1] > p.Tx().Range()[1] {
		return lo, hi, fmt.Errorf("Area is outside this world's build height")
	}
	return lo, hi, nil
}
func describe(w *Warp) string {
	dest := "no destination"
	if d := w.Destination; d != nil {
		dest = fmt.Sprintf("%s %.1f %.1f %.1f", d.World, d.Position[0], d.Position[1], d.Position[2])
	}
	area := fmt.Sprintf("%v to %v", w.Min, w.Max)
	if w.cells != nil {
		area += fmt.Sprintf(" (%d shaped blocks)", len(w.cells))
	}
	state := ""
	if !w.Enabled {
		state += " [OFF]"
	}
	if w.Rank != "" {
		state += " rank=" + w.Rank
	}
	if w.Require != "" {
		state += " require=" + w.Require
	}
	if w.Deny != "" {
		state += " deny=" + w.Deny
	}
	if w.Fade {
		state += " fade=on"
	}
	return fmt.Sprintf("%s%s: %s %s -> %s", w.Name, state, w.World, area, dest)
}
