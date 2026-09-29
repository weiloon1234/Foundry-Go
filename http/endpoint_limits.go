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
	// Raw bounds a RawRequestBody. Zero is allowed on other endpoints.
	Raw RawBodyLimits
}

// DefaultResponseBytes and DefaultResponseNodes bound a typed JSON response by
// default: large enough for ordinary list pages (tens of thousands of rows),
// still bounded. Nodes count values and object names; Steps allow four visits per node.
const (
	DefaultResponseBytes = 32 << 20
	DefaultResponseNodes = 1 << 20
)

// DefaultEndpointLimits returns independent defaults without allocating payload
// buffers. Request body bytes use the framework's default HTTP body size. A
// response that exceeds its limits is an internal error whose redacted
// diagnostic names EndpointLimits.Response and the exceeded bound.
func DefaultEndpointLimits() EndpointLimits {
	json := contract.JSONLimits{Bytes: int(DefaultServerConfig().MaxBodyBytes), Depth: 32, Nodes: 65536, Steps: 262144, Issues: 16}
	response := contract.JSONLimits{Bytes: DefaultResponseBytes, Depth: 32, Nodes: DefaultResponseNodes, Steps: 4 * DefaultResponseNodes, Issues: 16}
	return EndpointLimits{Form: QueryLimits{Bytes: json.Bytes, Pairs: 1024, Issues: json.Issues}, Files: DefaultFileResponseLimits(), Multipart: DefaultMultipartLimits(), Query: QueryLimits{Bytes: 16 << 10, Pairs: 128, Issues: 16}, Body: json, Response: response, Validation: validation.DefaultLimits(), Raw: RawBodyLimits{Bytes: DefaultServerConfig().MaxBodyBytes}}
}

func (l EndpointLimits) Validate() error {
	// An inactive zero form budget preserves existing explicit limit literals.
	// Form endpoints additionally require a positive form budget at registration.
	if l.Form != (QueryLimits{}) {
		if err := l.Form.Validate(); err != nil {
			return err
		}
	}
	if l.Raw != (RawBodyLimits{}) {
		if err := l.Raw.Validate(); err != nil {
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
