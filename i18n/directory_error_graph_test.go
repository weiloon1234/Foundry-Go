package i18n

import (
	"github.com/weiloon1234/Foundry-Go/internal/errorgraphtest"
	"io/fs"
	"testing"
)

type graphDirectoryFS struct{ cause error }

func (f graphDirectoryFS) Open(string) (fs.File, error) { return graphDirectory{f.cause}, nil }

type graphDirectory struct{ cause error }

func (graphDirectory) Stat() (fs.FileInfo, error)           { return nil, nil }
func (graphDirectory) Read([]byte) (int, error)             { panic("not a file") }
func (graphDirectory) Close() error                         { return nil }
func (f graphDirectory) ReadDir(int) ([]fs.DirEntry, error) { return nil, f.cause }
func TestCatalogDirectoryContainsErrorInspection(t *testing.T) {
	set, _ := NewLocaleSet("en", "en")
	for _, mode := range []string{"cycle", "panic", "goexit"} {
		cause := &errorgraphtest.Error{Mode: mode}
		_, err := Load(t.Context(), graphDirectoryFS{cause}, set, CatalogOptions{})
		if err == nil || cause.Formatted.Load() != 0 || cause.Visits.Load() > 256 {
			t.Fatal("directory failure accepted or inspected without bounds")
		}
	}
}
