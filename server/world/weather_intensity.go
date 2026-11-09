//go:build crossfly_full

package world

// WeatherIntensity returns Java-style interpolated rain and rain-weighted thunder levels. Abrupt weather changes
// transition over one hundred game ticks; daylight detectors sample these rather than the binary weather flags.
func (w *World) WeatherIntensity() (rain, thunder float32) {
	w.set.Lock()
	defer w.set.Unlock()
	return w.rainIntensity, w.thunderIntensity * w.rainIntensity
}
func (w *World) updateWeatherIntensity() {
	if !w.Dimension().WeatherCycle() {
		w.rainIntensity = 0
		w.thunderIntensity = 0
		return
	}
	if w.set.Raining {
		w.rainIntensity = min(1, w.rainIntensity+.01)
	} else {
		w.rainIntensity = max(0, w.rainIntensity-.01)
	}
	if w.set.Thundering {
		w.thunderIntensity = min(1, w.thunderIntensity+.01)
	} else {
		w.thunderIntensity = max(0, w.thunderIntensity-.01)
	}
}
