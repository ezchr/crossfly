//go:build crossfly_full && !linux

package world_test

// processCPU is not measured on this system.
func processCPU() int64 { return 0 }
