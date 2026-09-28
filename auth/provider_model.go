package auth

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/model"
)

// CheckModel validates an already loaded model's stored identity and current
// eligibility, without a second lookup. Transactional framework adapters use it
// after locking the model. This does NOT verify a credential or grant authority.
// The caller owns freshness and bounds execution with its operation context.
func (p Provider[M, K]) CheckModel(ctx context.Context, subject M) (model.Reference[M, K], error) {
	if err := p.Validate(); err != nil {
		return model.Reference[M, K]{}, err
	}
	if ctx == nil {
		return model.Reference[M, K]{}, fault.New(fault.Invalid, "model check requires a context")
	}
	var reference model.Reference[M, K]
	err := callback.Isolated("check authentication model", func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		identity, err := subject.FoundryIdentity()
		if err != nil {
			return err
		}
		reference, err = p.Parse(identity)
		if err != nil {
			return err
		}
		return p.checkEligibility(ctx, subject)
	})
	if err != nil {
		return model.Reference[M, K]{}, err
	}
	if err := ctx.Err(); err != nil {
		return model.Reference[M, K]{}, err
	}
	return reference, nil
}
