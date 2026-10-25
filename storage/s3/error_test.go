package s3

import (
	"errors"
	"testing"

	"github.com/aws/smithy-go"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func TestMissingBucketPreservesEstablishedMutationOutcome(t *testing.T) {
	for _, tc := range []struct{ before, after storage.Outcome }{
		{storage.NotApplicable, storage.NotApplicable},
		{storage.Unchanged, storage.Unchanged},
		{storage.Unknown, storage.Unchanged},
		{storage.Applied, storage.Applied},
	} {
		err := failure(storage.PutOperation, tc.before, &smithy.GenericAPIError{Code: "NoSuchBucket"})
		var detail *storage.Error
		if !errors.As(err, &detail) || detail.Code() != storage.Unavailable || detail.Outcome() != tc.after {
			t.Fatalf("outcome %v lost expected %v", tc.before, tc.after)
		}
	}
}
