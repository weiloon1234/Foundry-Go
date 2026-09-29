package validation

import (
	"context"
	"strings"

	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/i18n/message"
)

// Rejection is a message chosen at check time. The zero value accepts input.
// Messages are public text written by the application; never include secrets
// or unapproved submitted values.
type Rejection struct {
	rejected bool
	text     string
	bind     func(context.Context, string) (i18n.PreparedMessage, error)
}

// Reject rejects input with literal public text. Empty text uses the rule's
// declared message (including its translation).
func Reject(text string) Rejection { return Rejection{rejected: true, text: text} }

// RejectWith rejects input with a generated typed message whose arguments are
// computed at check time, such as a remaining quota. It renders in the request
// locale like built-in messages; the rule's declared message is the English
// fallback. Arguments are bound only when the rejection is recorded.
func RejectWith[A any](translated message.Message[A], args A) Rejection {
	return Rejection{rejected: true, bind: func(ctx context.Context, fallback string) (i18n.PreparedMessage, error) {
		return translated.BindLiteral(ctx, args, fallback)
	}}
}

// Dynamic defines a server rule whose rejection message is decided at check
// time. Return the zero Rejection to accept and an error when execution fails.
// Spec supplies the stable code and declared message; built-in IDs are reserved.
func Dynamic[T any](spec Spec, check func(context.Context, T) (Rejection, error)) Rule[T] {
	if strings.HasPrefix(string(spec.ID), "foundry.") {
		return failed[T](invalid("foundry validation IDs are reserved"))
	}
	if check == nil {
		return failed[T](invalid("validation callback is missing"))
	}
	rule := applicationReport(spec, func(s *execution, input T, spec Spec, declared *i18n.PreparedMessage) error {
		rejection, err := check(s.ctx, input)
		if err != nil || !rejection.rejected {
			return err
		}
		return s.reject(spec, declared, rejection)
	})
	return rule
}

// Report records Hook issues relative to the hook's input path. It is valid only
// during the hook call and is not safe for concurrent use.
type Report struct {
	state    *execution
	spec     Spec
	declared *i18n.PreparedMessage
	err      error
	closed   bool
}

// Add rejects a location with the hook's declared message. Each path segment is
// one unescaped JSON object name or array index, relative to the hook's input.
// No segments address the input itself. Adds past the issue cap are dropped and
// mark the result truncated.
func (r *Report) Add(path ...string) { r.AddRejection(Reject(""), path...) }

// AddRejection records a location with a check-time message.
func (r *Report) AddRejection(rejection Rejection, path ...string) {
	if r == nil || r.closed || r.err != nil || !rejection.rejected {
		return
	}
	s := r.state
	if s.err != nil || s.truncated {
		return
	}
	if len(s.issues) >= s.limits.Issues {
		s.truncated = true
		return
	}
	for _, name := range path {
		if !validText(name, true) {
			r.err = invalid("validation report path is invalid")
			return
		}
	}
	if !s.take(0) {
		return
	}
	previous, previousKey, previousField := s.label, s.labelKey, s.field
	if len(path) != 0 {
		// The addressed wire name is the message attribute; labels belong to
		// declared fields, which this location need not be.
		s.label, s.labelKey, s.field = "", "", path[len(path)-1]
	}
	for _, name := range path {
		s.enter(name)
	}
	r.err = s.reject(r.spec, r.declared, rejection)
	s.segments = s.segments[:len(s.segments)-len(path)]
	s.label, s.labelKey, s.field = previous, previousKey, previousField
}

// Hook runs an application callback that may add issues at arbitrary paths below
// its input, for checks spanning several fields or collection elements. Place it
// after field rules in Bail when it needs them to pass first. Returning an error
// means execution failed, not invalid input. Spec supplies the issue code and
// declared message; built-in IDs are reserved.
func Hook[T any](spec Spec, hook func(context.Context, T, *Report) error) Rule[T] {
	if strings.HasPrefix(string(spec.ID), "foundry.") {
		return failed[T](invalid("foundry validation IDs are reserved"))
	}
	if hook == nil {
		return failed[T](invalid("validation callback is missing"))
	}
	return applicationReport(spec, func(s *execution, input T, spec Spec, declared *i18n.PreparedMessage) error {
		report := &Report{state: s, spec: spec, declared: declared}
		err := hook(s.ctx, input, report)
		report.closed = true
		if err != nil {
			return err
		}
		return report.err
	})
}

func applicationReport[T any](spec Spec, report func(*execution, T, Spec, *i18n.PreparedMessage) error) Rule[T] {
	rule := valueRule(spec, true, func(*execution, T) (bool, error) { return true, nil })
	if rule.err != nil {
		return rule
	}
	rule.definitions = []*definition{{id: spec.ID}}
	rule.leaf, rule.report, rule.callbacks = nil, report, true
	return rule
}

// reject records one issue at the current path with a check-time message.
func (s *execution) reject(spec Spec, declared *i18n.PreparedMessage, rejection Rejection) error {
	switch {
	case rejection.bind != nil:
		prepared, err := rejection.bind(s.ctx, spec.Message)
		if err != nil {
			return err
		}
		if err := validateLabelParameters(prepared.Description().Definition); err != nil {
			return err
		}
		s.issue(spec, &prepared)
	case rejection.text != "":
		if !validText(rejection.text, false) {
			return invalid("dynamic validation message is invalid")
		}
		spec.Message = rejection.text
		s.issue(spec, nil)
	default:
		s.issue(spec, declared)
	}
	return nil
}
