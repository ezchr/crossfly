//go:build crossfly_full

package mobtest

import (
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/mob"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// Ref follows one entity across ticks and across Reload (by UUID).
type Ref struct {
	w  *World
	id uuid.UUID
	h  *world.EntityHandle
}

// UUID is the entity's UUID.
func (r *Ref) UUID() uuid.UUID { return r.id }

// H is the entity's current handle (nil when it is gone after a Reload).
func (r *Ref) H() *world.EntityHandle { return r.h }

// Entity opens the entity in tx.
func (r *Ref) Entity(tx *world.Tx) (world.Entity, bool) {
	if r.h == nil {
		return nil, false
	}
	return r.h.Entity(tx)
}

// Mob opens the mob in tx; ok is false when it is gone or not a mob.
func (r *Ref) Mob(tx *world.Tx) (*mob.Mob, bool) {
	if r.h == nil {
		return nil, false
	}
	return mob.OfHandle(tx, r.h)
}

// Alive reports whether the entity is still in the world (and, for living
// entities, not dead).
func (r *Ref) Alive() bool {
	alive := false
	r.w.Do(func(tx *world.Tx) {
		e, ok := r.Entity(tx)
		if !ok {
			return
		}
		if l, ok := e.(interface{ Dead() bool }); ok && l.Dead() {
			return
		}
		alive = true
	})
	return alive
}

// Gone reports whether the entity left the world (removed, exploded, ...).
func (r *Ref) Gone() bool {
	gone := true
	r.w.Do(func(tx *world.Tx) { _, ok := r.Entity(tx); gone = !ok })
	return gone
}

// Pos returns the entity's position (zero when gone).
func (r *Ref) Pos() mgl64.Vec3 {
	var p mgl64.Vec3
	r.w.Do(func(tx *world.Tx) { p = r.PosTx(tx) })
	return p
}

// PosTx is Pos inside a transaction.
func (r *Ref) PosTx(tx *world.Tx) mgl64.Vec3 {
	if e, ok := r.Entity(tx); ok {
		return e.Position()
	}
	return mgl64.Vec3{}
}

// With runs f with the mob in a transaction; it fails the test when the
// entity is gone or not a mob.
func (r *Ref) With(f func(m *mob.Mob)) {
	r.w.T.Helper()
	ok := false
	r.w.Do(func(tx *world.Tx) {
		var m *mob.Mob
		if m, ok = r.Mob(tx); ok {
			f(m)
		}
	})
	if !ok {
		r.w.T.Fatalf("mobtest: mob %v is gone", r.id)
	}
}

// Located is anything with a position in a transaction: *Ref and *Player.
type Located interface{ PosTx(tx *world.Tx) mgl64.Vec3 }

// DistTx is the distance between two entities' positions, from a
// transaction.
func DistTx(tx *world.Tx, a, b Located) float64 { return a.PosTx(tx).Sub(b.PosTx(tx)).Len() }

// HDistTx is the horizontal (x/z) distance.
func HDistTx(tx *world.Tx, a, b Located) float64 {
	d := a.PosTx(tx).Sub(b.PosTx(tx))
	d[1] = 0
	return d.Len()
}

// Dist is DistTx in its own transaction.
func (w *World) Dist(a, b Located) float64 {
	var d float64
	w.Do(func(tx *world.Tx) { d = DistTx(tx, a, b) })
	return d
}

// Player is a session-less survival player standing in for a real one: a
// target for hostile mobs, a holder of tempting items. It ticks like any
// other entity (gravity, hunger), so put it on solid ground.
type Player struct {
	*Ref
	name     string
	pos      mgl64.Vec3
	main     item.Stack
	off      item.Stack
	gameMode world.GameMode
}

// AddPlayer adds a survival player named name at pos.
func (w *World) AddPlayer(name string, pos mgl64.Vec3) *Player {
	w.T.Helper()
	p := &Player{name: name, pos: pos, gameMode: world.GameModeSurvival}
	p.Ref = &Ref{w: w, id: uuid.New()}
	w.players = append(w.players, p)
	p.attach(w)
	w.grow(boxAround(pos, 1))
	return p
}

func (p *Player) attach(w *World) {
	w.Do(func(tx *world.Tx) {
		h := world.EntitySpawnOpts{Position: p.pos, ID: p.id}.New(player.Type, player.Config{
			Name: p.name, UUID: p.id, GameMode: p.gameMode, Position: p.pos,
		})
		tx.AddEntity(h)
		p.h = h
		if pl, ok := p.playerTx(tx); ok && (!p.main.Empty() || !p.off.Empty()) {
			pl.SetHeldItems(p.main, p.off)
		}
	})
}

func (p *Player) detach(w *World) {
	w.Do(func(tx *world.Tx) {
		pl, ok := p.playerTx(tx)
		if !ok {
			return
		}
		p.pos = pl.Position()
		p.main, p.off = pl.HeldItems()
		h := tx.RemoveEntity(pl)
		if h != nil {
			_ = h.Close()
		}
	})
	p.h = nil
}

func (p *Player) playerTx(tx *world.Tx) (*player.Player, bool) {
	e, ok := p.Entity(tx)
	if !ok {
		return nil, false
	}
	pl, ok := e.(*player.Player)
	return pl, ok
}

// With runs f with the player in a transaction.
func (p *Player) With(f func(pl *player.Player)) {
	p.w.T.Helper()
	ok := false
	p.w.Do(func(tx *world.Tx) {
		var pl *player.Player
		if pl, ok = p.playerTx(tx); ok {
			f(pl)
		}
	})
	if !ok {
		p.w.T.Fatalf("mobtest: player %v is gone", p.name)
	}
}

// Hold puts s in the player's main hand.
func (p *Player) Hold(s item.Stack) {
	p.With(func(pl *player.Player) {
		_, off := pl.HeldItems()
		pl.SetHeldItems(s, off)
	})
}

// Health returns the player's health (0 when it is gone).
func (p *Player) Health() float64 {
	var h float64
	p.w.Do(func(tx *world.Tx) { h = p.HealthTx(tx) })
	return h
}

// HealthTx is Health in a transaction (0 when gone).
func (p *Player) HealthTx(tx *world.Tx) float64 {
	if pl, ok := p.playerTx(tx); ok {
		return pl.Health()
	}
	return 0
}

// SetMaxHealth sets the player's max health and heals it to full, so a
// long fight does not kill the stand-in.
func (p *Player) SetMaxHealth(v float64) {
	p.With(func(pl *player.Player) {
		pl.SetMaxHealth(v)
		pl.Heal(v, entity.FoodHealingSource{})
	})
}

// Teleport moves the player.
func (p *Player) Teleport(pos mgl64.Vec3) {
	p.With(func(pl *player.Player) { pl.Teleport(pos) })
	p.w.grow(boxAround(pos, 1))
}

// SetGameMode changes the player's game mode (creative players are not
// targeted).
func (p *Player) SetGameMode(g world.GameMode) {
	p.gameMode = g
	p.With(func(pl *player.Player) { pl.SetGameMode(g) })
}

// Interact makes the player use held on the mob behind r (the player's
// held item if held is empty), as a right click does. It returns whether a
// component handled it and what is left of the stack, which becomes the
// player's held item.
func (p *Player) Interact(r *Ref, held item.Stack) (item.Stack, bool) {
	p.w.T.Helper()
	var (
		left item.Stack
		done bool
	)
	p.w.Do(func(tx *world.Tx) {
		pl, ok := p.playerTx(tx)
		if !ok {
			p.w.T.Fatalf("mobtest: player %v is gone", p.name)
		}
		m, ok := r.Mob(tx)
		if !ok {
			p.w.T.Fatalf("mobtest: mob %v is gone", r.id)
		}
		if held.Empty() {
			held, _ = pl.HeldItems()
		} else {
			// The player holds what it uses: filters such as has_equipment
			// (subject other, domain hand) read the player's hand.
			_, off := pl.HeldItems()
			pl.SetHeldItems(held, off)
		}
		left, done = m.Interact(pl, held, m.Position())
		if done {
			_, off := pl.HeldItems()
			pl.SetHeldItems(left, off)
		}
	})
	return left, done
}
