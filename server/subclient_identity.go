package server

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
)

// subClientIdentity is the identity of a split-screen guest without an Xbox account of its own. It is
// the one Beacon Proxy gives such a guest, so the guest keeps its player data whichever of the two
// logs it in: a 19-digit XUID from 9000000000000000000 up that fits an int64 (never a real one)
// and a UUID, both derived from the host's XUID and the guest's sub-client ID, and the host's name followed by the ID.
func subClientIdentity(host login.IdentityData, sub byte, _ login.ClientData) login.IdentityData {
	key := host.XUID + ":" + strconv.Itoa(int(sub))
	h := fnv.New64a()
	_, _ = h.Write([]byte("beacon-guest:" + key))
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			return r
		}
		return -1
	}, host.DisplayName)
	if len(name) > 13 {
		name = name[:13]
	}
	return login.IdentityData{
		XUID:        strconv.FormatUint(9_000_000_000_000_000_000+h.Sum64()%223_372_036_854_775_807, 10),
		Identity:    uuid.NewSHA1(uuid.NameSpaceOID, []byte("beacon-guest:"+key)).String(),
		DisplayName: fmt.Sprintf("%s_%d", name, sub),
		TitleID:     host.TitleID,
	}
}
