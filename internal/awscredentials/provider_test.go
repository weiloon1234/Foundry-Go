package awscredentials

import (
	"context"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/secret"
)

func TestUnknownExpiryRefreshesWithoutClaimingACredentialExpiry(t *testing.T) {
	var expires time.Time
	adapter := Adapter{Provider: credentials.ProviderFunc(func(context.Context) (credentials.Value, error) {
		return credentials.Value{AccessKey: secret.New("fixture-key"), SecretKey: secret.New("fixture-secret"), Expires: expires}, nil
	})}
	before := time.Now()
	value, err := adapter.Retrieve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// The SDK credentials cache never refreshes values that cannot expire, so a
	// rotating provider without reported expiry must still be consulted again.
	if !value.CanExpire || value.Expires.Before(before.Add(RefreshInterval)) || value.Expires.After(time.Now().Add(RefreshInterval)) {
		t.Fatal("unknown expiry was cached indefinitely", value.CanExpire, value.Expires)
	}
	if _, known := KnownExpiry(value); known {
		t.Fatal("synthetic refresh deadline reported as credential expiry")
	}
	expires = time.Now().Add(time.Hour).UTC()
	value, err = adapter.Retrieve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if at, known := KnownExpiry(value); !known || !at.Equal(expires) || !value.CanExpire {
		t.Fatal("reported temporary credential expiry was lost", at, known)
	}
}
