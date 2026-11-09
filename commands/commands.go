// Package commands holds the commands of the Crossfly server: the basic vanilla commands (/gamemode, /tp, /give,
// /time, /weather, /kill, /effect, /clear, /say, /list, /kick, /op...) in every build, and the extra commands of
// Crossfly Full. Operators may run every command; other players only /list, /tell and /me. The server console
// (standard input) runs commands as an operator.
package commands

import (
	"bufio"
	"io"
	"log/slog"
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// srv and ops are set by Register.
var (
	srv *server.Server
	ops *Operators
)

// Register registers the basic vanilla commands for srv, with ops as the operators.
func Register(s *server.Server, o *Operators) {
	srv, ops = s, o
	for _, c := range coreCommands() {
		cmd.Register(c)
	}
}

// RunConsole runs the commands typed on r (the server's standard input) as the console, until r ends. A line may
// start with a slash or not.
func RunConsole(r io.Reader, log *slog.Logger) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "/")
		name, args, _ := strings.Cut(line, " ")
		c, ok := cmd.ByAlias(name)
		if !ok {
			log.Info("Unknown command: " + name + ". Commands: " + commandNames())
			continue
		}
		src := Console{log: log}
		<-srv.World().Do(func(tx *world.Tx) { c.Execute(args, src, tx) }).Done()
	}
}

// commandNames lists the registered commands.
func commandNames() string {
	var names []string
	for alias, c := range cmd.Commands() {
		if alias == c.Name() {
			names = append(names, alias)
		}
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

// Console is the command source of the server console. It runs every command.
type Console struct {
	log *slog.Logger
}

// Name ...
func (Console) Name() string { return "Server" }

// Position is the world spawn.
func (Console) Position() mgl64.Vec3 { return srv.World().Spawn().Vec3Centre() }

// SendCommandOutput logs the output of a command.
func (c Console) SendCommandOutput(o *cmd.Output) {
	for _, m := range o.Messages() {
		c.log.Info(m.String())
	}
	for _, e := range o.Errors() {
		c.log.Error(e.Error())
	}
}

// op is embedded in commands only operators (and the console) may run.
type op struct{}

// Allow ...
func (op) Allow(src cmd.Source) bool { return isOp(src) }

// isOp reports whether src may run operator commands.
func isOp(src cmd.Source) bool {
	switch s := src.(type) {
	case Console:
		return true
	case *player.Player:
		return ops.Is(s)
	}
	return false
}

// playersOf returns the players among targets.
func playersOf(targets []cmd.Target) []*player.Player {
	var out []*player.Player
	for _, t := range targets {
		if p, ok := t.(*player.Player); ok {
			out = append(out, p)
		}
	}
	return out
}

// targetsOrSelf returns the players among t, or the source if t is not set and it is a player.
func targetsOrSelf(src cmd.Source, t cmd.Optional[[]cmd.Target], o *cmd.Output) ([]*player.Player, bool) {
	if targets, ok := t.Load(); ok {
		players := playersOf(targets)
		if len(players) == 0 {
			o.Error("No player was found")
			return nil, false
		}
		return players, true
	}
	p, ok := src.(*player.Player)
	if !ok {
		o.Error("A player target is needed when run from the console")
		return nil, false
	}
	return []*player.Player{p}, true
}

// names joins the names of players.
func names(players []*player.Player) string {
	n := make([]string, len(players))
	for i, p := range players {
		n[i] = p.Name()
	}
	return strings.Join(n, ", ")
}
