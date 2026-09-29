package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

const MaxWorkflowSteps = 256

// MaxWorkflowMembers bounds all jobs of one workflow: its steps plus the
// optional completion, catch and finally jobs.
const MaxWorkflowMembers = MaxWorkflowSteps + 3
const MaxWorkflowBytes = 8 << 20

type WorkflowID = model.ID[WorkflowExecution]
type WorkflowExecution struct{}
type WorkflowKind string

const (
	ChainKind WorkflowKind = "chain"
	BatchKind WorkflowKind = "batch"
)

// Step erases payload type only after typed Capture. Private type metadata lets
// dispatch reject a foreign definition even when its name/version match.
type Step struct {
	envelope Envelope
	typ      reflect.Type
	policy   Policy
}

func (p Pending[P]) Step() Step {
	return Step{envelope: p.envelope, typ: reflect.TypeFor[P](), policy: p.definition.policy.snapshot()}
}

// Workflow is an immutable atomic group. All members use one queue authority.
// A chain stops after the first terminal failure/cancellation. A batch continues
// its other members; its optional completion runs only if every member succeeds.
// Optional callbacks run once every member (and the completion) is terminal:
// the catch job only if a member failed or was cancelled, the finally job
// always. An explicit workflow cancellation cancels callbacks too. A callback
// that is not triggered ends cancelled with reason not_triggered. Callbacks are
// ordinary registered jobs with their own retries. Reuse this captured value on
// ambiguous enqueue; member IDs remain stable.
type Workflow struct {
	envelope WorkflowEnvelope
	steps    []Step
}
type WorkflowReceipt struct {
	ID       WorkflowID
	Inserted bool
}

func NewChain(steps ...Step) (Workflow, error) { return newWorkflow(ChainKind, steps) }
func NewBatch(steps ...Step) (Workflow, error) { return newWorkflow(BatchKind, steps) }
func newWorkflow(kind WorkflowKind, steps []Step) (Workflow, error) {
	id, err := model.NewID[WorkflowExecution]()
	if err != nil {
		return Workflow{}, err
	}
	result := Workflow{envelope: WorkflowEnvelope{id: id, kind: kind}, steps: slices.Clone(steps)}
	for _, step := range steps {
		result.envelope.steps = append(result.envelope.steps, step.envelope)
	}
	return result, result.envelope.Validate()
}
func (w Workflow) ID() WorkflowID             { return w.envelope.id }
func (w Workflow) Envelope() WorkflowEnvelope { return w.envelope }

// WithCompletion returns a new batch snapshot with one success-only final job.
// Call before dispatch; changing an already accepted identity conflicts.
func (w Workflow) WithCompletion(step Step) (Workflow, error) {
	if w.envelope.kind != BatchKind || w.envelope.completion.IsSet() {
		return Workflow{}, fault.New(fault.Invalid, "only a batch without completion accepts a completion job")
	}
	w.envelope.completion = value.Set(step.envelope)
	w.steps = append(slices.Clone(w.steps), step)
	return w, w.envelope.Validate()
}

// WithCatch returns a new snapshot whose catch job runs once, after every
// member is terminal, if any member failed or was cancelled by a failure.
func (w Workflow) WithCatch(step Step) (Workflow, error) {
	if w.envelope.catch.IsSet() {
		return Workflow{}, fault.New(fault.Invalid, "workflow already has a catch job")
	}
	w.envelope.catch = value.Set(step.envelope)
	w.steps = append(slices.Clone(w.steps), step)
	return w, w.envelope.Validate()
}

// WithFinally returns a new snapshot whose finally job runs once after every
// member (and the completion) is terminal, whatever the outcome.
func (w Workflow) WithFinally(step Step) (Workflow, error) {
	if w.envelope.finally.IsSet() {
		return Workflow{}, fault.New(fault.Invalid, "workflow already has a finally job")
	}
	w.envelope.finally = value.Set(step.envelope)
	w.steps = append(slices.Clone(w.steps), step)
	return w, w.envelope.Validate()
}
func (w Workflow) Dispatch(ctx context.Context, dispatcher *Dispatcher) (WorkflowReceipt, error) {
	receipt, key, err := w.dispatch(ctx, dispatcher)
	if err == nil && receipt.Inserted {
		dispatcher.runInline(ctx, key)
	}
	return receipt, err
}
func (w Workflow) dispatch(ctx context.Context, dispatcher *Dispatcher) (WorkflowReceipt, Key, error) {
	receipt := WorkflowReceipt{ID: w.ID()}
	release, err := dispatcher.begin(ctx)
	if err != nil {
		return receipt, Key{}, err
	}
	defer release()
	if err := w.check(dispatcher.registry); err != nil {
		return receipt, Key{}, err
	}
	key, err := NewKey(dispatcher.config.Namespace, w.envelope.Queue())
	if err != nil {
		return receipt, Key{}, err
	}
	receipt.Inserted, err = dispatcher.backend.JobWorkflow(ctx, key, w.envelope)
	return receipt, key, err
}

// check validates the snapshot and that every member matches its registration.
func (w Workflow) check(registry *Registry) error {
	if err := w.envelope.Validate(); err != nil {
		return err
	}
	for _, step := range w.steps {
		entry, err := registry.lookup(jobKey{step.envelope.Name(), step.envelope.Version()})
		if err != nil {
			return err
		}
		if entry.typ != step.typ || !entry.policy.same(step.policy) {
			return fault.New(fault.Invalid, "workflow step differs from its registered definition")
		}
	}
	return nil
}
func (w Workflow) Cancel(ctx context.Context, dispatcher *Dispatcher) (bool, error) {
	release, err := dispatcher.begin(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	if err := w.envelope.Validate(); err != nil {
		return false, err
	}
	key, err := NewKey(dispatcher.config.Namespace, w.envelope.Queue())
	if err != nil {
		return false, err
	}
	return dispatcher.backend.JobCancelWorkflow(ctx, key, w.ID())
}

// WorkflowEnvelope is the bounded adapter transport, never an executable service
// graph. Workflow members may not carry admission uniqueness: partial suppression
// would violate atomic group acceptance. Stable IDs provide group deduplication.
type WorkflowEnvelope struct {
	id         WorkflowID
	kind       WorkflowKind
	steps      []Envelope
	completion value.Optional[Envelope]
	catch      value.Optional[Envelope]
	finally    value.Optional[Envelope]
}

func (w WorkflowEnvelope) ID() WorkflowID                       { return w.id }
func (w WorkflowEnvelope) Kind() WorkflowKind                   { return w.kind }
func (w WorkflowEnvelope) Steps() []Envelope                    { return slices.Clone(w.steps) }
func (w WorkflowEnvelope) Completion() value.Optional[Envelope] { return w.completion }

// Catch and Finally are the optional callback jobs.
func (w WorkflowEnvelope) Catch() value.Optional[Envelope]   { return w.catch }
func (w WorkflowEnvelope) Finally() value.Optional[Envelope] { return w.finally }

// Members returns every job of the workflow: steps, then the completion, catch
// and finally jobs when present.
func (w WorkflowEnvelope) Members() []Envelope {
	members := slices.Clone(w.steps)
	for _, optional := range []value.Optional[Envelope]{w.completion, w.catch, w.finally} {
		if item, ok := optional.Get(); ok {
			members = append(members, item)
		}
	}
	return members
}
func (w WorkflowEnvelope) Queue() Queue {
	if len(w.steps) == 0 {
		return ""
	}
	return w.steps[0].Queue()
}
func (w WorkflowEnvelope) Validate() error {
	if w.id.IsZero() || (w.kind != ChainKind && w.kind != BatchKind) || len(w.steps) == 0 || len(w.steps) > MaxWorkflowSteps {
		return fault.New(fault.Invalid, "invalid job workflow identity, kind or size")
	}
	if w.completion.IsSet() && w.kind != BatchKind {
		return fault.New(fault.Invalid, "chain cannot have a batch completion")
	}
	members := w.Members()
	if len(members) > MaxWorkflowMembers {
		return fault.New(fault.Invalid, "invalid job workflow size")
	}
	seen := make(map[ExecutionID]bool)
	size := 0
	for _, item := range members {
		if err := item.Validate(); err != nil {
			return err
		}
		if item.Queue() != w.Queue() || seen[item.ID()] || item.Uniqueness().Digest != "" {
			return fault.New(fault.Invalid, "workflow requires distinct non-unique jobs in one queue")
		}
		seen[item.ID()] = true
		data, err := item.MarshalJSON()
		if err != nil {
			return err
		}
		size += len(data)
		if size > MaxWorkflowBytes {
			return fault.New(fault.Invalid, "job workflow exceeds byte capacity")
		}
	}
	return nil
}
func (w WorkflowEnvelope) MarshalJSON() ([]byte, error) {
	if err := w.Validate(); err != nil {
		return nil, err
	}
	wire := workflowWire{ID: w.id, Kind: w.kind, Steps: w.steps}
	for _, item := range []struct {
		source value.Optional[Envelope]
		target *json.RawMessage
	}{{w.completion, &wire.Completion}, {w.catch, &wire.Catch}, {w.finally, &wire.Finally}} {
		if envelope, ok := item.source.Get(); ok {
			data, err := envelope.MarshalJSON()
			if err != nil {
				return nil, err
			}
			*item.target = data
		}
	}
	return json.Marshal(wire)
}

// Catch and Finally are omitted when absent, so workflows without callbacks
// keep the transport older readers accept.
type workflowWire struct {
	ID         WorkflowID      `json:"id"`
	Kind       WorkflowKind    `json:"kind"`
	Steps      []Envelope      `json:"steps"`
	Completion json.RawMessage `json:"completion,omitempty"`
	Catch      json.RawMessage `json:"catch,omitempty"`
	Finally    json.RawMessage `json:"finally,omitempty"`
}

func DecodeWorkflow(data []byte) (WorkflowEnvelope, error) {
	if len(data) > MaxWorkflowBytes+64*1024 {
		return WorkflowEnvelope{}, fault.New(fault.Invalid, "job workflow exceeds transport capacity")
	}
	if _, err := jsonwire.Decode(data, jsonwire.Limits{Bytes: MaxWorkflowBytes + 64*1024, Depth: value.JSONMaxDepth, Nodes: (MaxWorkflowMembers + 1) * value.JSONMaxNodes}); err != nil {
		return WorkflowEnvelope{}, err
	}
	var wire workflowInput
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return WorkflowEnvelope{}, fault.New(fault.Invalid, "invalid job workflow transport")
	}

	result := WorkflowEnvelope{id: wire.ID, kind: wire.Kind}
	for _, data := range wire.Steps {
		step, err := DecodeEnvelope(data)
		if err != nil {
			return WorkflowEnvelope{}, err
		}
		result.steps = append(result.steps, step)
	}
	for _, item := range []struct {
		source json.RawMessage
		target *value.Optional[Envelope]
	}{{wire.Completion, &result.completion}, {wire.Catch, &result.catch}, {wire.Finally, &result.finally}} {
		if len(item.source) > 0 {
			step, err := DecodeEnvelope(item.source)
			if err != nil {
				return WorkflowEnvelope{}, err
			}
			*item.target = value.Set(step)
		}
	}
	return result, result.Validate()
}

type workflowInput struct {
	ID         WorkflowID        `json:"id"`
	Kind       WorkflowKind      `json:"kind"`
	Steps      []json.RawMessage `json:"steps"`
	Completion json.RawMessage   `json:"completion,omitempty"`
	Catch      json.RawMessage   `json:"catch,omitempty"`
	Finally    json.RawMessage   `json:"finally,omitempty"`
}
