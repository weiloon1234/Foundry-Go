package oracle

import (
	"fmt"
	"path/filepath"
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

func (b BuildEnvironment) Validate(dir string) error {
	if b.GOFLAGS != "-p=4" || b.GOWORK != "off" || b.GOENV != "" || b.GOTOOLCHAIN != "local" || b.GCFlags != "" || b.GOEXPERIMENT != "" || b.GOOS == "" || b.GOARCH == "" || b.GOVERSION == "" || b.GOROOT == "" || b.CGO_ENABLED == "" || !filepath.IsAbs(b.GOMOD) || !filepath.IsAbs(dir) {
		return fmt.Errorf("missing or noncanonical build provenance")
	}
	return nil
}
