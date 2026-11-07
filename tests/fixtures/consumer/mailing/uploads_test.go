package mailing_test

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"testing"

	"foundry.test/consumer/httpuploads"
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/email/memory"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// doc-example:begin attach-request-upload
type uploadMailService struct {
	base email.Message
}

func (s *uploadMailService) Build(ctx context.Context, input httpuploads.ProfileInput) (email.Message, error) {
	message, err := s.base.AttachUpload(ctx, input.Attachment)
	if err != nil {
		return email.Message{}, err
	}
	return message, nil
}

// doc-example:end attach-request-upload

// uploadMailRecorder captures one request for the sequential tests below.
// It is test bookkeeping, not part of the reusable mail service.
type uploadMailRecorder struct {
	service *uploadMailService
	message email.Message
	file    foundryhttp.UploadedFile
}

func (r *uploadMailRecorder) Save(ctx context.Context, input httpuploads.ProfileInput) (httpuploads.UploadReply, error) {
	r.file = input.Attachment
	var err error
	r.message, err = r.service.Build(ctx, input)
	return httpuploads.UploadReply{Name: input.Attachment.Name(), Bytes: input.Attachment.Size(), DetectedType: input.Attachment.ContentType()}, err
}

func TestBrowserUploadCanBeEmailedWithoutAttachmentStorage(t *testing.T) {
	checkBrowserUploadMail(t, "browser upload")
}

func TestBrowserUploadMailKeepsRequestsIndependent(t *testing.T) {
	checkBrowserUploadMail(t, "first request upload", "second request upload")
}

func checkBrowserUploadMail(t *testing.T, contents ...string) {
	t.Helper()
	from, err := email.ParseAddress("sender@example.test")
	if err != nil {
		t.Fatal(err)
	}
	to, err := email.ParseAddress("reader@example.test")
	if err != nil {
		t.Fatal(err)
	}
	svc := &uploadMailRecorder{service: &uploadMailService{base: email.NewMessage(from, "Your upload", to).Text("Attached")}}
	directory := t.TempDir()
	router, err := httpuploads.Router(directory, svc)
	if err != nil {
		t.Fatal(err)
	}
	var messages []email.Message
	for _, content := range contents {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("document", "document.txt")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(part, content); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest("POST", "/profile", &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 201 {
			t.Fatal("upload failed", response.Code)
		}
		if _, err := svc.file.Open(t.Context()); err == nil {
			t.Fatal("request upload was not cleaned")
		}
		messages = append(messages, svc.message)
		if files, err := os.ReadDir(directory); err != nil || len(files) != 0 {
			t.Fatal("upload temp files remained", err)
		}
	}
	driver, err := memory.New(len(contents))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	mailer, err := email.New(driver, nil, email.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := mailer.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	for _, message := range messages {
		if _, err := mailer.Send(t.Context(), message, email.SendOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	deliveries := driver.Messages()
	if len(deliveries) != len(contents) {
		t.Fatal("upload messages were not delivered independently")
	}
	for i, content := range contents {
		files := deliveries[i].Attachments()
		if len(files) != 1 || files[0].Reference().Filename != "document.txt" || string(files[0].Bytes()) != content {
			t.Fatalf("message %d did not contain only its request upload", i)
		}
	}
}
