// Package errordiag builds redacted failure diagnostics for logs and reporters.
// It records dynamic type names, framework fault notes and framework-owned
// attributes; it never calls Error or formats application errors or payloads.
package errordiag

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/internal/frameworkadapter"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

const (
	maxTypes      = 16
	maxFaults     = 8
	maxAttributes = 16
)

// Seal authorizes framework diagnostic attributes. Its zero value is invalid.
type Seal struct{ diagnostic *sentinel }

type sentinel struct{ live byte }

var diagnosticCapability = sentinel{live: 1}

func grant() Seal { return Seal{diagnostic: &diagnosticCapability} }

// Valid reports whether the capability was issued by this package.
func (s Seal) Valid() bool { return s.diagnostic == &diagnosticCapability }

// Provider contributes redaction-safe attributes only after concrete framework
// ownership is established. The method signature alone does not prove ownership.
type Provider interface {
	FoundryDiagnostic(Seal) []fault.Attribute
}

// Describe summarizes err within errorgraph's bounds. Custom Unwrap/As methods
// run in an isolated callback; a failing method yields a truncated summary.
func Describe(err error) fault.Diagnostic {
	if err == nil {
		return fault.Diagnostic{}
	}
	var diagnostic fault.Diagnostic
	seen := make(map[safeerror.DiagnosticID]bool)
	attributes := make(map[error]bool)
	failed := callback.Isolated("error diagnostic", func() error {
		complete := errorgraph.Walk(err, func(current error) bool {
			record(&diagnostic, current, seen, attributes)
			return true
		})
		if !complete {
			diagnostic.Truncated = true
		}
		return nil
	})
	if failed != nil {
		diagnostic.Truncated = true
	}
	return diagnostic
}

func record(d *fault.Diagnostic, err error, seen map[safeerror.DiagnosticID]bool, attributes map[error]bool) {
	if len(d.Types) < maxTypes {
		d.Types = append(d.Types, reflect.TypeOf(err).String())
	} else {
		d.Truncated = true
	}
	if note, frames, ok := safeerror.FaultDiagnostic(err); ok && !seen[safeerror.FaultDiagnosticID(err)] {
		seen[safeerror.FaultDiagnosticID(err)] = true
		if len(d.Faults) < maxFaults {
			d.Faults = append(d.Faults, note)
		} else {
			d.Truncated = true
		}
		if len(d.Frames) == 0 {
			d.Frames = frames
		}
	}
	if source := safeerror.AttributeSource(err); source != nil && !attributes[source] {
		attributes[source] = true
		for _, attribute := range safeerror.Attributes(err) {
			recordAttribute(d, attribute)
		}
	}
	switch current := err.(type) {
	case fault.Code:
		// Code is public and string-backed. Unknown application values are private.
		switch current {
		case fault.Invalid, fault.Duplicate, fault.Missing, fault.Cycle, fault.Closed, fault.Conflict, fault.Internal, fault.Panicked, fault.Timeout, fault.Overloaded:
			if len(d.Faults) < maxFaults {
				d.Faults = append(d.Faults, fault.Note{Code: current, Message: string(current)})
			} else {
				d.Truncated = true
			}
		}
	case Provider:
		if !frameworkadapter.IsFrameworkType(current) {
			return
		}
		if reflect.TypeOf(current).Comparable() {
			if attributes[err] {
				return
			}
			attributes[err] = true
		}
		for _, attribute := range current.FoundryDiagnostic(grant()) {
			recordAttribute(d, attribute)
		}
	}
}

func recordAttribute(d *fault.Diagnostic, attribute fault.Attribute) {
	if len(d.Attributes) == maxAttributes {
		d.Truncated = true
		return
	}
	d.Attributes = append(d.Attributes, attribute)
}
