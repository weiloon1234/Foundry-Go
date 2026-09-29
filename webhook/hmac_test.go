package webhook_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/webhook"
)

const providerSecret = "provider-shared-secret-value"

func sign(message string) []byte {
	mac := hmac.New(sha256.New, []byte(providerSecret))
	_, _ = mac.Write([]byte(message))
	return mac.Sum(nil)
}
func hmacVerifier(t *testing.T, config webhook.Config) *webhook.Verifier[accountID] {
	t.Helper()
	v, err := webhook.New(config, accountID(9), []secret.String{secret.New("rotated-previous-secret"), secret.New(providerSecret)}, frozenClock{now})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestHMACPresetsVerifyProviderSignatures(t *testing.T) {
	body := `{"action":"opened"}`
	github := hmacVerifier(t, webhook.GitHubConfig("github"))
	headers := http.Header{"X-Hub-Signature-256": {"sha256=" + hex.EncodeToString(sign(body))}, "X-Github-Delivery": {"72d3162e-cc78-11e3-81ab-4c9367dc0958"}}
	bodyDigest := sha256.Sum256([]byte(body))
	contentID := "sha256-" + hex.EncodeToString(bodyDigest[:])
	delivery, err := github.Verify(t.Context(), headers, []byte(body))
	if err != nil || string(delivery.ID()) != contentID || delivery.Provider() != "github" {
		t.Fatal("GitHub signature rejected or identity came from an unsigned header", err)
	}
	if provided, ok := delivery.UnverifiedProviderID(); !ok || provided != "72d3162e-cc78-11e3-81ab-4c9367dc0958" {
		t.Fatal("provider delivery header was not kept as unverified metadata")
	}
	// A captured body and signature replayed with a fresh delivery header must
	// keep the same authenticated identity, so durable idempotency rejects it.
	replayed := headers.Clone()
	replayed.Set("X-GitHub-Delivery", "00000000-0000-4000-8000-000000000001")
	if replay, err := github.Verify(t.Context(), replayed, []byte(body)); err != nil || replay.ID() != delivery.ID() {
		t.Fatal("replay with a changed delivery header got a new identity", err)
	}
	if _, err := github.Verify(t.Context(), headers, []byte(body+" ")); !errors.Is(err, webhook.InvalidSignature) {
		t.Fatal("tampered GitHub body accepted", err)
	}
	unprefixed := headers.Clone()
	unprefixed.Set("X-Hub-Signature-256", hex.EncodeToString(sign(body)))
	if _, err := github.Verify(t.Context(), unprefixed, []byte(body)); !errors.Is(err, webhook.InvalidSignature) {
		t.Fatal("signature without the declared prefix accepted", err)
	}

	shopify := hmacVerifier(t, webhook.ShopifyConfig("shopify"))
	headers = http.Header{"X-Shopify-Hmac-Sha256": {base64.StdEncoding.EncodeToString(sign(body))}, "X-Shopify-Webhook-Id": {"b54557e4-bdd9-4b37-8a5f-bf7d70bcd043"}}
	if delivery, err := shopify.Verify(t.Context(), headers, []byte(body)); err != nil || string(delivery.ID()) != contentID {
		t.Fatal("Shopify signature rejected", err)
	}
	headers.Set("X-Shopify-Webhook-Id", "another-id")
	if delivery, err := shopify.Verify(t.Context(), headers, []byte(body)); err != nil || string(delivery.ID()) != contentID {
		t.Fatal("Shopify replay with a changed delivery header got a new identity", err)
	}

	slack := hmacVerifier(t, webhook.SlackConfig("slack"))
	stamp := "1700000000"
	headers = http.Header{"X-Slack-Signature": {"v0=" + hex.EncodeToString(sign("v0:"+stamp+":"+body))}, "X-Slack-Request-Timestamp": {stamp}}
	delivery, err = slack.Verify(t.Context(), headers, []byte(body))
	digest := sha256.Sum256([]byte("v0:" + stamp + ":" + body))
	if err != nil || string(delivery.ID()) != "sha256-"+hex.EncodeToString(digest[:]) || !delivery.Timestamp().Equal(now) {
		t.Fatal("Slack signature rejected or lost its signed-content identity", err)
	}
	if _, ok := delivery.UnverifiedProviderID(); ok {
		t.Fatal("Slack has no provider delivery header")
	}
	stale := "1699999000"
	headers = http.Header{"X-Slack-Signature": {"v0=" + hex.EncodeToString(sign("v0:"+stale+":"+body))}, "X-Slack-Request-Timestamp": {stale}}
	if _, err := slack.Verify(t.Context(), headers, []byte(body)); !errors.Is(err, webhook.InvalidSignature) {
		t.Fatal("stale Slack request accepted", err)
	}
}

func TestHMACSchemeIsExplicitAndValidated(t *testing.T) {
	config := webhook.DefaultConfig("partner", webhook.HMAC)
	config.HMAC = webhook.HMACScheme{SignatureHeader: "X-Partner-Signature", Algorithm: webhook.SHA512, Encoding: webhook.Base64}
	verifier := hmacVerifier(t, config)
	body := "payload"
	mac := hmac.New(sha512.New, []byte(providerSecret))
	_, _ = mac.Write([]byte(body))
	if _, err := verifier.Verify(t.Context(), http.Header{"X-Partner-Signature": {base64.StdEncoding.EncodeToString(mac.Sum(nil))}}, []byte(body)); err != nil {
		t.Fatal("configured algorithm and encoding rejected", err)
	}
	for _, scheme := range []webhook.HMACScheme{
		{},
		{SignatureHeader: "Bad Header", Algorithm: webhook.SHA256, Encoding: webhook.Hex},
		{SignatureHeader: "X-Sig", Algorithm: "md5", Encoding: webhook.Hex},
		{SignatureHeader: "X-Sig", Algorithm: webhook.SHA256, Encoding: "base32"},
		{SignatureHeader: "X-Sig", Algorithm: webhook.SHA256, Encoding: webhook.Hex, PayloadPrefix: "v0:"},
	} {
		invalid := webhook.DefaultConfig("partner", webhook.HMAC)
		invalid.HMAC = scheme
		if invalid.Validate() == nil {
			t.Fatal("invalid HMAC scheme accepted", scheme)
		}
	}
	stripe := webhook.DefaultConfig("stripe", webhook.Stripe)
	stripe.HMAC = webhook.HMACScheme{SignatureHeader: "X-Sig", Algorithm: webhook.SHA256, Encoding: webhook.Hex}
	if stripe.Validate() == nil {
		t.Fatal("HMAC scheme silently ignored for another protocol")
	}
	if _, err := webhook.New(webhook.GitHubConfig("github"), accountID(1), []secret.String{secret.New("short")}, frozenClock{now}); err == nil {
		t.Fatal("weak HMAC secret accepted")
	}
}

func TestSignatureVerificationWorkIsBounded(t *testing.T) {
	body := `{"value":1}`
	valid := standardHeaders("msg_2KWPBgLlAfxdpx2AI54pPJ85f4W", "1700000000", body).Get("Webhook-Signature")
	bogus := func(i int) string {
		return "v1," + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{byte(i + 1)}, sha256.Size))
	}
	verifier := standard(t)
	// Repeated copies count once, so a valid signature after them is checked.
	repeated := strings.Repeat(bogus(0)+" ", 15) + valid
	headers := standardHeaders("msg_2KWPBgLlAfxdpx2AI54pPJ85f4W", "1700000000", body)
	headers.Set("Webhook-Signature", repeated)
	if _, err := verifier.Verify(t.Context(), headers, []byte(body)); err != nil {
		t.Fatal("duplicate signatures displaced a valid one", err)
	}
	// Only a bounded number of distinct signatures is considered.
	var many []string
	for i := range 8 {
		many = append(many, bogus(i))
	}
	headers.Set("Webhook-Signature", strings.Join(append(many, valid), " "))
	if _, err := verifier.Verify(t.Context(), headers, []byte(body)); !errors.Is(err, webhook.InvalidSignature) {
		t.Fatal("signature beyond the checked bound was verified", err)
	}
	// Ed25519 checks are bounded across keys: 8 public keys and 8 signatures
	// never run 64 full-body verifications, and fail closed.
	var keys []secret.String
	for i := range 8 {
		private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{byte(i + 1)}, ed25519.SeedSize))
		keys = append(keys, secret.New("whpk_"+base64.StdEncoding.EncodeToString(private.Public().(ed25519.PublicKey))))
	}
	asymmetric := standard(t, keys...)
	var forged []string
	for i := range 8 {
		forged = append(forged, "v1a,"+base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{byte(i + 1)}, ed25519.SignatureSize)))
	}
	headers.Set("Webhook-Signature", strings.Join(forged, " "))
	if _, err := asymmetric.Verify(t.Context(), headers, []byte(body)); !errors.Is(err, webhook.InvalidSignature) {
		t.Fatal("forged asymmetric signatures accepted", err)
	}
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{1}, ed25519.SeedSize))
	headers.Set("Webhook-Signature", "v1a,"+base64.StdEncoding.EncodeToString(ed25519.Sign(private, []byte("msg_2KWPBgLlAfxdpx2AI54pPJ85f4W.1700000000."+body))))
	if _, err := asymmetric.Verify(t.Context(), headers, []byte(body)); err != nil {
		t.Fatal("valid asymmetric signature rejected", err)
	}
}

func TestStandardDeliveryIDsAcceptProviderCharacters(t *testing.T) {
	verifier := standard(t)
	body := `{}`
	for _, id := range []string{"msg_2KWPBgLlAfxdpx2AI54pPJ85f4W", "72d3162e-cc78-11e3-81ab-4c9367dc0958", "evt:2024/01~a+b=c", "ID_with-mixed_Case"} {
		if _, err := verifier.Verify(t.Context(), standardHeaders(id, "1700000000", body), []byte(body)); err != nil {
			t.Fatal("provider delivery ID rejected", id, err)
		}
	}
	for _, id := range []string{"has space", "dot.separated", "comma,separated", "tab\tseparated", "non-ascii-é"} {
		if _, err := verifier.Verify(t.Context(), standardHeaders(id, "1700000000", body), []byte(body)); !errors.Is(err, webhook.InvalidSignature) {
			t.Fatal("ambiguous delivery ID accepted", id, err)
		}
	}
}
