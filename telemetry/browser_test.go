package telemetry

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestBrowserValidationIsAtomic(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*BrowserError)
	}{
		{"unknown portal", func(r *BrowserError) { r.Portal = "unknown" }},
		{"unknown release", func(r *BrowserError) { r.Release = "unknown" }},
		{"unknown class", func(r *BrowserError) { r.Code = "Exception: private-message" }},
		{"too many frames", func(r *BrowserError) { r.Frames = make([]BrowserFrame, 21) }},
		{"wrong portal", func(r *BrowserError) { r.Frames[0].File = "user/_assets/app.js" }},
		{"url", func(r *BrowserError) { r.Frames[0].File = "https://private/admin/_assets/app.js" }},
		{"query", func(r *BrowserError) { r.Frames[0].File = "admin/_assets/app.js?token=secret" }},
		{"traversal", func(r *BrowserError) { r.Frames[0].File = "admin/_assets/../app.js" }},
		{"normalized traversal", func(r *BrowserError) { r.Frames[0].File = "admin/ignored/../_assets/app.js" }},
		{"encoded traversal", func(r *BrowserError) { r.Frames[0].File = "admin/_assets/%2e%2e.js" }},
		{"backslash", func(r *BrowserError) { r.Frames[0].File = "admin/_assets/private\\app.js" }},
		{"wrong extension", func(r *BrowserError) { r.Frames[0].File = "admin/_assets/app.map" }},
		{"line zero", func(r *BrowserError) { r.Frames[0].Line = 0 }},
		{"line high", func(r *BrowserError) { r.Frames[0].Line = 1000001 }},
		{"column negative", func(r *BrowserError) { r.Frames[0].Column = -1 }},
		{"column high", func(r *BrowserError) { r.Frames[0].Column = 1000001 }},
		{"invalid trailing frame", func(r *BrowserError) { r.Frames = append(r.Frames, BrowserFrame{File: "invalid"}) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _ := newCollector(t, collectorConfig())
			r := browserReport()
			test.edit(&r)
			if c.RecordBrowser(t.Context(), r) == nil {
				t.Fatal("invalid browser report accepted")
			}
			if len(c.issues) != 0 {
				t.Fatal("partial browser report admitted")
			}
		})
	}
	cfg := collectorConfig()
	cfg.Browser.Enabled = false
	c, _ := newCollector(t, cfg)
	if c.RecordBrowser(t.Context(), browserReport()) == nil {
		t.Fatal("disabled intake admitted")
	}
}

func TestBrowserAllowedClassesAndFrameBounds(t *testing.T) {
	for _, code := range []BrowserCode{BrowserCodeError, BrowserCodeTypeError, BrowserCodeRangeError, BrowserCodeReferenceError, BrowserCodeSyntaxError, BrowserCodeURIError, BrowserCodeEvalError, BrowserCodeUnhandledRejection, BrowserCodeReactBoundary} {
		t.Run(string(code), func(t *testing.T) {
			c, _ := newCollector(t, collectorConfig())
			r := browserReport()
			r.Code = code
			r.Frames = make([]BrowserFrame, 20)
			for i := range r.Frames {
				r.Frames[i] = BrowserFrame{File: "admin/_assets/chunk-ABC_123.js", Line: 1000000, Column: 1000000}
			}
			if err := c.RecordBrowser(t.Context(), r); err != nil {
				t.Fatal(err)
			}
			b := capture(t, c)
			issue := b.issueSnapshot()[0]
			if !issue.Detail.Untrusted || len(issue.Detail.Frames) != 20 || issue.Detail.Frames[0].Function != "" {
				t.Fatal("browser detail contract changed")
			}
		})
	}
}

func TestEngineRecordBrowserRejectsUninitializedEngine(t *testing.T) {
	for _, engine := range []*Engine{nil, {}} {
		if err := engine.RecordBrowser(t.Context(), browserReport()); !errors.Is(err, fault.Invalid) {
			t.Fatalf("uninitialized browser admission: %v", err)
		}
	}
}
