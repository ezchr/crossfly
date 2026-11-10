//go:build crossfly_full

package jnether

// ShulkerDuplicates is the roll of Shulker.hitByShulkerBullet: with n
// shulkers (alive, the hit one included) within 8 of where the hit one sat,
// a new shulker appears unless r < (n-1)/5 (r uniform in [0, 1)): always
// alone, 4 in 5 with two, ... never with six or more.
func ShulkerDuplicates(n int, r float32) bool {
	return !(r < float32(n-1)/5)
}
