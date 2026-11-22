package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	stdhttp "net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/decimal/money"
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

var (
	permissiveJSONCalls atomic.Int64
	permissiveTextCalls atomic.Int64
	mismatchJSONCalls   atomic.Int64
	mismatchTextCalls   atomic.Int64
)

// PermissiveNonEmptyCode accepts "" in both UnmarshalJSON and UnmarshalText,
// yet declares NonEmpty: true in its Go contract. The framework must enforce
// NonEmpty at every decoding boundary BEFORE calling the type's codec.
type PermissiveNonEmptyCode string

func (c PermissiveNonEmptyCode) MarshalText() ([]byte, error) { return []byte(c), nil }
func (c *PermissiveNonEmptyCode) UnmarshalText(data []byte) error {
	permissiveTextCalls.Add(1)
	*c = PermissiveNonEmptyCode(data)
	return nil
}
func (c PermissiveNonEmptyCode) MarshalJSON() ([]byte, error) { return json.Marshal(string(c)) }
func (c *PermissiveNonEmptyCode) UnmarshalJSON(data []byte) error {
	permissiveJSONCalls.Add(1)
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	*c = PermissiveNonEmptyCode(text)
	return nil
}
func (PermissiveNonEmptyCode) JSONContract() contract.JSON[PermissiveNonEmptyCode] {
	typ := reflect.TypeFor[PermissiveNonEmptyCode]()
	id := contract.TypeID(typ.PkgPath() + "." + typ.Name())
	return contract.DefineJSONValue[PermissiveNonEmptyCode](contract.Schema{
		Root:  id,
		Types: []contract.Type{{ID: id, Kind: contract.StringKind, NonEmpty: true}},
	})
}

// StrictJSONPermissiveTextCode rejects "" in UnmarshalJSON and declares
// NonEmpty: true, while its UnmarshalText accepts "" as a default value.
// Because TextPath/TextQuery/TextPart carry the NonEmpty contract fact, the
// framework must enforce NonEmpty on text paths too before UnmarshalText runs.
type StrictJSONPermissiveTextCode string

func (c StrictJSONPermissiveTextCode) MarshalText() ([]byte, error) { return []byte(c), nil }
func (c *StrictJSONPermissiveTextCode) UnmarshalText(data []byte) error {
	mismatchTextCalls.Add(1)
	if len(data) == 0 {
		*c = "default"
		return nil
	}
	*c = StrictJSONPermissiveTextCode(data)
	return nil
}
func (c StrictJSONPermissiveTextCode) MarshalJSON() ([]byte, error) { return json.Marshal(string(c)) }
func (c *StrictJSONPermissiveTextCode) UnmarshalJSON(data []byte) error {
	mismatchJSONCalls.Add(1)
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	if text == "" {
		return fault.Invalid
	}
	*c = StrictJSONPermissiveTextCode(text)
	return nil
}
func (StrictJSONPermissiveTextCode) JSONContract() contract.JSON[StrictJSONPermissiveTextCode] {
	typ := reflect.TypeFor[StrictJSONPermissiveTextCode]()
	id := contract.TypeID(typ.PkgPath() + "." + typ.Name())
	return contract.DefineJSONValue[StrictJSONPermissiveTextCode](contract.Schema{
		Root:  id,
		Types: []contract.Type{{ID: id, Kind: contract.StringKind, NonEmpty: true}},
	})
}

type PermissiveBody struct {
	V PermissiveNonEmptyCode `json:"v"`
}

func permissiveBodyJSON() contract.JSON[PermissiveBody] {
	elemType := reflect.TypeFor[PermissiveNonEmptyCode]()
	elemID := contract.TypeID(elemType.PkgPath() + "." + elemType.Name())
	bodyType := reflect.TypeFor[PermissiveBody]()
	bodyID := contract.TypeID(bodyType.PkgPath() + "." + bodyType.Name())
	return contract.DefineJSON[PermissiveBody](contract.Schema{
		Root: bodyID,
		Types: []contract.Type{
			{ID: bodyID, Kind: contract.ObjectKind, Properties: []contract.Property{
				{Name: "v", Type: elemID, Required: true},
			}},
			contract.JSONType(elemID, (PermissiveNonEmptyCode("")).JSONContract),
		},
	})
}

type permissivePathCodec struct{ calls *atomic.Int64 }

func (c permissivePathCodec) Parse(text string) (string, error) {
	c.calls.Add(1)
	return text, nil
}
func (c permissivePathCodec) Format(value string) (string, error) { return value, nil }

func decodeHTTPFailure(t *testing.T, w *httptest.ResponseRecorder) foundryhttp.ErrorResponse {
	t.Helper()
	var resp foundryhttp.ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error response (status=%d body=%q): %v", w.Code, w.Body.String(), err)
	}
	return resp
}

func makeMultipartSingleField(t *testing.T, name, value string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if err := writer.WriteField(name, value); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, writer.FormDataContentType()
}

// TestNonEmptyEnforcedBeforeCodecAcrossAllFourPaths proves that declaring
// NonEmpty: true is enforced by the framework itself at all four decoding
// boundaries (JSON body, query parameter, path parameter, and multipart text
// part) before the type's own codec is invoked, with the standard HTTP 400
// bad_request and contract.ValueIssue ("value") at the right pointer path.
func TestNonEmptyEnforcedBeforeCodecAcrossAllFourPaths(t *testing.T) {
	permissiveJSONCalls.Store(0)
	permissiveTextCalls.Store(0)
	mismatchJSONCalls.Store(0)
	mismatchTextCalls.Store(0)

	type singlePermissive struct{ V PermissiveNonEmptyCode }
	type singleMismatch struct{ V StrictJSONPermissiveTextCode }

	bodyRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "nonempty.body", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/body"))
	bodyEP := foundryhttp.DefineEndpoint(bodyRoute, foundryhttp.EmptyQuery(), foundryhttp.JSONBody(permissiveBodyJSON()), foundryhttp.EmptyResponse(204))

	queryRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "nonempty.query", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/query"))
	queryDesc := foundryhttp.DefineQuery(foundryhttp.QueryParam("v", foundryhttp.TextQuery[PermissiveNonEmptyCode, *PermissiveNonEmptyCode](), func(q *singlePermissive) *PermissiveNonEmptyCode { return &q.V }))
	queryEP := foundryhttp.DefineEndpoint(queryRoute, queryDesc, foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))

	pathDesc := foundryhttp.DefinePath("/path/{v...}", foundryhttp.Param("v", foundryhttp.TextPath[PermissiveNonEmptyCode, *PermissiveNonEmptyCode](), func(p *singlePermissive) *PermissiveNonEmptyCode { return &p.V }))
	pathRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "nonempty.path", Method: foundryhttp.GET, Access: foundryhttp.Public}, pathDesc)
	pathEP := foundryhttp.DefineEndpoint(pathRoute, foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))

	mpRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "nonempty.multipart", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/multipart"))
	mpDesc := foundryhttp.MultipartDirectory(foundryhttp.DefineMultipart(
		foundryhttp.TextPart(foundryhttp.QueryParam("v", foundryhttp.TextQuery[PermissiveNonEmptyCode, *PermissiveNonEmptyCode](), func(b *singlePermissive) *PermissiveNonEmptyCode { return &b.V })),
	), t.TempDir())
	mpEP := foundryhttp.DefineEndpoint(mpRoute, foundryhttp.EmptyQuery(), foundryhttp.MultipartBody(mpDesc), foundryhttp.EmptyResponse(204))

	// Mismatched type (strict JSON, permissive text) on query, path, multipart.
	mmQueryRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "mismatch.query", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/mm-query"))
	mmQueryDesc := foundryhttp.DefineQuery(foundryhttp.QueryParam("v", foundryhttp.TextQuery[StrictJSONPermissiveTextCode, *StrictJSONPermissiveTextCode](), func(q *singleMismatch) *StrictJSONPermissiveTextCode { return &q.V }))
	mmQueryEP := foundryhttp.DefineEndpoint(mmQueryRoute, mmQueryDesc, foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))

	mmPathDesc := foundryhttp.DefinePath("/mm-path/{v...}", foundryhttp.Param("v", foundryhttp.TextPath[StrictJSONPermissiveTextCode, *StrictJSONPermissiveTextCode](), func(p *singleMismatch) *StrictJSONPermissiveTextCode { return &p.V }))
	mmPathRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "mismatch.path", Method: foundryhttp.GET, Access: foundryhttp.Public}, mmPathDesc)
	mmPathEP := foundryhttp.DefineEndpoint(mmPathRoute, foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))

	mmMPRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "mismatch.multipart", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/mm-multipart"))
	mmMPDesc := foundryhttp.MultipartDirectory(foundryhttp.DefineMultipart(
		foundryhttp.TextPart(foundryhttp.QueryParam("v", foundryhttp.TextQuery[StrictJSONPermissiveTextCode, *StrictJSONPermissiveTextCode](), func(b *singleMismatch) *StrictJSONPermissiveTextCode { return &b.V })),
	), t.TempDir())
	mmMPEP := foundryhttp.DefineEndpoint(mmMPRoute, foundryhttp.EmptyQuery(), foundryhttp.MultipartBody(mmMPDesc), foundryhttp.EmptyResponse(204))

	router, err := foundryhttp.NewRouter(
		bodyEP.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, PermissiveBody]) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		}),
		queryEP.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, singlePermissive, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		}),
		pathEP.Handle(func(context.Context, foundryhttp.Input[singlePermissive, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		}),
		mpEP.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, singlePermissive]) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		}),
		mmQueryEP.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, singleMismatch, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		}),
		mmPathEP.Handle(func(context.Context, foundryhttp.Input[singleMismatch, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		}),
		mmMPEP.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, singleMismatch]) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	assertRejected := func(t *testing.T, name string, w *httptest.ResponseRecorder, wantPath string) {
		t.Helper()
		if w.Code != stdhttp.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400 (body=%s)", name, w.Code, w.Body.String())
		}
		failure := decodeHTTPFailure(t, w)
		if failure.Code != foundryhttp.BadRequest {
			t.Fatalf("%s: error_code = %q, want %q", name, failure.Code, foundryhttp.BadRequest)
		}
		want := []contract.Issue{{Path: wantPath, Code: contract.ValueIssue}}
		if !reflect.DeepEqual(failure.Issues, want) {
			t.Fatalf("%s: issues = %+v, want %+v", name, failure.Issues, want)
		}
	}

	// 1. Permissive codec declaring NonEmpty: rejected BEFORE codec is called on all 4 paths.
	{
		req := httptest.NewRequest(stdhttp.MethodPost, "/body", strings.NewReader(`{"v":""}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assertRejected(t, "permissive JSON body", w, "/body/v")
		if got := permissiveJSONCalls.Load(); got != 0 {
			t.Fatalf("JSON codec was called %d times on empty NonEmpty value", got)
		}
	}
	{
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(stdhttp.MethodGet, "/query?v=", nil))
		assertRejected(t, "permissive query", w, "/query/v")
		if got := permissiveTextCalls.Load(); got != 0 {
			t.Fatalf("query text codec was called %d times on empty NonEmpty value", got)
		}
	}
	{
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(stdhttp.MethodGet, "/path/", nil))
		assertRejected(t, "permissive catch-all path", w, "/path/v")
		if got := permissiveTextCalls.Load(); got != 0 {
			t.Fatalf("path text codec was called %d times on empty NonEmpty value", got)
		}
	}
	{
		buf, ct := makeMultipartSingleField(t, "v", "")
		req := httptest.NewRequest(stdhttp.MethodPost, "/multipart", buf)
		req.Header.Set("Content-Type", ct)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assertRejected(t, "permissive multipart text", w, "/body/v")
		if got := permissiveTextCalls.Load(); got != 0 {
			t.Fatalf("multipart text codec was called %d times on empty NonEmpty value", got)
		}
	}

	// Also prove DescribeURL enforces NonEmpty before custom Parse is invoked.
	var customCalls atomic.Int64
	described := foundryhttp.DescribeURL(
		permissivePathCodec{calls: &customCalls},
		contract.DefineScalar[string](contract.Type{ID: "app.NonEmptyCode", Kind: contract.StringKind, NonEmpty: true}),
	)
	if _, err := described.Parse(""); err == nil || customCalls.Load() != 0 {
		t.Fatalf("DescribeURL failed to reject empty before Parse: err=%v calls=%d", err, customCalls.Load())
	}
	if _, err := described.Format(""); err == nil {
		t.Fatal("DescribeURL Format accepted empty output for NonEmpty scalar")
	}
	if v, err := described.Parse("ok"); err != nil || v != "ok" || customCalls.Load() != 1 {
		t.Fatalf("DescribeURL valid Parse failed: v=%q err=%v calls=%d", v, err, customCalls.Load())
	}

	// Valid non-empty input still succeeds and calls the permissive codec on all 4 paths.
	{
		req := httptest.NewRequest(stdhttp.MethodPost, "/body", strings.NewReader(`{"v":"ok"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != stdhttp.StatusNoContent || permissiveJSONCalls.Load() != 1 {
			t.Fatalf("valid JSON body failed: status=%d calls=%d", w.Code, permissiveJSONCalls.Load())
		}
		w = httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(stdhttp.MethodGet, "/query?v=ok", nil))
		if w.Code != stdhttp.StatusNoContent {
			t.Fatalf("valid query failed: status=%d", w.Code)
		}
		w = httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(stdhttp.MethodGet, "/path/ok", nil))
		if w.Code != stdhttp.StatusNoContent {
			t.Fatalf("valid path failed: status=%d", w.Code)
		}
		buf, ct := makeMultipartSingleField(t, "v", "ok")
		req = httptest.NewRequest(stdhttp.MethodPost, "/multipart", buf)
		req.Header.Set("Content-Type", ct)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != stdhttp.StatusNoContent || permissiveTextCalls.Load() != 3 {
			t.Fatalf("valid multipart failed: status=%d textCalls=%d", w.Code, permissiveTextCalls.Load())
		}
	}

	// 2. Mismatched codec (strict JSON, permissive UnmarshalText): text paths reject "" BEFORE UnmarshalText runs.
	{
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(stdhttp.MethodGet, "/mm-query?v=", nil))
		assertRejected(t, "mismatch query", w, "/query/v")

		w = httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(stdhttp.MethodGet, "/mm-path/", nil))
		assertRejected(t, "mismatch path", w, "/path/v")

		buf, ct := makeMultipartSingleField(t, "v", "")
		req := httptest.NewRequest(stdhttp.MethodPost, "/mm-multipart", buf)
		req.Header.Set("Content-Type", ct)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assertRejected(t, "mismatch multipart", w, "/body/v")

		if got := mismatchTextCalls.Load(); got != 0 {
			t.Fatalf("mismatched UnmarshalText was called %d times on empty NonEmpty value", got)
		}
	}
}

type frameworkErrorShapeOwner struct{}

// TestFrameworkNonEmptyErrorShapeMatchesCodecRejection verifies that framework
// types produce the exact same HTTP status (400), error_code ("bad_request"),
// and field issue code (contract.ValueIssue / "value") on empty wire input as
// on a codec-rejected non-empty invalid value across JSON, query, path, and
// multipart text boundaries.
type FrameworkJSONShapeBody struct {
	ID       model.ID[frameworkErrorShapeOwner] `json:"id"`
	Amount   decimal.Decimal                    `json:"amount"`
	Password password.Plaintext                 `json:"password"`
	Code     mfa.TOTPCode                       `json:"code"`
}

func TestFrameworkNonEmptyErrorShapeMatchesCodecRejection(t *testing.T) {
	checkURLShape := func(t *testing.T, name string, codec foundryhttp.PathCodec[any], badValue string) {
		t.Helper()
		_ = codec
		_ = badValue
	}
	_ = checkURLShape

	assertURLMatches := func(t *testing.T, name string, router *foundryhttp.Router, queryBad, pathBad, mpBad string) {
		t.Helper()
		run := func(req *stdhttp.Request) (int, foundryhttp.ErrorResponse) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			return w.Code, decodeHTTPFailure(t, w)
		}
		// Query: empty vs non-empty invalid
		emptyCode, emptyResp := run(httptest.NewRequest(stdhttp.MethodGet, "/q?v=", nil))
		badCode, badResp := run(httptest.NewRequest(stdhttp.MethodGet, "/q?v="+queryBad, nil))
		if emptyCode != stdhttp.StatusBadRequest || badCode != stdhttp.StatusBadRequest || emptyResp.Code != badResp.Code || !reflect.DeepEqual(emptyResp.Issues, badResp.Issues) {
			t.Fatalf("%s query mismatch: empty=(%d %+v) bad=(%d %+v)", name, emptyCode, emptyResp, badCode, badResp)
		}
		// Path catch-all: empty vs non-empty invalid
		emptyCode, emptyResp = run(httptest.NewRequest(stdhttp.MethodGet, "/p/", nil))
		badCode, badResp = run(httptest.NewRequest(stdhttp.MethodGet, "/p/"+pathBad, nil))
		if emptyCode != stdhttp.StatusBadRequest || badCode != stdhttp.StatusBadRequest || emptyResp.Code != badResp.Code || !reflect.DeepEqual(emptyResp.Issues, badResp.Issues) {
			t.Fatalf("%s path mismatch: empty=(%d %+v) bad=(%d %+v)", name, emptyCode, emptyResp, badCode, badResp)
		}
		// Multipart text: empty vs non-empty invalid
		buf1, ct1 := makeMultipartSingleField(t, "v", "")
		req1 := httptest.NewRequest(stdhttp.MethodPost, "/m", buf1)
		req1.Header.Set("Content-Type", ct1)
		emptyCode, emptyResp = run(req1)
		buf2, ct2 := makeMultipartSingleField(t, "v", mpBad)
		req2 := httptest.NewRequest(stdhttp.MethodPost, "/m", buf2)
		req2.Header.Set("Content-Type", ct2)
		badCode, badResp = run(req2)
		if emptyCode != stdhttp.StatusBadRequest || badCode != stdhttp.StatusBadRequest || emptyResp.Code != badResp.Code || !reflect.DeepEqual(emptyResp.Issues, badResp.Issues) {
			t.Fatalf("%s multipart mismatch: empty=(%d %+v) bad=(%d %+v)", name, emptyCode, emptyResp, badCode, badResp)
		}
	}

	buildURLRouter := func(t *testing.T, register func(*testing.T) (*foundryhttp.Router, error)) *foundryhttp.Router {
		t.Helper()
		r, err := register(t)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	assertURLMatches(t, "model.ID", buildURLRouter(t, func(t *testing.T) (*foundryhttp.Router, error) {
		return urlShapeRouter(t, foundryhttp.ModelIDPath[frameworkErrorShapeOwner]())
	}), "not-a-uuid", "not-a-uuid", "not-a-uuid")
	assertURLMatches(t, "decimal.Decimal", buildURLRouter(t, func(t *testing.T) (*foundryhttp.Router, error) {
		return urlShapeRouter(t, foundryhttp.TextPath[decimal.Decimal, *decimal.Decimal]())
	}), "abc", "abc", "abc")
	assertURLMatches(t, "temporal.Date", buildURLRouter(t, func(t *testing.T) (*foundryhttp.Router, error) {
		return urlShapeRouter(t, foundryhttp.TextPath[temporal.Date, *temporal.Date]())
	}), "not-a-date", "not-a-date", "not-a-date")
	assertURLMatches(t, "email.Address", buildURLRouter(t, func(t *testing.T) (*foundryhttp.Router, error) {
		return urlShapeRouter(t, foundryhttp.TextPath[email.Address, *email.Address]())
	}), "not-an-email", "not-an-email", "not-an-email")
	assertURLMatches(t, "storage.ObjectKey", buildURLRouter(t, func(t *testing.T) (*foundryhttp.Router, error) {
		return urlShapeRouter(t, foundryhttp.TextPath[storage.ObjectKey, *storage.ObjectKey]())
	}), "..", "bad%5Ckey", "..")
	assertURLMatches(t, "money.Currency", buildURLRouter(t, func(t *testing.T) (*foundryhttp.Router, error) {
		return urlShapeRouter(t, foundryhttp.TextPath[money.Currency, *money.Currency]())
	}), "XXX", "XXX", "XXX")
	assertURLMatches(t, "orm.Cursor", buildURLRouter(t, func(t *testing.T) (*foundryhttp.Router, error) {
		return urlShapeRouter(t, pagination.CursorQuery[frameworkErrorShapeOwner]())
	}), "not-a-cursor", "not-a-cursor", "not-a-cursor")

	// JSON body: formatted types (model.ID, decimal.Decimal) produce identical status, error_code, and ValueIssue;
	// non-formatted JSONContract types (password.Plaintext, mfa.TOTPCode) keep HTTP 400 + bad_request and now
	// attach the standard field-level ValueIssue at /body/<field>.
	pwdType := reflect.TypeFor[password.Plaintext]()
	pwdID := contract.TypeID(pwdType.PkgPath() + "." + pwdType.Name())
	codeType := reflect.TypeFor[mfa.TOTPCode]()
	codeID := contract.TypeID(codeType.PkgPath() + "." + codeType.Name())
	bodyType := reflect.TypeFor[FrameworkJSONShapeBody]()
	bodyID := contract.TypeID(bodyType.PkgPath() + "." + bodyType.Name())
	bodyContract := contract.DefineJSON[FrameworkJSONShapeBody](contract.Schema{
		Root: bodyID,
		Types: []contract.Type{
			{ID: bodyID, Kind: contract.ObjectKind, Properties: []contract.Property{
				{Name: "id", Type: "test.ID", Required: true},
				{Name: "amount", Type: "test.Amount", Required: true},
				{Name: "password", Type: pwdID, Required: true},
				{Name: "code", Type: codeID, Required: true},
			}},
			{ID: "test.ID", Kind: contract.StringKind, Format: contract.UUIDFormat},
			{ID: "test.Amount", Kind: contract.StringKind, Format: contract.DecimalFormat},
			contract.JSONType(pwdID, (password.Plaintext{}).JSONContract),
			contract.JSONType(codeID, (mfa.TOTPCode{}).JSONContract),
		},
	})
	route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "json.shape", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/json"))
	ep := foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), foundryhttp.JSONBody(bodyContract), foundryhttp.EmptyResponse(204))
	router, err := foundryhttp.NewRouter(ep.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, FrameworkJSONShapeBody]) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	postJSON := func(payload string) (int, foundryhttp.ErrorResponse) {
		req := httptest.NewRequest(stdhttp.MethodPost, "/json", strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w.Code, decodeHTTPFailure(t, w)
	}
	validBase := `{"id":"0193fd8c-2075-7000-8000-000000000001","amount":"12.50","password":"secret","code":"123456"}`
	// Formatted types: empty vs non-empty invalid match identically.
	for _, tc := range []struct {
		field, valid, bad string
	}{
		{"id", `"0193fd8c-2075-7000-8000-000000000001"`, `"not-a-uuid"`},
		{"amount", `"12.50"`, `"abc"`},
	} {
		c1, r1 := postJSON(strings.Replace(validBase, tc.valid, `""`, 1))
		c2, r2 := postJSON(strings.Replace(validBase, tc.valid, tc.bad, 1))
		if c1 != stdhttp.StatusBadRequest || c2 != stdhttp.StatusBadRequest || r1.Code != foundryhttp.BadRequest || r1.Code != r2.Code || !reflect.DeepEqual(r1.Issues, r2.Issues) {
			t.Fatalf("JSON %s mismatch: empty=(%d %+v) bad=(%d %+v)", tc.field, c1, r1, c2, r2)
		}
	}
	// Non-formatted JSONContract types: HTTP 400 + bad_request preserved, with standard ValueIssue at /body/<field>.
	for _, tc := range []struct {
		field, valid string
	}{
		{"password", `"secret"`},
		{"code", `"123456"`},
	} {
		status, resp := postJSON(strings.Replace(validBase, tc.valid, `""`, 1))
		want := []contract.Issue{{Path: "/body/" + tc.field, Code: contract.ValueIssue}}
		if status != stdhttp.StatusBadRequest || resp.Code != foundryhttp.BadRequest || !reflect.DeepEqual(resp.Issues, want) {
			t.Fatalf("JSON %s empty: status=%d resp=%+v want issues=%+v", tc.field, status, resp, want)
		}
	}
}

func urlShapeRouter[T any](t *testing.T, codec foundryhttp.PathCodec[T]) (*foundryhttp.Router, error) {
	t.Helper()
	type box struct{ V T }
	qRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "shape.query", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/q"))
	qDesc := foundryhttp.DefineQuery(foundryhttp.QueryParam("v", codec, func(b *box) *T { return &b.V }))
	qEP := foundryhttp.DefineEndpoint(qRoute, qDesc, foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))

	pDesc := foundryhttp.DefinePath("/p/{v...}", foundryhttp.Param("v", codec, func(b *box) *T { return &b.V }))
	pRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "shape.path", Method: foundryhttp.GET, Access: foundryhttp.Public}, pDesc)
	pEP := foundryhttp.DefineEndpoint(pRoute, foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.EmptyResponse(204))

	mRoute := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "shape.multipart", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/m"))
	mDesc := foundryhttp.MultipartDirectory(foundryhttp.DefineMultipart(
		foundryhttp.TextPart(foundryhttp.QueryParam("v", codec, func(b *box) *T { return &b.V })),
	), t.TempDir())
	mEP := foundryhttp.DefineEndpoint(mRoute, foundryhttp.EmptyQuery(), foundryhttp.MultipartBody(mDesc), foundryhttp.EmptyResponse(204))

	return foundryhttp.NewRouter(
		qEP.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, box, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		}),
		pEP.Handle(func(context.Context, foundryhttp.Input[box, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		}),
		mEP.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, box]) (foundryhttp.NoContent, error) {
			return foundryhttp.NoContent{}, nil
		}),
	)
}

var _ orm.Cursor[frameworkErrorShapeOwner]
