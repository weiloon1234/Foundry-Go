package jobs

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/value"
)

const MaxListLimit = 100
const ListScanLimit = 128

// ListOptions scans execution IDs in ascending order. Zero After begins a scan;
// zero Name/Version and State select all records. Pagination is weakly consistent
// under concurrent enqueue/expiry; Next may be nonzero for an empty filtered page.
type ListOptions struct {
	After   ExecutionID
	Name    Name
	Version Version
	State   State
	Limit   int
}

func (o ListOptions) Validate() error {
	if o.Limit < 1 || o.Limit > MaxListLimit {
		return fault.New(fault.Invalid, "job list limit must be between 1 and 100")
	}
	if (o.Name == "") != (o.Version == 0) || (o.Name != "" && !identifier.Semantic(string(o.Name))) {
		return fault.New(fault.Invalid, "job list schema filter requires name and version")
	}
	switch o.State {
	case "", Waiting, Blocked, Reserved, Running, Succeeded, Failed, Cancelled:
		return nil
	}
	return fault.New(fault.Invalid, "invalid job list state")
}
func (o ListOptions) Matches(r Record) bool {
	return (o.State == "" || o.State == r.State) && (o.Name == "" || (o.Name == r.Envelope.Name() && o.Version == r.Envelope.Version()))
}

type Page struct {
	Records []Record
	Next    ExecutionID
}

// Summary is safe operational metadata: no payload, origin, lease owner or raw
// error. RetryToken identifies the current failed state; it grants no permission.
type Summary struct {
	ID          ExecutionID `json:"id"`
	Name        Name        `json:"name"`
	Version     Version     `json:"version"`
	Queue       Queue       `json:"queue"`
	State       State       `json:"state"`
	Attempts    uint32      `json:"attempts"`
	MaxAttempts uint32      `json:"max_attempts"`
	Retries     uint32      `json:"retries"`
	Exceptions  uint32      `json:"exceptions,omitzero"`
	Reason      Reason      `json:"reason,omitempty"`
	Workflow    WorkflowID  `json:"workflow,omitzero"`
	AvailableAt time.Time   `json:"available_at"`
	CreatedAt   time.Time   `json:"created_at"`
	FinishedAt  time.Time   `json:"finished_at,omitzero"`
	RetryToken  RetryToken  `json:"retry_token,omitempty"`
}

func (r Record) Summary() Summary {
	token, _ := r.RetryToken()
	result := Summary{ID: r.Envelope.ID(), Name: r.Envelope.Name(), Version: r.Envelope.Version(), Queue: r.Envelope.Queue(), State: r.State, Attempts: r.Attempts, MaxAttempts: r.Envelope.Policy().Attempts, Retries: r.Retries, Exceptions: r.Exceptions, Workflow: r.Workflow, AvailableAt: r.AvailableAt, CreatedAt: r.CreatedAt, FinishedAt: r.FinishedAt, RetryToken: token}
	if len(r.History) > 0 {
		result.Reason = r.History[len(r.History)-1].Reason
	}
	return result
}

// Inspect is the explicit heterogeneous operations boundary. Prefer the typed
// Definition.Inspect in application domain code. Records contain private payloads;
// use Summary when emitting operator output.
func (d *Dispatcher) Inspect(ctx context.Context, queue Queue, id ExecutionID) (value.Optional[Record], error) {
	release, err := d.begin(ctx)
	if err != nil {
		return value.Optional[Record]{}, err
	}
	defer release()
	key, err := NewKey(d.config.Namespace, queue)
	if err != nil {
		return value.Optional[Record]{}, err
	}
	if id.IsZero() {
		return value.Optional[Record]{}, fault.New(fault.Invalid, "job inspection requires an identity")
	}
	return d.backend.JobInspect(ctx, key, id)
}

// List is the explicit operational boundary. It returns owned payload/history
// snapshots; callers must authorize inspection before exposing them over HTTP.
func (d *Dispatcher) List(ctx context.Context, queue Queue, options ListOptions) (Page, error) {
	release, err := d.begin(ctx)
	if err != nil {
		return Page{}, err
	}
	defer release()
	if err := options.Validate(); err != nil {
		return Page{}, err
	}
	key, err := NewKey(d.config.Namespace, queue)
	if err != nil {
		return Page{}, err
	}
	return d.backend.JobList(ctx, key, options)
}
