package security

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/webhook/outbound"
)

// doc-example:begin invoice-event
// InvoicePaidPayload contains the merchant's public invoice reference.
//
//foundry:dto
type InvoicePaidPayload struct {
	Invoice string `json:"invoice"`
}

// InvoicePaid binds the public event name to its generated payload contract.
var InvoicePaid = outbound.DefineEvent("invoice.paid", InvoicePaidPayloadJSON())
var deliverWebhooks = outbound.DefineDeliveryJob("webhooks.deliver", jobs.DefaultPolicy("webhooks"))

// doc-example:end invoice-event

func runWebhookWorker(ctx context.Context, db *database.DB, keyring *encryption.Keyring, restrictedClient *httpclient.Client, backend jobs.Backend, workerConfig jobs.WorkerConfig) error {
	// doc-example:begin webhooks-service
	service, err := outbound.New(outbound.Dependencies{DB: db, Keys: keyring, Client: restrictedClient, Clock: clock.System{}}, outbound.DefaultConfig())
	if err != nil {
		return err
	}
	defer service.Close(context.Background())
	declaration, err := deliverWebhooks.Declare(service) // register with the job registry
	if err != nil {
		return err
	}
	sink, err := deliverWebhooks.FailureSink(service) // register with every worker running it
	if err != nil {
		return err
	}
	// doc-example:end webhooks-service
	registry, err := jobs.NewRegistry(declaration)
	if err != nil {
		return err
	}
	worker, err := jobs.NewWorker(backend, registry, workerConfig, jobs.WithFailureSink(sink))
	if err != nil {
		return err
	}
	return worker.Run(ctx)
}

func invoiceDeliveryQueue(service *outbound.Service, producer *jobs.Outbox) (outbound.Queue, error) {
	// doc-example:begin webhooks-queue
	return deliverWebhooks.ToOutbox(service, producer)
	// doc-example:end webhooks-queue
}

func subscribeInvoices(ctx context.Context, service *outbound.Service, customerURL string) (outbound.EndpointID, secret.String, error) {
	// doc-example:begin webhooks-endpoint
	return service.CreateEndpoint(ctx, outbound.EndpointOptions{URL: customerURL, Events: []outbound.EventType{"invoice.paid"}})
	// doc-example:end webhooks-endpoint
}

func sendInvoice(ctx context.Context, tx *database.Tx, queue outbound.Queue, endpoint outbound.EndpointID, payload InvoicePaidPayload) (outbound.DeliveryID, error) {
	// doc-example:begin webhooks-send
	return outbound.Send(ctx, tx, queue, endpoint, InvoicePaid, payload)
	// doc-example:end webhooks-send
}

func publishInvoice(ctx context.Context, tx *database.Tx, queue outbound.Queue, payload InvoicePaidPayload) ([]outbound.DeliveryID, error) {
	// doc-example:begin webhooks-publish
	return outbound.Publish(ctx, tx, queue, InvoicePaid, payload)
	// doc-example:end webhooks-publish
}
