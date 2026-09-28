package httpstreams

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/models"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

type reportService struct {
	open func(context.Context, model.ID[models.User]) (io.ReadCloser, error)
}

func (s reportService) Open(ctx context.Context, id model.ID[models.User]) (io.ReadCloser, error) {
	return s.open(ctx, id)
}

type reportReader struct {
	io.Reader
	closed *atomic.Int32
}

func (r reportReader) Close() error { r.closed.Add(1); return nil }

func TestTypedStreamConsumer(t *testing.T) {
	for _, method := range []string{"GET", "HEAD"} {
		t.Run(method, func(t *testing.T) {
			id, err := model.NewID[models.User]()
			if err != nil {
				t.Fatal(err)
			}
			var calls, closes atomic.Int32
			router, err := Router(reportService{open: func(ctx context.Context, key model.ID[models.User]) (io.ReadCloser, error) {
				calls.Add(1)
				if ctx == nil || key != id {
					t.Error("typed path not retained", key)
				}
				return reportReader{Reader: strings.NewReader("name\nAva\n"), closed: &closes}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			// The generated UserPath descriptor is shared with existing HTTP fixtures.
			location, err := Export.URL(t.Context(), httpkernel.UserPath{User: id}, foundryhttp.NoQuery{})
			if err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(method, location, nil))
			expected := "name\nAva\n"
			if method == "HEAD" {
				expected = ""
			}
			if recorder.Code != 200 || recorder.Body.String() != expected || calls.Load() != 1 || closes.Load() != 1 || recorder.Header().Get("Content-Type") != "text/csv; charset=utf-8" {
				t.Fatal("consumer", recorder.Code, calls.Load(), closes.Load(), recorder.Body.String())
			}
		})
	}
}
