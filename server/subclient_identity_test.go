package server

import (
	"strconv"
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
)

// The derivation matches Beacon Proxy's for the same host and sub-client.
func TestSubClientIdentityStable(t *testing.T) {
	host := login.IdentityData{XUID: "2535400000000000", DisplayName: "Some Host!"}
	a, b := subClientIdentity(host, 1, login.ClientData{}), subClientIdentity(host, 1, login.ClientData{})
	if a != b || a.DisplayName != "SomeHost_1" || len(a.XUID) != 19 || a.XUID[0] != '9' {
		t.Fatalf("identity %+v", a)
	}
	for _, x := range []string{"", "1", "2535400000000000", "2535499999999999"} {
		for sub := byte(1); sub <= 3; sub++ {
			id := subClientIdentity(login.IdentityData{XUID: x}, sub, login.ClientData{}).XUID
			if v, err := strconv.ParseInt(id, 10, 64); err != nil || v < 9_000_000_000_000_000_000 {
				t.Fatalf("XUID %s does not fit an int64 from 9e18 up: %v", id, err)
			}
		}
	}
	if c := subClientIdentity(host, 2, login.ClientData{}); c.XUID == a.XUID || c.Identity == a.Identity {
		t.Fatal("sub-clients share an identity")
	}
}
