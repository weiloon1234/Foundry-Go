package audit

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Correlation groups audit rows written by one logical operation, such as a
// request, import or bulk change. It is metadata for reading history together,
// not a secret, authorization scope or idempotency key.
type Correlation string

// Validate applies the attribution request-ID bounds: 1–128 bytes of valid,
// trimmed UTF-8 without control characters.
func (c Correlation) Validate() error {
	if c == "" {
		return fault.New(fault.Invalid, "audit correlation requires an identifier")
	}
	return attribution.Request{ID: attribution.RequestID(c)}.Validate()
}

type correlationIdentity struct{}
type correlationContextKey struct{}

// NewCorrelation generates a time-ordered UUIDv7 correlation identifier.
func NewCorrelation() (Correlation, error) {
	id, err := model.NewID[correlationIdentity]()
	if err != nil {
		return "", err
	}
	return Correlation(id.String()), nil
}

// WithCorrelation scopes subsequent audit writes to an explicit correlation,
// such as one batch of a bulk operation. Without it, rows use the attribution
// request ID when one is present, so a request's changes can be read together.
func WithCorrelation(ctx context.Context, correlation Correlation) (context.Context, error) {
	if ctx == nil {
		return nil, fault.New(fault.Invalid, "audit correlation requires a context")
	}
	if err := correlation.Validate(); err != nil {
		return nil, err
	}
	return context.WithValue(ctx, correlationContextKey{}, correlation), nil
}

// CorrelationFromContext returns the correlation audit writes in ctx will store.
func CorrelationFromContext(ctx context.Context) value.Optional[Correlation] {
	if ctx == nil {
		return value.Optional[Correlation]{}
	}
	if explicit, ok := ctx.Value(correlationContextKey{}).(Correlation); ok {
		return value.Set(explicit)
	}
	if id := attribution.FromContext(ctx).Request().ID; id != "" {
		return value.Set(Correlation(id))
	}
	return value.Optional[Correlation]{}
}
