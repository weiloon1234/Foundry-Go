package safeerror_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// These are explicit construction owners, not an exemption for their packages.
// Counts pin the reviewed sites: a new literal needs a new disposition.
var typedDiagnosticOwners = map[string]int{
	// Native lifecycle owner captures fixed text before retaining application causes.
	"internal/lifecore/failure.go#Failure#failure": 1,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"auth/errors.go#WithCause#failure": 1,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"auth/lockout/client.go#runClient#Rejection": 2,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"auth/lockout/errors.go#rejectionFailure#Rejection": 1,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"auth/lockout/errors.go#unavailableFailure#unavailable": 1,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"auth/policy_hooks.go#NewDenial#Denial": 1,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"cli/arguments.go#InvalidArguments#UsageError": 1,
	// Public constructor: untrusted until a framework owner captures it.
	"cli/arguments.go#Usage#UsageError": 1,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"contract/json.go#decodeFailure#DecodeError": 1,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"contract/json_encoding.go#encodeFailure#EncodeError": 1,
	// Typed execution/reconciliation result captured by its presentation owner.
	"database/errors.go#FoundryStatementUnknown#Error": 1,
	// Public constructor: untrusted until a framework owner captures it.
	"database/errors.go#NewError#Error": 1,
	// Typed execution/reconciliation result captured by its presentation owner.
	"database/errors.go#failure#Error": 1,
	// Typed execution/reconciliation result captured by its presentation owner.
	"database/errors.go#inspect#Error": 3,
	// Typed execution/reconciliation result captured by its presentation owner.
	"database/errors.go#scope#Error": 1,
	// Typed execution/reconciliation result captured by its presentation owner.
	"database/errors.go#wrap#Error": 1,
	// Typed execution/reconciliation result captured by its presentation owner.
	"database/migrate/progress.go#applyNonTransactional#InterruptedMigration": 5,
	// Typed execution/reconciliation result captured by its presentation owner.
	"internal/querycore/cursor_error.go#CursorInputFailure#CursorInputError": 1,
	// Typed execution/reconciliation result captured by its presentation owner.
	"database/orm/write.go#writeFailure#WriteError": 1,
	// Typed execution/reconciliation result captured by its presentation owner.
	"database/routing.go#FoundryAutocommitQuery#Error": 1,
	// Typed execution/reconciliation result captured by its presentation owner.
	"database/transaction.go#afterCommitFailure#Error": 1,
	// Typed execution/reconciliation result captured by its presentation owner.
	"database/transaction.go#runTransaction#Error": 1,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"datatable/importer/importer.go#headings#HeadingError": 1,
	// Public constructor: untrusted until a framework owner captures it.
	"fault/error.go#New#Error": 1,
	// Public constructor: untrusted until a framework owner captures it.
	"fault/error.go#Panic#Error": 1,
	// Public constructor: untrusted until a framework owner captures it.
	"fault/error.go#Wrap#Error": 1,
	// Public constructor: untrusted until a framework owner captures it.
	"http/error_declaration.go#DefineError#ErrorDeclaration": 1,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"http/errors.go#responseFailure#responseError": 1,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"http/multipart.go#multipartFailure#MultipartError": 1,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"http/query.go#queryFailure#QueryError": 1,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"http/rate_limit.go#RateLimitExceeded#rateLimitRejection": 1,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"httpclient/error.go#failure#Error": 1,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"idempotency/errors.go#failure#Error": 1,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"internal/dependency/order.go#Order#Failure": 2,
	// Shared immutable diagnostic or opaque foreign-library capsule.
	"internal/jsoncodec/stream.go#MethodError#streamFailure": 1,
	// Immutable metadata attached to an opaque native JSON callback capsule.
	"internal/jsoncodec/methods.go#invoke#callbackFailure": 1,
	// Shared immutable diagnostic or opaque foreign-library capsule.
	"internal/safeerror/diagnostic.go#Panic#owned": 1,
	// Shared immutable diagnostic or opaque foreign-library capsule.
	"internal/safeerror/diagnostic.go#Retain#owned": 1,
	// Shared immutable diagnostic or opaque foreign-library capsule.
	"internal/safeerror/diagnostic.go#Sentinel#owned": 1,
	// Shared immutable diagnostic or opaque foreign-library capsule.
	"internal/safeerror/diagnostic.go#WrapFault#owned": 2,
	// Shared immutable diagnostic or opaque foreign-library capsule.
	"internal/safeerror/error.go#Diagnostic#redacted": 1,
	// Shared immutable diagnostic or opaque foreign-library capsule.
	"internal/safeerror/error.go#Owned#owned": 1,
	// Shared immutable diagnostic or opaque foreign-library capsule.
	"internal/safeerror/error.go#Reveal#redacted": 1,
	// Shared immutable diagnostic or opaque foreign-library capsule.
	"internal/safeerror/error.go#Seal#sealed": 1,
	// Shared immutable diagnostic or opaque foreign-library capsule.
	"internal/safeerror/error.go#Wrap#redacted": 1,
	// Shared immutable diagnostic or opaque foreign-library capsule.
	"internal/safeerror/error.go#revealNode#revealed": 1,
	// Typed bounded I/O outcome captured with immutable text at construction.
	"internal/upload/batch.go#create#LimitError": 1,
	// Typed bounded I/O outcome captured with immutable text at construction.
	"internal/upload/batch.go#take#LimitError": 1,
	// Typed bounded I/O outcome captured with immutable text at construction.
	"internal/upload/capture.go#Capture#CleanupError": 1,
	// Typed bounded I/O outcome captured with immutable text at construction.
	"internal/upload/capture.go#Capture#LimitError": 1,
	// Typed bounded I/O outcome captured with immutable text at construction.
	"internal/upload/capture.go#Capture#ReadError": 2,
	// Typed bounded I/O outcome captured with immutable text at construction.
	"internal/upload/file.go#Open#LimitError": 1,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"jobs/declaration.go#payloadFailure#payloadError": 1,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"jobs/permanent.go#Permanent#permanentError": 1,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"pubsub/supervise.go#failedHandler#handlerFailure": 1,
	// Public constructor: untrusted until a framework owner captures it.
	"storage/error.go#Failure#Error": 1,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"validation/label.go#LocalizeLabels#Errors": 1,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"validation/maps.go#sortedKeys#LimitError": 1,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"validation/rule.go#complete#Errors": 1,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"validation/rule.go#take#LimitError": 2,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"validation/text.go#textValue#LimitError": 1,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"websocket/cluster_runtime.go#clusterCall#clusterError": 2,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"websocket/cluster_wire.go#publishDistributed#notPublished": 1,
	// Concrete diagnostic captured by Owned/Retain at this owner.
	"websocket/publish.go#publish#notPublished": 1,
}

func typedDiagnosticLiterals(sources []diagnosticSource) map[string]int {
	const module = "github.com/weiloon1234/Foundry-Go/"
	types := map[string]bool{}
	for _, source := range sources {
		for _, declaration := range source.file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "Error" || fn.Recv == nil {
				continue
			}
			receiver := fn.Recv.List[0].Type
			if pointer, ok := receiver.(*ast.StarExpr); ok {
				receiver = pointer.X
			}
			if name, ok := diagnosticBaseType(receiver).(*ast.Ident); ok {
				types[module+filepath.Dir(source.relative)+"."+name.Name] = true
			}
		}
	}
	sites := map[string]int{}
	for _, source := range sources {
		imports := map[string]string{}
		for _, entry := range source.file.Imports {
			path, _ := strconv.Unquote(entry.Path.Value)
			name := filepath.Base(path)
			if entry.Name != nil {
				name = entry.Name.Name
			}
			imports[name] = path
		}
		for _, declaration := range source.file.Decls {
			owner := "initializer"
			if fn, ok := declaration.(*ast.FuncDecl); ok {
				owner = fn.Name.Name
			}
			ast.Inspect(declaration, func(node ast.Node) bool {
				literal, ok := node.(*ast.CompositeLit)
				if !ok {
					return true
				}
				pkg, name := diagnosticConstructor(diagnosticBaseType(literal.Type), imports)
				if local, ok := diagnosticBaseType(literal.Type).(*ast.Ident); ok {
					pkg, name = module+filepath.Dir(source.relative), local.Name
				}
				if types[pkg+"."+name] {
					sites[source.relative+"#"+owner+"#"+name]++
				}
				return true
			})
		}
	}
	return sites
}

func checkTypedDiagnosticOwners(sources []diagnosticSource) []string {
	var violations []string
	for site, count := range typedDiagnosticLiterals(sources) {
		if typedDiagnosticOwners[site] != count {
			violations = append(violations, "typed diagnostic disposition required: "+site+" = "+strconv.Itoa(count))
		}
	}
	return violations
}

func TestTypedDiagnosticLiteralGuard(t *testing.T) {
	for _, body := range []string{
		`func f() error { return &failure{} }`,
		`func f() error { err := failure{}; return err }`,
		`var callback = func() error { return failure{} }`,
	} {
		file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", `package example; type failure struct{}; func (failure) Error() string { return "failure" }; `+body, 0)
		if err != nil {
			t.Fatal(err)
		}
		got := checkTypedDiagnosticOwners([]diagnosticSource{{"example/new.go", file}})
		if len(got) != 1 || !strings.Contains(got[0], "disposition required") {
			t.Fatal("unmarked typed literal escaped guard", got)
		}
	}
}

func diagnosticBaseType(expression ast.Expr) ast.Expr {
	switch expression := expression.(type) {
	case *ast.ParenExpr:
		return diagnosticBaseType(expression.X)
	case *ast.IndexExpr:
		return diagnosticBaseType(expression.X)
	case *ast.IndexListExpr:
		return diagnosticBaseType(expression.X)
	}
	return expression
}
