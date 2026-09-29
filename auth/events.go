package auth

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// EventKind identifies an authentication lifecycle transition.
type EventKind string

const (
	// EventLogin: a full (not pending-MFA) session or token was issued.
	EventLogin EventKind = "login"
	// EventLogout: the current credential was revoked by its holder.
	EventLogout EventKind = "logout"
	// EventFailed: a password attempt was rejected. Subject is omitted when no
	// account matched, so the event never confirms submitted identifiers.
	EventFailed EventKind = "failed"
	// EventLockout: failed attempts started a temporary lockout.
	EventLockout EventKind = "lockout"
	// EventPasswordReset: a recovery link replaced the password.
	EventPasswordReset EventKind = "password_reset"
	// EventVerified: an email verification link was consumed.
	EventVerified EventKind = "verified"
	// EventOtherDevicesLoggedOut: every other credential was revoked while the
	// current one was kept. Count reports the provisional revoked total.
	EventOtherDevicesLoggedOut EventKind = "other_devices_logged_out"
	// EventImpersonationStarted: Impersonator began acting as Subject.
	EventImpersonationStarted EventKind = "impersonation_started"
	// EventImpersonationStopped: the impersonation session of Subject ended
	// and Impersonator returned to its own identity.
	EventImpersonationStopped EventKind = "impersonation_stopped"
)

// Event is safe lifecycle metadata. It never contains a password, secret,
// hash, submitted login key or model snapshot. Subject is the affected stored
// identity when known; Impersonator is the original actor for impersonation
// events and for events in an impersonated session; Request is the initiating
// request's trusted metadata.
type Event struct {
	Kind         EventKind
	Guard        GuardName
	Provider     ProviderName
	Subject      value.Optional[model.Identity]
	Impersonator value.Optional[model.Identity]
	Request      attribution.Request
	Count        uint64
}

func (Event) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("authentication event")) }

// Observer receives an in-process notification after the transition finished
// (after commit for persisted changes). It cannot veto or undo the change and
// is not durable delivery: for durable processing, publish an application event
// through the transactional outbox from the owning domain transaction instead.
// Observers must be fast, concurrency-safe and honor ctx.
type Observer func(context.Context, Event)

// Notify invokes observer, when present, with the request metadata from ctx.
// Panics and Goexit are contained; the observed operation's result is final.
func Notify(ctx context.Context, observer Observer, event Event) {
	if observer == nil || ctx == nil {
		return
	}
	event.Request = attribution.FromContext(ctx).Request()
	_ = callback.Isolated("authentication event observer", func() error { observer(ctx, event); return nil })
}
