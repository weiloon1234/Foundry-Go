package s3

import (
	"github.com/aws/smithy-go"
	"github.com/weiloon1234/Foundry-Go/storage"
	"testing"
)

func TestMissingBucketPreservesEstablishedMutationOutcome(t *testing.T) {
	for _, tc := range []struct{ before, after storage.Outcome }{
		{storage.NotApplicable, storage.NotApplicable},
		{storage.Unchanged, storage.Unchanged},
		{storage.Unknown, storage.Unchanged},
		{storage.Applied, storage.Applied},
	} {
		err := failure(storage.PutOperation, tc.before, &smithy.GenericAPIError{Code: "NoSuchBucket"})
		if err.Code() != storage.Unavailable || err.Outcome() != tc.after {
			t.Fatalf("outcome %v became %v; want %v", tc.before, err.Outcome(), tc.after)
		}
	}
}
