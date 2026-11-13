package http

import "github.com/weiloon1234/Foundry-Go/internal/httpheader"

// HeaderValue is an HTTP field value, distinct from a field name or method.
// Validation checks its byte bound and rejects control bytes except horizontal
// tab. A header-specific grammar, such as cookies or CSP, is a separate contract.
type HeaderValue string

const MaxHeaderValueBytes = httpheader.MaxValueBytes

// Validate checks the byte bound and rejects control bytes other than horizontal tabs.
func (v HeaderValue) Validate() error {
	return httpheader.ValidateValue(string(v))
}

// ResponseHeader is one explicit, typed native response-header declaration.
// SecurityHeaders validates names/values and rejects duplicates and framing
// headers; it does not infer header-specific semantics from arbitrary strings.
type ResponseHeader struct {
	Name  HeaderName
	Value HeaderValue
}

// Validate checks the header's name and value.
func (h ResponseHeader) Validate() error {
	if err := h.Name.Validate(); err != nil {
		return err
	}
	return h.Value.Validate()
}
