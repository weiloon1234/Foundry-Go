package generate

import (
	"fmt"
	"go/token"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/routecontract"
)

func httpDependencySet(dependencies []string) map[string]bool {
	result := map[string]bool{framework + "/http": true}
	for _, path := range dependencies {
		if path == framework || strings.HasPrefix(path, framework+"/") {
			result[path] = true
		}
	}
	return result
}

func (p *packageInput) httpMode(position token.Pos, role, explicit string, generic bool) (string, error) {
	if p.frameworkPackage {
		if explicit != "" {
			return "", p.diagnostic(position, "framework packages keep direct HTTP adapters; HTTP helpers belong to application modules")
		}
		return "none", nil
	}
	if p.httpDependency || generic {
		if explicit != "" {
			if p.httpDependency {
				return "", p.diagnostic(position, fmt.Sprintf("http=%s cannot emit HTTP helpers: framework http imports this package transitively; keep the adapter in http or omit the key", explicit))
			}
			return "", p.diagnostic(position, fmt.Sprintf("http=%s requires a concrete DTO; generic transport helpers are not supported", explicit))
		}
		return "none", nil
	}
	if explicit != "" {
		return explicit, nil
	}
	return routecontract.DefaultHTTP(role), nil
}

func (e *emitter) emitHTTPHelpers(name, descriptor, mode string, multipart bool) {
	if mode == "none" || mode == "" {
		return
	}
	http := e.useNamed(framework+"/http", "foundryhttp")
	if routecontract.HasBody(mode) {
		helper := routecontract.HelperName(name, "Body")
		e.line("// %s builds the HTTP body from %s; the generated descriptor owns the schema.", helper, descriptor)
		e.line("//go:noinline")
		if multipart {
			e.line("func %s(directory string) %s.Body[%s] { return %s.MultipartBody(%s.MultipartDirectory(%s(), directory)) }", helper, http, name, http, http, descriptor)
		} else {
			e.line("func %s() %s.Body[%s] { return %s.JSONBody(%s()) }", helper, http, name, http, descriptor)
		}
	}
	if routecontract.HasResponse(mode) {
		helper := routecontract.HelperName(name, "Response")
		e.line("// %s builds the HTTP response from %s; status remains part of the route declaration.", helper, descriptor)
		e.line("//go:noinline")
		e.line("func %s(status int) %s.Response[%s] { return %s.JSONResponse(status, %s()) }", helper, http, name, http, descriptor)
	}
}
