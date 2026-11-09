//go:build crossfly_full && linux

package world_test

import "syscall"

// processCPU returns the CPU time the process used so far in nanoseconds. Benchmarks report it as well as the wall
// time, which on a busy machine includes the time other processes ran.
func processCPU() int64 {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return ru.Utime.Nano() + ru.Stime.Nano()
}
