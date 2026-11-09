package javasession

import (
	v777 "github.com/df-mc/dragonfly/server/java/protocol/v777"
)

// ViewTime sets the time of day. 26.3 keeps time in world clocks: set_time carries the game time
// and a map from minecraft:world_clock entries to (total ticks, partial tick, rate).
func (s *Session) ViewTime(t int) {
	s.timeMu.Lock()
	s.time = t
	cycle := !s.timeStopped
	s.timeMu.Unlock()
	rate := float32(1)
	if !cycle {
		rate = 0
	}
	w := s.packet()
	w.Int64(int64(t)) // game time
	w.VarInt(1)
	w.VarInt(s.ver.RegistryID("minecraft:world_clock", "minecraft:overworld"))
	w.VarLong(int64(t))
	w.Float32(0)
	w.Float32(rate)
	s.queue(v777.ClientboundPlaySetTime, w)
}

// ViewTimeCycle turns the daylight cycle on or off.
func (s *Session) ViewTimeCycle(doCycle bool) {
	s.timeMu.Lock()
	s.timeStopped = !doCycle
	t := s.time
	s.timeMu.Unlock()
	s.ViewTime(t)
}

// resendLevelInfo sends the time and weather again, for a client that made a new level. Like
// vanilla's sendLevelInfo, the weather is sent as it is now, without fading in.
func (s *Session) resendLevelInfo() {
	s.borderSent.Store(false) // a new level starts without a border
	s.timeMu.Lock()
	t := s.time
	s.rainLevel, s.thunderLevel = levelOf(s.raining), levelOf(s.thunder)
	if !s.raining {
		s.thunderLevel = 0
	}
	rain, thunder := s.rainLevel, s.thunderLevel
	s.weatherSent = true
	s.timeMu.Unlock()
	s.ViewTime(t)
	if rain > rainingLevel {
		s.gameEvent(1, 0) // start raining
	} else {
		s.gameEvent(2, 0) // stop raining
	}
	s.gameEvent(7, rain)
	s.gameEvent(8, thunder)
}

// rainingLevel is the rain level above which a vanilla level counts as raining (Level.isRaining).
const rainingLevel = 0.2

// weatherStep is how much the rain and thunder levels change a tick, as in vanilla's
// ServerLevel.advanceWeatherCycle: rain and thunder fade in and out over five seconds.
const weatherStep = 0.01

func levelOf(on bool) float32 {
	if on {
		return 1
	}
	return 0
}

// gameEvent sends a game event with a value.
func (s *Session) gameEvent(id byte, v float32) {
	w := s.packet()
	w.Byte(id)
	w.Float32(v)
	s.queue(v777.ClientboundPlayGameEvent, w)
}

// ViewWeather shows rain and thunder. The client sees them fade in and out (tickWeather), as on a
// vanilla server.
func (s *Session) ViewWeather(raining, thunder bool) {
	s.timeMu.Lock()
	s.raining, s.thunder = raining, thunder
	s.timeMu.Unlock()
	// Before the client has its level info, the weather only waits for resendLevelInfo.
}

// tickWeather moves the rain and thunder levels the client sees one step towards the weather,
// sending each change, and starts or stops the rain when its level crosses rainingLevel, as
// vanilla's ServerLevel.advanceWeatherCycle does. Thunder needs rain: it is only shown while it
// rains.
func (s *Session) tickWeather() {
	s.timeMu.Lock()
	if !s.weatherSent {
		s.timeMu.Unlock()
		return
	}
	rainTarget, thunderTarget := levelOf(s.raining), levelOf(s.thunder && s.raining)
	oldRain, oldThunder := s.rainLevel, s.thunderLevel
	s.rainLevel = stepTowards(oldRain, rainTarget)
	s.thunderLevel = stepTowards(oldThunder, thunderTarget)
	rain, thunder := s.rainLevel, s.thunderLevel
	s.timeMu.Unlock()

	if was, is := oldRain > rainingLevel, rain > rainingLevel; was != is {
		if is {
			s.gameEvent(1, 0) // start raining
		} else {
			s.gameEvent(2, 0) // stop raining
		}
	}
	if rain != oldRain {
		s.gameEvent(7, rain)
	}
	if thunder != oldThunder {
		s.gameEvent(8, thunder)
	}
}

func stepTowards(v, target float32) float32 {
	switch {
	case v < target:
		return min(v+weatherStep, target)
	case v > target:
		return max(v-weatherStep, target)
	}
	return v
}
