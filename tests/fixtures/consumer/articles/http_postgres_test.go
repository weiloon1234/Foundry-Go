package articles_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http/httptest"
	"net/textproto"
	"slices"
	"strings"
	"testing"

	"foundry.test/consumer/articles"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/metadata"
	"github.com/weiloon1234/Foundry-Go/translations"
)

// httpFixture serves the article endpoints over the application's managers
// with production locale negotiation.
func httpFixture(t *testing.T) (fixture, func(method, path, contentType string, body []byte, locale string) (int, []byte)) {
	t.Helper()
	f := startApplication(t)
	runtime, err := f.services.ModelExtensions()
	if err != nil {
		t.Fatal(err)
	}
	routes, err := articles.NewService(f.store, runtime).Routes()
	if err != nil {
		t.Fatal(err)
	}
	router, err := foundryhttp.NewRouter(routes...)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := f.services.Locales()
	if err != nil {
		t.Fatal(err)
	}
	handler, err := foundryhttp.ApplyMiddleware(router, foundryhttp.Locale(catalog))
	if err != nil {
		t.Fatal(err)
	}
	return f, func(method, path, contentType string, body []byte, locale string) (int, []byte) {
		t.Helper()
		request := httptest.NewRequest(method, path, bytes.NewReader(body)).WithContext(t.Context())
		if contentType != "" {
			request.Header.Set("Content-Type", contentType)
		}
		if locale != "" {
			request.Header.Set("Accept-Language", locale)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.Code, data
	}
}

func decode[T any](t *testing.T, data []byte) T {
	t.Helper()
	var result T
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	return result
}

func issueCodes(t *testing.T, data []byte) map[string]string {
	t.Helper()
	failure := decode[foundryhttp.ErrorResponse](t, data)
	codes := make(map[string]string, len(failure.Issues))
	for _, issue := range failure.Issues {
		codes[issue.Path] = string(issue.Code)
	}
	return codes
}

// Translated input, metadata and files travel over real JSON and multipart
// endpoints; validation comes from the model's slot policy and responses
// resolve the request locale.
func TestArticleEndpointsUseModelSlots(t *testing.T) {
	f, do := httpFixture(t)
	created := `{"slug":"hello","author":"Aisyah","title":{"en":"Hello","ms":"Helo"},"summary":{"en":"Short"},"seo":{"canonical":"/hello"}}`
	status, body := do("POST", "/articles", "application/json", []byte(created), "ms")
	if status != 201 {
		t.Fatalf("create %d: %s", status, body)
	}
	view := decode[map[string]any](t, body)
	if view["title"] != "Helo" || view["title_locale"] != "ms" || view["summary"] != "Short" || view["seo"].(map[string]any)["canonical"] != "/hello" {
		t.Fatalf("created view: %s", body)
	}
	id := view["id"].(string)

	// The rule rejects unsupported and missing required locales and oversized
	// text at their entry paths before any write.
	invalid := `{"slug":"bad","author":"X","title":{"fr":"Bonjour","ms":"` + strings.Repeat("x", 513) + `"},"summary":{}}`
	status, body = do("POST", "/articles", "application/json", []byte(invalid), "")
	codes := issueCodes(t, body)
	if status != 422 || codes["/body/title/fr"] != "foundry.supported_locale" || codes["/body/title/en"] != "foundry.required" || codes["/body/title/ms"] != "foundry.max_bytes" {
		t.Fatalf("translated validation %d: %s", status, body)
	}
	status, body = do("GET", "/articles", "", nil, "")
	if list := decode[map[string][]map[string]any](t, body); status != 200 || len(list["items"]) != 1 {
		t.Fatalf("rejected input was written: %d %s", status, body)
	}

	// Update synchronizes: the Malay title the request omits is removed.
	updated := `{"slug":"hello","author":"Aisyah","title":{"en":"Hello again"},"summary":{"en":"Short"}}`
	status, body = do("PUT", "/articles/"+id, "application/json", []byte(updated), "ms")
	view = decode[map[string]any](t, body)
	if status != 200 || view["title"] != "Hello again" || view["title_locale"] != "en" || view["seo"] != nil {
		t.Fatalf("synchronized update %d: %s", status, body)
	}

	// Multipart media: a JSON part merges text, the logo is transformed by its
	// image plan and gallery files receive their thumbnail variant.
	form, contentType := mediaForm(t, `{"ms":"Baharu"}`, []part{{"logo", "logo.png", pngImage(t, 40)}, {"galleries", "one.png", pngImage(t, 20)}, {"galleries", "two.png", pngImage(t, 20)}})
	status, body = do("POST", "/articles/"+id+"/media", contentType, form, "ms")
	view = decode[map[string]any](t, body)
	if status != 200 || view["title"] != "Baharu" {
		t.Fatalf("media upload %d: %s", status, body)
	}
	logo := view["logo"].(map[string]any)
	galleries := view["galleries"].([]any)
	if logo["width"] != float64(32) || logo["media_type"] != "image/png" || len(galleries) != 2 || galleries[0].(map[string]any)["thumbnail"] != true {
		t.Fatalf("media view: %s", body)
	}
	var names []string
	for _, file := range galleries {
		names = append(names, file.(map[string]any)["name"].(string))
	}
	if !slices.Equal(names, []string{"one.png", "two.png"}) {
		t.Fatal("gallery order", names)
	}

	// Files are checked against the slot policy with write-time detection:
	// text named .png is not an image, and the gallery limit is four.
	form, contentType = mediaForm(t, "", []part{{"logo", "fake.png", []byte("plain text")}})
	status, body = do("POST", "/articles/"+id+"/media", contentType, form, "")
	if codes := issueCodes(t, body); status != 422 || codes["/body/logo"] != "articles.logo" {
		t.Fatalf("logo policy %d: %s", status, body)
	}
	many := make([]part, 5)
	for i := range many {
		many[i] = part{"galleries", "extra.png", pngImage(t, 8)}
	}
	form, contentType = mediaForm(t, "", many)
	status, body = do("POST", "/articles/"+id+"/media", contentType, form, "")
	if codes := issueCodes(t, body); status != 422 || codes["/body/galleries"] != "foundry.max_items" {
		t.Fatalf("gallery limit %d: %s", status, body)
	}

	// Deletion removes the article and its extension data.
	if status, body = do("DELETE", "/articles/"+id, "", nil, ""); status != 204 {
		t.Fatalf("delete %d: %s", status, body)
	}
	if status, _ = do("GET", "/articles/"+id, "", nil, ""); status != 404 {
		t.Fatal("deleted article is still visible", status)
	}
	runtime, err := f.services.ModelExtensions()
	if err != nil {
		t.Fatal(err)
	}
	for name, orphans := range map[string]func() (int, error){
		"translations": func() (int, error) {
			page, err := translations.InspectOrphans(t.Context(), runtime.Translations, "articles", translations.Cursor{}, 100)
			return page.Scanned, err
		},
		"metadata": func() (int, error) {
			page, err := metadata.InspectOrphans(t.Context(), runtime.Metadata, "articles", metadata.Cursor{}, 100)
			return page.Scanned, err
		},
	} {
		if scanned, err := orphans(); err != nil || scanned != 0 {
			t.Fatal("deletion left "+name+" rows", scanned, err)
		}
	}
}

type part struct {
	name, filename string
	data           []byte
}

func mediaForm(t *testing.T, title string, files []part) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if title != "" {
		header := textproto.MIMEHeader{"Content-Disposition": []string{`form-data; name="title"`}, "Content-Type": []string{"application/json"}}
		field, err := writer.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(field, title); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range files {
		header := textproto.MIMEHeader{
			"Content-Disposition": []string{mime.FormatMediaType("form-data", map[string]string{"name": file.name, "filename": file.filename})},
			"Content-Type":        []string{"image/png"},
		}
		field, err := writer.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := field.Write(file.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes(), writer.FormDataContentType()
}
