package httpkernel_test

import (
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"foundry.test/consumer/httpkernel"
	"foundry.test/consumer/models"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestGeneratedPathsFromIndependentConsumer(t *testing.T) {
	id, err := model.NewID[models.User]()
	if err != nil {
		t.Fatal(err)
	}
	input := httpkernel.UserPath{User: id}
	if input.User != id {
		t.Fatal("path lost model ownership")
	}
	plain, err := httpkernel.UserPathDescriptor().URL(input)
	if err != nil || plain != "/users/"+id.String() {
		t.Fatalf("generated descriptor URL: %q %v", plain, err)
	}
	location, err := httpkernel.ShowUser.URL(input)
	if err != nil || location != "/api"+plain {
		t.Fatalf("scoped generated URL: %q %v", location, err)
	}
	feed := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "users.feed", Method: foundryhttp.GET, Access: foundryhttp.Public}, httpkernel.UserFeedPathDescriptor())
	var users, feeds int
	router, err := foundryhttp.NewRouter(
		httpkernel.ShowUser.HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, path httpkernel.UserPath) {
			if path.User != id {
				t.Error("generated path changed model ID")
			}
			users++
			w.WriteHeader(204)
		}),
		feed.HandleRaw(func(w stdhttp.ResponseWriter, _ *stdhttp.Request, path httpkernel.UserFeedPath) {
			if path.Status != models.StatusActive || path.Page != 2 {
				t.Error("generated path changed enum or page")
			}
			feeds++
			w.WriteHeader(204)
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	feedURL, err := feed.URL(httpkernel.UserFeedPath{Status: models.StatusActive, Page: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		url    string
		status int
	}{
		{location, 204}, {feedURL, 204},
		{strings.Replace(location, id.String(), "invalid", 1), 400},
		{"/users/status/unknown/2", 400}, {"/users/status/active/65536", 400},
		{"/users/status/active/02", 400},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", test.url, nil))
		if response.Code != test.status {
			t.Fatalf("%s: status %d want %d", test.url, response.Code, test.status)
		}
	}
	if users != 1 || feeds != 1 {
		t.Fatal("invalid path reached a consumer handler")
	}
	if _, err := feed.URL(httpkernel.UserFeedPath{Status: models.Status("unknown"), Page: 2}); err == nil {
		t.Fatal("named URL bypassed enum validation")
	}
	if location, err := httpkernel.AssetPathDescriptor().URL(httpkernel.AssetPath{File: "images/a b.png"}); err != nil || location != "/assets/images/a%20b.png" {
		t.Fatalf("generated catch-all: %q %v", location, err)
	}
}
