package email

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

// Builder renders one deterministic message from a declared job DTO. Capture
// template inputs and pinned attachment references in that DTO. Never read
// mutable recipient state or current time when an identical retry is required.
type Builder[P any] func(context.Context, P) (Message, error)

// JobHandler plugs into Definition[P].Declare. Capture/Dispatch/Enqueue,
// attribution, serialization, versioning, retries and outbox routing remain the
// existing jobs API. Each execution reuses its stable ID as provider key.
// Ambiguous acceptance is terminal, including on idempotent providers: provider
// retention is finite and does not cover arbitrary worker redelivery intervals.
func JobHandler[P any](mailer *Mailer, build Builder[P]) jobs.Handler[P] {
	return func(ctx context.Context, data P) error {
		attempt, ok := jobs.Current(ctx)
		if !ok || mailer == nil || build == nil {
			return jobs.Permanent(Construction)
		}
		var message Message
		var buildFailure Kind
		failed := callback.Isolated("build queued email", func() error {
			var err error
			message, err = build(ctx, data)
			if err != nil {
				buildFailure = Classification(err)
				return Construction
			}
			return nil
		})
		if ctx.Err() != nil || buildFailure == Transient {
			return Transient
		}
		if failed != nil {
			return jobs.Permanent(Construction)
		}
		for _, a := range message.attachments {
			if a.Version == "" && a.IfMatch == "" {
				return jobs.Permanent(Construction)
			}
		}
		result, err := mailer.Send(ctx, message, SendOptions{IdempotencyKey: IdempotencyKey("email/" + attempt.ID.String())})
		if result.Accepted {
			jobs.PreventRetry(ctx)
		}
		if err != nil && Classification(err) != Transient {
			return jobs.Permanent(err)
		}
		return err
	}
}
