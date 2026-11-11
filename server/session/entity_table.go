package session

import (
	"reflect"
	"sync"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft"
)

// entityTable holds the entity runtime IDs shown to a client. Normally every session has its own.
// Split-screen players on one console share one entity ID space, so a guest session uses the table of
// the session of the player whose connection it shares (shared is then true): one entity has one ID
// for all of them and each player has its own self ID. Every player is still sent the entities of its
// own view, as the console keeps a view per player (one may be in another dimension). An ID is given
// up when no player on the console views its entity any longer (refs).
type entityTable struct {
	mu       sync.RWMutex
	current  uint64
	ids      map[*world.EntityHandle]uint64
	entities map[uint64]*world.EntityHandle

	shared  bool
	refs    map[*world.EntityHandle]int
	members []*Session
}

func newEntityTable() *entityTable {
	return &entityTable{
		current:  selfEntityRuntimeID,
		ids:      map[*world.EntityHandle]uint64{},
		entities: map[uint64]*world.EntityHandle{},
		refs:     map[*world.EntityHandle]int{},
	}
}

// subClientHosts holds the session of each main connection that may get split-screen guests, and
// pendingSelf the self IDs reserved for guests before their session is made.
var (
	subClientMu    sync.Mutex
	subClientHosts = map[*minecraft.Conn]*Session{}
	pendingSelf    = map[*minecraft.Conn]uint64{}
)

// minecraftConn returns the *minecraft.Conn behind c, looking through wrappers that hold it in a
// field named Conn.
func minecraftConn(c any) *minecraft.Conn {
	for range 8 {
		if mc, ok := c.(*minecraft.Conn); ok {
			return mc
		}
		v := reflect.ValueOf(c)
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return nil
			}
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct {
			return nil
		}
		f := v.FieldByName("Conn")
		if !f.IsValid() || !f.CanInterface() {
			return nil
		}
		c = f.Interface()
	}
	return nil
}

// SelfRuntimeID returns the entity runtime ID the player of conn has for itself, to be sent in its
// StartGame. It is 1, except for a split-screen guest, which gets a free ID in the entity table of the
// session of its main player, so that it differs from every entity on the console.
func SelfRuntimeID(conn Conn) uint64 {
	mc := minecraftConn(conn)
	if mc == nil || mc.Parent() == nil {
		return selfEntityRuntimeID
	}
	subClientMu.Lock()
	defer subClientMu.Unlock()
	host, ok := subClientHosts[mc.Parent()]
	if !ok {
		return selfEntityRuntimeID
	}
	host.et.mu.Lock()
	host.et.current++
	id := host.et.current
	host.et.mu.Unlock()
	pendingSelf[mc] = id
	return id
}

// attachEntityTable sets the entity table of a new session: the one of its main player's session for
// a split-screen guest, its own otherwise.
func (s *Session) attachEntityTable() {
	s.et, s.selfRID = newEntityTable(), selfEntityRuntimeID
	s.et.members = []*Session{s}
	mc := minecraftConn(s.conn)
	if mc == nil {
		return
	}
	subClientMu.Lock()
	defer subClientMu.Unlock()
	if mc.Parent() == nil {
		subClientHosts[mc] = s
		return
	}
	host, ok := subClientHosts[mc.Parent()]
	id, reserved := pendingSelf[mc]
	delete(pendingSelf, mc)
	if !ok || !reserved {
		return
	}
	s.et, s.selfRID, s.subClient = host.et, id, true
	s.et.mu.Lock()
	s.et.shared = true
	s.et.members = append(s.et.members, s)
	s.et.mu.Unlock()
}

// detachEntityTable lets go of the entity table as the session closes. A guest gives up the IDs of
// the entities only it viewed.
func (s *Session) detachEntityTable() {
	if mc := minecraftConn(s.conn); mc != nil {
		subClientMu.Lock()
		if subClientHosts[mc] == s {
			delete(subClientHosts, mc)
		}
		delete(pendingSelf, mc)
		subClientMu.Unlock()
	}
	et := s.et
	et.mu.Lock()
	var others []*Session
	for _, m := range et.members {
		if m != s {
			others = append(others, m)
		}
	}
	et.members = others
	if len(others) == 0 {
		clear(et.ids)
		clear(et.entities)
		clear(et.refs)
		et.mu.Unlock()
		return
	}
	for h := range s.viewed {
		et.refs[h]--
		if et.refs[h] > 0 {
			continue
		}
		delete(et.refs, h)
		id, ok := et.ids[h]
		if !ok || isPlayer(h) {
			continue // players keep their IDs while they are listed
		}
		delete(et.ids, h)
		delete(et.entities, id)
	}
	clear(s.viewed)
	if s.ent != nil {
		delete(et.refs, s.ent)
	}
	et.mu.Unlock()
}

// isPlayer reports whether h is the handle of a player.
func isPlayer(h *world.EntityHandle) bool {
	return h.Type() != nil && h.Type().EncodeEntity() == "minecraft:player"
}
