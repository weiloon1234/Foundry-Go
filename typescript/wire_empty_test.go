package typescript_test

import (
	"bytes"
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"mime/multipart"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/decimal/money"
	"github.com/weiloon1234/Foundry-Go/email"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
	"github.com/weiloon1234/Foundry-Go/internal/authtransport"
	"github.com/weiloon1234/Foundry-Go/jobs/archive"
	"github.com/weiloon1234/Foundry-Go/lease"
	"github.com/weiloon1234/Foundry-Go/maintenance"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/tracing"
	"github.com/weiloon1234/Foundry-Go/typescript"
)

type wireEmptyOwner struct{}

func checkJSONContractNonEmpty[T any](t *testing.T, name string, codec contract.JSON[T], expectNonEmpty bool) {
	t.Helper()
	description, err := codec.Description()
	if err != nil {
		t.Fatalf("%s JSON contract description: %v", name, err)
	}
	var root contract.Type
	found := false
	for _, typ := range description.Types {
		if typ.ID == description.Root {
			root = typ
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("%s JSON contract missing root %q", name, description.Root)
	}
	if root.NonEmpty != expectNonEmpty {
		t.Errorf("%s JSON contract NonEmpty = %v, want %v", name, root.NonEmpty, expectNonEmpty)
	}
	limits := contract.JSONLimits{Bytes: 64, Depth: 4, Nodes: 16, Steps: 32, Issues: 8}
	_, err = codec.Decode(t.Context(), []byte(`""`), limits)
	if expectNonEmpty && err == nil {
		t.Errorf("%s JSON contract accepted empty JSON string", name)
	}
	if !expectNonEmpty && err != nil {
		t.Errorf("%s JSON contract rejected empty JSON string: %v", name, err)
	}
}

func checkURLPathsRejectEmpty[T any](t *testing.T, name string, codec foundryhttp.PathCodec[T], expectNonEmpty bool) {
	t.Helper()
	selectValue := func(v *T) *T { return v }

	// Path parameter metadata + real path codec parsing.
	pathDesc := foundryhttp.DefinePath("/{v}", foundryhttp.Param("v", codec, selectValue))
	pathParams, err := foundryhttp.PathParameters(pathDesc)
	if err != nil || len(pathParams) != 1 || pathParams[0].Scalar == nil {
		t.Fatalf("%s path metadata: %+v %v", name, pathParams, err)
	}
	if pathParams[0].Scalar.Value.NonEmpty != expectNonEmpty {
		t.Errorf("%s path metadata NonEmpty = %v, want %v", name, pathParams[0].Scalar.Value.NonEmpty, expectNonEmpty)
	}
	_, parseErr := codec.Parse("")
	if expectNonEmpty && parseErr == nil {
		t.Errorf("%s path codec accepted empty value", name)
	}
	if !expectNonEmpty && parseErr != nil {
		t.Errorf("%s path codec rejected empty value: %v", name, parseErr)
	}

	// Query parameter metadata + real HTTP query decoding for "?v=".
	queryDesc := foundryhttp.DefineQuery(foundryhttp.QueryParam("v", codec, selectValue))
	queryParams, err := foundryhttp.QueryParameters(queryDesc)
	if err != nil || len(queryParams) != 1 || queryParams[0].Scalar == nil {
		t.Fatalf("%s query metadata: %+v %v", name, queryParams, err)
	}
	if queryParams[0].Scalar.Value.NonEmpty != expectNonEmpty {
		t.Errorf("%s query metadata NonEmpty = %v, want %v", name, queryParams[0].Scalar.Value.NonEmpty, expectNonEmpty)
	}
	queryRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "query.scalar", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/q"))
	queryEndpoint := foundryhttp.DefineEndpoint(queryRoute, queryDesc, foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))

	// Multipart text part metadata + real HTTP multipart decoding for field v="".
	mpDesc := foundryhttp.MultipartDirectory(foundryhttp.DefineMultipart(
		foundryhttp.TextPart(foundryhttp.QueryParam("v", codec, selectValue)),
	), t.TempDir())
	mpInfo, err := foundryhttp.InspectMultipart(mpDesc)
	if err != nil || len(mpInfo.Parts) != 1 || mpInfo.Parts[0].Scalar == nil {
		t.Fatalf("%s multipart metadata: %+v %v", name, mpInfo, err)
	}
	if mpInfo.Parts[0].Scalar.Value.NonEmpty != expectNonEmpty {
		t.Errorf("%s multipart metadata NonEmpty = %v, want %v", name, mpInfo.Parts[0].Scalar.Value.NonEmpty, expectNonEmpty)
	}
	mpRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "multipart.scalar", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/m"))
	mpEndpoint := foundryhttp.DefineEndpoint(mpRoute, foundryhttp.EmptyQuery(), foundryhttp.MultipartBody(mpDesc), foundryhttp.EmptyResponse(204))

	router, err := foundryhttp.NewRouter(
		queryEndpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, T, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		}),
		mpEndpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, T]) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		}),
	)
	if err != nil {
		t.Fatalf("%s router: %v", name, err)
	}

	qRec := httptest.NewRecorder()
	router.ServeHTTP(qRec, httptest.NewRequest(stdhttp.MethodGet, "/q?v=", nil))
	if expectNonEmpty && qRec.Code != stdhttp.StatusBadRequest {
		t.Errorf("%s query ?v= status = %d, want 400", name, qRec.Code)
	}
	if !expectNonEmpty && qRec.Code != stdhttp.StatusNoContent {
		t.Errorf("%s query ?v= status = %d, want 204", name, qRec.Code)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("v", ""); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(stdhttp.MethodPost, "/m", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	mRec := httptest.NewRecorder()
	router.ServeHTTP(mRec, req)
	if expectNonEmpty && mRec.Code != stdhttp.StatusBadRequest {
		t.Errorf("%s multipart v=\"\" status = %d, want 400", name, mRec.Code)
	}
	if !expectNonEmpty && mRec.Code != stdhttp.StatusNoContent {
		t.Errorf("%s multipart v=\"\" status = %d, want 204", name, mRec.Code)
	}
}

func formatJSONField[T any](id contract.TypeID, format contract.Format) contract.JSON[T] {
	return contract.DefineJSONField[T](contract.Schema{
		Root:  id,
		Types: []contract.Type{{ID: id, Kind: contract.StringKind, Format: format}},
	})
}

type declaredNonEmptyCase struct {
	key       string // canonical "<pkg>.<Type>" matching AST/transport discovery
	checkJSON func(t *testing.T)
	checkURL  func(t *testing.T)
}

// declaredNonEmptyCases is the single shared table executed by
// TestDeclaredNonEmptyCodecsRejectEmptyAcrossSupportedPaths and required by
// TestFrameworkScalarCodecsDeclareNonEmpty. A framework scalar cannot be
// credited in the inventory test without an executable proof case here.
var declaredNonEmptyCases = []declaredNonEmptyCase{
	{
		key: "model.ID",
		checkJSON: func(t *testing.T) {
			var v model.ID[wireEmptyOwner]
			if err := json.Unmarshal([]byte(`""`), &v); err == nil {
				t.Error("model.ID UnmarshalJSON accepted empty string")
			}
			checkJSONContractNonEmpty(t, "model.ID", formatJSONField[model.ID[wireEmptyOwner]]("test.ModelID", contract.UUIDFormat), true)
		},
		checkURL: func(t *testing.T) {
			checkURLPathsRejectEmpty(t, "model.ID", foundryhttp.ModelIDPath[wireEmptyOwner](), true)
		},
	},
	{
		key: "auth/mfa.EnrollmentID",
		checkJSON: func(t *testing.T) {
			var v mfa.EnrollmentID[wireEmptyOwner]
			if err := json.Unmarshal([]byte(`""`), &v); err == nil {
				t.Error("mfa.EnrollmentID UnmarshalJSON accepted empty string")
			}
			checkJSONContractNonEmpty(t, "mfa.EnrollmentID", v.JSONContract(), true)
		},
	},
	{
		key: "decimal.Decimal",
		checkJSON: func(t *testing.T) {
			var v decimal.Decimal
			if err := json.Unmarshal([]byte(`""`), &v); err == nil {
				t.Error("decimal.Decimal UnmarshalJSON accepted empty string")
			}
			checkJSONContractNonEmpty(t, "decimal.Decimal", formatJSONField[decimal.Decimal]("test.Decimal", contract.DecimalFormat), true)
		},
		checkURL: func(t *testing.T) {
			checkURLPathsRejectEmpty(t, "decimal.Decimal", foundryhttp.TextPath[decimal.Decimal, *decimal.Decimal](), true)
		},
	},
	{
		key: "temporal.Date",
		checkJSON: func(t *testing.T) {
			var v temporal.Date
			if err := json.Unmarshal([]byte(`""`), &v); err == nil {
				t.Error("temporal.Date UnmarshalJSON accepted empty string")
			}
			checkJSONContractNonEmpty(t, "temporal.Date", formatJSONField[temporal.Date]("test.Date", contract.DateFormat), true)
		},
		checkURL: func(t *testing.T) {
			checkURLPathsRejectEmpty(t, "temporal.Date", foundryhttp.TextPath[temporal.Date, *temporal.Date](), true)
		},
	},
	{
		key: "temporal.Time",
		checkJSON: func(t *testing.T) {
			var v temporal.Time
			if err := json.Unmarshal([]byte(`""`), &v); err == nil {
				t.Error("temporal.Time UnmarshalJSON accepted empty string")
			}
			checkJSONContractNonEmpty(t, "temporal.Time", formatJSONField[temporal.Time]("test.Time", contract.TimeFormat), true)
		},
		checkURL: func(t *testing.T) {
			checkURLPathsRejectEmpty(t, "temporal.Time", foundryhttp.TextPath[temporal.Time, *temporal.Time](), true)
		},
	},
	{
		key: "temporal.DateTime",
		checkJSON: func(t *testing.T) {
			var v temporal.DateTime
			if err := json.Unmarshal([]byte(`""`), &v); err == nil {
				t.Error("temporal.DateTime UnmarshalJSON accepted empty string")
			}
			checkJSONContractNonEmpty(t, "temporal.DateTime", formatJSONField[temporal.DateTime]("test.DateTime", contract.DateTimeFormat), true)
		},
		checkURL: func(t *testing.T) {
			checkURLPathsRejectEmpty(t, "temporal.DateTime", foundryhttp.TextPath[temporal.DateTime, *temporal.DateTime](), true)
		},
	},
	{
		key: "time.Time",
		checkJSON: func(t *testing.T) {
			var v time.Time
			if err := json.Unmarshal([]byte(`""`), &v); err == nil {
				t.Error("time.Time UnmarshalJSON accepted empty string")
			}
			checkJSONContractNonEmpty(t, "time.Time", formatJSONField[time.Time]("test.StdTime", contract.DateTimeFormat), true)
		},
		checkURL: func(t *testing.T) {
			checkURLPathsRejectEmpty(t, "time.Time", foundryhttp.TextPath[time.Time, *time.Time](), true)
		},
	},
	{
		key: "temporal.LocalDateTime",
		checkJSON: func(t *testing.T) {
			var v temporal.LocalDateTime
			if err := json.Unmarshal([]byte(`""`), &v); err == nil {
				t.Error("temporal.LocalDateTime UnmarshalJSON accepted empty string")
			}
			checkJSONContractNonEmpty(t, "temporal.LocalDateTime", formatJSONField[temporal.LocalDateTime]("test.LocalDateTime", contract.LocalDateTimeFormat), true)
		},
		checkURL: func(t *testing.T) {
			checkURLPathsRejectEmpty(t, "temporal.LocalDateTime", foundryhttp.TextPath[temporal.LocalDateTime, *temporal.LocalDateTime](), true)
		},
	},
	{
		key: "temporal.Interval",
		checkJSON: func(t *testing.T) {
			var v temporal.Interval
			if err := json.Unmarshal([]byte(`""`), &v); err == nil {
				t.Error("temporal.Interval UnmarshalJSON accepted empty string")
			}
			checkJSONContractNonEmpty(t, "temporal.Interval", formatJSONField[temporal.Interval]("test.Interval", contract.IntervalFormat), true)
		},
		checkURL: func(t *testing.T) {
			checkURLPathsRejectEmpty(t, "temporal.Interval", foundryhttp.TextPath[temporal.Interval, *temporal.Interval](), true)
		},
	},
	{
		key: "auth/password.Plaintext",
		checkJSON: func(t *testing.T) {
			var v password.Plaintext
			if err := json.Unmarshal([]byte(`""`), &v); err == nil {
				t.Error("password.Plaintext UnmarshalJSON accepted empty string")
			}
			checkJSONContractNonEmpty(t, "password.Plaintext", v.JSONContract(), true)
		},
	},
	{
		key: "auth/mfa.TOTPCode",
		checkJSON: func(t *testing.T) {
			var v mfa.TOTPCode
			if err := json.Unmarshal([]byte(`""`), &v); err == nil {
				t.Error("mfa.TOTPCode UnmarshalJSON accepted empty string")
			}
			checkJSONContractNonEmpty(t, "mfa.TOTPCode", v.JSONContract(), true)
		},
	},
	{
		key: "auth/mfa.RecoveryCode",
		checkJSON: func(t *testing.T) {
			var v mfa.RecoveryCode
			if err := json.Unmarshal([]byte(`""`), &v); err == nil {
				t.Error("mfa.RecoveryCode UnmarshalJSON accepted empty string")
			}
			checkJSONContractNonEmpty(t, "mfa.RecoveryCode", v.JSONContract(), true)
		},
	},
	{
		key: "auth/challenge.Token",
		checkJSON: func(t *testing.T) {
			var v challenge.Token[wireEmptyOwner, challenge.PasswordReset]
			if err := json.Unmarshal([]byte(`""`), &v); err == nil {
				t.Error("challenge.Token UnmarshalJSON accepted empty string")
			}
			checkJSONContractNonEmpty(t, "challenge.Token[PasswordReset]", v.JSONContract(), true)
			var ev challenge.Token[wireEmptyOwner, challenge.EmailVerification]
			if err := json.Unmarshal([]byte(`""`), &ev); err == nil {
				t.Error("challenge.Token[EmailVerification] UnmarshalJSON accepted empty string")
			}
			checkJSONContractNonEmpty(t, "challenge.Token[EmailVerification]", ev.JSONContract(), true)
		},
	},
	{
		key: "email.Address",
		checkJSON: func(t *testing.T) {
			var v email.Address
			if err := json.Unmarshal([]byte(`""`), &v); err == nil {
				t.Error("email.Address UnmarshalJSON accepted empty string")
			}
			checkJSONContractNonEmpty(t, "email.Address", v.JSONContract(), true)
		},
		checkURL: func(t *testing.T) {
			checkURLPathsRejectEmpty(t, "email.Address", foundryhttp.TextPath[email.Address, *email.Address](), true)
		},
	},
	{
		key: "storage.ObjectKey",
		checkJSON: func(t *testing.T) {
			var v storage.ObjectKey
			if err := json.Unmarshal([]byte(`""`), &v); err == nil {
				t.Error("storage.ObjectKey UnmarshalJSON accepted empty string")
			}
			checkJSONContractNonEmpty(t, "storage.ObjectKey", v.JSONContract(), true)
		},
		checkURL: func(t *testing.T) {
			checkURLPathsRejectEmpty(t, "storage.ObjectKey", foundryhttp.TextPath[storage.ObjectKey, *storage.ObjectKey](), true)
		},
	},
	{
		key: "decimal/money.Currency",
		checkJSON: func(t *testing.T) {
			var v money.Currency
			if err := json.Unmarshal([]byte(`""`), &v); err == nil {
				t.Error("money.Currency UnmarshalJSON accepted empty string")
			}
			checkJSONContractNonEmpty(t, "money.Currency", v.JSONContract(), true)
		},
		checkURL: func(t *testing.T) {
			checkURLPathsRejectEmpty(t, "money.Currency", foundryhttp.TextPath[money.Currency, *money.Currency](), true)
		},
	},
	{
		key: "internal/authtransport.MFAChallengeCredential",
		checkJSON: func(t *testing.T) {
			var v authtransport.MFAChallengeCredential
			if err := json.Unmarshal([]byte(`""`), &v); err == nil {
				t.Error("authtransport.MFAChallengeCredential UnmarshalJSON accepted empty string")
			}
			checkJSONContractNonEmpty(t, "authtransport.MFAChallengeCredential", v.JSONContract(), true)
		},
	},
	{
		key: "internal/authtransport.RefreshCredential",
		checkJSON: func(t *testing.T) {
			var v authtransport.RefreshCredential
			if err := json.Unmarshal([]byte(`""`), &v); err == nil {
				t.Error("authtransport.RefreshCredential UnmarshalJSON accepted empty string")
			}
			checkJSONContractNonEmpty(t, "authtransport.RefreshCredential", v.JSONContract(), true)
		},
	},
	{
		key: "database/orm.Cursor",
		checkURL: func(t *testing.T) {
			checkURLPathsRejectEmpty(t, "orm.Cursor", pagination.CursorQuery[wireEmptyOwner](), true)
		},
	},
}

// TestDeclaredNonEmptyCodecsRejectEmptyAcrossSupportedPaths proves that every
// framework type whose Go contract declares NonEmpty rejects the empty wire
// value "" on every transport path it supports (JSON body, query parameter,
// path parameter and multipart text part), while []byte (base64) and plain
// string accept "" and keep NonEmpty false.
func TestDeclaredNonEmptyCodecsRejectEmptyAcrossSupportedPaths(t *testing.T) {
	t.Parallel()
	empty := []byte(`""`)

	for _, tc := range declaredNonEmptyCases {
		t.Run(tc.key, func(t *testing.T) {
			if tc.checkJSON == nil && tc.checkURL == nil {
				t.Fatalf("%s has no executable check", tc.key)
			}
			if tc.checkJSON != nil {
				tc.checkJSON(t)
			}
			if tc.checkURL != nil {
				tc.checkURL(t)
			}
		})
	}

	// Negative controls: []byte (base64) and plain string accept "" and keep NonEmpty false.
	t.Run("base64_and_string_accept_empty", func(t *testing.T) {
		var rawBytes []byte
		if err := json.Unmarshal(empty, &rawBytes); err != nil {
			t.Errorf("[]byte UnmarshalJSON rejected empty string: %v", err)
		}
		checkJSONContractNonEmpty(t, "base64", formatJSONField[[]byte]("test.Base64", contract.Base64Format), false)

		var plain string
		if err := json.Unmarshal(empty, &plain); err != nil {
			t.Errorf("string UnmarshalJSON rejected empty string: %v", err)
		}
		checkJSONContractNonEmpty(t, "string", formatJSONField[string]("test.String", ""), false)
		checkURLPathsRejectEmpty(t, "string", foundryhttp.StringPath[string](), false)
	})

	// Rendered TypeScript reads the manifest non_empty fact directly and exposes Gap B symbols.
	rendered, err := typescript.Render(clientManifest(t))
	if err != nil {
		t.Fatal(err)
	}
	text := string(rendered)
	for _, snippet := range []string{
		"readonly non_empty?: boolean;",
		"type?.non_empty === true",
		"export function decimalPlaces(",
		"readonly maxPlaces?: number;",
		"readonly malformed?: boolean;",
	} {
		if !strings.Contains(text, snippet) {
			t.Errorf("rendered TypeScript output missing %q", snippet)
		}
	}
}

func receiverTypeName(expr ast.Expr) string {
	switch node := expr.(type) {
	case *ast.StarExpr:
		return receiverTypeName(node.X)
	case *ast.IndexExpr:
		return receiverTypeName(node.X)
	case *ast.IndexListExpr:
		return receiverTypeName(node.X)
	case *ast.Ident:
		return node.Name
	default:
		return ""
	}
}

const frameworkModule = "github.com/weiloon1234/Foundry-Go"

// resolveExprTypeKey resolves a type expression to a canonical "<pkg>.<Type>"
// key when it names a framework (or stdlib time.Time) type, ignoring generic
// type parameters and built-in primitive types.
func resolveExprTypeKey(expr ast.Expr, pkgRel string, imports map[string]string, typeParams map[string]bool) string {
	switch node := expr.(type) {
	case *ast.StarExpr:
		return resolveExprTypeKey(node.X, pkgRel, imports, typeParams)
	case *ast.IndexExpr:
		return resolveExprTypeKey(node.X, pkgRel, imports, typeParams)
	case *ast.IndexListExpr:
		return resolveExprTypeKey(node.X, pkgRel, imports, typeParams)
	case *ast.Ident:
		if typeParams[node.Name] {
			return ""
		}
		switch node.Name {
		case "string", "bool", "byte", "rune", "any", "error",
			"int", "int8", "int16", "int32", "int64",
			"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
			"float32", "float64":
			return ""
		}
		return pkgRel + "." + node.Name
	case *ast.SelectorExpr:
		pkgIdent, ok := node.X.(*ast.Ident)
		if !ok {
			return ""
		}
		importPath, ok := imports[pkgIdent.Name]
		if !ok {
			return ""
		}
		if importPath == "time" && node.Sel.Name == "Time" {
			return "time.Time"
		}
		if strings.HasPrefix(importPath, frameworkModule+"/") {
			return strings.TrimPrefix(importPath, frameworkModule+"/") + "." + node.Sel.Name
		}
		return ""
	default:
		return ""
	}
}

func collectTypeParams(list *ast.FieldList) map[string]bool {
	params := map[string]bool{}
	if list == nil {
		return params
	}
	for _, field := range list.List {
		for _, name := range field.Names {
			params[name.Name] = true
		}
	}
	return params
}

func assertUnexportableInManifest[T any, P foundryhttp.QueryTextPointer[T]](t *testing.T, name string) {
	t.Helper()
	type query struct{ V T }
	codec := foundryhttp.TextQuery[T, P]()
	qDesc := foundryhttp.DefineQuery(foundryhttp.QueryParam("v", codec, func(q *query) *T { return &q.V }))
	params, err := foundryhttp.QueryParameters(qDesc)
	if err != nil || len(params) != 1 {
		t.Fatalf("%s query params: %+v %v", name, params, err)
	}
	if params[0].Scalar != nil {
		t.Fatalf("%s unexpectedly acquired automatic URL scalar metadata %+v; classify it as an exportable codec and declare NonEmpty if it rejects empty", name, params[0].Scalar)
	}
	route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "unexportable.check", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/u"))
	ep := foundryhttp.DefineEndpoint(route, qDesc, foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))
	router, err := foundryhttp.NewRouter(ep.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, query, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, nil
	}))
	if err != nil {
		t.Fatalf("%s router: %v", name, err)
	}
	if _, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router}); err == nil {
		t.Fatalf("%s exported through manifest.Build without explicit DescribeURL; audit whether it must declare NonEmpty", name)
	}
}

type codecEvidence struct {
	unmarshalJSON     bool
	unmarshalJSONFrom bool
	unmarshalText     bool
	jsonContract      bool
	hasEnumDescriptor bool
	defineScalar      bool
	defineJSONValue   bool
	describeURL       bool
	pathCodecValue    bool
	promoted          bool
}

func scanFrameworkCodecs(root string, skipDirs map[string]bool) (map[string]*codecEvidence, map[string]string, error) {
	discovered := map[string]*codecEvidence{}
	ensure := func(key string) *codecEvidence {
		entry := discovered[key]
		if entry == nil {
			entry = &codecEvidence{}
			discovered[key] = entry
		}
		return entry
	}

	// Track struct embeddings (promoted methods on distinct types) and Go type
	// aliases (same runtime type as target), plus Parse/Format PathCodecs.
	type typeRelation struct {
		targets []string
	}
	relations := map[string]*typeRelation{}
	aliasTargets := map[string]string{}
	addRelation := func(from, to string) {
		if from == "" || to == "" || from == to {
			return
		}
		rel := relations[from]
		if rel == nil {
			rel = &typeRelation{}
			relations[from] = rel
		}
		rel.targets = append(rel.targets, to)
	}
	type parseFormatInfo struct {
		parseTarget string
		hasFormat   bool
	}
	pathCodecs := map[string]*parseFormatInfo{}

	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if rel != "." && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		pkgRel := filepath.ToSlash(filepath.Dir(rel))
		imports := map[string]string{}
		for _, imp := range file.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			alias := filepath.Base(importPath)
			if imp.Name != nil && imp.Name.Name != "_" && imp.Name.Name != "." {
				alias = imp.Name.Name
			}
			imports[alias] = importPath
		}

		// 1. Collect type aliases and embedded struct fields for method promotion.
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				ownerKey := pkgRel + "." + ts.Name.Name
				typeParams := collectTypeParams(ts.TypeParams)
				if ts.Assign.IsValid() {
					// Type alias: identical runtime type to its target.
					target := resolveExprTypeKey(ts.Type, pkgRel, imports, typeParams)
					if target != "" {
						aliasTargets[ownerKey] = target
						addRelation(ownerKey, target)
					}
					continue
				}
				if st, ok := ts.Type.(*ast.StructType); ok && st.Fields != nil {
					for _, field := range st.Fields.List {
						if len(field.Names) == 0 {
							addRelation(ownerKey, resolveExprTypeKey(field.Type, pkgRel, imports, typeParams))
						}
					}
				}
			}
		}

		// 2. Collect codec methods and Parse/Format methods.
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
				continue
			}
			typeName := receiverTypeName(fn.Recv.List[0].Type)
			if typeName == "" {
				continue
			}
			key := pkgRel + "." + typeName
			typeParams := collectTypeParams(fn.Type.TypeParams)
			// Also collect receiver type parameters (e.g. func (c cursorCodec[M]) Parse(...)).
			recvExpr := fn.Recv.List[0].Type
			if star, ok := recvExpr.(*ast.StarExpr); ok {
				recvExpr = star.X
			}
			switch idx := recvExpr.(type) {
			case *ast.IndexExpr:
				if id, ok := idx.Index.(*ast.Ident); ok {
					typeParams[id.Name] = true
				}
			case *ast.IndexListExpr:
				for _, arg := range idx.Indices {
					if id, ok := arg.(*ast.Ident); ok {
						typeParams[id.Name] = true
					}
				}
			}
			switch fn.Name.Name {
			case "UnmarshalJSON":
				ensure(key).unmarshalJSON = true
			case "UnmarshalJSONFrom":
				ensure(key).unmarshalJSONFrom = true
			case "UnmarshalText":
				ensure(key).unmarshalText = true
			case "JSONContract":
				ensure(key).jsonContract = true
			case "EnumDescriptor":
				ensure(key).hasEnumDescriptor = true
			case "Parse":
				// PathCodec[V]: Parse(string) (V, error)
				if fn.Type.Params != nil && len(fn.Type.Params.List) == 1 && fn.Type.Results != nil && len(fn.Type.Results.List) == 2 {
					if target := resolveExprTypeKey(fn.Type.Results.List[0].Type, pkgRel, imports, typeParams); target != "" {
						info := pathCodecs[key]
						if info == nil {
							info = &parseFormatInfo{}
							pathCodecs[key] = info
						}
						info.parseTarget = target
					}
				}
			case "Format":
				// PathCodec[V]: Format(V) (string, error)
				if fn.Type.Params != nil && len(fn.Type.Params.List) == 1 && fn.Type.Results != nil && len(fn.Type.Results.List) == 2 {
					info := pathCodecs[key]
					if info == nil {
						info = &parseFormatInfo{}
						pathCodecs[key] = info
					}
					info.hasFormat = true
				}
			}
		}

		// 3. Collect explicit transport scalar registrations inside BOTH function
		//    bodies AND package-level var/const initializers:
		//    contract.DefineScalar[T], contract.DefineJSONValue[T], http.DescribeURL[V], nativeURLCodec[V].
		inspectCalls := func(rootNode ast.Node, typeParams map[string]bool) {
			ast.Inspect(rootNode, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				var callee ast.Expr
				var typeArg ast.Expr
				switch fun := call.Fun.(type) {
				case *ast.IndexExpr:
					callee, typeArg = fun.X, fun.Index
				case *ast.IndexListExpr:
					if len(fun.Indices) > 0 {
						callee, typeArg = fun.X, fun.Indices[0]
					}
				default:
					return true
				}
				var funcName string
				var targetPkg string
				switch c := callee.(type) {
				case *ast.Ident:
					funcName = c.Name
					targetPkg = pkgRel
				case *ast.SelectorExpr:
					if id, ok := c.X.(*ast.Ident); ok {
						funcName = c.Sel.Name
						if imp, found := imports[id.Name]; found && strings.HasPrefix(imp, frameworkModule+"/") {
							targetPkg = strings.TrimPrefix(imp, frameworkModule+"/")
						}
					}
				}
				resolved := resolveExprTypeKey(typeArg, pkgRel, imports, typeParams)
				if resolved == "" {
					return true
				}
				switch {
				case targetPkg == "contract" && funcName == "DefineScalar":
					ensure(resolved).defineScalar = true
				case targetPkg == "contract" && funcName == "DefineJSONValue":
					ensure(resolved).defineJSONValue = true
				case targetPkg == "http" && funcName == "DescribeURL":
					ensure(resolved).describeURL = true
				case targetPkg == "http" && funcName == "nativeURLCodec":
					ensure(resolved).describeURL = true
				}
				return true
			})
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Body != nil {
					inspectCalls(d.Body, collectTypeParams(d.Type.TypeParams))
				}
			case *ast.GenDecl:
				if d.Tok == token.VAR || d.Tok == token.CONST {
					for _, spec := range d.Specs {
						if vs, ok := spec.(*ast.ValueSpec); ok {
							for _, expr := range vs.Values {
								inspectCalls(expr, nil)
							}
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// Propagate Parse/Format across embedded structs and type aliases until fixpoint
	// so codecs inheriting Parse or Format from an embedded type have their effective
	// PathCodec method set discovered.
	for {
		changed := false
		for owner, rel := range relations {
			for _, target := range rel.targets {
				src := pathCodecs[target]
				if src == nil {
					continue
				}
				dst := pathCodecs[owner]
				if dst == nil {
					dst = &parseFormatInfo{}
					pathCodecs[owner] = dst
				}
				before := *dst
				if dst.parseTarget == "" && src.parseTarget != "" {
					dst.parseTarget = src.parseTarget
				}
				if src.hasFormat {
					dst.hasFormat = true
				}
				if *dst != before {
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}

	// Register decoded value types of every PathCodec (effective Parse + Format).
	for _, info := range pathCodecs {
		if info.parseTarget != "" && info.hasFormat {
			ensure(info.parseTarget).pathCodecValue = true
		}
	}

	// Propagate codec methods across embedded structs and type aliases until fixpoint.
	for {
		changed := false
		for owner, rel := range relations {
			for _, target := range rel.targets {
				src := discovered[target]
				if src == nil {
					continue
				}
				if !(src.unmarshalJSON || src.unmarshalJSONFrom || src.unmarshalText || src.jsonContract) {
					continue
				}
				dst := ensure(owner)
				before := *dst
				dst.unmarshalJSON = dst.unmarshalJSON || src.unmarshalJSON
				dst.unmarshalJSONFrom = dst.unmarshalJSONFrom || src.unmarshalJSONFrom
				dst.unmarshalText = dst.unmarshalText || src.unmarshalText
				dst.jsonContract = dst.jsonContract || src.jsonContract
				dst.promoted = true
				if *dst != before {
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}

	return discovered, aliasTargets, nil
}

// TestFrameworkScalarCodecsDeclareNonEmpty scans all non-test framework Go
// files for:
//  1. Direct codec methods (UnmarshalJSON, UnmarshalJSONFrom, UnmarshalText, JSONContract),
//  2. Promoted/inherited codec methods (including Parse/Format) via embedded structs and type aliases,
//  3. Transport PathCodec implementations (effective Parse + Format) and their decoded value types, and
//  4. Transport scalar declarations (contract.DefineScalar, contract.DefineJSONValue,
//     http.DescribeURL, http.nativeURLCodec) in both function bodies and package-level var/const initializers.
//
// Every discovered exportable framework scalar that rejects "" must appear in
// the shared declaredNonEmptyCases table (proven live across all supported
// paths by TestDeclaredNonEmptyCodecsRejectEmptyAcrossSupportedPaths). Every
// exempted empty-accepting or unexportable text codec is verified live below.
func TestFrameworkScalarCodecsDeclareNonEmpty(t *testing.T) {
	t.Parallel()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := wd
	for {
		if _, statErr := os.Stat(filepath.Join(repoRoot, "go.mod")); statErr == nil {
			break
		}
		parent := filepath.Dir(repoRoot)
		if parent == repoRoot {
			t.Fatalf("cannot locate go.mod above %s", wd)
		}
		repoRoot = parent
	}

	skipDirs := map[string]bool{
		".git":      true,
		"blueprint": true,
		"cmd":       true,
		"docs":      true,
		"examples":  true,
		"testdata":  true,
		"tests":     true,
		"tools":     true,
	}
	discovered, aliasTargets, err := scanFrameworkCodecs(repoRoot, skipDirs)
	if err != nil {
		t.Fatalf("scan framework codecs: %v", err)
	}

	// Build the declared-non-empty lookup directly from the shared executable
	// proof table (declaredNonEmptyCases). No entry can be trusted here unless
	// TestDeclaredNonEmptyCodecsRejectEmptyAcrossSupportedPaths executes it.
	declaredNonEmpty := make(map[string]bool, len(declaredNonEmptyCases))
	for _, tc := range declaredNonEmptyCases {
		declaredNonEmpty[tc.key] = true
	}

	// Framework scalar codecs that genuinely accept "". Verified live below so
	// switching any of them to reject "" fails this test immediately.
	acceptsEmpty := map[string]func() error{
		"maintenance.SecretDigest": func() error {
			var v maintenance.SecretDigest
			return v.UnmarshalText([]byte(""))
		},
		"secret.String": func() error {
			var v secret.String
			return v.UnmarshalText([]byte(""))
		},
	}

	// Public framework text codecs that cannot be request fields of a generated
	// client: they have no JSONContract() (rejected in //foundry:dto) and no
	// automatic URLScalarInfo (rejected by manifest.Build unless wrapped in an
	// explicit http.DescribeURL). Both facts are verified live below.
	nonExportableTextCodecs := map[string]func(t *testing.T){
		"database/migrate.Checksum": func(t *testing.T) {
			assertUnexportableInManifest[migrate.Checksum, *migrate.Checksum](t, "database/migrate.Checksum")
		},
		"jobs/archive.Cursor": func(t *testing.T) {
			assertUnexportableInManifest[archive.Cursor, *archive.Cursor](t, "jobs/archive.Cursor")
		},
		"lease.Token": func(t *testing.T) {
			assertUnexportableInManifest[lease.Token[wireEmptyOwner], *lease.Token[wireEmptyOwner]](t, "lease.Token")
		},
		"tracing.SpanID": func(t *testing.T) {
			assertUnexportableInManifest[tracing.SpanID, *tracing.SpanID](t, "tracing.SpanID")
		},
		"tracing.TraceID": func(t *testing.T) {
			assertUnexportableInManifest[tracing.TraceID, *tracing.TraceID](t, "tracing.TraceID")
		},
	}

	// Non-scalar codecs: JSON objects, internal encrypted cookie structs,
	// generic presence/container wrappers, and configuration map loaders
	// (none are StringKind scalar codecs).
	nonScalar := map[string]bool{
		"application.BrowserGuards":          true,
		"application.EncryptionKeys":         true,
		"application.LogChannels":            true,
		"attribution.Origin":                 true,
		"audit/record.Snapshot":              true,
		"auth/oauth.Pending":                 true,
		"decimal/money.Money":                true,
		"email.Snapshot":                     true,
		"infrastructure.Brokers":             true,
		"infrastructure.CacheStores":         true,
		"infrastructure.CredentialSources":   true,
		"infrastructure.DatabaseConnections": true,
		"infrastructure.Disks":               true,
		"infrastructure.HTTPClients":         true,
		"infrastructure.JobConnections":      true,
		"infrastructure.Mailers":             true,
		"infrastructure.RealtimeConnections": true,
		"infrastructure.RedisConnections":    true,
		"logging.ContextFields":              true,
		"maintenance.State":                  true,
		"model.Identity":                     true,
		"tracing.Context":                    true,
		"value.JSON":                         true,
		"value.List":                         true,
		"value.Nullable":                     true,
		"value.Optional":                     true,
	}

	classify := func(t *testing.T, items map[string]*codecEvidence, aliases map[string]string) []string {
		t.Helper()
		resolve := func(key string) string {
			seen := map[string]bool{}
			for next, ok := aliases[key]; ok && !seen[next]; next, ok = aliases[key] {
				seen[key] = true
				key = next
			}
			return key
		}
		var unclassified []string
		itemKeys := make([]string, 0, len(items))
		for k := range items {
			itemKeys = append(itemKeys, k)
		}
		sort.Strings(itemKeys)
		for _, key := range itemKeys {
			ev := items[key]
			switch {
			case declaredNonEmpty[key]:
				continue
			case aliases[key] != "" && declaredNonEmpty[resolve(key)]:
				continue
			case ev.hasEnumDescriptor:
				// Declared enums publish explicit wire cases; emptiness is decided from Cases.
				continue
			case acceptsEmpty[key] != nil:
				if err := acceptsEmpty[key](); err != nil {
					t.Errorf("framework scalar codec %s rejected empty value but does not declare NonEmpty: %v", key, err)
				}
			case nonExportableTextCodecs[key] != nil:
				if ev.jsonContract || ev.unmarshalJSON || ev.unmarshalJSONFrom || ev.defineScalar || ev.describeURL {
					t.Errorf("framework type %s gained exportable transport support; audit whether it rejects empty and must declare NonEmpty: true", key)
				}
				nonExportableTextCodecs[key](t)
			case nonScalar[key]:
				if ev.jsonContract || ev.defineScalar || ev.describeURL {
					t.Errorf("framework type %s declared a transport contract; audit whether it is a scalar that must declare NonEmpty: true", key)
				}
			default:
				unclassified = append(unclassified, key)
			}
		}
		return unclassified
	}

	for _, key := range classify(t, discovered, aliasTargets) {
		t.Errorf("unclassified framework codec %s (%+v): if this exportable scalar rejects empty text, declare NonEmpty: true on its Go contract and add an executable case to declaredNonEmptyCases", key, *discovered[key])
	}

	// Negative discovery fixtures in testdata/non_empty_inventory prove that a
	// framework scalar whose codec inherits Format/Parse from an embedded type
	// and/or is registered via a package-level var (DescribeURL + DefineScalar)
	// without declaring NonEmpty is discovered and fails the inventory audit.
	t.Run("discovery_fixtures", func(t *testing.T) {
		fixturesRoot := filepath.Join(repoRoot, "typescript", "testdata", "non_empty_inventory")
		cases := []struct {
			dir                string
			wantKey            string
			wantPathCodecValue bool
			wantDefineScalar   bool
		}{
			{dir: "embedded_var", wantKey: "embedded_var.SKU", wantPathCodecValue: true, wantDefineScalar: true},
			{dir: "embedded_only", wantKey: "embedded_only.Code", wantPathCodecValue: true, wantDefineScalar: false},
			{dir: "var_only", wantKey: "var_only.Token", wantPathCodecValue: false, wantDefineScalar: true},
		}
		for _, tc := range cases {
			t.Run(tc.dir, func(t *testing.T) {
				found, foundAliases, err := scanFrameworkCodecs(fixturesRoot, nil)
				if err != nil {
					t.Fatalf("scan fixtures: %v", err)
				}
				ev := found[tc.wantKey]
				if ev == nil {
					t.Fatalf("scanner missed %q (discovered keys: %v)", tc.wantKey, mapKeysEvidence(found))
				}
				if ev.pathCodecValue != tc.wantPathCodecValue || ev.defineScalar != tc.wantDefineScalar {
					t.Fatalf("%s evidence = %+v, want pathCodecValue=%v defineScalar=%v", tc.wantKey, *ev, tc.wantPathCodecValue, tc.wantDefineScalar)
				}
				unclassified := classify(t, map[string]*codecEvidence{tc.wantKey: ev}, foundAliases)
				if len(unclassified) != 1 || unclassified[0] != tc.wantKey {
					t.Fatalf("audit did not fail naming %q: got unclassified=%v", tc.wantKey, unclassified)
				}
			})
		}
	})

	// Verify every executable entry in declaredNonEmptyCases (except stdlib time.Time)
	// was actually discovered in framework source, and no stale entries remain.
	for _, tc := range declaredNonEmptyCases {
		if tc.key == "time.Time" {
			continue
		}
		if discovered[tc.key] == nil {
			t.Errorf("declaredNonEmptyCases entry %q was not discovered in framework source", tc.key)
		}
	}
	for _, key := range mapKeys(nonScalar) {
		if discovered[key] == nil {
			t.Errorf("stale entry %q in nonScalar: no codec declaration found in framework source", key)
		}
	}
	for key := range acceptsEmpty {
		if discovered[key] == nil {
			t.Errorf("stale entry %q in acceptsEmpty: no codec declaration found in framework source", key)
		}
	}
	for key := range nonExportableTextCodecs {
		if discovered[key] == nil {
			t.Errorf("stale entry %q in nonExportableTextCodecs: no codec declaration found in framework source", key)
		}
	}
}

func mapKeysEvidence(m map[string]*codecEvidence) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func mapKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
