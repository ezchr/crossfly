//go:build crossfly_full

package world

import (
	"math"
	"testing"
	"time"
)

func TestWeatherIntensityTransitionsOverOneHundredTicks(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()
	w.StopWeatherCycle()
	w.StartRaining(time.Hour)
	for range 50 {
		w.AdvanceTick()
	}
	rain, thunder := w.WeatherIntensity()
	if math.Abs(float64(rain)-.5) > 1e-5 || thunder != 0 {
		t.Fatalf("rain transition: %v %v", rain, thunder)
	}
	w.StartThundering(time.Hour)
	for range 50 {
		w.AdvanceTick()
	}
	rain, thunder = w.WeatherIntensity()
	if math.Abs(float64(rain)-1) > 1e-5 || math.Abs(float64(thunder)-.5) > 1e-5 {
		t.Fatalf("thunder transition: %v %v", rain, thunder)
	}
	w.StopRaining()
	w.StopThundering()
	for range 100 {
		w.AdvanceTick()
	}
	rain, thunder = w.WeatherIntensity()
	if math.Abs(float64(rain)) > 1e-5 || math.Abs(float64(thunder)) > 1e-5 {
		t.Errorf("clear weather did not finish transition: %v %v", rain, thunder)
	}
}
