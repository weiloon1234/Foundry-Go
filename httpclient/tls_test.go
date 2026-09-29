package httpclient_test

import (
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func TestPerClientCertificateAuthorities(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	t.Cleanup(server.Close)
	config := testConfig()
	config.BaseURL = server.URL
	config.Retry = httpclient.NoRetries()
	plain := newClient(t, config, nil)
	if _, err := plain.Do(t.Context(), plain.Get("x")); err == nil {
		t.Fatal("private certificate authority trusted without configuration")
	}
	config.TLS.CertificateAuthorities = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	c := newClient(t, config, nil)
	if response, err := c.Do(t.Context(), c.Get("x")); err != nil || response.Status() != 204 {
		t.Fatal("configured certificate authority was not trusted", err)
	}
	for _, tls := range []httpclient.TLSConfig{
		{CertificateAuthorities: "not PEM"},
		{AppendSystemRoots: true},
		{Certificate: secret.New("certificate without key")},
		{Certificate: secret.New("bad"), PrivateKey: secret.New("bad")},
	} {
		invalid := testConfig()
		invalid.TLS = tls
		if err := invalid.Validate(); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid TLS configuration accepted", err)
		}
	}
	custom := testConfig()
	custom.TLS.ServerName = "api.example.test"
	if _, err := httpclient.New(custom, http.DefaultTransport); err == nil {
		t.Fatal("TLS options combined with a caller transport")
	}
}
