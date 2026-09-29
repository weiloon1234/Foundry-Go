package attachments

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/internal/extensiontest"
	"github.com/weiloon1234/Foundry-Go/storage"
)

var testRemote = Define(extensiontest.Members, "imports", Policy{Disk: testDisk, Cardinality: Multiple, MaxFiles: 3, MaxBytes: 32, Accepted: []storage.MediaType{"text/plain", "text/csv"}})

func remoteClient(t *testing.T, server *httptest.Server, restricted bool) *httpclient.Client {
	t.Helper()
	config := httpclient.DefaultConfig("imports")
	config.Retry = httpclient.NoRetries()
	if restricted {
		address, err := url.Parse(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		port, err := strconv.Atoi(address.Port())
		if err != nil {
			t.Fatal(err)
		}
		config.Destination = httpclient.DestinationPolicy{Mode: httpclient.RestrictedDestinations, Schemes: []httpclient.Scheme{httpclient.HTTP}, Ports: []uint16{uint16(port)}, Networks: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}}
	}
	client, err := httpclient.New(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return client
}

func TestPostgresAttachmentAddFromURLUsesRestrictedClientAndPolicy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/exports/rows.csv":
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			_, _ = w.Write([]byte("a,b\n1,2\n"))
		case "/large.txt":
			_, _ = w.Write([]byte(strings.Repeat("x", 64)))
		case "/moved":
			http.Redirect(w, r, "/exports/rows.csv", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	f := openAttachments(t, testRemote.Registration())
	owner := member(t, 1)
	client := remoteClient(t, server, true)
	result, err := testRemote.AddFromURL(t.Context(), f.manager, owner, RemoteSource{Client: client, URL: server.URL + "/exports/rows.csv"})
	if err != nil || result.Publication != Published {
		t.Fatal("remote import failed", err)
	}
	file := attachmentOf(t, result)
	if file.Info().OriginalName != "rows.csv" || file.Info().MediaType != "text/csv" {
		t.Fatal("remote import lost its name or accepted media", file.Info())
	}
	if body, err := testRemote.ReadBytes(t.Context(), f.manager, owner, file.ID(), 1024); err != nil || string(body) != "a,b\n1,2\n" {
		t.Fatal("remote bytes changed", err)
	}
	// An unrestricted client could reach internal services from untrusted URLs.
	if _, err := testRemote.AddFromURL(t.Context(), f.manager, owner, RemoteSource{Client: remoteClient(t, server, false), URL: server.URL + "/exports/rows.csv"}); !errors.Is(err, fault.Invalid) {
		t.Fatal("unrestricted client accepted for untrusted URLs", err)
	}
	for _, address := range []string{server.URL + "/large.txt", server.URL + "/moved", server.URL + "/missing", "http://169.254.169.254/latest/meta-data"} {
		if _, err := testRemote.AddFromURL(t.Context(), f.manager, owner, RemoteSource{Client: client, URL: address, OriginalName: "import.txt"}); err == nil {
			t.Fatal("oversized, redirected, failed or forbidden remote file imported", address)
		}
	}
	files, err := testRemote.List(t.Context(), f.manager, owner)
	if err != nil || len(files) != 1 {
		t.Fatal("rejected imports changed the collection", err, len(files))
	}
}
