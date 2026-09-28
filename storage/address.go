package storage

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/internal/callback"
)

// ObjectAddress identifies a provider object independently of disk names and
// namespace aliases. Adapters use a stable store identity and the complete key.
// It is opaque operational metadata, not a URL, authorization token or DB ID.
type ObjectAddress struct{ store, object string }

func NewObjectAddress(store, object string) (ObjectAddress, error) {
	if store == "" || object == "" || len(store) > 8192 || len(object) > 8192 {
		return ObjectAddress{}, Failure(Invalid, CopyOperation, Unchanged, nil)
	}
	return ObjectAddress{store, object}, nil
}
func (ObjectAddress) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("storage object address")) }

// ObjectLocator lets safe moves reject aliases of the same physical object.
// Locate performs no I/O. Return the same store identity across adapter instances
// for the same store, and account for Namespace in the complete object key.
type ObjectLocator interface {
	Locate(ObjectKey) (ObjectAddress, error)
}

func (d *Disk) address(ctx context.Context, key ObjectKey) (ObjectAddress, error) {
	if err := key.Validate(); err != nil {
		return ObjectAddress{}, err
	}
	locator, ok := d.backend.(ObjectLocator)
	if !ok {
		return ObjectAddress{}, Failure(Unsupported, MoveOperation, Unchanged, nil)
	}
	op, release, err := d.begin(ctx, MoveOperation)
	if err != nil {
		return ObjectAddress{}, err
	}
	defer release()
	var result ObjectAddress
	err = callback.Isolated("storage object address", func() error { var err error; result, err = locator.Locate(key); return err })
	if err == nil {
		_, err = NewObjectAddress(result.store, result.object)
	}
	if err = finish(MoveOperation, op, err, Unchanged); err != nil {
		return ObjectAddress{}, err
	}
	return result, nil
}
