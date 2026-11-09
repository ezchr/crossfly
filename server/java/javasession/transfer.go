package javasession

import (
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/google/uuid"
)

// Moving Java players to other servers: by the client itself (Transfer, with cookies it takes along), or by the
// proxy in front of this server (ProxyConnect).

// StoreCookie stores a cookie on the Java client of the player with this UUID (store_cookie). A vanilla client
// keeps cookies only for the server it is transferred to next, which asks for them with cookie_request. value is
// at most 5 KiB. It reports false when id is not a Java player here or value is too long.
func StoreCookie(id uuid.UUID, key string, value []byte) bool {
	v, ok := javaSessions.Load(id)
	if !ok || len(value) > 5120 {
		return false
	}
	s := v.(*Session)
	w := s.packet()
	w.String(key)
	w.VarInt(int32(len(value)))
	w.Raw(value)
	s.queue(v777.ClientboundPlayStoreCookie, w)
	return true
}

// Transfer sends the Java client of the player with this UUID to another server (transfer): the client
// disconnects and connects to host:port itself, as it resolves that address. Behind Velocity the proxy passes it
// on to the client. It reports false when id is not a Java player here.
func Transfer(id uuid.UUID, host string, port int) bool {
	v, ok := javaSessions.Load(id)
	if !ok {
		return false
	}
	s := v.(*Session)
	w := s.packet()
	w.String(host)
	w.VarInt(int32(port))
	s.queue(v777.ClientboundPlayTransfer, w)
	return true
}

// ProxyConnect asks the proxy in front of this server (Velocity, BungeeCord) to move the Java player id to the
// proxy server named server, with the Connect message of the BungeeCord plugin channel. It reports false when id
// is not a Java player here. Velocity needs bungee-plugin-message-channel = true (its default).
func ProxyConnect(id uuid.UUID, server string) bool {
	v, ok := javaSessions.Load(id)
	if !ok {
		return false
	}
	s := v.(*Session)
	w := s.packet()
	w.String("bungeecord:main")
	w.Raw(javaUTF("Connect"))
	w.Raw(javaUTF(server))
	s.queue(v777.ClientboundPlayCustomPayload, w)
	return true
}

// javaUTF is s as Java DataOutput.writeUTF writes it: a big-endian uint16 length, then the bytes (the same as
// modified UTF-8 for text without NUL or characters outside the BMP).
func javaUTF(s string) []byte {
	b := make([]byte, 0, 2+len(s))
	b = append(b, byte(len(s)>>8), byte(len(s)))
	return append(b, s...)
}
