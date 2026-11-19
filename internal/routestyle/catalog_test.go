package routestyle

import (
	"go/types"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
)

// These are consumers of declarations, not declaration composition: inspectors,
// validators, URL/redirect builders, resolver execution, registration sentinels,
// and router/foundation registration containers. Each name is explicit so a new
// public function cannot silently inherit an exclusion by spelling or prefix.
var catalogueExclusions = map[string]string{
	"/http":              `AuthenticatedEndpointID AuthenticatedEndpointMethod AuthenticatedEndpointPattern AuthenticatedRouteURL EndpointID EndpointLocation EndpointMethod EndpointPattern IdempotentEndpointID InspectAuthenticated InspectAuthenticatedIdempotent InspectAuthenticatedRoute InspectEndpoint InspectIdempotent InspectOptional InspectOptionalRoute InspectSigned InspectSignedAuthenticated InspectSignedAuthenticatedRoute InspectTicketAuthenticated InvalidRouteRegistration MatchedRoute Module NewRouter OptionalEndpointID OptionalEndpointMethod OptionalEndpointPattern OptionalRouteURL PermanentAuthenticatedEndpointURL PermanentAuthenticatedRouteURL PermanentEndpointURL PermanentRouteURL RedirectToEndpoint RedirectToRoute RegisterEndpoint RegisterIdempotentEndpoint RegisterRoute RegisterRouterWithRoutes RegisterSignedEndpoint RegisterSignedRoute RelativeAuthenticatedEndpointURL RelativeAuthenticatedRouteURL RelativeEndpointURL RelativeRouteURL RouteIDOf RouteLocation RouteMethod RouteParameters RoutePattern RouteURL SignedAuthenticatedEndpointID SignedAuthenticatedEndpointMethod SignedAuthenticatedEndpointPattern SignedAuthenticatedRouteURL SignedEndpointID SignedEndpointMethod SignedEndpointPattern SignedRouteID SignedRouteMethod SignedRoutePattern SignedRouteURL ValidateAuthenticated ValidateAuthenticatedIdempotent ValidateAuthenticatedRoute ValidateEndpoint ValidateIdempotent ValidateOptional ValidateOptionalRoute ValidateRoute ValidateSigned ValidateSignedAuthenticated ValidateSignedAuthenticatedRoute ValidateSignedRoute ValidateTicketAuthenticated`,
	"/http/pagination":   `AuthenticatedEndpointID AuthenticatedEndpointMethod AuthenticatedEndpointPattern AuthenticatedURL EndpointID EndpointMethod EndpointPattern Inspect InspectAuthenticated URL Validate ValidateAuthenticated`,
	"/http/modelbinding": `DescribeAuthenticatedEndpoint DescribeAuthenticatedIdempotentEndpoint DescribeEndpoint DescribeIdempotentEndpoint Resolve ValidateAuthenticatedEndpoint ValidateAuthenticatedIdempotentEndpoint ValidateEndpoint ValidateIdempotentEndpoint ValidateResolver`,
	"/datatable":         `Module`,
}

func declarationType(typ types.Type) bool {
	if alias, ok := typ.(*types.Alias); ok && alias.Obj().Exported() {
		return declarationName(alias.Obj())
	}
	switch t := types.Unalias(typ).(type) {
	case *types.Pointer:
		return declarationType(t.Elem())
	case *types.Slice:
		return declarationType(t.Elem())
	case *types.Named:
		obj := t.Obj()
		if declarationName(obj) {
			return true
		}
		// Public pagination aliases expose genuine private generic origins.
		if obj.Pkg() != nil {
			for _, name := range obj.Pkg().Scope().Names() {
				alias, ok := obj.Pkg().Scope().Lookup(name).(*types.TypeName)
				if ok && alias.IsAlias() && declarationName(alias) {
					if origin, ok := types.Unalias(alias.Type()).(*types.Named); ok && origin.Origin() == t.Origin() {
						return true
					}
				}
			}
		}
	}
	return false
}
func declarationName(obj *types.TypeName) bool {
	if obj.Pkg() == nil || !obj.Exported() {
		return false
	}
	path := obj.Pkg().Path()
	if path != frameworkinfo.ModulePath+"/http" && path != frameworkinfo.ModulePath+"/http/pagination" && path != frameworkinfo.ModulePath+"/http/modelbinding" {
		return false
	}
	name := obj.Name()
	return name == "RouteRegistration" || strings.HasSuffix(name, "Endpoint") || strings.HasSuffix(name, "Route") || strings.HasSuffix(name, "Transport") || (path == frameworkinfo.ModulePath+"/http/modelbinding" && name == "Resolver")
}

func TestRealDeclarationCatalogueIsComplete(t *testing.T) {
	// Independent of the catalogue: removing an entire entry must fail too.
	families := []string{"/http", "/http/pagination", "/http/modelbinding", "/datatable"}
	wanted := map[string]bool{}
	var patterns []string
	for _, family := range families {
		wanted[family] = true
		patterns = append(patterns, "."+family)
	}
	pkgs, err := Load(t.Context(), "../..", patterns)
	if err != nil {
		t.Fatal(err)
	}
	covered := map[string]bool{}
	loaded := map[string]bool{}
	for _, pkg := range pkgs {
		suffix := strings.TrimPrefix(pkg.Types.Path(), frameworkinfo.ModulePath)
		if !wanted[suffix] {
			continue
		}
		loaded[suffix] = true
		_, catalogued := declarations[suffix]
		excluded := map[string]bool{}
		for _, name := range strings.Fields(catalogueExclusions[suffix]) {
			excluded[name] = true
		}
		analysis := NewAnalysis(pkg.TypesInfo, pkg.Fset)
		// These regression expectations distinguish private runtime machinery from
		// private origins deliberately exposed by public pagination aliases.
		for qualified, stage := range map[string]bool{
			"/http.matchedRoute":                         false,
			"/http.uploadRoute":                          false,
			"/http.endpointTransport":                    false,
			"/http.requestEndpoint":                      false,
			"/http/pagination.pageEndpoint":              true,
			"/http/pagination.authenticatedPageEndpoint": true,
			"/datatable.Table":                           false,
		} {
			family, name, _ := strings.Cut(qualified, ".")
			if family != suffix {
				continue
			}
			obj, ok := pkg.Types.Scope().Lookup(name).(*types.TypeName)
			if !ok {
				t.Errorf("expected type %s missing", qualified)
				continue
			}
			if declarationType(obj.Type()) != stage || analysis.IsStage(obj.Type()) != stage {
				t.Errorf("%s: independent predicate and derived stage membership must both be %t", qualified, stage)
			}
		}
		for _, name := range pkg.Types.Scope().Names() {
			if typ, ok := pkg.Types.Scope().Lookup(name).(*types.TypeName); ok {
				if declarationType(typ.Type()) && name != "RouteRegistration" && !analysis.IsStage(typ.Type()) {
					t.Errorf("stage type %s.%s is not derived from the catalogue", suffix, name)
				}
				switch name {
				case "Scope", "RouteSpec", "RouteRegistration", "Body", "Response", "Query", "EndpointOption", "RouteOption":
					if analysis.IsStage(typ.Type()) {
						t.Errorf("non-stage %s.%s classified as a stage", suffix, name)
					}
				}
			}
			fn, ok := pkg.Types.Scope().Lookup(name).(*types.Func)
			if !ok || !fn.Exported() || strings.HasSuffix(pkg.Fset.Position(fn.Pos()).Filename, "_test.go") {
				continue
			}
			signature := fn.Type().(*types.Signature)
			candidate := false
			for _, tuple := range []*types.Tuple{signature.Params(), signature.Results()} {
				for i := 0; i < tuple.Len(); i++ {
					candidate = candidate || declarationType(tuple.At(i).Type())
				}
			}
			if IsDeclaration(fn) {
				covered[suffix+"."+name] = true
			}
			if candidate && !catalogued {
				t.Errorf("%s has declaration candidates but no catalogue entry", suffix)
			}
			if candidate && !IsDeclaration(fn) && !excluded[name] {
				t.Errorf("%s.%s consumes or returns a route declaration: add it to the catalogue or a documented explicit exclusion", suffix, name)
			}
		}
	}
	for _, family := range families {
		if !loaded[family] {
			t.Errorf("declaration family %s was not loaded", family)
		}
	}
	for suffix, names := range declarations {
		for _, name := range strings.Fields(names) {
			if !covered[suffix+"."+name] {
				t.Errorf("catalogue entry %s.%s is not a real exported function", suffix, name)
			}
		}
	}
}
