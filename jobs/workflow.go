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
// Reuse this captured value on ambiguous enqueue; member IDs remain stable.
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
func (w Workflow) Dispatch(ctx context.Context, dispatcher *Dispatcher) (WorkflowReceipt, error) {
	receipt := WorkflowReceipt{ID: w.ID()}
	release, err := dispatcher.begin(ctx)
	if err != nil {
		return receipt, err
	}
	defer release()
	if err := w.envelope.Validate(); err != nil {
		return receipt, err
	}
	for _, step := range w.steps {
		entry, err := dispatcher.registry.lookup(jobKey{step.envelope.Name(), step.envelope.Version()})
		if err != nil {
			return receipt, err
		}
		if entry.typ != step.typ || !entry.policy.same(step.policy) {
			return receipt, fault.New(fault.Invalid, "workflow step differs from its registered definition")
		}
	}
	key, err := NewKey(dispatcher.config.Namespace, w.envelope.Queue())
	if err != nil {
		return receipt, err
	}
	receipt.Inserted, err = dispatcher.backend.JobWorkflow(ctx, key, w.envelope)
	return receipt, err
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
}

func (w WorkflowEnvelope) ID() WorkflowID                       { return w.id }
func (w WorkflowEnvelope) Kind() WorkflowKind                   { return w.kind }
func (w WorkflowEnvelope) Steps() []Envelope                    { return slices.Clone(w.steps) }
func (w WorkflowEnvelope) Completion() value.Optional[Envelope] { return w.completion }
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
	members := slices.Clone(w.steps)
	if completion, ok := w.completion.Get(); ok {
		if w.kind != BatchKind {
			return fault.New(fault.Invalid, "chain cannot have a batch completion")
		}
		members = append(members, completion)
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
	if completion, ok := w.completion.Get(); ok {
		data, err := completion.MarshalJSON()
		if err != nil {
			return nil, err
		}
		wire.Completion = data
	}
	return json.Marshal(wire)
}

type workflowWire struct {
	ID         WorkflowID      `json:"id"`
	Kind       WorkflowKind    `json:"kind"`
	Steps      []Envelope      `json:"steps"`
	Completion json.RawMessage `json:"completion,omitempty"`
}

func DecodeWorkflow(data []byte) (WorkflowEnvelope, error) {
	if len(data) > MaxWorkflowBytes+64*1024 {
		return WorkflowEnvelope{}, fault.New(fault.Invalid, "job workflow exceeds transport capacity")
	}
	if _, err := jsonwire.Decode(data, jsonwire.Limits{Bytes: MaxWorkflowBytes + 64*1024, Depth: value.JSONMaxDepth, Nodes: (MaxWorkflowSteps + 2) * value.JSONMaxNodes}); err != nil {
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
	if len(wire.Completion) > 0 {
		step, err := DecodeEnvelope(wire.Completion)
		if err != nil {
			return WorkflowEnvelope{}, err
		}
		result.completion = value.Set(step)
	}
	return result, result.Validate()
}

type workflowInput struct {
	ID         WorkflowID        `json:"id"`
	Kind       WorkflowKind      `json:"kind"`
	Steps      []json.RawMessage `json:"steps"`
	Completion json.RawMessage   `json:"completion,omitempty"`
}
