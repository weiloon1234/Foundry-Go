package application

import (
	"io"
	"log/slog"
	"testing"
)

func TestStatementDetectorConfiguration(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := DefaultSettings()
	opts, middleware, err := prepareStatementDetector(s, logger)
	if err != nil || len(opts) != 0 || len(middleware) != 0 {
		t.Fatal("disabled installed work", err)
	}
	s.HTTP.StatementDetector.Enabled = true
	opts, middleware, err = prepareStatementDetector(s, logger)
	if err != nil || len(opts) != 1 || len(middleware) != 1 {
		t.Fatal("development detector absent", err)
	}
	s.Services.Namespace.Environment = "production"
	if _, _, err = prepareStatementDetector(s, logger); err == nil {
		t.Fatal("production enabled without explicit permission")
	}
	s.HTTP.StatementDetector.AllowNonDevelopment = true
	if _, _, err = prepareStatementDetector(s, logger); err != nil {
		t.Fatal(err)
	}
	s.HTTP.StatementDetector.Limits.MaxShapes = 0
	if _, _, err = prepareStatementDetector(s, logger); err == nil {
		t.Fatal("invalid limits accepted")
	}
	s.HTTP.StatementDetector.Limits.MaxShapes = 1
	s.HTTP.Enabled = false
	if _, _, err = prepareStatementDetector(s, logger); err == nil {
		t.Fatal("request detector without HTTP")
	}
}
