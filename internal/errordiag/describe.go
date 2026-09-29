// Package errordiag builds redacted failure diagnostics for logs and reporters.
// It records dynamic type names, framework fault notes and framework-owned
// attributes; it never calls Error or formats application errors or payloads.
package errordiag

import (
	"reflect"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
)

const (
	maxTypes      = 16
	maxFaults     = 8
	maxAttributes = 16
)

// Seal can only be named inside this module, so Provider is implementable only
// by framework-owned error types. Describing an error therefore never runs an
// application's diagnostic method.
type Seal struct{ _ struct{} }

// Provider contributes framework-owned, redaction-safe attributes.
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
	failed := callback.Isolated("error diagnostic", func() error {
		complete := errorgraph.Walk(err, func(current error) bool {
			record(&diagnostic, current)
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

func record(d *fault.Diagnostic, err error) {
	if len(d.Types) < maxTypes {
		d.Types = append(d.Types, reflect.TypeOf(err).String())
	} else {
		d.Truncated = true
	}
	switch current := err.(type) {
	case *fault.Error:
		if current == nil {
			return
		}
		if len(d.Faults) < maxFaults {
			d.Faults = append(d.Faults, fault.Note{Code: current.Code(), Message: current.Message()})
		} else {
			d.Truncated = true
		}
		if len(d.Frames) == 0 {
			d.Frames = current.Frames()
		}
	case fault.Code:
		if len(d.Faults) < maxFaults {
			d.Faults = append(d.Faults, fault.Note{Code: current, Message: string(current)})
		} else {
			d.Truncated = true
		}
	case Provider:
		for _, attribute := range current.FoundryDiagnostic(Seal{}) {
			if len(d.Attributes) == maxAttributes {
				d.Truncated = true
				break
			}
			d.Attributes = append(d.Attributes, attribute)
		}
	}
}
