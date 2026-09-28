package toml_test

import (
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/config/toml"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/secret"
)

type settings struct {
	Name     string
	Port     int
	Enabled  bool
	Timeout  time.Duration
	Password secret.String
	Hosts    []string
	Labels   map[string][]string
	Backends []backend
	Exact    []int64
}

type backend struct {
	Name   string `json:"name"`
	Weight int    `json:"weight"`
}

var (
	name  = config.String("app.name", func(s *settings) *string { return &s.Name })
	port  = config.Int("http.port", func(s *settings) *int { return &s.Port })
	hosts = config.JSON("http.hosts", func(s *settings) *[]string { return &s.Hosts })
)

func schema(t testing.TB) *config.Schema[settings] {
	t.Helper()
	schema, err := config.New(name, port, hosts,
		config.Bool("app.enabled", func(s *settings) *bool { return &s.Enabled }),
		config.Duration("http.timeout", func(s *settings) *time.Duration { return &s.Timeout }),
		config.Secret("database.password", func(s *settings) *secret.String { return &s.Password }),
		config.JSON("app.labels", func(s *settings) *map[string][]string { return &s.Labels }),
		config.JSON("http.backends", func(s *settings) *[]backend { return &s.Backends }),
		config.JSON("app.exact", func(s *settings) *[]int64 { return &s.Exact }),
	)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func TestDecodeScalarCollectionsAndSourcePrecedence(t *testing.T) {
	s := schema(t)
	base, err := toml.Decode(strings.NewReader(`
app.name = "base"
app.enabled = true
app.exact = [9223372036854775807, -9223372036854775808]
app.labels = { "literal.dot" = ["one", "two"] }
[http]
port = 0x1f90
timeout = "3s"
hosts = ["a.example", "b.example"]
[[http.backends]]
name = "first"
weight = 2
[[http.backends]]
name = "second"
weight = 3
[database]
password = "private-credential"
`), s, toml.Options{Name: "base.toml"})
	if err != nil {
		t.Fatal(err)
	}
	local, err := toml.Decode(strings.NewReader(`app.name = "local"`), s, toml.Options{Name: "local.toml"})
	if err != nil {
		t.Fatal(err)
	}
	loaded, report, err := s.Load(settings{Name: "default"}, config.Inputs[settings]{
		Files: []config.Values{base, local}, Prefix: "APP",
		Environment: func(key string) (string, bool) {
			value, ok := map[string]string{"APP__APP__NAME": "environment", "APP__HTTP__HOSTS": `["env.example"]`}[key]
			return value, ok
		},
		Overrides: []config.Override[settings]{port.Set(9000)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != "environment" || loaded.Port != 9000 || !loaded.Enabled || loaded.Timeout != 3*time.Second || loaded.Password.Reveal() != "private-credential" ||
		!reflect.DeepEqual(loaded.Hosts, []string{"env.example"}) || !reflect.DeepEqual(loaded.Labels, map[string][]string{"literal.dot": {"one", "two"}}) ||
		!reflect.DeepEqual(loaded.Backends, []backend{{"first", 2}, {"second", 3}}) || !reflect.DeepEqual(loaded.Exact, []int64{math.MaxInt64, math.MinInt64}) {
		t.Fatal("TOML values or precedence were lost")
	}
	sources := make(map[string]config.Entry)
	for _, entry := range report.Entries() {
		sources[entry.Name] = entry
	}
	if sources["app.name"].Source != "environment:APP__APP__NAME" || sources["http.port"].Source != "override" || sources["database.password"] != (config.Entry{Name: "database.password", Source: "base.toml", Secret: true}) {
		t.Fatalf("incorrect provenance: %+v", report.Entries())
	}
	withoutEnv, _, err := s.Load(settings{Name: "default"}, config.Inputs[settings]{Files: []config.Values{base, local}})
	if err != nil || withoutEnv.Name != "local" || withoutEnv.Port != 8080 {
		t.Fatal("file order or scalar decoding lost")
	}
}

func TestTOMLTemporalTextHasNoImplicitTimezone(t *testing.T) {
	type dates struct {
		Offset, Local, Date, Time string
		Collection                []string
	}
	s, err := config.New(
		config.String("offset", func(s *dates) *string { return &s.Offset }),
		config.String("local", func(s *dates) *string { return &s.Local }),
		config.String("date", func(s *dates) *string { return &s.Date }),
		config.String("time", func(s *dates) *string { return &s.Time }),
		config.JSON("collection", func(s *dates) *[]string { return &s.Collection }),
	)
	if err != nil {
		t.Fatal(err)
	}
	file, err := toml.Decode(strings.NewReader(`
offset = 2026-09-11T12:34:56.123+08:00
local = 2026-09-11T12:34:56.123
date = 2026-09-11
time = 12:34:56.123
collection = [2026-09-11, 12:34:56.123]
`), s, toml.Options{Name: "dates"})
	if err != nil {
		t.Fatal(err)
	}
	loaded, _, err := s.Load(dates{}, config.Inputs[dates]{Files: []config.Values{file}})
	if err != nil {
		t.Fatal(err)
	}
	want := dates{"2026-09-11T12:34:56.123+08:00", "2026-09-11T12:34:56.123", "2026-09-11", "12:34:56.123", []string{"2026-09-11", "12:34:56.123"}}
	if !reflect.DeepEqual(loaded, want) {
		t.Fatalf("temporal text: %#v", loaded)
	}
}

func TestInvalidDocumentsReturnNoPartialLayerOrSecrets(t *testing.T) {
	s := schema(t)
	for label, document := range map[string]string{
		"syntax":              `app.name = "private-credential`,
		"unknown":             `app.name = "good"` + "\n" + `app.private_credential = "private-credential"`,
		"empty unknown table": `[private_credential]`,
		"quoted path":         `"app.name" = "private-credential"`,
		"scalar namespace":    `app = "private-credential"`,
		"duplicate":           "app.name = 'one'\napp.name = 'private-credential'",
		"nan":                 `http.port = nan`,
		"nested infinity":     `app.labels = { secret = [inf] }`,
		"depth":               "app.labels = " + strings.Repeat("[", 66) + `"private-credential"` + strings.Repeat("]", 66),
	} {
		t.Run(label, func(t *testing.T) {
			file, err := toml.Decode(strings.NewReader(document), s, toml.Options{Name: "test"})
			assertSafeFailure(t, file, err)
		})
	}
	for label, document := range map[string]string{
		"scalar type":          `http.port = "private-credential"`,
		"collection member":    `http.hosts = [1]`,
		"unknown struct field": `http.backends = [{ name = "one", typo = "private-credential" }]`,
	} {
		t.Run(label, func(t *testing.T) {
			file, err := toml.Decode(strings.NewReader(document), s, toml.Options{Name: "test"})
			if err != nil {
				t.Fatal(err)
			}
			loaded, report, err := s.Load(settings{Name: "default"}, config.Inputs[settings]{Files: []config.Values{file}})
			if !errors.Is(err, fault.Invalid) || !reflect.DeepEqual(loaded, settings{}) || len(report.Entries()) != 0 {
				t.Fatal("invalid setting returned partial data")
			}
			assertSafeFailure(t, config.Values{}, err)
		})
	}
}

func assertSafeFailure(t *testing.T, file config.Values, err error) {
	t.Helper()
	if !errors.Is(err, fault.Invalid) || file.Name != "" || file.Data != nil {
		t.Fatalf("invalid result: %+v %v", file, err)
	}
	for _, format := range []string{"%v", "%+v", "%#v"} {
		if strings.Contains(fmt.Sprintf(format, err), "private-credential") {
			t.Fatal("error leaked source value")
		}
	}
}

func TestBoundedReadsAndInputFailures(t *testing.T) {
	s := schema(t)
	input := `app.name = "ok"`
	for _, limit := range []int64{int64(len(input)), int64(len(input)) + 1} {
		if _, err := toml.Decode(strings.NewReader(input), s, toml.Options{Name: "test", MaxBytes: limit}); err != nil {
			t.Fatal(err)
		}
	}
	reader := &countingReader{Reader: strings.NewReader(input + strings.Repeat(" ", 100))}
	file, err := toml.Decode(reader, s, toml.Options{Name: "test", MaxBytes: int64(len(input)) - 1})
	assertSafeFailure(t, file, err)
	if reader.read != len(input) {
		t.Fatalf("read beyond limit plus one: %d", reader.read)
	}
	for _, limit := range []int64{-1, math.MaxInt64} {
		file, err := toml.Decode(strings.NewReader(input), s, toml.Options{Name: "test", MaxBytes: limit})
		assertSafeFailure(t, file, err)
	}
	file, err = toml.Decode(strings.NewReader(strings.Repeat(" ", int(toml.DefaultMaxBytes)+1)), s, toml.Options{Name: "test"})
	assertSafeFailure(t, file, err)
	cause := errors.New("private-credential")
	file, err = toml.Decode(failingReader{cause}, s, toml.Options{Name: "test"})
	assertSafeFailure(t, file, err)
	if !errors.Is(err, cause) {
		t.Fatal("reader cause lost")
	}
	file, err = toml.Decode[settings](strings.NewReader(input), nil, toml.Options{Name: "test"})
	assertSafeFailure(t, file, err)
	file, err = toml.Decode[settings](nil, s, toml.Options{Name: "test"})
	assertSafeFailure(t, file, err)
	file, err = toml.Decode(strings.NewReader(input), s, toml.Options{})
	assertSafeFailure(t, file, err)
}

type countingReader struct {
	io.Reader
	read int
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.read += n
	return n, err
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func FuzzDecodeBoundedDocument(f *testing.F) {
	s := schema(f)
	for _, seed := range []string{"", "[app]", `app.name = "valid"`, `http.backends = [{name = "one", weight = 2}]`, `app.labels = {one = [nan]}`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, document string) {
		file, err := toml.Decode(strings.NewReader(document), s, toml.Options{Name: "fuzz", MaxBytes: 4096})
		if err != nil {
			if file.Name != "" || file.Data != nil {
				t.Fatal("failure returned partial layer")
			}
			return
		}
		loaded, report, err := s.Load(settings{}, config.Inputs[settings]{Files: []config.Values{file}})
		if err != nil && (!reflect.DeepEqual(loaded, settings{}) || len(report.Entries()) != 0) {
			t.Fatal("failure returned partial settings")
		}
	})
}
