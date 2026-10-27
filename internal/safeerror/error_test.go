package safeerror

import (
	"errors"
	"fmt"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
)

type privateError struct{}

func TestDiagnosticRetainsCauseAndHasConcreteBoundary(t *testing.T) {
	cause := &privateError{}
	err := Diagnostic("safe", cause)
	var retained *privateError
	if !IsDiagnostic(err) || !errors.Is(err, cause) || !errors.As(err, &retained) || retained != cause {
		t.Fatal("diagnostic cause lost")
	}
	if IsDiagnostic(Wrap("safe", cause)) || Diagnostic("safe", nil) != nil {
		t.Fatal("neutral or nil wrapper became a diagnostic boundary")
	}
}

func (*privateError) Error() string          { panic("private") }
func (*privateError) Format(fmt.State, rune) { panic("private formatter") }
func TestWrapPreservesCauseWithoutClassificationOrFormatting(t *testing.T) {
	private := &privateError{}
	for _, cause := range []error{private, fault.Wrap(fault.Invalid, "invalid value", private), fault.Wrap(fault.Internal, "infrastructure", private)} {
		err := Wrap("safe", cause)
		for _, code := range []fault.Code{fault.Invalid, fault.Internal, fault.Panicked} {
			if errors.Is(err, code) != errors.Is(cause, code) {
				t.Fatal("redaction added classification")
			}
		}
		var retained *privateError
		if !errors.Is(err, private) || !errors.As(err, &retained) || retained != private {
			t.Fatal("cause lost")
		}
		if fmt.Sprintf("%v %+v %#v", err, err, err) != "safe safe safe" {
			t.Fatal("unsafe formatting")
		}
	}
	if Wrap("safe", nil) != nil {
		t.Fatal("nil became failure")
	}
}

type cyclicCapsuleCause struct{ calls int }

func (e *cyclicCapsuleCause) Error() string { e.calls++; panic("private") }
func (e *cyclicCapsuleCause) Is(error) bool { e.calls++; panic("private") }
func (e *cyclicCapsuleCause) As(any) bool   { e.calls++; panic("private") }
func (e *cyclicCapsuleCause) Unwrap() error { e.calls++; return e }

func TestForeignCapsuleIsOpaqueUntilReveal(t *testing.T) {
	cause := &cyclicCapsuleCause{}
	sealed := Seal("conversion failed", cause)
	if errors.Is(sealed, cause) {
		t.Fatal("foreign inspection reached cause")
	}
	var hidden *cyclicCapsuleCause
	if errors.As(sealed, &hidden) {
		t.Fatal("foreign classification reached cause")
	}
	_ = fmt.Sprintf("%v %+v %#v", sealed, sealed, sealed)
	revealed := Reveal("operation failed", fmt.Errorf("foreign: %w", sealed))
	if !errors.Is(revealed, cause) || !errors.As(revealed, &hidden) || hidden != cause {
		t.Fatal("cause not restored")
	}
	if cause.calls != 0 {
		t.Fatal("application error methods executed")
	}
}

func TestRevealRestoresNestedForeignCapsulesWithoutInspection(t *testing.T) {
	cause := &cyclicCapsuleCause{}
	leaf := Seal("leaf failed", cause)
	middle := Seal("native conversion failed", fmt.Errorf("native: %w", leaf))
	outer := Seal("driver failed", fmt.Errorf("driver: %w", middle))
	err := Reveal("query failed", outer)
	if cause.calls != 0 {
		t.Fatal("restoration inspected application error")
	}
	var retained *cyclicCapsuleCause
	if !errors.Is(err, cause) || !errors.As(err, &retained) || retained != cause {
		t.Fatal("nested cause identity lost")
	}
	if cause.calls != 0 {
		t.Fatal("matching identity executed application methods")
	}
}

func TestNestedRevealKeepsDiagnosticPruningAndSiblingPrecedence(t *testing.T) {
	diagnostic := errors.New("diagnostic classification")
	sibling := errors.New("explicit classification")
	err := Reveal("outer", Reveal("inner", errors.Join(Diagnostic("safe", diagnostic), sibling)))
	if !errors.Is(err, diagnostic) || !errors.Is(err, sibling) {
		t.Fatal("caller identity lost")
	}
	hidden, explicit := false, false
	complete := errorgraph.WalkPruned(err, func(current error) bool {
		hidden = hidden || errorgraph.Matches(current, diagnostic)
		explicit = explicit || errorgraph.Matches(current, sibling)
		return true
	}, IsDiagnostic)
	if !complete || hidden || !explicit {
		t.Fatal("nested projection bypassed diagnostic boundary or hid sibling")
	}
}

type wideCause struct {
	children []error
	calls    int
}

func (*wideCause) Error() string     { return "wide graph" }
func (e *wideCause) Unwrap() []error { e.calls++; return e.children }

func TestRevealRejectsOversizedProjectionWithoutTruncatingNativeIdentity(t *testing.T) {
	leaf := errors.New("last retained cause")
	graph := &wideCause{children: make([]error, 4096)}
	graph.children[len(graph.children)-1] = Seal("leaf", leaf)
	view := &revealed{node: graph}
	projected, complete := view.UnwrapBounded(3)
	if complete || projected != nil || graph.calls != 1 {
		t.Fatal("oversized graph was projected")
	}
	graph.calls = 0
	visited := 0
	if errorgraph.Walk(Reveal("outer", Reveal("inner", graph)), func(error) bool { visited++; return true }) || graph.calls != 1 || visited > 8 {
		t.Fatal("nested projection escaped shared traversal bound")
	}
	if !errors.Is(view, leaf) {
		t.Fatal("native caller lost cause beyond framework traversal budget")
	}
}

func TestRepeatedRevealKeepsOneViewAndBoundedInspection(t *testing.T) {
	cause := &privateError{}
	first := errors.Unwrap(Reveal("first", cause))
	view := first
	for range 1000 {
		view = errors.Unwrap(Reveal("again", view))
	}
	if view != first {
		t.Fatal("repeated restoration stacked forwarding views")
	}
	var retained *privateError
	if !errorgraph.Is(view, cause) || !errors.As(view, &retained) || retained != cause {
		t.Fatal("canonical restoration lost cause identity")
	}
	// Children may already be restored by an earlier foreign boundary.
	parent := errors.Unwrap(Reveal("parent", errors.Join(view)))
	children, complete := parent.(*revealed).UnwrapBounded(1)
	if !complete || len(children) != 1 || children[0] != first {
		t.Fatal("child projection stacked forwarding views")
	}
	boundary := errors.Unwrap(Reveal("diagnostic", Diagnostic("safe", cause)))
	for range 1000 {
		boundary = errors.Unwrap(Reveal("again", boundary))
	}
	visited := false
	if !errorgraph.WalkPruned(boundary, func(error) bool { visited = true; return true }, IsDiagnostic) || visited {
		t.Fatal("canonical restoration lost diagnostic pruning")
	}
}

func TestOwnedMessageUsesOnlyConcreteImmutableMarker(t *testing.T) {
	cause := &cyclicCapsuleCause{}
	owned := Owned("owned", cause)
	message, ok := OwnedMessage(owned)
	if !ok || message != "owned" || Owned("owned", nil) != nil {
		t.Fatal("owned marker was not recognized")
	}
	for _, other := range []error{nil, cause, Wrap("redacted", cause), Seal("sealed", cause)} {
		if _, ok := OwnedMessage(other); ok {
			t.Fatal("untrusted error gained owned status")
		}
	}
	if cause.calls != 0 || IsDiagnostic(owned) {
		t.Fatal("owned marker inspected application methods or changed classification discovery")
	}
}
