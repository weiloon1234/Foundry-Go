package idempotenthttp

import (
	"bytes"
	"encoding/json"
	"foundry.test/consumer/internal/clientfixture"
	"testing"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/openapi"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

func TestIdempotentManifestAndTypeScriptHTTP(t *testing.T) {
	tools := clientfixture.Load(t)
	app := startApp(t, pgtest.Isolate(t), Hooks{}, nil)
	router, err := foundation.Resolve(app.app.Services(), application.RouterKey)
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
	if err != nil {
		t.Fatal(err)
	}
	document, err := source.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(document.HTTP) != 1 || document.HTTP[0].Idempotency == nil || document.HTTP[0].Idempotency.Header != "Idempotency-Key" {
		t.Fatal("actual replay contract was omitted")
	}
	api, err := openapi.Render(source, openapi.Options{Title: "Idempotent API", APIVersion: "1"})
	if err != nil || !bytes.Contains(api, []byte(`"x-foundry-idempotency"`)) || !bytes.Contains(api, []byte(`"Idempotency-Key"`)) {
		t.Fatal("OpenAPI omitted replay contract", err)
	}
	metadata, err := source.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manifest.Decode(metadata); err != nil {
		t.Fatal(err)
	}
	malformed := document
	malformed.HTTP[0].Idempotency.SuccessOnly = false
	bad, _ := json.Marshal(malformed)
	if _, err := manifest.Decode(bad); err == nil {
		t.Fatal("manifest accepted unsupported failure caching")
	}
	tools.Check(t, source, app.url, "testdata")
}
