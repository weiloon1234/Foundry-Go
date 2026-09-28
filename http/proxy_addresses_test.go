package http

import (
	"net/netip"
	"strings"
	"testing"
)

func TestForwardedAddressGrammar(t *testing.T) {
	for _, test := range []struct {
		input string
		want  []string
	}{
		{`for=192.0.2.1;proto=https;host="example.test:443"`, []string{"192.0.2.1"}},
		{`FoR="[2001:db8::1]:4711";by=_proxy`, []string{"2001:db8::1"}},
		{`for="192.0.2.1:1234",for="[::ffff:198.51.100.2]"`, []string{"192.0.2.1", "198.51.100.2"}},
		{`for="192.0.2.1:_port"`, []string{"192.0.2.1"}},
		{`for=unknown,for=_hidden,for="unknown:1234",for="_hidden:_port"`, []string{"", "", "", ""}},
		{`for=192.0.2.1;ext="comma,semicolon;escaped\"quote"`, []string{"192.0.2.1"}},
		{`for=192.0.2.1;;proto=https,;by=_private;`, []string{"192.0.2.1", ""}},
		{`for=192.0.2.1,,for=10.0.0.1`, []string{"192.0.2.1", "", "10.0.0.1"}},
	} {
		got, err := parseProxyAddresses(ForwardedHeader(), []string{test.input})
		if err != nil || len(got) != len(test.want) {
			t.Fatalf("Forwarded %q: %v, %v", test.input, got, err)
		}
		for i, raw := range test.want {
			var want netip.Addr
			if raw != "" {
				want = netip.MustParseAddr(raw)
			}
			if got[i] != want {
				t.Errorf("Forwarded %q[%d] = %v; want %v", test.input, i, got[i], want)
			}
		}
	}
	for _, input := range []string{
		``, `for=`, `for=192.0.2.1;FOR=192.0.2.2`, `for=192.0.2.1;x=a;X=b`,
		`for=2001:db8::1`, `for="2001:db8::1"`, `for="[192.0.2.1]"`,
		`for="[2001:db8::1]:65536"`, `for="[2001:db8::1]:"`, `for="192.0.2.1:abc"`,
		`for="[fe80::1%eth0]"`, `for=host.example`, `for=_`, `for="_private:!"`,
		`for="192.0.2.1`, `for=192.0.2.1 "extra"`, "for=192.0.2.1\r\nInjected=yes", "for=192.0.2.1;x=\"a\x00b\"",
	} {
		if _, err := parseProxyAddresses(ForwardedHeader(), []string{input}); err == nil {
			t.Errorf("accepted malformed Forwarded %q", input)
		}
	}
}

func TestProxyAddressBoundsAndSingleHeaderMultiplicity(t *testing.T) {
	for _, source := range []ProxyHeader{ForwardedHeader(), XForwardedForHeader(), ClientIPHeader("X-Real-IP")} {
		for _, values := range [][]string{nil, make([]string, maxProxyHops+1), {strings.Repeat("x", maxProxyHeaderBytes+1)}} {
			if _, err := parseProxyAddresses(source, values); err == nil {
				t.Errorf("accepted unbounded/absent header %v", source.Name())
			}
		}
	}
	for _, values := range [][]string{{"192.0.2.1", "192.0.2.1"}, {"192.0.2.1,192.0.2.2"}} {
		if _, err := parseProxyAddresses(ClientIPHeader("X-Real-IP"), values); err == nil {
			t.Error("single address header accepted a chain")
		}
	}
	for _, input := range []string{"", "192.0.2.1,", "192.0.2.1,,192.0.2.2", "192.0.2.1:1234", "fe80::1%eth0", strings.Repeat("192.0.2.1,", maxProxyHops) + "192.0.2.1"} {
		if _, err := parseProxyAddresses(XForwardedForHeader(), []string{input}); err == nil {
			t.Errorf("accepted malformed XFF %q", input)
		}
	}
	if _, err := parseProxyAddresses(ForwardedHeader(), []string{strings.Repeat("for=192.0.2.1,", maxProxyHops) + "for=192.0.2.1"}); err == nil {
		t.Error("unbounded Forwarded hop count")
	}
}

func FuzzProxyAddresses(f *testing.F) {
	for _, input := range []string{"for=192.0.2.1", `for="[2001:db8::1]:1234"`, "for=unknown,for=10.0.0.1", `for=192.0.2.1;ext="a,b"`, "192.0.2.1,10.0.0.1", "", "\r\n"} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input string) {
		for _, source := range []ProxyHeader{ForwardedHeader(), XForwardedForHeader(), ClientIPHeader("X-Real-IP")} {
			addresses, err := parseProxyAddresses(source, []string{input})
			if err != nil {
				continue
			}
			if len(addresses) == 0 || len(addresses) > maxProxyHops || len(input) > maxProxyHeaderBytes {
				t.Fatal("accepted input exceeds its bounds")
			}
			for _, ip := range addresses {
				if ip.IsValid() && (ip.Zone() != "" || ip.Is4In6()) {
					t.Fatal("address was not normalized")
				}
			}
		}
	})
}
