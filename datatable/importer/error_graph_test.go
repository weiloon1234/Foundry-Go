package importer

import (
	"context"
	"encoding/csv"
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraphtest"
	"github.com/weiloon1234/Foundry-Go/validation"
	"strings"
	"testing"
)

func TestImportContainsReaderErrorGraphs(t *testing.T) {
	for _, mode := range []string{"cycle", "panic", "goexit"} {
		for _, xlsx := range []bool{false, true} {
			cause := &errorgraphtest.Error{Mode: mode}
			source := CSVSource(cause)
			if xlsx {
				source = XLSXSource(cause, 1024)
			}
			calls := 0
			_, err := Define(memberSpec()).Run(t.Context(), source, func(_ context.Context, _ []Row[memberImport]) error { calls++; return nil })
			if err == nil || !errors.Is(err, cause) || calls != 0 || cause.Formatted.Load() != 0 || cause.Visits.Load() > 512 {
				t.Fatal("reader error escaped containment or published rows")
			}
		}
	}
}

func TestImportContainsValidationErrorGraphs(t *testing.T) {
	for _, mode := range []string{"cycle", "panic", "goexit"} {
		cause := &errorgraphtest.Error{Mode: mode}
		spec := memberSpec()
		spec.Rules = []validation.Rule[memberImport]{validation.Custom(validation.Spec{ID: "app.validation", Message: "Invalid row."}, func(context.Context, memberImport) (bool, error) { return false, cause })}
		if err := Define(spec).Validate(); err != nil {
			t.Fatal(err)
		}
		calls := 0
		_, err := Define(spec).Run(t.Context(), CSVSource(strings.NewReader("Name,Age,Balance,Joined,Active,Nickname\nAda,36,1.25,2026-09-25,true,x\n")), func(context.Context, []Row[memberImport]) error { calls++; return nil })
		if err == nil || !errors.Is(err, cause) || calls != 0 || cause.Formatted.Load() != 0 || cause.Visits.Load() > 512 {
			t.Fatal("validation graph was accepted or formatted")
		}
	}
}

type csvFailureReader struct{ err error }

func (r csvFailureReader) Read([]byte) (int, error) { return 0, r.err }
func TestImportCSVParseErrorInspectionFailureStaysInternal(t *testing.T) {
	for _, mode := range []string{"cycle", "panic", "goexit"} {
		cause := &errorgraphtest.Error{Mode: mode}
		parse := &csv.ParseError{StartLine: 1, Line: 1, Column: 1, Err: cause}
		calls := 0
		_, err := Define(memberSpec()).Run(t.Context(), CSVSource(csvFailureReader{parse}), func(context.Context, []Row[memberImport]) error { calls++; return nil })
		if err == nil || !errors.Is(err, parse) || !errors.Is(err, cause) || calls != 0 || cause.Formatted.Load() != 0 {
			t.Fatal("CSV inspection failure normalized or lost cause")
		}
		want := fault.Panicked
		if mode == "cycle" {
			want = fault.Internal
		}
		if !errors.Is(err, want) {
			t.Fatal("CSV inspection failure lost internal classification")
		}
	}
}
