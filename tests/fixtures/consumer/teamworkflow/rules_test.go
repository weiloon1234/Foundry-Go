package teamworkflow

import (
	"bytes"
	"encoding/json"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"mime/multipart"
	"net/url"
	"testing"
)

func TestOneValueRuleAcrossActualInputSources(t *testing.T) {
	app := startApp(t, pgtest.Isolate(t), pgtest.Isolate(t), Hooks{})
	for _, name := range []string{"Member", "x", "null"} {
		for _, source := range []string{"json", "query", "form", "multipart"} {
			t.Run(source+"-"+name, func(t *testing.T) {
				method, path, media := "POST", "/rules/"+source, "application/json"
				body, _ := json.Marshal(map[string]string{"name": name})
				switch source {
				case "query":
					method = "GET"
					path += "?name=" + url.QueryEscape(name)
					body = nil
					media = ""
				case "form":
					media = "application/x-www-form-urlencoded"
					body = []byte(url.Values{"name": {name}}.Encode())
				case "multipart":
					var buffer bytes.Buffer
					writer := multipart.NewWriter(&buffer)
					if err := writer.WriteField("name", name); err != nil {
						t.Fatal(err)
					}
					if err := writer.Close(); err != nil {
						t.Fatal(err)
					}
					media = writer.FormDataContentType()
					body = buffer.Bytes()
				}
				response, err := app.send(t.Context(), method, path, "", "", media, body)
				if err != nil {
					t.Fatal(err)
				}
				want := 200
				if name == "x" {
					want = 422
				}
				if response.Status() != want {
					t.Fatalf("%s rule status %d: %s", source, response.Status(), response.Bytes())
				}
				if want == 200 {
					var reply RuleReply
					if json.Unmarshal(response.Bytes(), &reply) != nil || reply.Name != name {
						t.Fatal("input source changed text, including literal null")
					}
				}
			})
		}
	}
	for _, sample := range []struct{ method, path, media, body, field string }{
		{"POST", "/rules/json", "application/json", `{"name":2}`, "/body/name"},
		{"GET", "/rules/query?name=one&name=two", "", "", "/query/name"},
		{"POST", "/rules/form", "application/x-www-form-urlencoded", "name=one&name=two", "/body/name"},
	} {
		response, err := app.send(t.Context(), sample.method, sample.path, "", "", sample.media, []byte(sample.body))
		if err != nil {
			t.Fatal(err)
		}
		if response.Status() != 400 || !bytes.Contains(response.Bytes(), []byte(sample.field)) {
			t.Fatalf("source error lost field path: %d %s", response.Status(), response.Bytes())
		}
	}
}
