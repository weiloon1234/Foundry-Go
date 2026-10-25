package safeerror

import (
	"errors"
	"slices"
	"strings"
	"sync"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// NewFault snapshots a diagnostic at its framework construction site. Unlike
// recognizing an arbitrary *fault.Error later, this cannot trust application
// messages or an application mutation of an exposed fault.
func NewFault(code fault.Code, message string) error {
	return WrapFault(code, message, nil)
}

// WrapFault retains the cause without formatting it. Only framework-owned
// diagnostic text belongs in message.
func WrapFault(code fault.Code, message string, cause error) error {
	failure := fault.Wrap(code, message, cause)
	// Code may have been discovered in an application error graph. Preserve its
	// identity, but only publish known framework classification text.
	switch code {
	case fault.Invalid, fault.Duplicate, fault.Missing, fault.Cycle, fault.Closed, fault.Conflict, fault.Internal, fault.Panicked, fault.Timeout, fault.Overloaded:
		return &owned{message: failure.Error(), cause: failure, note: &fault.Note{Code: code, Message: message}}
	default:
		return &owned{message: "internal: " + message, cause: failure}
	}
}

// Join retains immutable diagnostics across cleanup aggregation. Unmarked
// siblings contribute only fallback text; every original cause remains in order.
// A single failure keeps its exact identity, including unmarked failures.
func Join(fallback string, failures ...error) error {
	var messages []string
	var only error
	known := false
	for _, failure := range failures {
		if failure == nil {
			continue
		}
		only = failure
		message, owned := OwnedMessage(failure)
		known = known || owned
		if !owned {
			message = fallback
		}
		messages = append(messages, message)
	}
	if len(messages) <= 1 {
		return only
	}
	joined := errors.Join(failures...)
	if known {
		return Owned(strings.Join(messages, "\n"), joined)
	}
	return joined
}

// Retain reattaches an already captured diagnostic after an owner adds outcome
// or cleanup causes. It neither inspects nor replaces that returned cause graph.
func Retain(original, result error) error {
	if message, ok := OwnedMessage(original); ok {
		retained := &owned{message: message, cause: result}
		source := snapshot(result)
		if source == nil {
			source = snapshot(original)
		}
		if source != nil {
			retained.note, retained.frames, retained.attributes = source.note, source.frames, source.attributes
		}
		return retained
	}
	return result
}

// Panic snapshots source locations at the recovery owner, never from an
// application-returned fault or recovered payload.
func Panic(message string, frames []fault.Frame) error {
	failure := fault.Panic(message, frames)
	return &owned{message: failure.Error(), cause: failure, note: &fault.Note{Code: fault.Panicked, Message: message}, frames: slices.Clone(frames)}
}

// FaultDiagnostic reads only an immutable construction snapshot. The cause may
// expose a mutable public fault for identity; it is not a diagnostic authority.
func FaultDiagnostic(err error) (fault.Note, []fault.Frame, bool) {
	value := snapshot(err)
	if value == nil || value.note == nil {
		return fault.Note{}, nil, false
	}
	return *value.note, slices.Clone(value.frames), true
}

// IsPanic recognizes owner-issued panic classification without error traversal.
func IsPanic(err error) bool {
	return HasCode(err, fault.Panicked)
}

// HasCode checks a construction snapshot without inspecting application methods.
func HasCode(err error, code fault.Code) bool {
	value := snapshot(err)
	return value != nil && value.note != nil && value.note.Code == code
}

// WrapFailure adds an operation classification and a fallback diagnostic while
// retaining a more specific diagnostic already captured by a framework owner.
func WrapFailure(code fault.Code, message string, cause error) error {
	return Retain(cause, WrapFault(code, message, cause))
}

// New creates an unclassified source diagnostic; it never adds a fault code.
func New(message string) error { return Owned(message, errors.New(message)) }

// sentinels contains only framework constants registered during package
// initialization, never application configuration or request-owned failures.
// The public sentinel remains *fault.Error for source compatibility and direct
// identity matching; its presentation snapshot cannot be changed through it.
var sentinels sync.Map // map[*fault.Error]*owned

// Sentinel constructs a framework sentinel with immutable diagnostic metadata.
// Use only for package-level constants, never for per-operation failures.
func Sentinel(code fault.Code, message string) *fault.Error {
	failure := fault.New(code, message)
	sentinels.Store(failure, &owned{message: failure.Error(), cause: failure, note: &fault.Note{Code: code, Message: message}})
	return failure
}

func snapshot(err error) *owned {
	if value, ok := err.(*owned); ok {
		return value
	}
	if failure, ok := err.(*fault.Error); ok && failure != nil {
		if value, ok := sentinels.Load(failure); ok {
			return value.(*owned)
		}
	}
	return nil
}

// DiagnosticID identifies one immutable construction snapshot. Transparent
// wrappers share it; distinct constructions keep distinct identities.
type DiagnosticID struct{ note *fault.Note }

func FaultDiagnosticID(err error) DiagnosticID {
	if value := snapshot(err); value != nil {
		return DiagnosticID{value.note}
	}
	return DiagnosticID{}
}
