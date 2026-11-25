package httpdto

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin http-dtos-1
// UpdateProfile distinguishes omitted fields from explicit nickname clearing.
//
//foundry:dto
type UpdateProfile struct {
	Email    value.Optional[string]                 `json:"email,omitzero"`
	Nickname value.Optional[value.Nullable[string]] `json:"nickname,omitzero"`
}

// ProfileBody retains the generated bounded JSON decoder.
var ProfileBody = UpdateProfileJSON()

// doc-example:end http-dtos-1

// doc-example:begin http-dtos-2
// ProfileTags publishes a non-null JSON array.
//
//foundry:dto role=response
type ProfileTags struct {
	Tags value.List[string] `json:"tags"`
}

// doc-example:end http-dtos-2

func decodeProfile(ctx context.Context, payload []byte) (UpdateProfile, error) {
	// doc-example:begin http-dtos-3
	limits := contract.JSONLimits{
		Bytes:  4096,
		Depth:  16,
		Nodes:  500,
		Steps:  2000,
		Issues: 20,
	}
	request, err := ProfileBody.Decode(ctx, payload, limits)
	if err != nil {
		return UpdateProfile{}, err
	}
	// doc-example:end http-dtos-3
	return request, nil
}
