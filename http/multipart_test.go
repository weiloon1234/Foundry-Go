package http

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	stdhttp "net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type multipartRequest struct {
	Name    value.Optional[string]
	Count   int
	Active  bool
	Tags    []string
	Primary UploadedFile
	Avatar  value.Optional[UploadedFile]
	Photos  []UploadedFile
	Details value.Optional[EndpointPatch]
}
type multipartRequestInput = Input[NoPath, NoQuery, multipartRequest]

func multipartForm(directory string) Multipart[multipartRequest] {
	return DefineMultipart(
		TextPart(OptionalQueryParam("name", StringQuery[string](), func(b *multipartRequest) *value.Optional[string] { return &b.Name })),
		TextPart(DefaultQueryParam("count", IntegerQuery[int](), 9, func(b *multipartRequest) *int { return &b.Count })),
		TextPart(DefaultQueryParam("active", BoolQuery[bool](), true, func(b *multipartRequest) *bool { return &b.Active })),
		TextPart(RepeatedQueryParam("tags[]", StringQuery[string](), func(b *multipartRequest) *[]string { return &b.Tags })),
		FilePart("primary", func(b *multipartRequest) *UploadedFile { return &b.Primary }),
		OptionalFilePart("avatar", func(b *multipartRequest) *value.Optional[UploadedFile] { return &b.Avatar }),
		RepeatedFilePart("photos", func(b *multipartRequest) *[]UploadedFile { return &b.Photos }),
		OptionalJSONPart("details", endpointPatchJSON(), func(b *multipartRequest) *value.Optional[EndpointPatch] { return &b.Details }),
	).WithTempDirectory(directory)
}

func multipartEndpoint(directory string) Endpoint[NoPath, NoQuery, multipartRequest, NoContent] {
	route := DefineRoute(RouteSpec{ID: "uploads.store", Method: POST, Access: Public}, StaticPath("/uploads"))
	return DefineEndpoint(route, EmptyQuery(), MultipartBody(multipartForm(directory)), EmptyResponse(204))
}

type multipartTestPart struct {
	name, filename, media, contents string
	file                            bool
	headers                         textproto.MIMEHeader
}

func multipartWire(t *testing.T, parts ...multipartTestPart) ([]byte, string) {
	t.Helper()
	var wire bytes.Buffer
	writer := multipart.NewWriter(&wire)
	for _, part := range parts {
		parameters := map[string]string{"name": part.name}
		if part.file {
			parameters["filename"] = part.filename
		}
		header := textproto.MIMEHeader{"Content-Disposition": []string{mime.FormatMediaType("form-data", parameters)}}
		if part.media != "" {
			header.Set("Content-Type", part.media)
		}
		for name, values := range part.headers {
			header[name] = values
		}
		target, err := writer.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(target, part.contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return wire.Bytes(), writer.FormDataContentType()
}
func primaryUpload(contents string) multipartTestPart {
	return multipartTestPart{name: "primary", filename: `C:\fakepath\primary.TXT`, file: true, media: "application/octet-stream", contents: contents}
}
func assertMultipartCleanup(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Errorf("request retained temporary resources: count=%d error=%v", len(entries), err)
	}
}
func submitMultipart(t *testing.T, router *Router, wire []byte, media string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest("POST", "/uploads", bytes.NewReader(wire))
	request.Header.Set("Content-Type", media)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestMultipartTypedPresenceAndRequestOwnership(t *testing.T) {
	directory := t.TempDir()
	endpoint := multipartEndpoint(directory)
	var retained UploadedFile
	var retainedReader io.ReadSeekCloser
	called := 0
	router, err := NewRouter(endpoint.Handle(func(ctx context.Context, input multipartRequestInput) (NoContent, error) {
		called++
		body := input.Body
		retained = body.Primary
		name, present := body.Name.Get()
		if !present || name != "x+y%20z" || body.Count != 0 || body.Active {
			t.Error("scalar omission, zero, false or raw form spelling changed")
		}
		if len(body.Tags) != 2 || body.Tags[0] != "one" || body.Tags[1] != "two" {
			t.Error("repeated text order changed")
		}
		avatar, present := body.Avatar.Get()
		if !present || avatar.IsZero() || avatar.Size() != 0 || avatar.Name() != "upload" {
			t.Error("empty filename/content became absence")
		}
		if len(body.Photos) != 2 || body.Photos[0].Name() != "first.txt" || body.Photos[1].Name() != "second.txt" {
			t.Error("repeated file order changed")
		}
		details, present := body.Details.Get()
		note, supplied := details.Note.Get()
		if !present || details.Name != "json" || !supplied || !note.IsNull() {
			t.Error("structured JSON part lost nullability")
		}
		if body.Primary.Name() != "primary.TXT" || body.Primary.ContentType() != "text/plain; charset=utf-8" || body.Primary.ClientContentType() != "application/octet-stream" {
			t.Error("file metadata or sniff was confused with client media")
		}
		var err error
		retainedReader, err = body.Primary.Open(ctx)
		if err != nil {
			return NoContent{}, err
		}
		data, err := io.ReadAll(retainedReader)
		if err != nil || string(data) != "primary contents" {
			t.Error("captured file bytes changed")
		}
		return NoContent{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	wire, media := multipartWire(t, primaryUpload("primary contents"),
		multipartTestPart{name: "name", contents: "x+y%20z"}, multipartTestPart{name: "count", contents: "0"}, multipartTestPart{name: "active", contents: "false"},
		multipartTestPart{name: "tags[]", contents: "one"}, multipartTestPart{name: "tags[]", contents: "two"},
		multipartTestPart{name: "avatar", file: true},
		multipartTestPart{name: "photos", filename: "first.txt", file: true}, multipartTestPart{name: "photos", filename: "second.txt", file: true},
		multipartTestPart{name: "details", media: "application/json", contents: `{"name":"json","note":null}`})
	response := submitMultipart(t, router, wire, media)
	if response.Code != 204 || called != 1 {
		t.Fatalf("response %d: %s", response.Code, response.Body)
	}
	assertMultipartCleanup(t, directory)
	if _, err := retained.Open(context.Background()); !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("upload escaped request lifetime: %v", err)
	}
	if _, err := retainedReader.Read(make([]byte, 1)); !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("reader escaped request lifetime: %v", err)
	}
}

func TestMultipartOmittedFieldsUseTypedDefaults(t *testing.T) {
	directory := t.TempDir()
	router, err := NewRouter(multipartEndpoint(directory).Handle(func(_ context.Context, input multipartRequestInput) (NoContent, error) {
		if _, present := input.Body.Name.Get(); present {
			t.Error("omitted text is present")
		}
		if _, present := input.Body.Avatar.Get(); present {
			t.Error("omitted file is present")
		}
		if _, present := input.Body.Details.Get(); present {
			t.Error("omitted JSON is present")
		}
		if input.Body.Count != 9 || !input.Body.Active || input.Body.Photos != nil || input.Body.Tags != nil {
			t.Error("omission defaults or slice states changed")
		}
		return NoContent{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	wire, media := multipartWire(t, primaryUpload(""))
	response := submitMultipart(t, router, wire, media)
	if response.Code != 204 {
		t.Fatalf("response %d: %s", response.Code, response.Body)
	}
	assertMultipartCleanup(t, directory)
}

func TestMultipartMalformedPartsRejectBeforeHandlerAndCleanUp(t *testing.T) {
	for _, test := range []struct {
		name  string
		parts []multipartTestPart
		issue string
	}{
		{"missing-required", nil, "/body/primary"},
		{"unknown", []multipartTestPart{primaryUpload("bytes"), {name: "private-input", contents: "secret"}}, "/body"},
		{"duplicate-file", []multipartTestPart{primaryUpload("first"), primaryUpload("second")}, "/body/primary"},
		{"duplicate-text", []multipartTestPart{primaryUpload("bytes"), {name: "name", contents: "first"}, {name: "name", contents: "second"}}, "/body/name"},
		{"file-as-text", []multipartTestPart{{name: "primary", contents: "text"}}, "/body/primary"},
		{"text-as-file", []multipartTestPart{primaryUpload("bytes"), {name: "name", filename: "name", file: true}}, "/body/name"},
		{"invalid-scalar", []multipartTestPart{primaryUpload("bytes"), {name: "count", contents: "typo"}}, "/body/count"},
		{"invalid-utf8", []multipartTestPart{primaryUpload("bytes"), {name: "name", contents: "bad\xff"}}, "/body/name"},
		{"invalid-json", []multipartTestPart{primaryUpload("bytes"), {name: "details", media: "application/json", contents: `{"name":12}`}}, "/body/details/name"},
		{"json-media", []multipartTestPart{primaryUpload("bytes"), {name: "details", media: "text/plain", contents: `{"name":"value"}`}}, "/body/details"},
		{"text-charset", []multipartTestPart{primaryUpload("bytes"), {name: "name", media: "text/plain; charset=latin1", contents: "value"}}, "/body/name"},
		{"transfer-encoding", []multipartTestPart{primaryUpload("bytes"), {name: "name", contents: "=41", headers: textproto.MIMEHeader{"Content-Transfer-Encoding": []string{"quoted-printable"}}}}, "/body/name"},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			called := false
			router, err := NewRouter(multipartEndpoint(directory).Handle(func(context.Context, multipartRequestInput) (NoContent, error) {
				called = true
				return NoContent{}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			wire, media := multipartWire(t, test.parts...)
			response := submitMultipart(t, router, wire, media)
			if response.Code != 400 || called || !strings.Contains(response.Body.String(), test.issue) {
				t.Fatalf("response %d, handler=%v: %s", response.Code, called, response.Body)
			}
			if strings.Contains(response.Body.String(), "private-input") || strings.Contains(response.Body.String(), "secret") {
				t.Fatal("submitted data leaked into public error")
			}
			assertMultipartCleanup(t, directory)
		})
	}
}

func TestMultipartBodyAndFieldResourceLimits(t *testing.T) {
	for _, test := range []struct {
		name    string
		parts   []multipartTestPart
		change  func(*EndpointLimits)
		chunked bool
	}{
		{"file-bytes", []multipartTestPart{primaryUpload("1234")}, func(l *EndpointLimits) { l.Multipart.FileBytes = 3 }, false},
		{"field-bytes", []multipartTestPart{primaryUpload(""), {name: "name", contents: "1234"}}, func(l *EndpointLimits) { l.Multipart.FieldBytes = 3 }, false},
		{"total-fields", []multipartTestPart{primaryUpload(""), {name: "name", contents: "123"}, {name: "tags[]", contents: "456"}}, func(l *EndpointLimits) { l.Multipart.FieldBytes = 4; l.Multipart.FieldsBytes = 5 }, false},
		{"files", []multipartTestPart{primaryUpload(""), {name: "avatar", file: true}}, func(l *EndpointLimits) { l.Multipart.Files = 1 }, false},
		{"parts", []multipartTestPart{primaryUpload(""), {name: "name", contents: "one"}}, func(l *EndpointLimits) { l.Multipart.Parts = 1; l.Multipart.Files = 1 }, false},
		{"encoded-known", []multipartTestPart{primaryUpload(strings.Repeat("x", 1500))}, func(l *EndpointLimits) {
			l.Multipart.Bytes = 1024
			l.Multipart.FileBytes = 1024
			l.Multipart.FieldBytes = 256
			l.Multipart.FieldsBytes = 256
		}, false},
		{"encoded-chunked", []multipartTestPart{primaryUpload(strings.Repeat("x", 1500))}, func(l *EndpointLimits) {
			l.Multipart.Bytes = 1024
			l.Multipart.FileBytes = 1024
			l.Multipart.FieldBytes = 256
			l.Multipart.FieldsBytes = 256
		}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			limits := DefaultEndpointLimits()
			test.change(&limits)
			called := false
			router, err := NewRouter(multipartEndpoint(directory).WithLimits(limits).Handle(func(context.Context, multipartRequestInput) (NoContent, error) {
				called = true
				return NoContent{}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			wire, media := multipartWire(t, test.parts...)
			request := httptest.NewRequest("POST", "/uploads", bytes.NewReader(wire))
			request.Header.Set("Content-Type", media)
			if test.chunked {
				request.ContentLength = -1
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != 413 || called {
				t.Fatalf("response %d, handler=%v: %s", response.Code, called, response.Body)
			}
			assertMultipartCleanup(t, directory)
		})
	}
}

func TestMultipartEndpointFailuresCleanCapturedFiles(t *testing.T) {
	for _, test := range []struct {
		name     string
		status   int
		action   func() error
		validate bool
	}{
		{"handler-error", 500, func() error { return errors.New("private failure") }, false},
		{"handler-panic", 500, func() error { panic("private panic") }, false},
		{"handler-goexit", 500, func() error { runtime.Goexit(); return nil }, false},
		{"validation", 422, func() error { return nil }, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			endpoint := multipartEndpoint(directory)
			if test.validate {
				endpoint = endpoint.WithBodyValidation(validation.Custom(validation.Spec{ID: "test.reject_upload", Message: "Rejected upload"}, func(context.Context, multipartRequest) (bool, error) { return false, nil }))
			}
			called := false
			router, err := NewRouter(endpoint.Handle(func(context.Context, multipartRequestInput) (NoContent, error) {
				called = true
				return NoContent{}, test.action()
			}))
			if err != nil {
				t.Fatal(err)
			}
			wire, media := multipartWire(t, primaryUpload("captured bytes"))
			response := submitMultipart(t, router, wire, media)
			if response.Code != test.status || test.validate && called {
				t.Fatalf("response %d: %s", response.Code, response.Body)
			}
			assertMultipartCleanup(t, directory)
		})
	}
}

func TestMultipartMetadataAndInvalidDeclarations(t *testing.T) {
	endpoint := multipartEndpoint(t.TempDir())
	info, err := endpoint.Description()
	if err != nil {
		t.Fatal(err)
	}
	if info.Body == nil || info.Body.MediaType != "multipart/form-data" || info.Body.Multipart == nil || info.Body.Schema.Root != "" {
		t.Fatal("multipart acquired a JSON model shape")
	}
	if len(info.Body.Multipart.Parts) != 8 {
		t.Fatal("part declarations missing")
	}
	var file, text, structured bool
	for _, part := range info.Body.Multipart.Parts {
		switch part.Kind {
		case MultipartFile:
			file = true
			if part.Scalar != nil || part.Schema != nil {
				t.Error("file schema was inferred")
			}
		case MultipartText:
			text = true
			if part.Scalar == nil {
				t.Error("text codec scalar metadata missing")
			}
		case MultipartJSON:
			structured = true
			if part.Schema == nil {
				t.Error("JSON part schema missing")
			}
		}
	}
	if !file || !text || !structured {
		t.Fatal("multipart kinds missing")
	}
	info.Body.Multipart.Parts[0].Name = "changed"
	again, err := endpoint.Description()
	if err != nil || again.Body.Multipart.Parts[0].Name == "changed" {
		t.Fatal("multipart metadata was not owned")
	}
	if err := (Multipart[multipartRequest]{}).Validate(); err == nil {
		t.Fatal("zero multipart declaration accepted")
	}
	if err := DefineMultipart(FilePart[multipartRequest]("primary", nil)).Validate(); err == nil {
		t.Fatal("nil file selector accepted")
	}
	field := FilePart("same", func(b *multipartRequest) *UploadedFile { return &b.Primary })
	textPart := TextPart(QueryParam("same", StringQuery[string](), func(b *multipartRequest) *string { return new(string) }))
	if err := DefineMultipart(field, textPart).Validate(); err == nil {
		t.Fatal("cross-kind duplicate accepted")
	}
	if err := multipartForm("relative/directory").Validate(); err == nil {
		t.Fatal("relative temporary directory accepted")
	}
}

func TestMultipartSelectorFailureStaysInternal(t *testing.T) {
	for _, exit := range []bool{false, true} {
		directory := t.TempDir()
		form := DefineMultipart(FilePart("primary", func(b *multipartRequest) *UploadedFile {
			if exit {
				runtime.Goexit()
			}
			return nil
		})).WithTempDirectory(directory)
		endpoint := DefineEndpoint(DefineRoute(RouteSpec{ID: "uploads.store", Method: POST, Access: Public}, StaticPath("/uploads")), EmptyQuery(), MultipartBody(form), EmptyResponse(204))
		router, err := NewRouter(endpoint.Handle(func(context.Context, multipartRequestInput) (NoContent, error) {
			t.Error("handler ran after selector failure")
			return NoContent{}, nil
		}))
		if err != nil {
			t.Fatal(err)
		}
		wire, media := multipartWire(t, primaryUpload("captured"))
		response := submitMultipart(t, router, wire, media)
		if response.Code != 500 {
			t.Fatalf("response %d: %s", response.Code, response.Body)
		}
		assertMultipartCleanup(t, directory)
	}
}

func TestMultipartEnvelopeAndInterruptedStreamFailures(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		change func(*stdhttp.Request)
	}{
		{"missing-media", 415, func(r *stdhttp.Request) { r.Header.Del("Content-Type") }},
		{"duplicate-media", 415, func(r *stdhttp.Request) { r.Header.Add("Content-Type", r.Header.Get("Content-Type")) }},
		{"missing-boundary", 415, func(r *stdhttp.Request) { r.Header.Set("Content-Type", "multipart/form-data") }},
		{"content-encoding", 415, func(r *stdhttp.Request) { r.Header.Set("Content-Encoding", "gzip") }},
		{"truncated-stream", 400, func(r *stdhttp.Request) {
			data, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(data[:len(data)-30]))
			r.ContentLength = -1
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			called := false
			router, err := NewRouter(multipartEndpoint(directory).Handle(func(context.Context, multipartRequestInput) (NoContent, error) {
				called = true
				return NoContent{}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			wire, media := multipartWire(t, primaryUpload(strings.Repeat("x", 1024)))
			request := httptest.NewRequest("POST", "/uploads", bytes.NewReader(wire))
			request.Header.Set("Content-Type", media)
			test.change(request)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status || called {
				t.Fatalf("response %d, handler=%v: %s", response.Code, called, response.Body)
			}
			assertMultipartCleanup(t, directory)
		})
	}
}

func TestMultipartCancellationAfterCaptureCleansFiles(t *testing.T) {
	directory := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var retained UploadedFile
	router, err := NewRouter(multipartEndpoint(directory).Handle(func(_ context.Context, input multipartRequestInput) (NoContent, error) {
		retained = input.Body.Primary
		cancel()
		return NoContent{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	wire, media := multipartWire(t, primaryUpload("captured"))
	request := httptest.NewRequest("POST", "/uploads", bytes.NewReader(wire)).WithContext(ctx)
	request.Header.Set("Content-Type", media)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 408 {
		t.Fatalf("response %d: %s", response.Code, response.Body)
	}
	assertMultipartCleanup(t, directory)
	if _, err := retained.Open(context.Background()); !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("file remained after canceled response: %v", err)
	}
}

type multipartShortWriter struct {
	header stdhttp.Header
	wrote  bool
	check  func()
}

func (w *multipartShortWriter) Header() stdhttp.Header { return w.header }
func (w *multipartShortWriter) WriteHeader(int)        {}
func (w *multipartShortWriter) Write(data []byte) (int, error) {
	w.wrote = true
	w.check()
	return len(data) - 1, nil
}

func TestMultipartCleanupFollowsResponseWriteFailure(t *testing.T) {
	directory := t.TempDir()
	var retained UploadedFile
	route := DefineRoute(RouteSpec{ID: "uploads.store", Method: POST, Access: Public}, StaticPath("/uploads"))
	endpoint := DefineEndpoint(route, EmptyQuery(), MultipartBody(multipartForm(directory)), JSONResponse(200, endpointReplyJSON()))
	router, err := NewRouter(endpoint.Handle(func(_ context.Context, input multipartRequestInput) (EndpointReply, error) {
		retained = input.Body.Primary
		return EndpointReply{Name: "response"}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	writer := &multipartShortWriter{header: make(stdhttp.Header), check: func() {
		reader, err := retained.Open(context.Background())
		if err != nil {
			t.Errorf("upload cleaned before response finished: %v", err)
			return
		}
		if err := reader.Close(); err != nil {
			t.Error(err)
		}
	}}
	wire, media := multipartWire(t, primaryUpload("captured"))
	request := httptest.NewRequest("POST", "/uploads", bytes.NewReader(wire))
	request.Header.Set("Content-Type", media)
	func() {
		defer func() {
			if recovered := recover(); recovered != nil && recovered != stdhttp.ErrAbortHandler {
				t.Errorf("unexpected response panic: %v", recovered)
			}
		}()
		router.ServeHTTP(writer, request)
	}()
	if !writer.wrote {
		t.Fatal("response writer was not reached")
	}
	assertMultipartCleanup(t, directory)
	if _, err := retained.Open(context.Background()); !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("file remained after response failure: %v", err)
	}
}

func TestMultipartJSONDiagnosticsRespectFormLimit(t *testing.T) {
	directory := t.TempDir()
	limits := DefaultEndpointLimits()
	limits.Multipart.Issues = 1
	router, err := NewRouter(multipartEndpoint(directory).WithLimits(limits).Handle(func(context.Context, multipartRequestInput) (NoContent, error) {
		t.Error("handler received invalid JSON")
		return NoContent{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	wire, media := multipartWire(t, primaryUpload("captured"), multipartTestPart{name: "details", media: "application/json", contents: `{"name":12,"note":13}`})
	response := submitMultipart(t, router, wire, media)
	if response.Code != 400 || strings.Count(response.Body.String(), `"path"`) != 1 {
		t.Fatalf("diagnostic ceiling ignored: %d %s", response.Code, response.Body)
	}
	assertMultipartCleanup(t, directory)
}

func TestMultipartPartHeaderLimitCleansEarlierFiles(t *testing.T) {
	directory := t.TempDir()
	limits := DefaultEndpointLimits()
	limits.Multipart.HeaderBytes = 512
	router, err := NewRouter(multipartEndpoint(directory).WithLimits(limits).Handle(func(context.Context, multipartRequestInput) (NoContent, error) {
		t.Error("handler ran after header limit")
		return NoContent{}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	wire, media := multipartWire(t, primaryUpload("captured"), multipartTestPart{name: "name", contents: "small", headers: textproto.MIMEHeader{"X-Metadata": []string{strings.Repeat("a", 1024)}}})
	response := submitMultipart(t, router, wire, media)
	if response.Code != 413 {
		t.Fatalf("header limit response %d: %s", response.Code, response.Body)
	}
	assertMultipartCleanup(t, directory)
}
