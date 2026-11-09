package javasession

import "testing"

// TestWeatherFades checks that rain fades in over 100 ticks, as in vanilla, and that it starts
// raining once the level passes 0.2.
func TestWeatherFades(t *testing.T) {
	var rain float32
	startedAt := -1
	for tick := 1; tick <= 200; tick++ {
		was := rain > rainingLevel
		rain = stepTowards(rain, 1)
		if !was && rain > rainingLevel && startedAt < 0 {
			startedAt = tick
		}
		if rain == 1 {
			if tick < 99 || tick > 101 {
				t.Fatalf("rain reached 1 at tick %d, want about 100", tick)
			}
			break
		}
	}
	if startedAt < 20 || startedAt > 22 {
		t.Fatalf("rain started at tick %d, want about 21", startedAt)
	}
	for i := 0; i < 200; i++ {
		rain = stepTowards(rain, 0)
	}
	if rain != 0 {
		t.Fatalf("rain level %v after fading out, want 0", rain)
	}
}
