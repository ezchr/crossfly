//go:build crossfly_full

package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"math"
	"sync"
)

// DaylightDetector samples sky brightness every twenty game ticks, following the vanilla Java 26.3 day timeline.
type DaylightDetector struct {
	solid
	transparent
	sourceWaterDisplacer
	Power    int
	Inverted bool
}

func (d DaylightDetector) EncodeBlock() (string, map[string]any) {
	name := "minecraft:daylight_detector"
	if d.Inverted {
		name = "minecraft:daylight_detector_inverted"
	}
	return name, map[string]any{"redstone_signal": int32(d.Power)}
}
func (DaylightDetector) EncodeItem() (string, int16) { return "minecraft:daylight_detector", 0 }
func (DaylightDetector) BreakInfo() BreakInfo {
	return newBreakInfo(.2, alwaysHarvestable, axeEffective, oneOf(DaylightDetector{}))
}
func (DaylightDetector) Model() world.BlockModel                            { return daylightDetectorModel{} }
func (DaylightDetector) EncodeNBT() map[string]any                          { return map[string]any{"id": "DaylightDetector"} }
func (d DaylightDetector) DecodeNBT(map[string]any) any                     { return d }
func (d DaylightDetector) RedstonePower(cube.Pos, *world.Tx, cube.Face) int { return d.Power }
func (d DaylightDetector) Tick(tick int64, pos cube.Pos, tx *world.Tx) {
	if tick%20 == 0 && tx.World().Dimension().TimeCycle() {
		d.update(pos, tx)
	}
}
func (d DaylightDetector) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, user item.User, _ *item.UseContext) bool {
	if game, ok := user.(interface{ GameMode() world.GameMode }); ok && !game.GameMode().AllowsEditing() {
		return false
	}
	d.Inverted = !d.Inverted
	tx.SetBlock(pos, d, &world.SetOpts{DisableBlockUpdates: true})
	d.update(pos, tx)
	return true
}
func (d DaylightDetector) update(pos cube.Pos, tx *world.Tx) {
	rain, thunder := tx.World().WeatherIntensity()
	sky := 0
	if tx.World().Dimension().TimeCycle() {
		sky = int(tx.CurrentSkyLight(pos))
	}
	power := daylightPower(sky, tx.World().Time(), d.Inverted, rain, thunder)
	if d.Power != power {
		d.Power = power
		tx.SetBlock(pos, d, nil)
	}
}
func allDaylightDetectors() (blocks []world.Block) {
	for _, inverted := range []bool{false, true} {
		for power := 0; power <= 15; power++ {
			blocks = append(blocks, DaylightDetector{Power: power, Inverted: inverted})
		}
	}
	return
}

type daylightDetectorModel struct{}

func (daylightDetectorModel) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	return []cube.BBox{cube.Box(0, 0, 0, 1, .375, 1)}
}
func (daylightDetectorModel) FaceSolid(_ cube.Pos, f cube.Face, _ world.BlockSource) bool {
	return f == cube.FaceDown
}

func daylightPower(sky, timeOfDay int, inverted bool, rain, thunder float32) int {
	timeOfDay = ((timeOfDay % 24000) + 24000) % 24000
	// Java 26.3 data/minecraft/timeline/day.json: gameplay/sky_light_level is a linear multiplier.
	t := float32(timeOfDay)
	level := float32(1)
	switch {
	case timeOfDay >= 11867 && timeOfDay < 13670:
		level = 1 + (float32(.26666668)-1)*(t-11867)/(13670-11867)
	case timeOfDay >= 13670 && timeOfDay < 22330:
		level = .26666668
	case timeOfDay >= 22330 || timeOfDay < 133:
		if timeOfDay < 133 {
			t += 24000
		}
		level = float32(.26666668) + (1-float32(.26666668))*(t-22330)/(24133-22330)
	}
	level *= 15
	rainOnly := max(0, rain-thunder)
	rainValue := level + (4-level)*float32(.3125)
	level += (rainValue - level) * rainOnly
	thunderValue := level + (4-level)*float32(.52734375)
	level += (thunderValue - level) * thunder
	effective := max(0, sky-int(15-level))
	if inverted {
		return world.ClampRedstonePower(15 - effective)
	}
	if effective == 0 {
		return 0
	}
	cycle := (timeOfDay - 6000 + 24000) % 24000
	angle := daylightBezier(float32(cycle)/24000) * 360
	angle *= float32(math.Pi / 180)
	target := float32(0)
	if angle >= float32(math.Pi) {
		target = float32(2 * math.Pi)
	}
	angle += (target - angle) * float32(.2)
	result := float32(effective) * daylightJavaCos(angle)
	return world.ClampRedstonePower(int(math.Floor(float64(result) + .5)))
}

func daylightBezier(x float32) float32 {
	curve := func(a, b, t float32) float32 { return (((3*a-3*b+1)*t+(-6*a+3*b))*t + 3*a) * t }
	gradient := func(a, b, t float32) float32 { return (3*(3*a-3*b+1)*t+2*(-6*a+3*b))*t + 3*a }
	t := x
	for i := 0; i < 4; i++ {
		error := curve(.362, .638, t) - x
		if math.Abs(float64(error)) < 1e-5 {
			return curve(.241, .759, t)
		}
		g := gradient(.362, .638, t)
		if g < 1e-5 {
			break
		}
		t -= min(float32(.25), max(float32(-.25), error/g))
	}
	lo, hi := float32(0), float32(1)
	for i := 0; i < 64; i++ {
		error := curve(.362, .638, t) - x
		if math.Abs(float64(error)) < 1e-5 {
			break
		}
		if error < 0 {
			lo = t
		} else {
			hi = t
		}
		next := (lo + hi) / 2
		if next == t {
			break
		}
		t = next
	}
	return curve(.241, .759, t)
}

var daylightSine = sync.OnceValue(func() *[65536]float32 {
	values := new([65536]float32)
	for i := range values {
		values[i] = float32(math.Sin(float64(i) * 2 * math.Pi / 65536))
	}
	return values
})

func daylightJavaCos(angle float32) float32 {
	return daylightSine()[int(angle*float32(10430.378)+16384)&65535]
}
