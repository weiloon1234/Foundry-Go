package routestyle

import "context"

// Check returns when analysis completes or the context expires. Go parsing and
// type checking cannot be interrupted; their worker may finish after this call.
func Check(ctx context.Context, dir string, patterns []string) ([]Diagnostic, error) {
	patterns = append([]string(nil), patterns...)
	return waitForCheck(ctx, func() ([]Diagnostic, error) { return check(ctx, dir, patterns) })
}

func waitForCheck(ctx context.Context, analyze func() ([]Diagnostic, error)) ([]Diagnostic, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	type outcome struct {
		diagnostics []Diagnostic
		err         error
	}
	completed := make(chan outcome, 1)
	go func() {
		diagnostics, err := analyze()
		completed <- outcome{diagnostics, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-completed:
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return result.diagnostics, result.err
	}
}
