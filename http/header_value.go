package http

import "github.com/weiloon1234/Foundry-Go/fault"

// HeaderValue is an HTTP field value, distinct from a field name or method.
// Validation checks its byte bound and rejects control bytes except horizontal
// tab. A header-specific grammar, such as cookies or CSP, is a separate contract.
type HeaderValue string

const MaxHeaderValueBytes = 8192

func (v HeaderValue) Validate() error {
	if len(v) > MaxHeaderValueBytes {
		return fault.New(fault.Invalid, "HTTP header value exceeds its byte bound")
	}
	for i := range len(v) {
		if v[i] != '\t' && (v[i] < 0x20 || v[i] == 0x7f) {
			return fault.New(fault.Invalid, "HTTP header value contains an invalid control byte")
		}
	}
	return nil
}

// ResponseHeader is one explicit, typed native response-header declaration.
// SecurityHeaders validates names/values and rejects duplicates and framing
// headers; it does not infer header-specific semantics from arbitrary strings.
type ResponseHeader struct {
	Name  HeaderName
	Value HeaderValue
}

func (h ResponseHeader) Validate() error {
	if err := h.Name.Validate(); err != nil {
		return err
	}
	return h.Value.Validate()
}
