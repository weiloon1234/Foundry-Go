package http

import (
	"net/netip"
	"strings"
	"testing"
)

func forwardedAddresses(values []string) []netip.Addr {
	var addresses []netip.Addr
	for _, hop := range forwardedHops(values) {
		if !hop.valid {
			addresses = append(addresses, netip.IPv4Unspecified())
			continue
		}
		addresses = append(addresses, hop.address)
	}
	return addresses
}

func TestForwardedAddressGrammar(t *testing.T) {
	// Results are nearest-first; 0.0.0.0 marks a malformed element and the
	// empty string a valid unknown/obfuscated node. Both stop a trusted walk.
	for _, test := range []struct {
		input string
		want  []string
	}{
		{`for=192.0.2.1;proto=https;host="example.test:443"`, []string{"192.0.2.1"}},
		{`FoR="[2001:db8::1]:4711";by=_proxy`, []string{"2001:db8::1"}},
		{`for="192.0.2.1:1234",for="[::ffff:198.51.100.2]"`, []string{"198.51.100.2", "192.0.2.1"}},
		{`for="192.0.2.1:_port"`, []string{"192.0.2.1"}},
		{`for=unknown,for=_hidden,for="unknown:1234",for="_hidden:_port"`, []string{"", "", "", ""}},
		// A quoted comma no longer hides an element boundary: the halves are
		// malformed, so the walk stops there (no standard value has commas).
		{`for=192.0.2.1;ext="comma,semicolon;escaped\"quote"`, []string{"0.0.0.0", "0.0.0.0"}},
		{`for=192.0.2.1;;proto=https,;by=_private;`, []string{"", "192.0.2.1"}},
		{`for=192.0.2.1,,for=10.0.0.1`, []string{"10.0.0.1", "192.0.2.1"}},
		{`for="2001:db8::1"`, []string{"2001:db8::1"}},
		{`for=192.0.2.1;FOR=192.0.2.2, for=10.0.0.1`, []string{"10.0.0.1", "0.0.0.0"}},
		// A client's unterminated quote cannot swallow the hop a trusted proxy
		// appended to the same line.
		{`for="192.0.2.1, for=10.0.0.1`, []string{"10.0.0.1", "0.0.0.0"}},
	} {
		got := forwardedAddresses([]string{test.input})
		if len(got) != len(test.want) {
			t.Fatalf("Forwarded %q: %v", test.input, got)
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
		`for=`, `for=192.0.2.1;x=a;X=b`, `for="[192.0.2.1]"`,
		`for="[2001:db8::1]:65536"`, `for="[2001:db8::1]:"`, `for="192.0.2.1:abc"`,
		`for="[fe80::1%eth0]"`, `for=host.example`, `for=_`, `for="_private:!"`,
		`for="192.0.2.1`, `for=192.0.2.1 "extra"`, "for=192.0.2.1\r\nInjected=yes", "for=192.0.2.1;x=\"a\x00b\"",
	} {
		hops := forwardedHops([]string{input})
		if len(hops) != 1 || hops[0].valid {
			t.Errorf("accepted malformed Forwarded %q as usable: %+v", input, hops)
		}
	}
	// Separate lines are tokenized independently: an unterminated client quote
	// cannot swallow the element a trusted proxy appended on a later line.
	if got := forwardedAddresses([]string{`for="198.51.100.1`, `for=203.0.113.9`}); len(got) != 2 || got[0] != netip.MustParseAddr("203.0.113.9") {
		t.Fatalf("line isolation failed: %v", got)
	}
	if got := forwardedHops([]string{strings.Repeat("for=192.0.2.1,", maxProxyHops) + "for=192.0.2.9"}); len(got) != maxProxyHops || got[0].address != netip.MustParseAddr("192.0.2.9") {
		t.Fatalf("Forwarded hop bound did not keep the nearest hops: %d", len(got))
	}
}

func TestProxyHopAddressesAreLenient(t *testing.T) {
	for input, want := range map[string]string{
		"192.0.2.1": "192.0.2.1", " 192.0.2.1\t": "192.0.2.1", "192.0.2.1:1234": "192.0.2.1",
		"2001:db8::1": "2001:db8::1", "[2001:db8::1]": "2001:db8::1", "[2001:db8::1]:443": "2001:db8::1",
		"::ffff:198.51.100.2": "198.51.100.2",
	} {
		if got := proxyHopAddress(input); got != netip.MustParseAddr(want) {
			t.Errorf("hop %q = %v; want %s", input, got, want)
		}
	}
	for _, input := range []string{"", "unknown", "_hidden", "garbage", "fe80::1%eth0", "[192.0.2.1]", "192.0.2.1:99999", strings.Repeat("1", maxProxyEntryBytes+1)} {
		if proxyHopAddress(input).IsValid() {
			t.Errorf("unusable hop %q accepted", input)
		}
	}
	var visited []string
	reverseProxyEntries([]string{"a, b,,", " c ,d"}, func(entry string) bool { visited = append(visited, entry); return true })
	if strings.Join(visited, "|") != "d|c|b|a" {
		t.Fatalf("reverse list order = %v", visited)
	}
	visited = nil
	reverseProxyEntries([]string{strings.Repeat("x,", 2*maxProxyHops)}, func(entry string) bool { visited = append(visited, entry); return true })
	if len(visited) != maxProxyHops {
		t.Fatalf("list walk exceeded its hop bound: %d", len(visited))
	}
}

func FuzzProxyAddresses(f *testing.F) {
	for _, input := range []string{"for=192.0.2.1", `for="[2001:db8::1]:1234"`, "for=unknown,for=10.0.0.1", `for=192.0.2.1;ext="a,b"`, "192.0.2.1,10.0.0.1", "", "\r\n"} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input string) {
		hops := forwardedHops([]string{input})
		if len(hops) > maxProxyHops {
			t.Fatal("Forwarded hops exceed their bound")
		}
		for _, hop := range hops {
			if hop.address.IsValid() && (!hop.valid || hop.address.Zone() != "" || hop.address.Is4In6()) {
				t.Fatal("address was not normalized")
			}
		}
		count := 0
		reverseProxyEntries([]string{input}, func(entry string) bool {
			count++
			if ip := proxyHopAddress(entry); ip.IsValid() && (ip.Zone() != "" || ip.Is4In6()) {
				t.Fatal("address was not normalized")
			}
			return true
		})
		if count > maxProxyHops {
			t.Fatal("list walk exceeded its bound")
		}
	})
}
