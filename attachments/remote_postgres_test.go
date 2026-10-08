package attachments

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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

func remoteManager(t *testing.T, f attachmentFixture, config Config) *Manager {
	t.Helper()
	m, err := New(Dependencies{Store: f.Store, Disks: f.registry}, config, testRemote.Registration())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return m
}

func awaitRemotePhase(t *testing.T, phase <-chan struct{}) {
	t.Helper()
	select {
	case <-phase:
	case <-time.After(5 * time.Second):
		t.Fatal("remote upload did not reach expected phase")
	}
}

type remoteOutcome struct {
	result Result[extensiontest.Member, int64]
	err    error
}

func awaitRemoteResult(t *testing.T, completed <-chan remoteOutcome) remoteOutcome {
	t.Helper()
	select {
	case outcome := <-completed:
		return outcome
	case <-time.After(5 * time.Second):
		t.Fatal("remote upload did not return")
		return remoteOutcome{}
	}
}

func TestPostgresAttachmentRemoteAdmissionCoversDownloadAndStorage(t *testing.T) {
	downloaded, finishDownload := make(chan struct{}), make(chan struct{})
	storing, finishStore := make(chan struct{}), make(chan struct{})
	var downloadOnce, storeOnce sync.Once
	defer downloadOnce.Do(func() { close(finishDownload) })
	defer storeOnce.Do(func() { close(finishStore) })
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path == "/slow" {
			_, _ = w.Write([]byte("remote "))
			w.(http.Flusher).Flush()
			close(downloaded)
			select {
			case <-finishDownload:
			case <-r.Context().Done():
				return
			}
			_, _ = w.Write([]byte("bytes"))
			return
		}
		_, _ = w.Write([]byte("next"))
	}))
	t.Cleanup(server.Close)
	f := openAttachments(t, testRemote.Registration())
	config := DefaultConfig()
	config.MaxActive = 1
	m := remoteManager(t, f, config)
	client := remoteClient(t, server, true)
	f.backend.setPut(func(ctx context.Context, key storage.ObjectKey, source io.Reader, options storage.PutOptions) (storage.ObjectInfo, error) {
		close(storing)
		<-finishStore
		return f.backend.Backend.Put(ctx, key, source, options)
	})
	owner := member(t, 1)
	completed := make(chan remoteOutcome, 1)
	go func() {
		result, err := testRemote.AddFromURL(t.Context(), m, owner, RemoteSource{Client: client, URL: server.URL + "/slow", OriginalName: "slow.txt"})
		completed <- remoteOutcome{result, err}
	}()
	awaitRemotePhase(t, downloaded)
	for _, phase := range []string{"download", "storage"} {
		busy, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		result, err := testRemote.AddFromURL(busy, m, owner, RemoteSource{Client: client, URL: server.URL + "/next", OriginalName: "next.txt"})
		cancel()
		if !errors.Is(err, fault.Overloaded) || !result.Operation.IsZero() || result.Publication != Unpublished {
			t.Fatal("remote import bypassed admission during", phase, err)
		}
		if requests.Load() != 1 {
			t.Fatal("queued remote import sent a request during", phase)
		}
		if phase == "download" {
			files, err := testRemote.List(t.Context(), m, owner)
			if err != nil || len(files) != 0 {
				t.Fatal("remote download blocked separate read capacity", err)
			}
			downloadOnce.Do(func() { close(finishDownload) })
			awaitRemotePhase(t, storing)
		}
	}
	storeOnce.Do(func() { close(finishStore) })
	outcome := awaitRemoteResult(t, completed)
	if outcome.err != nil || outcome.result.Publication != Published {
		t.Fatal("remote import could not publish with one admission slot", outcome.err)
	}
	f.backend.setPut(nil)
	file := attachmentOf(t, outcome.result)
	if body, err := testRemote.ReadBytes(t.Context(), m, owner, file.ID(), 1024); err != nil || string(body) != "remote bytes" {
		t.Fatal("remote upload bytes or checksum changed", err)
	}
	if _, err := testRemote.AddFromURL(t.Context(), m, owner, RemoteSource{Client: client, URL: server.URL + "/next", OriginalName: "next.txt"}); err != nil {
		t.Fatal("remote import retained admission after publication", err)
	}
	if err := m.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := testRemote.AddFromURL(t.Context(), m, owner, RemoteSource{Client: client, URL: server.URL + "/next", OriginalName: "next.txt"}); !errors.Is(err, fault.Closed) {
		t.Fatal("closed manager admitted a remote import", err)
	}
	if requests.Load() != 2 {
		t.Fatal("closed manager sent a remote request")
	}
}

func TestPostgresAttachmentRemoteFailuresReleaseAdmissionWithoutIntent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/short":
			w.Header().Set("Content-Length", "12")
			_, _ = w.Write([]byte("short"))
		case "/large":
			w.(http.Flusher).Flush()
			_, _ = w.Write([]byte(strings.Repeat("x", 64)))
		case "/unsupported":
			_, _ = w.Write([]byte{0, 255, 0, 255})
		case "/redirect":
			http.Redirect(w, r, "/valid", http.StatusFound)
		case "/failed":
			http.Error(w, "failed", http.StatusServiceUnavailable)
		default:
			_, _ = w.Write([]byte("valid"))
		}
	}))
	t.Cleanup(server.Close)
	f := openAttachments(t, testRemote.Registration())
	config := DefaultConfig()
	config.MaxActive = 1
	m := remoteManager(t, f, config)
	client := remoteClient(t, server, true)
	owner := member(t, 1)
	for i, address := range []string{"/short", "/large", "/unsupported", "/redirect", "/failed"} {
		t.Run(address, func(t *testing.T) {
			result, err := testRemote.AddFromURL(t.Context(), m, owner, RemoteSource{Client: client, URL: server.URL + address, OriginalName: "import.txt"})
			if err == nil || !result.Operation.IsZero() || result.Publication != Unpublished {
				t.Fatal("failed download or validation created an upload intent", err)
			}
			if files, err := testRemote.List(t.Context(), m, owner); err != nil || len(files) != 0 {
				t.Fatal("failed remote import changed collection membership", err)
			}
			if _, err := testRemote.AddFromURL(t.Context(), m, member(t, int64(i+2)), RemoteSource{Client: client, URL: server.URL + "/valid", OriginalName: "valid.txt"}); err != nil {
				t.Fatal("remote failure retained manager admission", err)
			}
		})
	}
}

func TestPostgresAttachmentRemoteDownloadCancellationTimeoutAndShutdown(t *testing.T) {
	for _, mode := range []string{"cancellation", "timeout", "shutdown"} {
		t.Run(mode, func(t *testing.T) {
			entered, ended := make(chan struct{}), make(chan struct{})
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.URL.Path == "/held" {
					_, _ = w.Write([]byte("partial"))
					w.(http.Flusher).Flush()
					close(entered)
					<-r.Context().Done()
					close(ended)
					return
				}
				_, _ = w.Write([]byte("valid"))
			}))
			t.Cleanup(server.Close)
			f := openAttachments(t, testRemote.Registration())
			config := DefaultConfig()
			config.MaxActive = 1
			if mode == "timeout" {
				config.Timeout = 500 * time.Millisecond
			}
			m := remoteManager(t, f, config)
			client := remoteClient(t, server, true)
			owner := member(t, 1)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			completed := make(chan remoteOutcome, 1)
			go func() {
				result, err := testRemote.AddFromURL(ctx, m, owner, RemoteSource{Client: client, URL: server.URL + "/held", OriginalName: "held.txt"})
				completed <- remoteOutcome{result, err}
			}()
			awaitRemotePhase(t, entered)
			want := context.Canceled
			switch mode {
			case "cancellation":
				cancel()
			case "timeout":
				want = context.DeadlineExceeded
			case "shutdown":
				closeCtx, closeCancel := context.WithTimeout(t.Context(), 5*time.Second)
				err := m.Close(closeCtx)
				closeCancel()
				if err != nil {
					t.Fatal("manager failed to cancel and drain remote download", err)
				}
			}
			outcome := awaitRemoteResult(t, completed)
			if !errors.Is(outcome.err, want) || !outcome.result.Operation.IsZero() || outcome.result.Publication != Unpublished {
				t.Fatal("interrupted remote download created an intent or lost cancellation", outcome.err)
			}
			awaitRemotePhase(t, ended)
			result, err := testRemote.AddFromURL(t.Context(), m, owner, RemoteSource{Client: client, URL: server.URL + "/valid", OriginalName: "valid.txt"})
			if mode == "shutdown" {
				if !errors.Is(err, fault.Closed) || requests.Load() != 1 {
					t.Fatal("shutdown manager sent or admitted another remote import", err)
				}
				awaitRemotePhase(t, m.Done())
			} else if err != nil || result.Publication != Published {
				t.Fatal("interrupted remote download retained admission", err)
			}
		})
	}
}
