package attachments

import "context"

// Admission class comes from the registered policy, including small inputs to a
// large policy. Acquire its sublimit before shared capacity or opening a source.
func (m *Manager) runUpload(ctx context.Context, policy Policy, name string, fn func(context.Context) error) error {
	if m.largeCalls != nil && (policy.MaxBytes > MaxBufferedBytes || policy.MaxStoredBytes > MaxBufferedBytes) {
		return m.largeCalls.Run(ctx, name, func(ctx context.Context) error {
			return m.calls.Run(ctx, name, fn)
		})
	}
	return m.calls.Run(ctx, name, fn)
}
