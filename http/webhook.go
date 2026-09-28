package http

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	stdhttp "net/http"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/webhook"
)

// VerifyWebhook verifies before typed decoding and every idempotent replay. It
// replaces any caller-supplied Idempotency-Key with a digest of the authenticated
// provider and delivery ID. The endpoint must scope effects to the typed account
// from verifier.FromContext; distinct accounts must have separate endpoint keys.
func VerifyWebhook[A any](verifier *webhook.Verifier[A]) Middleware {
	return defineReplayMiddleware("foundry.webhook", func(next stdhttp.Handler) (stdhttp.Handler, error) {
		if err := verifier.Validate(); err != nil {
			return nil, err
		}
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			w.Header().Set("Cache-Control", "no-store")
			var delivery webhook.Delivery[A]
			var verification error
			isolation := callback.Isolated("HTTP webhook verification", func() error { delivery, verification = verifier.VerifyRequest(r); return nil })
			if isolation != nil {
				writeRoutingError(w, r, InternalError)
				return
			}
			if verification != nil {
				code := InternalError
				switch {
				case errors.Is(verification, webhook.InvalidSignature):
					code = Unauthenticated
				case errors.Is(verification, webhook.BodyTooLarge):
					code = PayloadTooLarge
				case errors.Is(verification, context.Canceled), errors.Is(verification, context.DeadlineExceeded):
					code = RequestTimeout
				case errors.Is(verification, fault.Invalid):
					code = BadRequest
				}
				writeRoutingError(w, r, code)
				return
			}
			ctx, err := verifier.WithDelivery(r.Context(), delivery)
			if err != nil {
				writeRoutingError(w, r, InternalError)
				return
			}
			digest := sha256.Sum256([]byte(string(delivery.Provider()) + "\x00" + string(delivery.ID())))
			request := r.Clone(ctx)
			request.Header.Set("Idempotency-Key", hex.EncodeToString(digest[:]))
			next.ServeHTTP(w, request)
		}), nil
	})
}
