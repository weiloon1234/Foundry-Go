package jobs

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
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
