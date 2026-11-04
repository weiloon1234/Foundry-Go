package clientcontracts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"foundry.test/consumer/clientcontracts"
	"foundry.test/consumer/localization"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/typescript"
)

func TestGeneratedDetailedErrorBoundaryAndRedaction(t *testing.T) {
	id, err := model.ParseID[models.User]("0193fd8c-2075-7000-8000-000000000099")
	if err != nil {
		t.Fatal(err)
	}
	undeclared := foundryhttp.DefineErrorDetails("member_unlisted", 409, "Unlisted member.", clientcontracts.MemberExistsDetailsJSON())
	cause := errors.New("private-detail-cause")
	rejected := undeclared.New(clientcontracts.MemberExistsDetails{ExistingMemberID: id, Existing: clientcontracts.MemberConflictState{Name: "private-detail-payload", State: localization.Ready}}).WithCause(cause)
	if !undeclared.Matches(rejected) || clientcontracts.MemberExists.Matches(rejected) || !errors.Is(rejected, cause) {
		t.Fatal("declaration matching or cause identity changed")
	}
	router, err := foundryhttp.NewRouter(clientcontracts.MemberCreate.Handle(func(_ context.Context, in foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, clientcontracts.CreateMember]) (clientcontracts.MemberCreated, error) {
		if in.Body.Name == "healthy" {
			return clientcontracts.MemberCreated{ID: id}, nil
		}
		return clientcontracts.MemberCreated{}, rejected
	}))
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	config := foundryhttp.DefaultServerConfig()
	config.Address = "127.0.0.1:0"
	server, err := foundryhttp.Prepare(router, config, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-server.Done():
		case <-time.After(5 * time.Second):
			t.Error("server did not stop")
		}
	})
	ready, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()
	address, err := server.Ready(ready)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	for _, tc := range []struct {
		name   string
		status int
	}{{"rejected", 500}, {"healthy", 201}} {
		response, err := client.Post("http://"+address+"/members", "application/json", strings.NewReader(`{"name":"`+tc.name+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != tc.status {
			t.Fatalf("response: %d %s %v", response.StatusCode, body, err)
		}
		if tc.status == 500 {
			if !bytes.Contains(body, []byte(`"error_code":"internal_error"`)) {
				t.Fatal("undeclared details did not fail closed", string(body))
			}
			for _, hidden := range []string{"details", "private-detail-payload", "private-detail-cause", id.String()} {
				if bytes.Contains(body, []byte(hidden)) {
					t.Fatal("private detail reached response", hidden)
				}
			}
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop")
	}
	if !strings.Contains(logs.String(), "HTTP request failed") || !strings.Contains(logs.String(), "error_code=internal_error") {
		t.Fatal("server failure was not diagnosed")
	}
	for _, hidden := range []string{"private-detail-payload", "private-detail-cause", id.String()} {
		if strings.Contains(logs.String(), hidden) {
			t.Fatal("private detail reached logs", hidden)
		}
	}
}

// The real server knows Ready; this temporary client predates that enum case.
func olderErrorClient(t *testing.T, source *manifest.Manifest) []byte {
	t.Helper()
	doc, err := source.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for i := range doc.Types {
		if doc.Types[i].ID == "foundry.test/consumer/localization.Status" {
			cases := doc.Types[i].Cases[:0]
			for _, value := range doc.Types[i].Cases {
				if string(value) != `"ready"` {
					cases = append(cases, value)
				}
			}
			doc.Types[i].Cases = cases
		}
	}
	for i := range doc.Enums {
		if doc.Enums[i].PackagePath == "foundry.test/consumer/localization" && doc.Enums[i].Name == "Status" {
			cases := doc.Enums[i].Cases[:0]
			for _, value := range doc.Enums[i].Cases {
				if string(value.Value) != `"ready"` {
					cases = append(cases, value)
				}
			}
			doc.Enums[i].Cases = cases
		}
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	older, err := manifest.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	output, err := typescript.Render(older)
	if err != nil {
		t.Fatal(err)
	}
	return output
}
