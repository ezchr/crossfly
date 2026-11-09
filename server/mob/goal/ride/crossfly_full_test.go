//go:build crossfly_full

package ride_test

import "github.com/df-mc/dragonfly/server/entity"

// The tests run with the mobs on, as vanilla.Register sets them up.
func init() { entity.SetMobs(true) }
