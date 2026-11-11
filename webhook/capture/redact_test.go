package capture

import (
	"bytes"
	"encoding/json"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/internal/capturedisclosure"
	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
	store "github.com/weiloon1234/Foundry-Go/internal/webhookcapturestore"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/value"
)

const testRequestID = attribution.RequestID("0192f3a4-5b6c-7d8e-9f01-23456789abcd")

func testPolicy() policy { return DefaultConfig().policy() }

func snapshot(contentType string, body []byte) httpcapture.Snapshot {
	s := httpcapture.Snapshot{
		Metadata: httpcapture.Metadata{RequestID: testRequestID, ReceivedAt: time.Date(2026, 11, 11, 1, 2, 3, 0, time.UTC), Method: "POST", EscapedPath: "/wh/stripe", ContentType: contentType, ClientIP: netip.MustParseAddr("203.0.113.4")},
		Body:     httpcapture.Body{Prefix: body, End: httpcapture.EOF, ObservedBytes: int64(len(body)), MeasuredBytes: value.Of(int64(len(body)))},
		Completion: httpcapture.Completion{Route: "webhooks.stripe", Result: observability.Result{Status: 200}, Status: value.Of(uint16(200)),
			ResponseHead: []byte(`{"ok":true}`), ResponseContentType: "application/json", ResponseBytes: 11, Duration: 1500 * time.Microsecond},
	}
	return s
}

func redactBodyText(t *testing.T, s httpcapture.Snapshot, p policy) (store.Capture, string) {
	t.Helper()
	row, err := redact(s, p)
	if err != nil {
		t.Fatal(err)
	}
	return row, string(row.Body)
}

func TestRedactJSONMasksCredentialsAndShortensValues(t *testing.T) {
	p := testPolicy()
	p.valueBytes = 8
	body := []byte(`{"id":"evt_1","amount":1.50e2,"token":{"nested":"secret"},"card":{"number":"4242"},"api_key":"k","note":"0123456789","list":[true,null,"x"],"id":"dup","html":"<b>&"}`)
	row, got := redactBodyText(t, snapshot("application/json; charset=utf-8", body), p)
	want := `{"id":"evt_1","amount":1.50e2,"token":"[masked]","card":{"number":"4242"},"api_key":"[masked]","note":"[omitted 10 bytes]","list":[true,null,"x"],"id":"dup","html":"<b>&"}`
	if got != want || row.BodyState != string(BodyJSON) || row.BodyMode != string(Redacted) || !row.BodyOmission.IsNull() {
		t.Fatalf("redacted JSON changed shape:\n got %s\nwant %s", got, want)
	}
	if !json.Valid(row.Body) {
		t.Fatal("redacted JSON is not valid JSON")
	}
}

func TestRedactBodyOmissionsNeverStoreRawInput(t *testing.T) {
	secret := `{"token":"secret-value",`
	for _, test := range []struct {
		name     string
		edit     func(*httpcapture.Snapshot)
		omission CaptureBodyOmission
	}{
		{"malformed json", func(s *httpcapture.Snapshot) { s.Body.Prefix = []byte(secret) }, OmittedMalformed},
		{"trailing value", func(s *httpcapture.Snapshot) { s.Body.Prefix = []byte(`{"a":1} {"token":"secret-value"}`) }, OmittedMalformed},
		{"unsupported media", func(s *httpcapture.Snapshot) { s.Metadata.ContentType = "text/plain" }, OmittedUnsupportedMediaType},
		{"unparseable media", func(s *httpcapture.Snapshot) { s.Metadata.ContentType = ";;" }, OmittedUnsupportedMediaType},
		{"encoded", func(s *httpcapture.Snapshot) { s.Metadata.ContentEncoding = "gzip" }, OmittedContentEncoded},
		{"unread", func(s *httpcapture.Snapshot) { s.Body = httpcapture.Body{} }, OmittedUnread},
		{"over limit", func(s *httpcapture.Snapshot) { s.Body.End = httpcapture.OverCaptureLimit }, OmittedOverCaptureLimit},
		{"body limit", func(s *httpcapture.Snapshot) {
			s.Body.End, s.Body.ReadFailure = httpcapture.SourceFailure, httpcapture.BodyLimit
		}, OmittedBodyLimit},
		{"disconnect", func(s *httpcapture.Snapshot) {
			s.Body.End, s.Body.ReadFailure = httpcapture.SourceFailure, httpcapture.ClientDisconnect
		}, OmittedClientDisconnect},
		{"timeout", func(s *httpcapture.Snapshot) {
			s.Body.End, s.Body.ReadFailure = httpcapture.SourceFailure, httpcapture.ReadTimeout
		}, OmittedReadTimeout},
		{"read failed", func(s *httpcapture.Snapshot) {
			s.Body.End, s.Body.ReadFailure = httpcapture.SourceFailure, httpcapture.ReadFailed
		}, OmittedReadFailed},
		{"depth", func(s *httpcapture.Snapshot) {
			s.Body.Prefix = []byte(strings.Repeat("[", 65) + strings.Repeat("]", 65))
		}, OmittedRedactionLimit},
		{"malformed form", func(s *httpcapture.Snapshot) {
			s.Metadata.ContentType, s.Body.Prefix = "application/x-www-form-urlencoded", []byte("token=secret-value&x=%zz")
		}, OmittedMalformed},
		{"malformed multipart", func(s *httpcapture.Snapshot) {
			s.Metadata.ContentType, s.Body.Prefix = "multipart/form-data; boundary=b", []byte("--b\r\nContent-Disposition: form-data; name=\"token\"\r\n\r\nsecret-value")
		}, OmittedMalformed},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := snapshot("application/json", []byte(`{"token":"secret-value"}`))
			test.edit(&s)
			row, err := redact(s, testPolicy())
			if err != nil {
				t.Fatal(err)
			}
			omission, _ := row.BodyOmission.Get()
			if row.BodyState != string(BodyOmitted) || omission != string(test.omission) || len(row.Body) != 0 {
				t.Fatal("body was not omitted with its reason", row.BodyState, omission)
			}
		})
	}
}

func TestRedactFullModeIsExactAndForceRedactedReducesIt(t *testing.T) {
	body := []byte("{\"token\":\"secret\"}\n\x00\xff")
	s := snapshot("application/octet-stream", body)
	s.Metadata.Full = true
	s.Metadata.RawQuery = "signature=abc&ticket=t&plain=1"
	s.Metadata.Headers = []httpcapture.Field{{Name: "Authorization", Values: []string{"Bearer x"}}, {Name: "Stripe-Signature", Values: []string{"t=1,v1=abc"}}}
	row, _ := redactBodyText(t, s, testPolicy())
	if row.BodyMode != string(Full) || row.BodyState != string(BodyExact) || !bytes.Equal(row.Body, body) {
		t.Fatal("full mode did not store exact bytes")
	}
	headers, _ := row.Headers.Decode()
	query, _ := row.Query.Decode()
	if headers[0].Values[0] != capturedisclosure.Masked || headers[1].Values[0] != "t=1,v1=abc" || query[0].Values[0] != capturedisclosure.Masked || query[1].Values[0] != capturedisclosure.Masked || query[2].Values[0] != "1" {
		t.Fatal("full mode bypassed header or query masking", headers, query)
	}
	p := testPolicy()
	p.forceRedacted = true
	row, _ = redactBodyText(t, s, p)
	omission, _ := row.BodyOmission.Get()
	if row.BodyMode != string(Redacted) || omission != string(OmittedUnsupportedMediaType) {
		t.Fatal("force_redacted did not reduce a full declaration")
	}
	s.Body.End = httpcapture.OverCaptureLimit
	row, _ = redactBodyText(t, s, testPolicy())
	if row.BodyState != string(BodyOmitted) || len(row.Body) != 0 {
		t.Fatal("full mode stored a partial body")
	}
}

func TestRedactFormMultipartQueryAndMetadata(t *testing.T) {
	p := testPolicy()
	p.valueBytes = 6
	form := snapshot("application/x-www-form-urlencoded", []byte("b=2&password=hunter2&a=1&b=3&long=1234567"))
	_, got := redactBodyText(t, form, p)
	if got != `[{"name":"b","values":["2","3"]},{"name":"password","values":["[masked]"]},{"name":"a","values":["1"]},{"name":"long","values":["[omitted 7 bytes]"]}]` {
		t.Fatal("form redaction changed", got)
	}
	multipartBody := "--b\r\nContent-Disposition: form-data; name=\"secret\"\r\n\r\nvalue\r\n--b\r\nContent-Disposition: form-data; name=\"file\"; filename=\"r.pdf\"\r\nContent-Type: application/pdf\r\n\r\n%PDF-raw\r\n--b--\r\n"
	_, got = redactBodyText(t, snapshot("multipart/form-data; boundary=b", []byte(multipartBody)), p)
	if got != `[{"name":"secret","bytes":5,"value":"[masked]"},{"name":"file","filename":"r.pdf","content_type":"application/pdf","bytes":8}]` || strings.Contains(got, "PDF-raw") {
		t.Fatal("multipart summary changed or kept file bytes", got)
	}
	s := snapshot("application/json", nil)
	s.Body.Prefix, s.Body.MeasuredBytes = []byte{}, value.Of(int64(0))
	s.Metadata.RawQuery, s.Metadata.QueryTruncated = "a=1&b=%zz", false
	s.Metadata.PathTruncated, s.Metadata.HeadersTruncated = true, true
	s.Completion.ResponseTruncated, s.Completion.Aborted, s.Completion.Status = true, true, value.Nullable[uint16]{}
	s.Completion.Result = observability.Result{Outcome: observability.Panicked}
	row, err := redact(s, p)
	if err != nil {
		t.Fatal(err)
	}
	omissions, _ := row.Omissions.Decode()
	query, _ := row.Query.Decode()
	if row.BodyState != string(BodyEmpty) || len(query) != 0 || row.ResponseState != string(ResponseAborted) || row.Outcome != observability.Panicked || !row.ResponseStatus.IsNull() ||
		!slices.Equal(omissions, []string{"path_truncated", "query_malformed", "headers_truncated", "response_truncated"}) {
		t.Fatal("metadata facts changed", row.BodyState, omissions, query, row.ResponseState)
	}
	s.Metadata.RawQuery, s.Metadata.QueryTruncated = "a=1&token=secret&b=par", true
	row, _ = redact(s, p)
	query, _ = row.Query.Decode()
	if len(query) != 2 || query[1].Values[0] != capturedisclosure.Masked {
		t.Fatal("truncated query kept its partial last pair or leaked a credential", query)
	}
	if _, err := redact(httpcapture.Snapshot{Metadata: httpcapture.Metadata{RequestID: "not-a-uuid"}}, p); err == nil {
		t.Fatal("invalid request ID produced a row")
	}
}

func TestRedactSensitiveHeaderNames(t *testing.T) {
	for name, masked := range map[string]bool{"Authorization": true, "Proxy-Authorization": true, "Cookie": true, "X-Api-Key": true, "X-Auth-Token": true, "Stripe-Signature": false, "X-Hub-Signature-256": false, "Content-Type": false,
		"X-Auth-Key": true, "X-RapidAPI-Key": true, "Ocp-Apim-Subscription-Key": true, "Idempotency-Key": false, "X-Author": false} {
		s := snapshot("application/json", nil)
		s.Metadata.Headers = []httpcapture.Field{{Name: name, Values: []string{"value"}}}
		s.Completion.ResponseHeaders = []httpcapture.Field{{Name: name, Values: []string{"sent"}}}
		row, err := redact(s, testPolicy())
		if err != nil {
			t.Fatal(err)
		}
		request, _ := row.Headers.Decode()
		response, _ := row.ResponseHeaders.Decode()
		if request[0].Name != name || (request[0].Values[0] == capturedisclosure.Masked) != masked {
			t.Fatal("request header disclosure rule changed for", name, request[0].Values)
		}
		if response[0].Name != name || response[0].Values[0] != "sent" {
			t.Fatal("response header was not stored whole", name, response[0].Values)
		}
	}
}

func TestRedactStoresOnlyValidText(t *testing.T) {
	s := snapshot("application/x-www-form-urlencoded", []byte("a=%00%ff"))
	s.Metadata.Method = "P\x00ST"
	s.Metadata.EscapedPath = strings.Repeat("p", 4095) + "\xff"
	s.Metadata.RawQuery = "q=%00&n=%C3"
	s.Metadata.Headers = []httpcapture.Field{{Name: "X-Latin", Values: []string{"caf\xe9"}}}
	s.Completion.ResponseHeaders = []httpcapture.Field{{Name: "X-N\x00ul", Values: []string{"a\x00\xff"}}}
	s.Metadata.ClientIP = netip.MustParseAddr("fe80::1%eth0")
	row, err := redact(s, testPolicy())
	if err != nil {
		t.Fatal(err)
	}
	headers, _ := row.Headers.Text()
	responseHeaders, _ := row.ResponseHeaders.Text()
	query, _ := row.Query.Text()
	ip, _ := row.ClientIP.Get()
	for _, text := range []string{row.Method, row.Path, headers, responseHeaders, query, string(row.Body)} {
		if !utf8.ValidString(text) || strings.Contains(text, "\x00") || strings.Contains(text, `\u0000`) {
			t.Fatal("stored text is not valid PostgreSQL text", text)
		}
	}
	if len(row.Path) > 4096 || ip != "fe80::1" {
		t.Fatal("sanitized metadata exceeded its column bound or kept an IP zone", len(row.Path), ip)
	}
}
