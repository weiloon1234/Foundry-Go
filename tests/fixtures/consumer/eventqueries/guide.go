package eventqueries

import (
	"context"

	"foundry.test/consumer/observerqueries"
	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin dispatch-event
func DispatchCreated(ctx context.Context, bus *events.Bus, id model.ID[observerqueries.Plain]) error {
	return Created.Dispatch(ctx, bus, RecordCreated{ID: id})
}

// doc-example:end dispatch-event

// doc-example:begin configured-event-producer
type PublicationService struct{ producer *events.Outbox }

func NewPublicationService(services application.Services) (*PublicationService, error) {
	producer, err := application.Resolve(services, Publications)
	if err != nil {
		return nil, err
	}
	return &PublicationService{producer: producer}, nil
}

// doc-example:end configured-event-producer

// doc-example:begin enqueue-event
func (s *PublicationService) Enqueue(ctx context.Context, tx *database.Tx, id model.ID[observerqueries.Plain]) (outbox.ID[RecordCreated], error) {
	return Created.Enqueue(ctx, tx, s.producer, RecordCreated{ID: id})
}

// doc-example:end enqueue-event

// doc-example:begin reload-event
func (s *PublicationService) Payload(ctx context.Context, db database.Executor, id outbox.ID[RecordCreated]) (value.Optional[RecordCreated], error) {
	result, err := Created.Find(ctx, db, s.producer, id)
	if err != nil {
		return value.Optional[RecordCreated]{}, err
	}
	stored, present := result.Get()
	if !present {
		return value.Optional[RecordCreated]{}, nil
	}
	payload, err := stored.Payload()
	if err != nil {
		return value.Optional[RecordCreated]{}, err
	}
	return value.Set(payload), nil
}

// doc-example:end reload-event
