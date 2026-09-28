package credentials_test

import (
	"context"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/internal/awscredentials"
	"github.com/weiloon1234/Foundry-Go/secret"
	"strings"
	"testing"
	"time"
)

func TestSharedCredentialBoundaryAndRotation(t *testing.T) {
	settings := credentials.Settings{Mode: credentials.Static, AccessKey: secret.New("private-access"), SecretKey: secret.New("private-key"), SessionToken: secret.New("private-session")}
	source, err := credentials.Prepare(settings)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if _, err := source.Retrieve(t.Context()); err == nil {
		t.Fatal("source active before boot")
	}
	if err := source.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	value, err := source.Retrieve(t.Context())
	if err != nil || value.AccessKey.Reveal() != "private-access" {
		t.Fatal("credential retrieval failed", err)
	}
	for _, v := range []any{settings, value, source, *source} {
		if strings.Contains(fmt.Sprintf("%+v %#v", v, v), "private-") {
			t.Fatal("credential formatting leaked")
		}
	}
	generation := 0
	rotating := credentials.ProviderFunc(func(ctx context.Context) (credentials.Value, error) {
		generation++
		return credentials.Value{AccessKey: secret.New(fmt.Sprint(generation)), SecretKey: secret.New("secret"), Expires: time.Now().Add(time.Hour)}, ctx.Err()
	})
	bridge := awscredentials.Adapter{Provider: rotating}
	first, err := bridge.Retrieve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	second, err := bridge.Retrieve(t.Context())
	if err != nil || first.AccessKeyID == second.AccessKeyID || !second.CanExpire {
		t.Fatal("rotation lost", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := bridge.Retrieve(ctx); err != context.Canceled || generation != 2 {
		t.Fatal("canceled retrieval called provider", err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Retrieve(t.Context()); err == nil {
		t.Fatal("closed source usable")
	}
}

func TestZeroSourceFailsBeforeAcquisition(t *testing.T) {
	var source credentials.Source
	if err := source.Start(t.Context()); err == nil {
		t.Fatal("zero credential source started")
	}
}
