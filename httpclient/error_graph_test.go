package httpclient_test

import (
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraphtest"
	"net/http"
	"testing"
)

func TestIncompleteTransportInspectionDoesNotRetry(t *testing.T) {
	for _, mode := range []string{"cycle", "panic", "goexit"} {
		cause := &errorgraphtest.Error{Mode: mode}
		calls := 0
		config := testConfig()
		config.Retry = httpclient.RetryPolicy{Mode: httpclient.SafeReads, Attempts: 3}
		client := newClient(t, config, roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, cause }))
		_, err := client.Do(t.Context(), client.Get("failure"))
		if err == nil || calls != 1 || cause.Formatted.Load() != 0 || cause.Visits.Load() > 256 {
			t.Fatal("incomplete transport inspection triggered retry or formatted error")
		}
	}
}
