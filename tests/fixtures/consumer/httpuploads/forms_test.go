package httpuploads_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"net/http/httptest"
	"net/textproto"
	"os"
	"strings"
	"testing"

	"foundry.test/consumer/httpuploads"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

type service struct {
	called int
	file   foundryhttp.UploadedFile
	input  httpuploads.ProfileInput
}

func (s *service) Save(ctx context.Context, input httpuploads.ProfileInput) (httpuploads.UploadReply, error) {
	s.called++
	s.file = input.Attachment
	s.input = input
	count, err := httpuploads.ReadContents(ctx, input.Attachment)
	return httpuploads.UploadReply{Name: input.Attachment.Name(), Bytes: count, DetectedType: input.Attachment.ContentType()}, err
}

func formBody(t *testing.T, filename, contents, clientType string) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := textproto.MIMEHeader{
		"Content-Disposition": []string{mime.FormatMediaType("form-data", map[string]string{"name": "document", "filename": filename})},
		"Content-Type":        []string{clientType},
	}
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, contents); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ name, text string }{{"title", ""}, {"tags[]", "one"}, {"tags[]", "two"}} {
		if err := writer.WriteField(item.name, item.text); err != nil {
			t.Fatal(err)
		}
	}
	header = textproto.MIMEHeader{"Content-Disposition": []string{`form-data; name="settings"`}, "Content-Type": []string{"application/json"}}
	part, err = writer.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, `{"caption":"demo","note":null}`); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes(), writer.FormDataContentType()
}

func TestGeneratedMultipartConsumer(t *testing.T) {
	directory := t.TempDir()
	svc := &service{}
	router, err := httpuploads.Router(directory, svc)
	if err != nil {
		t.Fatal(err)
	}
	body, media := formBody(t, `C:\fakepath\document.txt`, "file contents", "image/png")
	request := httptest.NewRequest("POST", "/profile", bytes.NewReader(body))
	request.Header.Set("Content-Type", media)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 201 || svc.called != 1 {
		t.Fatalf("response %d: %s", response.Code, response.Body)
	}
	var reply httpuploads.UploadReply
	if err := json.Unmarshal(response.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Name != "document.txt" || reply.Bytes != 13 || reply.DetectedType != "text/plain; charset=utf-8" {
		t.Fatalf("explicit reply: %+v", reply)
	}
	if title, present := svc.input.Title.Get(); !present || title != "" {
		t.Fatal("blank form field became omitted")
	}
	if len(svc.input.Tags) != 2 || svc.input.Tags[1] != "two" {
		t.Fatal("typed repeated fields changed")
	}
	settings, present := svc.input.Settings.Get()
	note, supplied := settings.Note.Get()
	if !present || settings.Caption != "demo" || !supplied || !note.IsNull() {
		t.Fatal("JSON part nullability changed")
	}
	if _, err := svc.file.Open(context.Background()); !errors.Is(err, fs.ErrClosed) {
		t.Fatal("file survived request cleanup")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("temporary files remained")
	}
	if strings.Contains(response.Body.String(), "settings") || strings.Contains(response.Body.String(), "file contents") {
		t.Fatal("request details leaked into reply")
	}
}

func TestGeneratedFileValidationRejectsAndCleans(t *testing.T) {
	for _, test := range []struct{ name, filename, contents, clientType, rule string }{
		{"empty", "empty.txt", "", "text/plain", "foundry.file_min_size"},
		{"extension", "renamed.exe", "text", "text/plain", "foundry.file_extensions"},
		{"detected-content", "claims.png", strings.Repeat("\x00", 32), "image/png", "foundry.file_content_types"},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			svc := &service{}
			router, err := httpuploads.Router(directory, svc)
			if err != nil {
				t.Fatal(err)
			}
			body, media := formBody(t, test.filename, test.contents, test.clientType)
			request := httptest.NewRequest("POST", "/profile", bytes.NewReader(body))
			request.Header.Set("Content-Type", media)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != 422 || svc.called != 0 || !strings.Contains(response.Body.String(), "/body/document") || !strings.Contains(response.Body.String(), test.rule) {
				t.Fatalf("response %d: %s", response.Code, response.Body)
			}
			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 0 {
				t.Fatal("validation retained captured files")
			}
		})
	}
}
