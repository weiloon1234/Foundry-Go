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

type uploadMailService struct {
	message email.Message
	file    foundryhttp.UploadedFile
}

func (s *uploadMailService) Save(ctx context.Context, input httpuploads.ProfileInput) (httpuploads.UploadReply, error) {
	s.file = input.Attachment
	var err error
	s.message, err = s.message.AttachUpload(ctx, input.Attachment)
	return httpuploads.UploadReply{Name: input.Attachment.Name(), Bytes: input.Attachment.Size(), DetectedType: input.Attachment.ContentType()}, err
}

func TestBrowserUploadCanBeEmailedWithoutAttachmentStorage(t *testing.T) {
	from, err := email.ParseAddress("sender@example.test")
	if err != nil {
		t.Fatal(err)
	}
	to, err := email.ParseAddress("reader@example.test")
	if err != nil {
		t.Fatal(err)
	}
	svc := &uploadMailService{message: email.NewMessage(from, "Your upload", to).Text("Attached")}
	directory := t.TempDir()
	router, err := httpuploads.Router(directory, svc)
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("document", "document.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, "browser upload"); err != nil {
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
	if files, err := os.ReadDir(directory); err != nil || len(files) != 0 {
		t.Fatal("upload temp files remained", err)
	}
	driver, err := memory.New(1)
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
	if _, err := mailer.Send(t.Context(), svc.message, email.SendOptions{}); err != nil {
		t.Fatal(err)
	}
	files := driver.Messages()[0].Attachments()
	if len(files) != 1 || files[0].Reference().Filename != "document.txt" || string(files[0].Bytes()) != "browser upload" {
		t.Fatal("browser attachment did not reach common mailer")
	}
}
