package javasession

import (
	"github.com/df-mc/dragonfly/server/item"
	jitem "github.com/df-mc/dragonfly/server/java/protocol/item"
	"github.com/df-mc/dragonfly/server/java/protocol/wire"
	"github.com/google/uuid"
)

// HeadSkinKey and HeadSignatureKey are the stack values (item.Stack.WithValue) that give a player
// head a skin for Java players: a "textures" property's value and signature, as a Mojang profile
// carries them. The head is then sent with a minecraft:profile component holding that property.
const (
	HeadSkinKey      = "zid_skin"
	HeadSignatureKey = "zid_skin_sig"
)

// headProfile adds the minecraft:profile component of a player head with a skin (HeadSkinKey).
// The component is a ResolvableProfile: a full game profile (either: true, UUID, name, properties)
// and an empty skin patch (no body, cape, elytra or model override).
func headProfile(ds item.Stack, js *jitem.Stack) {
	v, ok := ds.Value(HeadSkinKey)
	textures, _ := v.(string)
	if !ok || textures == "" {
		return
	}
	var sig string
	if v, ok := ds.Value(HeadSignatureKey); ok {
		sig, _ = v.(string)
	}
	var w wire.Writer
	w.Bool(true)
	w.UUID(uuid.NewSHA1(uuid.NameSpaceOID, []byte(textures))) // one id per skin
	w.String("Head")
	w.VarInt(1)
	w.String("textures")
	w.String(textures)
	w.Bool(sig != "")
	if sig != "" {
		w.String(sig)
	}
	for range 4 {
		w.Bool(false)
	}
	js.Raw = append(js.Raw, jitem.RawComponent{Type: jitem.CompProfile, Data: append([]byte(nil), w.B...)})
}

// withHeadSkin gives ds the skin of the minecraft:profile js carries (HeadSkinKey), so a head a
// creative client sends back keeps it.
func withHeadSkin(ds item.Stack, js *jitem.Stack) item.Stack {
	for _, c := range js.Raw {
		if c.Type != jitem.CompProfile {
			continue
		}
		r := wire.Reader{B: c.Data}
		if r.Bool() {
			r.UUID()
			r.String(16)
		} else {
			if r.Bool() {
				r.String(16)
			}
			if r.Bool() {
				r.UUID()
			}
		}
		n := r.VarInt()
		for i := int32(0); i < n && i < 16 && r.Err == nil; i++ {
			name, value := r.String(64), r.String(32767)
			var sig string
			if r.Bool() {
				sig = r.String(1024)
			}
			if r.Err == nil && name == "textures" && value != "" {
				ds = ds.WithValue(HeadSkinKey, value)
				if sig != "" {
					ds = ds.WithValue(HeadSignatureKey, sig)
				}
				return ds
			}
		}
	}
	return ds
}
