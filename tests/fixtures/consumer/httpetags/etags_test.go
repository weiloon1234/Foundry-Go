package httpetags_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"foundry.test/consumer/httpetags"
	"foundry.test/consumer/httppagination"
	"foundry.test/consumer/mutatorqueries"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type directory struct {
	mu     sync.Mutex
	member mutatorqueries.Member
	calls  int
}

func (d *directory) Find(ctx context.Context, id model.ID[mutatorqueries.Member]) (mutatorqueries.Member, error) {
	if err := ctx.Err(); err != nil {
		return mutatorqueries.Member{}, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	if d.member.ID != id {
		return mutatorqueries.Member{}, foundryhttp.NotFound
	}
	return d.member, nil
}
func (d *directory) setEmail(email string) { d.mu.Lock(); defer d.mu.Unlock(); d.member.Email = email }
func (d *directory) snapshot() (mutatorqueries.Member, int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.member, d.calls
}

func TestAutomaticETagConsumerPreservesTypedGetterDTOs(t *testing.T) {
	id, err := model.ParseID[mutatorqueries.Member]("0193fd8c-2075-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	store := &directory{member: mutatorqueries.Member{ID: id, Email: "stored@example.test", Nickname: value.Of("nickname")}}
	handler, err := httpetags.Handler(store)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client := server.Client()
	client.Timeout = 3 * time.Second
	path, err := httpetags.Show.URL(t.Context(), httpetags.MemberPath{Member: id}, foundryhttp.NoQuery{})
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, tag string) (*stdhttp.Response, []byte) {
		t.Helper()
		request, err := stdhttp.NewRequestWithContext(t.Context(), method, server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if tag != "" {
			request.Header.Set("If-None-Match", tag)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return response, body
	}
	first, body := call("GET", "")
	hash := sha256.Sum256(body)
	tag := "\"" + hex.EncodeToString(hash[:]) + "\""
	if first.StatusCode != 200 || first.Header.Get("ETag") != tag {
		t.Fatal("validator does not describe actual DTO bytes", first.StatusCode, first.Header)
	}
	var dto httppagination.MemberResponse
	if err = json.Unmarshal(body, &dto); err != nil {
		t.Fatal(err)
	}
	if dto.ID != id || dto.Email != "STORED@EXAMPLE.TEST" {
		t.Fatal("typed presenter lost getter output", dto)
	}
	if nickname, _ := dto.Nickname.Get(); nickname != "NICKNAME" {
		t.Fatal("nullable getter was bypassed")
	}
	stored, calls := store.snapshot()
	if stored.Email != "stored@example.test" || stored.ID != id || calls != 1 {
		t.Fatal("stored fields changed or handler ran twice")
	}

	unchanged, body := call("GET", "W/"+tag)
	if unchanged.StatusCode != 304 || len(body) != 0 || unchanged.Header.Get("ETag") != tag {
		t.Fatal("revalidation failed", unchanged.StatusCode)
	}
	head, body := call("HEAD", tag)
	if head.StatusCode != 200 || len(body) != 0 || head.ContentLength <= 0 || head.Header.Get("ETag") != "" {
		t.Fatal("HEAD invented an unseen-body validator", head.StatusCode, head.Header)
	}
	if _, calls = store.snapshot(); calls != 3 {
		t.Fatal("HEAD or conditional GET repeated/skipped domain evaluation", calls)
	}
	store.setEmail("updated@example.test")
	changed, body := call("GET", tag)
	if changed.StatusCode != 200 || changed.Header.Get("ETag") == tag || !strings.Contains(string(body), "UPDATED@EXAMPLE.TEST") {
		t.Fatal("model change retained stale DTO or validator")
	}
	store.setEmail("hidden@example.test")
	failure, body := call("GET", "*")
	if failure.StatusCode != 500 || failure.Header.Get("ETag") != "" || strings.Contains(string(body), "hidden@example.test") {
		t.Fatal("getter failure was validated or exposed", failure.StatusCode, string(body))
	}
}
