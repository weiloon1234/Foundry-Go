package diagnostics

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/health"
)

func TestProfileEndpointValidatesQueryAndServesOneProfileAtATime(t *testing.T) {
	probes, err := health.NewRegistry(health.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.Profiling = true
	runtime, err := prepare(config, probes)
	if err != nil {
		t.Fatal(err)
	}
	serve := func(target string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		runtime.serveProfile(response, httptest.NewRequest("GET", target, nil))
		return response
	}
	if response := serve("/profile?name=goroutine&debug=1"); response.Code != 200 || !strings.Contains(response.Body.String(), "goroutine") || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/plain") {
		t.Fatal("text goroutine profile failed", response.Code)
	}
	if response := serve("/profile"); response.Code != 200 || response.Body.Len() == 0 || response.Header().Get("Content-Disposition") == "" {
		t.Fatal("default heap profile failed", response.Code)
	}
	if response := serve("/profile?name=cpu&seconds=1"); response.Code != 200 || response.Body.Len() == 0 {
		t.Fatal("bounded CPU profile failed", response.Code)
	}
	for _, target := range []string{"/profile?name=unknown", "/profile?name=cpu&seconds=60", "/profile?debug=2", "/profile?extra=1", "/profile?name=heap&name=cpu"} {
		if response := serve(target); response.Code != 400 {
			t.Fatal("invalid profile query accepted", target, response.Code)
		}
	}
	runtime.profile <- struct{}{}
	if response := serve("/profile"); response.Code != 503 || response.Header().Get("Retry-After") == "" {
		t.Fatal("concurrent profile was admitted", response.Code)
	}
	<-runtime.profile
	disabled, err := prepare(DefaultConfig(), probes)
	if err != nil {
		t.Fatal(err)
	}
	if disabled.profiling {
		t.Fatal("profiling enabled by default")
	}
}
