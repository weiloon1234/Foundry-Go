package jobs

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/outboxstore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/internal/sqlscope"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
)

const jobMessageKind = "job"

// Workflows publish on the job route under a reserved message name.
const (
	workflowMessageName    = "foundry.workflow"
	workflowMessageVersion = 1
)

// Outbox snapshots jobs through business transactions. It never publishes on
// transaction callback return; the shared publisher only sees committed rows.
type Outbox struct {
	destination outbox.Destination
	dispatcher  *Dispatcher
	database    *database.DB
	schema      string
}

func PrepareOutbox(destination outbox.Destination, dispatcher *Dispatcher) (*Outbox, error) {
	if err := destination.Validate(); err != nil {
		return nil, err
	}
	if dispatcher == nil || dispatcher.slots == nil {
		return nil, fault.New(fault.Invalid, "job outbox requires a dispatcher")
	}
	return &Outbox{destination: destination, dispatcher: dispatcher}, nil
}

// PrepareOutboxIn borrows an exact pool and scopes writes to its migration schema.
// Enqueue still joins the caller's transaction; it never commits independently.
func PrepareOutboxIn(destination outbox.Destination, dispatcher *Dispatcher, db *database.DB, schema string) (*Outbox, error) {
	if db == nil || !sqlname.Valid(schema) {
		return nil, fault.New(fault.Invalid, "job outbox requires a database and schema")
	}
	producer, err := PrepareOutbox(destination, dispatcher)
	if err != nil {
		return nil, err
	}
	producer.database = db
	producer.schema = schema
	return producer, nil
}
func (o *Outbox) Destination() outbox.Destination {
	if o == nil {
		return ""
	}
	return o.destination
}
func (d Definition[P]) Enqueue(ctx context.Context, tx *database.Tx, producer *Outbox, input P, options Options[P]) (outbox.ID[P], error) {
	pending, err := d.Capture(ctx, input, options)
	if err != nil {
		return outbox.ID[P]{}, err
	}
	return pending.Enqueue(ctx, tx, producer)
}

// Enqueue preserves this Pending's stable execution ID across publication gaps
// and intentional transaction retries. The returned outbox ID is not a receipt
// for commit or execution. Duplicate publication is bounded by queue retention.
func (p Pending[P]) Enqueue(ctx context.Context, tx *database.Tx, producer *Outbox) (outbox.ID[P], error) {
	if producer == nil || tx == nil {
		return outbox.ID[P]{}, fault.New(fault.Invalid, "job enqueue requires an outbox and transaction")
	}
	release, err := producer.dispatcher.begin(ctx)
	if err != nil {
		return outbox.ID[P]{}, err
	}
	defer release()
	if err := p.definition.check(producer.dispatcher.registry); err != nil {
		return outbox.ID[P]{}, err
	}
	data, err := p.envelope.MarshalJSON()
	if err != nil {
		return outbox.ID[P]{}, err
	}
	var row outboxstore.Message
	appendRow := func(tx *database.Tx) error {
		var err error
		row, err = outboxstore.Append(ctx, tx, outboxstore.Address{Kind: jobMessageKind, Destination: producer.destination, Name: string(p.envelope.Name()), Version: uint32(p.envelope.Version())}, string(data), p.envelope.Origin())
		return err
	}
	if producer.database != nil {
		err = sqlscope.InSchema(ctx, tx, producer.database, producer.schema, appendRow)
	} else {
		err = appendRow(tx)
	}
	if err != nil {
		return outbox.ID[P]{}, err
	}
	return outbox.IDFromBytes[P](row.ID.Bytes()), nil
}

// Enqueue snapshots the whole workflow into the business transaction. After
// commit the shared publisher submits it as one atomic group through the job
// route; rollback suppresses it. Every member must be registered with the
// producer's dispatcher. The stable workflow and member IDs deduplicate
// repeated publication, bounded by queue retention.
func (w Workflow) Enqueue(ctx context.Context, tx *database.Tx, producer *Outbox) (outbox.ID[WorkflowExecution], error) {
	if producer == nil || tx == nil {
		return outbox.ID[WorkflowExecution]{}, fault.New(fault.Invalid, "workflow enqueue requires an outbox and transaction")
	}
	release, err := producer.dispatcher.begin(ctx)
	if err != nil {
		return outbox.ID[WorkflowExecution]{}, err
	}
	defer release()
	if err := w.check(producer.dispatcher.registry); err != nil {
		return outbox.ID[WorkflowExecution]{}, err
	}
	data, err := w.envelope.MarshalJSON()
	if err != nil {
		return outbox.ID[WorkflowExecution]{}, err
	}
	var row outboxstore.Message
	appendRow := func(tx *database.Tx) error {
		var err error
		row, err = outboxstore.Append(ctx, tx, outboxstore.Address{Kind: jobMessageKind, Destination: producer.destination, Name: workflowMessageName, Version: workflowMessageVersion}, string(data), w.envelope.steps[0].Origin())
		return err
	}
	if producer.database != nil {
		err = sqlscope.InSchema(ctx, tx, producer.database, producer.schema, appendRow)
	} else {
		err = appendRow(tx)
	}
	if err != nil {
		return outbox.ID[WorkflowExecution]{}, err
	}
	return outbox.IDFromBytes[WorkflowExecution](row.ID.Bytes()), nil
}

// DurableBackend declares durable acceptance under the backend's configured
// persistence/failover policy. A process-local queue cannot publish an outbox.
type DurableBackend interface {
	Backend
	DurableAcceptance() bool
}

func (d *Dispatcher) RequireDurable() error {
	if d == nil || d.slots == nil {
		return fault.New(fault.Invalid, "job dispatcher is not initialized")
	}
	authority, ok := d.backend.(DurableBackend)
	if !ok || !authority.DurableAcceptance() {
		return fault.New(fault.Invalid, "outbox publication requires a durable job backend")
	}
	return nil
}
func (o *Outbox) PublicationRoute() (publisher.Route, error) {
	if o == nil {
		return publisher.Route{}, fault.New(fault.Invalid, "job outbox is not initialized")
	}
	if err := o.dispatcher.RequireDurable(); err != nil {
		return publisher.Route{}, err
	}
	return publisher.Route{Kind: jobMessageKind, Destination: o.destination, Publish: func(ctx context.Context, message publisher.Message) error {
		release, err := o.dispatcher.begin(ctx)
		if err != nil {
			return err
		}
		defer release()
		data, err := message.PayloadJSON()
		if err != nil {
			return err
		}
		if message.Name() == workflowMessageName && message.Version() == workflowMessageVersion && message.Destination() == o.destination {
			return o.publishWorkflow(ctx, data)
		}
		envelope, err := DecodeEnvelope([]byte(data))
		if err != nil {
			return err
		}
		if string(envelope.Name()) != message.Name() || uint32(envelope.Version()) != message.Version() || message.Destination() != o.destination {
			return fault.New(fault.Invalid, "outbox job address differs from its payload")
		}
		if _, err := o.dispatcher.registry.lookup(jobKey{envelope.Name(), envelope.Version()}); err != nil {
			return err
		}
		key, err := NewKey(o.dispatcher.config.Namespace, envelope.Queue())
		if err != nil {
			return err
		}
		_, err = o.dispatcher.backend.JobEnqueue(ctx, key, envelope)
		return err
	}}, nil
}

// publishWorkflow submits a committed workflow row. A member name/version this
// process does not register is fault.Missing, which the publisher retries.
func (o *Outbox) publishWorkflow(ctx context.Context, data string) error {
	workflow, err := DecodeWorkflow([]byte(data))
	if err != nil {
		return err
	}
	for _, member := range workflow.Members() {
		if _, err := o.dispatcher.registry.lookup(jobKey{member.Name(), member.Version()}); err != nil {
			return err
		}
	}
	key, err := NewKey(o.dispatcher.config.Namespace, workflow.Queue())
	if err != nil {
		return err
	}
	_, err = o.dispatcher.backend.JobWorkflow(ctx, key, workflow)
	return err
}
