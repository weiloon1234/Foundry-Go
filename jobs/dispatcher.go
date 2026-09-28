package jobs

import (
	"context"
	"reflect"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/tracing"
	"github.com/weiloon1234/Foundry-Go/value"
)

// DispatchConfig bounds simultaneous payload capture/adapter operations. Full
// admission fails immediately; recursive dispatch cannot deadlock on capacity.
type DispatchConfig struct {
	Namespace   keyspace.Namespace
	MaxInFlight int
}

func DefaultDispatchConfig(namespace keyspace.Namespace) DispatchConfig {
	return DispatchConfig{Namespace: namespace, MaxInFlight: 64}
}
func (c DispatchConfig) Validate() error {
	if err := c.Namespace.Validate(); err != nil {
		return err
	}
	if c.MaxInFlight <= 0 || c.MaxInFlight > 65536 {
		return fault.New(fault.Invalid, "invalid job dispatch concurrency")
	}
	return nil
}

// Dispatcher borrows a backend and immutable registry. It starts no goroutines
// and performs no I/O during construction. Each call owns and waits for its
// capture/adapter operation; the application must drain callers before closing
// the borrowed backend. It must not be copied.
type Dispatcher struct {
	managed  bool
	backend  Backend
	registry *Registry
	config   DispatchConfig
	slots    chan struct{}
}

func NewDispatcher(backend Backend, registry *Registry, config DispatchConfig) (*Dispatcher, error) {
	if backend == nil || isNil(backend) || registry == nil || registry.entries == nil {
		return nil, fault.New(fault.Invalid, "job dispatcher requires a backend and registry")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Dispatcher{backend: backend, registry: registry, config: config, slots: make(chan struct{}, config.MaxInFlight)}, nil
}
func isNil(v any) bool {
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}
func (d *Dispatcher) begin(ctx context.Context) (func(), error) {
	if d == nil || d.slots == nil || ctx == nil {
		return nil, fault.New(fault.Invalid, "job dispatch requires an initialized dispatcher and context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case d.slots <- struct{}{}:
		return func() { <-d.slots }, nil
	default:
		return nil, fault.New(fault.Conflict, "job dispatch capacity reached")
	}
}

// Options are owned by the concrete job payload. Zero ID generates a new one;
// nonzero ID permits an intentional retry. Zero At means immediately eligible.
// Queue optionally overrides the definition's default routing destination.
type Options[P any] struct {
	ID     ID[P]
	At     time.Time
	Queue  Queue
	Unique Unique[P]
	// PropagateTrace captures the current immutable trace in envelope format 2.
	// Upgrade every worker/outbox reader before enabling it. False preserves
	// the legacy wire shape; an absent trace also keeps the legacy shape.
	PropagateTrace bool
}

// Receipt identifies the captured dispatch even if backend acceptance is unknown.
// Inserted=false with nil error means the identity already existed unchanged.
type Receipt[P any] struct {
	ID       ID[P]
	Inserted bool
}

// Pending owns an immutable payload, identity and attribution snapshot. Reusing
// it after an ambiguous enqueue retries the same message without rerunning codecs
// or retaining the original request context. It is also the composition boundary
// for durable publication; it contains no executable callback.
type Pending[P any] struct {
	definition Definition[P]
	envelope   Envelope
}

func (p Pending[P]) ID() ID[P]          { return model.IDFromBytes[ExecutionOf[P]](p.envelope.ID().Bytes()) }
func (p Pending[P]) Envelope() Envelope { return p.envelope }

// Capture prepares a stable snapshot without backend I/O. Custom payload codecs
// must be deterministic and cooperate; this call waits for actual codec exit.
func (d Definition[P]) Capture(ctx context.Context, input P, options Options[P]) (Pending[P], error) {
	if ctx == nil {
		return Pending[P]{}, fault.New(fault.Invalid, "job capture requires a context")
	}
	if err := ctx.Err(); err != nil {
		return Pending[P]{}, err
	}
	if err := d.Validate(); err != nil {
		return Pending[P]{}, err
	}
	unique, err := options.Unique.capture(d.name, d.version)
	if err != nil {
		return Pending[P]{}, err
	}
	origin := attribution.FromContext(ctx)
	if err := origin.Validate(); err != nil {
		return Pending[P]{}, err
	}
	id := options.ID
	if id.IsZero() {
		var err error
		id, err = NewID[P]()
		if err != nil {
			return Pending[P]{}, err
		}
	}
	policy := d.policy.snapshot()
	if options.Queue != "" {
		policy.Queue = options.Queue
	}
	var payload string
	err = callback.Isolated("capture job payload", func() error {
		snapshot, err := value.NewJSON(input)
		if err != nil {
			return err
		}
		payload, err = snapshot.Text()
		return err
	})
	if err != nil {
		return Pending[P]{}, err
	}
	if err := ctx.Err(); err != nil {
		return Pending[P]{}, err
	}
	envelope := Envelope{wire: envelopeWire{ID: model.IDFromBytes[Execution](id.Bytes()), Name: d.name, Version: d.version, Policy: policy, AvailableAt: options.At.UTC(), Origin: origin, Unique: unique}, payload: payload}
	if trace := tracing.FromContext(ctx); options.PropagateTrace && !trace.IsZero() {
		envelope.wire.EnvelopeVersion = TracedEnvelope
		envelope.wire.Trace = value.Set(trace)
	}
	if _, err := envelope.MarshalJSON(); err != nil {
		return Pending[P]{}, err
	}
	return Pending[P]{definition: d, envelope: envelope}, nil
}
func (d Definition[P]) check(registry *Registry) error {
	entry, err := registry.lookup(jobKey{d.name, d.version})
	if err != nil {
		return err
	}
	if entry.typ != reflect.TypeFor[P]() || !entry.policy.same(d.policy) {
		return fault.New(fault.Invalid, "job definition differs from its registered payload or policy")
	}
	return nil
}

// Dispatch captures and submits a concrete payload. For deliberate retries after
// network ambiguity, prefer Capture once followed by Pending.Dispatch.
func (d Definition[P]) Dispatch(ctx context.Context, dispatcher *Dispatcher, input P, options Options[P]) (Receipt[P], error) {
	release, err := dispatcher.begin(ctx)
	if err != nil {
		return Receipt[P]{}, err
	}
	defer release()
	if err := d.check(dispatcher.registry); err != nil {
		return Receipt[P]{}, err
	}
	pending, err := d.Capture(ctx, input, options)
	if err != nil {
		return Receipt[P]{}, err
	}
	return pending.submit(ctx, dispatcher)
}
func (p Pending[P]) Dispatch(ctx context.Context, dispatcher *Dispatcher) (Receipt[P], error) {
	release, err := dispatcher.begin(ctx)
	if err != nil {
		return Receipt[P]{}, err
	}
	defer release()
	if err := p.definition.check(dispatcher.registry); err != nil {
		return Receipt[P]{}, err
	}
	return p.submit(ctx, dispatcher)
}
func (p Pending[P]) submit(ctx context.Context, dispatcher *Dispatcher) (Receipt[P], error) {
	receipt := Receipt[P]{ID: p.ID()}
	if err := p.envelope.Validate(); err != nil {
		return receipt, err
	}
	key, err := NewKey(dispatcher.config.Namespace, p.envelope.Queue())
	if err != nil {
		return receipt, err
	}
	receipt.Inserted, err = dispatcher.backend.JobEnqueue(ctx, key, p.envelope)
	return receipt, err
}

// Inspect and Cancel preserve the payload owner on ordinary application IDs.
func (d Definition[P]) Inspect(ctx context.Context, dispatcher *Dispatcher, id ID[P], queue Queue) (value.Optional[Record], error) {
	release, err := dispatcher.begin(ctx)
	if err != nil {
		return value.Optional[Record]{}, err
	}
	defer release()
	if err := d.check(dispatcher.registry); err != nil {
		return value.Optional[Record]{}, err
	}
	if queue == "" {
		queue = d.policy.Queue
	}
	key, err := NewKey(dispatcher.config.Namespace, queue)
	if err != nil {
		return value.Optional[Record]{}, err
	}
	record, err := dispatcher.backend.JobInspect(ctx, key, model.IDFromBytes[Execution](id.Bytes()))
	if err != nil {
		return value.Optional[Record]{}, err
	}
	if found, ok := record.Get(); ok && (found.Envelope.Name() != d.name || found.Envelope.Version() != d.version) {
		return value.Optional[Record]{}, nil
	}
	return record, nil
}
func (d Definition[P]) Cancel(ctx context.Context, dispatcher *Dispatcher, id ID[P], queue Queue) (bool, error) {
	release, err := dispatcher.begin(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	if err := d.check(dispatcher.registry); err != nil {
		return false, err
	}
	if queue == "" {
		queue = d.policy.Queue
	}
	key, err := NewKey(dispatcher.config.Namespace, queue)
	if err != nil {
		return false, err
	}
	return dispatcher.backend.JobCancel(ctx, key, Target{ID: model.IDFromBytes[Execution](id.Bytes()), Name: d.name, Version: d.version})
}
