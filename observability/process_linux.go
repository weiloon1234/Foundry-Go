//go:build linux

package observability

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strconv"
)

// maxCountedFDs bounds directory reads during one scrape.
const maxCountedFDs = 1 << 20

func addPlatformProcessStats(stats *processStats) {
	if dir, err := os.Open("/proc/self/fd"); err == nil {
		count := 0
		for count <= maxCountedFDs {
			names, err := dir.Readdirnames(1024)
			count += len(names)
			if errors.Is(err, io.EOF) {
				// The directory handle itself is one open descriptor.
				stats.openFDs, stats.hasOpenFDs = float64(max(0, count-1)), true
				break
			}
			if err != nil {
				break
			}
		}
		_ = dir.Close()
	}
	data, err := os.ReadFile("/proc/self/statm")
	if err != nil || len(data) > 256 {
		return
	}
	fields := bytes.Fields(data)
	if len(fields) < 2 {
		return
	}
	if pages, err := strconv.ParseUint(string(fields[1]), 10, 64); err == nil {
		stats.residentBytes, stats.hasResidentMem = float64(pages)*float64(os.Getpagesize()), true
	}
}
