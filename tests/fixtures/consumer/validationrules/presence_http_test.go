package validationrules_test

import (
	"context"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"foundry.test/consumer/validationrules"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type presenceBody = validationrules.PresenceInput
type presenceRequest = foundryhttp.Input[foundryhttp.NoPath, presenceBody, presenceBody]

type presenceCalls struct {
	mu    sync.Mutex
	count int
	last  presenceRequest
}

func (c *presenceCalls) record(_ context.Context, input presenceRequest) (foundryhttp.NoContent, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.count++
	c.last = input
	return foundryhttp.NoContent{}, nil
}

func (c *presenceCalls) snapshot() (int, presenceRequest) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count, c.last
}

// These native query declarations select a transport subset of the existing DTO.
// JSON-only nullable fields are not assigned a fictitious query null spelling.
func presenceEndpoint() foundryhttp.Endpoint[foundryhttp.NoPath, presenceBody, presenceBody, foundryhttp.NoContent] {
	query := foundryhttp.DefineQuery(
		foundryhttp.OptionalQueryParam("label", foundryhttp.StringQuery[string](), func(input *presenceBody) *value.Optional[string] { return &input.Label }),
		foundryhttp.OptionalQueryParam("count", foundryhttp.IntegerQuery[int](), func(input *presenceBody) *value.Optional[int] { return &input.Count }),
		foundryhttp.OptionalQueryParam("active", foundryhttp.BoolQuery[bool](), func(input *presenceBody) *value.Optional[bool] { return &input.Active }),
	)
	return foundryhttp.DefineEndpoint(
		foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "presence.check", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/presence")),
		query,
		foundryhttp.JSONBody(validationrules.PresenceInputJSON()),
		foundryhttp.EmptyResponse(stdhttp.StatusNoContent),
	)
}

func presenceServer(t *testing.T, endpoint foundryhttp.Endpoint[foundryhttp.NoPath, presenceBody, presenceBody, foundryhttp.NoContent]) (*httptest.Server, *presenceCalls) {
	t.Helper()
	calls := &presenceCalls{}
	router, err := foundryhttp.NewRouter(endpoint.Handle(calls.record))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(router)
	server.Client().Timeout = 5 * time.Second
	t.Cleanup(server.Close)
	return server, calls
}

func sendPresence(t *testing.T, server *httptest.Server, query, body string) (int, foundryhttp.ErrorResponse) {
	t.Helper()
	path := server.URL + "/presence"
	if query != "" {
		path += "?" + query
	}
	request, err := stdhttp.NewRequestWithContext(t.Context(), stdhttp.MethodPost, path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
	if err != nil || len(data) > 64*1024 {
		t.Fatalf("bounded response: length=%d, error=%v", len(data), err)
	}
	var failure foundryhttp.ErrorResponse
	if response.StatusCode == stdhttp.StatusNoContent {
		if len(data) != 0 {
			t.Fatal("empty success contained a body")
		}
	} else if err := json.Unmarshal(data, &failure); err != nil {
		t.Fatalf("shared error response: %v; %s", err, data)
	}
	return response.StatusCode, failure
}

type presenceRule struct {
	name string
	rule validation.Rule[presenceBody]
}

type presenceCase struct {
	name string
	wire string
	want presenceBody
	// Present, Required, Prohibited, Absent: expectations from blueprint 08.
	accept [4]bool
}

func TestHTTPJSONPresenceMatrix(t *testing.T) {
	fields := validationrules.PresenceInputValidationFields()
	groups := []struct {
		name  string
		rules []presenceRule
		cases []presenceCase
	}{
		{"nickname", []presenceRule{
			{"present", fields.Nickname.Rules(validation.Present[value.Nullable[string]]())},
			{"required", fields.Nickname.Rules(validation.RequiredNullable[string]())},
			{"prohibited", fields.Nickname.Rules(validation.ProhibitedNullable[string]())},
			{"absent", fields.Nickname.Rules(validation.Absent[value.Nullable[string]]())},
		}, []presenceCase{
			{"omitted", "{}", presenceBody{}, [4]bool{false, false, true, true}},
			{"null", "{\"nickname\":null}", presenceBody{Nickname: value.Set(value.Nullable[string]{})}, [4]bool{true, false, true, false}},
			{"empty", "{\"nickname\":\"\"}", presenceBody{Nickname: value.Set(value.Of(""))}, [4]bool{true, false, true, false}},
			{"whitespace", "{\"nickname\":\" \\t\\n\"}", presenceBody{Nickname: value.Set(value.Of(" \t\n"))}, [4]bool{true, false, true, false}},
			{"unicode_whitespace", "{\"nickname\":\"\\u2003\"}", presenceBody{Nickname: value.Set(value.Of("\u2003"))}, [4]bool{true, false, true, false}},
			{"value", "{\"nickname\":\" Ada \"}", presenceBody{Nickname: value.Set(value.Of(" Ada "))}, [4]bool{true, true, false, false}},
		}},
		{"tags", []presenceRule{
			{"present", fields.Tags.Rules(validation.Present[[]string]())},
			{"required", fields.Tags.Rules(validation.Required[[]string]())},
			{"prohibited", fields.Tags.Rules(validation.Prohibited[[]string]())},
			{"absent", fields.Tags.Rules(validation.Absent[[]string]())},
		}, []presenceCase{
			{"omitted", "{}", presenceBody{}, [4]bool{false, false, true, true}},
			{"empty", "{\"tags\":[]}", presenceBody{Tags: value.Set([]string{})}, [4]bool{true, false, true, false}},
			{"one_empty_item", "{\"tags\":[\"\"]}", presenceBody{Tags: value.Set([]string{""})}, [4]bool{true, true, false, false}},
			{"value", "{\"tags\":[\"one\",\"two\"]}", presenceBody{Tags: value.Set([]string{"one", "two"})}, [4]bool{true, true, false, false}},
		}},
		{"count", []presenceRule{
			{"present", fields.Count.Rules(validation.Present[int]())},
			{"required", fields.Count.Rules(validation.Required[int]())},
			{"prohibited", fields.Count.Rules(validation.Prohibited[int]())},
			{"absent", fields.Count.Rules(validation.Absent[int]())},
		}, []presenceCase{
			{"omitted", "{}", presenceBody{}, [4]bool{false, false, true, true}},
			{"zero", "{\"count\":0}", presenceBody{Count: value.Set(0)}, [4]bool{true, true, false, false}},
			{"value", "{\"count\":7}", presenceBody{Count: value.Set(7)}, [4]bool{true, true, false, false}},
		}},
		{"active", []presenceRule{
			{"present", fields.Active.Rules(validation.Present[bool]())},
			{"required", fields.Active.Rules(validation.Required[bool]())},
			{"prohibited", fields.Active.Rules(validation.Prohibited[bool]())},
			{"absent", fields.Active.Rules(validation.Absent[bool]())},
		}, []presenceCase{
			{"omitted", "{}", presenceBody{}, [4]bool{false, false, true, true}},
			{"false", "{\"active\":false}", presenceBody{Active: value.Set(false)}, [4]bool{true, true, false, false}},
			{"true", "{\"active\":true}", presenceBody{Active: value.Set(true)}, [4]bool{true, true, false, false}},
		}},
	}
	for _, group := range groups {
		t.Run(group.name, func(t *testing.T) {
			for index, policy := range group.rules {
				t.Run(policy.name, func(t *testing.T) {
					server, calls := presenceServer(t, presenceEndpoint().WithBodyValidation(policy.rule))
					for _, tc := range group.cases {
						t.Run(tc.name, func(t *testing.T) {
							before, _ := calls.snapshot()
							status, failure := sendPresence(t, server, "", tc.wire)
							after, received := calls.snapshot()
							if tc.accept[index] {
								if status != 204 || after != before+1 || !reflect.DeepEqual(received.Body, tc.want) {
									t.Fatalf("presence changed before domain handler: status=%d, calls=%d, got=%+v, want=%+v", status, after-before, received.Body, tc.want)
								}
							} else {
								assertPresenceRejection(t, status, failure, "/body/"+group.name, policy.name)
								if after != before {
									t.Fatal("rejected body reached domain handler")
								}
							}
						})
					}
				})
			}
		})
	}
}

func assertPresenceRejection(t *testing.T, status int, failure foundryhttp.ErrorResponse, path, policy string) {
	t.Helper()
	if status != 422 || failure.Status != status || failure.Code != foundryhttp.ValidationFailed ||
		len(failure.Issues) != 1 || failure.Issues[0].Path != path ||
		string(failure.Issues[0].Code) != "foundry."+policy || failure.Issues[0].Message == "" {
		t.Fatalf("presence diagnostics: status=%d, failure=%+v", status, failure)
	}
}

func TestHTTPQueryPresenceMatrix(t *testing.T) {
	fields := validationrules.PresenceInputValidationFields()
	groups := []struct {
		name  string
		rules []presenceRule
		cases []presenceCase
	}{
		{"label", []presenceRule{
			{"present", fields.Label.Rules(validation.Present[string]())},
			{"required", fields.Label.Rules(validation.Required[string]())},
			{"prohibited", fields.Label.Rules(validation.Prohibited[string]())},
			{"absent", fields.Label.Rules(validation.Absent[string]())},
		}, []presenceCase{
			{"omitted", "", presenceBody{}, [4]bool{false, false, true, true}},
			{"bare", "label", presenceBody{Label: value.Set("")}, [4]bool{true, false, true, false}},
			{"empty", "label=", presenceBody{Label: value.Set("")}, [4]bool{true, false, true, false}},
			{"whitespace", "label=+%09%0A", presenceBody{Label: value.Set(" \t\n")}, [4]bool{true, false, true, false}},
			{"unicode_whitespace", "label=%E2%80%83", presenceBody{Label: value.Set("\u2003")}, [4]bool{true, false, true, false}},
			{"literal_null", "label=null", presenceBody{Label: value.Set("null")}, [4]bool{true, true, false, false}},
			{"value", "label=+Ada+", presenceBody{Label: value.Set(" Ada ")}, [4]bool{true, true, false, false}},
		}},
		{"count", []presenceRule{
			{"present", fields.Count.Rules(validation.Present[int]())},
			{"required", fields.Count.Rules(validation.Required[int]())},
			{"prohibited", fields.Count.Rules(validation.Prohibited[int]())},
			{"absent", fields.Count.Rules(validation.Absent[int]())},
		}, []presenceCase{
			{"omitted", "", presenceBody{}, [4]bool{false, false, true, true}},
			{"zero", "count=0", presenceBody{Count: value.Set(0)}, [4]bool{true, true, false, false}},
			{"value", "count=7", presenceBody{Count: value.Set(7)}, [4]bool{true, true, false, false}},
		}},
		{"active", []presenceRule{
			{"present", fields.Active.Rules(validation.Present[bool]())},
			{"required", fields.Active.Rules(validation.Required[bool]())},
			{"prohibited", fields.Active.Rules(validation.Prohibited[bool]())},
			{"absent", fields.Active.Rules(validation.Absent[bool]())},
		}, []presenceCase{
			{"omitted", "", presenceBody{}, [4]bool{false, false, true, true}},
			{"false", "active=false", presenceBody{Active: value.Set(false)}, [4]bool{true, true, false, false}},
			{"true", "active=true", presenceBody{Active: value.Set(true)}, [4]bool{true, true, false, false}},
		}},
	}
	for _, group := range groups {
		t.Run(group.name, func(t *testing.T) {
			for index, policy := range group.rules {
				t.Run(policy.name, func(t *testing.T) {
					server, calls := presenceServer(t, presenceEndpoint().WithQueryValidation(policy.rule))
					for _, tc := range group.cases {
						t.Run(tc.name, func(t *testing.T) {
							before, _ := calls.snapshot()
							status, failure := sendPresence(t, server, tc.wire, "{}")
							after, received := calls.snapshot()
							if tc.accept[index] {
								if status != 204 || after != before+1 || !reflect.DeepEqual(received.Query, tc.want) {
									t.Fatalf("query presence changed: status=%d, calls=%d, got=%+v, want=%+v", status, after-before, received.Query, tc.want)
								}
							} else {
								assertPresenceRejection(t, status, failure, "/query/"+group.name, policy.name)
								if after != before {
									t.Fatal("rejected query reached domain handler")
								}
							}
						})
					}
				})
			}
		})
	}
}
