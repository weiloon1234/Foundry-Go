package testinputs

import (
	"go/build"
	"go/importer"
	"go/types"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var standardImporterOnce sync.Once

// StandardImporter returns a types.Importer for standard library packages.
// Under -trimpath, runtime.GOROOT() is empty so build.Default.GOROOT must be
// resolved from go env GOROOT before using importer.Default().
func StandardImporter() types.Importer {
	standardImporterOnce.Do(func() {
		if build.Default.GOROOT != "" {
			return
		}
		if out, err := exec.Command("go", "env", "GOROOT").Output(); err == nil {
			if root := filepath.Clean(strings.TrimSpace(string(out))); root != "" {
				build.Default.GOROOT = root
			}
		}
	})
	return importer.Default()
}
