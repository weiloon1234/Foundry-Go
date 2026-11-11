package capturedisclosure

import (
	"encoding/json"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
)

func TestCredentialNameWordRules(t *testing.T) {
	for _, test := range []struct {
		name   string
		masked bool
	}{
		{"Authorization", true}, {"Proxy-Authorization", true}, {"Cookie", true}, {"Set-Cookie", true},
		{"X-Api-Key", true}, {"apiKey", true}, {"client_secret", true}, {"access_token", true},
		{"auth", true}, {"Auth", true}, {"X-Auth-Token", true}, {"X-Auth-Key", true}, {"x_auth", true}, {"authToken", true}, {"X-Auth-Email", true},
		{"key", true}, {"KEY", true}, {"X-RapidAPI-Key", true}, {"Ocp-Apim-Subscription-Key", true}, {"api_key", true}, {"license.key", true},
		{"subscriptionKey", true}, {"accesskey", true}, {"awsAccessKey", true}, {"secretKey", true}, {"privatekey", true}, {"webhookSigningKey", true},
		{"Idempotency-Key", false}, {"X-Idempotency-Key", false}, {"idempotency_key", false}, {"idempotencyKey", false},
		{"monkey", false}, {"turkey", false}, {"hockey", false}, {"turnkey", false}, {"donkey", false}, {"whiskey", false}, {"keyboard", false},
		{"author", false}, {"Authority", false}, {"authorName", false}, {"oauth_state", false}, {"WWW-Authenticate", false},
		{"Stripe-Signature", false}, {"X-Twilio-Signature", false}, {"Content-Type", false}, {"amount", false},
	} {
		if got := CredentialName(test.name, nil); got != test.masked {
			t.Errorf("CredentialName(%q) = %v, want %v", test.name, got, test.masked)
		}
	}
}

func TestCredentialQueryNameAndExtraNames(t *testing.T) {
	for _, test := range []struct {
		name   string
		masked bool
	}{
		{"signature", true}, {"ticket", true}, {"sig", true}, {"X-Amz-Signature", true}, {"X-Goog-Signature", true},
		{"key", true}, {"access_token", true}, {"page", false}, {"signed", false}, {"design", false}, {"X-Amz-Expires", false},
	} {
		if got := CredentialQueryName(test.name, nil); got != test.masked {
			t.Errorf("CredentialQueryName(%q) = %v, want %v", test.name, got, test.masked)
		}
	}
	extra := NewNames([]string{"PWD", "sid"})
	if !CredentialName("pwd", extra) || !CredentialName("SID", extra) || CredentialName("side", extra) || CredentialName("pwd", nil) {
		t.Fatal("extra names are not exact and case-insensitive")
	}
	if NewNames(nil) != nil {
		t.Fatal("empty extra names allocated a set")
	}
}

func TestInspectableDecidesFromHeadersAlone(t *testing.T) {
	for _, test := range []struct {
		contentType, encoding string
		format                Format
		reason                Reason
	}{
		{"application/json", "", JSON, NoReason},
		{"application/problem+json; charset=utf-8", "identity", JSON, NoReason},
		{"application/x-www-form-urlencoded", "", Form, NoReason},
		{"multipart/form-data; boundary=x", "", Multipart, NoReason},
		{"application/json", "gzip", Opaque, ContentEncoded},
		{"text/html", "", Opaque, UnsupportedMediaType},
		{"", "", Opaque, UnsupportedMediaType},
		{";;", "", Opaque, UnsupportedMediaType},
	} {
		media, reason := Inspectable(test.contentType, test.encoding)
		if media.Format != test.format || reason != test.reason {
			t.Errorf("Inspectable(%q, %q) = %v, %v", test.contentType, test.encoding, media.Format, reason)
		}
	}
}

// Boss Club's Smartpay gateway returns its session token in a JSON field
// named auth, and authenticates with an api_key multipart field.
func TestRedactMasksGatewayCredentialsAndExtraNames(t *testing.T) {
	p := Policy{ValueBytes: 4096, BodyBytes: 1 << 20, Names: NewNames([]string{"member_ref"})}
	media, _ := Inspectable("application/json", "")
	out, reason := Redact([]byte(`{"auth":"eyJ.token","author":"Ann","Idempotency-Key":"k1","member_ref":"1234","nested":{"key":"v"}}`), media, p)
	if reason != NoReason || string(out) != `{"auth":"[masked]","author":"Ann","Idempotency-Key":"k1","member_ref":"[masked]","nested":{"key":"[masked]"}}` {
		t.Fatal("JSON credential fields were not masked by the shared rule", string(out), reason)
	}
	media, _ = Inspectable("multipart/form-data; boundary=b", "")
	body := "--b\r\nContent-Disposition: form-data; name=\"api_key\"\r\n\r\nsk_live\r\n--b\r\nContent-Disposition: form-data; name=\"amount\"\r\n\r\n12\r\n--b--\r\n"
	out, reason = Redact([]byte(body), media, p)
	if reason != NoReason || string(out) != `[{"name":"api_key","bytes":7,"value":"[masked]"},{"name":"amount","bytes":2,"value":"12"}]` {
		t.Fatal("multipart credential field was not masked", string(out), reason)
	}
	media, _ = Inspectable("application/json", "")
	if _, reason := Redact([]byte(`{"a":`), media, p); reason != Malformed {
		t.Fatal("a truncated document was stored", reason)
	}
	if _, reason := Redact([]byte(`[1,2,3]`), media, Policy{ValueBytes: 64, BodyBytes: 4}); reason != RedactionLimit {
		t.Fatal("output over the body bound was stored", reason)
	}
}

func TestFieldsAndHeadersMaskAndStoreValidText(t *testing.T) {
	p := Policy{ValueBytes: 4, BodyBytes: 1 << 20}
	fields, cut, valid := Fields("a=1&key=secret&a=2&long=abcdef&partial=x", true, p, func(name string) bool { return CredentialQueryName(name, nil) })
	if !valid || !cut || len(fields) != 3 || fields[0].Values[1] != "2" || fields[1].Values[0] != Masked || fields[2].Values[0] != "[omitted 6 bytes]" {
		t.Fatal("query fields changed", fields, cut, valid)
	}
	if _, _, valid := Fields("a=%zz", false, p, func(string) bool { return false }); valid {
		t.Fatal("malformed escape accepted")
	}
	headers := Headers([]httpcapture.Field{{Name: "X-Auth", Values: []string{"t"}}, {Name: "X-N\x00", Values: []string{"\xff"}}}, func(name string) bool { return CredentialName(name, nil) })
	if headers[0].Values[0] != Masked || headers[1].Name != "X-N�" || headers[1].Values[0] != "�" {
		t.Fatal("headers were not masked or made storable", headers)
	}
	if TextLimit("abé", 3) != "ab" || len(Bytes(nil)) != 0 || Bytes(nil) == nil {
		t.Fatal("storable text or bytes changed")
	}
}

func FuzzRedactJSON(f *testing.F) {
	for _, seed := range []string{`{"a":1}`, `[{"token":{"x":[1,2]}},"s"]`, `{"a":"é\ud800"}`, `"x"`, `{`, `{"a":1}{}`} {
		f.Add([]byte(seed))
	}
	p := Policy{ValueBytes: 16, BodyBytes: 1 << 16}
	f.Fuzz(func(t *testing.T, raw []byte) {
		out, err := redactJSON(raw, p)
		if err != nil {
			if err != errMalformed && err != errLimit {
				t.Fatal("unexpected redaction error", err)
			}
			return
		}
		if !json.Valid(out) || len(out) > p.BodyBytes {
			t.Fatal("redaction produced invalid or unbounded JSON")
		}
		if !json.Valid(raw) {
			t.Fatal("redaction accepted input that is not one valid JSON value")
		}
	})
}

func FuzzRedactFields(f *testing.F) {
	for _, seed := range []string{"a=1&b=2", "token=x&token=y", "%zz", "a&&b=", "signature=s", "key=k&sig=s"} {
		f.Add(seed, false)
	}
	mask := func(name string) bool { return CredentialQueryName(name, nil) }
	f.Fuzz(func(t *testing.T, raw string, truncated bool) {
		fields, _, valid := Fields(raw, truncated, Policy{ValueBytes: 4096, BodyBytes: 1 << 20}, mask)
		if !valid {
			return
		}
		if len(fields) > maxFormFields {
			t.Fatal("field cap exceeded")
		}
		for _, field := range fields {
			for _, v := range field.Values {
				if mask(field.Name) && v != Masked {
					t.Fatal("credential query value disclosed")
				}
			}
		}
	})
}
