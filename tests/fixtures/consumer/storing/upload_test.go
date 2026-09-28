package storing_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"foundry.test/consumer/httpuploads"
	"foundry.test/consumer/storing"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/storage"
	"github.com/weiloon1234/Foundry-Go/storage/local"
)

type uploadService struct {
	disk *storage.Disk
	key  storage.ObjectKey
	file foundryhttp.UploadedFile
}

func (s *uploadService) Save(ctx context.Context, input httpuploads.ProfileInput) (httpuploads.UploadReply, error) {
	s.file = input.Attachment
	stored, err := storing.PersistUpload(ctx, s.disk, s.key, input.Attachment)
	if err != nil {
		return httpuploads.UploadReply{}, err
	}
	return httpuploads.UploadReply{Name: input.Attachment.Name(), Bytes: stored.Object.Size, DetectedType: string(stored.Object.ContentType)}, nil
}
func TestCapturedUploadPersistenceAndRequestCleanup(t *testing.T) {
	for _, limited := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "storage-limit"}[limited], func(t *testing.T) {
			backend, err := local.Open(t.Context(), local.DefaultConfig(t.TempDir()))
			if err != nil {
				t.Fatal(err)
			}
			defer backend.Close()
			config := storage.DefaultConfig()
			if limited {
				config.MaxObjectBytes = 8
			}
			disk, err := storage.NewDisk("uploads", backend, config)
			if err != nil {
				t.Fatal(err)
			}
			defer disk.Close(context.Background())
			key, err := storage.ParseKey("server-selected/report.txt")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := disk.PutBytes(t.Context(), key, []byte("old"), storage.PutOptions{}); err != nil {
				t.Fatal(err)
			}
			service := &uploadService{disk: disk, key: key}
			temporary := t.TempDir()
			router, err := httpuploads.Router(temporary, service)
			if err != nil {
				t.Fatal(err)
			}
			data := strings.Repeat("text content ", 4096)
			var wire bytes.Buffer
			writer := multipart.NewWriter(&wire)
			part, err := writer.CreateFormFile("document", "untrusted-name.txt")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = part.Write([]byte(data)); err != nil {
				t.Fatal(err)
			}
			if err = writer.Close(); err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest("POST", "/profile", &wire)
			request.Header.Set("Content-Type", writer.FormDataContentType())
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			expectedStatus, expectedBody := 201, data
			if limited {
				expectedStatus, expectedBody = 500, "old"
			}
			if response.Code != expectedStatus {
				t.Fatal("upload response", response.Code, response.Body.String())
			}
			stored, _, err := disk.ReadBytes(t.Context(), key, config.MaxObjectBytes, storage.ReadOptions{})
			if err != nil || string(stored) != expectedBody {
				t.Fatal("upload publication changed", err)
			}
			if _, err := service.file.Open(context.Background()); !errors.Is(err, fs.ErrClosed) {
				t.Fatal("request upload escaped cleanup", err)
			}
			files, err := os.ReadDir(temporary)
			if err != nil || len(files) != 0 || disk.Stats().Active != 0 {
				t.Fatal("temporary resources leaked", err)
			}
		})
	}
}
