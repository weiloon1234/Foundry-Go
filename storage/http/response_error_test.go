package http_test

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/storage"
	storagehttp "github.com/weiloon1234/Foundry-Go/storage/http"
)

func TestResponseErrorPreservesMappingAndCause(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause error
		want  foundryhttp.ErrorCode
	}{
		{"missing", storage.Failure(storage.NotFound, storage.OpenOperation, storage.NotApplicable, nil), foundryhttp.NotFound},
		{"forbidden", storage.Failure(storage.Forbidden, storage.OpenOperation, storage.NotApplicable, nil), foundryhttp.Forbidden},
		{"precondition", storage.Failure(storage.PreconditionFailed, storage.OpenOperation, storage.NotApplicable, nil), foundryhttp.PreconditionFailed},
		{"range", storage.Failure(storage.RangeNotSatisfiable, storage.OpenOperation, storage.NotApplicable, nil), foundryhttp.RangeNotSatisfiable},
		{"limit", storage.Failure(storage.LimitExceeded, storage.OpenOperation, storage.NotApplicable, nil), foundryhttp.Unavailable},
		{"closed", storage.Failure(storage.Closed, storage.OpenOperation, storage.NotApplicable, nil), foundryhttp.Unavailable},
		{"unclassified storage", storage.Failure(storage.Unavailable, storage.OpenOperation, storage.NotApplicable, nil), foundryhttp.InternalError},
		{"overloaded", fault.Overloaded, foundryhttp.Unavailable},
		{"deadline", context.DeadlineExceeded, foundryhttp.Unavailable},
		{"canceled", context.Canceled, foundryhttp.Unavailable},
		{"unknown", errors.New("private adapter failure"), foundryhttp.InternalError},
		{"bare code", storage.NotFound, foundryhttp.InternalError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cause := fmt.Errorf("application read: %w", tc.cause)
			got := storagehttp.ResponseError(cause)
			if !errors.Is(got, tc.want) || !errors.Is(got, cause) {
				t.Fatal("response classification or cause changed")
			}
		})
	}
	if err := storagehttp.ResponseError(nil); err != nil {
		t.Fatalf("nil mapped to %v", err)
	}
}

func TestResponseErrorInsideApplicationStreamSource(t *testing.T) {
	disk, key, _ := testDisk(t)
	if err := disk.Delete(t.Context(), key, storage.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	endpoint := foundryhttp.DefineEndpoint(foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "files.application", Method: foundryhttp.GET, Access: foundryhttp.Public}, foundryhttp.StaticPath("/file")), foundryhttp.EmptyQuery(), foundryhttp.EmptyBody(), foundryhttp.StreamResponse("text/plain"))
	opened := false
	router, err := foundryhttp.NewRouter(endpoint.Handle(func(context.Context, foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, foundryhttp.NoBody]) (foundryhttp.Stream, error) {
		return foundryhttp.StreamFrom(func(ctx context.Context) (foundryhttp.StreamContent, error) {
			opened = true
			_, _, err := disk.Open(ctx, key, storage.ReadOptions{})
			return foundryhttp.StreamContent{}, storagehttp.ResponseError(err)
		}), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/file", nil))
	if !opened || response.Code != 404 || disk.Stats().Active != 0 {
		t.Fatal("deferred application read did not return a clean 404")
	}
}
