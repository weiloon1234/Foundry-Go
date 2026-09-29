package sanitize_test

import (
	"context"
	"errors"
	"io"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/sanitize"
	"golang.org/x/net/html"
)

func assertPassiveFragment(t *testing.T, text string) {
	t.Helper()
	tokens := html.NewTokenizer(strings.NewReader(text))
	for {
		kind := tokens.Next()
		if kind == html.ErrorToken {
			if err := tokens.Err(); err != io.EOF {
				t.Fatal(err)
			}
			return
		}
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		token := tokens.Token()
		switch token.Data {
		case "a", "b", "blockquote", "br", "code", "em", "i", "li", "ol", "p", "pre", "strong", "ul":
		default:
			t.Fatal("active or undeclared element survived", token.Data)
		}
		for _, attribute := range token.Attr {
			switch attribute.Key {
			case "title", "rel":
			case "href":
				u, err := url.Parse(attribute.Val)
				if err != nil || u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "mailto" {
					t.Fatal("unsafe URL survived")
				}
			default:
				t.Fatal("unsafe attribute survived", attribute.Key)
			}
		}
	}
}
func TestSanitizationAttackInputsAndImmutablePolicy(t *testing.T) {
	config := sanitize.DefaultConfig()
	p, err := sanitize.New(config)
	if err != nil {
		t.Fatal(err)
	}
	config.AllowedTags[0] = "script"
	for _, input := range []string{
		`<p>Hello</p><script>alert(1)</script><p>World</p>`,
		`<a href="javascript:alert(1)" onclick="steal()">link</a>`,
		`<a href="&#106;avascript:alert(1)">link</a>`,
		`<a href="java&#x0a;script:alert(1)">link</a>`,
		`<a href="data:text/html,<script>alert(1)</script>">link</a>`,
		`<svg><a xlink:href="javascript:alert(1)">x</a></svg>`,
		`<math><mtext><table><mglyph><style><!--</style><img title="--><img src=1 onerror=alert(1)>">`,
		`<scr<script>ipt>alert(1)</scr</script>ipt>`,
		`<p style="background:url(javascript:alert(1))" data-user="secret">text</p>`,
		`<iframe srcdoc="<script>alert(1)</script>"></iframe><template><p>hidden</p></template>`,
		`<form><input autofocus onfocus=alert(1)></form><p>safe</p>`,
	} {
		output, err := p.HTML(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		assertPassiveFragment(t, output)
	}
	output, err := p.HTML(t.Context(), `<p>Hello <b>world</b></p><a href="https://example.com">link</a>`)
	if err != nil || !strings.Contains(output, "<b>world</b>") || !strings.Contains(output, `href="https://example.com"`) || !strings.Contains(output, "nofollow") {
		t.Fatal(output, err)
	}
	stripped, err := sanitize.StripTags(t.Context(), `<b>bold</b><script>private()</script> text`)
	if err != nil || stripped != "bold text" {
		t.Fatal(stripped, err)
	}
	for _, tag := range []string{"script", "STYLE", "iframe", "svg", "math", "form", "input", "template", "custom-element"} {
		bad := sanitize.DefaultConfig()
		bad.AllowedTags = []string{tag}
		if _, err := sanitize.New(bad); err == nil {
			t.Fatal("active element explicitly allowed", tag)
		}
	}
	var wg sync.WaitGroup
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := p.HTML(t.Context(), `<b>safe</b>`)
			if err != nil || out != "<b>safe</b>" {
				t.Error(out, err)
			}
		}()
	}
	wg.Wait()
}
func TestStripTagsReusesOnePolicyConcurrently(t *testing.T) {
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := sanitize.StripTags(t.Context(), `<p>a &amp; <i>b</i></p><style>x</style>`)
			if err != nil || out != "a &amp; b" {
				t.Error(out, err)
			}
		}()
	}
	wg.Wait()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if out, err := sanitize.StripTags(ctx, "<b>x</b>"); !errors.Is(err, context.Canceled) || out != "" {
		t.Fatal(out, err)
	}
	if _, err := sanitize.StripTags(t.Context(), strings.Repeat("x", sanitize.MaxInputBytes+1)); err == nil {
		t.Fatal("default input bound ignored")
	}
}

func BenchmarkStripTags(b *testing.B) {
	for b.Loop() {
		if _, err := sanitize.StripTags(b.Context(), `<p>Hello <b>world</b></p>`); err != nil {
			b.Fatal(err)
		}
	}
}

func TestSanitizerBoundsAndCancellationNeverReturnPartialOutput(t *testing.T) {
	config := sanitize.DefaultConfig()
	config.InputBytes = 32
	config.OutputBytes = 5
	p, err := sanitize.New(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{strings.Repeat("x", 33), "&&", string([]byte{255}), "x\x00", "<p>hello</p>"} {
		if output, err := p.HTML(t.Context(), input); err == nil || output != "" {
			t.Fatal("bound returned successful or partial output", output, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if output, err := p.HTML(ctx, "x"); !errors.Is(err, context.Canceled) || output != "" {
		t.Fatal(output, err)
	}
	if _, err := sanitize.New(sanitize.Config{}); err == nil {
		t.Fatal("missing resource bounds")
	}
}
func FuzzSanitizerPassiveOutput(f *testing.F) {
	for _, seed := range []string{`<p>hello</p>`, `<a href="javascript:alert(1)">x</a>`, `<svg/onload=alert(1)>`, `<math><style><!--</style><img src=x onerror=alert(1)>`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 64<<10 {
			return
		}
		p, err := sanitize.New(sanitize.DefaultConfig())
		if err != nil {
			t.Fatal(err)
		}
		output, err := p.HTML(t.Context(), input)
		if err == nil {
			assertPassiveFragment(t, output)
		}
	})
}
