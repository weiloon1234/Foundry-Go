package httpcookies_test

import (
	"encoding/json"
	"foundry.test/consumer/httpcookies"
	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/models"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/testkit"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTypedCookiesWithNativeTLSClientJar(t *testing.T) {
	keys, err := foundryhttp.NewSigningKeys(foundryhttp.SigningKey{ID: "fixture", Secret: secret.New(strings.Repeat("fixture-key", 4))})
	if err != nil {
		t.Fatal(err)
	}
	now := testkit.NewClock(time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC))
	handler, err := httpcookies.Handler(keys, now)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(handler)
	defer server.Close()
	client := server.Client()
	client.Timeout = 3 * time.Second
	client.Jar, err = cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	id, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	selectedPath, err := httpcookies.Select.URL(httpkernel.UserPath{User: id})
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path string) *http.Response {
		t.Helper()
		r, err := http.NewRequestWithContext(t.Context(), method, server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	response := call("POST", selectedPath)
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 204 || len(response.Cookies()) != 3 {
		t.Fatalf("cookie creation failed: %d", response.StatusCode)
	}
	for _, cookie := range response.Cookies() {
		if !cookie.Secure || !cookie.HttpOnly || cookie.Path != "/" || cookie.MaxAge != 3600 {
			t.Fatalf("insecure or incomplete scope: %+v", cookie)
		}
	}
	response = call("GET", "/preferences")
	var preferences httpcookies.Preferences
	err = json.NewDecoder(response.Body).Decode(&preferences)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	user, present := preferences.User.Get()
	locale, _ := preferences.Locale.Get()
	state, _ := preferences.State.Get()
	if response.StatusCode != 200 || !present || user != id || locale != "en-MY" || state != models.StatusActive {
		t.Fatalf("typed cookie round trip failed: %+v", preferences)
	}
	now.Advance(time.Hour)
	response = call("GET", "/preferences")
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 400 {
		t.Fatalf("server accepted expired signed cookie: %d", response.StatusCode)
	}
	response = call("DELETE", "/preferences")
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 204 {
		t.Fatal("cookie removal failed")
	}
	response = call("GET", "/preferences")
	defer response.Body.Close()
	preferences = httpcookies.Preferences{}
	if err := json.NewDecoder(response.Body).Decode(&preferences); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || preferences.User.IsSet() || preferences.Locale.IsSet() || preferences.State.IsSet() {
		t.Fatal("cookie jar retained cleared values")
	}
}
