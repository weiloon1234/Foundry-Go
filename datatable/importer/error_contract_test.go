package importer

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

type privateImportError struct {
	goexit bool
	calls  atomic.Int32
}

func (e *privateImportError) Error() string {
	e.calls.Add(1)
	if e.goexit {
		runtime.Goexit()
	}
	panic("private import error")
}
func (e *privateImportError) Format(fmt.State, rune) { _ = e.Error() }

type failedImportReader struct{ cause error }

func (r failedImportReader) Read([]byte) (int, error) { return 0, r.cause }

func TestImportErrorContracts(t *testing.T) {
	definition := Define(Spec[memberImport]{Columns: []Column[memberImport]{
		Field("Name", foundryhttp.StringQuery[string](), func(row *memberImport, v string) { row.Name = v }),
	}})
	handle := func(context.Context, []Row[memberImport]) error { return nil }
	t.Run("framework input diagnostic", func(t *testing.T) {
		_, err := definition.Run(t.Context(), CSVSource(strings.NewReader("Name\n\"unterminated")), handle)
		var failure *fault.Error
		if !errors.Is(err, fault.Invalid) || !errors.As(err, &failure) || failure.Message() != "import CSV is malformed" {
			t.Fatal("input fault lost its type or diagnostic", err)
		}
		if err.Error() != "datatable import: "+failure.Error() {
			t.Fatal("input diagnostic contract changed", err)
		}
	})
	t.Run("heading diagnostic", func(t *testing.T) {
		_, err := definition.Run(t.Context(), CSVSource(strings.NewReader("Name,Name\na,b\n")), handle)
		var failure *HeadingError
		if !errors.Is(err, fault.Invalid) || !errors.As(err, &failure) || len(failure.Issues()) == 0 {
			t.Fatal("heading error lost its type", err)
		}
		if err.Error() != "datatable import: "+failure.Error() {
			t.Fatal("heading diagnostic contract changed", err)
		}
	})
	for _, goexit := range []bool{false, true} {
		for _, boundary := range []string{"reader", "handler"} {
			t.Run(fmt.Sprintf("%s/goexit=%t", boundary, goexit), func(t *testing.T) {
				cause := &privateImportError{goexit: goexit}
				source := CSVSource(strings.NewReader("Name\nalice\n"))
				consume := handle
				if boundary == "reader" {
					source = CSVSource(failedImportReader{cause})
				} else {
					consume = func(context.Context, []Row[memberImport]) error { return cause }
				}
				_, err := definition.Run(t.Context(), source, consume)
				var retained *privateImportError
				if !errors.Is(err, cause) || !errors.As(err, &retained) || retained != cause {
					t.Fatal("application cause lost")
				}
				if text := fmt.Sprintf("%v %+v %#v", err, err, err); strings.Contains(text, "private") || cause.calls.Load() != 0 {
					t.Fatal("application error was formatted")
				}
			})
		}
	}
	t.Run("application fault remains private", func(t *testing.T) {
		cause := fault.New(fault.Invalid, "private application diagnostic")
		_, err := definition.Run(t.Context(), CSVSource(strings.NewReader("Name\nalice\n")), func(context.Context, []Row[memberImport]) error { return cause })
		var retained *fault.Error
		if !errors.Is(err, cause) || !errors.As(err, &retained) || retained != cause || strings.Contains(err.Error(), "private") {
			t.Fatal("application fault was exposed or lost")
		}
	})
}

type failedImportReaderAt struct{ cause error }

func (r failedImportReaderAt) ReadAt([]byte, int64) (int, error) { return 0, r.cause }

func TestXLSXArchiveReadDiagnosticPreservesCauseWithoutFormatting(t *testing.T) {
	for _, goexit := range []bool{false, true} {
		t.Run(fmt.Sprintf("goexit=%t", goexit), func(t *testing.T) {
			cause := &privateImportError{goexit: goexit}
			_, err := Define(memberSpec()).Run(t.Context(), XLSXSource(failedImportReaderAt{cause}, 22), func(context.Context, []Row[memberImport]) error {
				t.Error("unreadable archive reached the handler")
				return nil
			})
			var failure *fault.Error
			var retained *privateImportError
			if !errors.Is(err, fault.Invalid) || !errors.Is(err, cause) || !errors.As(err, &failure) || failure.Code() != fault.Invalid || failure.Message() != "import XLSX could not be read" || !errors.As(err, &retained) || retained != cause {
				t.Fatal("archive-read classification, diagnostic or cause identity changed")
			}
			want := "datatable import: invalid: import XLSX could not be read"
			if err.Error() != want || fmt.Sprintf("%v %+v %#v", err, err, err) != want+" "+want+" "+want {
				t.Fatal("archive-read diagnostic was hidden or exposed application text")
			}
			if cause.calls.Load() != 0 {
				t.Fatal("archive-read error invoked application formatting")
			}
		})
	}
}

func TestImportDiagnosticSnapshotSurvivesCauseMutation(t *testing.T) {
	definition := Define(Spec[memberImport]{Columns: []Column[memberImport]{
		Field("Name", foundryhttp.StringQuery[string](), func(row *memberImport, v string) { row.Name = v }),
	}})
	for _, goexit := range []bool{false, true} {
		for _, boundary := range []string{"reader", "handler"} {
			t.Run(fmt.Sprintf("%s/goexit=%t", boundary, goexit), func(t *testing.T) {
				// Obtain a genuine marker through the public source-validation path.
				marker := CSVSource(strings.NewReader("")).WithComma('"')
				_, marked := definition.Run(t.Context(), marker, func(context.Context, []Row[memberImport]) error { return nil })
				var original *fault.Error
				if !errors.As(marked, &original) {
					t.Fatal("source validation did not return its typed fault")
				}
				cause := &privateImportError{goexit: goexit}
				*original = *fault.Wrap(fault.Invalid, "private application text", cause)
				source := CSVSource(strings.NewReader("Name\nalice\n"))
				consume := func(context.Context, []Row[memberImport]) error { return marked }
				if boundary == "reader" {
					source = CSVSource(failedImportReader{marked})
				}
				_, err := definition.Run(t.Context(), source, consume)
				var retained *fault.Error
				var retainedCause *privateImportError
				if !errors.Is(err, fault.Invalid) || !errors.Is(err, marked) || !errors.Is(err, original) || !errors.Is(err, cause) || !errors.As(err, &retained) || retained != original || !errors.As(err, &retainedCause) || retainedCause != cause {
					t.Fatal("mutated input cause lost classification or identity")
				}
				want := "invalid: invalid CSV delimiter"
				if marked.Error() != want || err.Error() != "datatable import: "+want {
					t.Fatal("mutation changed the owned input diagnostic")
				}
				if text := fmt.Sprintf("%v %+v %#v %v %+v %#v", marked, marked, marked, err, err, err); strings.Contains(text, "private") || cause.calls.Load() != 0 {
					t.Fatal("mutated input cause reached formatting")
				}
			})
		}
	}
}
