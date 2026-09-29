package http

import (
	"bytes"
	"encoding/json"
	stdhttp "net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/internal/jsonwire"
)

// AssertJSONPath compares the JSON value at an RFC 6901 pointer (for example
// "/data/items/0/name"; "" selects the document) with want encoded by Go's
// JSON encoder. Object key order and insignificant whitespace are ignored.
// Prefer DecodeJSON with the generated DTO contract for complete responses.
func AssertJSONPath[T any](t testing.TB, response httpclient.Response, pointer string, want T) {
	t.Helper()
	document, ok := responseDocument(t, response)
	if !ok {
		return
	}
	actual, found := jsonPointer(document, pointer)
	if !found {
		t.Errorf("JSON path %q is absent", pointer)
		return
	}
	expected, err := json.Marshal(want)
	if err != nil {
		t.Errorf("encode expected JSON path %q value: %v", pointer, err)
		return
	}
	expectedNode, err := jsonwire.Decode(expected, jsonwire.Limits{Bytes: len(expected), Depth: jsonwire.MaxDepth, Nodes: 1 << 20})
	if err != nil {
		t.Errorf("decode expected JSON path %q value: %v", pointer, err)
		return
	}
	got, gotErr := json.Marshal(actual)
	wanted, wantErr := json.Marshal(expectedNode)
	if gotErr != nil || wantErr != nil || !bytes.Equal(got, wanted) {
		t.Errorf("JSON path %q: got %s, want %s", pointer, got, wanted)
	}
}

// AssertValidationErrors requires the framework's 422 validation envelope with
// an issue at every listed field. Fields are wire JSON pointers such as
// "/email"; a missing leading slash is added. Issue messages are not compared.
func AssertValidationErrors(t testing.TB, response httpclient.Response, fields ...string) {
	t.Helper()
	if response.Status() != stdhttp.StatusUnprocessableEntity {
		t.Errorf("HTTP status: got %d, want %d for validation errors", response.Status(), stdhttp.StatusUnprocessableEntity)
		return
	}
	envelope, err := DecodeJSON(t.Context(), response, foundryhttp.ErrorResponseJSON())
	if err != nil {
		t.Errorf("decode validation error envelope: %v", err)
		return
	}
	if envelope.Code != foundryhttp.ValidationFailed {
		t.Errorf("error code: got %q, want %q", envelope.Code, foundryhttp.ValidationFailed)
		return
	}
	paths := make([]string, 0, len(envelope.Issues))
	for _, issue := range envelope.Issues {
		paths = append(paths, issue.Path)
	}
	for _, field := range fields {
		if !strings.HasPrefix(field, "/") {
			field = "/" + field
		}
		if !slices.ContainsFunc(envelope.Issues, func(issue contract.Issue) bool { return issue.Path == field }) {
			t.Errorf("validation issue for %q is absent; issues are at %v", field, paths)
		}
	}
}

// AssertHeader compares a response header's first value. An absent header
// matches only an empty want.
func AssertHeader(t testing.TB, response httpclient.Response, name, want string) {
	t.Helper()
	if got := response.Headers().Get(name); got != want {
		t.Errorf("HTTP header %s: got %q, want %q", name, got, want)
	}
}

// AssertCookie requires exactly one Set-Cookie for name and returns it for
// attribute checks. Secret values are never printed.
func AssertCookie(t testing.TB, response httpclient.Response, name string) *stdhttp.Cookie {
	t.Helper()
	var matched []*stdhttp.Cookie
	for _, line := range response.Headers().Values("Set-Cookie") {
		cookie, err := stdhttp.ParseSetCookie(line)
		if err == nil && cookie.Name == name {
			matched = append(matched, cookie)
		}
	}
	if len(matched) != 1 {
		t.Errorf("Set-Cookie %s: got %d, want exactly one", name, len(matched))
		return nil
	}
	return matched[0]
}

// AssertRedirect requires a 3xx status and an exact Location. The test client
// never follows redirects, so the original response is inspected.
func AssertRedirect(t testing.TB, response httpclient.Response, location string) {
	t.Helper()
	if response.Status() < 300 || response.Status() > 399 {
		t.Errorf("HTTP status: got %d, want a redirect", response.Status())
		return
	}
	AssertHeader(t, response, "Location", location)
}

// AssertNoContent requires status 204 and an empty body.
func AssertNoContent(t testing.TB, response httpclient.Response) {
	t.Helper()
	AssertStatus(t, response, stdhttp.StatusNoContent)
	if body := response.Bytes(); len(body) != 0 {
		t.Errorf("HTTP body: got %d byte(s), want none", len(body))
	}
}

func responseDocument(t testing.TB, response httpclient.Response) (any, bool) {
	t.Helper()
	data := response.Bytes()
	document, err := jsonwire.Decode(data, jsonwire.Limits{Bytes: max(len(data), 1), Depth: jsonwire.MaxDepth, Nodes: 1 << 20})
	if err != nil {
		t.Errorf("HTTP response is not a JSON document: %v", err)
		return nil, false
	}
	return document, true
}

// jsonPointer follows RFC 6901 through decoded objects and arrays.
func jsonPointer(document any, pointer string) (any, bool) {
	if pointer == "" {
		return document, true
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, false
	}
	current := document
	for _, token := range strings.Split(pointer[1:], "/") {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		switch node := current.(type) {
		case map[string]any:
			next, ok := node[token]
			if !ok {
				return nil, false
			}
			current = next
		case []any:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(node) || (len(token) > 1 && token[0] == '0') {
				return nil, false
			}
			current = node[index]
		default:
			return nil, false
		}
	}
	return current, true
}
