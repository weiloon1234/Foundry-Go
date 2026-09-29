package errordiag_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
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
