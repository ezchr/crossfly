package javasession

import (
	"os"
	"strings"
	"sync"

	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
	"github.com/go-gl/mathgl/mgl64"
)

// soundLog (CF_JAVA_SOUND_LOG=1) logs every sound packet and damage_event sent to a Java
// player, and the attack and punch packets it sent: what a client heard, to debug sound
// mappings without a packet capture.
var soundLog = os.Getenv("CF_JAVA_SOUND_LOG") == "1"

var (
	soundNamesOnce sync.Once
	soundNamesByID map[int32]string
)

// soundEventName is the name of a 26.3 sound event registry id.
func soundEventName(id int32) string {
	soundNamesOnce.Do(func() {
		soundNamesByID = map[int32]string{}
		for n, i := range v777.Builtin["minecraft:sound_event"] {
			soundNamesByID[i] = strings.TrimPrefix(n, "minecraft:")
		}
	})
	return soundNamesByID[id]
}

// logSound logs a sound packet of a registry id (CF_JAVA_SOUND_LOG).
func (s *Session) logSound(id int32, pos mgl64.Vec3) {
	s.log.Info("java sound", "sound", soundEventName(id), "pos", pos)
}
