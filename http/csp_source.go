package http

import (
	"encoding/base64"
	"strings"

	"github.com/weiloon1234/Foundry-Go/fault"
)

// CSPSource is one source expression. Use a constructor; its zero value is
// invalid. Dynamic host and scheme inputs are validated during assembly.
type CSPSource struct {
	text string
	kind cspSourceKind
}

type cspSourceKind uint8

const (
	cspKeyword cspSourceKind = iota + 1
	cspHost
	cspScheme
	cspHash
	cspNonce
)

func CSPNone() CSPSource           { return CSPSource{"'none'", cspKeyword} }
func CSPSelf() CSPSource           { return CSPSource{"'self'", cspKeyword} }
func CSPAny() CSPSource            { return CSPSource{"*", cspHost} }
func CSPUnsafeInline() CSPSource   { return CSPSource{"'unsafe-inline'", cspKeyword} }
func CSPUnsafeEval() CSPSource     { return CSPSource{"'unsafe-eval'", cspKeyword} }
func CSPWasmUnsafeEval() CSPSource { return CSPSource{"'wasm-unsafe-eval'", cspKeyword} }
func CSPStrictDynamic() CSPSource  { return CSPSource{"'strict-dynamic'", cspKeyword} }
func CSPUnsafeHashes() CSPSource   { return CSPSource{"'unsafe-hashes'", cspKeyword} }
func CSPReportSample() CSPSource   { return CSPSource{"'report-sample'", cspKeyword} }

// CSPHostSource accepts a CSP host pattern, for example
// "https://*.example.test:443/assets/". It is not an arbitrary source-list string.
// Use ASCII/punycode hosts. CSP host grammar does not permit IPv6 literals.
func CSPHostSource(pattern string) CSPSource { return CSPSource{pattern, cspHost} }

// CSPSchemeSource takes a scheme without its colon, for example "https" or "data".
func CSPSchemeSource(scheme string) CSPSource { return CSPSource{scheme + ":", cspScheme} }

// CSPNonceSource asks the middleware to generate one fresh nonce per request.
// Read the matching value with CSPNonceFromContext when rendering trusted tags.
// Static configured nonces are deliberately not accepted by this API.
func CSPNonceSource() CSPSource { return CSPSource{"\x00", cspNonce} }

// CSPSHA256 accepts the digest of the exact allowed script/style bytes.
func CSPSHA256(digest [32]byte) CSPSource { return cspDigest("sha256", digest[:]) }
func CSPSHA384(digest [48]byte) CSPSource { return cspDigest("sha384", digest[:]) }
func CSPSHA512(digest [64]byte) CSPSource { return cspDigest("sha512", digest[:]) }
func cspDigest(name string, digest []byte) CSPSource {
	return CSPSource{"'" + name + "-" + base64.StdEncoding.EncodeToString(digest) + "'", cspHash}
}

func (s CSPSource) Validate() error {
	if len(s.text) > 2048 {
		return fault.New(fault.Invalid, "content security policy source exceeds its byte bound")
	}
	valid := false
	switch s.kind {
	case cspKeyword:
		switch s.text {
		case "'none'", "'self'", "'unsafe-inline'", "'unsafe-eval'", "'wasm-unsafe-eval'", "'strict-dynamic'", "'unsafe-hashes'", "'report-sample'":
			valid = true
		}
	case cspNonce:
		valid = s.text == "\x00"
	case cspHash:
		// Only digest constructors can produce this private representation.
		valid = len(s.text) > 0
	case cspHost:
		valid = validCSPHost(s.text)
	case cspScheme:
		valid = strings.HasSuffix(s.text, ":") && validCSPScheme(strings.TrimSuffix(s.text, ":"))
	}
	if !valid {
		return fault.New(fault.Invalid, "invalid content security policy source")
	}
	return nil
}

func validCSPScheme(text string) bool {
	if len(text) == 0 || !asciiLetter(text[0]) {
		return false
	}
	for i := 1; i < len(text); i++ {
		b := text[i]
		if !asciiLetter(b) && !(b >= '0' && b <= '9') && b != '+' && b != '-' && b != '.' {
			return false
		}
	}
	return true
}

func asciiLetter(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }

func validCSPHost(pattern string) bool {
	if len(pattern) == 0 || len(pattern) > 2048 {
		return false
	}
	if scheme, rest, ok := strings.Cut(pattern, "://"); ok {
		if !validCSPScheme(scheme) {
			return false
		}
		pattern = rest
	}
	host, path, hasPath := strings.Cut(pattern, "/")
	if hasPath && !validCSPPath("/"+path) {
		return false
	}
	if name, port, ok := strings.Cut(host, ":"); ok {
		host = name
		if port != "*" {
			if len(port) == 0 {
				return false
			}
			for i := range len(port) {
				if port[i] < '0' || port[i] > '9' {
					return false
				}
			}
		}
	}
	if host == "*" {
		return true
	}
	host = strings.TrimPrefix(host, "*.")
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return false
	}
	for part := range strings.SplitSeq(host, ".") {
		if part == "" {
			return false
		}
		for i := range len(part) {
			b := part[i]
			if !asciiLetter(b) && !(b >= '0' && b <= '9') && b != '-' {
				return false
			}
		}
	}
	return true
}

func validCSPPath(path string) bool {
	for i := 0; i < len(path); i++ {
		b := path[i]
		if asciiLetter(b) || b >= '0' && b <= '9' || strings.ContainsRune("/-._~!$&'()*+:=@", rune(b)) {
			continue
		}
		if b == '%' && i+2 < len(path) && cspHex(path[i+1]) && cspHex(path[i+2]) {
			i += 2
			continue
		}
		return false
	}
	return true
}

func cspHex(b byte) bool { return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F' }
