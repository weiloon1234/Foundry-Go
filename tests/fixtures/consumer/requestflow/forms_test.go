package requestflow_test

import (
	"bytes"
	"encoding/json"
	"foundry.test/consumer/requestflow"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/openapi"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSameNameRulesAcrossInputSources(t *testing.T) {
	router, err := requestflow.RuleSourceRouter()
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"json", "multipart", "query", "form"} {
		for _, name := range []string{"Jane", "x"} {
			t.Run(source+"/"+name, func(t *testing.T) {
				path, method, media := "/"+source, "POST", ""
				var body io.Reader
				switch source {
				case "json":
					body = strings.NewReader(`{"name":"` + name + `"}`)
					media = "application/json"
				case "multipart":
					var wire bytes.Buffer
					writer := multipart.NewWriter(&wire)
					if err := writer.WriteField("name", name); err != nil {
						t.Fatal(err)
					}
					writer.Close()
					body = &wire
					media = writer.FormDataContentType()
				case "query":
					method = "GET"
					path += "?name=" + name
				case "form":
					body = strings.NewReader("name=" + name)
					media = "application/x-www-form-urlencoded"
				}
				req := httptest.NewRequest(method, path, body)
				if media != "" {
					req.Header.Set("Content-Type", media)
				}
				res := httptest.NewRecorder()
				router.ServeHTTP(res, req)
				want := 204
				if name == "x" {
					want = 422
				}
				if res.Code != want {
					t.Fatalf("%d: %s", res.Code, res.Body.String())
				}
				if want == 422 {
					var failure foundryhttp.ErrorResponse
					if err := json.Unmarshal(res.Body.Bytes(), &failure); err != nil {
						t.Fatal(err)
					}
					prefix := "/body"
					if source == "query" {
						prefix = "/query"
					}
					if len(failure.Issues) != 1 || failure.Issues[0].Path != prefix+"/name" {
						t.Fatalf("%+v", failure.Issues)
					}
				}
			})
		}
	}
}
func TestFormManifestAndOpenAPI(t *testing.T) {
	router, err := foundryhttp.NewRouter(requestflow.Submit.Handle(requestflow.Handle))
	if err != nil {
		t.Fatal(err)
	}
	source, err := manifest.Build(t.Context(), manifest.Sources{HTTP: router})
	if err != nil {
		t.Fatal(err)
	}
	data, err := source.JSON()
	if err != nil {
		t.Fatal(err)
	}
	roundtrip, err := manifest.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	document, err := roundtrip.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	op := document.HTTP[0]
	if !op.Preparation || op.Body.MediaType != "application/x-www-form-urlencoded" || len(op.Body.Fields) != 4 || op.Body.Type != "" {
		t.Fatalf("form contract lost: %+v", op)
	}
	spec, err := openapi.Render(roundtrip, openapi.Options{Title: "Forms", APIVersion: "1"})
	if err != nil {
		t.Fatal(err)
	}
	var api map[string]any
	if err := json.Unmarshal(spec, &api); err != nil {
		t.Fatal(err)
	}
	post := api["paths"].(map[string]any)["/form"].(map[string]any)["post"].(map[string]any)
	if post["x-foundry-request-preparation"] != true {
		t.Fatal("missing preparation extension")
	}
	content := post["requestBody"].(map[string]any)["content"].(map[string]any)
	if len(content) != 1 || content["application/x-www-form-urlencoded"] == nil {
		t.Fatal("wrong form media")
	}
	form := content["application/x-www-form-urlencoded"].(map[string]any)
	schema := form["schema"].(map[string]any)
	if schema["additionalProperties"] != false {
		t.Fatal("undeclared form fields allowed")
	}
	props := schema["properties"].(map[string]any)
	if props["tags[]"].(map[string]any)["type"] != "array" {
		t.Fatal("repeated form lost")
	}
	// Returned metadata cannot change the source; invalid cross-media fields fail closed.
	document.HTTP[0].Body.MediaType = "application/json"
	bad, _ := json.Marshal(document)
	if _, err := manifest.Decode(bad); err == nil {
		t.Fatal("form fields accepted as JSON")
	}
}
