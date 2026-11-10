// Package commands holds the commands of Crossfly Full: the basic vanilla commands (/gamemode, /tp, /give, /time,
// /weather, /kill, /effect, /clear, /say, /list, /kick, /op...), the operators of ops.json, the server console and
// the extra commands ([ExtraCommands]: /setblock, /fill, /summon, /mobdebug). Operators may run every command;
// other players only /list, /tell and /me. The server console (standard input) runs commands as an operator.
//
// Every file but this one is built only with the crossfly_full tag: Crossfly Core has no commands, and links none
// of this package.
package commands
