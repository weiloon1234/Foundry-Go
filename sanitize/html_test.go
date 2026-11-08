package sanitize_test

import (
	"context"
	"errors"
	"io"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
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

func TestSanitizerURLOptions(t *testing.T) {
	for _, options := range []struct {
		name     string
		tel      bool
		relative bool
	}{
		{name: "default"},
		{name: "tel", tel: true},
		{name: "relative", relative: true},
		{name: "tel and relative", tel: true, relative: true},
	} {
		t.Run(options.name, func(t *testing.T) {
			config := sanitize.DefaultConfig()
			config.AllowedTags = append(config.AllowedTags, "img")
			config.AllowRelativeURLs = options.relative
			if options.tel {
				config.AdditionalURLSchemes = []sanitize.URLScheme{sanitize.URLSchemeTel}
			}
			policy, err := sanitize.New(config)
			if err != nil {
				t.Fatal(err)
			}
			for _, link := range []struct {
				input string
				want  string
				keep  bool
			}{
				{"https://example.com", "https://example.com", true},
				{"http://example.com", "http://example.com", true},
				{"mailto:help@example.com", "mailto:help@example.com", true},
				{"HtTpS://example.com", "https://example.com", true},
				{"tel:+60123456789", "tel:+60123456789", options.tel},
				{"TeL:+60123456789", "tel:+60123456789", options.tel},
				{"/me/support", "/me/support", options.relative},
				{"support", "support", options.relative},
				{"./support", "./support", options.relative},
				{"../support", "../support", options.relative},
				{"/", "/", options.relative},
				{"?page=2", "?page=2", options.relative},
				{"#support", "#support", options.relative},
				{" \t/me/support\n", "/me/support", options.relative},
				{"//evil.example", "", false},
				{"//EvIl.ExAmPlE", "", false},
				{"///evil.example", "", false},
				{" \t//evil.example\n", "", false},
				{"\v//evil.example", "", false},
				{"\f//evil.example", "", false},
				{"\u0085//evil.example", "", false},
				{"\u00a0//evil.example", "", false},
				{"\u2003//evil.example", "", false},
				{"&#47;&#47;evil.example", "", false},
				{`/\evil.example`, "", false},
				{`\\evil.example`, "", false},
				{"javascript:alert(1)", "", false},
				{"JaVaScRiPt:alert(1)", "", false},
				{"&#106;avascript:alert(1)", "", false},
				{"java&#x0a;script:alert(1)", "", false},
				{"data:text/html,<script>alert(1)</script>", "", false},
				{"DaTa:text/html,unsafe", "", false},
				{"vbscript:msgbox(1)", "", false},
				{"VbScRiPt:msgbox(1)", "", false},
				{"file:///etc/passwd", "", false},
				{"FiLe:///etc/passwd", "", false},
			} {
				t.Run(link.input, func(t *testing.T) {
					want := link.want
					if !link.keep {
						want = ""
					}
					assertSanitizedURL(t, policy, `<a href="`+link.input+`">link</a>`, "href", want)
					assertSanitizedURL(t, policy, `<img src="`+link.input+`" alt="image">`, "src", want)
				})
			}
		})
	}
}

func assertSanitizedURL(t *testing.T, policy *sanitize.Policy, input, attribute, want string) {
	t.Helper()
	output, err := policy.HTML(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	tokens := html.NewTokenizer(strings.NewReader(output))
	var got, rel string
	for {
		kind := tokens.Next()
		if kind == html.ErrorToken {
			if err := tokens.Err(); err != io.EOF {
				t.Fatal(err)
			}
			break
		}
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		for _, attr := range tokens.Token().Attr {
			if attr.Key == attribute {
				if want == "" {
					t.Fatalf("forbidden %s survived: %s", attribute, output)
				}
				got = attr.Val
			}
			if attr.Key == "rel" {
				rel = attr.Val
			}
		}
	}
	if got != want {
		t.Fatalf("%s = %q, want %q; output: %s", attribute, got, want, output)
	}
	if attribute == "href" && want != "" {
		var nofollow, noreferrer bool
		for _, value := range strings.Fields(rel) {
			nofollow = nofollow || value == "nofollow"
			noreferrer = noreferrer || value == "noreferrer"
		}
		if !nofollow || !noreferrer {
			t.Fatalf("link protections missing: %s", output)
		}
	}
}

func TestSanitizerRejectsInvalidAdditionalURLSchemes(t *testing.T) {
	for _, schemes := range [][]sanitize.URLScheme{
		{""}, {"https"}, {"http"}, {"mailto"}, {"ftp"}, {"unknown"},
		{"javascript"}, {"JaVaScRiPt"}, {"data"}, {"DaTa"},
		{"vbscript"}, {"VbScRiPt"}, {"file"}, {"FiLe"},
		{"TEL"}, {" tel "}, {sanitize.URLSchemeTel, sanitize.URLSchemeTel},
		{sanitize.URLSchemeTel, "javascript"},
	} {
		config := sanitize.DefaultConfig()
		config.AdditionalURLSchemes = schemes
		if policy, err := sanitize.New(config); policy != nil || !errors.Is(err, fault.Invalid) {
			t.Fatalf("schemes %q: policy = %v, error = %v", schemes, policy, err)
		}
	}
}

func TestSanitizerURLOptionsAreImmutable(t *testing.T) {
	config := sanitize.DefaultConfig()
	if len(config.AdditionalURLSchemes) != 0 || config.AllowRelativeURLs {
		t.Fatal("default URL options changed")
	}
	config.AdditionalURLSchemes = []sanitize.URLScheme{sanitize.URLSchemeTel}
	config.AllowRelativeURLs = true
	policy, err := sanitize.New(config)
	if err != nil {
		t.Fatal(err)
	}
	config.AdditionalURLSchemes[0] = "javascript"
	config.AllowRelativeURLs = false
	assertSanitizedURL(t, policy, `<a href="tel:+60123456789">call</a>`, "href", "tel:+60123456789")
	assertSanitizedURL(t, policy, `<a href="/me/support">support</a>`, "href", "/me/support")
	assertSanitizedURL(t, policy, `<a href="javascript:alert(1)">bad</a>`, "href", "")
	defaultPolicy, err := sanitize.New(sanitize.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	assertSanitizedURL(t, defaultPolicy, `<a href="tel:+60123456789">call</a>`, "href", "")
	assertSanitizedURL(t, defaultPolicy, `<a href="/me/support">support</a>`, "href", "")
}

func TestSanitizerCMSFragmentRetainsPassiveMarkup(t *testing.T) {
	config := sanitize.DefaultConfig()
	config.AllowedTags = []string{"p", "br", "h2", "h3", "strong", "b", "em", "i", "u", "s", "ul", "ol", "li", "blockquote", "pre", "code", "hr", "a"}
	config.AdditionalURLSchemes = []sanitize.URLScheme{sanitize.URLSchemeTel}
	config.AllowRelativeURLs = true
	policy, err := sanitize.New(config)
	if err != nil {
		t.Fatal(err)
	}
	input := `<h2 style="color:red" onclick="unsafe()">Support</h2><p data-private="secret"><u>Contact</u> <s>old</s></p>` +
		`<a href="/me/support" title="Support" rel="opener" onclick="unsafe()">Help</a><a href="tel:+60123456789">Call</a>` +
		`<script>unsafe()</script><style>unsafe</style><svg>unsafe</svg><iframe>unsafe</iframe>`
	output, err := policy.HTML(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<h2>Support</h2>`, `<p><u>Contact</u> <s>old</s></p>`, `title="Support"`} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %s: %s", want, output)
		}
	}
	for _, forbidden := range []string{"style=", "onclick", "data-private", "secret", "opener", "unsafe", "<script", "<style", "<svg", "<iframe"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("%s survived: %s", forbidden, output)
		}
	}
	assertSanitizedURL(t, policy, `<a href="/me/support">Help</a>`, "href", "/me/support")
	assertSanitizedURL(t, policy, `<a href="tel:+60123456789">Call</a>`, "href", "tel:+60123456789")
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
