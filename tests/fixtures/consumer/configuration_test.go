package consumer_test

import (
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/config/toml"
)

// The application's settings remain its own ordinary Go type. These declarations
// are shared by file, environment, and programmatic configuration boundaries.
type transportSettings struct {
	Listen         string
	Timeout        time.Duration
	AllowedOrigins []string
}

var (
	listen         = config.String("http.listen", func(s *transportSettings) *string { return &s.Listen })
	requestTimeout = config.Duration("http.timeout", func(s *transportSettings) *time.Duration { return &s.Timeout })
	allowedOrigins = config.JSON("http.allowed_origins", func(s *transportSettings) *[]string { return &s.AllowedOrigins })
)

func TestConsumerLoadsTOMLWithTypedOverrides(t *testing.T) {
	schema, err := config.New(listen, requestTimeout, allowedOrigins)
	if err != nil {
		t.Fatal(err)
	}
	file, err := toml.Decode(strings.NewReader(`
[http]
listen = "127.0.0.1:8080"
timeout = "3s"
allowed_origins = ["https://example.test"]
`), schema, toml.Options{Name: "app.toml", MaxBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	settings, report, err := schema.Load(transportSettings{Timeout: time.Second}, config.Inputs[transportSettings]{
		Files:     []config.Values{file},
		Overrides: []config.Override[transportSettings]{requestTimeout.Set(5 * time.Second)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.Listen != "127.0.0.1:8080" || settings.Timeout != 5*time.Second || len(settings.AllowedOrigins) != 1 || settings.AllowedOrigins[0] != "https://example.test" {
		t.Fatal("incorrect settings")
	}
	if report.Entries()[0].Source != "app.toml" || report.Entries()[1].Source != "override" {
		t.Fatal("incorrect provenance")
	}
}
