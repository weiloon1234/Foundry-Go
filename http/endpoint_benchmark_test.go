package http

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
)

type BenchmarkListItem struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Count  int64  `json:"count"`
	Active bool   `json:"active"`
}

type BenchmarkListReply struct {
	Items []BenchmarkListItem `json:"items"`
	Total int64               `json:"total"`
}

type BenchmarkCreateBody struct {
	Name  string   `json:"name"`
	Tags  []string `json:"tags"`
	Count int64    `json:"count"`
}

func benchmarkRoot[T any]() contract.TypeID {
	typ := reflect.TypeFor[T]()
	return contract.TypeID(typ.PkgPath() + "." + typ.Name())
}

func benchmarkReplyJSON() contract.JSON[BenchmarkListReply] {
	root := benchmarkRoot[BenchmarkListReply]()
	return contract.DefineJSON[BenchmarkListReply](contract.Schema{Root: root, Types: []contract.Type{
		{ID: root, Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "items", Type: "items", Required: true}, {Name: "total", Type: "int64", Required: true}}},
		{ID: "items", Kind: contract.ArrayKind, Element: "item", Nullable: true},
		{ID: "item", Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "id", Type: "text", Required: true}, {Name: "name", Type: "text", Required: true}, {Name: "count", Type: "int64", Required: true}, {Name: "active", Type: "bool", Required: true}}},
		{ID: "text", Kind: contract.StringKind},
		{ID: "int64", Kind: contract.IntegerKind, Bits: 64, Signed: true},
		{ID: "bool", Kind: contract.BooleanKind},
	}})
}

func benchmarkBodyJSON() contract.JSON[BenchmarkCreateBody] {
	root := benchmarkRoot[BenchmarkCreateBody]()
	return contract.DefineJSON[BenchmarkCreateBody](contract.Schema{Root: root, Types: []contract.Type{
		{ID: root, Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "name", Type: "text", Required: true}, {Name: "tags", Type: "tags", Required: true}, {Name: "count", Type: "int64", Required: true}}},
		{ID: "tags", Kind: contract.ArrayKind, Element: "text", Nullable: true},
		{ID: "text", Kind: contract.StringKind},
		{ID: "int64", Kind: contract.IntegerKind, Bits: 64, Signed: true},
	}})
}

// BenchmarkTypedJSONEndpoint measures one typed POST with a small JSON body and
// a 100-item JSON list response through the router, excluding network I/O.
func BenchmarkTypedJSONEndpoint(b *testing.B) {
	items := make([]BenchmarkListItem, 100)
	for i := range items {
		items[i] = BenchmarkListItem{ID: "item-" + strconv.Itoa(i), Name: "Example item " + strconv.Itoa(i), Count: int64(i * 17), Active: i%2 == 0}
	}
	route := DefineRoute(RouteSpec{ID: "bench.items", Method: POST, Access: Public}, StaticPath("/items"))
	endpoint := DefineEndpoint(route, EmptyQuery(), JSONBody(benchmarkBodyJSON()), JSONResponse(200, benchmarkReplyJSON()))
	router, err := NewRouter(endpoint.Handle(func(_ context.Context, in Input[NoPath, NoQuery, BenchmarkCreateBody]) (BenchmarkListReply, error) {
		return BenchmarkListReply{Items: items, Total: in.Body.Count}, nil
	}))
	if err != nil {
		b.Fatal(err)
	}
	body := []byte(`{"name":"widget","tags":["a","b","c"],"count":100}`)
	kernel := newHandlerLifetime().wrap(router, slog.New(slog.NewTextHandler(io.Discard, nil)), DefaultServerConfig())
	for _, target := range []struct {
		name    string
		handler stdhttp.Handler
	}{{"Router", router}, {"Kernel", kernel}} {
		b.Run(target.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				request := httptest.NewRequest("POST", "/items", bytes.NewReader(body))
				request.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				target.handler.ServeHTTP(response, request)
				if response.Code != 200 {
					b.Fatal(response.Code, response.Body.String())
				}
			}
		})
	}
}
