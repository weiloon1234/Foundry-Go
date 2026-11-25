package routestyle

import (
	"go/types"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
)

// The catalogue covers declaration/composition functions, not option builders,
// DTO descriptors, inspectors, runtime operations or registration containers.
var declarations = map[string]string{
	"/http": `DefineRoute ConfigureRoute DefineEndpoint ConfigureEndpoint
PrepareEndpoint AuthorizeEndpoint ValidateInput ValidatePath ValidateQuery ValidateHeaders ValidateBody
EndpointHeaders BodyExample ResponseExample
Authenticated OptionallyAuthenticated
ConfigureAuthenticated ConfigureOptional PrepareAuthenticated PrepareOptional AuthorizeActor AuthorizeOptionalActor
RouteAuthenticated RouteOptionallyAuthenticated ConfigureAuthenticatedRoute ConfigureOptionalRoute
Signed SignedAuthenticated SignedOptionalAuthentication SignRoute SignAuthenticatedRoute SignOptionalRoute
ConfigureSigned ConfigureSignedAuthenticated ConfigureSignedRoute ConfigureSignedAuthenticatedRoute
TicketAuthenticated Idempotent IdempotentSigned IdempotentAuthenticated IdempotentHeaders AuthenticatedIdempotentHeaders
ForBinding ForSignedBinding ForAuthenticatedBinding ForOptionalBinding ForSignedAuthenticatedBinding ForSignedOptionalBinding
AfterValidation AfterAuthenticatedValidation
HandleRaw HandleBound HandleAuthenticatedRaw HandleAuthenticatedBound HandleOptionalRaw HandleOptionalBound
HandleSignedRaw HandleSignedBound HandleSignedAuthenticatedRaw HandleSignedAuthenticatedBound PrepareIdempotent PrepareAuthenticatedIdempotent`,
	"/http/pagination": `DefineNumbered DefineSimple DefineCursor Configure ConfigureAuthenticated
Authenticated AuthorizeActor ValidatePath ValidateFilters Handle HandleAuthenticated`,
	"/http/modelbinding": `Define WithMissing ByKey ByColumn Then ThroughRelation ThroughSelectedRelation
Bind BindAuthenticated BindIdempotent BindAuthenticatedIdempotent
AuthorizeResource AuthorizeAuthenticatedResource AuthorizeIdempotentResource AuthorizeAuthenticatedIdempotentResource
IdempotentHeaders AuthenticatedIdempotentHeaders`,
	"/datatable": `Routes QueryEndpoint DownloadEndpoint BindTicketExport BindFileExport`,
}

// IsDeclaration uses the declared object's package identity, including imported aliases
// and explicit generic instantiations. A consumer function with the same name is unrelated.
func IsDeclaration(fn *types.Func) bool {
	if fn == nil || fn.Pkg() == nil || fn.Type().(*types.Signature).Recv() != nil {
		return false
	}
	for suffix, names := range declarations {
		if fn.Pkg().Path() != frameworkinfo.ModulePath+suffix {
			continue
		}
		for _, name := range strings.Fields(names) {
			if fn.Name() == name {
				return true
			}
		}
	}
	return false
}
