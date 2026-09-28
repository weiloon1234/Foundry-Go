package httpdownloads_test

import (
	"context"
	"encoding/json"
	"mime"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"foundry.test/consumer/httpdownloads"
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/models"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

type documents struct {
	document httpdownloads.Document
	key      model.ID[models.User]
	calls    int
	err      error
}

func (d *documents) File(_ context.Context, id model.ID[models.User]) (httpdownloads.Document, error) {
	d.calls++
	d.key = id
	return d.document, d.err
}

func TestConsumerLocalDocumentKeepsTypedKeyAndNativeTransfer(t *testing.T) {
	directory := t.TempDir()
	const payload = "%PDF-1.4 document"
	if err := os.WriteFile(filepath.Join(directory, "stored.bin"), []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	service := &documents{document: httpdownloads.Document{Path: "stored.bin", Name: "Résumé.pdf", MediaType: "application/pdf", EntityTag: "\"revision-1\""}}
	router, err := httpdownloads.Router(root, service)
	if err != nil {
		t.Fatal(err)
	}
	id, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	location, err := httpdownloads.Show.URL(t.Context(), httpkernel.UserPath{User: id}, foundryhttp.NoQuery{})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		method, rangeValue, etag string
		status                   int
		body                     string
	}{{"GET", "", "", 200, payload}, {"HEAD", "", "", 200, ""}, {"GET", "bytes=0-3", "", 206, "%PDF"}, {"GET", "", "\"revision-1\"", 304, ""}} {
		request := httptest.NewRequest(test.method, location, nil)
		request.Header.Set("Range", test.rangeValue)
		request.Header.Set("If-None-Match", test.etag)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != test.status || recorder.Body.String() != test.body || service.key != id {
			t.Fatal("typed download response", recorder.Code, recorder.Body.String())
		}
		if test.status == 200 || test.status == 206 {
			disposition, parameters, err := mime.ParseMediaType(recorder.Header().Get("Content-Disposition"))
			if err != nil || disposition != "attachment" || parameters["filename"] != "Résumé.pdf" || recorder.Header().Get("Content-Type") != "application/pdf" {
				t.Fatal("declared file metadata lost", recorder.Header(), err)
			}
		}
	}
	if service.calls != 4 {
		t.Fatal("domain model key resolved more than once per request", service.calls)
	}
	if _, err := root.Stat("stored.bin"); err != nil {
		t.Fatal("framework closed application root", err)
	}
	service.document.Path = "missing.bin"
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", location, nil))
	var failure foundryhttp.ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &failure); err != nil || recorder.Code != 404 || failure.Code != foundryhttp.NotFound {
		t.Fatal("missing local file did not use shared error", recorder.Code, err)
	}
	service.err = foundryhttp.Forbidden
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", location, nil))
	if recorder.Code != 403 {
		t.Fatal("domain failure did not stop transfer", recorder.Code)
	}
	previous := service.calls
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/users/not-a-model-id", nil))
	if recorder.Code != 400 || service.calls != previous {
		t.Fatal("invalid model key reached document service")
	}
}
