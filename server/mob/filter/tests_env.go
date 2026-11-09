//go:build crossfly_full

package filter

import (
	"fmt"
	"math"
	"strings"
)

// Environment tests: world and position of the subject. BDS reads the
// level/dimension/block source from the subject actor, so they fail without
// a subject; here they also fail without Context.World.

func envDef(name string, k ValueKind, src string, eval func(in *Input, s *Scope, w World) bool) *Def {
	return &Def{Name: name, Kind: k, Source: src, Eval: func(in *Input, s *Scope) bool {
		w := s.World()
		if w == nil {
			return false
		}
		return eval(in, s, w)
	}}
}

var difficulties = map[string]int{"peaceful": 0, "easy": 1, "normal": 2, "hard": 3}

// legacy is_biome types mapped to the biome tags that carry them.
var biomeTypeTags = map[string]string{
	"the_nether": "nether", "plain": "plains", "mushroom_island": "mooshroom_island",
	"stone_beach": "stone", "ice": "frozen", "flat": "flat",
}

var weatherNames = map[string]bool{"clear": true, "precipitation": true, "rain": true, "snow": true, "thunderstorm": true}

func matchWeather(w Weather, name string) bool {
	switch name {
	case "clear":
		return !w.Precipitation && !w.Thunder
	case "precipitation":
		return w.Precipitation
	case "rain":
		return w.Precipitation && !w.Snow
	case "snow":
		return w.Precipitation && w.Snow
	case "thunderstorm":
		return w.Thunder
	}
	return false
}

// celestialAngle is Dimension::getTimeOfDay: 0 noon, 0.25 sunset, 0.5
// midnight, 0.75 sunrise.
func celestialAngle(t int64) float64 {
	f := float64(((t%24000)+24000)%24000)/24000 - 0.25
	if f < 0 {
		f++
	}
	if f > 1 {
		f--
	}
	g := 1 - (math.Cos(f*math.Pi)+1)/2
	return f + (g-f)/3
}

var moonBrightness = [8]float64{1, 0.75, 0.5, 0.25, 0, 0.25, 0.5, 0.75}

func init() {
	Register(envDef("is_daytime", KindBool, "bds 1.21.3 FilterTestDaytime", func(in *Input, s *Scope, w World) bool {
		return in.CmpBool(w.Daytime())
	}))
	Register(envDef("hourly_clock_time", KindInt, "bds 1.21.3 FilterTestHourlyClock (value clamped to 0..24000)", func(in *Input, s *Scope, w World) bool {
		t := int(((w.Time() % 24000) + 24000) % 24000)
		return in.CmpInt(t)
	}))
	Register(envDef("clock_time", KindFloat, "bds 1.21.3 FilterTestClock", func(in *Input, s *Scope, w World) bool {
		return in.CmpFloat(celestialAngle(w.Time()))
	}))
	Register(envDef("moon_phase", KindInt, "bds 1.21.3 FilterTestMoonPhase", func(in *Input, s *Scope, w World) bool {
		return in.CmpInt(int((w.Time() / 24000) % 8))
	}))
	Register(envDef("moon_intensity", KindFloat, "bds 1.21.3 FilterTestMoonIntensity", func(in *Input, s *Scope, w World) bool {
		return in.CmpFloat(moonBrightness[(w.Time()/24000)%8])
	}))
	Register(envDef("is_difficulty", KindInt, "bds 1.21.3 FilterTestDifficulty", func(in *Input, s *Scope, w World) bool {
		return in.CmpInt(w.Difficulty())
	}))
	Register(envDef("is_game_rule", KindBool, "bds 1.21.3 FilterTestGameRule (domain = rule)", func(in *Input, s *Scope, w World) bool {
		return in.CmpBool(w.GameRule(strings.ToLower(in.Domain)))
	}))
	Register(envDef("is_brightness", KindFloat, "bds 1.21.3 FilterTestBrightness", func(in *Input, s *Scope, w World) bool {
		return in.CmpFloat(w.Brightness(s.Pos))
	}))
	Register(envDef("light_level", KindInt, "bds 1.21.3 FilterTestLightLevel", func(in *Input, s *Scope, w World) bool {
		return in.CmpInt(w.LightLevel(s.Pos))
	}))
	Register(envDef("has_biome_tag", KindString, "bds 1.21.3 FilterTestBiomeHasTag", func(in *Input, s *Scope, w World) bool {
		return in.CmpTrue(w.BiomeHasTag(s.Pos, in.Str))
	}))
	Register(envDef("is_biome", KindString, "bds 1.21.3 FilterTestBiome (legacy biome type, mapped to its tag)", func(in *Input, s *Scope, w World) bool {
		tag := in.Str
		if t, ok := biomeTypeTags[tag]; ok {
			tag = t
		}
		return in.CmpTrue(w.BiomeHasTag(s.Pos, tag))
	}))
	Register(envDef("is_snow_covered", KindBool, "bds 1.21.3 FilterTestBiomeSnowCovered", func(in *Input, s *Scope, w World) bool {
		return in.CmpBool(w.SnowCovered(s.Pos))
	}))
	Register(envDef("is_humid", KindBool, "bds 1.21.3 FilterTestBiomeHumid", func(in *Input, s *Scope, w World) bool {
		return in.CmpBool(w.Humid(s.Pos))
	}))
	Register(envDef("is_temperature_value", KindFloat, "bds 1.21.3 FilterTestTemperatureValue", func(in *Input, s *Scope, w World) bool {
		return in.CmpFloat(w.Temperature(s.Pos))
	}))
	Register(envDef("is_temperature_type", KindString, "bds 1.21.3 FilterTestTemperatureType (Biome temperature category)", func(in *Input, s *Scope, w World) bool {
		t := w.Temperature(s.Pos)
		cat := "mild"
		switch {
		case w.BiomeHasTag(s.Pos, "ocean"):
			cat = "ocean"
		case t < 0.2:
			cat = "cold"
		case t >= 1:
			cat = "warm"
		}
		return in.CmpTrue(cat == in.Str)
	}))
	Register(envDef("is_underground", KindBool, "bds 1.21.3 ActorUndergroundTest (cannot see sky)", func(in *Input, s *Scope, w World) bool {
		return in.CmpBool(w.Underground(s.Pos))
	}))
	Register(envDef("is_altitude", KindInt, "bds 1.21.3 FilterTestAltitude", func(in *Input, s *Scope, w World) bool {
		return in.CmpInt(int(math.Floor(s.Pos[1])))
	}))
	weatherSetup := func(in *Input) error {
		in.Str = strings.ToLower(in.Str)
		if !weatherNames[in.Str] {
			return fmt.Errorf("unknown weather %q", in.Str)
		}
		return nil
	}
	weatherAt := envDef("weather_at_position", KindString, "bds 1.21.3 ActorInWeatherTest", func(in *Input, s *Scope, w World) bool {
		return in.CmpTrue(matchWeather(w.WeatherAt(s.Pos), in.Str))
	})
	weatherAt.Setup = weatherSetup
	Register(weatherAt)
	isWeather := *weatherAt
	isWeather.Name = "is_weather" // deprecated alias of weather_at_position
	Register(&isWeather)
	weather := envDef("weather", KindString, "bds 1.21.3 FilterTestDimensionWeather", func(in *Input, s *Scope, w World) bool {
		return in.CmpTrue(matchWeather(w.DimensionWeather(), in.Str))
	})
	weather.Setup = weatherSetup
	Register(weather)
	Register(envDef("in_nether", KindBool, "bds 1.21.3 ActorInNetherTest", func(in *Input, s *Scope, w World) bool {
		return in.CmpBool(w.Dimension() == "nether")
	}))
	Register(envDef("in_overworld", KindBool, "bds 1.21.3 ActorInOverworldTest", func(in *Input, s *Scope, w World) bool {
		return in.CmpBool(w.Dimension() == "overworld")
	}))
	Register(envDef("distance_to_nearest_player", KindFloat, "bds 1.21.3 FilterTestDistanceToNearestPlayer", func(in *Input, s *Scope, w World) bool {
		d, ok := w.NearestPlayerDistance(s.Pos)
		return ok && in.CmpFloat(d)
	}))
	Register(envDef("redstone_strength_at_position", KindInt, "docs", func(in *Input, s *Scope, w World) bool {
		return in.CmpInt(w.RedstonePower(s.Pos))
	}))
	Register(envDef("in_block", KindString, "bds 1.21.3 ActorInBlockTest", func(in *Input, s *Scope, w World) bool {
		name := ns(in.Str)
		if ib, ok := s.Actor.(InBlock); ok {
			return in.CmpTrue(ib.InBlock(name))
		}
		return in.CmpTrue(ns(w.BlockName(s.Pos)) == name)
	}))

	// is_difficulty takes peaceful/easy/normal/hard or the number.
	d := Lookup("is_difficulty")
	d.Setup = func(in *Input) error {
		if str, ok := in.RawValue.(string); ok {
			n, ok := difficulties[strings.ToLower(str)]
			if !ok {
				return fmt.Errorf("unknown difficulty %q", str)
			}
			in.Int = n
		}
		return nil
	}
	h := Lookup("hourly_clock_time")
	h.Setup = func(in *Input) error {
		in.Int = max(0, min(in.Int, 24000))
		return nil
	}

	// Block subject tests.
	Register(&Def{Name: "is_block", Kind: KindString, Source: "bds 1.21.3 (subject block)",
		Eval: func(in *Input, s *Scope) bool {
			return s.Block != nil && in.CmpTrue(ns(s.Block.Name()) == ns(in.Str))
		}})
	Register(&Def{Name: "is_waterlogged", Kind: KindBool, Source: "bds 1.21.3 (subject block)",
		Eval: func(in *Input, s *Scope) bool {
			return s.Block != nil && in.CmpBool(s.Block.Waterlogged())
		}})

	// Damage (damage_sensor triggers).
	Register(&Def{Name: "has_damage", Kind: KindString, Source: "bds 1.21.3 ActorHasDamageTest: cause name or \"fatal\"",
		Eval: func(in *Input, s *Scope) bool {
			dmg := s.Ctx.Damage
			if dmg == nil {
				return in.CmpTrue(false)
			}
			if in.Str == "fatal" {
				return in.CmpTrue(dmg.Fatal)
			}
			return in.CmpTrue(dmg.Cause == in.Str)
		}})
}
