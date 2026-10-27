package sqlcallback

import (
	"database/sql"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Decode publishes only complete values. The trusted capability is supplied
// only by the codec owner's private framework constructor, never by applications.
func Decode[T any](operation string, decode func(any) (T, error), source any, trusted bool) (T, error) {
	if trusted {
		decoded, err := decode(source)
		if err != nil {
			return *new(T), safeerror.Wrap(operation+" failed", callback.Returned(operation, err))
		}
		return decoded, nil
	}
	var decoded T
	err := callback.Isolated(operation, func() error {
		var failure error
		decoded, failure = decode(source)
		return failure
	})
	if err != nil {
		return *new(T), safeerror.Wrap(operation+" failed", err)
	}
	return decoded, nil
}

// NewScanner retains the codec owner's capability. Its public Scan preserves
// error identity; its native SQL view seals causes until the database owner
// regains control. Both views use the same allocation and destination.
// Keep the destination closure shared across codec call sites. Inlining this
// constructor replicates generic closures in every column/aggregate adapter.
//
//go:noinline
func NewScanner[T any](decode func(any) (T, error), destination *T, trusted bool) sql.Scanner {
	s := &codecScanner{}
	s.scan = func(source any) error {
		if destination == nil || decode == nil {
			return safeerror.NewFault(fault.Invalid, "invalid database codec value or destination")
		}
		decoded, err := Decode("database value scanning", decode, source, trusted)
		if err != nil {
			return err
		}
		*destination = decoded
		return nil
	}
	s.native.source = s
	return s
}

// This private concrete type is the scanner capability. Destination adaptation
// uses an exact assertion, so embedding cannot confer it on an overriding Scan.
// Only the destination closure is generic; SQL adapters are shared by all types.
type codecScanner struct {
	scan   func(any) error
	native nativeCodecScanner
}

func (s *codecScanner) Scan(source any) error {
	if s == nil {
		return safeerror.NewFault(fault.Invalid, "invalid database codec value or destination")
	}
	return s.scan(source)
}

type nativeCodecScanner struct{ source *codecScanner }

func (s *nativeCodecScanner) Scan(source any) error {
	if s == nil {
		return safeerror.Seal("scan SQL value failed", safeerror.NewFault(fault.Invalid, "invalid database codec value or destination"))
	}
	err := s.source.Scan(source)
	if err != nil {
		return safeerror.Seal("scan SQL value failed", callback.Returned("scan SQL value", err))
	}
	return nil
}
