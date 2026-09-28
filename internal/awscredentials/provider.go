// Package awscredentials adapts the framework credential boundary to the one
// existing AWS SDK. Native credential types stay behind feature adapters.
package awscredentials

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/weiloon1234/Foundry-Go/cloud/credentials"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
)

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
	return aws.Credentials{AccessKeyID: value.AccessKey.Reveal(), SecretAccessKey: value.SecretKey.Reveal(), SessionToken: value.SessionToken.Reveal(), CanExpire: !value.Expires.IsZero(), Expires: value.Expires, Source: "Foundry"}, nil
}
