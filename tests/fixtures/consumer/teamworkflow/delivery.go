package teamworkflow

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/events"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
)

var Submitted = events.Define[SubmissionView]("workflow.submitted", 1)

// DeliverySink borrows the explicitly named receiver pool. Its unique receipt
// and local receiving effect share a real transaction. This cannot make an
// external provider call exactly once; such providers need their own protocol.
type DeliverySink struct{ db *database.DB }

func NewDeliverySink(db *database.DB) *DeliverySink { return &DeliverySink{db: db} }
func (s *DeliverySink) Deliver(ctx context.Context, message publisher.Message) error {
	payload, err := message.PayloadJSON()
	if err != nil {
		return err
	}
	return s.db.Transaction(ctx, func(tx *database.Tx) error {
		inserted, err := tx.Exec(ctx, `INSERT INTO workflow_delivery_receipts(id,payload) VALUES($1,$2::jsonb) ON CONFLICT(id) DO NOTHING`, message.ID(), payload)
		if err != nil || inserted.RowsAffected == 0 {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE workflow_delivery_totals SET total=total+1 WHERE name='submissions'`)
		return err
	})
}
func (s *DeliverySink) Route(observe func(publisher.Message)) publisher.Route {
	return publisher.Route{Kind: "event", Destination: "workflow", Publish: func(ctx context.Context, message publisher.Message) error {
		if err := s.Deliver(ctx, message); err != nil {
			return err
		}
		if observe != nil {
			observe(message)
		}
		return nil
	}}
}
