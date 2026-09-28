// Package log simulates acceptance while logging only bounded message metadata.
// It does not deliver mail and never logs recipients, bodies, subjects or keys.
package log

import (
	"context"
	"log/slog"

	"github.com/weiloon1234/Foundry-Go/email"
)

type Driver struct{ logger *slog.Logger }

func New(logger *slog.Logger) (*Driver, error) {
	if logger == nil {
		return nil, email.Construction
	}
	return &Driver{logger: logger}, nil
}
func (d *Driver) Send(ctx context.Context, out email.Outbound) (email.Receipt, error) {
	if d == nil || d.logger == nil || ctx == nil || out.Validate() != nil {
		return email.Receipt{}, email.Construction
	}
	if ctx.Err() != nil {
		return email.Receipt{}, email.Transient
	}
	d.logger.InfoContext(ctx, "email acceptance simulated", "recipients", out.Message().RecipientCount(), "bytes", out.Size(), "attachments", len(out.Attachments()))
	return email.Receipt{}, nil
}
