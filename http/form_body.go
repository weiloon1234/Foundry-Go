package http

import stdhttp "net/http"

// URL-encoded forms deliberately have a flat wire grammar. Brackets/dots are
// literal declared names, never instructions to create nested dynamic objects.
// The query owner supplies strict percent/UTF-8 decoding, omission, scalar
// cardinality, repeated values, owned callbacks and redacted field diagnostics.
func (b Body[B]) readForm(w stdhttp.ResponseWriter, r *stdhttp.Request, limits QueryLimits) (B, error) {
	data, err := readEndpointBody(w, r, int64(limits.Bytes), false, func(header stdhttp.Header) bool {
		return requestMedia(header, func(media string) bool { return media == "application/x-www-form-urlencoded" })
	})
	if err != nil {
		return *new(B), err
	}
	result, err := b.form.Decode(r.Context(), string(data), limits)
	if err != nil {
		return *new(B), endpointInputError(r.Context(), "body", err)
	}
	return result, nil
}
