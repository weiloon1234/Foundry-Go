package tooling

import (
	"context"
	"log/slog"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/logging"
)

// doc-example:begin logging-2
type tenantLogIdentity struct{ ID string }

func logTenant(ctx context.Context, logger *slog.Logger, tenant tenantLogIdentity) error {
	ctx = logging.WithAttrs(ctx, slog.String("tenant", string(tenant.ID)), slog.String("route", "orders.show"))
	logger.InfoContext(ctx, "order loaded") // {"msg":"order loaded","context":{"tenant":"t1","route":"orders.show"}}
	return nil
}

// doc-example:end logging-2

// doc-example:begin logging-3
// InvoiceID is the application invoice key carried by its job payload.
type InvoiceID string

func invoiceLog(ctx context.Context, logger *slog.Logger, id InvoiceID) error {
	type SendInvoice struct {
		InvoiceID InvoiceID             `json:"invoice_id"`
		Request   attribution.RequestID `json:"request_id,omitempty"`
		Log       logging.ContextFields `json:"log,omitzero"`
	}

	payload := SendInvoice{InvoiceID: id, Request: attribution.FromContext(ctx).Request().ID, Log: logging.FieldsFromContext(ctx)}

	// In the job handler:
	ctx = payload.Log.Context(ctx)
	logger.InfoContext(ctx, "sending invoice", slog.String("origin_request_id", string(payload.Request)))
	return nil
}

// doc-example:end logging-3

// doc-example:begin vendor-log-channel
func prepareVendorChannels(handler slog.Handler) (*logging.ChannelSet, error) {
	settings := map[logging.ChannelName]logging.ChannelSettings{
		"vendor": {Sink: logging.SinkConfig{Driver: logging.Custom, Level: slog.LevelWarn}},
	}
	return logging.PrepareChannels("vendor", settings, nil,
		logging.WithHandler("vendor", handler))
}

// doc-example:end vendor-log-channel
