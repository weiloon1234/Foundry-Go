package telemetry

import "context"

// The caller's context owns cancellation classification. A canceled database
// operation can report a broken connection without retaining a context cause.
// FlushTimeout still counts when the caller remains live. Transaction outcomes
// and pending receipt reconciliation are independent of failure accounting.
func storageInterrupted(ctx context.Context, err error) bool {
	return err != nil && ctx.Err() != nil
}
