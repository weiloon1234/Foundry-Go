//go:build unix

package observability

import "syscall"

func readProcessStats() processStats {
	var stats processStats
	var usage syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &usage) == nil {
		stats.cpuSeconds = float64(usage.Utime.Sec) + float64(usage.Utime.Usec)/1e6 + float64(usage.Stime.Sec) + float64(usage.Stime.Usec)/1e6
		stats.hasCPU = true
	}
	var limit syscall.Rlimit
	if syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit) == nil {
		stats.maxFDs, stats.hasMaxFDs = float64(limit.Cur), true
	}
	addPlatformProcessStats(&stats)
	return stats
}
