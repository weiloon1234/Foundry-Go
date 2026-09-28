package multifactor_test

import (
	"encoding/json"
	"testing"

	"foundry.test/consumer/multifactor"
	"github.com/weiloon1234/Foundry-Go/auth/mfa"
	"github.com/weiloon1234/Foundry-Go/encryption"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func TestConsumerEncryptionAndTypedFactorInputs(t *testing.T) {
	key, err := encryption.GenerateKey("test")
	if err != nil {
		t.Fatal(err)
	}
	ring, err := encryption.NewKeyring(key.ID(), key)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := encryption.NewContext("auth.mfa.totp.v1", secret.New("tenant-1/member-7/factor-9"))
	if err != nil {
		t.Fatal(err)
	}
	factor, err := mfa.GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := multifactor.EncryptEnrollment(t.Context(), ring, owner, factor)
	if err != nil {
		t.Fatal(err)
	}
	got, err := multifactor.DecryptEnrollment(t.Context(), ring, owner, encrypted)
	if err != nil || got.Secret() != factor.Secret() {
		t.Fatal("typed round trip", err)
	}
	var request multifactor.TOTPRequest
	if err := json.Unmarshal([]byte(`{"code":"001234"}`), &request); err != nil || request.Code.Secret().Reveal() != "001234" {
		t.Fatal("typed code decoding", err)
	}
	if err := request.Code.Validate(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(request)
	if err != nil || string(encoded) != `{"code":"[REDACTED]"}` {
		t.Fatal("input serialized its secret", err)
	}
}

func TestGeneratedFactorContractsDecodeAndRedact(t *testing.T) {
	limits := foundryhttp.DefaultEndpointLimits().Response
	input, err := multifactor.TOTPRequestJSON().Decode(t.Context(), []byte(`{"code":"001234"}`), limits)
	if err != nil || input.Code.Secret().Reveal() != "001234" {
		t.Fatal("generated TOTP decoder", err)
	}
	output, err := multifactor.TOTPRequestJSON().Encode(t.Context(), input, limits)
	if err != nil || string(output) != `{"code":"[REDACTED]"}` {
		t.Fatal("generated TOTP encoder", err)
	}
	recovery, err := multifactor.RecoveryRequestJSON().Decode(t.Context(), []byte(`{"code":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}`), limits)
	if err != nil || recovery.Code.Validate() != nil {
		t.Fatal("generated recovery decoder", err)
	}
	output, err = multifactor.RecoveryRequestJSON().Encode(t.Context(), recovery, limits)
	if err != nil || string(output) != `{"code":"[REDACTED]"}` {
		t.Fatal("generated recovery encoder", err)
	}
	for _, wire := range []string{`{"code":null}`, `{"code":123456}`, `{"code":"12345"}`, `{}`} {
		if _, err := multifactor.TOTPRequestJSON().Decode(t.Context(), []byte(wire), limits); err == nil {
			t.Fatal("invalid TOTP wire input accepted")
		}
	}
}
