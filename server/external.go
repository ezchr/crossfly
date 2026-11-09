package server

import (
	"context"
	"errors"
	"time"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// ExternalSession connects a player that did not come through a Listener, such
// as a Java Edition client. It does its own login, then joins through
// LoadPlayer and AddPlayer, and from then on is the player's Session.
type ExternalSession interface {
	player.Session
	// Spawn is called in the player's world transaction right after the
	// player entity is added. The session starts sending the world and
	// reading the client's input from here.
	Spawn(c session.Controllable, tx *world.Tx)
	// SetCloseHandler is called by AddPlayer before the player is added. The
	// session must call f exactly once when the player leaves (from its
	// Close method, in the player's world transaction, or with a nil tx if
	// the world is already closed): f saves the player's data and removes
	// them from the server.
	SetCloseHandler(f func(tx *world.Tx, c session.Controllable))
}

// incomingSession is what Accept needs from a joining player's session.
type incomingSession interface {
	Spawn(c session.Controllable, tx *world.Tx)
	Disconnect(message string)
	CloseConnection()
}

// LoadPlayer returns the saved data of the player with this UUID and the
// world they should spawn in. A player without saved data gets the default
// world's spawn and game mode. External sessions call it before sending their
// own login packets (which need the position, dimension and game mode).
func (srv *Server) LoadPlayer(id uuid.UUID) (player.Config, *world.World) {
	d, w, err := srv.conf.PlayerProvider.Load(id, srv.dimension)
	if err != nil {
		w = srv.world
		d.Position = w.Spawn().Vec3Centre()
		d.GameMode = w.DefaultGameMode()
	}
	return d, w
}

// ErrAlreadyOnline is returned by AddPlayer and TakeOver when a player with
// the same UUID is already online.
var ErrAlreadyOnline = errors.New("already logged in")

// DuplicateLoginMessage is what a player's old session is disconnected with
// when they log in again (TakeOver), as in vanilla.
const DuplicateLoginMessage = "You logged in from another location."

// takeOverTimeout is how long TakeOver waits for the old session to close.
const takeOverTimeout = 5 * time.Second

// TakeOver makes room for a new login of the player with this UUID, as vanilla
// does: a player still online under it is disconnected with
// DuplicateLoginMessage, and TakeOver returns once their data is saved and the
// UUID is free, or ErrAlreadyOnline if that took longer than takeOverTimeout.
//
// The old session is usually one whose connection died without the server
// noticing yet: a RakNet client that vanishes (a relay or proxy dropping its
// connection to the server, a crashed game) is only timed out after up to half
// a minute, and refusing the new login meanwhile locked the player out.
//
// Call it before loading the player's data (LoadPlayer), so the new login gets
// what the old session saved. Listener logins do this themselves.
func (srv *Server) TakeOver(id uuid.UUID) error {
	deadline := time.Now().Add(takeOverTimeout)
	kicked := false
	for {
		h, ok := srv.Player(id)
		if !ok {
			return nil
		}
		if time.Now().After(deadline) {
			return ErrAlreadyOnline
		}
		if !kicked {
			ctx, cancel := context.WithDeadline(context.Background(), deadline)
			_, err := player.Call(ctx, h, func(_ *world.Tx, p *player.Player) (struct{}, error) {
				p.Disconnect(DuplicateLoginMessage)
				return struct{}{}, nil
			})
			cancel()
			// Not in a world (yet): the join is still on its way in. Try again.
			if kicked = err == nil; kicked {
				srv.conf.Log.Info("Player logged in again: disconnected the old session.", "uuid", id)
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// AddPlayer adds a player controlled by an external session. conf comes from
// LoadPlayer with Name, XUID, UUID, Locale and Skin filled in. The player is
// then handed out by Accept like players from Listeners, and Spawn is called
// on the session.
func (srv *Server) AddPlayer(s ExternalSession, conf player.Config, w *world.World) (err error) {
	conf.Session = s
	handle := world.EntitySpawnOpts{Position: conf.Position, ID: conf.UUID}.New(player.Type, conf)
	op := &onlinePlayer{name: conf.Name, xuid: conf.XUID, handle: handle}

	// Reserve the UUID in the same critical section as the check, so two logins of one player at
	// the same time cannot both get in (and duplicate the player and its items).
	srv.pmu.Lock()
	if _, ok := srv.p[conf.UUID]; ok {
		srv.pmu.Unlock()
		_ = handle.Close()
		return ErrAlreadyOnline
	}
	srv.p[conf.UUID] = op
	srv.pmu.Unlock()
	srv.pwg.Add(1)

	defer func() {
		// srv.incoming is closed once the server is shutting down.
		if recover() != nil {
			srv.pmu.Lock()
			delete(srv.p, conf.UUID)
			srv.pmu.Unlock()
			srv.pwg.Done()
			_ = handle.Close()
			err = ErrServerClosed
		}
	}()
	s.SetCloseHandler(srv.handleSessionClose)
	// Like the Bedrock join: the session gets the handle before the player is in a world, so it
	// can be listed (tab list) before anyone is shown the player entity.
	s.SetHandle(handle, conf.Skin)
	srv.incoming <- incoming{s: s, w: w, conf: conf, p: op}
	return nil
}

// ErrServerClosed is returned by AddPlayer while the server is shutting down.
var ErrServerClosed = errors.New("server closed")
