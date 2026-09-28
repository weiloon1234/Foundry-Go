package invalid

import (
	"context"
	"foundry.test/consumer/recovering"
)

func wrong(ctx context.Context, requests *recovering.ResetRequests) {
	_ = requests.Request(ctx, int64(42))
}
