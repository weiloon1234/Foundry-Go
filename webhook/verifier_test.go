package webhook_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/webhook"
)

type accountID int64
type frozenClock struct{ now time.Time }

func (c frozenClock) Now() time.Time { return c.now }

var now = time.Unix(1700000000, 0).UTC()

const symmetricKey = "01234567890123456789012345678901"

func key() secret.String {
	return secret.New("whsec_" + base64.StdEncoding.EncodeToString([]byte(symmetricKey)))
}
func standardHeaders(id, stamp, body string) http.Header {
	mac := hmac.New(sha256.New, []byte(symmetricKey))
	io.WriteString(mac, id+"."+stamp+"."+body)
	h := make(http.Header)
	h.Set("Webhook-Id", id)
	h.Set("Webhook-Timestamp", stamp)
	h.Set("Webhook-Signature", "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	return h
}
func standard(t *testing.T, keys ...secret.String) *webhook.Verifier[accountID] {
	t.Helper()
	if len(keys) == 0 {
		keys = []secret.String{key()}
	}
	v, err := webhook.New(webhook.DefaultConfig("billing", webhook.Standard), accountID(7), keys, frozenClock{now})
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestStandardVerificationIdentityRotationAndTampering(t *testing.T) {
	verifier := standard(t, secret.New("whsec_"+base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))), key())
	body := `{"id":"event", "amount":12}`
	headers := standardHeaders("delivery_1", "1700000000", body)
	delivery, err := verifier.Verify(t.Context(), headers, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	var account accountID = delivery.Account()
	if account != 7 || delivery.Provider() != "billing" || delivery.ID() != "delivery_1" || !delivery.Timestamp().Equal(now) {
		t.Fatal("lost typed identity")
	}
	ctx, err := verifier.WithDelivery(t.Context(), delivery)
	if err != nil {
		t.Fatal(err)
	}
	if actual, ok := verifier.FromContext(ctx); !ok || actual.ID() != delivery.ID() {
		t.Fatal("missing context identity")
	}
	other := standard(t)
	if _, ok := other.FromContext(ctx); ok {
		t.Fatal("another verifier substituted")
	}
	if _, err := other.WithDelivery(ctx, delivery); err == nil {
		t.Fatal("cross-verifier delivery accepted")
	}
	if _, err := verifier.WithDelivery(ctx, webhook.Delivery[accountID]{}); err == nil {
		t.Fatal("zero proof accepted")
	}
	if _, err := verifier.Verify(t.Context(), headers, []byte(body)); err != nil {
		t.Fatal("verification replaced durable idempotency", err)
	}
	for _, change := range []func(http.Header) string{
		func(h http.Header) string { h.Set("Webhook-Id", "delivery_2"); return body },
		func(h http.Header) string { h.Set("Webhook-Timestamp", "1700000001"); return body },
		func(h http.Header) string { h.Add("Webhook-Signature", h.Get("Webhook-Signature")); return body },
		func(h http.Header) string { return strings.ReplaceAll(body, " ", "") },
		func(h http.Header) string { h.Set("Webhook-Signature", "v1,invalid"); return body },
	} {
		copy := headers.Clone()
		if _, err := verifier.Verify(t.Context(), copy, []byte(change(copy))); !errors.Is(err, webhook.InvalidSignature) {
			t.Fatal("tampering accepted", err)
		}
	}
	for _, seconds := range []int64{1699999699, 1700000301} {
		stamp := strconv.FormatInt(seconds, 10)
		if _, err := verifier.Verify(t.Context(), standardHeaders("delivery_1", stamp, body), []byte(body)); !errors.Is(err, webhook.InvalidSignature) {
			t.Fatal("stale/future signed input accepted", err)
		}
	}
	removed := standard(t, secret.New("whsec_"+base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))))
	if _, err := removed.Verify(t.Context(), headers, []byte(body)); !errors.Is(err, webhook.InvalidSignature) {
		t.Fatal("retired key accepted")
	}
	for _, value := range []any{verifier, *verifier} {
		for _, verb := range []string{"%v", "%+v", "%#v"} {
			if fmt.Sprintf(verb, value) != "webhook verifier" {
				t.Fatal("verifier must redact both pointers and copied values")
			}
		}
	}
}
func TestStandardEd25519AndUnknownSignatureVersion(t *testing.T) {
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{1}, ed25519.SeedSize))
	public := private.Public().(ed25519.PublicKey)
	verifier := standard(t, secret.New("whpk_"+base64.StdEncoding.EncodeToString(public)))
	body := `{"value":1}`
	headers := standardHeaders("delivery_2", "1700000000", body)
	signature := ed25519.Sign(private, []byte("delivery_2.1700000000."+body))
	headers.Set("Webhook-Signature", "v9,ignored v1a,"+base64.StdEncoding.EncodeToString(signature))
	if _, err := verifier.Verify(t.Context(), headers, []byte(body)); err != nil {
		t.Fatal(err)
	}
	headers.Set("Webhook-Id", "delivery.2")
	if _, err := verifier.Verify(t.Context(), headers, []byte(body)); !errors.Is(err, webhook.InvalidSignature) {
		t.Fatal("ambiguous ID accepted")
	}
}
func TestStripeSignsOriginalJSONAndRejectsDuplicateIdentity(t *testing.T) {
	const stripeKey = "whsec_example_endpoint_secret_for_tests"
	v, err := webhook.New(webhook.DefaultConfig("stripe", webhook.Stripe), accountID(8), []secret.String{secret.New(stripeKey)}, frozenClock{now})
	if err != nil {
		t.Fatal(err)
	}
	sign := func(body string) http.Header {
		mac := hmac.New(sha256.New, []byte(stripeKey))
		io.WriteString(mac, "1700000000."+body)
		h := make(http.Header)
		h.Set("Stripe-Signature", "t=1700000000,v0=00,v1="+hex.EncodeToString(mac.Sum(nil)))
		return h
	}
	body := `{"id":"evt_1","data":{"amount":12}}`
	delivery, err := v.Verify(t.Context(), sign(body), []byte(body))
	if err != nil || delivery.ID() != "evt_1" || delivery.Account() != 8 {
		t.Fatal(delivery, err)
	}
	for _, bad := range []string{`{"id":"evt_1","id":"evt_2"}`, `{"id":12}`, `[]`} {
		if _, err := v.Verify(t.Context(), sign(bad), []byte(bad)); !errors.Is(err, webhook.InvalidSignature) {
			t.Fatal("bad signed identity accepted", err)
		}
	}
	validHeader := sign(body).Get("Stripe-Signature")
	for _, duplicate := range []string{validHeader + ",t=1700000000", "t=," + validHeader, validHeader + ",t=", "t=,t="} {
		header := make(http.Header)
		header.Set("Stripe-Signature", duplicate)
		if _, err := v.Verify(t.Context(), header, []byte(body)); !errors.Is(err, webhook.InvalidSignature) {
			t.Fatal("repeated timestamp accepted, including an empty first value")
		}
	}
}

type trackedBody struct {
	*bytes.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }
func TestOriginalBodyBoundAndRestoration(t *testing.T) {
	config := webhook.DefaultConfig("billing", webhook.Standard)
	config.MaxBodyBytes = 32
	verifier, err := webhook.New(config, accountID(7), []secret.String{key()}, frozenClock{now})
	if err != nil {
		t.Fatal(err)
	}
	body := `{ "amount" : 12 }`
	request := httptest.NewRequest("POST", "/hook", strings.NewReader(body))
	original := &trackedBody{Reader: bytes.NewReader([]byte(body))}
	request.Body = original
	request.Header = standardHeaders("delivery_1", "1700000000", body)
	if _, err := verifier.VerifyRequest(request); err != nil {
		t.Fatal(err)
	}
	restored, _ := io.ReadAll(request.Body)
	if !original.closed || string(restored) != body {
		t.Fatal("original bytes/closure lost")
	}
	large := &trackedBody{Reader: bytes.NewReader(bytes.Repeat([]byte{'x'}, 1000))}
	request.Body = large
	request.ContentLength = -1
	if _, err := verifier.VerifyRequest(request); !errors.Is(err, webhook.BodyTooLarge) {
		t.Fatal(err)
	}
	if !large.closed || large.Len() != 1000-33 {
		t.Fatal("unbounded body read")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := verifier.Verify(canceled, request.Header, []byte(body)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	request = httptest.NewRequest("POST", "/hook", strings.NewReader(body))
	request.Header.Set("Content-Encoding", "gzip")
	if _, err := verifier.VerifyRequest(request); err == nil {
		t.Fatal("transformed body boundary accepted")
	}
}
func FuzzStandardHeaders(f *testing.F) {
	f.Add("delivery_1", "1700000000", "v1,invalid", []byte(`{}`))
	verifier, _ := webhook.New(webhook.DefaultConfig("billing", webhook.Standard), accountID(7), []secret.String{key()}, frozenClock{now})
	f.Fuzz(func(t *testing.T, id, stamp, signature string, body []byte) {
		if len(body) > 1<<20 {
			return
		}
		headers := make(http.Header)
		headers.Set("Webhook-Id", id)
		headers.Set("Webhook-Timestamp", stamp)
		headers.Set("Webhook-Signature", signature)
		_, _ = verifier.Verify(t.Context(), headers, body)
	})
}

func FuzzStripeHeaders(f *testing.F) {
	const stripeKey = "whsec_" + symmetricKey
	verifier, _ := webhook.New(webhook.DefaultConfig("stripe", webhook.Stripe), accountID(7), []secret.String{secret.New(stripeKey)}, frozenClock{now})
	body := []byte(`{"id":"evt_1"}`)
	mac := hmac.New(sha256.New, []byte(stripeKey))
	io.WriteString(mac, "1700000000.")
	mac.Write(body)
	signed := "t=1700000000,v1=" + hex.EncodeToString(mac.Sum(nil))
	f.Add(signed, body)
	f.Add("t=,"+signed, body)
	f.Fuzz(func(t *testing.T, signature string, body []byte) {
		if len(body) > 1<<20 {
			return
		}
		headers := make(http.Header)
		headers.Set("Stripe-Signature", signature)
		_, err := verifier.Verify(t.Context(), headers, body)
		if strings.HasPrefix(signature, "t=,") && err == nil {
			t.Fatal("empty first timestamp hid a repeated timestamp")
		}
	})
}
