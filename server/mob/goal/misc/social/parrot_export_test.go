//go:build crossfly_full

package social

// SetParrotRand replaces the random source of the parrot imitations for a test and returns a
// function restoring it.
func SetParrotRand(f func(n int) int) (restore func()) {
	old := parrotRand
	parrotRand = f
	return func() { parrotRand = old }
}
