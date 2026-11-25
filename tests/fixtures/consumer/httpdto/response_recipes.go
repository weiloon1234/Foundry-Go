package httpdto

import (
	"context"

	"foundry.test/consumer/models"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// doc-example:begin http-responses-1
func encodeUser(ctx context.Context, userID model.ID[models.User], email string, activeState models.Status, limits contract.JSONLimits) ([]byte, error) {
	response := UserResponse{ID: userID, Email: email, State: activeState}
	body, err := UserResponseJSON().Encode(ctx, response, limits)
	return body, err
}

// doc-example:end http-responses-1

// doc-example:begin http-responses-2
func encodeUsers(ctx context.Context, response UserResponse, limits contract.JSONLimits) ([]byte, error) {
	users := contract.Slice(UserResponseJSON())
	body, err := users.Encode(ctx, []UserResponse{response}, limits)
	return body, err
}

func encodePresentUser(ctx context.Context, response UserResponse, limits contract.JSONLimits) ([]byte, error) {
	optionalUser := contract.Nullable(UserResponseJSON())
	body, err := optionalUser.Encode(ctx, value.Of(response), limits)
	return body, err
}
func encodeNullUser(ctx context.Context, limits contract.JSONLimits) ([]byte, error) {
	optionalUser := contract.Nullable(UserResponseJSON())
	body, err := optionalUser.Encode(ctx, value.Null[UserResponse](), limits)
	return body, err
}

// doc-example:end http-responses-2
