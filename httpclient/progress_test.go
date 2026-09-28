package httpclient_test

import (
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
)

type noProgressReader struct{}

func (noProgressReader) Read([]byte) (int, error) { return 0, nil }

func TestNonProgressingResponseReleasesAdmissionAfterClose(t *testing.T) {
	var closed atomic.Int32
	config := testConfig()
	config.Concurrency = 1
	client := newClient(t, config, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: -1, Body: &countBody{Reader: noProgressReader{}, closed: &closed}}, nil
	}))
	// A second attempt proves that failure retained close ownership but released
	// the sole admission slot when cleanup actually finished.
	for attempt := int32(1); attempt <= 2; attempt++ {
		_, err := client.Do(t.Context(), client.Get("empty"))
		if !errors.Is(err, io.ErrNoProgress) {
			t.Fatalf("missing progress failure: %v", err)
		}
		if closed.Load() != attempt {
			t.Fatalf("close count: %d", closed.Load())
		}
	}
}
