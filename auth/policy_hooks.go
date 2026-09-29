package auth

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

// Verdict is a hook decision. Abstain leaves the declared policy in charge.
type Verdict uint8

const (
	Abstain Verdict = iota
	Allow
	Deny
)

func (v Verdict) valid() bool { return v == Abstain || v == Allow || v == Deny }

// Before runs ahead of every policy and permission evaluated for model M in a
// registry that contains it, for example to let a verified super-administrator
// pass checks or to deny a suspended tenant everywhere. Hooks run in
// registration order and the first Allow or Deny decides; the policy callback
// then does not run. A hook receives the current request's model and the
// policy name, never the resource. Errors deny; they never grant.
type Before[M any] struct{ definition *hookDefinition[M] }

// After runs once a policy or permission for model M allowed a request. It may
// veto with Deny (for example a global read-only mode) but can never turn a
// denial into an allow. Allow and Abstain keep the policy's decision.
type After[M any] struct{ definition *hookDefinition[M] }

type hookPhase uint8

const (
	beforePhase hookPhase = iota + 1
	afterPhase
)

type hookDefinition[M any] struct {
	id     *declarationID
	name   PolicyName
	phase  hookPhase
	decide func(context.Context, M, PolicyName) (Verdict, error)
}

// beforeHook is the registry's erased view. decide holds the declaring model's
// func(context.Context, M, PolicyName) (Verdict, error); policies select hooks
// by exact model type, so a hook for another model never runs.
type beforeHook struct {
	id     *declarationID
	phase  hookPhase
	decide any
}

func DefineBefore[M any](name PolicyName, decide func(context.Context, M, PolicyName) (Verdict, error)) Before[M] {
	return Before[M]{definition: &hookDefinition[M]{id: &declarationID{}, name: name, phase: beforePhase, decide: decide}}
}

// DefineAfter declares a veto-only hook. See After for its restricted contract.
func DefineAfter[M any](name PolicyName, veto func(context.Context, M, PolicyName) (Verdict, error)) After[M] {
	return After[M]{definition: &hookDefinition[M]{id: &declarationID{}, name: name, phase: afterPhase, decide: veto}}
}

func (d *hookDefinition[M]) validate() error {
	if d == nil || !identifier.Semantic(string(d.name)) || d.decide == nil {
		return fault.New(fault.Invalid, "authorization hook requires a semantic name and callback")
	}
	return nil
}
func (d *hookDefinition[M]) registration() Registration {
	if d == nil {
		return Registration{}
	}
	return Registration{kind: hookRegistration, id: d.id, name: string(d.name), validate: d.validate, hook: beforeHook{id: d.id, phase: d.phase, decide: d.decide}}
}

func (b Before[M]) Validate() error            { return b.definition.validate() }
func (b Before[M]) Registration() Registration { return b.definition.registration() }
func (b Before[M]) Name() PolicyName {
	if b.definition == nil {
		return ""
	}
	return b.definition.name
}
func (a After[M]) Validate() error            { return a.definition.validate() }
func (a After[M]) Registration() Registration { return a.definition.registration() }
func (a After[M]) Name() PolicyName {
	if a.definition == nil {
		return ""
	}
	return a.definition.name
}

// hooks evaluates registered hooks for M. Before returns a decisive verdict or
// Abstain; after returns Deny or Abstain. Each hook uses the registry's bounded,
// isolated callback execution and the same recursion protection as policies.
func runHooks[M any](ctx context.Context, scope *Scope, phase hookPhase, subject M, policy PolicyName) (Verdict, error) {
	for _, hook := range scope.registry.hooks {
		if hook.phase != phase {
			continue
		}
		decide, typed := hook.decide.(func(context.Context, M, PolicyName) (Verdict, error))
		if !typed {
			continue
		}
		var verdict Verdict
		err := scope.run(ctx, hook.id, func(op context.Context) error {
			var err error
			verdict, err = decide(op, subject, policy)
			return err
		})
		if err != nil {
			return Abstain, err
		}
		if !verdict.valid() {
			return Abstain, fault.New(fault.Invalid, "authorization hook returned an invalid verdict")
		}
		if phase == afterPhase && verdict == Allow {
			verdict = Abstain
		}
		if verdict != Abstain {
			return verdict, nil
		}
	}
	return Abstain, nil
}

// DenialCode is an application-owned reason for a denied authorization. It is
// a stable semantic identifier such as orders.closed, not a translated message.
type DenialCode string

// Denial is a typed Forbidden decision carrying a stable code and a safe public
// message. Return it from a policy callback (false, auth.NewDenial(...)) or a hook
// error to explain a denial; errors.Is(err, auth.Forbidden) remains true. To
// publish the code over HTTP, wrap it in a declared endpoint error.
type Denial struct {
	code    DenialCode
	message string
}

// NewDenial returns a typed denial. The message must be safe for the caller to see.
func NewDenial(code DenialCode, message string) error {
	return &Denial{code: code, message: message}
}
func (d *Denial) Code() DenialCode {
	if d == nil {
		return ""
	}
	return d.code
}
func (d *Denial) Message() string {
	if d == nil {
		return ""
	}
	return d.message
}
func (d *Denial) Error() string        { return Forbidden.Error() }
func (d *Denial) Is(target error) bool { return target == Forbidden }

// Decision is an inspected authorization outcome. A denied decision may carry
// the typed Denial returned by a policy or hook; Allowed never carries one.
type Decision struct {
	allowed bool
	denial  *Denial
}

func (d Decision) Allowed() bool { return d.allowed }

// Denial returns the typed reason when the policy supplied one.
func (d Decision) Denial() (*Denial, bool) { return d.denial, d.denial != nil }

// Err returns nil when allowed, the typed denial when present, or Forbidden.
func (d Decision) Err() error {
	if d.allowed {
		return nil
	}
	if d.denial != nil {
		return d.denial
	}
	return Forbidden
}
