package http_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type queryUser struct{}
type queryLabels []string
type querySearch struct {
	User   model.ID[queryUser]
	Search value.Optional[string]
	Page   value.Optional[uint16]
	Active value.Optional[bool]
	Labels queryLabels
}

var queryLimits = foundryhttp.QueryLimits{Bytes: 4096, Pairs: 128, Issues: 10}

func searchQuery() foundryhttp.Query[querySearch] {
	return foundryhttp.DefineQuery(
		foundryhttp.QueryParam("user", foundryhttp.ModelIDQuery[queryUser](), func(q *querySearch) *model.ID[queryUser] { return &q.User }),
		foundryhttp.OptionalQueryParam("q", foundryhttp.StringQuery[string](), func(q *querySearch) *value.Optional[string] { return &q.Search }),
		foundryhttp.OptionalQueryParam("page", foundryhttp.IntegerQuery[uint16](), func(q *querySearch) *value.Optional[uint16] { return &q.Page }),
		foundryhttp.OptionalQueryParam("active", foundryhttp.BoolQuery[bool](), func(q *querySearch) *value.Optional[bool] { return &q.Active }),
		foundryhttp.RepeatedQueryParam("label", foundryhttp.StringQuery[string](), func(q *querySearch) *queryLabels { return &q.Labels }),
	)
}

const queryUserID = "0192f915-3cf3-7000-8000-000000000001"

func TestTypedQueryRoundTrip(t *testing.T) {
	t.Parallel()
	d := searchQuery()
	decoded, err := d.Decode(context.Background(), "q=Jane+Doe%2B&page=0&active=false&label=a%2Cb&label=%E6%9D%8E&user="+queryUserID, queryLimits)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.User.String() != queryUserID {
		t.Fatal("model identity changed")
	}
	if q, ok := decoded.Search.Get(); !ok || q != "Jane Doe+" {
		t.Fatal("text did not decode exactly once")
	}
	if page, ok := decoded.Page.Get(); !ok || page != 0 {
		t.Fatal("present zero page lost")
	}
	if active, ok := decoded.Active.Get(); !ok || active {
		t.Fatal("present false lost")
	}
	if !reflect.DeepEqual(decoded.Labels, queryLabels{"a,b", "李"}) {
		t.Fatalf("list expanded implicitly or changed: %v", decoded.Labels)
	}
	encoded, err := d.Encode(context.Background(), decoded, queryLimits)
	want := "active=false&label=a%2Cb&label=%E6%9D%8E&page=0&q=Jane+Doe%2B&user=" + queryUserID
	if err != nil || encoded != want {
		t.Fatalf("got %q, %v; want %q", encoded, err, want)
	}
	roundTrip, err := d.Decode(context.Background(), encoded, queryLimits)
	if err != nil || !reflect.DeepEqual(decoded, roundTrip) {
		t.Fatalf("round trip: %v", err)
	}
}

func TestTypedQueryOmissionAndEmpty(t *testing.T) {
	t.Parallel()
	d := searchQuery()
	decoded, err := d.Decode(context.Background(), "user="+queryUserID, queryLimits)
	if err != nil || decoded.Search.IsSet() || decoded.Labels != nil {
		t.Fatalf("omission: %+v, %v", decoded, err)
	}
	decoded, err = d.Decode(context.Background(), "q&label=&user="+queryUserID, queryLimits)
	if err != nil {
		t.Fatal(err)
	}
	if text, ok := decoded.Search.Get(); !ok || text != "" {
		t.Fatal("explicit empty value lost")
	}
	if !reflect.DeepEqual(decoded.Labels, queryLabels{""}) {
		t.Fatal("empty list element lost")
	}
	decoded.Labels = queryLabels{}
	encoded, err := d.Encode(context.Background(), decoded, queryLimits)
	if err != nil || encoded != "q=&user="+queryUserID {
		t.Fatalf("empty slice encoding: %q, %v", encoded, err)
	}
}

func TestTypedQueryFieldIssues(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, raw, path string
		code            contract.IssueCode
	}{
		{"missing", "", "/user", contract.RequiredIssue},
		{"duplicate", "page=1&%70age=2&user=" + queryUserID, "/page", contract.LengthIssue},
		{"unknown", "secret=value&user=" + queryUserID, "", contract.UnknownIssue},
		{"wrong-case", "User=" + queryUserID, "", contract.UnknownIssue},
		{"integer-range", "page=65536&user=" + queryUserID, "/page", contract.ValueIssue},
		{"integer-shape", "page=01&user=" + queryUserID, "/page", contract.ValueIssue},
		{"boolean-shape", "active=1&user=" + queryUserID, "/active", contract.ValueIssue},
		{"empty-id", "user=00000000-0000-0000-0000-000000000000", "/user", contract.ValueIssue},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := searchQuery().Decode(context.Background(), tc.raw, queryLimits)
			if !reflect.DeepEqual(got, querySearch{}) {
				t.Fatal("partial decoded value escaped")
			}
			var failure *foundryhttp.QueryError
			if !errors.As(err, &failure) || !errors.Is(err, fault.Invalid) {
				t.Fatalf("missing typed failure: %v", err)
			}
			issues := failure.Issues()
			if len(issues) == 0 || issues[0].Path != tc.path || issues[0].Code != tc.code {
				t.Fatalf("issues: %+v", issues)
			}
			issues[0].Path = "changed"
			if failure.Issues()[0].Path == "changed" {
				t.Fatal("issues share mutable storage")
			}
			if strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), "secret") {
				t.Fatal("received query text leaked")
			}
		})
	}
	limits := queryLimits
	limits.Issues = 1
	_, err := searchQuery().Decode(context.Background(), "secret=x&page=1&page=2", limits)
	var failure *foundryhttp.QueryError
	if !errors.As(err, &failure) || len(failure.Issues()) != 1 {
		t.Fatal("issue limit not enforced")
	}
}

type queryFuncCodec struct {
	parse  func(string) (string, error)
	format func(string) (string, error)
}

func (c *queryFuncCodec) Parse(text string) (string, error) {
	if c.parse != nil {
		return c.parse(text)
	}
	return text, nil
}
func (c *queryFuncCodec) Format(text string) (string, error) {
	if c.format != nil {
		return c.format(text)
	}
	return text, nil
}

type queryCustom struct{ Text string }

func customQuery(codec foundryhttp.QueryCodec[string]) foundryhttp.Query[queryCustom] {
	return foundryhttp.DefineQuery(foundryhttp.QueryParam("text", codec, func(q *queryCustom) *string { return &q.Text }))
}

func TestTypedQueryValidatesStructureBeforeCodecs(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	d := customQuery(&queryFuncCodec{parse: func(text string) (string, error) { calls.Add(1); return text, nil }})
	for _, raw := range []string{"", "text=a&text=b", "text=a&unknown=b", "text=%GG"} {
		if _, err := d.Decode(context.Background(), raw, queryLimits); err == nil {
			t.Fatal("invalid structure accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("codec invoked before structural rejection")
	}
}

func TestTypedQueryCodecFailuresAreOwned(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"panic", "goexit", "ordinary", "panic-and-cancel"} {
		for _, direction := range []string{"decode", "encode"} {
			t.Run(mode+"-"+direction, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				sentinel := errors.New("private codec detail")
				method := func(string) (string, error) {
					switch mode {
					case "panic":
						panic("private panic")
					case "goexit":
						runtime.Goexit()
					case "panic-and-cancel":
						cancel()
						panic("private panic")
					}
					return "", sentinel
				}
				d := customQuery(&queryFuncCodec{parse: method, format: method})
				if mode == "goexit" {
					// Codecs run on the caller's goroutine (callback.Invoke):
					// Goexit ends it instead of becoming a returned error.
					returned := make(chan bool, 1)
					go func() {
						completed := false
						defer func() { returned <- completed }()
						if direction == "decode" {
							_, _ = d.Decode(ctx, "text=x", queryLimits)
						} else {
							_, _ = d.Encode(ctx, queryCustom{Text: "x"}, queryLimits)
						}
						completed = true
					}()
					if <-returned {
						t.Fatal("query codec Goexit was converted into a return")
					}
					return
				}
				var err error
				if direction == "decode" {
					var got queryCustom
					got, err = d.Decode(ctx, "text=x", queryLimits)
					if got.Text != "" {
						t.Fatal("partial model")
					}
				} else {
					var got string
					got, err = d.Encode(ctx, queryCustom{Text: "x"}, queryLimits)
					if got != "" {
						t.Fatal("partial output")
					}
				}
				if mode == "ordinary" {
					if !errors.Is(err, sentinel) || !errors.Is(err, fault.Invalid) {
						t.Fatalf("ordinary cause lost: %v", err)
					}
				} else if !errors.Is(err, fault.Internal) {
					t.Fatalf("callback failure not internal: %v", err)
				}
				if strings.Contains(fmt.Sprintf("%v %+v %#v", err, err, err), "private") {
					t.Fatal("private cause leaked")
				}
			})
		}
	}
}

func TestTypedQueryCancellationWaitsForCodecOwnership(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	d := customQuery(&queryFuncCodec{parse: func(string) (string, error) {
		close(entered)
		<-release
		return "finished", nil
	}})
	done := make(chan error, 1)
	go func() { _, err := d.Decode(ctx, "text=x", queryLimits); done <- err }()
	<-entered
	cancel()
	select {
	case <-done:
		t.Fatal("codec abandoned while it owned work")
	default:
	}
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestTypedQueryDeclarationAndSelectorFailures(t *testing.T) {
	t.Parallel()
	var nilCodec *queryFuncCodec
	var zero foundryhttp.Query[queryCustom]
	if zero.Validate() == nil {
		t.Fatal("undefined query accepted")
	}
	for _, d := range []foundryhttp.Query[queryCustom]{
		customQuery(nilCodec),
		foundryhttp.DefineQuery(foundryhttp.QueryParam[queryCustom]("text", foundryhttp.StringQuery[string](), nil)),
		foundryhttp.DefineQuery(foundryhttp.QueryParameter[queryCustom]{}),
		foundryhttp.DefineQuery(foundryhttp.QueryParam("bad name", foundryhttp.StringQuery[string](), func(q *queryCustom) *string { return &q.Text })),
	} {
		if d.Validate() == nil {
			t.Fatal("invalid declaration accepted")
		}
	}
	parameter := foundryhttp.QueryParam("text", foundryhttp.StringQuery[string](), func(q *queryCustom) *string { return &q.Text })
	if foundryhttp.DefineQuery(parameter, parameter).Validate() == nil {
		t.Fatal("duplicate declaration accepted")
	}
	d := foundryhttp.DefineQuery(foundryhttp.QueryParam("text", foundryhttp.StringQuery[string](), func(*queryCustom) *string { return nil }))
	if _, err := d.Decode(context.Background(), "text=x", queryLimits); !errors.Is(err, fault.Internal) {
		t.Fatalf("nil decode destination: %v", err)
	}
	if _, err := d.Encode(context.Background(), queryCustom{}, queryLimits); !errors.Is(err, fault.Internal) {
		t.Fatalf("nil encode source: %v", err)
	}
	parameters, err := searchQuery().Parameters()
	if err != nil || parameters[0].Name != "active" {
		t.Fatal("parameter metadata is not deterministic")
	}
	parameters[0].Name = "changed"
	parameters, _ = searchQuery().Parameters()
	if parameters[0].Name != "active" {
		t.Fatal("metadata shares mutable storage")
	}
}
