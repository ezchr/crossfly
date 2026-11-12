package warps

import (
	"fmt"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Wrap preserves the host handler and intercepts only warp use and editor clicks.
func (m *Manager) Wrap(h player.Handler) player.Handler {
	if h == nil {
		h = player.NopHandler{}
	}
	return handler{Handler: h, m: m}
}

type handler struct {
	player.Handler
	m *Manager
}

func (h handler) HandleTeleport(ctx *player.Context, pos mgl64.Vec3) {
	h.Handler.HandleTeleport(ctx, pos)
	if !ctx.Cancelled() {
		h.m.mu.Lock()
		delete(h.m.pending, ctx.Player().UUID())
		h.m.mu.Unlock()
	}
}

func (h handler) HandleMove(ctx *player.Context, to mgl64.Vec3, rot cube.Rotation) {
	h.Handler.HandleMove(ctx, to, rot)
	if ctx.Cancelled() {
		return
	}
	p := ctx.Player()
	if cube.PosFromVec3(p.Position()) == cube.PosFromVec3(to) {
		return
	}
	m := h.m
	m.mu.Lock()
	if ed := m.editing[p.UUID()]; ed != nil {
		if !m.config.Admin(p) {
			delete(m.editing, p.UUID())
			p.SetBlockPreviews(nil)
		} else if ed.world != p.Tx().World() || ed.at.Sub(to).LenSqr() >= 144 || ed.revision != m.revision {
			m.drawLocked(p, ed, to)
		}
		m.mu.Unlock()
		return
	}
	if time.Now().Before(m.cooldown[p.UUID()]) || m.config.CanWarp != nil && !m.config.CanWarp(p) {
		m.mu.Unlock()
		return
	}
	name := m.worldName(p.Tx().World())
	var chosen *Warp
	var locked string
	for _, w := range m.warps {
		if !w.Enabled || w.Destination == nil || w.World != name || !w.has(cube.PosFromVec3(to)) {
			continue
		}
		if !m.allowed(p, w) {
			if locked == "" && w.Rank != "" {
				locked = w.Rank
			}
			continue
		}
		if m.config.Worlds[w.Destination.World] == nil {
			continue
		}
		chosen = w
		break
	}
	if chosen == nil {
		if locked != "" {
			m.cooldown[p.UUID()] = time.Now().Add(time.Second)
			p.SendPopup("§cRequires rank: " + locked)
		}
		m.mu.Unlock()
		return
	}
	dest, fade, message := *chosen.Destination, chosen.Fade, chosen.Message
	m.cooldown[p.UUID()] = time.Now().Add(time.Second)
	m.nextTicket++
	ticket := m.nextTicket
	m.pending[p.UUID()] = ticket
	m.mu.Unlock()
	if fade {
		p.AddEffect(effect.New(effect.Blindness, 1, 1500*time.Millisecond).WithoutParticles())
		player.DoAfter(p.H(), 300*time.Millisecond, func(_ *world.Tx, p *player.Player) { m.finishWarp(p, ticket, dest, message) })
	} else {
		ctx.Cancel()
		m.finishWarp(p, ticket, dest, message)
	}
}

func (m *Manager) allowed(p *player.Player, w *Warp) bool {
	if m.config.Admin(p) {
		return w.Deny == ""
	}
	return (w.Rank == "" || m.config.RankAtLeast != nil && m.config.RankAtLeast(p, w.Rank)) &&
		(w.Require == "" || m.config.HasTag != nil && m.config.HasTag(p, w.Require)) &&
		(w.Deny == "" || m.config.HasTag == nil || !m.config.HasTag(p, w.Deny))
}

func (m *Manager) finishWarp(p *player.Player, ticket uint64, d Destination, message string) {
	m.mu.Lock()
	valid := m.pending[p.UUID()] == ticket && m.editing[p.UUID()] == nil
	if m.pending[p.UUID()] == ticket {
		delete(m.pending, p.UUID())
	}
	m.mu.Unlock()
	if !valid || m.config.CanWarp != nil && !m.config.CanWarp(p) {
		return
	}
	m.teleport(p, d)
	if message != "" {
		p.Message(colours(message))
	}
}

func (m *Manager) teleport(p *player.Player, d Destination) {
	w := m.config.Worlds[d.World]
	if w == nil {
		p.Message("§cDestination world is unavailable: " + d.World)
		return
	}
	if w == p.Tx().World() {
		p.Teleport(d.Position)
		rotate(p, d.Rotation)
		p.ResetFallDistance()
		return
	}
	handle := p.Tx().RemoveEntity(p)
	if handle == nil {
		return
	}
	w.Do(func(tx *world.Tx) {
		if np, ok := tx.AddEntityAt(handle, d.Position).(*player.Player); ok {
			rotate(np, d.Rotation)
			np.ResetFallDistance()
		}
	})
}
func rotate(p *player.Player, r cube.Rotation) {
	p.Move(mgl64.Vec3{}, r[0]-p.Rotation()[0], r[1]-p.Rotation()[1])
}

func (h handler) HandleStartBreak(ctx *player.Context, pos cube.Pos) {
	if h.m.removeCell(ctx, pos) {
		return
	}
	h.Handler.HandleStartBreak(ctx, pos)
}
func (h handler) HandleBlockBreak(ctx *player.Context, pos cube.Pos, drops *[]item.Stack, xp *int) {
	if h.m.removeCell(ctx, pos) {
		return
	}
	h.Handler.HandleBlockBreak(ctx, pos, drops, xp)
}
func (h handler) HandleItemUseOnBlock(ctx *player.Context, pos cube.Pos, face cube.Face, click mgl64.Vec3) {
	if h.m.addCell(ctx, pos, face) {
		return
	}
	h.Handler.HandleItemUseOnBlock(ctx, pos, face, click)
}
func (h handler) HandleQuit(p *player.Player) {
	h.m.mu.Lock()
	delete(h.m.editing, p.UUID())
	delete(h.m.cooldown, p.UUID())
	delete(h.m.pending, p.UUID())
	h.m.mu.Unlock()
	p.SetBlockPreviews(nil)
	h.Handler.HandleQuit(p)
}
func (h handler) HandleChangeWorld(p *player.Player, before, after *world.World) {
	h.Handler.HandleChangeWorld(p, before, after)
	h.m.mu.Lock()
	delete(h.m.pending, p.UUID())
	if ed := h.m.editing[p.UUID()]; ed != nil {
		if h.m.config.Admin(p) {
			h.m.drawLocked(p, ed, p.Position())
		} else {
			delete(h.m.editing, p.UUID())
		}
	}
	h.m.mu.Unlock()
}

func (m *Manager) drawLocked(p *player.Player, ed *edit, at mgl64.Vec3) int {
	shell := map[cube.Pos]world.Block{}
	count := 0
	for _, w := range m.warps {
		if w.World != m.worldName(p.Tx().World()) || w.volume() > maxEditVolume && w.cells == nil {
			continue
		}
		dist := 0.0
		for a := range 3 {
			v := max(0, float64(w.Min[a])-at[a], at[a]-float64(w.Max[a])-1)
			dist += v * v
		}
		if dist > 48*48 {
			continue
		}
		count++
		show := func(pos cube.Pos) {
			if pos.Vec3Centre().Sub(at).LenSqr() > 48*48 {
				return
			}
			inside := true
			for _, face := range cube.Faces() {
				if !w.has(pos.Side(face)) {
					inside = false
					break
				}
			}
			if inside {
				return
			}
			b, loaded := p.Tx().BlockLoaded(pos)
			if !loaded || len(b.Model().BBox(pos, p.Tx())) > 0 {
				return
			}
			shell[pos] = block.Glass{}
		}
		if w.cells != nil {
			for pos := range w.cells {
				show(pos)
			}
		} else {
			for x := w.Min[0]; x <= w.Max[0]; x++ {
				for y := w.Min[1]; y <= w.Max[1]; y++ {
					for z := w.Min[2]; z <= w.Max[2]; z++ {
						show(cube.Pos{x, y, z})
					}
				}
			}
		}
	}
	p.SetBlockPreviews(shell)
	ed.at, ed.world, ed.revision = at, p.Tx().World(), m.revision
	return count
}

func handGlass(p *player.Player) {
	for i := 0; i < 9; i++ {
		s, _ := p.Inventory().Item(i)
		if _, ok := s.Item().(block.Glass); ok {
			return
		}
	}
	for i := 0; i < 9; i++ {
		s, _ := p.Inventory().Item(i)
		if s.Empty() {
			p.Inventory().SetItem(i, item.NewStack(block.Glass{}, 64))
			return
		}
	}
	p.Message("§eFree a hotbar slot and run /warp edit <name> again to receive glass.")
}

func (m *Manager) warpAt(ed *edit, world string, pos cube.Pos) *Warp {
	if w := m.get(ed.picked); w != nil && w.World == world && w.has(pos) {
		return w
	}
	for _, w := range m.warps {
		if w.World == world && w.has(pos) {
			return w
		}
	}
	return nil
}

func (m *Manager) removeCell(ctx *player.Context, pos cube.Pos) bool {
	p := ctx.Player()
	m.mu.Lock()
	revision := m.revision
	defer func() {
		changed := m.revision != revision
		m.mu.Unlock()
		if changed {
			m.refreshEditors(p.H())
		}
	}()
	ed := m.editing[p.UUID()]
	if ed == nil {
		return false
	}
	if !m.config.Admin(p) {
		delete(m.editing, p.UUID())
		p.SetBlockPreviews(nil)
		return false
	}
	w := m.warpAt(ed, m.worldName(p.Tx().World()), pos)
	if w == nil {
		return false
	}
	ctx.Cancel()
	removed := 0
	for _, other := range m.warps {
		if other.World != w.World || !other.has(pos) {
			continue
		}
		if !other.shapeable() {
			p.SendPopup("§eArea too large; use /warp area " + other.Name)
			continue
		}
		if len(other.cells) <= 1 {
			p.SendPopup("§eCannot remove the last block of " + other.Name)
			continue
		}
		delete(other.cells, pos)
		other.fit()
		removed++
	}
	ed.picked = w.Name
	handGlass(p)
	if removed > 0 {
		m.changedLocked(p)
		p.SendPopup(fmt.Sprintf("§aRemoved block from %d warp(s). Selected: %s", removed, w.Name))
	}
	return true
}

func (m *Manager) addCell(ctx *player.Context, pos cube.Pos, face cube.Face) bool {
	p := ctx.Player()
	m.mu.Lock()
	revision := m.revision
	defer func() {
		changed := m.revision != revision
		m.mu.Unlock()
		if changed {
			m.refreshEditors(p.H())
		}
	}()
	ed := m.editing[p.UUID()]
	if ed == nil {
		return false
	}
	if !m.config.Admin(p) {
		delete(m.editing, p.UUID())
		p.SetBlockPreviews(nil)
		return false
	}
	main, _ := p.HeldItems()
	if _, ok := main.Item().(block.Glass); !ok {
		return false
	}
	ctx.Cancel()
	target := pos.Side(face)
	worldName := m.worldName(p.Tx().World())
	w := m.warpAt(ed, worldName, pos)
	if w == nil {
		for _, f := range cube.Faces() {
			w = m.warpAt(ed, worldName, target.Side(f))
			if w != nil {
				break
			}
		}
	}
	if w == nil {
		w = m.get(ed.picked)
	}
	if w == nil || w.World != worldName {
		p.SendPopup("§ePick a warp: /warp edit <name>")
		return true
	}
	if !validBox(target, target) || !w.shapeable() || len(w.cells) >= maxEditVolume {
		p.SendPopup("§eArea too large; use /warp area " + w.Name)
		return true
	}
	if !w.cells[target] {
		w.cells[target] = true
		w.fit()
		m.changedLocked(p)
	}
	ed.picked = w.Name
	p.SendPopup("§aAdded area block to " + w.Name)
	return true
}

func (m *Manager) changedLocked(p *player.Player) {
	m.revision++
	if err := m.saveLocked(); err != nil {
		p.Message("§cChanged in memory but failed to save warps: ", err)
	}
	// Refresh the actor before returning from the click, then the other editors on their owners.
	if ed := m.editing[p.UUID()]; ed != nil {
		m.drawLocked(p, ed, p.Position())
	}
}

func (m *Manager) refreshEditors(except *world.EntityHandle) {
	m.mu.Lock()
	var handles []*world.EntityHandle
	for _, ed := range m.editing {
		if ed.handle != nil && ed.handle != except {
			handles = append(handles, ed.handle)
		}
	}
	m.mu.Unlock()
	for _, handle := range handles {
		player.Do(handle, func(_ *world.Tx, p *player.Player) {
			m.mu.Lock()
			defer m.mu.Unlock()
			if ed := m.editing[p.UUID()]; ed != nil {
				if m.config.Admin(p) {
					m.drawLocked(p, ed, p.Position())
				} else {
					delete(m.editing, p.UUID())
					p.SetBlockPreviews(nil)
				}
			}
		})
	}
}
