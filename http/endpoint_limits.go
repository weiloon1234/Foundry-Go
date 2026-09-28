package http

import (
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// EndpointLimits bounds query parsing, URL-encoded forms, multipart and JSON independently.
// The HTTP kernel's MaxBodyBytes remains an additional global input ceiling.
type EndpointLimits struct {
	Multipart  MultipartLimits
	Files      FileResponseLimits
	Query      QueryLimits
	Form       QueryLimits
	Body       contract.JSONLimits
	Response   contract.JSONLimits
	Validation validation.Limits
}

// DefaultEndpointLimits returns independent defaults without allocating payload
// buffers. Body and response bytes use the framework's default HTTP body size.
func DefaultEndpointLimits() EndpointLimits {
	json := contract.JSONLimits{Bytes: int(DefaultServerConfig().MaxBodyBytes), Depth: 32, Nodes: 65536, Steps: 262144, Issues: 16}
	return EndpointLimits{Form: QueryLimits{Bytes: json.Bytes, Pairs: 1024, Issues: json.Issues}, Files: DefaultFileResponseLimits(), Multipart: DefaultMultipartLimits(), Query: QueryLimits{Bytes: 16 << 10, Pairs: 128, Issues: 16}, Body: json, Response: json, Validation: validation.DefaultLimits()}
}

func (l EndpointLimits) Validate() error {
	// An inactive zero form budget preserves existing explicit limit literals.
	// Form endpoints additionally require a positive form budget at registration.
	if l.Form != (QueryLimits{}) {
		if err := l.Form.Validate(); err != nil {
			return err
		}
	}
	for _, err := range []error{l.Files.Validate(), l.Multipart.Validate(), l.Query.Validate(), l.Body.Validate(), l.Response.Validate(), l.Validation.Validate()} {
		if err != nil {
			return err
		}
	}
	return nil
}
