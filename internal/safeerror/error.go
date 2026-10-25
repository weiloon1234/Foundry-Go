// Package safeerror redacts extension failures without adding classifications.
package safeerror

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"slices"
)

// Wrap retains cause for inspection without invoking its formatting methods.
// A nil cause stays nil; message must be framework-owned diagnostic text.
func Wrap(message string, cause error) error {
	if cause == nil {
		return nil
	}
	return Retain(cause, &redacted{message: message, cause: cause})
}

// Prefix adds framework operation context without formatting an application cause.
func Prefix(operation string, cause error) error {
	if cause == nil {
		return nil
	}
	if message, ok := OwnedMessage(cause); ok {
		return Owned(operation+": "+message, cause)
	}
	return Wrap(operation+" failed", cause)
}

// Owned marks an immutable diagnostic produced by a framework owner. Callback
// containment can retain this text without inspecting or formatting the cause.
// Never derive message from an application error or an already-exposed fault.
func Owned(message string, cause error) error {
	if cause == nil {
		return nil
	}
	result := &owned{message: message, cause: cause}
	if source := snapshot(cause); source != nil {
		result.note, result.frames, result.attributes = source.note, source.frames, source.attributes
	}
	return result
}

type owned struct {
	attributes *attributeSnapshot
	note       *fault.Note
	frames     []fault.Frame
	message    string
	cause      error
}

func (e *owned) Error() string    { return e.message }
func (e *owned) GoString() string { return e.message }
func (e *owned) Unwrap() error    { return e.cause }

// OwnedMessage recognizes only concrete framework markers and capsules carrying
// their captured text. It never walks causes or invokes application methods.
func OwnedMessage(err error) (string, bool) {
	if e, ok := err.(*sealed); ok && e != nil && e.owned {
		return e.message, true
	}
	if e, ok := err.(*redacted); ok && e != nil && e.owned {
		return e.message, true
	}
	e := snapshot(err)
	if e == nil {
		return "", false
	}
	return e.message, true
}

type redacted struct {
	message    string
	cause      error
	diagnostic bool
	owned      bool
}

// Diagnostic retains a cause for errors.Is/As that must not supply an HTTP
// classification. Use only where retaining a previously discarded cause.
func Diagnostic(message string, cause error) error {
	if cause == nil {
		return nil
	}
	return &redacted{message: message, cause: cause, diagnostic: true, owned: true}
}

// IsDiagnostic identifies the framework-owned boundary without calling error methods.
func IsDiagnostic(err error) bool {
	if view, ok := err.(*revealed); ok && view != nil {
		err = view.node
	}
	if marker, ok := err.(*owned); ok && marker != nil {
		err = marker.cause
	}
	e, ok := err.(*redacted)
	return ok && e != nil && e.diagnostic
}

func (e *redacted) Error() string    { return e.message }
func (e *redacted) GoString() string { return e.message }
func (e *redacted) Unwrap() error    { return e.cause }

// Seal keeps application causes opaque to foreign-library formatting and error
// inspection. Reveal restores them after the foreign operation has returned.
func Seal(message string, cause error) error {
	if cause == nil {
		return nil
	}
	diagnostic, owned := OwnedMessage(cause)
	if owned {
		message = diagnostic
	}
	return &sealed{message: message, cause: cause, owned: owned}
}

type sealed struct {
	message string
	cause   error
	match   error
	owned   bool
}

func (e *sealed) Error() string        { return e.message }
func (e *sealed) GoString() string     { return e.message }
func (e *sealed) Is(target error) bool { return e.match != nil && target == e.match }

// SealMatching preserves one already-established framework control sentinel
// without allowing the foreign library to inspect the original cause.
func SealMatching(message string, cause, match error) error {
	if cause == nil {
		return nil
	}
	sealed := Seal(message, cause).(*sealed)
	sealed.match = match
	return sealed
}

// Reveal exposes sealed causes only after leaving the foreign library. The
// projection is lazy: constructing it calls no application methods. Subsequent
// framework inspection must use bounded errorgraph traversal inside containment.
func Reveal(message string, foreign error) error {
	if foreign == nil {
		return nil
	}
	diagnostic, owned := OwnedMessage(foreign)
	if owned {
		message = diagnostic
	}
	return &redacted{message: message, cause: revealNode(foreign), diagnostic: IsDiagnostic(foreign), owned: owned}
}

type revealed struct{ node error }

// Reuse views so shallow matching never recursively forwards through views.
func revealNode(node error) error {
	if node == nil {
		return nil
	}
	if _, ok := node.(*revealed); ok {
		return node
	}
	return &revealed{node: node}
}

func (*revealed) Error() string          { return "application operation failed" }
func (*revealed) GoString() string       { return "application operation failed" }
func (e *revealed) Is(target error) bool { return errorgraph.Matches(e.node, target) }
func (e *revealed) As(target any) bool   { return errorgraph.AsTargetShallow(e.node, target) }
func (e *revealed) Unwrap() []error      { return revealChildren(e.children()) }

// UnwrapBounded lets the shared walker reject oversized graphs before allocating
// their projection. Ordinary errors.Is/As still receive every original child.
func (e *revealed) UnwrapBounded(maxChildren int) ([]error, bool) {
	var children []error
	if node, ok := e.node.(interface{ UnwrapBounded(int) ([]error, bool) }); ok {
		var complete bool
		children, complete = node.UnwrapBounded(maxChildren)
		if !complete {
			return nil, false
		}
	} else {
		children = e.children()
	}
	if len(children) > maxChildren {
		return nil, false
	}
	return revealChildren(children), true
}

func (e *revealed) children() []error {
	var children []error
	switch node := e.node.(type) {
	case *sealed:
		children = []error{node.cause}
	case interface{ Unwrap() error }:
		children = []error{node.Unwrap()}
	case interface{ Unwrap() []error }:
		children = node.Unwrap()
	}
	return children
}

func revealChildren(children []error) []error {
	result := make([]error, len(children))
	for i, child := range children {
		result[i] = revealNode(child)
	}
	return result
}

// UnwrapOwned removes only a source-issued immutable marker. It does not walk
// arbitrary causes or call application methods. Typed owners can use it where
// they previously checked a direct concrete result from another framework owner.
func UnwrapOwned(err error) error {
	if marked, ok := err.(*owned); ok && marked != nil {
		return marked.cause
	}
	return err
}

// WithAttributes captures attributes at their trusted construction owner. Values
// must be framework vocabulary, never application text or adapter metadata.
func WithAttributes(err, source error, attributes []fault.Attribute) error {
	if err == nil {
		return nil
	}
	message, ok := OwnedMessage(err)
	if !ok {
		message = "application operation failed"
	}
	result := Owned(message, err).(*owned)
	if original, ok := err.(*owned); ok {
		copy := *original
		result = &copy
	}
	result.attributes = &attributeSnapshot{source: source, values: slices.Clone(attributes)}
	return result
}

func Attributes(err error) []fault.Attribute {
	if value := snapshot(err); value != nil && value.attributes != nil {
		return slices.Clone(value.attributes.values)
	}
	return nil
}

// AttributeSource identifies the construction owner shared by transparent views.
// Distinct failures may carry equal attribute keys and values.
func AttributeSource(err error) error {
	if value := snapshot(err); value != nil && value.attributes != nil {
		return value.attributes.source
	}
	return nil
}

type attributeSnapshot struct {
	source error
	values []fault.Attribute
}
