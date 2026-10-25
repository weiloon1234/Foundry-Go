package i18n

import (
	"errors"
	"fmt"
	"io/fs"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

type privateCatalogError struct {
	exit  bool
	calls atomic.Int32
}

func (e *privateCatalogError) Error() string {
	e.calls.Add(1)
	if e.exit {
		runtime.Goexit()
	}
	panic("private filesystem diagnostic")
}
func (e *privateCatalogError) Format(fmt.State, rune) { _ = e.Error() }

type failedCatalogFS struct{ cause error }

func (f failedCatalogFS) Open(string) (fs.File, error) { return nil, f.cause }

func TestCatalogLoadDiagnosticsKeepFilesystemErrorsPrivate(t *testing.T) {
	set, _ := NewLocaleSet("en", "en")
	for _, exit := range []bool{false, true} {
		cause := &privateCatalogError{exit: exit}
		for _, read := range []bool{false, true} {
			var err error
			if read {
				_, err = ReadTemplates(t.Context(), failedCatalogFS{cause}, set)
			} else {
				_, err = Load(t.Context(), failedCatalogFS{cause}, set, CatalogOptions{})
			}
			var failed *fault.Error
			if !errors.Is(err, fault.Invalid) || !errors.As(err, &failed) || !strings.Contains(failed.Message(), catalogDirectoryReason) {
				t.Fatal("catalog diagnostic lost its type or safe reason")
			}
			if text := fmt.Sprintf("%v %+v %#v", err, err, err); strings.Contains(text, "private") || cause.calls.Load() != 0 {
				t.Fatal("filesystem error was formatted")
			}
			// Filesystem causes were already discarded by the catalog reader;
			// restoring its own diagnostic must not expose them now.
			if errors.Is(err, cause) {
				t.Fatal("catalog reader exposed a discarded filesystem cause")
			}
		}
	}
	for _, exit := range []bool{false, true} {
		if _, err := ReadTemplates(t.Context(), hostileCatalogFS{exit}, set); !errors.Is(err, fault.Panicked) || strings.Contains(err.Error(), "private") {
			t.Fatal("catalog read failed to contain filesystem panic/Goexit", err)
		}
	}
}
