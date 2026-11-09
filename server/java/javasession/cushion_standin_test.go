package javasession

import "testing"

// Cushions are new in 26.3: older clients, also behind a proxy, get the stand-in.
func TestJavaVersionBefore(t *testing.T) {
	for v, want := range map[string]bool{
		"26.2":     true,
		"26.2.1":   true,
		"1.21.11":  true,
		"26.3":     false,
		"26.3.1":   false,
		"26.4":     false,
		"27.1":     false,
		"":         false,
		"snapshot": false,
	} {
		if got := javaVersionBefore(v, 26, 3); got != want {
			t.Errorf("javaVersionBefore(%q, 26, 3) = %v, want %v", v, got, want)
		}
	}
}
