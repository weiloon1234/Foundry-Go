package jobs

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Connection pairs an existing dispatcher with its ordinary routing default.
// It borrows the dispatcher; no additional backend or kernel is created.
type Connection struct {
	dispatcher *Dispatcher
	queue      Queue
}

func NewConnection(dispatcher *Dispatcher, queue Queue) (*Connection, error) {
	if dispatcher == nil || dispatcher.slots == nil {
		return nil, fault.New(fault.Invalid, "job connection requires a dispatcher")
	}
	if err := queue.Validate(); err != nil {
		return nil, err
	}
	return &Connection{dispatcher, queue}, nil
}
func (c *Connection) DefaultQueue() Queue {
	if c == nil {
		return ""
	}
	return c.queue
}

// Dispatcher is the explicit advanced path retaining a definition's own queue.
func (c *Connection) Dispatcher() *Dispatcher {
	if c == nil {
		return nil
	}
	return c.dispatcher
}
func (c *Connection) Outbox(destination outbox.Destination) (*Outbox, error) {
	return PrepareOutbox(destination, c.Dispatcher())
}

// Bound retains the job payload type and one configured connection. On explicitly
// chooses connection routing: empty Options.Queue selects its default; an explicit
// queue wins. The underlying definition and advanced Dispatch behavior are unchanged.
type Bound[P any] struct {
	definition Definition[P]
	connection *Connection
}

func (d Definition[P]) On(connection *Connection) (Bound[P], error) {
	if err := d.Validate(); err != nil {
		return Bound[P]{}, err
	}
	if connection == nil || connection.dispatcher == nil {
		return Bound[P]{}, fault.New(fault.Invalid, "job binding requires a connection")
	}
	return Bound[P]{d, connection}, nil
}
func (b Bound[P]) options(options Options[P]) (Options[P], error) {
	if b.connection == nil || b.connection.dispatcher == nil {
		return options, fault.New(fault.Invalid, "job binding is not initialized")
	}
	if options.Queue == "" {
		options.Queue = b.connection.queue
	}
	return options, nil
}
func (b Bound[P]) Dispatch(ctx context.Context, payload P, options Options[P]) (Receipt[P], error) {
	options, err := b.options(options)
	if err != nil {
		return Receipt[P]{}, err
	}
	return b.definition.Dispatch(ctx, b.connection.dispatcher, payload, options)
}
func (b Bound[P]) Capture(ctx context.Context, payload P, options Options[P]) (Pending[P], error) {
	options, err := b.options(options)
	if err != nil {
		return Pending[P]{}, err
	}
	return b.definition.Capture(ctx, payload, options)
}
func (b Bound[P]) Inspect(ctx context.Context, id ID[P], queue Queue) (value.Optional[Record], error) {
	options, err := b.options(Options[P]{Queue: queue})
	if err != nil {
		return value.Optional[Record]{}, err
	}
	return b.definition.Inspect(ctx, b.connection.dispatcher, id, options.Queue)
}
func (b Bound[P]) Cancel(ctx context.Context, id ID[P], queue Queue) (bool, error) {
	options, err := b.options(Options[P]{Queue: queue})
	if err != nil {
		return false, err
	}
	return b.definition.Cancel(ctx, b.connection.dispatcher, id, options.Queue)
}
func (b Bound[P]) Enqueue(ctx context.Context, tx *database.Tx, producer *Outbox, payload P, options Options[P]) (outbox.ID[P], error) {
	options, err := b.options(options)
	if err != nil {
		return outbox.ID[P]{}, err
	}
	if producer == nil || producer.dispatcher != b.connection.dispatcher {
		return outbox.ID[P]{}, fault.New(fault.Invalid, "job outbox belongs to a different connection")
	}
	return b.definition.Enqueue(ctx, tx, producer, payload, options)
}
