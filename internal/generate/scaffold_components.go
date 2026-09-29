package generate

import (
	"fmt"
	"go/ast"
	"go/token"
	"regexp"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/internal/httppath"
)

// MaxScaffoldEnumCases bounds the cases of one enum scaffold.
const MaxScaffoldEnumCases = 64

var enumCaseName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

var endpointMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}

// validateComponentOptions rejects options that belong to another kind before
// any package is loaded.
func validateComponentOptions(options ScaffoldOptions) error {
	if options.Kind != EndpointScaffold && (options.Method != "" || options.Path != "") {
		return fmt.Errorf("--method and --path are only valid for an endpoint")
	}
	if options.Kind != EnumScaffold && len(options.Cases) != 0 {
		return fmt.Errorf("--cases is only valid for an enum")
	}
	if options.Kind != ListenerScaffold && options.Event != "" {
		return fmt.Errorf("--event is only valid for a listener")
	}
	if options.Kind != PolicyScaffold && (options.Subject != "" || options.Resource != "") {
		return fmt.Errorf("--subject and --resource are only valid for a policy")
	}
	localType := func(name string) bool { return len(name) <= 128 && token.IsIdentifier(name) && ast.IsExported(name) }
	switch options.Kind {
	case EndpointScaffold:
		if !slices.Contains(endpointMethods, options.Method) {
			return fmt.Errorf("endpoint scaffold needs --method GET, POST, PUT, PATCH or DELETE")
		}
		segments, err := httppath.Parse(options.Path)
		if err != nil {
			return fmt.Errorf("endpoint scaffold needs a valid --path: %w", err)
		}
		for _, segment := range segments {
			if segment.Name != "" {
				return fmt.Errorf("endpoint scaffold supports static paths; declare a //foundry:path struct for parameters afterwards")
			}
		}
	case EnumScaffold:
		if options.ID != "" {
			return fmt.Errorf("enum identity comes from its Go type; omit --id")
		}
		if len(options.Cases) == 0 || len(options.Cases) > MaxScaffoldEnumCases {
			return fmt.Errorf("enum scaffold needs one to %d --cases", MaxScaffoldEnumCases)
		}
		for i, value := range options.Cases {
			if !enumCaseName.MatchString(value) || slices.Contains(options.Cases[:i], value) {
				return fmt.Errorf("enum cases must be unique lower_snake_case values")
			}
		}
	case ListenerScaffold:
		if !localType(options.Event) {
			return fmt.Errorf("listener scaffold needs the exported --event payload type of this package")
		}
	case PolicyScaffold:
		if !localType(options.Subject) || !localType(options.Resource) {
			return fmt.Errorf("policy scaffold needs exported --subject and --resource types of this package")
		}
	}
	return nil
}

func componentScaffoldImports(options ScaffoldOptions) []string {
	switch options.Kind {
	case EndpointScaffold:
		return []string{framework + "/http", framework + "/fault"}
	case MiddlewareScaffold:
		return []string{framework + "/http"}
	case RuleScaffold:
		return []string{framework + "/validation", framework + "/fault"}
	case EventScaffold:
		return []string{framework + "/events"}
	case ListenerScaffold:
		return []string{framework + "/events", framework + "/fault"}
	case PolicyScaffold:
		return []string{framework + "/auth"}
	case NotificationScaffold:
		return []string{framework + "/notifications"}
	}
	return nil
}

// endpointContracts names the DTOs an endpoint scaffold declares. GET has no
// request body.
func endpointContracts(options ScaffoldOptions) (request, response string) {
	if options.Method != "GET" {
		request = options.Name + "Request"
	}
	return request, options.Name + "Response"
}

func renderComponentScaffold(pkg string, options ScaffoldOptions) (string, error) {
	switch options.Kind {
	case EndpointScaffold:
		request, response := endpointContracts(options)
		body, bodyType, status, contracts := "foundryhttp.EmptyBody()", "foundryhttp.NoBody", 200, response
		if request != "" {
			body, bodyType, contracts = "foundryhttp.JSONBody("+request+"JSON())", request, request+" and "+response
		}
		if options.Method == "POST" {
			status = 201
		}
		return fmt.Sprintf(`package %s

import (
	"context"

	%q
	foundryhttp %q
)

// %sRoute is the stable identity, method and path of this endpoint.
var %sRoute = foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: %q, Method: foundryhttp.%s, Access: foundryhttp.Public}, foundryhttp.StaticPath(%q))

// %sEndpoint connects the route to the generated %s contracts.
// Use Guarded access with an authentication transport for signed-in callers.
func %sEndpoint() foundryhttp.Endpoint[foundryhttp.NoPath, foundryhttp.NoQuery, %s, %s] {
	return foundryhttp.DefineEndpoint(%sRoute, foundryhttp.EmptyQuery(), %s, foundryhttp.JSONResponse(%d, %sJSON()))
}

// Handle%s performs the domain work. Register %sEndpoint().Handle(Handle%s)
// with the application router.
func Handle%s(ctx context.Context, input foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, %s]) (%s, error) {
	return %s{}, fault.New(fault.Invalid, %q)
}
`, pkg, framework+"/fault", framework+"/http",
			options.Name, options.Name, options.ID, options.Method, options.Path,
			options.Name, contracts, options.Name, bodyType, response, options.Name, body, status, response,
			options.Name, options.Name, options.Name, options.Name, bodyType, response, response, "endpoint "+options.ID+" has not been implemented"), nil
	case EnumScaffold:
		var cases strings.Builder
		for _, value := range options.Cases {
			fmt.Fprintf(&cases, "\t%s%s %s = %q\n", options.Name, exportedName(value), options.Name, value)
		}
		return fmt.Sprintf(`package %s

// %s is a closed set of values. Run foundry generate to create its
// validation, JSON and database codecs.
//
//foundry:enum
type %s string

const (
%s)
`, pkg, options.Name, options.Name, cases.String()), nil
	case MiddlewareScaffold:
		return fmt.Sprintf(`package %s

import (
	stdhttp "net/http"

	foundryhttp %q
)

// %sMiddleware wraps routes or scopes that list it. Its constructor runs once
// during router assembly; the wrapper must stay synchronous.
func %sMiddleware() foundryhttp.Middleware {
	return foundryhttp.DefineMiddleware(%q, func(next stdhttp.Handler) (stdhttp.Handler, error) {
		return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			// Inspect the request or wrap w here, then continue the chain.
			next.ServeHTTP(w, r)
		}), nil
	})
}
`, pkg, framework+"/http", options.Name, options.Name, options.ID), nil
	case RuleScaffold:
		return fmt.Sprintf(`package %s

import (
	"context"

	%q
	%q
)

// %sRule is a typed server validation rule. Compose it with generated
// validation fields; change string to the validated value type.
func %sRule() validation.Rule[string] {
	return validation.Custom[string](validation.Spec{ID: %q, Message: "The value is invalid."}, func(ctx context.Context, value string) (bool, error) {
		// Return false for invalid input; an error means the check itself failed.
		return false, fault.New(fault.Invalid, %q)
	})
}
`, pkg, framework+"/fault", framework+"/validation", options.Name, options.Name, options.ID, "rule "+options.ID+" has not been implemented"), nil
	case EventScaffold:
		return fmt.Sprintf(`package %s

import %q

// %s is the owned payload of the %s event. Add exported JSON fields.
type %s struct{}

// %sTopic declares the event name and payload version. Declare its listeners
// with %sTopic().Declare(...) and dispatch through the application events bus.
func %sTopic() events.Topic[%s] {
	return events.Define[%s](%q, 1)
}
`, pkg, framework+"/events", options.Name, options.ID, options.Name, options.Name, options.Name, options.Name, options.Name, options.Name, options.ID), nil
	case ListenerScaffold:
		return fmt.Sprintf(`package %s

import (
	"context"

	%q
	%q
)

// %sListener reacts to %s events. Declare it with %sTopic().Declare.
func %sListener() events.Listener[%s] {
	return events.Listen[%s](%q, Handle%s)
}

// Handle%s must respect ctx and return before its dispatch finishes.
func Handle%s(ctx context.Context, event %s) error {
	return fault.New(fault.Invalid, %q)
}
`, pkg, framework+"/events", framework+"/fault", options.Name, options.Event, options.Event, options.Name, options.Event, options.Event, options.ID, options.Name, options.Name, options.Name, options.Event, "listener "+options.ID+" has not been implemented"), nil
	case PolicyScaffold:
		return fmt.Sprintf(`package %s

import (
	"context"

	%q
)

// %sPolicy decides whether an authenticated %s may act on a %s. It denies
// until implemented; an evaluation error never grants access.
func %sPolicy() auth.Policy[%s, %s] {
	return auth.DefinePolicy[%s, %s](%q, func(ctx context.Context, subject %s, resource %s) (bool, error) {
		return false, nil
	})
}
`, pkg, framework+"/auth", options.Name, options.Subject, options.Resource, options.Name, options.Subject, options.Resource, options.Subject, options.Resource, options.ID, options.Subject, options.Resource), nil
	case NotificationScaffold:
		return fmt.Sprintf(`package %s

import %q

// %sNotification declares the versioned payload contract of the %s
// notification. Bind it to recipients and channels during assembly.
func %sNotification() notifications.Definition[%sPayload] {
	return notifications.Define[%sPayload](%q, 1, %sPayloadJSON())
}
`, pkg, framework+"/notifications", options.Name, options.ID, options.Name, options.Name, options.Name, options.ID, options.Name), nil
	}
	return "", fmt.Errorf("unsupported component scaffold")
}

// createTableSQL is the reviewed starting point of a create-table migration:
// a UUID primary key matching model.ID and managed timestamps.
func createTableSQL(table string) string {
	if table == "" {
		return ""
	}
	return "`CREATE TABLE " + table + " (\n\tid uuid PRIMARY KEY,\n\tcreated_at timestamptz NOT NULL,\n\tupdated_at timestamptz NOT NULL\n)`"
}
