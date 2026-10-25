package oracle

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

// BuildEnvironment is the effective `go env -json` snapshot in each module.
// GOFLAGS is fixed: overlays, alternate modfiles, tags, gcflags and ldflags are
// not accepted inputs to an oracle freeze. Workspace and persistent GOENV are off.
type BuildEnvironment struct {
	GOFLAGS      string
	GOWORK       string
	GOENV        string
	GOOS         string
	GOARCH       string
	CGO_ENABLED  string
	GOEXPERIMENT string
	GOTOOLCHAIN  string
	GOMOD        string
	GOROOT       string
	GOVERSION    string
	GCFlags      string
}

func FixedEnvironment(environ []string) ([]string, error) {
	// Do not silently change the caller's measurement request.
	for _, v := range environ {
		key, value, ok := strings.Cut(v, "=")
		if !ok {
			continue
		}
		if (key == "GOFLAGS" && value != "" && value != "-p=4") || (key == "GOWORK" && value != "" && value != "off") {
			return nil, fmt.Errorf("oracle freeze requires GOFLAGS=-p=4 and GOWORK=off (no overlays or external workspace inputs)")
		}
	}
	fixed := map[string]string{"GOFLAGS": "-p=4", "GOWORK": "off", "GOENV": "off", "GOTOOLCHAIN": "local", "GOOS": runtime.GOOS, "GOARCH": runtime.GOARCH, "GOEXPERIMENT": "", "GOMAXPROCS": "4"}
	// GOARCH-specific features and compiler tool overrides would alter archives.
	blocked := map[string]bool{"GOAMD64": true, "GOARM64": true, "GOARM": true, "GOMIPS": true, "GOPPC64": true, "GORISCV64": true, "GOWASM": true, "GCCGO": true, "CC": true, "CXX": true, "CGO_CFLAGS": true, "CGO_CPPFLAGS": true, "CGO_CXXFLAGS": true, "CGO_LDFLAGS": true, "GO_EXTLINK_ENABLED": true}
	result := []string{}
	for _, v := range environ {
		key, _, _ := strings.Cut(v, "=")
		if _, ok := fixed[key]; !ok && !blocked[key] {
			result = append(result, v)
		}
	}
	for key, value := range fixed {
		result = append(result, key+"="+value)
	}
	return result, nil
}
func (b BuildEnvironment) Validate(dir string) error {
	if b.GOFLAGS != "-p=4" || b.GOWORK != "off" || b.GOENV != "" || b.GOTOOLCHAIN != "local" || b.GCFlags != "" || b.GOEXPERIMENT != "" || b.GOOS == "" || b.GOARCH == "" || b.GOVERSION == "" || b.GOROOT == "" || b.CGO_ENABLED == "" || !filepath.IsAbs(b.GOMOD) || !filepath.IsAbs(dir) {
		return fmt.Errorf("missing or noncanonical build provenance")
	}
	return nil
}
