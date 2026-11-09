//go:build crossfly_full && !unix

package mobbench_test

import "time"

// cpuTime is not measured here: the benchmarks report wall time only.
func cpuTime() time.Duration { return 0 }
