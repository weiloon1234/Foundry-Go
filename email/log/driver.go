// Package log simulates acceptance while logging only bounded message metadata.
// It does not deliver mail and never logs recipients, bodies, subjects or keys.
package log

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/internal/emaildiag"
	"log/slog"

	"github.com/weiloon1234/Foundry-Go/email"
)

// Driver simulates email acceptance by logging message counts and size.
type Driver struct{ logger *slog.Logger }

// New creates a logging driver, rejecting a nil logger.
func New(logger *slog.Logger) (*Driver, error) {
	if logger == nil {
		return nil, emaildiag.Failure(email.Construction)
	}
	return &Driver{logger: logger}, nil
}

// Send logs counts and size to simulate acceptance without delivery.
func (d *Driver) Send(ctx context.Context, out email.Outbound) (email.Receipt, error) {
	if d == nil || d.logger == nil || ctx == nil || out.Validate() != nil {
		return email.Receipt{}, emaildiag.Failure(email.Construction)
	}
	if ctx.Err() != nil {
		return email.Receipt{}, emaildiag.Failure(email.Transient)
	}
	d.logger.InfoContext(ctx, "email acceptance simulated", "recipients", out.Message().RecipientCount(), "bytes", out.Size(), "attachments", len(out.Attachments()))
	return email.Receipt{}, nil
}
