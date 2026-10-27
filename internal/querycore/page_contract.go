package querycore

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"math"
)

// PageRequest selects a one-based page. Size must be between 1 and MaxPageSize.
// Invalid requests fail rather than being silently clamped.
type PageRequest struct{ Number, Size int }

// Page contains complete results and an unpaginated matching count. Count and
// rows are separate queries; use a repeatable-read transaction for one snapshot.
type Page[M any] struct {
	Items        []M
	Number, Size int
	Total, Pages int64
}

// SimplePage contains complete results without a total count. HasMore indicates
// an extra row existed after this page in the read's snapshot. Number > 1 permits
// backward navigation; neither direction guarantees rows survive a later request.
type SimplePage[R any] struct {
	Items        []R
	Number, Size int
	HasMore      bool
}

func ValidPageSize(size int) bool { return size > 0 && size <= MaxPageSize }

func PageOffset(r PageRequest) (int, error) {
	if r.Number < 1 || !ValidPageSize(r.Size) || r.Number-1 > math.MaxInt/r.Size {
		return 0, safeerror.NewFault(fault.Invalid, "invalid page number, size or offset overflow")
	}
	return (r.Number - 1) * r.Size, nil
}

// Validate checks number, size and offset representability without database I/O.
// Transport adapters reuse this contract rather than computing offsets.
func (r PageRequest) Validate() error { _, err := PageOffset(r); return err }

func PageCount(total int64, size int) int64 {
	pages := total / int64(size)
	if total%int64(size) != 0 {
		pages++
	}
	return pages
}

// Validate checks page metadata and the bounded item count. It does not assert
// that Total and Items came from one snapshot: counting and reading may observe
// different database states. Empty and beyond-last pages remain valid.
func (p Page[R]) Validate() error {
	if err := (PageRequest{Number: p.Number, Size: p.Size}).Validate(); err != nil {
		return err
	}
	if p.Total < 0 || p.Pages != PageCount(p.Total, p.Size) || len(p.Items) > p.Size {
		return safeerror.NewFault(fault.Invalid, "invalid numbered page metadata")
	}
	return nil
}

// Validate checks a bounded simple page without fabricating totals. HasMore
// requires a full page because its hidden lookahead row was removed by the read.
func (p SimplePage[R]) Validate() error {
	if err := (PageRequest{Number: p.Number, Size: p.Size}).Validate(); err != nil {
		return err
	}
	if len(p.Items) > p.Size || p.HasMore && len(p.Items) != p.Size {
		return safeerror.NewFault(fault.Invalid, "invalid simple page metadata")
	}
	return nil
}
