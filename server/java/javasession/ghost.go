package javasession

import (
	"sync"

	"github.com/df-mc/dragonfly/server/java/protocol/text"
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// Ghosts: a player entity (an NPC) that Java players see as a see-through copy of another player,
// wearing that player's skin. A server cannot tint or fade a skin for Java (skins must be signed by
// Mojang), but a Java client draws an invisible player half transparent when it is on the client's
// own team with "see friendly invisibles" set. So a ghost is sent invisible and put in a team with
// the client's own player, made the first time a ghost is shown.

// ghostTeam is the team of the client's own player and the ghosts it sees.
const ghostTeam = "df_ghosts"

// ghostSource is the player whose skin a ghost wears.
type ghostSource struct {
	id   uuid.UUID
	xuid string // a Bedrock player's XUID, whose skin comes from GeyserMC's database
}

// ghosts maps a ghost's UUID to its ghostSource.
var ghosts sync.Map

// ghostTeams holds the sessions whose client has the ghost team.
var ghostTeams sync.Map // *Session -> struct{}

// ShowAsGhost makes Java players see the player entity with UUID ghost as a see-through copy of
// source. Call it before the ghost is added to a world, and ForgetGhost once it is gone.
func ShowAsGhost(ghost uuid.UUID, source *player.Player) {
	g := ghostSource{id: source.UUID()}
	if !isJavaPlayer(source.UUID()) {
		g.xuid = source.XUID()
	}
	ghosts.Store(ghost, g)
}

// ForgetGhost undoes ShowAsGhost.
func ForgetGhost(ghost uuid.UUID) { ghosts.Delete(ghost) }

// IsGhost reports whether the player entity with UUID id is shown as a ghost (ShowAsGhost).
func IsGhost(id uuid.UUID) bool {
	_, ok := ghostOf(id)
	return ok
}

func ghostOf(id uuid.UUID) (ghostSource, bool) {
	g, ok := ghosts.Load(id)
	if !ok {
		return ghostSource{}, false
	}
	return g.(ghostSource), true
}

// isGhost reports whether e is shown as a ghost.
func isGhost(e world.Entity) bool {
	p, ok := e.(*player.Player)
	if !ok {
		return false
	}
	_, ok = ghostOf(p.UUID())
	return ok
}

// viewGhost puts a ghost that came into view in the ghost team, making the team first.
//
// Hook: entity.go, ViewEntity, after s.viewPlayerNameTag(p), which gives every player its own team.
func (s *Session) viewGhost(p *player.Player) {
	if _, ok := ghostOf(p.UUID()); !ok {
		return
	}
	name := tabName(p.Name())
	w := s.packet()
	w.String(ghostTeam)
	if _, made := ghostTeams.LoadOrStore(s, struct{}{}); made {
		w.Byte(3) // add entities
	} else {
		w.Byte(0) // create
		text.WriteString(w, "")
		text.WriteString(w, "") // prefix
		text.WriteString(w, "") // suffix
		w.VarInt(1)             // name tag: never
		w.VarInt(2)             // collision: push other teams only, so the ghost pushes nobody
		w.Bool(false)           // no colour
		w.Byte(0x02)            // see friendly invisibles: drawn see-through
		w.VarInt(2)
		w.String(s.jp.Profile.Name)
		w.String(name)
		s.queue(v777.ClientboundPlaySetPlayerTeam, w)
		return
	}
	w.VarInt(1)
	w.String(name)
	s.queue(v777.ClientboundPlaySetPlayerTeam, w)
}

// hideGhost takes a ghost that left view out of the ghost team.
//
// Hook: entity.go, HideEntity, before s.forgetEntityText(e).
func (s *Session) hideGhost(e world.Entity) {
	p, ok := e.(*player.Player)
	if !ok || !isGhost(p) {
		return
	}
	if _, made := ghostTeams.Load(s); !made {
		return
	}
	w := s.packet()
	w.String(ghostTeam)
	w.Byte(4) // remove entities
	w.VarInt(1)
	w.String(tabName(p.Name()))
	s.queue(v777.ClientboundPlaySetPlayerTeam, w)
}
