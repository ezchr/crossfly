//go:build crossfly_full

package jpets

import "math"

// Mth.sin / Mth.cos: Java reads a 65536-entry float table, which is what
// makes riding pushes and dashes come out as on a Java server.
var sinTable = func() []float32 {
	t := make([]float32, 65536)
	for i := range t {
		t[i] = float32(math.Sin(float64(i) / 10430.378350470453))
	}
	return t
}()

// mthSin is Mth.sin(double).
func mthSin(v float64) float32 {
	return sinTable[int(int64(v*10430.378350470453)&65535)]
}

// mthCos is Mth.cos(double).
func mthCos(v float64) float32 {
	return sinTable[int(int64(v*10430.378350470453+16384)&65535)]
}

// sinDeg32 and cosDeg32 are Mth.sin/cos(yRot * (float)(PI / 180)) with the
// float yaw Java entities keep (a float product widened to double).
func sinDeg32(yaw float64) float32 { return mthSin(float64(float32(yaw) * float32(math.Pi/180))) }
func cosDeg32(yaw float64) float32 { return mthCos(float64(float32(yaw) * float32(math.Pi/180))) }
