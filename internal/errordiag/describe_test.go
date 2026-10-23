package errordiag_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
)

type secretError struct{}

func (secretError) Error() string { return "password=hunter2" }

type hostileUnwrap struct{}

func (hostileUnwrap) Error() string { return "hostile" }
func (hostileUnwrap) Unwrap() error { panic("private") }

func TestDescribeIsRedactedAndBounded(t *testing.T) {
	err := fmt.Errorf("handler: %w", fault.Wrap(fault.Internal, "query failed", secretError{}))
	d := errordiag.Describe(err)
	rendered := fmt.Sprintf("%+v", d)
	if strings.Contains(rendered, "hunter2") {
		t.Fatal("diagnostic formatted an application error", rendered)
	}
	if len(d.Faults) != 1 || d.Faults[0].Code != fault.Internal || d.Faults[0].Message != "query failed" {
		t.Fatalf("fault note missing: %+v", d)
	}
	if len(d.Types) != 3 || d.Types[2] != "errordiag_test.secretError" {
		t.Fatalf("type chain missing: %+v", d.Types)
	}
	if errordiag.Describe(nil).IsZero() == false {
		t.Fatal("nil error produced a diagnostic")
	}
	if !errordiag.Describe(hostileUnwrap{}).Truncated {
		t.Fatal("failing Unwrap must truncate, not crash")
	}
	joined := errors.Join(fault.Overloaded, fault.New(fault.Closed, "closed"))
	if d := errordiag.Describe(joined); len(d.Faults) != 2 {
		t.Fatalf("joined faults missing: %+v", d)
	}
}

func call[K, R any](f func(K) R) R {
	var k K
	return f(k)
}

type catcher[K any] struct{ called bool }

func (*catcher[K]) Error() string { return "application error" }
func (c *catcher[K]) FoundryDiagnostic(K) []fault.Attribute {
	c.called = true
	return []fault.Attribute{{Key: "secret", Value: "must not publish"}}
}
func diagnosticCatcher[K any](_ func(K) []fault.Attribute) *catcher[K] { return new(catcher[K]) }

type databaseError = database.Error

type promotedDiagnostic struct{ *databaseError }

func TestDiagnosticCapabilitiesFailClosedAndStayWithConcreteOwners(t *testing.T) {
	db := database.NewError("read", database.NotFound).(*database.Error)
	for _, attributes := range [][]fault.Attribute{
		call(db.FoundryDiagnostic),
		call(foundryhttp.NotFound.FoundryDiagnostic),
		call(foundryhttp.NotFound.WithCause(errors.New("cause")).(errordiag.Provider).FoundryDiagnostic),
	} {
		if len(attributes) != 0 {
			t.Fatal("zero seal published diagnostic", attributes)
		}
	}
	captured := diagnosticCatcher(db.FoundryDiagnostic)
	for _, err := range []error{captured, promotedDiagnostic{db}, &promotedDiagnostic{db}} {
		if diagnostic := errordiag.Describe(err); len(diagnostic.Attributes) != 0 {
			t.Fatal("application diagnostic was trusted", diagnostic.Attributes)
		}
	}
	if captured.called {
		t.Fatal("application captured a diagnostic capability")
	}
	for _, err := range []error{db, foundryhttp.NotFound, foundryhttp.NotFound.WithCause(errors.New("cause"))} {
		if diagnostic := errordiag.Describe(err); len(diagnostic.Attributes) == 0 {
			t.Fatalf("framework diagnostic %T lost its attributes", err)
		}
	}
}
