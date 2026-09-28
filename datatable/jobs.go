package datatable

import (
	"context"
	"errors"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/jobs"
)

// ExportHandler adapts a table to an ordinary typed job definition. Resolve
// reconstructs CURRENT server authority from its persisted payload; it must not
// trust captured role/permission claims or turn a client model ID into authority.
// The handler always runs the table's Export authorization and row scope again.
// Reuse auth.Provider.Resolve for fresh eligibility, then apply current policy.
//
// Deliver receives only a complete artifact, after its database snapshot closes.
// The handler owns and closes it even when delivery fails, panics or Goexits.
// Its context retains job values and the export's deadline/ownership frame.
// Jobs remain at-least-once: use jobs.Current's execution ID for an idempotent
// destination. Locale/timezone/actor references belong in P's existing job
// contract; this adapter creates neither another queue nor implicit email sends.
func ExportHandler[P, S, R, A any](table Table[S, R, A], manager *Manager, resolve func(context.Context, P) (A, Request, ExportOptions, error), deliver func(context.Context, P, *Artifact) error) (jobs.Handler[P], error) {
	if err := table.check(manager); err != nil {
		return nil, err
	}
	if !table.definition.spec.Exports || resolve == nil || deliver == nil {
		return nil, invalid("export job requires an enabled table, authority resolver and delivery callback")
	}
	return func(ctx context.Context, payload P) error {
		return callback.Isolated("datatable export job", func() (err error) {
			if ctx == nil {
				return invalid("export job requires a context")
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			subject, request, options, err := resolve(ctx, payload)
			if err != nil {
				return err
			}
			artifact, err := table.Export(ctx, manager, subject, request, options)
			if err != nil {
				return err
			}
			defer func() { err = errors.Join(err, artifact.Close()) }()
			// Isolation lets the ownership defer run on normal return after Goexit too.
			deliveryContext := artifact.lease.Context()
			return callback.Isolated("deliver datatable export", func() error {
				return errors.Join(deliver(deliveryContext, payload, artifact), deliveryContext.Err())
			})
		})
	}, nil
}
