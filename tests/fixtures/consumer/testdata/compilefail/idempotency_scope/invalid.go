package invalid

import (
	"context"
	fixture "foundry.test/consumer/idempotenthttp"
	"github.com/weiloon1234/Foundry-Go/idempotency"
)

func bad(ctx context.Context, op idempotency.Operation[fixture.Submission, fixture.Receipt], key idempotency.Key, fn idempotency.Handler[fixture.Submission, fixture.Receipt]) {
	_, _ = op.Run(ctx, "untrusted-header", key, fixture.Submission{}, fn)
}
