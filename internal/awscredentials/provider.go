// Package awscredentials adapts the framework credential boundary to the one
// existing AWS SDK. Native credential types stay behind feature adapters.
package awscredentials

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
)

// RefreshInterval bounds how long credentials without a known expiry are
// cached. The SDK credentials cache never refreshes non-expiring values, so a
// rotating provider that does not report expiry must still be consulted again.
const RefreshInterval = 5 * time.Minute

// refreshSource marks a synthetic cache deadline. It is not a credential
// expiry and must not shorten presigned URL lifetimes.
const refreshSource = "FoundryRefresh"

type Adapter struct{ Provider credentials.Provider }

func (a Adapter) Retrieve(ctx context.Context) (aws.Credentials, error) {
	if credential.IsNil(a.Provider) || ctx == nil {
		return aws.Credentials{}, fault.New(fault.Invalid, "cloud credential provider is missing")
	}
	if err := ctx.Err(); err != nil {
		return aws.Credentials{}, err
	}
	value, err := a.Provider.Retrieve(ctx)
	if err != nil {
		return aws.Credentials{}, err
	}
	if err = value.Validate(); err != nil {
		return aws.Credentials{}, err
	}
	result := aws.Credentials{AccessKeyID: value.AccessKey.Reveal(), SecretAccessKey: value.SecretKey.Reveal(), SessionToken: value.SessionToken.Reveal(), CanExpire: true, Expires: value.Expires, Source: "Foundry"}
	if value.Expires.IsZero() {
		// Unknown expiry is treated as expiring so rotations are observed.
		result.Expires, result.Source = time.Now().Add(RefreshInterval), refreshSource
	}
	return result, nil
}

// KnownExpiry reports a real temporary-credential expiry, excluding the
// synthetic refresh deadline assigned to credentials of unknown lifetime.
func KnownExpiry(value aws.Credentials) (time.Time, bool) {
	if !value.CanExpire || value.Source == refreshSource || value.Expires.IsZero() {
		return time.Time{}, false
	}
	return value.Expires, true
}
