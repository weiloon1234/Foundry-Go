package http

import (
	stdhttp "net/http"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// Snapshot copies caller-owned slices before a server/module retains config.
func (c ServerConfig) Snapshot() ServerConfig {
	c.MaintenanceReadPaths = slices.Clone(c.MaintenanceReadPaths)
	return c
}

func (c ServerConfig) validateMaintenancePaths() error {
	if len(c.MaintenanceReadPaths) > 16 {
		return fault.New(fault.Invalid, "too many maintenance read paths")
	}
	seen := make(map[string]bool, len(c.MaintenanceReadPaths))
	for _, path := range c.MaintenanceReadPaths {
		if path == "" || len(path) > 1024 {
			return fault.New(fault.Invalid, "invalid maintenance read path")
		}
		canonical, err := StaticPath(path).URL(NoPath{})
		if err != nil {
			return err
		}
		if canonical != path {
			return fault.New(fault.Invalid, "maintenance paths must be canonical unescaped static paths")
		}
		if seen[path] {
			return fault.New(fault.Duplicate, "duplicate maintenance read path")
		}
		seen[path] = true
	}
	return nil
}

func (c ServerConfig) permitsMaintenanceRead(request *stdhttp.Request) bool {
	if request.Method != stdhttp.MethodGet && request.Method != stdhttp.MethodHead || request.URL == nil || request.URL.RawPath != "" {
		return false
	}
	return slices.Contains(c.MaintenanceReadPaths, request.URL.Path)
}
