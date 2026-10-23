package http_test

import (
	"bytes"
	"log/slog"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func TestStatementDetectorMiddleware(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		var logs bytes.Buffer
		d, _ := database.NewStatementDetector(database.StatementDetectorConfig{Threshold: 1, MaxShapes: 1})
		handler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
			for range 2 {
				d.Observe(r.Context(), database.QueryEvent{Statement: "select 'canary' /* secret */"})
			}
			for range 3 {
				d.Observe(r.Context(), database.QueryEvent{Statement: "select id from users"})
			}
			w.WriteHeader(204)
		})
		var middlewares []foundryhttp.Middleware
		if enabled {
			middlewares = append(middlewares, foundryhttp.StatementDetector(d, slog.New(slog.NewJSONHandler(&logs, nil))))
		}
		wrapped, err := foundryhttp.ApplyMiddleware(handler, middlewares...)
		if err != nil {
			t.Fatal(err)
		}
		for range 2 {
			response := httptest.NewRecorder()
			wrapped.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
			if response.Code != 204 {
				t.Fatal(response.Code)
			}
		}
		got := logs.String()
		if strings.Contains(got, "canary") || strings.Contains(got, "secret") {
			t.Fatal("logs leak", got)
		}
		if enabled {
			if strings.Count(got, "repeated database statement shape") != 2 || strings.Count(got, "observation incomplete") != 2 || !strings.Contains(got, `"count":2`) {
				t.Fatal(got)
			}
		} else if got != "" {
			t.Fatal(got)
		}
	}
}

func TestStatementSettingValuesRedacted(t *testing.T) {
	for _, value := range []string{"canary_token", `"canary_token"`, "'canary_token'"} {
		for _, sql := range []string{"SET app.token TO " + value, "SELECT set_config('token'," + value + ",false)", "SELECT id,current_setting(" + value + ") FROM records", "SELECT id,set_config('token'," + value + ",false) FROM records"} {
			d, _ := database.NewStatementDetector(database.StatementDetectorConfig{Threshold: 1, MaxShapes: 8})
			var logs bytes.Buffer
			handler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
				for range 2 {
					d.Observe(r.Context(), database.QueryEvent{Statement: sql})
				}
				w.WriteHeader(204)
			})
			wrapped, err := foundryhttp.ApplyMiddleware(handler, foundryhttp.StatementDetector(d, slog.New(slog.NewJSONHandler(&logs, nil))))
			if err != nil {
				t.Fatal(err)
			}
			wrapped.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
			got := logs.String()
			if !strings.Contains(got, "repeated database statement shape") || strings.Contains(got, "canary") {
				t.Fatal(got)
			}
		}
	}
}
