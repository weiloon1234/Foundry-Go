package attachments

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/storage"
)

type privateStorageOutcome struct {
	cause error
	calls atomic.Int32
}

func (e *privateStorageOutcome) Error() string { e.calls.Add(1); panic("private storage outcome") }
func (e *privateStorageOutcome) Unwrap() error { e.calls.Add(1); return e.cause }
func (e *privateStorageOutcome) As(any) bool   { e.calls.Add(1); panic("private classification") }

func TestStoragePublicationOutcomeUsesOnlyDirectResult(t *testing.T) {
	for _, outcome := range []storage.Outcome{storage.Unchanged, storage.Unknown, storage.Applied, storage.NotApplicable} {
		for _, marked := range []bool{false, true} {
			t.Run(fmt.Sprintf("outcome=%d/marked=%t", outcome, marked), func(t *testing.T) {
				raw := storage.Failure(storage.Unavailable, storage.PutOperation, outcome, nil)
				var result error = raw
				if marked {
					result = safeerror.Owned(raw.Error(), raw)
				}
				if got := storageUnchanged(result); got != (outcome == storage.Unchanged) {
					t.Fatal("source marker changed publication outcome")
				}
			})
		}
	}
	unchanged := storage.Failure(storage.Unavailable, storage.PutOperation, storage.Unchanged, nil)
	private := &privateStorageOutcome{cause: unchanged}
	for _, result := range []error{
		nil,
		(*storage.Error)(nil),
		private,
		safeerror.Owned("storage operation failed", private),
		errors.Join(errors.New("unknown publication"), unchanged),
		safeerror.Join("storage operation failed", storageFailure(storage.Unavailable, storage.PutOperation, storage.Unknown, nil), unchanged),
		storageFailure(storage.Unavailable, storage.PutOperation, storage.Unknown, unchanged),
	} {
		if storageUnchanged(result) {
			t.Fatal("nested cleanup or private wrapper falsely proved unchanged")
		}
	}
	if private.calls.Load() != 0 {
		t.Fatal("outcome classification invoked application methods")
	}
}
