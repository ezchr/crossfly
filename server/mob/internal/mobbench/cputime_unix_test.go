//go:build crossfly_full && unix

package mobbench_test

import (
	"syscall"
	"time"
)

// cpuTime is the user and system CPU time of the process so far.
func cpuTime() time.Duration {
	var u syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &u) != nil {
		return 0
	}
	return time.Duration(u.Utime.Nano() + u.Stime.Nano())
}
