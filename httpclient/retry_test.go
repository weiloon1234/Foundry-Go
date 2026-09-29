package httpclient_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/httpclient"
	fakehttp "github.com/weiloon1234/Foundry-Go/testkit/httpclient"
)

func TestRetryAfterSetsTheWaitOrEndsRetrying(t *testing.T) {
	config := testConfig()
	config.Retry = httpclient.RetryPolicy{Mode: httpclient.SafeReads, Attempts: 3, InitialBackoff: 5 * time.Second, MaxBackoff: 5 * time.Second}
	fake := newFake(t, fakehttp.Respond(503, http.Header{"Retry-After": {"0"}}, nil), fakehttp.Respond(200, nil, nil))
	c := newClient(t, config, fake)
	started := time.Now()
	response, err := c.Do(t.Context(), c.Get("soon"))
	if err != nil || response.Status() != 200 || response.Attempts() != 2 || time.Since(started) > 2*time.Second {
		t.Fatal("Retry-After did not replace the policy backoff", err, time.Since(started))
	}
	fake = newFake(t, fakehttp.Respond(429, http.Header{"Retry-After": {"120"}}, nil), fakehttp.Respond(200, nil, nil))
	c = newClient(t, config, fake)
	started = time.Now()
	response, err = c.Do(t.Context(), c.Get("later"))
	if err != nil || response.Status() != 429 || response.Attempts() != 1 || fake.Sent() != 1 || time.Since(started) > 2*time.Second {
		t.Fatal("a Retry-After beyond MaxBackoff was waited for", err)
	}
	if response.Headers().Get("Retry-After") != "120" {
		t.Fatal("rejection headers were not returned to the caller")
	}
}

func TestRetryStatusesAreConfigurableAndSnapshotted(t *testing.T) {
	config := testConfig()
	config.Retry.Statuses = []int{409}
	fake := newFake(t, fakehttp.Respond(409, nil, nil), fakehttp.Respond(200, nil, nil), fakehttp.Respond(503, nil, nil))
	c := newClient(t, config, fake)
	config.Retry.Statuses[0] = 503
	if response, err := c.Do(t.Context(), c.Get("conflict")); err != nil || response.Status() != 200 || response.Attempts() != 2 {
		t.Fatal("configured retry status ignored", err)
	}
	if response, err := c.Do(t.Context(), c.Get("unavailable")); err != nil || response.Status() != 503 || response.Attempts() != 1 {
		t.Fatal("status outside the configured set was retried", err)
	}
	for _, statuses := range [][]int{{200}, {302}, {600}} {
		invalid := testConfig()
		invalid.Retry.Statuses = statuses
		if invalid.Validate() == nil {
			t.Fatal("non-error retry status accepted", statuses)
		}
	}
}
